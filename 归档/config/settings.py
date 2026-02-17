"""全局配置

GO:migrate → go/internal/config/settings.go
GO:target  → Go viper 或 自写 JSON 配置
"""

import hashlib
import json
import logging
import os
import shutil
import sys
import uuid
from collections.abc import Generator
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

try:
    import fcntl
except ImportError:  # pragma: no cover
    fcntl = None  # type: ignore[assignment]

from dotenv import load_dotenv

from utils import as_float_env, as_int_env

load_dotenv()


# ========================
# LLM 配置
# ========================
LLM_MODEL = os.getenv("LLM_MODEL", "gpt-4o")
LLM_TEMPERATURE = as_float_env("LLM_TEMPERATURE", 0.7, min_value=0.0)
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
OPENAI_BASE_URL = os.getenv("OPENAI_BASE_URL", None)  # 支持第三方中转 API

# LLM 健壮性配置
LLM_TIMEOUT = as_int_env("LLM_TIMEOUT", 120, min_value=1)  # 单次 LLM 调用超时(秒)
LLM_MAX_RETRIES = as_int_env("LLM_MAX_RETRIES", 3, min_value=0)  # LLM 调用最大重试次数
GATEWAY_TIMEOUT = as_int_env("GATEWAY_TIMEOUT", 240, min_value=1)  # 单个 Gateway 执行超时(秒)
GATEWAY_MAX_ATTEMPTS = as_int_env("GATEWAY_MAX_ATTEMPTS", 2, min_value=1)  # Gateway 最大尝试次数（默认1次重试）

# ========================
# 日志配置
# ========================
LOG_LEVEL = os.getenv("LOG_LEVEL", "INFO")

# ========================
# 架构配置 (动态加载)
# ========================
CONFIG_FILE = Path(__file__).parent.parent / "config.json"
CONFIG_BACKUP_DIR = Path(__file__).parent.parent / "data" / "config_backups"
CONFIG_BACKUP_ENABLED = os.getenv("CONFIG_BACKUP_ENABLED", "1") == "1"
CONFIG_BACKUP_KEEP = as_int_env("CONFIG_BACKUP_KEEP", 50, min_value=1)


_settings_logger = logging.getLogger(__name__)


def load_architecture_raw() -> dict:
    """加载原始 config.json 内容"""
    if not CONFIG_FILE.exists():
        return {"gateways": []}
    try:
        with open(CONFIG_FILE, "r", encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, OSError) as exc:
        _settings_logger.error("config.json 解析失败: %s", exc)
        return {"gateways": []}


def _backup_config_file() -> str:
    if not CONFIG_FILE.exists():
        return ""

    CONFIG_BACKUP_DIR.mkdir(parents=True, exist_ok=True)
    ts = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup_name = f"config-{ts}-{uuid.uuid4().hex[:6]}.json"
    backup_path = CONFIG_BACKUP_DIR / backup_name
    shutil.copy2(CONFIG_FILE, backup_path)

    backups = sorted(CONFIG_BACKUP_DIR.glob("config-*.json"), key=lambda p: p.stat().st_mtime, reverse=True)
    for stale in backups[CONFIG_BACKUP_KEEP:]:
        try:
            stale.unlink()
        except OSError:
            pass

    return str(backup_path)


@contextmanager
def _file_lock(lock_path: Path) -> Generator[None, None, None]:
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with open(lock_path, "a+", encoding="utf-8") as lock_file:
        if fcntl is not None:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            if fcntl is not None:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)


def _fsync_directory(path: Path) -> None:
    try:
        dir_fd = os.open(str(path), os.O_RDONLY)
    except OSError:
        return
    try:
        os.fsync(dir_fd)
    except OSError:
        pass
    finally:
        os.close(dir_fd)


def save_architecture(data: dict) -> str:
    """保存架构配置到 config.json（跨进程锁 + 原子写入 + 可选备份）"""
    lock_file = CONFIG_FILE.with_suffix(CONFIG_FILE.suffix + ".lock")
    with _file_lock(lock_file):
        backup_path = ""
        if CONFIG_BACKUP_ENABLED:
            backup_path = _backup_config_file()

        tmp_file = CONFIG_FILE.with_suffix(".json.tmp")
        with open(tmp_file, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
            f.flush()
            os.fsync(f.fileno())

        tmp_file.replace(CONFIG_FILE)
        _fsync_directory(CONFIG_FILE.parent)

    return backup_path


def _build_agent_command(agent: dict) -> dict:
    agent_id = agent["id"]
    agent_name = agent.get("name", "")
    module_name = agent.get("module", "")
    plugin_names = _as_list(agent.get("plugins", []))

    if module_name:
        args = ["-m", module_name]
    else:
        args = [
            "-m",
            "agents.runtime_agent",
            "--id",
            agent_id,
        ]
        if agent_name:
            args.extend(["--name", agent_name])
        if plugin_names:
            args.extend(["--plugins", ",".join(plugin_names)])

    return {
        "command": sys.executable,
        "args": args,
        "transport": "stdio",
    }


def _as_list(values: Any) -> list[str]:
    if isinstance(values, list):
        return [str(v).strip() for v in values if str(v).strip()]
    if values is None:
        return []
    text = str(values).strip()
    return [text] if text else []


def _build_gateway_capability_summary(agent_meta: dict, gateway_capabilities: list[str]) -> list[str]:
    merged = list(dict.fromkeys(gateway_capabilities))
    seen = set(merged)

    for meta in agent_meta.values():
        for cap in meta.get("capabilities", []):
            if cap in seen:
                continue
            merged.append(cap)
            seen.add(cap)

    return merged


def _build_gateway_map(data: dict) -> dict:
    gw_map = {}

    for gateway in data.get("gateways", []):
        agents = {}
        agent_meta = {}

        for agent in gateway.get("agents", []):
            agent_id = agent["id"]
            agents[agent_id] = _build_agent_command(agent)
            agent_meta[agent_id] = {
                "id": agent_id,
                "name": agent.get("name", agent_id),
                "capabilities": _as_list(agent.get("capabilities", [])),
                "depends_on": _as_list(agent.get("depends_on", [])),
                "plugins": _as_list(agent.get("plugins", [])),
            }

        gateway_capabilities = _as_list(gateway.get("capabilities", []))
        merged_capabilities = _build_gateway_capability_summary(agent_meta, gateway_capabilities)

        gw_map[gateway["id"]] = {
            "name": gateway["name"],
            "description": gateway.get("description", ""),
            "capabilities": merged_capabilities,
            "agent_meta": agent_meta,
            "agents": agents,
        }

    return gw_map


def load_architecture() -> dict:
    """从 config.json 加载 Gateway/Agent 架构"""
    data = load_architecture_raw()
    return _build_gateway_map(data)


def load_architecture_snapshot() -> dict:
    raw = load_architecture_raw()
    normalized = json.dumps(raw, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return {
        "raw": raw,
        "gateway_map": _build_gateway_map(raw),
        "hash": f"sha256:{hashlib.sha256(normalized.encode('utf-8')).hexdigest()}",
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
