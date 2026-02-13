"""配置管理 Web 面板 (v2 — Dark OLED Design)

启动: python3 dashboard.py
访问: http://localhost:8080
"""

import json
import logging
import os
import sys
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
    save_command_card,
    list_command_card_versions,
    rollback_command_card,
    set_command_card_enabled,
    delete_command_cards,
    delete_prompt_templates,
    save_task_ack,
    list_task_acks,
    update_task_ack_status,
    delete_task_acks,
    save_task_dag,
    list_task_dags,
    get_task_dag_detail,
    save_dag_node,
    update_dag_node_status,
    delete_task_dags,
)
from config.prompt_template_presets import list_common_prompt_templates
from agent_monitor import patrol_agents_once, run_patrol_cycle
from agent_status_store import (
    query_agent_status as query_agent_status_rows,
    upsert_agent_status as upsert_agent_status_row,
)
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
from ai_log import query_ai_logs, list_ai_filter_values
from db.postgres import fetch_one
from system_log import query_logs as query_system_logs, list_filter_values as list_system_filter_values
from topology_approval import approve_approval, is_valid_approval_id, list_approvals, reject_approval

ENV_FILE = Path(__file__).parent / ".env"
STATIC_DIR = Path(__file__).parent / "static"
logger = logging.getLogger(__name__)

_AGENT_STATUS_NAMES = ("running", "idle", "stuck", "error", "disconnected", "unknown")
_AGENT_STATUS_MEMORY: dict[str, dict[str, Any]] = {}
_AGENT_MONITOR_THREAD: Optional[threading.Thread] = None
_AGENT_MONITOR_LOCK = threading.Lock()
_AGENT_MONITOR_STOP_EVENT = threading.Event()

# ── LLM 配置 (全局, 可从 UI 修改) ──
_llm_config: dict[str, str] = {
    "api_key": os.getenv("AGENTCTL_LLM_API_KEY", "sk-83e3fa49d77523d0004dde35ef34f577a8c89f9ebaec8bef"),
    "base_url": os.getenv("AGENTCTL_LLM_BASE_URL", "https://api.gpteamservices.com/v1"),
    "model": os.getenv("AGENTCTL_LLM_MODEL", "gpt-5.2"),
    "reasoning_effort": os.getenv("AGENTCTL_LLM_REASONING_EFFORT", "high"),
    "timeout": os.getenv("AGENTCTL_LLM_TIMEOUT", "30"),
    "poll_interval": os.getenv("AGENTCTL_POLL_INTERVAL", "8"),
    "cooldown_sec": os.getenv("AGENTCTL_COOLDOWN_SEC", "60"),
    "master_agent_id": os.getenv("AGENTCTL_MASTER_AGENT_ID", "A0-master"),
}

# ── Lifecycle 监控状态 (全局) ──
_lifecycle_running: bool = False
_lifecycle_state: dict[str, Any] = {"cycles": 0, "notifications_sent": 0, "errors": 0, "agents": {}}
_lifecycle_timeline: list[dict[str, Any]] = []
_lifecycle_thread: Optional[threading.Thread] = None
_lifecycle_stop_event = threading.Event()

# ── 编排引擎 (run.py integration) ──
_orchestration_lock = threading.Lock()
_orchestration_state: dict[str, Any] = {
    "status": "idle",       # idle | running | done | error
    "task": "",
    "started_at": "",
    "finished_at": "",
    "elapsed_sec": 0.0,
    "result": "",
    "error": "",
    "trace_id": "",
}


def _run_orchestration_worker(task_desc: str) -> None:
    """后台线程: 运行编排引擎 (来自 run.py 的 async run 函数)"""
    global _orchestration_state
    import asyncio as _aio
    try:
        from run import run as _run_task
        result = _aio.run(_run_task(task_desc))
        with _orchestration_lock:
            _orchestration_state["status"] = "done"
            _orchestration_state["finished_at"] = datetime.now(timezone.utc).isoformat()
            _orchestration_state["elapsed_sec"] = round(
                (datetime.now(timezone.utc) - datetime.fromisoformat(_orchestration_state["started_at"])).total_seconds(), 2
            )
            _orchestration_state["result"] = str(result.get("final_answer", ""))[:4000] if result else ""
        _publish_dashboard_event("sync", {
            "scope": ["orchestration", "audit"],
            "reason": "orchestration_done",
            "task": task_desc[:120],
        })
    except Exception as exc:
        with _orchestration_lock:
            _orchestration_state["status"] = "error"
            _orchestration_state["finished_at"] = datetime.now(timezone.utc).isoformat()
            _orchestration_state["error"] = str(exc)[:2000]
            try:
                _orchestration_state["elapsed_sec"] = round(
                    (datetime.now(timezone.utc) - datetime.fromisoformat(_orchestration_state["started_at"])).total_seconds(), 2
                )
            except Exception:
                pass
        _publish_dashboard_event("sync", {
            "scope": ["orchestration", "audit"],
            "reason": "orchestration_error",
            "task": task_desc[:120],
            "error": str(exc)[:200],
        })


def _lifecycle_add_timeline(icon: str, text: str) -> None:
    """添加一条时间线记录"""
    _lifecycle_timeline.append({
        "ts": datetime.now(timezone.utc).isoformat(),
        "icon": icon,
        "text": text,
    })
    # 最多保留 200 条
    while len(_lifecycle_timeline) > 200:
        _lifecycle_timeline.pop(0)


def _lifecycle_worker(dry_run: bool = False) -> None:
    """后台线程: 周期性采集数据 → 推送给 GPT-5.2 → 决策通知"""
    import httpx

    global _lifecycle_running, _lifecycle_state
    _lifecycle_running = True
    _lifecycle_add_timeline("🔭", "监控已启动" + (" (Dry Run)" if dry_run else ""))
    logger.info("lifecycle worker started, dry_run=%s", dry_run)

    # 从全局 LLM 配置读取 (可通过 UI 实时修改)
    poll_interval = max(3, int(_llm_config.get("poll_interval", "8")))
    llm_api_key = _llm_config.get("api_key", "")
    llm_base_url = _llm_config.get("base_url", "https://api.gpteamservices.com/v1")
    llm_model = _llm_config.get("model", "gpt-5.2")
    reasoning_effort = _llm_config.get("reasoning_effort", "high")
    llm_timeout = max(5, int(_llm_config.get("timeout", "30")))

    # 冷却: agent_id → last_notify_ts
    cooldown: dict[str, float] = {}
    cooldown_sec = max(10, float(_llm_config.get("cooldown_sec", "60")))

    while not _lifecycle_stop_event.is_set():
        try:
            # ── 采集数据 ──
            agent_payload = _build_agent_status_snapshot(read_lines=30)
            agents = list(agent_payload.get("agents", []))

            # 补充 roster 注册的 agent（可能无 iTerm session）
            try:
                from pathlib import Path as _P
                reg_path = _P(__file__).resolve().parent / "data" / "agent_registry.json"
                if reg_path.exists():
                    registry = json.loads(reg_path.read_text("utf-8"))
                    iterm_ids = {str(a.get("agent_id", "")).strip() for a in agents if isinstance(a, dict)}
                    for aid, info in registry.items():
                        if aid not in iterm_ids:
                            agents.append({
                                "agent_id": aid,
                                "agent_name": info.get("agent_name", aid),
                                "status": "registered",
                                "output_tail": [],
                                "source": "registry",
                                "skills": info.get("skills", []),
                            })
            except Exception:
                pass
            task_ack_list = []
            dag_detail_list = []
            try:
                task_ack_list = list_task_acks(limit=100)
            except Exception:
                pass
            try:
                raw_dags = list_task_dags(limit=50)
                for dag in raw_dags:
                    dag_id = str(dag.get("dag_id", "")).strip()
                    if dag_id:
                        try:
                            detail = get_task_dag_detail(dag_id)
                            if detail:
                                dag_detail_list.append(detail)
                        except Exception:
                            pass
            except Exception:
                pass

            # ── 对每个子 Agent 调 GPT-5.2 做决策 ──
            master_id = _llm_config.get("master_agent_id", "A0-master")
            for agent in agents:
                if not isinstance(agent, dict):
                    continue
                agent_id = str(agent.get("agent_id", "")).strip()
                if not agent_id or agent_id == master_id:
                    continue
                agent_name = str(agent.get("agent_name", agent_id))
                agent_status = str(agent.get("status", "unknown"))
                output_tail = agent.get("output_tail")
                term_lines = []
                if isinstance(output_tail, list):
                    term_lines = [str(l) for l in output_tail[-30:]]
                elif isinstance(output_tail, str):
                    term_lines = output_tail.strip().splitlines()[-30:]

                # 找该 agent 相关的 ack
                agent_ack = None
                for ack in task_ack_list:
                    if isinstance(ack, dict) and str(ack.get("agent_id", "")).strip() == agent_id:
                        agent_ack = ack
                        break

                # 构建 LLM prompt
                context = f"## Agent: {agent_id} ({agent_name})\n## 状态: {agent_status}\n"
                if term_lines:
                    context += f"## 终端输出 (最后 {len(term_lines)} 行):\n```\n" + "\n".join(term_lines) + "\n```\n"
                if agent_ack:
                    context += f"## Task-Ack: {json.dumps(agent_ack, ensure_ascii=False)}\n"

                llm_prompt = (
                    "你是多Agent编排系统的监控助手。分析子Agent是否完成任务。\n"
                    "返回严格JSON: {\"completed\": bool, \"confidence\": 0-1, \"status\": \"completed\"|\"running\"|\"errored\"|\"stalled\", \"reason\": \"简短理由\"}\n\n"
                    + context
                )

                # 调用 LLM
                judgment = {"completed": False, "confidence": 0.0, "status": "running", "reason": "LLM 未响应"}
                try:
                    with httpx.Client(timeout=llm_timeout) as client:
                        resp = client.post(
                            f"{llm_base_url}/responses",
                            headers={"Authorization": f"Bearer {llm_api_key}", "Content-Type": "application/json"},
                            content=json.dumps({"model": llm_model, "input": llm_prompt, "reasoning": {"effort": reasoning_effort}}, ensure_ascii=False),
                        )
                        if resp.status_code == 200:
                            result = resp.json()
                            out_text = ""
                            for item in result.get("output", []):
                                if item.get("type") == "message":
                                    for c in item.get("content", []):
                                        if c.get("type") == "output_text":
                                            out_text = c.get("text", "")
                                            break
                                    break
                            if out_text:
                                # 提取 JSON
                                jstr = out_text.strip()
                                if "```" in jstr:
                                    for part in jstr.split("```"):
                                        cl = part.strip()
                                        if cl.startswith("json"):
                                            cl = cl[4:].strip()
                                        if cl.startswith("{"):
                                            jstr = cl
                                            break
                                start = jstr.find("{")
                                end = jstr.rfind("}") + 1
                                if start >= 0 and end > start:
                                    judgment = json.loads(jstr[start:end])
                except Exception as e:
                    judgment["reason"] = f"LLM 错误: {str(e)[:60]}"

                # 更新 agent 状态
                _lifecycle_state["agents"][agent_id] = {
                    "agent_id": agent_id,
                    "agent_name": agent_name,
                    "runtime_status": agent_status,
                    "completed": judgment.get("completed", False),
                    "confidence": judgment.get("confidence", 0.0),
                    "llm_status": judgment.get("status", "unknown"),
                    "reason": judgment.get("reason", ""),
                    "output_tail": "\n".join(term_lines[-3:]) if term_lines else "",
                }

                # 通知逻辑
                should_notify = judgment.get("completed") or judgment.get("status") in ("errored",)
                if should_notify:
                    now = time.time()
                    last = cooldown.get(agent_id, 0.0)
                    if now - last < cooldown_sec:
                        continue
                    cooldown[agent_id] = now

                    status_text = "完成" if judgment.get("completed") else "异常"
                    reason = judgment.get("reason", "")
                    _lifecycle_add_timeline(
                        "📢" if not dry_run else "🔇",
                        f"{agent_id} {status_text}: {reason}" + (" [DRY RUN]" if dry_run else ""),
                    )
                    _lifecycle_state["notifications_sent"] = _lifecycle_state.get("notifications_sent", 0) + 1

                    # 实际发送通知
                    if not dry_run:
                        try:
                            sessions = _list_live_sessions() or []
                            master_session_id = ""
                            for s in sessions:
                                if str(s.get("badge", "")).strip() == master_id or str(s.get("agent_label", "")).strip() == master_id:
                                    master_session_id = str(s.get("session_id", "")).strip()
                                    break
                            if master_session_id:
                                notify_text = f"子 Agent {agent_id} ({agent_name}) 的任务已{status_text}。\n原因: {reason}\n请检查结果并安排下一步。"
                                send_to_session(session_id=master_session_id, text=notify_text)
                                _lifecycle_add_timeline("✅", f"已通知主 Agent ({master_id})")
                        except Exception as e:
                            _lifecycle_add_timeline("❌", f"通知失败: {str(e)[:60]}")
                            _lifecycle_state["errors"] = _lifecycle_state.get("errors", 0) + 1

            _lifecycle_state["cycles"] = _lifecycle_state.get("cycles", 0) + 1

        except Exception as e:
            _lifecycle_state["errors"] = _lifecycle_state.get("errors", 0) + 1
            logger.error("lifecycle worker error: %s", e, exc_info=True)

        _lifecycle_stop_event.wait(poll_interval)

    _lifecycle_running = False
    _lifecycle_add_timeline("⏹", "监控已停止")
    logger.info("lifecycle worker stopped")

# 配置项定义
CONFIG_SCHEMA = [
    {
        "group": "LLM 设置",
        "icon": "brain",
        "items": [
            {"key": "OPENAI_API_KEY", "label": "API Key", "type": "password", "desc": "OpenAI / 第三方 API Key"},
            {"key": "OPENAI_BASE_URL", "label": "API Base URL", "type": "text", "desc": "留空使用 OpenAI 官方"},
            {"key": "LLM_MODEL", "label": "模型", "type": "text", "desc": "如 gpt-4o, deepseek-chat"},
            {"key": "OPENAI_USE_PREVIOUS_RESPONSE_ID", "label": "串联上下文 previous_response_id", "type": "select",
             "options": ["0", "1"], "desc": "0=关闭（推荐第三方网关），1=开启多轮串联"},
            {"key": "OPENAI_RESPONSES_CONVERSATION_ID", "label": "持久会话 conversation_id", "type": "text",
             "desc": "可选，会话 ID（如 conv_xxx）"},
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
            {"key": "TG_WATCHDOG_INCLUDE_MASTER", "label": "看门狗包含主Agent", "type": "select", "options": ["1", "0"], "desc": "1=定时唤醒主 Agent，0=仅唤醒子 Agent"},
        ],
    },
]

DEFAULTS = {
    "OPENAI_API_KEY": "",
    "OPENAI_BASE_URL": "",
    "LLM_MODEL": "gpt-4o",
    "OPENAI_USE_PREVIOUS_RESPONSE_ID": "0",
    "OPENAI_RESPONSES_CONVERSATION_ID": "",
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
    "TG_MASTER_TAB_NAME": "主agent",
    "TG_WATCHDOG_INTERVAL": "120",
    "TG_WATCHDOG_PROMPT": "请继续执行当前任务。如果已完成，请汇报结果。",
    "TG_WATCHDOG_INCLUDE_MASTER": "1",
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
    ("ailog", "AI 日志", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="4" y="4" width="16" height="16" rx="3"/><circle cx="9" cy="10" r="1.2"/><circle cx="15" cy="10" r="1.2"/><path d="M8 15c1 .9 2.3 1.3 4 1.3s3-.4 4-1.3"/><path d="M12 4V2"/><path d="M4 12H2"/><path d="M22 12h-2"/></svg>'),
    ("commands", "命令卡", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M4 6h16M4 12h16M4 18h10"/><circle cx="18" cy="18" r="2"/></svg>'),
    ("prompts", "提示词", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/></svg>'),
    ("acks", "任务管理", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M22 11.08V12a10 10 0 11-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>'),
    ("dags", "DAG管理", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="5" cy="6" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="12" cy="18" r="2"/><path d="M6.5 7.5L10.5 10.5M17.5 7.5L13.5 10.5M12 14v2"/></svg>'),
    ("traces", "任务追踪", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M3 12h5l2-6 4 12 2-6h5"/></svg>'),
    ("monitor", "Agent 监控", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>'),
    ("lifecycle", "生命周期", '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/><path d="M22 12h-2M4 12H2M12 2v2M12 20v2"/></svg>'),
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


_MASTER_SESSION_NAME_TAG = "主agnet"


def _list_sessions_with_master() -> dict[str, Any]:
    """Wrap list_iterm_agent_sessions and also detect master by session name tag."""
    result = list_iterm_agent_sessions()
    if not result.get("ok"):
        return result

    # Check if master is already in sessions
    sessions = result.get("sessions", [])
    known_ids = {s.get("agent_id", "") for s in sessions}
    if "master" in known_ids:
        return result

    # Scan live sessions for master by session_name tag
    try:
        _, live_sessions = _list_live_sessions()
        for ls in live_sessions:
            session_name = str(ls.get("session_name", "") or "").strip()
            if _MASTER_SESSION_NAME_TAG in session_name:
                sessions.insert(0, {
                    "index": 0,
                    "agent_id": "master",
                    "agent_name": "主控 Agent",
                    "session_id": str(ls.get("session_id", "")),
                })
                result["sessions"] = sessions
                break
    except Exception:
        logger.debug("detect master session via name tag failed", exc_info=True)

    return result


def _build_agent_status_snapshot(read_lines: int = 30) -> dict[str, Any]:
    """Build agent status snapshot directly via iTerm API."""
    try:
        snapshot = patrol_agents_once(
            list_sessions_func=_list_sessions_with_master,
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


def query_agent_status(limit: int = 200) -> list[dict[str, Any]]:
    """Read latest agent status rows from PostgreSQL."""
    return query_agent_status_rows(limit=limit)


def _build_agent_status_payload_from_rows(rows: list[dict[str, Any]]) -> dict[str, Any]:
    agents, ts = _normalize_agent_status_rows(rows)
    return {
        "ok": True,
        "ts": ts or datetime.now(timezone.utc).isoformat(),
        "summary": _summarize_agent_status(agents),
        "agents": agents,
        "source": {"db_ok": True},
    }


def _publish_agent_status_event(payload: dict[str, Any]) -> None:
    _publish_dashboard_event("agent_status", payload)


def _agent_monitor_worker() -> None:
    interval_sec = _safe_int(os.getenv("DASHBOARD_SSE_SYNC_SEC", "5"), 5, 1, 60)
    read_lines = _safe_int(os.getenv("DASHBOARD_AGENT_STATUS_READ_LINES", "30"), 30, 1, 200)
    while not _AGENT_MONITOR_STOP_EVENT.is_set():
        try:
            run_patrol_cycle(
                list_sessions_func=_list_sessions_with_master,
                read_output_func=read_iterm_output,
                upsert_status_func=upsert_agent_status_row,
                publish_event_func=_publish_dashboard_event,
                read_lines=read_lines,
                status_memory=_AGENT_STATUS_MEMORY,
            )
        except Exception:
            logger.debug("agent monitor tick failed", exc_info=True)
        if _AGENT_MONITOR_STOP_EVENT.wait(interval_sec):
            break


def ensure_agent_monitor_started() -> None:
    global _AGENT_MONITOR_THREAD
    with _AGENT_MONITOR_LOCK:
        if _AGENT_MONITOR_THREAD is not None and _AGENT_MONITOR_THREAD.is_alive():
            return
        _AGENT_MONITOR_STOP_EVENT.clear()
        thread = threading.Thread(
            target=_agent_monitor_worker,
            name="dashboard-agent-monitor",
            daemon=True,
        )
        thread.start()
        _AGENT_MONITOR_THREAD = thread


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
    """Build the pinned system prompt describing all MCP interfaces.
    """
    tool_index: list[tuple[str, str]] = [
        ("iterm", "iTerm 会话统一管理（list/send/read/clean/unregister/clear_all）"),
        ("shared_file", "共享文件管理（write/read/list/delete）"),
        ("interaction", "Agent 交互记录（register/roster/create/list/review）"),
        ("prompt_template", "提示词模板管理（save/get/list/toggle）"),
        ("command_card", "命令卡管理与执行（save/get/list/toggle/prepare/review/exec_run/exec）"),
        ("db", "数据库读写（query/execute）"),
        ("task", "任务管理（create/list/get/update/assign/ready/progress/cancel）"),
        ("approval", "审批流（request/respond/list/get）"),
        ("lock", "资源锁（acquire/release/list/force_release）"),
        ("agent_watchdog", "看门狗（start/stop/status）"),
        ("orchestration_tui", "Codex TUI 编排状态总线（begin/update/end/warning/snapshot/events/reset）"),
    ]
    tool_lines = "\n".join([f"- **{name}**: {desc}" for name, desc in tool_index])
    prompt_text = (
        "# 多Agent编排系统 — 系统级工具\n\n"
        "以下为当前系统级工具索引（统一 action 风格）。\n\n"
        "## 系统级工具\n\n"
        f"{tool_lines}\n\n"
        "说明：优先使用 action 参数在单工具内切换操作。"
    )

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
    description = str(data.get("description", "") or "").strip()
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
        description=description,
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


def _save_command_card_entry(data: dict[str, Any], updated_by: str = "dashboard") -> dict[str, Any]:
    card_key = str(data.get("card_key", "") or "").strip()
    title = str(data.get("title", "") or "").strip()
    description = str(data.get("description", "") or "").strip()
    command_template = str(data.get("command_template", "") or "").strip()
    risk_level = str(data.get("risk_level", "normal") or "normal").strip().lower() or "normal"
    enabled = _safe_bool(data.get("enabled", True), default=True)

    if not card_key:
        raise ValueError("card_key 不能为空")
    if not title:
        raise ValueError("title 不能为空")
    if not command_template:
        raise ValueError("command_template 不能为空")

    if risk_level not in {"low", "normal", "high", "critical"}:
        raise ValueError("risk_level 非法，必须是 low/normal/high/critical")

    args_schema = _parse_json_object(data.get("args_schema", {}), "args_schema")

    return save_command_card(
        card_key=card_key,
        title=title,
        command_template=command_template,
        description=description,
        args_schema=args_schema,
        risk_level=risk_level,
        enabled=enabled,
        updated_by=str(updated_by or "dashboard").strip() or "dashboard",
    )


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
        agent_meta = gw_config.get("agent_meta", {})

        # Build compact agent rows
        agents_list = ""
        for a in gw_config["agents"].keys():
            meta = agent_meta.get(a, {})
            display_name = html.escape(str(meta.get("name", a)))
            agent_id_esc = html.escape(str(a), quote=True)

            # Capabilities tags
            caps = meta.get("capabilities", [])
            caps_html = "".join(
                f'<span class="agent-cap-tag">{html.escape(str(c))}</span>'
                for c in caps[:5]
            ) if caps else ""

            # Dependencies
            deps = meta.get("depends_on", [])
            deps_html = ""
            if deps:
                deps_text = " · ".join(html.escape(str(d)) for d in deps)
                deps_html = f'<span class="agent-dep-info">← {deps_text}</span>'

            agents_list += (
                f'<div class="agent-row agent-chip-status-unknown" data-agent-id="{agent_id_esc}">'
                f'<div class="agent-row-top">'
                f'<span class="agent-status-dot"></span>'
                f'<span class="agent-chip-name">{html.escape(str(a))}</span>'
                f'<span class="agent-row-label">{display_name}</span>'
                f'{deps_html}'
                f'<span class="agent-chip-state">unknown</span>'
                f'</div>'
                + (f'<div class="agent-caps">{caps_html}</div>' if caps_html else '')
                + '</div>'
            )

        # Gateway capabilities
        gw_caps = gw_config.get("capabilities", [])
        gw_caps_html = ""
        if gw_caps:
            gw_caps_html = '<div class="gw-caps">' + "".join(
                f'<span class="gw-cap-tag">{html.escape(str(c))}</span>'
                for c in gw_caps
            ) + '</div>'

        # Gateway description
        gw_desc = gw_config.get("description", "")
        gw_desc_html = ""
        if gw_desc:
            gw_desc_html = f'<div class="gw-desc">{html.escape(str(gw_desc))}</div>'

        gw_cards += f'''<div class="gw-card" style="--accent-color:{color}; border-left-color:{color}">
            <div class="gw-header-row">
                <div>
                    <h3 class="gw-name">{html.escape(str(gw_config['name']))}</h3>
                    <span class="gw-id">{html.escape(str(gw_name))}</span>
                </div>
                <span class="gw-agent-count">{len(gw_config['agents'])}</span>
            </div>
            {gw_desc_html}
            {gw_caps_html}
            <div class="gw-agents">{agents_list}</div>
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
                    <div id="agent-health-stat" style="display:none"></div>
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

        <!-- AI Log Page -->
        <div id="page-ailog" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><rect x="4" y="4" width="16" height="16" rx="3"/><circle cx="9" cy="10" r="1.2"/><circle cx="15" cy="10" r="1.2"/><path d="M8 15c1 .9 2.3 1.3 4 1.3s3-.4 4-1.3"/></svg>
                    <h2>AI 日志</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar">
                        <select id="ai-level" class="input"><option value="">级别</option></select>
                        <select id="ai-logger" class="input"><option value="">模块</option></select>
                        <select id="ai-category" class="input"><option value="">类别</option></select>
                        <select id="ai-endpoint" class="input"><option value="">端点</option></select>
                        <select id="ai-status" class="input"><option value="">状态码</option></select>
                        <input type="text" id="ai-keyword" class="input" placeholder="搜索关键词..." style="max-width:200px">
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadAiLogs()">刷新</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="exportAiLogs()">导出</button>
                    </div>
                    <table class="log-table"><thead><tr>
                        <th>时间</th><th>级别</th><th>类别</th><th>模块</th><th>端点</th><th>状态</th><th>消息</th>
                    </tr></thead><tbody id="ai-tbody"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Command Cards Page -->
        <div id="page-commands" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M8 9h8M8 13h8M8 17h5"/></svg>
                    <h2>命令卡管理</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar" style="gap:8px;flex-wrap:wrap">
                        <input type="text" id="cmd-search" class="input" placeholder="搜索 key / 标题 / 说明 / 命令..." style="max-width:300px" oninput="loadCommandCards()">
                        <select id="cmd-risk-filter" class="input" style="max-width:120px" onchange="loadCommandCards()">
                            <option value="">全部风险</option>
                            <option value="low">low</option>
                            <option value="normal">normal</option>
                            <option value="high">high</option>
                            <option value="critical">critical</option>
                        </select>
                        <label style="display:inline-flex;align-items:center;gap:6px;color:var(--text-secondary);font-size:0.8rem">
                            <input type="checkbox" id="cmd-enabled-only" onchange="loadCommandCards()"> 仅启用
                        </label>
                        <button type="button" class="btn btn-sm btn-primary" onclick="openCommandCreatePopup()">新建命令卡</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="openCommandPastePopup()">快速粘贴</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadCommandCards()">刷新</button>
                        <button type="button" class="btn btn-sm btn-danger" onclick="deleteSelectedCommandCards()">删除</button>
                    </div>
                    <table class="log-table" id="cmd-card-table">
                        <thead><tr>
                            <th style="width:34px;text-align:center"><input type="checkbox" id="cmd-card-select-all" onchange="toggleCommandCardSelectAll(this.checked)"></th>
                            <th style="width:240px">卡片 Key</th>
                            <th style="width:180px">标题</th>
                            <th style="width:100px">风险</th>
                            <th style="width:90px">状态</th>
                            <th style="width:140px">更新时间</th>
                            <th style="width:180px">最近执行</th>
                            <th>说明</th>
                            <th style="width:280px;text-align:center">操作</th>
                        </tr></thead>
                        <tbody id="cmd-card-tbody"></tbody>
                    </table>
                    <div id="cmd-card-empty" class="approval-empty">加载中...</div>
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

            <div id="cmd-popup" class="command-popup" style="display:none">
                <div class="command-popup-header">
                    <span id="cmd-popup-title" class="command-popup-title"></span>
                    <button class="command-popup-close" onclick="closeCommandPopup()">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    </button>
                </div>
                <div class="command-popup-meta">
                    <input type="text" id="cmd-popup-key" class="input" placeholder="card_key，例如 launch.wjboot.workspace">
                    <input type="text" id="cmd-popup-title-input" class="input" placeholder="标题">
                    <select id="cmd-popup-risk" class="input">
                        <option value="low">low</option>
                        <option value="normal" selected>normal</option>
                        <option value="high">high</option>
                        <option value="critical">critical</option>
                    </select>
                    <label style="display:inline-flex;align-items:center;gap:6px;color:var(--text-secondary);font-size:0.8rem">
                        <input type="checkbox" id="cmd-popup-enabled" checked> 启用
                    </label>
                </div>
                <textarea id="cmd-popup-desc" class="command-popup-desc" placeholder="说明（可选）"></textarea>
                <textarea id="cmd-popup-args-schema" class="command-popup-schema" placeholder='参数定义(JSON对象)，例如 {{"tabs":"number","layout":"string"}}'></textarea>
                <textarea id="cmd-popup-textarea" class="command-popup-textarea" placeholder="命令模板"></textarea>
                <div class="command-popup-actions">
                    <span class="prompt-shortcut-tip">⌘/Ctrl+S 保存 · Esc 关闭</span>
                    <span class="copy-ok" id="cmd-popup-copy-ok">已复制</span>
                    <button class="btn btn-sm btn-secondary" id="cmd-popup-fullscreen-btn" onclick="toggleCommandPopupFullscreen()">全屏编辑</button>
                    <button class="btn btn-sm btn-secondary" onclick="copyCommandPopup()">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                        复制
                    </button>
                    <button class="btn btn-sm btn-primary" id="cmd-popup-save" onclick="saveCommandPopup()">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/></svg>
                        保存
                    </button>
                    <button class="btn btn-sm btn-secondary" onclick="saveCommandPopup(true)">保存并关闭</button>
                </div>
            </div>

            <div id="command-version-popup" class="command-popup command-version-popup" style="display:none">
                <div class="command-popup-header">
                    <span id="command-version-popup-title" class="command-popup-title">命令卡版本历史</span>
                    <button class="command-popup-close" onclick="closeCommandVersionPopup()">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    </button>
                </div>
                <div class="command-version-toolbar">
                    <span id="command-version-key" class="prompt-shortcut-tip" style="margin-right:0">-</span>
                    <button class="btn btn-sm btn-secondary" onclick="loadCommandVersions()">刷新版本</button>
                </div>
                <div class="command-version-body">
                    <table class="log-table">
                        <thead>
                            <tr>
                                <th style="width:84px">版本ID</th>
                                <th style="width:160px">归档时间</th>
                                <th style="width:160px">来源更新时间</th>
                                <th style="width:120px">更新人</th>
                                <th style="width:80px">风险</th>
                                <th style="width:80px">启用</th>
                                <th>标题</th>
                                <th style="width:120px;text-align:center">操作</th>
                            </tr>
                        </thead>
                        <tbody id="command-version-tbody"></tbody>
                    </table>
                    <div id="command-version-empty" class="approval-empty" style="display:none">暂无历史版本</div>
                </div>
                <div class="command-popup-actions">
                    <span class="prompt-shortcut-tip">回滚会自动生成一个新版本（可继续回滚）</span>
                    <button class="btn btn-sm btn-secondary" onclick="closeCommandVersionPopup()">关闭</button>
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
                        <button type="button" class="btn btn-sm btn-danger" onclick="deleteSelectedPrompts()">删除</button>
                    </div>
                    <table class="log-table" id="prompt-table">
                        <thead><tr>
                            <th style="width:34px;text-align:center"><input type="checkbox" id="prompt-select-all" onchange="togglePromptSelectAll(this.checked)"></th>
                            <th style="width:220px">模板 Key</th>
                            <th style="width:160px">标题</th>
                            <th style="width:160px">能力说明</th>
                            <th style="width:120px">Agent/Tool</th>
                            <th style="width:140px">标签</th>
                            <th style="width:80px">状态</th>
                            <th style="width:130px">更新时间</th>
                            <th style="width:190px;text-align:center">操作</th>
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
                    <input type="text" id="prompt-popup-description" class="input" placeholder="能力说明（备注这个提示词的能力）" style="grid-column:1/3">
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

            <div id="prompt-version-popup" class="prompt-popup prompt-version-popup" style="display:none">
                <div class="prompt-popup-header">
                    <span id="prompt-version-popup-title" class="prompt-popup-title">提示词版本历史</span>
                    <button class="prompt-popup-close" onclick="closePromptVersionPopup()">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    </button>
                </div>
                <div class="prompt-version-toolbar">
                    <span id="prompt-version-key" class="prompt-shortcut-tip" style="margin-right:0">-</span>
                    <button class="btn btn-sm btn-secondary" onclick="loadPromptVersions()">刷新版本</button>
                </div>
                <div class="prompt-version-body">
                    <table class="log-table">
                        <thead>
                            <tr>
                                <th style="width:84px">版本ID</th>
                                <th style="width:160px">归档时间</th>
                                <th style="width:160px">来源更新时间</th>
                                <th style="width:120px">更新人</th>
                                <th style="width:80px">启用</th>
                                <th>标题</th>
                                <th style="width:120px;text-align:center">操作</th>
                            </tr>
                        </thead>
                        <tbody id="prompt-version-tbody"></tbody>
                    </table>
                    <div id="prompt-version-empty" class="approval-empty" style="display:none">暂无历史版本</div>
                </div>
                <div class="prompt-popup-actions">
                    <span class="prompt-shortcut-tip">回滚会自动生成一个新版本（可继续回滚）</span>
                    <button class="btn btn-sm btn-secondary" onclick="closePromptVersionPopup()">关闭</button>
                </div>
            </div>
        </div>

        <!-- ACK Management Page -->
        <div id="page-acks" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M22 11.08V12a10 10 0 11-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
                    <h2>任务管理</h2>
                </header>
                <div class="card-body">
                    <div id="ack-stats" style="display:flex;gap:12px;margin-bottom:14px;flex-wrap:wrap"></div>
                    <div class="log-toolbar">
                        <input type="text" id="ack-search" class="input" placeholder="搜索 task_id / 标题 / 描述 / project_id ..." style="min-width:200px" onkeydown="if(event.key==='Enter')loadTaskAcks()">
                        <select id="ack-status-filter" class="input" style="min-width:110px" onchange="loadTaskAcks()">
                            <option value="">全部状态</option>
                            <option value="pending">pending</option>
                            <option value="in_progress">in_progress</option>
                            <option value="done">done</option>
                            <option value="failed">failed</option>
                            <option value="cancelled">cancelled</option>
                        </select>
                        <select id="ack-priority-filter" class="input" style="min-width:100px" onchange="loadTaskAcks()">
                            <option value="">全部优先级</option>
                            <option value="critical">critical</option>
                            <option value="high">high</option>
                            <option value="normal">normal</option>
                            <option value="low">low</option>
                        </select>
                        <button type="button" class="btn btn-sm btn-primary" onclick="openAckPopup(-1)">新建任务</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadTaskAcks()">刷新</button>
                    </div>
                    <table class="log-table" id="ack-table">
                        <thead><tr>
                            <th style="width:100px">Task ID</th>
                            <th style="min-width:160px">标题</th>
                            <th style="width:140px">Project ID</th>
                            <th style="width:80px">Assignee</th>
                            <th style="width:70px">优先级</th>
                            <th style="width:80px">状态</th>
                            <th style="width:90px">进度</th>
                            <th style="width:120px">依赖</th>
                            <th style="width:130px">更新时间</th>
                            <th style="max-width:180px">结果摘要</th>
                        </tr></thead>
                        <tbody id="ack-tbody"></tbody>
                    </table>
                    <div id="ack-empty" class="approval-empty">加载中...</div>
                </div>
            </div>
        </div>
        <div id="ack-popup" class="prompt-popup" style="display:none">
            <div class="prompt-popup-header">
                <span id="ack-popup-title" class="prompt-popup-title">新建 ACK</span>
                <button class="prompt-popup-close" onclick="document.getElementById('ack-popup').style.display='none'">&times;</button>
            </div>
            <div class="prompt-popup-body" style="display:flex;flex-direction:column;gap:10px;padding:16px">
                <label style="font-size:.78rem;color:var(--text-secondary)">ACK Key</label>
                <input type="text" id="ack-popup-key" class="input" placeholder="唯一标识">
                <label style="font-size:.78rem;color:var(--text-secondary)">标题</label>
                <input type="text" id="ack-popup-title-input" class="input" placeholder="任务标题">
                <label style="font-size:.78rem;color:var(--text-secondary)">描述</label>
                <textarea id="ack-popup-desc" class="input" style="min-height:60px" placeholder="详细描述"></textarea>
                <div style="display:flex;gap:10px">
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">执行方</label>
                    <input type="text" id="ack-popup-assigned" class="input" placeholder="agent key"></div>
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">请求方</label>
                    <input type="text" id="ack-popup-requested" class="input" value="dashboard"></div>
                </div>
                <div style="display:flex;gap:10px">
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">优先级</label>
                    <select id="ack-popup-priority" class="input"><option value="low">low</option><option value="normal" selected>normal</option><option value="high">high</option><option value="critical">critical</option></select></div>
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">状态</label>
                    <select id="ack-popup-status" class="input"><option value="pending">pending</option><option value="acked">acked</option><option value="in_progress">in_progress</option><option value="done">done</option><option value="failed">failed</option><option value="cancelled">cancelled</option></select></div>
                </div>
                <div style="display:flex;gap:10px">
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">进度 (0-100)</label>
                    <input type="number" id="ack-popup-progress" class="input" min="0" max="100" value="0"></div>
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">截止时间 (可选)</label>
                    <input type="datetime-local" id="ack-popup-due" class="input"></div>
                </div>
                <label style="font-size:.78rem;color:var(--text-secondary)">确认消息</label>
                <textarea id="ack-popup-message" class="input" style="min-height:40px" placeholder="反馈 / 确认消息"></textarea>
                <label style="font-size:.78rem;color:var(--text-secondary)">结果摘要</label>
                <textarea id="ack-popup-result" class="input" style="min-height:40px" placeholder="结果摘要"></textarea>
            </div>
            <div class="prompt-popup-footer">
                <button class="btn btn-sm btn-primary" onclick="saveAckPopup()">保存</button>
                <button class="btn btn-sm btn-secondary" onclick="saveAckPopup(true)">保存并关闭</button>
            </div>
        </div>

        <!-- DAG Management Page -->
        <div id="page-dags" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><circle cx="5" cy="6" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="12" cy="18" r="2"/><path d="M6.5 7.5L10.5 10.5M17.5 7.5L13.5 10.5M12 14v2"/></svg>
                    <h2>DAG 管理</h2>
                </header>
                <div class="card-body">
                    <div class="log-toolbar">
                        <input type="text" id="dag-search" class="input" placeholder="搜索 key / 标题 / 描述 ..." style="min-width:180px" onkeydown="if(event.key==='Enter')loadTaskDags()">
                        <select id="dag-status-filter" class="input" style="min-width:110px" onchange="loadTaskDags()">
                            <option value="">全部状态</option>
                            <option value="draft">draft</option>
                            <option value="ready">ready</option>
                            <option value="running">running</option>
                            <option value="paused">paused</option>
                            <option value="done">done</option>
                            <option value="failed">failed</option>
                        </select>
                        <button type="button" class="btn btn-sm btn-primary" onclick="openDagPopup(-1)">新建 DAG</button>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadTaskDags()">刷新</button>
                        <button type="button" class="btn btn-sm btn-danger" onclick="deleteSelectedDags()">删除</button>
                    </div>
                    <table class="log-table" id="dag-table">
                        <thead><tr>
                            <th style="width:34px;text-align:center"><input type="checkbox" id="dag-select-all" onchange="toggleDagSelectAll(this.checked)"></th>
                            <th style="width:180px">DAG Key</th>
                            <th style="width:160px">标题</th>
                            <th style="width:90px">状态</th>
                            <th style="width:100px">节点进度</th>
                            <th style="width:90px">创建者</th>
                            <th style="width:130px">开始时间</th>
                            <th style="width:130px">更新时间</th>
                            <th style="max-width:200px">描述</th>
                            <th style="width:140px;text-align:center">操作</th>
                        </tr></thead>
                        <tbody id="dag-tbody"></tbody>
                    </table>
                    <div id="dag-empty" class="approval-empty">加载中...</div>
                    <div id="dag-detail-panel" style="display:none;margin-top:16px">
                        <div class="card" style="border:1px solid var(--border)">
                            <header class="card-header" style="padding:10px 16px">
                                <h3 id="dag-detail-title" style="margin:0;font-size:.9rem">节点明细</h3>
                                <button class="btn btn-sm btn-secondary" onclick="closeDagDetail()">收起</button>
                            </header>
                            <div class="card-body" style="padding:0">
                                <table class="log-table">
                                    <thead><tr>
                                        <th style="width:140px">节点 Key</th>
                                        <th style="width:140px">标题</th>
                                        <th style="width:70px">类型</th>
                                        <th style="width:90px">执行方</th>
                                        <th style="width:80px">状态</th>
                                        <th style="width:140px">依赖</th>
                                        <th style="width:110px">关联命令卡</th>
                                        <th style="width:110px">开始时间</th>
                                        <th style="width:110px">完成时间</th>
                                        <th style="width:100px;text-align:center">操作</th>
                                    </tr></thead>
                                    <tbody id="dag-node-tbody"></tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
        <div id="dag-popup" class="prompt-popup" style="display:none">
            <div class="prompt-popup-header">
                <span id="dag-popup-title" class="prompt-popup-title">新建 DAG</span>
                <button class="prompt-popup-close" onclick="document.getElementById('dag-popup').style.display='none'">&times;</button>
            </div>
            <div class="prompt-popup-body" style="display:flex;flex-direction:column;gap:10px;padding:16px">
                <label style="font-size:.78rem;color:var(--text-secondary)">DAG Key</label>
                <input type="text" id="dag-popup-key" class="input" placeholder="唯一标识">
                <label style="font-size:.78rem;color:var(--text-secondary)">标题</label>
                <input type="text" id="dag-popup-title-input" class="input" placeholder="DAG 标题">
                <label style="font-size:.78rem;color:var(--text-secondary)">描述</label>
                <textarea id="dag-popup-desc" class="input" style="min-height:60px" placeholder="详细描述"></textarea>
                <div style="display:flex;gap:10px">
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">状态</label>
                    <select id="dag-popup-status" class="input"><option value="draft">draft</option><option value="ready">ready</option><option value="running">running</option><option value="paused">paused</option><option value="done">done</option><option value="failed">failed</option></select></div>
                    <div style="flex:1"><label style="font-size:.78rem;color:var(--text-secondary)">创建者</label>
                    <input type="text" id="dag-popup-created-by" class="input" value="dashboard"></div>
                </div>
            </div>
            <div class="prompt-popup-footer">
                <button class="btn btn-sm btn-primary" onclick="saveDagPopup()">保存</button>
                <button class="btn btn-sm btn-secondary" onclick="saveDagPopup(true)">保存并关闭</button>
            </div>
        </div>

        <!-- Task Traces Page -->
        <div id="page-traces" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M3 12h5l2-6 4 12 2-6h5"/></svg>
                    <h2>任务追踪</h2>
                </header>
                <div class="card-body">
                    <div id="trace-stats" style="display:flex;gap:12px;margin-bottom:14px;flex-wrap:wrap"></div>
                    <div class="log-toolbar">
                        <input type="text" id="trace-search" class="input" placeholder="搜索 trace_id ..." style="min-width:180px" onkeydown="if(event.key==='Enter')loadTaskTraces()">
                        <select id="trace-status-filter" class="input" style="min-width:100px" onchange="loadTaskTraces()">
                            <option value="">全部状态</option>
                            <option value="running">running</option>
                            <option value="ok">ok</option>
                            <option value="error">error</option>
                        </select>
                        <select id="trace-component-filter" class="input" style="min-width:100px" onchange="loadTaskTraces()">
                            <option value="">全部组件</option>
                        </select>
                        <button type="button" class="btn btn-sm btn-secondary" onclick="loadTaskTraces()">刷新</button>
                    </div>
                    <table class="log-table" id="trace-table">
                        <thead><tr>
                            <th style="min-width:240px">Trace ID</th>
                            <th style="width:80px">状态</th>
                            <th style="width:70px">Span数</th>
                            <th style="width:140px">组件</th>
                            <th style="width:140px">开始时间</th>
                            <th style="width:140px">结束时间</th>
                        </tr></thead>
                        <tbody id="trace-tbody"></tbody>
                    </table>
                    <div id="trace-empty" class="approval-empty">加载中...</div>
                    <div id="trace-detail-panel" style="display:none;margin-top:16px;border:1px solid var(--border);border-radius:8px;padding:14px">
                        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px">
                            <h3 style="margin:0;font-size:.95rem" id="trace-detail-title">Span 明细</h3>
                            <button class="btn btn-sm btn-secondary" onclick="closeTraceDetail()">关闭</button>
                        </div>
                        <table class="log-table">
                            <thead><tr>
                                <th style="width:200px">Span Name</th>
                                <th style="width:100px">Component</th>
                                <th style="width:80px">Status</th>
                                <th style="width:80px">耗时(ms)</th>
                                <th style="width:130px">开始时间</th>
                                <th style="width:130px">结束时间</th>
                                <th style="min-width:200px">Input / Output</th>
                            </tr></thead>
                            <tbody id="trace-span-tbody"></tbody>
                        </table>
                    </div>
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

        <!-- Lifecycle Page -->
        <div id="page-lifecycle" class="page">
            <div class="card">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/><path d="M22 12h-2M4 12H2M12 2v2M12 20v2"/></svg>
                    <h2>Agent 生命周期监控</h2>
                    <span id="lc-engine-badge" class="badge badge-gray" style="margin-left:auto;font-size:0.7rem">未启动</span>
                </header>
                <div class="card-body">
                    <!-- Control Panel -->
                    <div class="lifecycle-control">
                        <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                            <button type="button" class="btn btn-sm btn-primary" id="lc-btn-start" onclick="lifecycleWatch('start')">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>
                                启动监控
                            </button>
                            <button type="button" class="btn btn-sm btn-danger" id="lc-btn-stop" onclick="lifecycleWatch('stop')" disabled>
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><rect x="4" y="4" width="16" height="16" rx="2"/></svg>
                                停止
                            </button>
                            <button type="button" class="btn btn-sm btn-secondary" onclick="lifecycleSendNotify()">
                                📢 手动通知
                            </button>
                            <button type="button" class="btn btn-sm btn-secondary" onclick="loadLifecycleStatus()">刷新</button>
                            <label style="display:flex;align-items:center;gap:4px;font-size:0.78rem;color:var(--text-secondary);margin-left:12px">
                                <input type="checkbox" id="lc-dry-run"> Dry Run
                            </label>
                        </div>
                        <div id="lc-stats" class="lifecycle-stats">
                            <span>轮次: <b id="lc-cycles">0</b></span>
                            <span>通知: <b id="lc-notified">0</b></span>
                            <span>错误: <b id="lc-errors">0</b></span>
                            <span>决策: <b id="lc-engine">GPT-5.2</b></span>
                        </div>
                    </div>

                    <!-- Agent Cards Grid -->
                    <div id="lc-grid" class="lifecycle-grid">
                        <div class="lifecycle-empty">启动监控后将显示子 Agent 状态...</div>
                    </div>
                </div>
            </div>

            <!-- Timeline -->
            <div class="card" style="margin-top:16px">
                <header class="card-header">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18"><path d="M12 8v4l3 3"/><circle cx="12" cy="12" r="10"/></svg>
                    <h2>通知时间线</h2>
                    <button type="button" class="btn btn-sm btn-secondary" style="margin-left:auto" onclick="clearLifecycleTimeline()">清空</button>
                </header>
                <div class="card-body">
                    <div id="lc-timeline" class="lifecycle-timeline">
                        <div class="lifecycle-empty">暂无通知记录</div>
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
            ensure_agent_monitor_started()

            try:
                rows = query_agent_status(limit=max(100, lines * 4))
                payload = _build_agent_status_payload_from_rows(rows)
            except Exception as exc:
                if _is_agent_status_table_missing_error(exc):
                    payload = {
                        "ok": False,
                        "ts": datetime.now(timezone.utc).isoformat(),
                        "error": "agent_status_table_unavailable",
                        "summary": _empty_agent_status_summary(),
                        "agents": [],
                        "source": {"db_ok": False},
                    }
                else:
                    logger.debug("query agent_status failed; fallback to iTerm snapshot", exc_info=True)
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

        elif path == "/api/ai-log":
            limit = _safe_int(params.get("limit", ["100"])[0], 100, 1, 500)
            logs = query_ai_logs(
                limit=limit,
                level=params.get("level", [""])[0],
                logger_name=params.get("logger", [""])[0],
                keyword=params.get("keyword", [""])[0],
                category=params.get("category", [""])[0],
                endpoint=params.get("endpoint", [""])[0],
                status_code=params.get("status_code", [""])[0],
            )
            filters = list_ai_filter_values()
            self._respond(200, "application/json",
                          json.dumps({"ok": True, "logs": logs, "filters": filters}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/ai-log/export":
            limit = _safe_int(params.get("limit", ["200"])[0], 200, 1, 2000)
            logs = query_ai_logs(
                limit=limit,
                level=params.get("level", [""])[0],
                logger_name=params.get("logger", [""])[0],
                keyword=params.get("keyword", [""])[0],
                category=params.get("category", [""])[0],
                endpoint=params.get("endpoint", [""])[0],
                status_code=params.get("status_code", [""])[0],
            )
            payload = "\n".join(json.dumps(row, ensure_ascii=False) for row in logs) + ("\n" if logs else "")
            ts = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
            self._respond(200, "application/x-ndjson; charset=utf-8", payload.encode("utf-8"),
                          headers={"Content-Disposition": f"attachment; filename=ai-log-{ts}.ndjson",
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
                from pathlib import Path as _P
                _store = _P(__file__).resolve().parent / "data" / "agent_tasks.json"
                _tasks = json.loads(_store.read_text("utf-8")) if _store.exists() else []
                kw = str(params.get("trace_id", [""])[0] or "").strip().lower()
                st = str(params.get("status", [""])[0] or "").strip()
                comp = str(params.get("component", [""])[0] or "").strip()

                # group by project_id => trace
                projects: dict[str, list] = {}
                for t in _tasks:
                    pid = t.get("project_id", "") or ""
                    if not pid:
                        continue
                    projects.setdefault(pid, []).append(t)

                traces = []
                for pid, tlist in sorted(projects.items(), key=lambda x: max(t.get("updated_at", "") for t in x[1]), reverse=True):
                    total = len(tlist)
                    done_count = sum(1 for t in tlist if t.get("status") == "done")
                    failed = sum(1 for t in tlist if t.get("status") == "failed")
                    in_prog = sum(1 for t in tlist if t.get("status") == "in_progress")
                    # derive trace status
                    if failed > 0:
                        trace_st = "error"
                    elif in_prog > 0 or (done_count < total and done_count > 0):
                        trace_st = "running"
                    elif done_count == total:
                        trace_st = "ok"
                    else:
                        trace_st = "running"
                    # components = unique assignees
                    components = sorted(set(t.get("assignee", "") for t in tlist if t.get("assignee")))
                    started = min((t.get("created_at", "") for t in tlist), default="")
                    finished = max((t.get("updated_at", "") for t in tlist), default="")
                    # filter
                    if kw and kw not in pid.lower():
                        continue
                    if st and trace_st != st:
                        continue
                    if comp and comp not in components:
                        continue
                    traces.append({
                        "trace_id": pid,
                        "status": trace_st,
                        "span_count": total,
                        "started_at": started,
                        "finished_at": finished,
                        "components": components,
                        "done": done_count,
                        "failed": failed,
                        "in_progress": in_prog,
                    })
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "traces": traces}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/task-traces/spans":
            try:
                trace_id = str(params.get("trace_id", [""])[0] or "").strip()
                if not trace_id:
                    self._respond(400, "application/json",
                                  json.dumps({"ok": False, "error": "trace_id required"}).encode("utf-8"))
                    return
                from pathlib import Path as _P
                _store = _P(__file__).resolve().parent / "data" / "agent_tasks.json"
                _tasks = json.loads(_store.read_text("utf-8")) if _store.exists() else []
                project_tasks = [t for t in _tasks if t.get("project_id") == trace_id]
                project_tasks.sort(key=lambda t: t.get("created_at", ""))

                spans = []
                for t in project_tasks:
                    deps = t.get("depends_on", [])
                    parent = deps[0] if deps else ""
                    # duration: estimate from created_at to updated_at
                    dur_ms = 0
                    try:
                        from datetime import datetime as _dt
                        c = _dt.fromisoformat(t.get("created_at", ""))
                        u = _dt.fromisoformat(t.get("updated_at", ""))
                        dur_ms = int((u - c).total_seconds() * 1000)
                    except Exception:
                        pass
                    spans.append({
                        "span_id": t.get("task_id", ""),
                        "span_name": t.get("title", ""),
                        "parent_span_id": parent,
                        "component": t.get("assignee", ""),
                        "status": t.get("status", "pending"),
                        "input_payload": {"description": t.get("description", ""), "priority": t.get("priority", ""), "depends_on": deps},
                        "output_payload": {"result": t.get("result", ""), "retry_count": t.get("retry_count", 0)},
                        "error_text": t.get("result", "") if t.get("status") == "failed" else "",
                        "metadata": {"assignee": t.get("assignee", ""), "creator": t.get("creator", "")},
                        "started_at": t.get("created_at", ""),
                        "finished_at": t.get("updated_at", "") if t.get("status") in ("done", "failed", "cancelled") else "",
                        "duration_ms": dur_ms,
                    })
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "trace_id": trace_id, "spans": spans}, ensure_ascii=False).encode("utf-8"))
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

        elif path == "/api/command-card-versions":
            try:
                card_key = str(params.get("card_key", [""])[0] or "").strip()
                limit = _safe_int(params.get("limit", ["50"])[0], 50, 1, 200)
                versions = list_command_card_versions(card_key=card_key, limit=limit)
                self._respond(
                    200,
                    "application/json",
                    json.dumps({"ok": True, "card_key": card_key, "versions": versions}, ensure_ascii=False).encode("utf-8"),
                )
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

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

        elif path == "/api/task-acks":
            try:
                from pathlib import Path as _P
                _store = _P(__file__).resolve().parent / "data" / "agent_tasks.json"
                _tasks = json.loads(_store.read_text("utf-8")) if _store.exists() else []
                kw = params.get("keyword", [""])[0].strip().lower()
                st = params.get("status", [""])[0].strip()
                pri = params.get("priority", [""])[0].strip()
                asn = params.get("assigned_to", [""])[0].strip()
                pid = params.get("project_id", [""])[0].strip()
                filtered = _tasks
                if kw:
                    filtered = [t for t in filtered if kw in (t.get("task_id","")+" "+t.get("title","")+" "+t.get("description","")+" "+t.get("project_id","")).lower()]
                if st:
                    filtered = [t for t in filtered if t.get("status") == st]
                if pri:
                    filtered = [t for t in filtered if t.get("priority") == pri]
                if asn:
                    filtered = [t for t in filtered if t.get("assignee") == asn]
                if pid:
                    filtered = [t for t in filtered if t.get("project_id") == pid]
                limit = _safe_int(params.get("limit", ["200"])[0], 200, 1, 500)
                acks = []
                for t in filtered[:limit]:
                    acks.append({
                        "ack_key": t.get("task_id", ""),
                        "title": t.get("title", ""),
                        "description": t.get("description", ""),
                        "assigned_to": t.get("assignee", ""),
                        "requested_by": t.get("creator", ""),
                        "priority": t.get("priority", "normal"),
                        "status": t.get("status", "pending"),
                        "progress": 100 if t.get("status") == "done" else (50 if t.get("status") == "in_progress" else 0),
                        "result_summary": t.get("result", ""),
                        "ack_message": "",
                        "project_id": t.get("project_id", ""),
                        "depends_on": t.get("depends_on", []),
                        "timeout_sec": t.get("timeout_sec", 0),
                        "max_retries": t.get("max_retries", 0),
                        "retry_count": t.get("retry_count", 0),
                        "idempotency_key": t.get("idempotency_key", ""),
                        "created_at": t.get("created_at", ""),
                        "updated_at": t.get("updated_at", ""),
                        "due_at": None,
                    })
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "acks": acks}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/task-dags":
            try:
                from pathlib import Path as _P
                _store = _P(__file__).resolve().parent / "data" / "agent_tasks.json"
                _tasks = json.loads(_store.read_text("utf-8")) if _store.exists() else []
                kw = params.get("keyword", [""])[0].strip().lower()
                st = params.get("status", [""])[0].strip()
                # Group by project_id
                projects: dict[str, list] = {}
                for t in _tasks:
                    pid = t.get("project_id", "") or ""
                    if not pid:
                        continue
                    projects.setdefault(pid, []).append(t)
                dags = []
                for pid, tlist in sorted(projects.items(), key=lambda x: x[1][0].get("created_at", ""), reverse=True):
                    total = len(tlist)
                    done_count = sum(1 for t in tlist if t.get("status") in ("done", "cancelled"))
                    in_progress = sum(1 for t in tlist if t.get("status") == "in_progress")
                    pending_count = sum(1 for t in tlist if t.get("status") == "pending")
                    failed_count = sum(1 for t in tlist if t.get("status") == "failed")
                    # derive DAG status
                    if done_count == total:
                        dag_st = "done"
                    elif in_progress > 0:
                        dag_st = "running"
                    elif failed_count > 0:
                        dag_st = "failed"
                    elif pending_count > 0 and done_count > 0:
                        dag_st = "running"
                    else:
                        dag_st = "draft"
                    # keyword filter
                    if kw and kw not in pid.lower() and not any(kw in t.get("title","").lower() for t in tlist):
                        continue
                    if st and dag_st != st:
                        continue
                    creators = set(t.get("creator","") for t in tlist if t.get("creator"))
                    dags.append({
                        "dag_key": pid,
                        "title": pid,
                        "description": f"{total} 个任务 ({done_count} done, {in_progress} running, {pending_count} pending)",
                        "status": dag_st,
                        "node_total": total,
                        "node_done": done_count,
                        "created_by": ", ".join(creators) if creators else "",
                        "started_at": tlist[0].get("created_at", ""),
                        "updated_at": max(t.get("updated_at", "") for t in tlist),
                    })
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "dags": dags}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/task-dags/detail":
            try:
                dag_key = params.get("dag_key", [""])[0]
                if not dag_key:
                    self._respond(400, "application/json",
                                  json.dumps({"ok": False, "error": "dag_key 必须提供"}).encode("utf-8"))
                    return
                from pathlib import Path as _P
                _store = _P(__file__).resolve().parent / "data" / "agent_tasks.json"
                _tasks = json.loads(_store.read_text("utf-8")) if _store.exists() else []
                project_tasks = [t for t in _tasks if t.get("project_id") == dag_key]
                if not project_tasks:
                    self._respond(404, "application/json",
                                  json.dumps({"ok": False, "error": "DAG 未找到"}).encode("utf-8"))
                    return
                total = len(project_tasks)
                done_count = sum(1 for t in project_tasks if t.get("status") in ("done", "cancelled"))
                in_progress = sum(1 for t in project_tasks if t.get("status") == "in_progress")
                nodes = []
                for t in project_tasks:
                    nodes.append({
                        "node_key": t.get("task_id", ""),
                        "title": t.get("title", ""),
                        "node_type": "task",
                        "assigned_to": t.get("assignee", ""),
                        "status": t.get("status", "pending"),
                        "depends_on": t.get("depends_on", []),
                        "command_ref": t.get("idempotency_key", ""),
                        "config": {},
                        "result": t.get("result", ""),
                        "started_at": t.get("created_at", ""),
                        "finished_at": t.get("updated_at", "") if t.get("status") in ("done", "cancelled", "failed") else "",
                        "priority": t.get("priority", "normal"),
                        "retry_count": t.get("retry_count", 0),
                        "max_retries": t.get("max_retries", 0),
                    })
                detail = {
                    "dag_key": dag_key,
                    "title": dag_key,
                    "description": f"{total} 个任务",
                    "status": "running" if in_progress > 0 else ("done" if done_count == total else "draft"),
                    "node_total": total,
                    "node_done": done_count,
                    "nodes": nodes,
                }
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "dag": detail}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/lifecycle/status":
            payload = {
                "ok": True,
                "running": _lifecycle_running,
                "cycles": _lifecycle_state.get("cycles", 0),
                "notifications_sent": _lifecycle_state.get("notifications_sent", 0),
                "errors": _lifecycle_state.get("errors", 0),
                "agents": _lifecycle_state.get("agents", {}),
                "timeline": list(_lifecycle_timeline[-50:]),
                "ts": datetime.now(timezone.utc).isoformat(),
            }
            self._respond(200, "application/json; charset=utf-8",
                          json.dumps(payload, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

        elif path == "/api/llm/config":
            # 返回当前 LLM 配置 (mask API key)
            cfg = dict(_llm_config)
            key = cfg.get("api_key", "")
            if key and len(key) > 12:
                cfg["api_key"] = key[:8] + "..." + key[-4:]
            elif key:
                cfg["api_key"] = "***"
            cfg["api_key_full_length"] = len(_llm_config.get("api_key", ""))
            self._respond(200, "application/json; charset=utf-8",
                          json.dumps({"ok": True, "config": cfg}, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

        elif path == "/api/run/status":
            with _orchestration_lock:
                payload = {"ok": True, **dict(_orchestration_state)}
            self._respond(200, "application/json; charset=utf-8",
                          json.dumps(payload, ensure_ascii=False).encode("utf-8"),
                          headers={"Cache-Control": "no-store"})

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

        elif path == "/api/prompt-templates/delete":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                prompt_keys = data.get("prompt_keys", [])
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                result = delete_prompt_templates(prompt_keys=prompt_keys, updated_by=updated_by)
                if result.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["prompts", "audit"],
                        "reason": "prompt_template_delete",
                    })
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

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

        # ─── ACK endpoints ───
        elif path == "/api/task-acks/save":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                result = save_task_ack(
                    ack_key=str(data.get("ack_key", "") or "").strip(),
                    title=str(data.get("title", "") or "").strip(),
                    description=str(data.get("description", "") or "").strip(),
                    assigned_to=str(data.get("assigned_to", "") or "").strip(),
                    requested_by=str(data.get("requested_by", "dashboard") or "").strip() or "dashboard",
                    priority=str(data.get("priority", "normal") or "").strip(),
                    status=str(data.get("status", "pending") or "").strip(),
                    progress=_safe_int(data.get("progress", "0"), 0, 0, 100),
                    ack_message=str(data.get("ack_message", "") or "").strip(),
                    result_summary=str(data.get("result_summary", "") or "").strip(),
                    metadata=data.get("metadata"),
                    due_at=str(data.get("due_at", "") or "").strip() or None,
                )
                if result.get("ok"):
                    _publish_dashboard_event("sync", {"scope": ["acks", "audit"], "reason": "task_ack_save"})
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/task-acks/status":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                result = update_task_ack_status(
                    ack_key=str(data.get("ack_key", "") or "").strip(),
                    status=str(data.get("status", "") or "").strip(),
                    progress=data.get("progress"),
                    ack_message=str(data.get("ack_message", "") or "").strip(),
                    result_summary=str(data.get("result_summary", "") or "").strip(),
                    updated_by=str(data.get("updated_by", "dashboard") or "").strip() or "dashboard",
                )
                if result.get("ok"):
                    _publish_dashboard_event("sync", {"scope": ["acks", "audit"], "reason": "task_ack_status"})
                code = 200 if result.get("ok") else 400
                self._respond(code, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/task-acks/delete":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                result = delete_task_acks(
                    ack_keys=data.get("ack_keys", []),
                    updated_by=str(data.get("updated_by", "dashboard") or "").strip() or "dashboard",
                )
                if result.get("ok"):
                    _publish_dashboard_event("sync", {"scope": ["acks", "audit"], "reason": "task_ack_delete"})
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        # ─── DAG endpoints ───
        elif path == "/api/task-dags/save":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                result = save_task_dag(
                    dag_key=str(data.get("dag_key", "") or "").strip(),
                    title=str(data.get("title", "") or "").strip(),
                    description=str(data.get("description", "") or "").strip(),
                    status=str(data.get("status", "draft") or "").strip(),
                    created_by=str(data.get("created_by", "dashboard") or "").strip() or "dashboard",
                    metadata=data.get("metadata"),
                )
                if result.get("ok"):
                    _publish_dashboard_event("sync", {"scope": ["dags", "audit"], "reason": "task_dag_save"})
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/task-dags/delete":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                dag_keys = data.get("dag_keys", [])
                if not dag_keys:
                    self._respond(400, "application/json",
                                  json.dumps({"ok": False, "error": "dag_keys 不能为空"}).encode("utf-8"))
                    return
                from pathlib import Path as _P
                _store = _P(__file__).resolve().parent / "data" / "agent_tasks.json"
                _tasks = json.loads(_store.read_text("utf-8")) if _store.exists() else []
                keys_set = set(dag_keys)
                before = len(_tasks)
                _tasks = [t for t in _tasks if t.get("project_id", "") not in keys_set]
                deleted = before - len(_tasks)
                _store.write_text(json.dumps(_tasks, ensure_ascii=False, indent=2), "utf-8")
                if deleted > 0:
                    _publish_dashboard_event("sync", {"scope": ["dags", "acks", "audit"], "reason": "task_dag_delete"})
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "deleted": deleted}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/task-dags/node/save":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                result = save_dag_node(
                    dag_key=str(data.get("dag_key", "") or "").strip(),
                    node_key=str(data.get("node_key", "") or "").strip(),
                    title=str(data.get("title", "") or "").strip(),
                    node_type=str(data.get("node_type", "task") or "").strip(),
                    assigned_to=str(data.get("assigned_to", "") or "").strip(),
                    depends_on=data.get("depends_on"),
                    command_ref=str(data.get("command_ref", "") or "").strip(),
                    config=data.get("config"),
                )
                if result.get("ok"):
                    _publish_dashboard_event("sync", {"scope": ["dags", "audit"], "reason": "dag_node_save"})
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/task-dags/node/status":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                result = update_dag_node_status(
                    dag_key=str(data.get("dag_key", "") or "").strip(),
                    node_key=str(data.get("node_key", "") or "").strip(),
                    status=str(data.get("status", "") or "").strip(),
                    result=data.get("result"),
                    updated_by=str(data.get("updated_by", "dashboard") or "").strip() or "dashboard",
                )
                if result.get("ok"):
                    _publish_dashboard_event("sync", {"scope": ["dags", "audit"], "reason": "dag_node_status"})
                code = 200 if result.get("ok") else 400
                self._respond(code, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

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

        elif path == "/api/session/send":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body) if body else {}
                agent_key = str(data.get("agent_key", "master") or "master").strip()
                text = str(data.get("text", "") or "").strip()
                if not text:
                    raise ValueError("text 不能为空")

                from agents.iterm_bridge import _list_live_sessions, AgentSession, _run_iterm_io

                _, live_sessions = _list_live_sessions()
                target_session = None
                # 1) 优先按 agent_id 精确匹配
                for s in live_sessions:
                    if s.get("agent_id", "") == agent_key:
                        target_session = s
                        break
                # 2) 对 master: 回退到 session_name 包含 "主agent" / "主agnet"
                if not target_session and agent_key == "master":
                    for s in live_sessions:
                        sname = (s.get("session_name") or s.get("name") or "").lower()
                        if "主agent" in sname or "主agnet" in sname:
                            target_session = s
                            break

                if not target_session:
                    self._respond(404, "application/json",
                        json.dumps({"ok": False, "error": f"未找到 agent_id={agent_key} 的活跃 iTerm 会话"}, ensure_ascii=False).encode("utf-8"))
                    return

                target = AgentSession(
                    index=0,
                    agent_id=agent_key,
                    agent_name=target_session.get("agent_name") or target_session.get("name", ""),
                    session_id=target_session["session_id"],
                )
                result = _run_iterm_io(
                    targets=[target], text=text, append_enter=False,
                    wait_sec=0, read_lines=0,
                )
                if result and result[0].get("error"):
                    raise RuntimeError(result[0]["error"])

                self._respond(200, "application/json",
                    json.dumps({"ok": True, "session_id": target_session["session_id"],
                                "agent_name": target.agent_name}, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                logger.warning("session/send 失败: %s", e, exc_info=True)
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False).encode("utf-8"))

        elif path == "/api/command-cards":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                saved = _save_command_card_entry(data, updated_by=updated_by)
                _publish_dashboard_event("sync", {
                    "scope": ["command_cards", "audit", "system"],
                    "reason": "command_card_saved",
                    "card_key": saved.get("card_key", ""),
                })
                self._respond(200, "application/json", json.dumps({"ok": True, "command_card": saved}, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-cards/toggle":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                card_key = str(data.get("card_key", "") or "").strip()
                if not card_key:
                    raise ValueError("card_key 不能为空")

                enabled = _safe_bool(data.get("enabled", True), default=True)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                result = set_command_card_enabled(card_key=card_key, enabled=enabled, updated_by=updated_by)
                code = 200 if result.get("ok") else 400
                if result.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["command_cards", "audit", "system"],
                        "reason": "command_card_toggle",
                        "card_key": card_key,
                        "enabled": enabled,
                    })
                self._respond(code, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-cards/delete":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body) if body else {}
                card_keys_raw = data.get("card_keys", [])
                if not isinstance(card_keys_raw, list):
                    raise ValueError("card_keys 必须是数组")

                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                card_keys = [str(item or "").strip() for item in card_keys_raw]
                result = delete_command_cards(card_keys=card_keys, updated_by=updated_by)

                _publish_dashboard_event("sync", {
                    "scope": ["command_cards", "audit", "system"],
                    "reason": "command_card_delete",
                    "deleted": int(result.get("deleted", 0) or 0),
                })
                self._respond(200, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json", json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/command-cards/rollback":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body)
                card_key = str(data.get("card_key", "") or "").strip()
                version_id = _safe_int(data.get("version_id", "0"), 0, 1, 2_147_483_647)
                updated_by = str(data.get("updated_by", "dashboard") or "").strip() or "dashboard"
                result = rollback_command_card(card_key=card_key, version_id=version_id, updated_by=updated_by)
                code = 200 if result.get("ok") else 400
                if result.get("ok"):
                    _publish_dashboard_event("sync", {
                        "scope": ["command_cards", "audit", "system"],
                        "reason": "command_card_rollback",
                        "card_key": card_key,
                        "version_id": version_id,
                    })
                self._respond(code, "application/json", json.dumps(result, ensure_ascii=False).encode("utf-8"))
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

        elif path == "/api/lifecycle/watch":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body) if body else {}
                action = str(data.get("action", "start")).strip()
                dry_run = bool(data.get("dry_run", False))

                global _lifecycle_thread, _lifecycle_running

                if action == "start":
                    if _lifecycle_running:
                        self._respond(200, "application/json",
                                      json.dumps({"ok": True, "message": "监控已在运行中"}, ensure_ascii=False).encode("utf-8"))
                        return
                    _lifecycle_stop_event.clear()
                    _lifecycle_state.update({"cycles": 0, "notifications_sent": 0, "errors": 0, "agents": {}})
                    _lifecycle_thread = threading.Thread(target=_lifecycle_worker, args=(dry_run,), daemon=True)
                    _lifecycle_thread.start()
                    self._respond(200, "application/json",
                                  json.dumps({"ok": True, "message": "监控已启动" + (" (Dry Run)" if dry_run else "")}, ensure_ascii=False).encode("utf-8"))

                elif action == "stop":
                    if not _lifecycle_running:
                        self._respond(200, "application/json",
                                      json.dumps({"ok": True, "message": "监控未在运行"}, ensure_ascii=False).encode("utf-8"))
                        return
                    _lifecycle_stop_event.set()
                    self._respond(200, "application/json",
                                  json.dumps({"ok": True, "message": "正在停止监控..."}, ensure_ascii=False).encode("utf-8"))

                else:
                    self._respond(400, "application/json",
                                  json.dumps({"ok": False, "error": f"未知操作: {action}"}, ensure_ascii=False).encode("utf-8"))

            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/llm/config":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body) if body else {}
                allowed_keys = {"api_key", "base_url", "model", "reasoning_effort", "timeout", "poll_interval", "cooldown_sec", "master_agent_id"}
                updated = []
                for k, v in data.items():
                    if k in allowed_keys and isinstance(v, str) and v.strip():
                        _llm_config[k] = v.strip()
                        updated.append(k)
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "updated": updated, "message": f"已更新 {len(updated)} 项配置"}, ensure_ascii=False).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/llm/test":
            body = self.rfile.read(self._safe_content_length())
            try:
                import httpx
                api_key = _llm_config.get("api_key", "")
                base_url = _llm_config.get("base_url", "https://api.gpteamservices.com/v1")
                model = _llm_config.get("model", "gpt-5.2")
                effort = _llm_config.get("reasoning_effort", "high")
                timeout = max(5, int(_llm_config.get("timeout", "30")))

                started = time.perf_counter()
                with httpx.Client(timeout=timeout) as client:
                    resp = client.post(
                        f"{base_url}/responses",
                        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
                        content=json.dumps({"model": model, "input": "hello", "reasoning": {"effort": effort}}, ensure_ascii=False),
                    )
                elapsed_ms = int((time.perf_counter() - started) * 1000)

                if resp.status_code == 200:
                    result = resp.json()
                    out_text = ""
                    for item in result.get("output", []):
                        if item.get("type") == "message":
                            for c in item.get("content", []):
                                if c.get("type") == "output_text":
                                    out_text = c.get("text", "")
                                    break
                            break
                    self._respond(200, "application/json; charset=utf-8",
                                  json.dumps({
                                      "ok": True,
                                      "status_code": resp.status_code,
                                      "elapsed_ms": elapsed_ms,
                                      "model": model,
                                      "response_text": out_text[:500],
                                      "response_id": result.get("id", ""),
                                  }, ensure_ascii=False).encode("utf-8"))
                else:
                    self._respond(200, "application/json; charset=utf-8",
                                  json.dumps({
                                      "ok": False,
                                      "status_code": resp.status_code,
                                      "elapsed_ms": elapsed_ms,
                                      "error": resp.text[:300],
                                  }, ensure_ascii=False).encode("utf-8"))

            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        elif path == "/api/run":
            body = self.rfile.read(self._safe_content_length())
            try:
                data = json.loads(body) if body else {}
                task_desc = str(data.get("task", "") or "").strip()
                if not task_desc:
                    raise ValueError("task 不能为空")
                with _orchestration_lock:
                    if _orchestration_state["status"] == "running":
                        self._respond(409, "application/json",
                                      json.dumps({"ok": False, "error": "已有编排任务在运行中",
                                                  "task": _orchestration_state["task"]},
                                                 ensure_ascii=False).encode("utf-8"))
                        return
                    _orchestration_state.update({
                        "status": "running",
                        "task": task_desc,
                        "started_at": datetime.now(timezone.utc).isoformat(),
                        "finished_at": "",
                        "elapsed_sec": 0.0,
                        "result": "",
                        "error": "",
                        "trace_id": "",
                    })
                t = threading.Thread(target=_run_orchestration_worker, args=(task_desc,), daemon=True)
                t.start()
                _publish_dashboard_event("sync", {
                    "scope": ["orchestration", "audit"],
                    "reason": "orchestration_start",
                    "task": task_desc[:120],
                })
                self._respond(200, "application/json",
                              json.dumps({"ok": True, "message": "编排任务已启动",
                                          "task": task_desc[:200]},
                                         ensure_ascii=False).encode("utf-8"))
            except ValueError as e:
                self._respond(400, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))

        else:
            self.send_error(404)

    def _serve_event_stream(self) -> None:
        sync_interval_sec = _safe_int(os.getenv("DASHBOARD_SSE_SYNC_SEC", "5"), 5, 1, 60)
        ensure_agent_monitor_started()
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

            try:
                rows = query_agent_status(limit=500)
                initial_snapshot = _build_agent_status_payload_from_rows(rows)
            except Exception as exc:
                if _is_agent_status_table_missing_error(exc):
                    initial_snapshot = {
                        "ok": False,
                        "ts": datetime.now(timezone.utc).isoformat(),
                        "error": "agent_status_table_unavailable",
                        "summary": _empty_agent_status_summary(),
                        "agents": [],
                        "source": {"db_ok": False},
                    }
                else:
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
    import signal

    port = _safe_int(os.getenv("DASHBOARD_PORT", "8080"), 8080, 1, 65535)

    from logging_setup import setup_global_logging
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    setup_global_logging(default_level=log_level)

    # ── 信号 & 全局异常日志 ──────────────────────────────
    def _signal_handler(signum, frame):
        sig_name = signal.Signals(signum).name if hasattr(signal, "Signals") else str(signum)
        logger.warning("Dashboard 收到信号 %s (%s)，即将退出", sig_name, signum)
        raise SystemExit(128 + signum)

    for sig in (signal.SIGTERM, signal.SIGHUP, signal.SIGQUIT):
        try:
            signal.signal(sig, _signal_handler)
        except (OSError, ValueError):
            pass  # 某些信号在非主线程中不可捕获

    _orig_excepthook = sys.excepthook

    def _excepthook(exc_type, exc_value, exc_tb):
        if exc_type is not KeyboardInterrupt:
            logger.critical("Dashboard 未捕获异常导致退出", exc_info=(exc_type, exc_value, exc_tb))
        _orig_excepthook(exc_type, exc_value, exc_tb)

    sys.excepthook = _excepthook
    # ────────────────────────────────────────────────────

    logger.info("Dashboard v2 启动中 port=%s", port)

    Path(__file__).parent.joinpath("data").mkdir(exist_ok=True)

    start_tg_bridge()
    ensure_agent_monitor_started()

    server = http.server.ThreadingHTTPServer(("0.0.0.0", port), DashboardHandler)
    logger.info("Dashboard v2 已启动: http://localhost:%s", port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Dashboard 收到 KeyboardInterrupt，正常停止")
        server.shutdown()
    except SystemExit as e:
        logger.info("Dashboard 退出 code=%s", e.code)
        server.shutdown()
    except Exception:
        logger.critical("Dashboard 异常退出", exc_info=True)
        server.shutdown()



if __name__ == "__main__":
    main()
