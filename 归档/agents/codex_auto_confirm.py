#!/usr/bin/env python3
"""
Codex 自动确认 v10 (iTerm2 API)
通过 iTerm2 Python API 监控所有终端会话，自动确认 Codex CLI 的审批弹窗。

用法:
    python3 codex_auto_confirm.py run

前置条件:
    1. pip3 install iterm2
    2. iTerm2 → Settings → General → Magic → Enable Python API ✓
"""

import iterm2
import asyncio
import hashlib
import os
import re
import signal
import subprocess
import sys
import time
import unicodedata
from datetime import datetime
from pathlib import Path

# ── 配置 ──────────────────────────────────────────────
VERSION = "v11"
SCAN_INTERVAL = 0.5        # 扫描间隔（秒）
LOG_INTERVAL  = 60         # 状态日志间隔（秒）
BOTTOM_LINES  = 30         # 读取屏幕底部行数（增大以覆盖长弹窗）
DEBUG         = False      # 调试模式：输出每次扫描的屏幕内容
CONFIRM_COOLDOWN = 3.0     # 同一会话两次确认之间的最小间隔（秒）

# Codex CLI 确认弹窗关键词（全部小写匹配）
# 仅匹配 Codex TUI 弹窗，发送 Enter 选择默认 "Yes"，不会在输入框输入任何字符
CONFIRM_PATTERNS = [
    # ── 新版 Codex CLI (2025+) ──
    "would you like to run the following command",  # 主弹窗标题
    "yes, proceed",                                  # 选项文本
    "press enter to confirm",                        # 底部提示
    # ── 旧版 Codex CLI ──
    "codex wants to",          # "Codex wants to run ..."
    "allow command",           # "Allow command?"
    "apply changes?",
    "apply patch?",
    "apply these changes?",
    "do you want to apply",
]

# 发送的确认按键（仅 Enter，不发送任何字符）
# 注意：终端 raw 模式下 Enter 产生 \r（CR=0x0D），不是 \n（LF=0x0A）
CONFIRM_KEY = "\r"


# ── 核心类 ────────────────────────────────────────────
class AutoConfirmer:
    """监控 iTerm2 所有会话，自动确认 Codex CLI 弹窗。"""

    def __init__(self):
        self.confirm_count = 0
        self.scan_count    = 0
        self.cache: dict[str, str] = {}   # session_id → content_hash
        self.last_confirm: dict[str, float] = {}  # session_id → 上次确认时间
        self.start_time    = time.time()
        self.last_log_time = 0.0

    # ── 工具方法 ──────────────────────────────────────

    @staticmethod
    def _ts() -> str:
        return datetime.now().strftime("%H:%M:%S")

    def _runtime(self) -> str:
        s = int(time.time() - self.start_time)
        return f"{s // 3600}h{(s % 3600) // 60}m"

    def _log(self, msg: str):
        print(f"[{self._ts()}] {msg}", flush=True)

    def _log_status(self):
        self._log(
            f"\U0001f7e5 确认 {self.confirm_count} | "
            f"扫描 {self.scan_count} | "
            f"缓存 {len(self.cache)} | "
            f"运行 {self._runtime()}"
        )

    @staticmethod
    def _content_hash(text: str) -> str:
        return hashlib.md5(text.encode("utf-8", errors="replace")).hexdigest()

    @staticmethod
    def _normalize(text: str) -> str:
        """去除 TUI 特殊字符（箭头、选择符号、box-drawing 等），仅保留可读文本。"""
        # 将全角字符转半角
        text = unicodedata.normalize("NFKC", text)
        # 去掉非 ASCII 标点/符号（保留字母、数字、基本标点、空格、换行）
        # 这可以处理 ›、▶、│ 等 TUI 装饰符号
        cleaned = []
        for ch in text:
            if ch in ('\n', '\r', '\t', ' '):
                cleaned.append(ch)
            elif ch.isalnum():
                cleaned.append(ch)
            elif ch in '.,;:!?\'"()-_/\\@#$%&*+=<>[]{}|~`^':
                cleaned.append(ch)
            # 其他字符（TUI 装饰）替换为空格
            else:
                cleaned.append(' ')
        return ''.join(cleaned)

    @staticmethod
    def _needs_confirm(text: str) -> bool:
        """检测屏幕是否包含 Codex 确认弹窗。"""
        low = text.lower()
        # 同时对标准化后的文本进行匹配
        normalized_low = AutoConfirmer._normalize(low)
        # 压缩多余空格
        normalized_low = re.sub(r'\s+', ' ', normalized_low)
        return (
            any(pat in low for pat in CONFIRM_PATTERNS) or
            any(pat in normalized_low for pat in CONFIRM_PATTERNS)
        )

    # ── 屏幕读取 ──────────────────────────────────────

    async def _read_bottom(self, session, n: int = BOTTOM_LINES) -> str:
        """读取会话屏幕底部 n 行文本（正确处理 scrollback）。"""
        try:
            contents = await session.async_get_screen_contents()
            total = contents.number_of_lines
            # 读取整个可见区域（最多 n 行），从底部向上
            start = max(0, total - n)
            lines = []
            for i in range(start, total):
                try:
                    line = contents.line(i)
                    lines.append(line.string)
                except Exception:
                    pass
            return "\n".join(lines)
        except Exception as e:
            if DEBUG:
                self._log(f"⚠️ _read_bottom 异常: {e}")
            return ""

    # ── 处理单个会话 ──────────────────────────────────

    async def _process_session(self, session):
        self.scan_count += 1

        text = await self._read_bottom(session)
        if not text.strip():
            self.cache.pop(session.session_id, None)
            return

        if DEBUG:
            name = session.session_id
            try:
                n = await session.async_get_variable("name")
                if n:
                    name = n
            except Exception:
                pass
            self._log(f"🔍 [{name}] 读取到 {len(text)} 字符:")
            # 打印每行的 repr 来暴露隐藏字符
            for i, ln in enumerate(text.split('\n')[-15:]):
                self._log(f"   L{i:02d}: {repr(ln)}")
            self._log(f"   _needs_confirm = {self._needs_confirm(text)}")

        if not self._needs_confirm(text):
            self.cache.pop(session.session_id, None)
            self.last_confirm.pop(session.session_id, None)
            return

        # 冷却期：避免对同一会话疯狂发 Enter
        now = time.time()
        last_t = self.last_confirm.get(session.session_id, 0.0)
        if now - last_t < CONFIRM_COOLDOWN:
            return

        # 发送 Enter（\r）选择默认 "Yes"
        try:
            await session.async_send_text(CONFIRM_KEY)
            self.last_confirm[session.session_id] = now
            self.confirm_count += 1

            name = session.session_id
            try:
                n = await session.async_get_variable("name")
                if n:
                    name = n
            except Exception:
                pass

            self._log(f"✅ 已确认 [{name}] (CR)")
        except Exception as e:
            self._log(f"❌ 确认失败: {e}")

    # ── 主循环 ────────────────────────────────────────

    async def run(self, connection):
        app = await iterm2.async_get_app(connection)
        self._log(f"🚀 Codex 自动确认 {VERSION} (iTerm2 API) 启动")
        self.last_log_time = time.time()

        while True:
            try:
                # 遍历所有窗口 → 标签 → 会话
                for window in app.terminal_windows:
                    for tab in window.tabs:
                        for session in tab.sessions:
                            await self._process_session(session)

                # 定时输出状态
                now = time.time()
                if now - self.last_log_time >= LOG_INTERVAL:
                    self.last_log_time = now
                    self._log_status()

            except Exception as e:
                self._log(f"⚠️ 扫描异常: {e}")

            await asyncio.sleep(SCAN_INTERVAL)


# ── iTerm2 入口 ───────────────────────────────────────

async def _main(connection):
    await AutoConfirmer().run(connection)


# ── 路径常量 ───────────────────────────────────────────
PID_FILE = Path.home() / ".codex_auto_confirm.pid"
LOG_FILE = Path.home() / ".codex_auto_confirm.log"


def cmd_run():
    """前台启动自动确认守护进程（iterm2.run_forever）。"""
    iterm2.run_forever(_main)


def cmd_start():
    """后台启动：通过 nohup 将自身以 run 模式启动为守护进程。"""
    # 检查是否已有实例在运行
    if PID_FILE.exists():
        pid = PID_FILE.read_text().strip()
        try:
            os.kill(int(pid), 0)  # 检查进程是否存在
            print(f"⚠️  已有实例在运行 (PID {pid})，如需重启请先执行 stop")
            return
        except (ProcessLookupError, ValueError):
            PID_FILE.unlink(missing_ok=True)  # 清理残留 PID 文件

    script = os.path.abspath(__file__)
    log = str(LOG_FILE)
    proc = subprocess.Popen(
        [sys.executable, script, "run"],
        stdout=open(log, "a"),
        stderr=subprocess.STDOUT,
        stdin=subprocess.DEVNULL,
        start_new_session=True,  # macOS 上等价于 setsid，脱离终端
    )
    PID_FILE.write_text(str(proc.pid))
    print(f"🚀 后台启动成功 (PID {proc.pid})")
    print(f"   日志: {log}")
    print(f"   PID 文件: {PID_FILE}")
    print(f"   停止: python3 {script} stop")


def cmd_stop():
    """停止后台运行的守护进程。"""
    if not PID_FILE.exists():
        print("ℹ️  没有找到运行中的实例（PID 文件不存在）")
        return

    pid_str = PID_FILE.read_text().strip()
    try:
        pid = int(pid_str)
    except ValueError:
        print(f"❌ PID 文件内容无效: {pid_str}")
        PID_FILE.unlink(missing_ok=True)
        return

    try:
        os.kill(pid, signal.SIGTERM)
        print(f"✅ 已发送 SIGTERM 到 PID {pid}")
    except ProcessLookupError:
        print(f"ℹ️  进程 {pid} 不存在（可能已退出）")
    except PermissionError:
        print(f"❌ 无权限终止进程 {pid}")
        return

    PID_FILE.unlink(missing_ok=True)
    print("   PID 文件已清理")


def cmd_status():
    """查看后台守护进程状态。"""
    if not PID_FILE.exists():
        print("ℹ️  没有找到运行中的实例（PID 文件不存在）")
        return

    pid_str = PID_FILE.read_text().strip()
    try:
        pid = int(pid_str)
    except ValueError:
        print(f"❌ PID 文件内容无效: {pid_str}")
        return

    try:
        os.kill(pid, 0)
        print(f"🟢 运行中 (PID {pid})")
        print(f"   日志: {LOG_FILE}")
    except ProcessLookupError:
        print(f"🔴 进程 {pid} 已退出（PID 文件残留）")
        PID_FILE.unlink(missing_ok=True)
    except PermissionError:
        print(f"🟡 进程 {pid} 存在但无权限检查")


# ── CLI ───────────────────────────────────────────────

USAGE = f"""\
Codex 自动确认 {VERSION} (iTerm2 API)

用法:
    python3 codex_auto_confirm.py run        前台启动自动确认
    python3 codex_auto_confirm.py start      后台启动（守护进程）
    python3 codex_auto_confirm.py stop       停止后台进程
    python3 codex_auto_confirm.py status     查看后台进程状态
    python3 codex_auto_confirm.py debug      调试模式（打印屏幕内容）

前置条件:
    pip3 install iterm2
    iTerm2 → Settings → General → Magic → Enable Python API ✓
"""

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(USAGE)
        sys.exit(0)

    cmd = sys.argv[1].lower()
    if cmd == "run":
        cmd_run()
    elif cmd == "start":
        cmd_start()
    elif cmd == "stop":
        cmd_stop()
    elif cmd == "status":
        cmd_status()
    elif cmd == "debug":
        DEBUG = True
        print(f"🐛 调试模式已启用 — 将打印每次扫描的屏幕内容")
        cmd_run()
    elif cmd in ("-h", "--help", "help"):
        print(USAGE)
        sys.exit(0)
    else:
        print(f"未知命令: {cmd}")
        print(USAGE)
        sys.exit(1)
