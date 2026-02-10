"""Telegram Bot Bridge — 将 Master Agent 对话通过 Telegram Bot 转发。

用法:
  1. 在 Dashboard Telegram 管理页设置 TG_BOT_TOKEN 和 TG_CHAT_ID
  2. Dashboard 启动时自动拉起 TG bridge 后台线程
  3. 用户通过 Telegram 发消息 → Master 执行 → 结果回发 Telegram

环境变量:
  TG_BOT_TOKEN   — @BotFather 获取的 Bot Token
  TG_CHAT_ID     — 允许通信的 Telegram Chat ID（留空则首个 /start 用户自动绑定）
"""

from __future__ import annotations

import asyncio
import logging
import os
import threading
import time
from collections import deque
from datetime import datetime, timezone
from typing import Any, Optional

logger = logging.getLogger(__name__)

__all__ = [
    "start_tg_bridge", "stop_tg_bridge", "is_tg_bridge_running",
    "get_tg_history", "clear_tg_history", "send_message_to_tg",
    "get_tg_bridge_info",
    "start_watchdog", "stop_watchdog", "is_watchdog_running",
    "get_watchdog_info",
]

# ---- 对话历史 ----
_MAX_HISTORY = 200
_history: deque[dict[str, Any]] = deque(maxlen=_MAX_HISTORY)
_history_lock = threading.Lock()


def _add_history(role: str, text: str, chat_id: str = "", user: str = "", status: str = "ok") -> dict:
    entry = {
        "ts": datetime.now(timezone.utc).isoformat(),
        "role": role,          # "user" / "bot" / "system"
        "text": str(text)[:4000],
        "chat_id": str(chat_id),
        "user": str(user),
        "status": status,
    }
    with _history_lock:
        _history.append(entry)
    return entry


def get_tg_history(limit: int = 50) -> list[dict]:
    with _history_lock:
        items = list(_history)
    return items[-max(1, min(limit, _MAX_HISTORY)):]


def clear_tg_history() -> None:
    with _history_lock:
        _history.clear()


# ---- 状态 ----
_bridge_lock = threading.Lock()
_bridge_thread: Optional[threading.Thread] = None
_bridge_stop_event = threading.Event()
_bridge_loop: Optional[asyncio.AbstractEventLoop] = None
_bot_info: dict[str, Any] = {}


_DEFAULT_TG_BOT_TOKEN = "8411951426:AAGzdMxTUHXhvcj9_3a3iHP2CB3Mvn8oKm8"


def _get_token() -> str:
    return os.getenv("TG_BOT_TOKEN", _DEFAULT_TG_BOT_TOKEN).strip()


def _get_chat_id() -> str:
    return os.getenv("TG_CHAT_ID", "").strip()


def _set_chat_id(chat_id: str) -> None:
    os.environ["TG_CHAT_ID"] = str(chat_id)


def _is_authorized(chat_id: int) -> bool:
    allowed = _get_chat_id()
    if not allowed:
        return True
    return str(chat_id) == allowed


def _truncate(text: str, max_len: int = 4000) -> str:
    if len(text) <= max_len:
        return text
    half = max_len // 2 - 20
    return text[:half] + "\n\n... (已截断) ...\n\n" + text[-half:]


def get_tg_bridge_info() -> dict[str, Any]:
    return {
        "running": is_tg_bridge_running(),
        "token_set": bool(_get_token()),
        "token_masked": ("..." + _get_token()[-6:]) if len(_get_token()) > 6 else "",
        "chat_id": _get_chat_id() or "(未绑定)",
        "bot_username": _bot_info.get("username", ""),
        "bot_name": _bot_info.get("first_name", ""),
        "history_count": len(_history),
        "master_tab": _get_master_tab_name(),
        "watchdog": get_watchdog_info(),
    }


# ---- 主 Agent 发现 ----
_DEFAULT_MASTER_TAB_NAME = "主agnet"
_ITERM_READ_WAIT_SEC = 8.0  # 发送后等待输出的秒数
_ITERM_READ_LINES = 60      # 读取的行数


def _get_master_tab_name() -> str:
    return os.getenv("TG_MASTER_TAB_NAME", _DEFAULT_MASTER_TAB_NAME).strip()


def _find_master_session() -> dict[str, Any] | None:
    """3 步发现主 Agent 会话:
    1. 按 tab 名匹配
    2. 扫描未绑定的 iTerm 会话
    3. 返回 None（调用方向用户确认）
    """
    try:
        from agents.iterm_bridge import _list_live_sessions, _load_state, _normalize_state_file
    except ImportError:
        logger.error("无法导入 iterm_bridge")
        return None

    try:
        _, sessions = _list_live_sessions()
    except Exception as exc:
        logger.error("iTerm 会话列表获取失败: %s", exc)
        return None

    if not sessions:
        return None

    # Step 1: 按 tab 名称匹配
    tab_name = _get_master_tab_name().lower()
    for s in sessions:
        sname = (s.get("session_name") or s.get("name") or "").lower()
        if tab_name and tab_name in sname:
            logger.info("主 Agent 发现 (tab 名匹配): session=%s name=%s",
                        s.get("session_id"), s.get("session_name"))
            return s

    # Step 2: 找未绑定的会话（不在 state file 中注册的）
    try:
        state_path = _normalize_state_file()
        state = _load_state(state_path)
        registered_ids = set()
        for row in state.get("agents", []):
            sid = str(row.get("session_id", "")).strip()
            if sid:
                registered_ids.add(sid)
    except Exception:
        registered_ids = set()

    unbound = [s for s in sessions if s.get("session_id") not in registered_ids]
    if len(unbound) == 1:
        s = unbound[0]
        logger.info("主 Agent 发现 (唯一未绑定会话): session=%s name=%s",
                    s.get("session_id"), s.get("session_name"))
        return s

    # 多个未绑定会话时，优先选名称含 agent 关键词的
    if unbound:
        for s in unbound:
            sname = (s.get("session_name") or s.get("name") or "").lower()
            if any(kw in sname for kw in ("agent", "master", "codex", "claude", "主")):
                logger.info("主 Agent 发现 (关键词匹配): session=%s name=%s",
                            s.get("session_id"), s.get("session_name"))
                return s

    # Step 3: 没找到
    return None


def _send_to_iterm_session(session_id: str, text: str) -> str:
    """向指定 iTerm session 发送文本并读取输出。"""
    try:
        from agents.iterm_bridge import AgentSession, _run_iterm_io
    except ImportError:
        return "❌ 无法导入 iterm_bridge"

    target = AgentSession(
        index=0,
        agent_id="master",
        agent_name="Master Agent",
        session_id=session_id,
    )

    try:
        # 先读取发送前的输出（用于后续 diff）
        before_rows = _run_iterm_io(
            targets=[target], text=None, append_enter=False,
            wait_sec=0, read_lines=_ITERM_READ_LINES,
        )
        before_lines = set()
        if before_rows:
            before_lines = set(before_rows[0].get("output", []))

        # 发送消息
        rows = _run_iterm_io(
            targets=[target], text=text, append_enter=True,
            wait_sec=_ITERM_READ_WAIT_SEC, read_lines=_ITERM_READ_LINES,
        )

        if not rows:
            return "❌ iTerm 无响应"

        row = rows[0]
        if row.get("error"):
            return f"❌ iTerm 错误: {row['error']}"

        # 提取新增输出
        all_lines = row.get("output", [])
        new_lines = [l for l in all_lines if l not in before_lines]

        if new_lines:
            return "\n".join(new_lines)
        elif all_lines:
            return "\n".join(all_lines[-20:])
        else:
            return "(主 Agent 暂无输出)"

    except Exception as exc:
        logger.error("iTerm 交互失败: %s", exc, exc_info=True)
        return f"❌ iTerm 交互失败: {exc}"


async def _run_master_task(task: str) -> str:
    """将任务转发到主 Agent 的 iTerm 会话。"""
    # iTerm2 API 需要自己的事件循环，必须在独立线程中运行
    loop = asyncio.get_event_loop()
    session = await loop.run_in_executor(None, _find_master_session)

    if session is None:
        return (
            "⚠️ 未找到主 Agent 会话\n\n"
            "请确认:\n"
            f"1. iTerm 中有名为 \"{_get_master_tab_name()}\" 的 tab\n"
            "2. 或有未被系统绑定的 Agent 会话正在运行\n\n"
            "是否需要我唤醒主 Agent？请回复 /wake"
        )

    session_id = session.get("session_id", "")
    session_name = session.get("session_name") or session.get("name") or session_id
    logger.info("TG → iTerm [%s]: %s", session_name, task[:100])

    result = await loop.run_in_executor(
        None, _send_to_iterm_session, session_id, task
    )
    return result


async def _bot_main(stop_event: threading.Event) -> None:
    try:
        from telegram import Update
        from telegram.ext import (
            Application,
            CommandHandler,
            MessageHandler,
            ContextTypes,
            filters,
        )
    except ImportError:
        logger.error("python-telegram-bot 未安装，请运行: pip install python-telegram-bot")
        _add_history("system", "python-telegram-bot 未安装", status="error")
        return

    token = _get_token()
    if not token:
        logger.warning("TG_BOT_TOKEN 未设置，Telegram bridge 未启动")
        _add_history("system", "TG_BOT_TOKEN 未设置", status="error")
        return

    # ---- /start ----
    async def cmd_start(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        chat_id = update.effective_chat.id
        user = update.effective_user

        if not _get_chat_id():
            _set_chat_id(str(chat_id))
            logger.info("TG bridge: 自动绑定 chat_id=%s (user=%s)", chat_id, user.username)
            _add_history("system", f"自动绑定 chat_id={chat_id} user={user.username}")

        if not _is_authorized(chat_id):
            await update.message.reply_text("⛔ 未授权，请在 Dashboard 配置 TG_CHAT_ID")
            return

        await update.message.reply_text(
            f"✅ ACP-BUS Master Agent 已连接\n\n"
            f"👤 User: {user.first_name} ({user.username})\n"
            f"🆔 Chat ID: {chat_id}\n\n"
            f"直接发送消息即可与 Master Agent 对话。\n"
            f"命令: /status Agent 状态, /id Chat ID, /wake 唤醒 Agent"
        )
        _add_history("system", f"/start from {user.username} chat_id={chat_id}")

    # ---- /id ----
    async def cmd_id(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        await update.message.reply_text(f"Chat ID: {update.effective_chat.id}")

    # ---- /wake ----
    async def cmd_wake(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_authorized(update.effective_chat.id):
            await update.message.reply_text("⛔ 未授权")
            return

        loop = asyncio.get_event_loop()
        session = await loop.run_in_executor(None, _find_master_session)
        if session:
            name = session.get("session_name") or session.get("name") or session.get("session_id")
            await update.message.reply_text(
                f"✅ 主 Agent 已在运行\n"
                f"📍 会话: {name}\n"
                f"🔗 Session ID: {session.get('session_id', '')}"
            )
            return

        await update.message.reply_text(
            f"⚠️ 未检测到主 Agent 会话\n\n"
            f"请在 iTerm 中:\n"
            f"1. 新建 tab 并命名为 \"{_get_master_tab_name()}\"\n"
            f"2. 启动你的主 Agent 进程\n\n"
            f"完成后发送任意消息即可开始对话。"
        )
        _add_history("system", "/wake — 未找到主 Agent")

    # ---- /status ----
    async def cmd_status(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_authorized(update.effective_chat.id):
            await update.message.reply_text("⛔ 未授权")
            return

        try:
            from agent_monitor import patrol_agents_once
            from agents.iterm_bridge import list_iterm_agent_sessions, read_iterm_output

            snapshot = patrol_agents_once(
                list_sessions_func=list_iterm_agent_sessions,
                read_output_func=read_iterm_output,
                read_lines=10,
            )
            agents = snapshot.get("agents", [])
            if not agents:
                await update.message.reply_text("📊 当前无活跃 Agent 会话")
                return

            lines = ["📊 Agent 状态\n"]
            for a in agents:
                emoji = {"running": "🟢", "idle": "🔵", "stuck": "🟡",
                         "error": "🔴", "disconnected": "⚫"}.get(a.get("status", ""), "⚪")
                lines.append(f"{emoji} {a['agent_id']} — {a.get('status', 'unknown')}")

            s = snapshot.get("summary", {})
            lines.append(f"\n合计: {s.get('total', 0)} agents, {s.get('healthy', 0)} healthy")
            await update.message.reply_text("\n".join(lines))
        except Exception as exc:
            await update.message.reply_text(f"❌ 查询失败: {exc}")

    # ---- 普通消息 → Master ----
    async def handle_message(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        chat_id = update.effective_chat.id
        if not _is_authorized(chat_id):
            await update.message.reply_text("⛔ 未授权")
            return

        task_text = (update.message.text or "").strip()
        if not task_text:
            return

        user = update.effective_user
        username = user.username or user.first_name or str(chat_id)
        _add_history("user", task_text, chat_id=str(chat_id), user=username)
        logger.info("TG bridge: 收到任务 from %s: %s", username, task_text[:100])

        pending_msg = await update.message.reply_text(f"⏳ 任务已接收，Master 编排中...")

        answer = await _run_master_task(task_text)
        _add_history("bot", answer, chat_id=str(chat_id))

        response = _truncate(answer)
        try:
            await pending_msg.edit_text(response)
        except Exception:
            await update.message.reply_text(response)

    # ---- /watchdog ----
    async def cmd_watchdog(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_authorized(update.effective_chat.id):
            await update.message.reply_text("⛔ 未授权")
            return

        if is_watchdog_running():
            stop_watchdog()
            await update.message.reply_text("⏰ 看门狗已停止")
        else:
            start_watchdog()
            interval = _get_watchdog_interval()
            await update.message.reply_text(
                f"⏰ 看门狗已启动\n"
                f"📍 每 {interval}s 唤醒 Agent\n"
                f"📝 提示词: {_get_nudge_prompt()[:60]}\n\n"
                f"再次发送 /watchdog 关闭"
            )

    # ---- 构建 Application ----
    app = Application.builder().token(token).build()
    app.add_handler(CommandHandler("start", cmd_start))
    app.add_handler(CommandHandler("id", cmd_id))
    app.add_handler(CommandHandler("wake", cmd_wake))
    app.add_handler(CommandHandler("watchdog", cmd_watchdog))
    app.add_handler(CommandHandler("status", cmd_status))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))

    logger.info("TG bridge: Bot 启动中 (token=...%s)", token[-6:])
    _add_history("system", "Bot 启动中...")

    try:
        await app.initialize()
        await app.start()
        await app.updater.start_polling(drop_pending_updates=True)

        # 获取 bot info
        global _bot_info
        bot_me = await app.bot.get_me()
        _bot_info = {
            "username": bot_me.username or "",
            "first_name": bot_me.first_name or "",
            "id": bot_me.id,
        }
        logger.info("TG bridge: Bot 已启动 @%s", _bot_info.get("username", ""))
        _add_history("system", f"Bot 已启动 @{_bot_info.get('username', '')}")

        while not stop_event.is_set():
            await asyncio.sleep(0.5)

        logger.info("TG bridge: 关闭中...")
        _add_history("system", "Bot 关闭中...")
        await app.updater.stop()
        await app.stop()
        await app.shutdown()
    except Exception as exc:
        logger.error("TG bridge: Bot 运行异常: %s", exc, exc_info=True)
        _add_history("system", f"Bot 异常: {exc}", status="error")


def _bridge_thread_target() -> None:
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    global _bridge_loop
    _bridge_loop = loop
    try:
        loop.run_until_complete(_bot_main(_bridge_stop_event))
    except Exception as exc:
        logger.error("TG bridge thread error: %s", exc, exc_info=True)
    finally:
        _bridge_loop = None
        loop.close()


def start_tg_bridge() -> bool:
    global _bridge_thread
    token = _get_token()
    if not token:
        logger.debug("TG_BOT_TOKEN 未设置，跳过 TG bridge")
        return False

    with _bridge_lock:
        if _bridge_thread and _bridge_thread.is_alive():
            return True
        _bridge_stop_event.clear()
        _bridge_thread = threading.Thread(
            target=_bridge_thread_target,
            name="tg-bridge",
            daemon=True,
        )
        _bridge_thread.start()
        logger.info("TG bridge 线程已启动")
        return True


def stop_tg_bridge(timeout: float = 3.0) -> None:
    global _bridge_thread
    with _bridge_lock:
        _bridge_stop_event.set()
        thread = _bridge_thread
        _bridge_thread = None
    if thread and thread.is_alive():
        thread.join(timeout=max(0.5, timeout))
        logger.info("TG bridge 已停止")


def is_tg_bridge_running() -> bool:
    with _bridge_lock:
        return bool(_bridge_thread and _bridge_thread.is_alive())


def send_message_to_tg(text: str) -> bool:
    token = _get_token()
    chat_id = _get_chat_id()
    if not token or not chat_id:
        return False
    try:
        import requests
        resp = requests.post(
            f"https://api.telegram.org/bot{token}/sendMessage",
            json={"chat_id": chat_id, "text": _truncate(text)},
            timeout=10,
        )
        if resp.ok:
            _add_history("bot", text, chat_id=chat_id)
        return resp.ok
    except Exception as exc:
        logger.debug("send_message_to_tg 失败: %s", exc)
        return False


# ---- 定时看门狗 ----
_DEFAULT_WATCHDOG_INTERVAL = 120  # 秒
_DEFAULT_NUDGE_PROMPT = "请继续执行当前任务。如果已完成，请汇报结果。"

_watchdog_thread: Optional[threading.Thread] = None
_watchdog_stop = threading.Event()
_watchdog_info: dict[str, Any] = {"running": False, "interval": _DEFAULT_WATCHDOG_INTERVAL,
                                   "last_nudge": "", "nudge_count": 0}
_watchdog_lock = threading.Lock()


def _get_watchdog_interval() -> int:
    try:
        return max(30, int(os.getenv("TG_WATCHDOG_INTERVAL", str(_DEFAULT_WATCHDOG_INTERVAL))))
    except (ValueError, TypeError):
        return _DEFAULT_WATCHDOG_INTERVAL


def _get_nudge_prompt() -> str:
    return os.getenv("TG_WATCHDOG_PROMPT", _DEFAULT_NUDGE_PROMPT).strip()


def _watchdog_loop() -> None:
    """定时巡检：到时间就向主 Agent 和子 Agent 发唤醒提示。"""
    interval = _get_watchdog_interval()
    prompt = _get_nudge_prompt()
    logger.info("看门狗启动: 间隔=%ds, 提示=%s", interval, prompt[:50])
    _add_history("system", f"⏰ 看门狗启动 — 每 {interval}s 唤醒一次")

    with _watchdog_lock:
        _watchdog_info.update(running=True, interval=interval, nudge_count=0)

    while not _watchdog_stop.wait(timeout=interval):
        try:
            _do_nudge(prompt)
        except Exception as exc:
            logger.error("看门狗异常: %s", exc, exc_info=True)

    with _watchdog_lock:
        _watchdog_info["running"] = False
    logger.info("看门狗已停止")
    _add_history("system", "⏰ 看门狗已停止")


def _do_nudge(prompt: str) -> None:
    """执行一次唤醒：发送提示到所有 Agent 会话。"""
    nudged: list[str] = []

    # 唤醒主 Agent
    master = _find_master_session()
    if master:
        sid = master.get("session_id", "")
        name = master.get("session_name") or master.get("name") or sid
        try:
            _send_to_iterm_session(sid, prompt)
            nudged.append(f"主Agent({name})")
            logger.info("看门狗唤醒主 Agent: %s", name)
        except Exception as exc:
            logger.warning("看门狗唤醒主 Agent 失败: %s", exc)

    # 唤醒子 Agent（已注册的 agent 会话）
    try:
        from agents.iterm_bridge import (
            _list_live_sessions, _load_state, _normalize_state_file,
            AgentSession, _run_iterm_io,
        )
        state_path = _normalize_state_file()
        state = _load_state(state_path)
        for row in state.get("agents", []):
            sid = str(row.get("session_id", "")).strip()
            agent_name = row.get("agent_name") or row.get("agent_id") or sid
            if not sid:
                continue
            try:
                target = AgentSession(index=0, agent_id=row.get("agent_id", ""),
                                     agent_name=agent_name, session_id=sid)
                _run_iterm_io(targets=[target], text=prompt,
                             append_enter=True, wait_sec=0.5, read_lines=0)
                nudged.append(agent_name)
                logger.info("看门狗唤醒子 Agent: %s", agent_name)
            except Exception as exc:
                logger.warning("看门狗唤醒 %s 失败: %s", agent_name, exc)
    except Exception:
        pass  # 没有子 Agent 也没关系

    now_iso = datetime.now(timezone.utc).isoformat()
    with _watchdog_lock:
        _watchdog_info["last_nudge"] = now_iso
        _watchdog_info["nudge_count"] = _watchdog_info.get("nudge_count", 0) + 1

    if nudged:
        msg = f"⏰ 看门狗唤醒: {', '.join(nudged)}"
        _add_history("system", msg)
        send_message_to_tg(msg)
    else:
        _add_history("system", "⏰ 看门狗巡检: 未发现活跃会话")


def start_watchdog() -> bool:
    global _watchdog_thread
    with _watchdog_lock:
        if _watchdog_thread and _watchdog_thread.is_alive():
            return True
        _watchdog_stop.clear()
        _watchdog_thread = threading.Thread(
            target=_watchdog_loop, name="tg-watchdog", daemon=True,
        )
        _watchdog_thread.start()
        return True


def stop_watchdog(timeout: float = 3.0) -> None:
    global _watchdog_thread
    with _watchdog_lock:
        _watchdog_stop.set()
        thread = _watchdog_thread
        _watchdog_thread = None
    if thread and thread.is_alive():
        thread.join(timeout=max(0.5, timeout))


def is_watchdog_running() -> bool:
    with _watchdog_lock:
        return bool(_watchdog_thread and _watchdog_thread.is_alive())


def get_watchdog_info() -> dict[str, Any]:
    with _watchdog_lock:
        return dict(_watchdog_info)
