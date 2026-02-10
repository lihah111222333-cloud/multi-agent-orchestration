"""配置管理 Web 面板 (v2 — Dark OLED Design)

启动: python3 dashboard.py
访问: http://localhost:8080
"""

import json
import logging
import os
import html
import inspect
import http.server
import queue
import re
import threading
import time
import urllib.parse

from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional

from dotenv import load_dotenv, set_key

from agent_ops_store import (
    get_prompt_template,
    save_prompt_template,
    list_prompt_templates,
    list_prompt_template_versions,
    list_task_trace_spans,
    list_task_traces,
    rollback_prompt_template,
    set_prompt_template_enabled,
)
from config.prompt_template_presets import list_common_prompt_templates
from agent_monitor import patrol_agents_once
from agents.iterm_bridge import (list_iterm_agent_sessions, read_iterm_output,
                                  read_session_screen, send_to_session,
                                  start_session_streamer, stop_session_streamer,
                                  list_active_streamers,
                                  _list_live_sessions)
from tg_bridge import (
    start_tg_bridge, stop_tg_bridge, is_tg_bridge_running,
    get_tg_history, clear_tg_history, send_message_to_tg, get_tg_bridge_info,
    start_watchdog, stop_watchdog, is_watchdog_running, get_watchdog_info,
)
from audit_log import append_event, query_events, list_filter_values as list_audit_filter_values
from db.postgres import fetch_one
from system_log import query_logs as query_system_logs, list_filter_values as list_system_filter_values
from topology_approval import approve_approval, is_valid_approval_id, list_approvals, reject_approval

ENV_FILE = Path(__file__).parent / ".env"
STATIC_DIR = Path(__file__).parent / "static"
logger = logging.getLogger(__name__)

_AGENT_STATUS_NAMES = ("running", "idle", "stuck", "error", "disconnected", "unknown")
_AGENT_STATUS_MEMORY: dict[str, dict[str, Any]] = {}

# 配置项定义
CONFIG_SCHEMA = [
    {
        "group": "LLM 设置",
        "icon": "brain",
        "items": [
            {"key": "OPENAI_API_KEY", "label": "API Key", "type": "password", "desc": "OpenAI / 第三方 API Key"},
            {"key": "OPENAI_BASE_URL", "label": "API Base URL", "type": "text", "desc": "留空使用 OpenAI 官方"},
            {"key": "LLM_MODEL", "label": "模型", "type": "text", "desc": "如 gpt-4o, deepseek-chat"},
            {"key": "LLM_TEMPERATURE", "label": "Temperature", "type": "number", "desc": "0-2，越低越确定"},
            {"key": "LLM_TIMEOUT", "label": "LLM 超时(秒)", "type": "number", "desc": "单次 LLM 请求超时"},
            {"key": "LLM_MAX_RETRIES", "label": "LLM 重试次数", "type": "number", "desc": "失败后重试次数"},
            {"key": "GATEWAY_TIMEOUT", "label": "Gateway 超时(秒)", "type": "number", "desc": "单个 Gateway 执行超时"},
            {"key": "GATEWAY_MAX_ATTEMPTS", "label": "Gateway 最大尝试", "type": "number", "desc": "含首次执行的总次数"},
            {"key": "GATEWAY_MIN_QUALITY_SCORE", "label": "结果质量阈值", "type": "number", "desc": "Aggregator 采用结果的最低质量分"},
        ],
    },
    {
        "group": "系统设置",
        "icon": "cog",
        "items": [
            {"key": "LOG_LEVEL", "label": "日志级别", "type": "select",
             "options": ["DEBUG", "INFO", "WARNING", "ERROR"], "desc": "日志输出级别"},
            {"key": "TOPOLOGY_PROPOSAL_ENABLED", "label": "自动拓扑提案", "type": "select",
             "options": ["1", "0"], "desc": "是否启用 Master 自动提出拓扑变更"},
            {"key": "TOPOLOGY_APPROVAL_TTL_SEC", "label": "审批过期(秒)", "type": "number", "desc": "审批单超时自动过期"},
            {"key": "DASHBOARD_SSE_SYNC_SEC", "label": "实时同步间隔(秒)", "type": "number", "desc": "SSE 心跳同步周期"},
            {"key": "TG_AUTO_REFRESH_SEC", "label": "TG自动刷新(秒)", "type": "number", "desc": "Telegram 页面自动刷新间隔，0=关闭"},
            {"key": "AUDIT_LOG_LIMIT", "label": "审计日志条数", "type": "number", "desc": "面板默认加载条数"},
            {"key": "SYSTEM_LOG_LIMIT", "label": "系统日志条数", "type": "number", "desc": "面板默认加载条数"},
            {"key": "AUDIT_LOG_MAX_BYTES", "label": "审计日志最大字节", "type": "number", "desc": "超过后自动轮转"},
            {"key": "AUDIT_LOG_BACKUP_COUNT", "label": "审计日志轮转份数", "type": "number", "desc": "保留的备份文件数量"},
            {"key": "SYSTEM_LOG_MAX_BYTES", "label": "系统日志最大字节", "type": "number", "desc": "system.log 超过后轮转"},
            {"key": "SYSTEM_LOG_BACKUP_COUNT", "label": "系统日志轮转份数", "type": "number", "desc": "system.log 备份文件数量"},
            {"key": "CONFIG_BACKUP_ENABLED", "label": "配置备份开关", "type": "select", "options": ["1", "0"], "desc": "审批通过写入前是否备份"},
            {"key": "CONFIG_BACKUP_KEEP", "label": "配置备份保留份数", "type": "number", "desc": "超过后自动清理最旧备份"},
        ],
    },
    {
        "group": "Agent 监控",
        "icon": "eye",
        "items": [
            {"key": "AGENT_MONITOR_INTERVAL_SEC", "label": "巡检间隔(秒)", "type": "number", "desc": "后台监控线程轮询周期，1-300"},
            {"key": "AGENT_MONITOR_READ_LINES", "label": "读取行数", "type": "number", "desc": "每次采集 iTerm 输出的行数，1-200"},
        ],
    },
    {
        "group": "Telegram Bot",
        "icon": "send",
        "items": [
            {"key": "TG_BOT_TOKEN", "label": "Bot Token", "type": "password", "desc": "@BotFather 获取的 Bot Token"},
            {"key": "TG_CHAT_ID", "label": "Chat ID", "type": "text", "desc": "允许通信的 Telegram Chat ID（留空则 /start 自动绑定）"},
            {"key": "TG_MASTER_TAB_NAME", "label": "主 Agent Tab 名", "type": "text", "desc": "iTerm 中主 Agent 终端 tab 名称"},
            {"key": "TG_WATCHDOG_INTERVAL", "label": "看门狗间隔(秒)", "type": "text", "desc": "定时唤醒 Agent 的间隔秒数（默认120）"},
            {"key": "TG_WATCHDOG_PROMPT", "label": "唤醒提示词", "type": "text", "desc": "发送给 Agent 的唤醒提示词"},
        ],
    },
]

DEFAULTS = {
    "OPENAI_API_KEY": "",
    "OPENAI_BASE_URL": "",
    "LLM_MODEL": "gpt-4o",
    "LLM_TEMPERATURE": "0.7",
    "LLM_TIMEOUT": "120",
    "LLM_MAX_RETRIES": "3",
    "GATEWAY_TIMEOUT": "240",
    "GATEWAY_MAX_ATTEMPTS": "2",
    "GATEWAY_MIN_QUALITY_SCORE": "25",
    "LOG_LEVEL": "INFO",
    "TOPOLOGY_PROPOSAL_ENABLED": "1",
    "TOPOLOGY_APPROVAL_TTL_SEC": "120",
    "DASHBOARD_SSE_SYNC_SEC": "5",
    "TG_AUTO_REFRESH_SEC": "60",
    "AUDIT_LOG_LIMIT": "100",
    "SYSTEM_LOG_LIMIT": "100",
    "AUDIT_LOG_MAX_BYTES": str(5 * 1024 * 1024),
    "AUDIT_LOG_BACKUP_COUNT": "3",
    "SYSTEM_LOG_MAX_BYTES": str(10 * 1024 * 1024),
    "SYSTEM_LOG_BACKUP_COUNT": "3",
    "CONFIG_BACKUP_ENABLED": "1",
    "CONFIG_BACKUP_KEEP": "5",
    "AGENT_MONITOR_INTERVAL_SEC": "5",
    "AGENT_MONITOR_READ_LINES": "30",
    "TG_BOT_TOKEN": "8411951426:AAGzdMxTUHXhvcj9_3a3iHP2CB3Mvn8oKm8",
    "TG_CHAT_ID": "",
    "TG_MASTER_TAB_NAME": "主agnet",
    "TG_WATCHDOG_INTERVAL": "120",
    "TG_WATCHDOG_PROMPT": "请继续执行当前任务。如果已完成，请汇报结果。",
}

CONFIG_TYPE_MAP = {item["key"]: item["type"] for group in CONFIG_SCHEMA for item in group.get("items", [])}
CONFIG_SELECT_OPTIONS = {
    item["key"]: item.get("options", [])
    for group in CONFIG_SCHEMA
    for item in group.get("items", [])
    if item.get("type") == "select"
}


class DashboardEventBus:
    """Dashboard 实时事件总线（单进程内）。"""

    def __init__(self, queue_size: int = 128):
        self._lock = threading.Lock()
        self._subscribers: set[queue.Queue] = set()
        self._next_id = 1
        self._queue_size = max(1, int(queue_size))

    def subscribe(self) -> queue.Queue:
        channel: queue.Queue = queue.Queue(maxsize=self._queue_size)
        with self._lock:
            self._subscribers.add(channel)
        return channel

    def unsubscribe(self, channel: queue.Queue) -> None:
        with self._lock:
            self._subscribers.discard(channel)

    def _new_event_id(self) -> int:
        # Called under self._lock only
        event_id = self._next_id
        self._next_id += 1
        return event_id

    def publish(self, event_type: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
        dead_channels: list[queue.Queue] = []

        with self._lock:
            event = {
                "id": self._new_event_id(),
                "event": str(event_type or "sync"),
                "ts": datetime.now(timezone.utc).isoformat(),
                "payload": payload or {},
            }
            subscribers = list(self._subscribers)

        for channel in subscribers:
            try:
                channel.put_nowait(event)
            except queue.Full:
                try:
                    channel.get_nowait()
                except queue.Empty:
                    pass
                try:
                    channel.put_nowait(event)
                except queue.Full:
                    dead_channels.append(channel)
                    continue

        # D3/D11: auto-cleanup channels that are permanently full (dead SSE clients)
        if dead_channels:
            with self._lock:
                for ch in dead_channels:
                    self._subscribers.discard(ch)

        return event


EVENT_BUS = DashboardEventBus()


def _publish_dashboard_event(event_type: str, payload: dict[str, Any] | None = None) -> None:
    try:
        EVENT_BUS.publish(event_type, payload)
    except Exception as exc:
        logger.debug("publish dashboard event failed: %s", exc)


def _encode_sse_event(event: dict[str, Any]) -> bytes:
    event_name = str(event.get("event", "sync")).replace("\n", " ").strip() or "sync"
    event_id = str(event.get("id", ""))
    data = json.dumps(event, ensure_ascii=False)

    lines = []
    if event_id:
        lines.append(f"id: {event_id}\n")
    lines.append(f"event: {event_name}\n")
    lines.append(f"data: {data}\n\n")
    return "".join(lines).encode("utf-8")


# SVG Icons
ICONS = {
    "brain": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 2a7 7 0 017 7c0 2.38-1.19 4.47-3 5.74V17a2 2 0 01-2 2h-4a2 2 0 01-2-2v-2.26C6.19 13.47 5 11.38 5 9a7 7 0 017-7z"/><path d="M10 21h4"/></svg>',
    "cog": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/></svg>',
    "agent": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="4" y="4" width="16" height="16" rx="2"/><circle cx="9" cy="10" r="1.5"/><circle cx="15" cy="10" r="1.5"/><path d="M9 16c1.5 1 4.5 1 6 0"/></svg>',
    "eye": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>',
}

# Navigation items
NAV_ITEMS = [
    ("config", "配置管理", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/></svg>'),
    ("arch", "架构拓扑", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="2" y="3" width="6" height="5" rx="1"/><rect x="16" y="3" width="6" height="5" rx="1"/><rect x="9" y="16" width="6" height="5" rx="1"/><path d="M5 8v3a2 2 0 002 2h10a2 2 0 002-2V8"/><path d="M12 13v3"/></svg>'),
    ("approvals", "拓扑审批", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2h11"/></svg>'),
    ("audit", "审计日志", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><path d="M14 2v6h6"/><path d="M16 13H8"/><path d="M16 17H8"/><path d="M10 9H8"/></svg>'),
    ("syslog", "系统日志", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><polyline points="4,17 10,11 4,5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>'),
    ("commands", "命令卡", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M4 6h16M4 12h16M4 18h10"/><circle cx="18" cy="18" r="2"/></svg>'),
    ("prompts", "提示词", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/></svg>'),
    ("traces", "任务追踪", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M3 12h5l2-6 4 12 2-6h5"/></svg>'),
    ("monitor", "Agent 监控", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>'),
    ("telegram", "Telegram", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4 20-7z"/></svg>'),
]


# ─── helpers ───

def load_current_config() -> dict:
    load_dotenv(ENV_FILE, override=True)
    config = {}
    for key, default in DEFAULTS.items():
        config[key] = os.getenv(key, default)
    return config


def _sanitize_config_updates(updates: dict) -> dict[str, str]:
    if not isinstance(updates, dict):
        raise ValueError("配置更新必须是 JSON 对象")
    unknown_keys = [str(key) for key in updates.keys() if str(key) not in DEFAULTS]
    if unknown_keys:
        raise ValueError(f"包含不允许的配置项: {', '.join(sorted(unknown_keys))}")
    sanitized: dict[str, str] = {}
    for raw_key, raw_value in updates.items():
        key = str(raw_key)
        value = "" if raw_value is None else str(raw_value).strip()
        value_type = CONFIG_TYPE_MAP.get(key, "text")
        if value_type == "number" and value:
            try:
                float(value)
            except ValueError as e:
                raise ValueError(f"配置项 {key} 需要数字") from e
        if value_type == "select":
            options = CONFIG_SELECT_OPTIONS.get(key, [])
            if options and value not in options:
                raise ValueError(f"配置项 {key} 非法选项: {value}")
        sanitized[key] = value
    return sanitized


def save_config(updates: dict) -> list[str]:
    clean_updates = _sanitize_config_updates(updates)
    if not ENV_FILE.exists():
        ENV_FILE.touch()
    for key, value in clean_updates.items():
        set_key(str(ENV_FILE), key, value)
    return sorted(clean_updates.keys())


def _load_gateway_map() -> dict:
    from config.settings import load_architecture
    return load_architecture()


def _safe_int(value: str, default: int, min_value: int, max_value: int) -> int:
    try:
        iv = int(float(value))
    except (TypeError, ValueError):
        iv = default
    return max(min_value, min(iv, max_value))


def _parse_required_int(value: Any, field_name: str, min_value: int, max_value: int) -> int:
    if isinstance(value, bool):
        raise ValueError(f"{field_name} 必须是整数")

    text = str(value).strip()
    if not text:
        raise ValueError(f"{field_name} 不能为空")
    if not re.fullmatch(r"-?\d+", text):
        raise ValueError(f"{field_name} 必须是整数")

    parsed = int(text)
    if parsed < min_value or parsed > max_value:
        raise ValueError(f"{field_name} 超出范围: {min_value}~{max_value}")
    return parsed


def _safe_identifier(value: str) -> str:
    return "".join(ch for ch in str(value) if ch.isalnum() or ch in ("_", "-"))


def _safe_bool(value: Any, default: bool = False) -> bool:
    if isinstance(value, bool):
        return value
    if value is None:
        return default
    text = str(value).strip().lower()
    if text in {"1", "true", "yes", "y", "on"}:
        return True
    if text in {"0", "false", "no", "n", "off"}:
        return False
    return default


def _empty_agent_status_summary() -> dict[str, int]:
    return {
        "total": 0,
        "healthy": 0,
        "unhealthy": 0,
        **{name: 0 for name in _AGENT_STATUS_NAMES},
    }


def _normalize_agent_status_rows(rows: list[dict[str, Any]]) -> tuple[list[dict[str, Any]], str]:
    ts = datetime.now(timezone.utc).isoformat()
    by_agent: dict[str, tuple[float, dict[str, Any]]] = {}
    latest_epoch = 0.0
    latest_ts = ts

    for idx, row in enumerate(rows):
        if not isinstance(row, dict):
            continue

        agent_id = str(row.get("agent_id", "")).strip()
        if not agent_id:
            continue

        raw_updated = row.get("updated_at") or row.get("ts") or row.get("created_at")
        updated_epoch = _to_epoch_seconds(raw_updated)
        if updated_epoch <= 0:
            updated_epoch = float(idx + 1)

        if raw_updated is not None:
            updated_text = str(raw_updated).strip()
            if updated_text and _to_epoch_seconds(raw_updated) >= latest_epoch:
                latest_epoch = _to_epoch_seconds(raw_updated)
                latest_ts = updated_text

        status = str(row.get("status", "unknown")).strip().lower()
        if status not in _AGENT_STATUS_NAMES:
            status = "unknown"

        output_source = row.get("output_tail", row.get("output"))
        normalized_row = {
            "agent_id": agent_id,
            "agent_name": str(row.get("agent_name", "")).strip(),
            "session_id": str(row.get("session_id", "")).strip(),
            "status": status,
            "stagnant_sec": _safe_int(row.get("stagnant_sec", 0), 0, 0, 2147483647),
            "error": str(row.get("error", "") or "").strip(),
            "output_tail": _normalize_output_tail(output_source),
        }

        previous = by_agent.get(agent_id)
        if not previous or updated_epoch >= previous[0]:
            by_agent[agent_id] = (updated_epoch, normalized_row)

    normalized_rows = [item[1] for item in sorted(by_agent.values(), key=lambda item: item[0], reverse=True)]
    return normalized_rows, latest_ts


def _summarize_agent_status(agents: list[dict[str, Any]]) -> dict[str, int]:
    summary = _empty_agent_status_summary()
    for agent in agents:
        status = str(agent.get("status", "unknown")).strip().lower()
        if status not in _AGENT_STATUS_NAMES:
            status = "unknown"
        summary[status] += 1
        summary["total"] += 1

    summary["healthy"] = summary["running"] + summary["idle"]
    summary["unhealthy"] = summary["total"] - summary["healthy"]
    return summary


def _is_agent_status_table_missing_error(exc: Exception) -> bool:
    msg = str(exc)
    normalized = msg.lower()
    if "agent_status" not in normalized:
        return False
    return (
        "does not exist" in normalized
        or "undefinedtable" in normalized
        or "no such table" in normalized
        or "不存在" in msg
    )


def _to_epoch_seconds(raw: Any) -> float:
    if raw is None:
        return 0.0
    if isinstance(raw, datetime):
        dt = raw
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt.timestamp()

    text = str(raw).strip()
    if not text:
        return 0.0

    iso_text = text.replace("Z", "+00:00")
    try:
        dt = datetime.fromisoformat(iso_text)
    except ValueError:
        return 0.0
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.timestamp()


def _normalize_output_tail(raw: Any) -> list[str]:
    if raw is None:
        return []

    values: list[Any]
    if isinstance(raw, list):
        values = raw
    elif isinstance(raw, tuple):
        values = list(raw)
    elif isinstance(raw, str):
        text = raw.strip()
        if not text:
            return []
        if text.startswith("["):
            try:
                parsed = json.loads(text)
            except json.JSONDecodeError:
                parsed = text
            if isinstance(parsed, list):
                values = parsed
            else:
                values = [text]
        else:
            values = text.splitlines()
    else:
        values = [raw]

    normalized = [str(item).strip() for item in values if str(item).strip()]
    return normalized[-20:]


def _build_agent_status_snapshot(read_lines: int = 30) -> dict[str, Any]:
    """Build agent status snapshot directly via iTerm API."""
    try:
        snapshot = patrol_agents_once(
            list_sessions_func=list_iterm_agent_sessions,
            read_output_func=read_iterm_output,
            read_lines=max(1, read_lines),
            status_memory=_AGENT_STATUS_MEMORY,
        )
    except Exception:
        logger.debug("iTerm agent status snapshot failed", exc_info=True)
        return {
            "ok": False,
            "ts": datetime.now(timezone.utc).isoformat(),
            "error": "iterm_snapshot_failed",
            "summary": _empty_agent_status_summary(),
            "agents": [],
            "source": {"sessions_ok": False, "output_ok": False},
        }
    return snapshot


def _check_dashboard_ready() -> tuple[bool, str]:
    try:
        row = fetch_one("SELECT 1 AS ok")
    except Exception as exc:
        return False, str(exc)

    if not isinstance(row, dict):
        return False, "db_no_response"
    if int(row.get("ok", 0) or 0) != 1:
        return False, "db_unexpected_response"
    return True, ""


def _parse_json_object(raw: Any, field_name: str) -> dict[str, Any]:
    if raw is None:
        return {}
    if isinstance(raw, dict):
        return raw
    text = str(raw).strip()
    if not text:
        return {}
    try:
        loaded = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ValueError(f"{field_name} 不是合法 JSON 对象") from exc
    if not isinstance(loaded, dict):
        raise ValueError(f"{field_name} 必须是 JSON 对象")
    return loaded


def _parse_json_array(raw: Any, field_name: str) -> list[Any]:
    if raw is None:
        return []
    if isinstance(raw, list):
        return raw
    text = str(raw).strip()
    if not text:
        return []
    try:
        loaded = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ValueError(f"{field_name} 不是合法 JSON 数组") from exc
    if not isinstance(loaded, list):
        raise ValueError(f"{field_name} 必须是 JSON 数组")
    return loaded


def _generate_default_prompt(agent_desc: str, tool_name: str, tool_desc: str, params_str: str) -> str:
    """Generate default prompt text for a tool."""
    return (f'你有一个 MCP 工具叫 "{tool_name}"。\n\n'
            f'功能: {tool_desc}\n'
            f'参数: {params_str or "无"}\n\n'
            f'使用场景: 当用户需要{tool_desc}时，调用此工具。')


# 系统级工具分组定义 — 用于自动生成提示词
_SYSTEM_TOOL_GROUPS: list[tuple[str, list[str]]] = [
    ('iTerm 会话管理', ['iterm']),
    ('共享文件', ['shared_file']),
    ('Agent 交互', ['interaction']),
    ('提示词模板', ['prompt_template']),
    ('命令卡', ['command_card']),
    ('数据库', ['db']),
    ('任务管理', ['task']),
    ('审批/错误处理', ['approval']),
    ('看门狗', ['agent_watchdog']),
]


def _format_sig_params(fn: Any) -> str:
    """Extract human-readable parameter string from a function via inspect."""
    try:
        sig = inspect.signature(fn)
    except (ValueError, TypeError):
        return ''
    parts: list[str] = []
    for name, param in sig.parameters.items():
        ann = param.annotation
        if ann is inspect.Parameter.empty:
            type_str = ''
        elif hasattr(ann, '__name__'):
            type_str = f': {ann.__name__}'
        else:
            type_str = f': {ann}'
        if param.default is not inspect.Parameter.empty:
            default_val = param.default
            if isinstance(default_val, str):
                parts.append(f'{name}{type_str} = "{default_val}"')
            elif default_val is None:
                parts.append(f'{name}{type_str} = None')
            else:
                parts.append(f'{name}{type_str} = {default_val}')
        else:
            parts.append(f'{name}{type_str}')
    return ', '.join(parts)


def _build_system_prompt() -> dict:
    """Build the pinned system prompt describing all MCP interfaces."""
    from pathlib import Path
    doc_path = Path(__file__).resolve().parent / "docs" / "MCP_TOOLS.md"
    try:
        prompt_text = doc_path.read_text("utf-8").strip()
    except Exception:
        prompt_text = "# MCP 工具总览\n\n请查看 docs/MCP_TOOLS.md"

    return {
        'key': '_system',
        'description': '多Agent编排系统 — MCP 系统级工具提示词（置顶）',
        'is_pinned': True,
        'tools': [{
            'name': 'system_prompt',
            'description': '系统级提示词：描述所有可用 MCP 工具及使用场景',
            'params': [],
        }],
        'prompt_text': prompt_text,
    }


def _get_all_agent_specs() -> list[dict]:
    """Return system prompt as the only spec entry (agents are dynamic)."""
    # Pin system prompt at top (supports prompt_templates persistence)
    sys_prompt = _build_system_prompt()
    try:
        row = get_prompt_template("_system.system_prompt")
        if row and str(row.get("prompt_text", "")).strip():
            sys_prompt["prompt_text"] = str(row.get("prompt_text", ""))
    except Exception:
        logger.debug("读取 prompt_templates 失败，回退默认提示词", exc_info=True)

    return [sys_prompt]


def _save_prompt(agent_key: str, tool_name: str, prompt_text: str) -> dict:
    """Upsert prompt template into prompt_templates table."""
    key = f"{str(agent_key or '').strip()}.{str(tool_name or '').strip()}"
    saved = save_prompt_template(
        prompt_key=key,
        title=f"{agent_key}/{tool_name}",
        prompt_text=str(prompt_text or "").strip(),
        agent_key=str(agent_key or "").strip(),
        tool_name=str(tool_name or "").strip(),
        variables={},
        tags=["dashboard", "prompt"],
        enabled=True,
        updated_by="dashboard",
    )
    return {
        "ok": True,
        "agent_key": str(agent_key or "").strip(),
        "tool_name": str(tool_name or "").strip(),
        "prompt": saved,
    }


def _save_prompt_template_entry(data: dict[str, Any], updated_by: str = "dashboard") -> dict[str, Any]:
    prompt_key = str(data.get("prompt_key", "") or "").strip()
    title = str(data.get("title", "") or "").strip()
    prompt_text = str(data.get("prompt_text", "") or "").strip()
    agent_key = str(data.get("agent_key", "") or "").strip()
    tool_name = str(data.get("tool_name", "") or "").strip()
    enabled = _safe_bool(data.get("enabled", True), default=True)

    if not prompt_key:
        raise ValueError("prompt_key 不能为空")
    if not title:
        raise ValueError("title 不能为空")
    if not prompt_text:
        raise ValueError("prompt_text 不能为空")

    variables = _parse_json_object(data.get("variables", {}), "variables")
    tags = _parse_json_array(data.get("tags", []), "tags")

    return save_prompt_template(
        prompt_key=prompt_key,
        title=title,
        prompt_text=prompt_text,
        agent_key=agent_key,
        tool_name=tool_name,
        variables=variables,
        tags=tags,
        enabled=enabled,
        updated_by=str(updated_by or "dashboard").strip() or "dashboard",
    )


def _seed_common_prompt_templates(overwrite: bool = False, updated_by: str = "dashboard") -> dict[str, Any]:
    templates = list_common_prompt_templates()
    inserted = 0
    updated = 0
    skipped = 0
    saved_items: list[dict[str, Any]] = []

    for item in templates:
        prompt_key = str(item.get("prompt_key", "") or "").strip()
        if not prompt_key:
            continue

        existing = get_prompt_template(prompt_key)
        if existing and not overwrite:
            skipped += 1
            continue

        saved = _save_prompt_template_entry(item, updated_by=updated_by)
        saved_items.append(saved)
        if existing:
            updated += 1
        else:
            inserted += 1

    return {
        "ok": True,
        "total": len(templates),
        "inserted": inserted,
        "updated": updated,
        "skipped": skipped,
        "templates": saved_items,
    }


def _summarize_traces(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    grouped: dict[str, dict[str, Any]] = {}
    for row in rows:
        trace_id = str(row.get("trace_id", "")).strip()
        if not trace_id:
            continue

        current = grouped.get(trace_id)
        if current is None:
            current = {
                "trace_id": trace_id,
                "status": str(row.get("status", "running")),
                "span_count": 0,
                "started_at": str(row.get("started_at", "")),
                "finished_at": str(row.get("finished_at", "")),
                "components": set(),
            }
            grouped[trace_id] = current

        current["span_count"] = int(current.get("span_count", 0)) + 1
        status = str(row.get("status", "running"))
        if status == "error":
            current["status"] = "error"
        elif current.get("status") != "error" and status == "running":
            current["status"] = "running"
        elif current.get("status") not in {"error", "running"}:
            current["status"] = status

        started_at = str(row.get("started_at", ""))
        finished_at = str(row.get("finished_at", ""))
        if not str(current.get("started_at", "")) or started_at < str(current.get("started_at", "")):
            current["started_at"] = started_at
        if finished_at and finished_at > str(current.get("finished_at", "")):
            current["finished_at"] = finished_at

        component = str(row.get("component", "")).strip()
        if component:
            current["components"].add(component)

    traces = []
    for item in grouped.values():
        traces.append(
            {
                "trace_id": str(item.get("trace_id", "")),
                "status": str(item.get("status", "running")),
                "span_count": int(item.get("span_count", 0)),
                "started_at": str(item.get("started_at", "")),
                "finished_at": str(item.get("finished_at", "")),
                "components": sorted(str(v) for v in item.get("components", set())),
            }
        )

    traces.sort(key=lambda row: str(row.get("started_at", "")), reverse=True)
    return traces


# ─── HTML render ───

def render_html() -> str:
    config = load_current_config()

    # Config form
    groups_html = ""
    for group in CONFIG_SCHEMA:
        icon_svg = ICONS.get(group["icon"], "")
        items_html = ""
        for item in group["items"]:
            val = config.get(item["key"], "")
            safe_val = html.escape(str(val), quote=True)
            if item["type"] == "password":
                input_html = f'''<div class="password-wrap">
                    <input type="password" name="{item['key']}" value="{safe_val}" class="input" id="pw-{item['key']}" placeholder="未设置" autocomplete="off">
                    <button type="button" class="pw-toggle" onclick="togglePw('{item['key']}')" aria-label="显示/隐藏密码">{ICONS['eye']}</button>
                </div>'''
            elif item["type"] == "select":
                opts = "".join(
                    f'<option value="{html.escape(str(o), quote=True)}" {"selected" if str(o) == str(val) else ""}>{html.escape(str(o))}</option>'
                    for o in item.get("options", [])
                )
                input_html = f'<select name="{item["key"]}" class="input">{opts}</select>'
            elif item["type"] == "number":
                input_html = f'<input type="number" name="{item["key"]}" value="{safe_val}" class="input" step="0.1">'
            else:
                input_html = f'<input type="text" name="{item["key"]}" value="{safe_val}" class="input" placeholder="未设置">'

            items_html += f'''<div class="config-row">
                <div class="config-meta"><label class="config-label">{item['label']}</label><div class="config-desc">{item['desc']}</div></div>
                <div class="config-control">{input_html}</div>
            </div>'''

        groups_html += f'''<section class="card">
            <header class="card-header">{icon_svg}<h2>{group['group']}</h2></header>
            <div class="card-body">{items_html}</div>
        </section>'''

    # Gateway cards
    gateway_agent_map = _load_gateway_map()
    colors = ["#22C55E", "#3B82F6", "#8B5CF6", "#06B6D4", "#F59E0B"]
    gw_cards = ""
    for i, (gw_name, gw_config) in enumerate(gateway_agent_map.items()):
        color = colors[i % len(colors)]
        agents_list = "".join(
            (
                f'<div class="agent-chip agent-chip-status-unknown" data-agent-id="{html.escape(str(a), quote=True)}">'
                f'{ICONS["agent"]}'
                f'<span class="agent-chip-name">{html.escape(str(a))}</span>'
                '<span class="agent-chip-state">unknown</span>'
                '</div>'
            )
            for a in gw_config["agents"].keys()
        )
        gw_cards += f'''<div class="gw-card" style="--accent-color:{color}; border-left-color:{color}">
            <h3 class="gw-name">{html.escape(str(gw_config['name']))}</h3>
            <span class="gw-id">{html.escape(str(gw_name))}</span>
            <div class="gw-agents">{agents_list}</div>
            <div class="gw-count">{len(gw_config['agents'])} agents</div>
        </div>'''

    gateway_count = len(gateway_agent_map)
    agent_count = sum(len(gw.get("agents", {})) for gw in gateway_agent_map.values())
    sse_sync_sec = _safe_int(config.get("DASHBOARD_SSE_SYNC_SEC", "5"), 5, 1, 60)

    # Nav HTML
    nav_html = ""
    for page_id, label, icon in NAV_ITEMS:
        active = " active" if page_id == "config" else ""
        nav_html += f'<button class="nav-btn{active}" id="nav-{page_id}" data-page="{page_id}">{icon}<span>{label}</span></button>'

    return f'''<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>多Agent编排 — 控制台</title>
<link rel="stylesheet" href="/static/style.css">
</head>
<body>
<div id="toast-container" class="toast-container"></div>

<div class="shell">
    <aside class="sidebar">
        <div class="sidebar-brand">
            <h1>ACP-BUS</h1>
            <div class="subtitle">多Agent编排控制台</div>
        </div>
        <nav class="sidebar-nav">{nav_html}</nav>
        <div class="sidebar-footer">
            <div id="live-status" class="live-status live-status-pending">实时通道连接中...</div>
            <div>v2.0 · Dark OLED</div>
        </div>
    </aside>

    <main class="main">


        <!-- Config Page -->
        <div id="page-config" class="page active">
            <form id="config-form" onsubmit="event.preventDefault(); saveConfig();">
                {groups_html}
                <div class="save-bar">
                    <button type="submit" class="btn btn-primary">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><polyline points="17,21 17,13 7,13 7,21"/><polyline points="7,3 7,8 15,8"/></svg>
                        保存配置
                    </button>
                </div>
            </form>
        </div>

        <!-- Architecture Page -->
        <div id="page-arch" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><rect x="2" y="3" width="6" height="5" rx="1"/><rect x="16" y="3" width="6" height="5" rx="1"/><rect x="9" y="16" width="6" height="5" rx="1"/><path d="M5 8v3a2 2 0 002 2h10a2 2 0 002-2V8"/><path d="M12 13v3"/></svg>
                    <h2>Gateway — Agent 拓扑</h2>
                </header>
                <div class="card-body">
                    <div class="gw-grid">{gw_cards}</div>
                    <div id="agent-status-summary" class="agent-status-summary">Agent 状态加载中...</div>
                </div>
            </div>
        </div>

        <!-- Approvals Page -->
        <div id="page-approvals" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2h11"/></svg>
                    <h2>待审批拓扑变更</h2>
                </header>
                <div class="card-body" id="approval-list">
                    <div class="approval-empty">加载中...</div>
                </div>
            </div>
        </div>

        <!-- Audit Page -->
        <div id="page-audit" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><path d="M14 2v6h6"/></svg>
                    <h2>审计日志</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar">
                        <select id="audit-event-type" class="input"><option value="">类型</option></select>
                        <select id="audit-action" class="input"><option value="">动作</option></select>
                        <select id="audit-result" class="input"><option value="">结果</option></select>
                        <select id="audit-actor" class="input"><option value="">角色</option></select>
                        <input type="text" id="audit-keyword" class="input" placeholder="搜索关键词..." style="max-width:200px">
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadAuditLogs()">刷新</button>
                    </div>
                    <table class="log-table"><thead><tr>
                        <th>时间</th><th>类型</th><th>动作</th><th>结果</th><th>角色</th><th>详情</th>
                    </tr></thead><tbody id="audit-tbody"></tbody></table>
                </div>
            </div>
        </div>

        <!-- System Log Page -->
        <div id="page-syslog" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><polyline points="4,17 10,11 4,5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
                    <h2>系统日志</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar">
                        <select id="system-level" class="input"><option value="">级别</option></select>
                        <select id="system-logger" class="input"><option value="">模块</option></select>
                        <input type="text" id="system-keyword" class="input" placeholder="搜索关键词..." style="max-width:200px">
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadSystemLogs()">刷新</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="exportSystemLogs()">导出</button>
                    </div>
                    <table class="log-table"><thead><tr>
                        <th>时间</th><th>级别</th><th>模块</th><th>消息</th>
                    </tr></thead><tbody id="system-tbody"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Command Cards Page -->
        <div id="page-commands" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M4 6h16M4 12h16M4 18h10"/><circle cx="18" cy="18" r="2"/></svg>
                    <h2>命令卡执行</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar">
                        <select id="cmd-card-key" class="input" style="min-width:220px"><option value="">选择命令卡</option></select>
                        <input type="text" id="cmd-requested-by" class="input" value="dashboard" style="max-width:140px" placeholder="请求人">
                        <label style="display:inline-flex;align-items:center;gap:6px;color:var(--text-secondary);font-size:0.8rem">
                            <input type="checkbox" id="cmd-auto-approve"> 自动审批
                        </label>
                        <button type="button" class="btn btn-sm btn-primary" onclick="submitCommandCardRun()">提交执行</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadCommandCards();loadCommandRuns();">刷新</button>
                    </div>
                    <textarea id="cmd-params" class="input" style="min-height:96px;font-family:var(--font-mono)" placeholder='运行参数 JSON，如 {{"service":"api"}}'>{{}}</textarea>
                </div>
            </div>

            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3 6h.01M3 12h.01M3 18h.01"/></svg>
                    <h2>执行流水</h2>
                </header>
                <div class="card-body">
                    <table class="log-table"><thead><tr>
                        <th>ID</th><th>命令卡</th><th>状态</th><th>风险</th><th>请求人</th><th>更新时间</th><th>动作</th>
                    </tr></thead><tbody id="cmd-run-tbody"></tbody></table>
                </div>
            </div>

            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M8 9h8M8 13h8M8 17h5"/></svg>
                    <h2>命令卡清单</h2>
                </header>
                <div class="card-body">
                    <table class="log-table"><thead><tr>
                        <th>卡片</th><th>风险</th><th>启用</th><th>说明</th>
                    </tr></thead><tbody id="cmd-card-tbody"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Prompts Page -->
        <div id="page-prompts" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/></svg>
                    <h2>提示词模板管理</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar" style="gap:8px;flex-wrap:wrap">
                        <input type="text" id="prompt-search" class="input" placeholder="搜索 key / 标题 / 标签 / 内容..." style="max-width:300px" oninput="loadPrompts()">
                        <label style="display:inline-flex;align-items:center;gap:6px;color:var(--text-secondary);font-size:0.8rem">
                            <input type="checkbox" id="prompt-enabled-only" onchange="loadPrompts()"> 仅启用
                        </label>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="seedPromptTemplates(false)">
                            导入常用模板
                        </button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="seedPromptTemplates(true)">
                            覆盖更新模板
                        </button>
                        <button type="button" class="btn btn-sm btn-primary" onclick="openPromptCreatePopup()">
                            新建模板
                        </button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="openPromptPastePopup()">
                            快速粘贴
                        </button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadPrompts()">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 11-2.12-9.36L23 10"/></svg>
                            刷新
                        </button>
                    </div>
                    <table class="log-table" id="prompt-table">
                        <thead><tr>
                            <th style="width:220px">模板 Key</th>
                            <th style="width:180px">标题</th>
                            <th style="width:140px">Agent/Tool</th>
                            <th style="width:160px">标签</th>
                            <th style="width:90px">状态</th>
                            <th style="width:160px">更新时间</th>
                            <th style="width:180px;text-align:center">操作</th>
                        </tr></thead>
                        <tbody id="prompt-tbody"></tbody>
                    </table>
                    <div id="prompt-empty" class="approval-empty">加载中...</div>
                </div>
            </div>
            <div id="prompt-popup" class="prompt-popup" style="display:none">
                <div class="prompt-popup-header">
                    <span id="prompt-popup-title" class="prompt-popup-title"></span>
                    <button class="prompt-popup-close" onclick="closePromptPopup()">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    </button>
                </div>
                <div class="prompt-popup-meta">
                    <input type="text" id="prompt-popup-key" class="input" placeholder="prompt_key，例如 orch.review.plan_dag">
                    <input type="text" id="prompt-popup-title-input" class="input" placeholder="标题">
                    <input type="text" id="prompt-popup-agent-key" class="input" placeholder="agent_key，例如 master">
                    <input type="text" id="prompt-popup-tool-name" class="input" placeholder="tool_name，例如 task">
                    <input type="text" id="prompt-popup-tags" class="input" placeholder="标签，逗号分隔，如 preset,orchestration">
                    <label style="display:inline-flex;align-items:center;gap:6px;color:var(--text-secondary);font-size:0.8rem">
                        <input type="checkbox" id="prompt-popup-enabled" checked> 启用
                    </label>
                </div>
                <textarea id="prompt-popup-variables" class="prompt-popup-variables" placeholder='模板变量(JSON对象)，例如 {{"PROJECT_ROOT":"项目根目录"}}'></textarea>
                <textarea id="prompt-popup-textarea" class="prompt-popup-textarea" placeholder="提示词正文"></textarea>
                <div class="prompt-popup-actions">
                    <span class="prompt-shortcut-tip">⌘/Ctrl+S 保存 · Esc 关闭</span>
                    <span class="copy-ok" id="prompt-popup-copy-ok">已复制</span>
                    <button class="btn btn-sm btn-secondary" id="prompt-popup-fullscreen-btn" onclick="togglePromptPopupFullscreen()">全屏编辑</button>
                    <button class="btn btn-sm btn-secondary" onclick="copyPromptPopup()">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                        复制
                    </button>
                    <button class="btn btn-sm btn-primary" id="prompt-popup-save" onclick="savePromptPopup()">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/></svg>
                        保存
                    </button>
                    <button class="btn btn-sm btn-secondary" onclick="savePromptPopup(true)">保存并关闭</button>
                </div>
            </div>
        </div>

        <!-- Agent Monitor Page -->
        <div id="page-monitor" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                    <h2>Agent 健康监控</h2>
                </header>
                <div class="card-body">
                    <div id="agent-status-summary" style="display:flex;gap:12px;flex-wrap:wrap;margin-bottom:16px">
                        <span class="badge badge-green">running: <b id="mon-running">0</b></span>
                        <span class="badge badge-blue">idle: <b id="mon-idle">0</b></span>
                        <span class="badge badge-amber">stuck: <b id="mon-stuck">0</b></span>
                        <span class="badge badge-red">error: <b id="mon-error">0</b></span>
                        <span class="badge badge-gray">disconnected: <b id="mon-disconnected">0</b></span>
                        <span class="badge badge-gray">unknown: <b id="mon-unknown">0</b></span>
                    </div>
                    <div class="log-toolbar">
                        <button type="button" class="btn btn-sm btn-secondary" onclick="refreshAgentMonitor()">刷新</button>
                        <span id="mon-updated" style="font-size:0.78rem;color:var(--text-secondary)">最后更新: --</span>
                    </div>
                    <table class="log-table"><thead><tr>
                        <th>Agent</th><th>名称</th><th>状态</th><th>停滞(秒)</th><th>错误</th><th>最近输出</th>
                    </tr></thead><tbody id="mon-tbody"></tbody></table>
                    <div id="mon-empty" class="approval-empty" style="display:none">暂无 Agent 会话</div>
                </div>
            </div>
            <!-- Terminal Live Viewer -->
            <div class="card" style="margin-top:16px">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
                    <h2>终端实时查看器</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar" style="flex-wrap:wrap;gap:8px">
                        <div class="terminal-mode-group">
                            <button type="button" class="btn btn-sm terminal-mode-btn active" data-mode="stream" onclick="switchTerminalMode('stream')">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="2"/><path d="M16.24 7.76a6 6 0 010 8.49m-8.48-.01a6 6 0 010-8.49m11.31-2.82a10 10 0 010 14.14m-14.14 0a10 10 0 010-14.14"/></svg>
                                实时
                            </button>
                            <button type="button" class="btn btn-sm terminal-mode-btn" data-mode="stream-cmd" onclick="switchTerminalMode('stream-cmd')">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
                                实时+命令
                            </button>
                            <button type="button" class="btn btn-sm terminal-mode-btn" data-mode="stream-cmd-snap" onclick="switchTerminalMode('stream-cmd-snap')">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>
                                实时+命令+画面
                            </button>
                        </div>
                        <select id="terminal-agent-select" class="input" style="max-width:220px;font-size:0.78rem" onchange="onTerminalAgentChange()">
                            <option value="">选择 Agent...</option>
                        </select>
                        <span id="terminal-stream-status" class="badge badge-gray" style="font-size:0.7rem">未连接</span>
                    </div>
                    <div id="terminal-output" class="terminal-output"><span style="color:var(--text-muted)">选择 Agent 后开始实时推流...</span></div>
                    <div id="terminal-cmd-bar" class="terminal-cmd-bar" style="display:none">
                        <input type="text" id="terminal-cmd-input" class="input" placeholder="输入命令后回车发送..." style="flex:1;font-family:var(--font-mono);font-size:0.78rem" />
                        <button type="button" class="btn btn-sm btn-primary" onclick="termSendCommand()">发送</button>
                    </div>
                </div>
            </div>
        </div>

        <!-- Telegram Bot Page -->
        <div id="page-telegram" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4 20-7z"/></svg>
                    <h2>Telegram Bot 管理</h2>
                </header>
                <div class="card-body">
                    <div id="tg-status" style="display:flex;gap:12px;flex-wrap:wrap;margin-bottom:16px;align-items:center">
                        <span id="tg-running-badge" class="badge badge-gray">状态: 加载中</span>
                        <span id="tg-bot-name" style="font-size:0.82rem;color:var(--text-secondary)"></span>
                        <span id="tg-chat-id" style="font-size:0.78rem;color:var(--text-muted);font-family:var(--font-mono)"></span>
                    </div>
                    <div class="log-toolbar" style="margin-bottom:16px">
                        <button type="button" class="btn btn-sm btn-primary" onclick="tgStartBridge()">启动 Bot</button>
                        <button type="button" class="btn btn-sm btn-danger" onclick="tgStopBridge()">停止 Bot</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="tgRefresh()">刷新</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="tgClearHistory()">清空记录</button>
                    </div>

                    <!-- 测试发送 -->
                    <div style="margin-bottom:20px;display:flex;gap:8px">
                        <input id="tg-test-input" class="input" style="flex:1" placeholder="输入测试消息，发送到 Telegram..." />
                        <button type="button" class="btn btn-sm btn-primary" onclick="tgSendTest()">发送</button>
                    </div>

                    <!-- 对话记录 -->
                    <h3 style="font-size:0.85rem;font-weight:600;margin-bottom:12px">对话记录</h3>
                    <div id="tg-chat-log" style="max-height:500px;overflow-y:auto;border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px;background:var(--bg-base)">
                        <div class="approval-empty">加载中...</div>
                    </div>
                </div>
            </div>
        </div>
    </main>
</div>

<script>
window.__SSE_SYNC_SEC = {sse_sync_sec};
window.__TG_AUTO_REFRESH_SEC = {_safe_int(config.get("TG_AUTO_REFRESH_SEC", "60"), 60, 0, 3600)};
window.__AUDIT_LOG_LIMIT = {_safe_int(config.get("AUDIT_LOG_LIMIT", "100"), 100, 10, 500)};
window.__SYSTEM_LOG_LIMIT = {_safe_int(config.get("SYSTEM_LOG_LIMIT", "100"), 100, 10, 500)};
</script>
<script src="/static/app.js"></script>
</body>
</html>'''


# ─── HTTP Handler ───

MIME_TYPES = {
    ".css": "text/css; charset=utf-8",
    ".js": "application/javascript; charset=utf-8",
    ".svg": "image/svg+xml",
    ".png": "image/png",
    ".ico": "image/x-icon",
}


class DashboardHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path
        params = urllib.parse.parse_qs(parsed.query)

        if path in ("/", "/index.html"):
            page_html = render_html()
            self._respond(200, "text/html; charset=utf-8", page_html.encode("utf-8"))

        elif path.startswith("/static/"):
            self._serve_static(path)

        elif path == "/health":
            payload = {"ok": True, "status": "live", "ts": datetime.now(timezone.utc).isoformat()}
            self._respond(200, "application/json; charset=utf-8",
                          json.dumps(payload, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

        elif path == "/ready":
            started = time.perf_counter()
            is_ready, err_msg = _check_dashboard_ready()
            db_latency_ms = int(max(0.0, (time.perf_counter() - started) * 1000.0))
            payload = {
                "ok": bool(is_ready),
                "status": "ready" if is_ready else "not_ready",
                "ts": datetime.now(timezone.utc).isoformat(),
                "db_latency_ms": db_latency_ms,
            }
            if err_msg:
                payload["error"] = err_msg
            self._respond(200 if is_ready else 503, "application/json; charset=utf-8",
                          json.dumps(payload, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

        elif path == "/api/events/stream":

            self._serve_event_stream()

        elif path == "/api/config":
            config = load_current_config()
            if config.get("OPENAI_API_KEY"):
                k = config["OPENAI_API_KEY"]
                config["OPENAI_API_KEY"] = k[:8] + "..." + k[-4:] if len(k) > 12 else "***"
            config["ignored_keys"] = []
            self._respond(200, "application/json", json.dumps(config).encode("utf-8"))

        elif path == "/api/agent-status":

            lines = _safe_int(params.get("lines", ["30"])[0], 30, 1, 200)
            payload = _build_agent_status_snapshot(read_lines=lines)
            status_code = 200 if payload.get("ok") else 503
            self._respond(
                status_code,
                "application/json; charset=utf-8",
                json.dumps(payload, ensure_ascii=False).encode("utf-8"),
                headers={"Cache-Control": "no-store"},
            )

        elif path == "/api/terminal/read":
            session_id = params.get("session_id", [""])[0].strip()
            lines_count = _safe_int(params.get("lines", ["60"])[0], 60, 1, 200)
            if not session_id:
                self._respond(400, "application/json", b'{"ok":false,"error":"missing session_id"}')
            else:
                result = read_session_screen(session_id, lines=lines_count)
                code = 200 if result.get("ok") else 500
                self._respond(code, "application/json; charset=utf-8",
                              json.dumps(result, ensure_ascii=False).encode("utf-8"),
                              headers={"Cache-Control": "no-store"})

        elif path == "/api/terminal/sessions":
            try:
                # Merge two sources: state file (registered agents) + live scan (master, etc.)
                merged: dict[str, dict] = {}  # session_id -> info

                # 1) registered agents from state file
                try:
                    agent_status = _build_agent_status_snapshot(read_lines=0)
                    for a in (agent_status.get("agents") or []):
                        sid = str(a.get("session_id", "") or "").strip()
                        if sid:
                            merged[sid] = {
                                "session_id": sid,
                                "badge": str(a.get("badge", "") or "").strip(),
                                "agent_id": str(a.get("agent_id", "") or "").strip(),
                                "agent_name": str(a.get("agent_name", "") or "").strip(),
                                "name": str(a.get("agent_name", "") or "").strip(),
                            }
                except Exception:
                    pass

                # 2) live sessions (picks up master + unregistered)
                try:
                    window_id, live = _list_live_sessions()
                    for s in live:
                        sid = str(s.get("session_id", "") or "").strip()
                        if sid and sid not in merged:
                            merged[sid] = s
                except Exception:
                    window_id = ""

                sessions = list(merged.values())
                self._respond(200, "application/json; charset=utf-8",
                              json.dumps({"ok": True, "sessions": sessions},
                                         ensure_ascii=False).encode("utf-8"),
                              headers={"Cache-Control": "no-store"})
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/info":
            info = get_tg_bridge_info()
            self._respond(200, "application/json; charset=utf-8",
                          json.dumps({"ok": True, **info}, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

        elif path == "/api/tg/history":
            limit = _safe_int(params.get("limit", ["50"])[0], 50, 1, 200)
            history = get_tg_history(limit=limit)
            self._respond(200, "application/json; charset=utf-8",
                          json.dumps({"ok": True, "history": history}, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

        elif path == "/api/topology/approvals":
            status = params.get("status", [""])[0]
            approvals = list_approvals(status=status, limit=100)
            self._respond(200, "application/json",
                          json.dumps({"ok": True, "approvals": approvals}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/audit":
            limit = _safe_int(params.get("limit", ["100"])[0], 100, 1, 500)
            events = query_events(
                limit=limit,
                event_type=params.get("event_type", [""])[0],
                action=params.get("action", [""])[0],
                result=params.get("result", [""])[0],
                actor=params.get("actor", [""])[0],
                keyword=params.get("keyword", [""])[0],
            )
            filters = list_audit_filter_values()
            self._respond(200, "application/json",
                          json.dumps({"ok": True, "events": events, "filters": filters}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/system-log":
            limit = _safe_int(params.get("limit", ["100"])[0], 100, 1, 500)
            logs = query_system_logs(
                limit=limit,
                level=params.get("level", [""])[0],
                logger_name=params.get("logger", [""])[0],
                keyword=params.get("keyword", [""])[0],
            )
            filters = list_system_filter_values()
            self._respond(200, "application/json",
                          json.dumps({"ok": True, "logs": logs, "filters": filters}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/system-log/export":
            limit = _safe_int(params.get("limit", ["100"])[0], 100, 1, 2000)
            logs = query_system_logs(
                limit=limit,
                level=params.get("level", [""])[0],
                logger_name=params.get("logger", [""])[0],
                keyword=params.get("keyword", [""])[0],
            )
            payload = "\n".join(json.dumps(row, ensure_ascii=False) for row in logs) + ("\n" if logs else "")
            ts = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
            self._respond(200, "application/x-ndjson; charset=utf-8", payload.encode("utf-8"),
                          headers={"Content-Disposition": f"attachment; filename=system-log-{ts}.ndjson",
                                   "Cache-Control": "no-store"})

        elif path == "/api/prompt-templates":
            try:
                limit = _safe_int(params.get("limit", ["200"])[0], 200, 1, 500)
                templates = list_prompt_templates(
                    agent_key=str(params.get("agent_key", [""])[0] or "").strip(),
                    tool_name=str(params.get("tool_name", [""])[0] or "").strip(),
                    keyword=str(params.get("keyword", [""])[0] or "").strip(),
                    enabled_only=_safe_bool(params.get("enabled_only", ["0"])[0], default=False),
                    limit=limit,
                )
                self._respond(
                    200,
                    "application/json",
                    json.dumps({"ok": True, "count": len(templates), "templates": templates}, ensure_ascii=False).encode("utf-8"),
                )
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/prompts":
            try:
                agents = _get_all_agent_specs()
                total_tools = sum(len(a['tools']) for a in agents)
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "agents": agents, "total_tools": total_tools}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/task-traces":
            try:
                limit = _safe_int(params.get("limit", ["200"])[0], 200, 1, 500)
                trace_id = str(params.get("trace_id", [""])[0] or "").strip()
                component = str(params.get("component", [""])[0] or "").strip()
                status = str(params.get("status", [""])[0] or "").strip()

                rows = list_task_traces(
                    trace_id=trace_id,
                    component=component,
                    status=status,
                    limit=limit,
                )
                traces = _summarize_traces(rows)
                self._respond(
                    200,
                    "application/json",
                    json.dumps({"ok": True, "traces": traces, "rows": rows}, ensure_ascii=False).encode("utf-8"),
                )
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/task-traces/spans":
            try:
                trace_id = str(params.get("trace_id", [""])[0] or "").strip()
                limit = _safe_int(params.get("limit", ["500"])[0], 500, 1, 1000)
                rows = list_task_trace_spans(trace_id=trace_id, limit=limit)
                self._respond(
                    200,
                    "application/json",
                    json.dumps({"ok": True, "trace_id": trace_id, "spans": rows}, ensure_ascii=False).encode("utf-8"),
                )
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/prompt-versions":
            try:
                prompt_key = str(params.get("prompt_key", [""])[0] or "").strip()
                limit = _safe_int(params.get("limit", ["50"])[0], 50, 1, 200)
                versions = list_prompt_template_versions(prompt_key=prompt_key, limit=limit)
                self._respond(
                    200,
                    "application/json",
                    json.dumps({"ok": True, "prompt_key": prompt_key, "versions": versions}, ensure_ascii=False).encode("utf-8"),
                )
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-cards":
            try:
                from agent_ops_store import list_command_cards

                limit = _safe_int(params.get("limit", ["200"])[0], 200, 1, 500)
                cards = list_command_cards(
                    keyword=params.get("keyword", [""])[0],
                    risk_level=params.get("risk_level", [""])[0],
                    enabled_only=_safe_bool(params.get("enabled_only", ["0"])[0], default=False),
                    limit=limit,
                )
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "cards": cards}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-card-runs":
            try:
                from command_card_executor import list_command_card_runs

                limit = _safe_int(params.get("limit", ["200"])[0], 200, 1, 500)
                runs = list_command_card_runs(
                    card_key=params.get("card_key", [""])[0],
                    status=params.get("status", [""])[0],
                    requested_by=params.get("requested_by", [""])[0],
                    limit=limit,
                )
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "runs": runs}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
        else:
            self.send_error(404)

    def _safe_content_length(self) -> int:
        """Parse Content-Length defensively; return 0 on invalid input."""
        raw = self.headers.get("Content-Length", "0")
        try:
            return max(0, int(raw))
        except (TypeError, ValueError):
            return 0

    def do_POST(self) -> None:
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path

        if path == "/api/config":
            body = self.rfile.read(self._safe_content_length())
            try:
                updates = json.loads(body)
                saved_keys = save_config(updates)
                append_event(event_type="config", action="update", result="ok",
                             actor="dashboard", target=".env", detail=",".join(saved_keys))
                _publish_dashboard_event("sync", {
                    "scope": ["config", "approvals", "audit", "system"],
                    "reason": "config_updated",
                    "updated_keys": saved_keys,
                })
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "updated": saved_keys, "restart_required": True,
                                          "message": "配置已写入 .env，需重启 Master 进程生效"},
                                         ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                detail = str(e)
                append_event(event_type="config", action="update", result="invalid_input",
                             actor="dashboard", target=".env", detail=detail)
                self._respond(400, "application/json",
                              json.dumps({"ok": False, "error": detail, "error_detail": detail},
                                         ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                detail = str(e)
                append_event(event_type="config", action="update", result="error",
                             actor="dashboard", target=".env", detail=detail)
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": detail, "error_detail": detail},
                                         ensure_ascii=False).encode("utf-8"))

        elif path == "/api/prompt-templates":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                saved = _save_prompt_template_entry(data, updated_by=updated_by)
                result = {"ok": True, "prompt": saved}
                _publish_dashboard_event("sync", {
                    "scope": ["prompts", "audit"],
                    "reason": "prompt_template_saved",
                    "prompt_key": saved.get("prompt_key", ""),
                })
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/prompt-templates/toggle":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                prompt_key = str(data.get("prompt_key", "") or "").strip()
                if not prompt_key:
                    raise ValueError("prompt_key 不能为空")
                enabled = _safe_bool(data.get("enabled", True), default=True)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                result = set_prompt_template_enabled(prompt_key=prompt_key, enabled=enabled, updated_by=updated_by)
                code = 200 if result.get("ok") else 400
                if result.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["prompts", "audit"],
                        "reason": "prompt_template_toggle",
                        "prompt_key": prompt_key,
                        "enabled": enabled,
                    })
                self._respond(code, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/prompt-templates/rollback":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                prompt_key = str(data.get("prompt_key", "") or "").strip()
                version_id = _safe_int(data.get("version_id", "0"), 0, 1, 2_147_483_647)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                result = rollback_prompt_template(prompt_key=prompt_key, version_id=version_id, updated_by=updated_by)
                code = 200 if result.get("ok") else 400
                if result.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["prompts", "audit"],
                        "reason": "prompt_template_rollback",
                        "prompt_key": prompt_key,
                        "version_id": version_id,
                    })
                self._respond(code, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/prompt-templates/seed":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body) if body else {}
                overwrite = _safe_bool(data.get("overwrite", False), default=False)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                result = _seed_common_prompt_templates(overwrite=overwrite, updated_by=updated_by)
                _publish_dashboard_event("sync", {
                    "scope": ["prompts", "audit"],
                    "reason": "prompt_templates_seeded",
                    "inserted": int(result.get("inserted") or 0),
                    "updated": int(result.get("updated") or 0),
                    "skipped": int(result.get("skipped") or 0),
                })
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/prompts":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                agent_key = str(data.get('agent_key', '')).strip()
                tool_name = str(data.get('tool_name', '')).strip()
                prompt_text = str(data.get('prompt_text', '')).strip()
                if not agent_key or not tool_name:
                    raise ValueError('agent_key 和 tool_name 不能为空')
                result = _save_prompt(agent_key, tool_name, prompt_text)
                _publish_dashboard_event("sync", {
                    "scope": ["prompts", "audit"],
                    "reason": "prompt_saved",
                    "agent_key": agent_key,
                    "tool_name": tool_name,
                })
                self._respond(200, "application/json",
                              json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-cards/execute":
            body = self.rfile.read(self._safe_content_length())
            try:
                from command_card_executor import execute_command_card

                data = json.loads(body)
                card_key = str(data.get("card_key", "")).strip()
                if not card_key:
                    raise ValueError("card_key 不能为空")

                params_obj = _parse_json_object(data.get("params", {}), "params")
                requested_by = _safe_identifier(data.get("requested_by", "dashboard")) or "dashboard"
                auto_approve = _safe_bool(data.get("auto_approve", False), default=False)
                reviewer = str(data.get("reviewer", "")).strip()
                review_note = str(data.get("review_note", "")).strip()
                timeout_sec = _safe_int(data.get("timeout_sec", "240"), 240, 1, 3600)

                res = execute_command_card(
                    card_key=card_key,
                    params=params_obj,
                    requested_by=requested_by,
                    auto_approve=auto_approve,
                    reviewer=reviewer,
                    review_note=review_note,
                    timeout_sec=timeout_sec,
                )

                if res.get("ok"):
                    run = res.get("run", {})
                    _publish_dashboard_event("sync", {
                        "scope": ["command_cards", "audit", "system"],
                        "reason": "command_card_execute",
                        "run_id": run.get("id"),
                        "status": run.get("status", ""),
                        "pending_review": bool(res.get("pending_review", False)),
                    })

                code = 200 if res.get("ok") else 400
                self._respond(code, "application/json", json.dumps(res, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-card-runs/review":
            body = self.rfile.read(self._safe_content_length())
            try:
                from command_card_executor import review_command_card_run

                data = json.loads(body)
                run_id = _parse_required_int(data.get("run_id"), "run_id", 1, 10_000_000)
                decision = str(data.get("decision", "")).strip().lower()
                reviewer = _safe_identifier(data.get("reviewer", "dashboard")) or "dashboard"
                note = str(data.get("note", "")).strip()

                res = review_command_card_run(
                    run_id=run_id,
                    decision=decision,
                    reviewer=reviewer,
                    note=note,
                )
                if res.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["command_cards", "audit", "system"],
                        "reason": f"command_card_review_{decision}",
                        "run_id": run_id,
                    })

                code = 200 if res.get("ok") else 400
                self._respond(code, "application/json", json.dumps(res, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-card-runs/execute":
            body = self.rfile.read(self._safe_content_length())
            try:
                from command_card_executor import execute_command_card_run

                data = json.loads(body)
                run_id = _parse_required_int(data.get("run_id"), "run_id", 1, 10_000_000)
                actor = _safe_identifier(data.get("actor", "dashboard")) or "dashboard"
                timeout_sec = _safe_int(data.get("timeout_sec", "240"), 240, 1, 3600)

                res = execute_command_card_run(
                    run_id=run_id,
                    actor=actor,
                    timeout_sec=timeout_sec,
                )
                if res.get("ok"):
                    run = res.get("run", {})
                    _publish_dashboard_event("sync", {
                        "scope": ["command_cards", "audit", "system"],
                        "reason": "command_card_run_execute",
                        "run_id": run_id,
                        "status": run.get("status", ""),
                    })

                code = 200 if res.get("ok") else 400
                self._respond(code, "application/json", json.dumps(res, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/start":
            try:
                ok = start_tg_bridge()
                self._respond(200, "application/json",
                              json.dumps({"ok": ok, "message": "TG bridge 已启动" if ok else "启动失败（检查 TG_BOT_TOKEN）"}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/stop":
            try:
                stop_tg_bridge()
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "message": "TG bridge 已停止"}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/send":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                text = str(data.get("text", "")).strip()
                if not text:
                    raise ValueError("text 不能为空")
                ok = send_message_to_tg(text)
                self._respond(200, "application/json",
                              json.dumps({"ok": ok, "message": "已发送" if ok else "发送失败（检查 Token 和 Chat ID）"}, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/clear-history":
            try:
                clear_tg_history()
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "message": "记录已清空"}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/watchdog/start":
            try:
                start_watchdog()
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "message": "看门狗已启动"}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/tg/watchdog/stop":
            try:
                stop_watchdog()
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "message": "看门狗已停止"}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path.startswith("/api/topology/approvals/"):
            parts = path.strip("/").split("/")
            if len(parts) == 5:
                _, _, _, approval_id, action = parts
                if not is_valid_approval_id(approval_id):
                    self._respond(400, "application/json", b'{"ok":false,"error":"invalid approval id"}')
                    return
                if action == "approve":
                    res = approve_approval(approval_id=approval_id, reviewer="dashboard")
                elif action == "reject":
                    res = reject_approval(approval_id=approval_id, reviewer="dashboard")
                else:
                    res = {"ok": False, "error": f"unknown action: {action}"}
                if res.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["approvals", "audit", "system"],
                        "reason": f"approval_{action}",
                        "approval_id": approval_id,
                    })
                code = 200 if res.get("ok") else 400
                self._respond(code, "application/json", json.dumps(res, ensure_ascii=False).encode("utf-8"))
            else:
                self._respond(400, "application/json", b'{"ok":false,"error":"invalid path"}')
        elif path == "/api/terminal/send":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                session_id = str(data.get("session_id", "")).strip()
                text = str(data.get("text", ""))
                if not session_id:
                    raise ValueError("missing session_id")
                result = send_to_session(session_id, text)
                code = 200 if result.get("ok") else 500
                self._respond(code, "application/json; charset=utf-8",
                              json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(400, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/terminal/stream/start":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                session_id = str(data.get("session_id", "")).strip()
                if not session_id:
                    raise ValueError("missing session_id")
                result = start_session_streamer(session_id, publish_fn=_publish_dashboard_event)
                self._respond(200, "application/json; charset=utf-8",
                              json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(400, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/terminal/stream/stop":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                session_id = str(data.get("session_id", "")).strip()
                if not session_id:
                    raise ValueError("missing session_id")
                result = stop_session_streamer(session_id)
                self._respond(200, "application/json; charset=utf-8",
                              json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(400, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        else:
            self.send_error(404)

    def _serve_event_stream(self) -> None:
        sync_interval_sec = _safe_int(os.getenv("DASHBOARD_SSE_SYNC_SEC", "5"), 5, 1, 60)
        channel = EVENT_BUS.subscribe()

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Connection", "keep-alive")
        self.send_header("X-Accel-Buffering", "no")
        self.end_headers()

        connected_event = {
            "id": "hello",
            "event": "connected",
            "ts": datetime.now(timezone.utc).isoformat(),
            "payload": {
                "sync_interval_sec": sync_interval_sec,
            },
        }

        try:
            self.wfile.write(_encode_sse_event(connected_event))
            self.wfile.flush()

            initial_snapshot = _build_agent_status_snapshot()
            self.wfile.write(
                _encode_sse_event(
                    {
                        "id": "agent_status_init",
                        "event": "agent_status",
                        "ts": datetime.now(timezone.utc).isoformat(),
                        "payload": {
                            "ok": bool(initial_snapshot.get("ok")),
                            "summary": initial_snapshot.get("summary", _empty_agent_status_summary()),
                            "agents": initial_snapshot.get("agents", []),
                            "source": initial_snapshot.get("source", {}),
                            "error": str(initial_snapshot.get("error", "") or ""),
                        },
                    }
                )
            )
            self.wfile.flush()

            while True:
                try:
                    event = channel.get(timeout=sync_interval_sec)
                except queue.Empty:
                    event = {
                        "id": "heartbeat",
                        "event": "sync",
                        "ts": datetime.now(timezone.utc).isoformat(),
                        "payload": {
                            "scope": ["approvals", "audit", "system", "command_cards", "agent_status"],
                            "reason": "heartbeat",
                        },
                    }

                self.wfile.write(_encode_sse_event(event))
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            return
        except Exception as exc:
            logger.debug("SSE stream terminated: %s", exc)
        finally:
            EVENT_BUS.unsubscribe(channel)

    def _serve_static(self, path: str) -> None:
        """Serve files from static/ directory."""
        rel = path.lstrip("/").replace("static/", "", 1)
        safe_rel = Path(rel).name  # prevent path traversal
        file_path = STATIC_DIR / safe_rel
        if not file_path.exists() or not file_path.is_file():
            self.send_error(404)
            return
        suffix = file_path.suffix
        content_type = MIME_TYPES.get(suffix, "application/octet-stream")
        self._respond(200, content_type, file_path.read_bytes(),
                      headers={"Cache-Control": "public, max-age=300"})

    def _respond(self, code: int, content_type: str, body: bytes, headers: Optional[dict[str, str]] = None) -> None:
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        if headers:
            for key, value in headers.items():
                self.send_header(key, value)
        self.end_headers()
        self.wfile.write(body)
        self.wfile.flush()
        self.close_connection = True

    def handle(self) -> None:
        try:
            super().handle()
        except ConnectionResetError:
            return

    def log_message(self, format: str, *args: Any) -> None:
        pass


def main() -> None:
    port = _safe_int(os.getenv("DASHBOARD_PORT", "8080"), 8080, 1, 65535)

    from logging_setup import setup_global_logging
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    setup_global_logging(default_level=log_level)

    logger.info("Dashboard v2 启动中 port=%s", port)

    Path(__file__).parent.joinpath("data").mkdir(exist_ok=True)

    start_tg_bridge()

    server = http.server.ThreadingHTTPServer(("0.0.0.0", port), DashboardHandler)
    logger.info("Dashboard v2 已启动: http://localhost:%s", port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Dashboard 已停止")
        server.shutdown()



if __name__ == "__main__":
    main()
