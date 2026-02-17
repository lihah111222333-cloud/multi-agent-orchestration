"""PostgreSQL 本地自动启动（Homebrew / pg_ctl）。

在 ensure_schema() 之前调用 auto_start_postgres()，自动完成：
1. pg_isready 检测 PG 是否在线
2. 清理 stale postmaster.pid（PID 进程不存在时）
3. pg_ctl start 启动
4. 创建缺失的用户和数据库
5. 自动恢复最新备份（如果目标表为空）

环境变量：
  PG_AUTOSTART_ENABLED   = 1（默认启用，生产环境设 0）
  PG_BREW_SERVICE_NAME   = postgresql@16（Homebrew formula 名）
  PG_DATA_DIR            = 自动探测 /opt/homebrew/var/<service>
  PG_AUTOSTART_TIMEOUT   = 15（等待就绪最大秒数）
"""

from __future__ import annotations

import logging
import os
import re
import signal
import subprocess
import sys
import time
from pathlib import Path
from urllib.parse import urlparse

_logger = logging.getLogger("pg_autostart")
_started: bool = False  # 每个进程只尝试一次


def _env_bool(key: str, default: bool = True) -> bool:
    val = str(os.getenv(key, str(int(default)))).strip().lower()
    return val not in {"0", "false", "no", "off", ""}


def _parse_conn_string() -> dict[str, str]:
    """从 POSTGRES_CONNECTION_STRING 解析 host / port / user / password / dbname。"""
    raw = os.getenv("POSTGRES_CONNECTION_STRING", "")
    if not raw:
        return {}
    parsed = urlparse(raw)
    return {
        "host": parsed.hostname or "localhost",
        "port": str(parsed.port or 5432),
        "user": parsed.username or "",
        "password": parsed.password or "",
        "dbname": parsed.path.lstrip("/") or "postgres",
    }


def _find_pg_bin(binary: str) -> str | None:
    """查找 PG 二进制，优先 Homebrew 安装路径。"""
    service = os.getenv("PG_BREW_SERVICE_NAME", "postgresql@16")
    candidates = [
        f"/opt/homebrew/opt/{service}/bin/{binary}",
        f"/usr/local/opt/{service}/bin/{binary}",
        f"/opt/homebrew/bin/{binary}",
        f"/usr/local/bin/{binary}",
    ]
    for p in candidates:
        if os.path.isfile(p) and os.access(p, os.X_OK):
            return p
    # fallback: PATH
    try:
        result = subprocess.run(
            ["which", binary], capture_output=True, text=True, timeout=5,
        )
        if result.returncode == 0:
            return result.stdout.strip()
    except Exception:
        pass
    return None


def _pg_isready(host: str, port: str) -> bool:
    """调用 pg_isready 检测 PG 是否在线。"""
    pg_isready = _find_pg_bin("pg_isready")
    if not pg_isready:
        _logger.debug("[pg-autostart] pg_isready 不在 PATH，跳过检测")
        return True  # 无法检测就假定在线，让后续连接自己报错
    try:
        r = subprocess.run(
            [pg_isready, "-h", host, "-p", port],
            capture_output=True, text=True, timeout=5,
        )
        return r.returncode == 0
    except Exception:
        return False


def _get_data_dir() -> str | None:
    """获取 PG 数据目录。"""
    explicit = os.getenv("PG_DATA_DIR", "").strip()
    if explicit:
        return explicit
    service = os.getenv("PG_BREW_SERVICE_NAME", "postgresql@16")
    candidates = [
        f"/opt/homebrew/var/{service}",
        f"/usr/local/var/{service}",
    ]
    for d in candidates:
        if os.path.isdir(d):
            return d
    return None


def _clean_stale_pid(data_dir: str) -> bool:
    """如果 postmaster.pid 指向的进程不存在，删除它。返回是否清理了。"""
    pid_file = os.path.join(data_dir, "postmaster.pid")
    if not os.path.exists(pid_file):
        return False
    try:
        with open(pid_file) as f:
            pid_str = f.readline().strip()
        pid = int(pid_str)
        os.kill(pid, 0)  # 进程存在时不抛异常
        return False
    except (ValueError, ProcessLookupError, PermissionError):
        # 进程不存在或无法确认 → 清理
        try:
            os.remove(pid_file)
            _logger.info("[pg-autostart] 清理 stale postmaster.pid (pid=%s)", pid_str)
            return True
        except OSError as e:
            _logger.warning("[pg-autostart] 无法删除 postmaster.pid: %s", e)
            return False
    except Exception:
        return False


def _start_pg(data_dir: str) -> bool:
    """通过 pg_ctl 启动 PostgreSQL。"""
    pg_ctl = _find_pg_bin("pg_ctl")
    if not pg_ctl:
        _logger.warning("[pg-autostart] 找不到 pg_ctl，无法自动启动")
        return False

    service = os.getenv("PG_BREW_SERVICE_NAME", "postgresql@16")
    log_file = f"/opt/homebrew/var/log/{service}.log"
    # 确保日志目录存在
    os.makedirs(os.path.dirname(log_file), exist_ok=True)

    _logger.info("[pg-autostart] PostgreSQL 未运行，正在启动...")
    try:
        r = subprocess.run(
            [pg_ctl, "-D", data_dir, "-l", log_file, "start"],
            capture_output=True, text=True, timeout=30,
        )
        if r.returncode == 0:
            _logger.info("[pg-autostart] pg_ctl start 成功")
            return True
        else:
            _logger.error("[pg-autostart] pg_ctl start 失败: %s", r.stderr.strip())
            return False
    except subprocess.TimeoutExpired:
        _logger.error("[pg-autostart] pg_ctl start 超时")
        return False
    except Exception as e:
        _logger.error("[pg-autostart] pg_ctl start 异常: %s", e)
        return False


def _wait_for_ready(host: str, port: str, timeout: int = 15) -> bool:
    """轮询等待 PG 就绪。"""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if _pg_isready(host, port):
            return True
        time.sleep(1)
    return False


def _ensure_user_and_db(host: str, port: str, user: str, password: str, dbname: str) -> None:
    """确保目标用户和数据库存在（通过系统用户连接 postgres 库执行）。"""
    psql = _find_pg_bin("psql")
    if not psql:
        return

    def _run_sql(sql: str, db: str = "postgres") -> subprocess.CompletedProcess:
        return subprocess.run(
            [psql, "-h", host, "-p", port, "-d", db, "-c", sql],
            capture_output=True, text=True, timeout=10,
        )

    # 创建用户
    if user:
        check = _run_sql(f"SELECT 1 FROM pg_roles WHERE rolname = '{user}';")
        if "1" not in (check.stdout or ""):
            escaped_pw = password.replace("'", "''")
            _run_sql(f"CREATE ROLE {user} WITH LOGIN PASSWORD '{escaped_pw}';")
            _logger.info("[pg-autostart] 创建用户 %s", user)

    # 创建数据库
    if dbname and dbname != "postgres":
        check = _run_sql(f"SELECT 1 FROM pg_database WHERE datname = '{dbname}';")
        if "1" not in (check.stdout or ""):
            owner_clause = f" OWNER {user}" if user else ""
            _run_sql(f"CREATE DATABASE {dbname}{owner_clause};")
            _logger.info("[pg-autostart] 创建数据库 %s", dbname)

    # 授权
    if user and dbname:
        _run_sql(f"GRANT ALL PRIVILEGES ON DATABASE {dbname} TO {user};")


def _find_latest_backup() -> str | None:
    """查找 db/backups/ 下最新的 .sql 备份文件。"""
    backup_dir = Path(__file__).resolve().parent / "backups"
    if not backup_dir.is_dir():
        return None
    sql_files = sorted(backup_dir.glob("*.sql"), key=lambda p: p.stat().st_mtime, reverse=True)
    return str(sql_files[0]) if sql_files else None


def _db_has_data(host: str, port: str, user: str, dbname: str) -> bool:
    """检查数据库是否已有数据（通过查询任意一张业务表的行数）。"""
    psql = _find_pg_bin("psql")
    if not psql:
        return True  # 无法检测就跳过恢复
    try:
        r = subprocess.run(
            [psql, "-h", host, "-p", port, "-U", user, "-d", dbname, "-t", "-c",
             "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE';"],
            capture_output=True, text=True, timeout=10,
        )
        count = int(r.stdout.strip()) if r.returncode == 0 else 0
        return count > 0
    except Exception:
        return True  # 出错时不冒险恢复


def _restore_backup(host: str, port: str, user: str, dbname: str, backup_path: str) -> None:
    """用 psql 恢复备份。"""
    psql = _find_pg_bin("psql")
    if not psql:
        return
    _logger.info("[pg-autostart] 正在恢复备份: %s", os.path.basename(backup_path))
    try:
        r = subprocess.run(
            [psql, "-h", host, "-p", port, "-U", user, "-d", dbname, "-f", backup_path],
            capture_output=True, text=True, timeout=120,
        )
        if r.returncode == 0:
            _logger.info("[pg-autostart] 备份恢复完成")
        else:
            # 索引已存在等非致命错误很常见，只要数据进去了就行
            errors = [l for l in (r.stderr or "").splitlines() if "ERROR" in l and "already exists" not in l]
            if errors:
                _logger.warning("[pg-autostart] 恢复有非索引错误: %s", "; ".join(errors[:3]))
            else:
                _logger.info("[pg-autostart] 备份恢复完成（索引已存在的警告已忽略）")
    except subprocess.TimeoutExpired:
        _logger.error("[pg-autostart] 备份恢复超时")
    except Exception as e:
        _logger.error("[pg-autostart] 备份恢复异常: %s", e)


def auto_start_postgres() -> None:
    """自动启动本地 PostgreSQL（幂等，每进程只执行一次）。"""
    global _started
    if _started:
        return
    _started = True

    if not _env_bool("PG_AUTOSTART_ENABLED", default=True):
        return

    conn_info = _parse_conn_string()
    if not conn_info:
        return  # 未配置连接串，跳过

    host = conn_info["host"]
    port = conn_info["port"]

    # 只对本地连接自动启动
    if host not in ("localhost", "127.0.0.1", "::1"):
        return

    # 已经在线就跳过
    if _pg_isready(host, port):
        _logger.debug("[pg-autostart] PostgreSQL 已在线 (%s:%s)", host, port)
        return

    # 尝试启动
    data_dir = _get_data_dir()
    if not data_dir:
        _logger.warning("[pg-autostart] 找不到 PG 数据目录，无法自动启动")
        return

    # 清理可能的 stale PID 文件
    _clean_stale_pid(data_dir)

    # 启动
    if not _start_pg(data_dir):
        return

    # 等待就绪
    timeout = int(os.getenv("PG_AUTOSTART_TIMEOUT", "15"))
    if not _wait_for_ready(host, port, timeout):
        _logger.error("[pg-autostart] PostgreSQL 启动超时 (%ds)，请手动检查", timeout)
        return

    _logger.info("[pg-autostart] PostgreSQL 已就绪 (%s:%s)", host, port)

    # 确保用户和数据库存在
    user = conn_info.get("user", "")
    password = conn_info.get("password", "")
    dbname = conn_info.get("dbname", "")
    if user or dbname:
        try:
            _ensure_user_and_db(host, port, user, password, dbname)
        except Exception as e:
            _logger.warning("[pg-autostart] 创建用户/库时出错: %s", e)

    # 如果数据库为空，自动恢复最新备份
    if user and dbname:
        try:
            if not _db_has_data(host, port, user, dbname):
                backup = _find_latest_backup()
                if backup:
                    _restore_backup(host, port, user, dbname, backup)
                else:
                    _logger.debug("[pg-autostart] 未找到备份文件，跳过恢复")
        except Exception as e:
            _logger.warning("[pg-autostart] 备份恢复检查失败: %s", e)
