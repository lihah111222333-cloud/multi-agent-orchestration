"""配置管理 Web 面板 (v2 — Dark OLED Design)

启动: python3 dashboard.py
访问: http://localhost:8080
"""

import json
import logging
import os
import html
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

from agent_monitor import classify_status
from agents.iterm_bridge import list_iterm_agent_sessions, read_iterm_output
from audit_log import append_event, query_events, list_filter_values as list_audit_filter_values
from db.postgres import fetch_all, fetch_one
from system_log import query_logs as query_system_logs, list_filter_values as list_system_filter_values
from topology_approval import approve_approval, is_valid_approval_id, list_approvals, reject_approval

ENV_FILE = Path(__file__).parent / ".env"
STATIC_DIR = Path(__file__).parent / "static"
logger = logging.getLogger(__name__)

AGENT_STATUS_STUCK_SEC = 60
_AGENT_STATUS_MEMORY: dict[str, dict[str, Any]] = {}
_AGENT_STATUS_LOCK = threading.Lock()
_AGENT_STATUS_NAMES = ("running", "idle", "stuck", "error", "disconnected", "unknown")

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
    "AUDIT_LOG_LIMIT": "100",
    "SYSTEM_LOG_LIMIT": "100",
    "AUDIT_LOG_MAX_BYTES": str(5 * 1024 * 1024),
    "AUDIT_LOG_BACKUP_COUNT": "3",
    "SYSTEM_LOG_MAX_BYTES": str(10 * 1024 * 1024),
    "SYSTEM_LOG_BACKUP_COUNT": "3",
    "CONFIG_BACKUP_ENABLED": "1",
    "CONFIG_BACKUP_KEEP": "5",
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


def _build_agent_status_snapshot_from_table() -> Optional[dict[str, Any]]:
    ts = datetime.now(timezone.utc).isoformat()

    try:
        rows = fetch_all("SELECT * FROM agent_status LIMIT 500")
    except Exception as exc:
        if _is_agent_status_table_missing_error(exc):
            return None
        logger.debug("读取 agent_status 表失败，回退旧状态采集逻辑", exc_info=True)
        return None

    if not isinstance(rows, list):
        rows = []

    by_agent: dict[str, tuple[float, dict[str, Any]]] = {}
    latest_ts = ts
    latest_epoch = 0.0

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

    agents = [item[1] for item in sorted(by_agent.values(), key=lambda row: row[0], reverse=True)]

    return {
        "ok": True,
        "ts": latest_ts,
        "summary": _summarize_agent_status(agents),
        "agents": agents,
        "source": {
            "sessions_ok": True,
            "output_ok": True,
            "db_ok": True,
            "mode": "agent_status_table",
        },
    }


def _build_agent_status_snapshot_legacy(read_lines: int = 30) -> dict[str, Any]:
    """Collect current iTerm agent status snapshot for dashboard API."""
    ts = datetime.now(timezone.utc).isoformat()

    sessions_payload = list_iterm_agent_sessions()
    if not sessions_payload.get("ok"):
        return {
            "ok": False,
            "ts": ts,
            "error": str(sessions_payload.get("error", "list_sessions_failed")),
            "summary": _empty_agent_status_summary(),
            "agents": [],
            "source": {"sessions_ok": False, "output_ok": False},
        }

    sessions = sessions_payload.get("sessions", [])
    if not isinstance(sessions, list):
        sessions = []

    output_payload = read_iterm_output(all_agents=True, read_lines=max(1, int(read_lines)))
    if not output_payload.get("ok"):
        agents = [
            {
                "agent_id": str(item.get("agent_id", "")),
                "agent_name": str(item.get("agent_name", "")),
                "session_id": str(item.get("session_id", "")),
                "status": "unknown",
                "stagnant_sec": 0,
                "error": str(output_payload.get("error", "read_output_failed")),
                "output_tail": [],
            }
            for item in sessions
        ]
        return {
            "ok": False,
            "ts": ts,
            "error": str(output_payload.get("error", "read_output_failed")),
            "summary": _summarize_agent_status(agents),
            "agents": agents,
            "source": {"sessions_ok": True, "output_ok": False},
        }

    rows = output_payload.get("results", [])
    if not isinstance(rows, list):
        rows = []
    row_by_agent: dict[str, dict[str, Any]] = {}
    for row in rows:
        if not isinstance(row, dict):
            continue
        agent_id = str(row.get("agent_id", "")).strip()
        if agent_id:
            row_by_agent[agent_id] = row

    now_ts = time.time()
    agents: list[dict[str, Any]] = []

    with _AGENT_STATUS_LOCK:
        for item in sessions:
            if not isinstance(item, dict):
                continue

            agent_id = str(item.get("agent_id", "")).strip()
            agent_name = str(item.get("agent_name", "")).strip()
            session_id = str(item.get("session_id", "")).strip()

            row = row_by_agent.get(agent_id, {})
            output_tail_raw = row.get("output", [])
            output_tail = output_tail_raw if isinstance(output_tail_raw, list) else [str(output_tail_raw)]
            output_tail = [str(line) for line in output_tail if str(line).strip()]

            error_text = str(row.get("error", "")).strip()
            has_session = bool(session_id) and "session not found" not in error_text.lower()

            fingerprint = "\n".join(output_tail[-6:])
            memory = _AGENT_STATUS_MEMORY.get(agent_id)
            if memory and memory.get("fingerprint") == fingerprint:
                last_change_ts = float(memory.get("last_change_ts", now_ts))
            else:
                last_change_ts = now_ts

            _AGENT_STATUS_MEMORY[agent_id] = {
                "fingerprint": fingerprint,
                "last_change_ts": last_change_ts,
            }

            stagnant_sec = max(0, int(now_ts - last_change_ts))
            status = classify_status(
                output_tail,
                has_session=has_session,
                stagnant_sec=stagnant_sec,
            )

            if error_text and status not in {"error", "disconnected"}:
                status = "disconnected"
            if status not in _AGENT_STATUS_NAMES:
                status = "unknown"

            agents.append(
                {
                    "agent_id": agent_id,
                    "agent_name": agent_name,
                    "session_id": session_id,
                    "status": status,
                    "stagnant_sec": stagnant_sec,
                    "error": error_text,
                    "output_tail": output_tail[-20:],
                }
            )

    return {
        "ok": True,
        "ts": ts,
        "summary": _summarize_agent_status(agents),
        "agents": agents,
        "source": {"sessions_ok": True, "output_ok": True},
    }


def _build_agent_status_snapshot(read_lines: int = 30) -> dict[str, Any]:
    table_snapshot = _build_agent_status_snapshot_from_table()
    if table_snapshot is not None:
        return table_snapshot
    return _build_agent_status_snapshot_legacy(read_lines=read_lines)


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


def _generate_default_prompt(agent_desc: str, tool_name: str, tool_desc: str, params_str: str) -> str:
    """Generate default prompt text for a tool."""
    return (f'你有一个 MCP 工具叫 "{tool_name}"。\n\n'
            f'功能: {tool_desc}\n'
            f'参数: {params_str or "无"}\n\n'
            f'使用场景: 当用户需要{tool_desc}时，调用此工具。')


def _build_system_prompt() -> dict:
    """Build the pinned system prompt describing all MCP interfaces."""
    from agents.specs import AGENT_SPECS
    from agents.factory import MISSING

    lines = ['# 多Agent编排系统 — MCP 工具总览', '',
             '本系统通过 ACP-BUS 提供以下 MCP 工具接口。',
             '每个工具以 `{agent_id}__{tool_name}` 格式命名。', '']
    for spec in AGENT_SPECS.values():
        lines.append(f'## {spec.key} — {spec.description}')
        for t in spec.tools:
            params_parts = []
            for p in t.params:
                def_str = '' if p.default is MISSING else f' = "{p.default}"'
                type_name = p.annotation.__name__ if hasattr(p.annotation, '__name__') else str(p.annotation)
                params_parts.append(f'{p.name}: {type_name}{def_str}')
            lines.append(f'- **{spec.key}__{t.name}**({", ".join(params_parts)}): {t.description}')
        lines.append('')

    lines.append('## 使用方式')
    lines.append('将以上提示词粘贴到你的 AI Agent 系统提示词中，Agent 即可通过 MCP 协议调用这些工具。')

    return {
        'key': '_system',
        'description': '多Agent编排系统 — 全量 MCP 工具提示词（置顶）',
        'is_pinned': True,
        'tools': [{
            'name': 'system_prompt',
            'description': '系统级提示词：描述所有可用 MCP 工具及使用场景',
            'params': [],
        }],
        'prompt_text': '\n'.join(lines),
    }


def _get_all_agent_specs() -> list[dict]:
    """Return all agent specs merged with PG-saved prompts, system prompt pinned at top."""
    from agents.specs import AGENT_SPECS
    from agents.factory import MISSING

    # Load saved prompts from PG
    saved_prompts = {}
    try:
        from db.postgres import fetch_all
        rows = fetch_all('SELECT agent_key, tool_name, prompt_text FROM prompts')
        for row in rows:
            saved_prompts[(row['agent_key'], row['tool_name'])] = row['prompt_text']
    except Exception:
        logger.debug("读取 prompts 表失败，回退默认提示词", exc_info=True)

    # Build agent list
    result = []
    for key, spec in AGENT_SPECS.items():
        tools = []
        for t in spec.tools:
            params = []
            for p in t.params:
                params.append({
                    'name': p.name,
                    'type': p.annotation.__name__ if hasattr(p.annotation, '__name__') else str(p.annotation),
                    'default': None if p.default is MISSING else p.default,
                })
            params_str = ', '.join(f"{p['name']}: {p['type']}" for p in params)
            saved = saved_prompts.get((spec.key, t.name))
            prompt = saved if saved else _generate_default_prompt(spec.description, t.name, t.description, params_str)
            tools.append({'name': t.name, 'description': t.description, 'params': params, 'prompt_text': prompt})
        result.append({'key': spec.key, 'description': spec.description, 'tools': tools})

    # Pin system prompt at top
    sys_prompt = _build_system_prompt()
    saved_sys = saved_prompts.get(('_system', 'system_prompt'))
    if saved_sys:
        sys_prompt['prompt_text'] = saved_sys
    result.insert(0, sys_prompt)

    return result


def _save_prompt(agent_key: str, tool_name: str, prompt_text: str) -> dict:
    """Upsert a prompt into PG."""
    from db.postgres import execute
    execute(
        """
        INSERT INTO prompts (agent_key, tool_name, prompt_text, updated_at)
        VALUES (%s, %s, %s, NOW())
        ON CONFLICT (agent_key, tool_name)
        DO UPDATE SET prompt_text = EXCLUDED.prompt_text, updated_at = NOW()
        """,
        (agent_key, tool_name, prompt_text),
    )
    return {'ok': True, 'agent_key': agent_key, 'tool_name': tool_name}


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
        <div class="stats-bar">
            <div class="stat-card"><div class="stat-label">Gateways</div><div class="stat-value green">{gateway_count}</div></div>
            <div class="stat-card"><div class="stat-label">Agents</div><div class="stat-value blue">{agent_count}</div></div>
            <div class="stat-card"><div class="stat-label">Model</div><div class="stat-value cyan" style="font-size:1rem">{html.escape(config.get('LLM_MODEL', ''))}</div></div>
            <div class="stat-card"><div class="stat-label">Agent Health</div><div id="agent-health-stat" class="stat-value amber">--</div></div>
        </div>

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
                    <h2>MCP 工具提示词</h2>
                </header>
                <div class="card-body">
                    <p style="font-size:0.82rem;color:var(--text-secondary);margin-bottom:16px">
                        每个 Agent 的 MCP 工具提示词。点击复制后可直接粘贴给 AI Agent，也可在线编辑后再复制。
                    </p>
                    <div id="prompts-list"><div class="approval-empty">加载中...</div></div>
                </div>
            </div>
        </div>
    </main>
</div>

<script>
window.__SSE_SYNC_SEC = {sse_sync_sec};
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

        elif path == "/api/prompts":
            try:
                agents = _get_all_agent_specs()
                total_tools = sum(len(a['tools']) for a in agents)
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "agents": agents, "total_tools": total_tools}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

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

    server = http.server.ThreadingHTTPServer(("0.0.0.0", port), DashboardHandler)
    logger.info("Dashboard v2 已启动: http://localhost:%s", port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Dashboard 已停止")
        server.shutdown()


if __name__ == "__main__":
    main()
