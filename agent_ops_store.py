"""Agent 运营数据存储：交互 / 任务追踪 / 提示词版本 / 命令卡。"""

from __future__ import annotations

import json
import os
import re
import uuid
from datetime import datetime, timedelta, timezone
from typing import Any, Optional

from audit_log import append_event
from db.postgres import connect_cursor, execute, fetch_all, fetch_one
from utils import escape_like, normalize_limit

KEY_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{1,127}$")
MAX_LIMIT = 1000
MAX_SQL_LENGTH = 4096
_SQL_WRITE_KEYWORD_RE = re.compile(
    r"\b(insert|update|delete|merge|create|alter|drop|truncate|grant|revoke|comment|copy|vacuum|analyze|refresh|reindex|cluster|call|do)\b",
    re.IGNORECASE,
)
_SQL_DANGEROUS_EXEC_KEYWORD_RE = re.compile(
    r"\b(create|alter|drop|truncate|grant|revoke|comment|copy|vacuum|analyze|refresh|reindex|cluster|call|do)\b",
    re.IGNORECASE,
)
_SQL_DML_KEYWORD_RE = re.compile(r"\b(insert|update|delete|merge)\b", re.IGNORECASE)
_SQL_TOKEN_RE = re.compile(r"('(?:''|[^'])*')|(\"(?:\"\"|[^\"])*\")|(--[^\n]*$)|(/\*.*?\*/)", re.MULTILINE | re.DOTALL)
_ALLOWED_EXEC_KEYWORDS = {
    "insert",
    "update",
    "delete",
    "merge",
    "with",
}
_DB_EXECUTE_ALLOWED_TABLES = {
    "agent_interactions",
    "prompt_templates",
    "prompt_versions",
    "task_traces",
    "command_cards",
    "command_card_versions",
    "command_card_runs",
    "task_acks",
    "task_dags",
    "task_dag_nodes",
}
_DML_TARGET_TABLE_RE = re.compile(
    r"\b(?:insert\s+into|update|delete\s+from|merge\s+into)\s+([A-Za-z_][A-Za-z0-9_$]*(?:\.[A-Za-z_][A-Za-z0-9_$]*)?)\b",
    re.IGNORECASE,
)
_TRACE_STATUS_SET = {"running", "ok", "error", "cancelled"}


class RowMissingError(RuntimeError):
    pass


def _require_row(row: Optional[dict[str, Any]], action: str) -> dict[str, Any]:
    if row is None:
        raise RowMissingError(f"{action} 执行失败：数据库未返回结果")
    return row





def _strip_sql_literals(query: str) -> str:
    return _SQL_TOKEN_RE.sub(" ", str(query))


def _validate_single_statement(query: str) -> str:
    text = str(query or "").strip()
    if not text:
        raise ValueError("sql 不能为空")
    if len(text) > MAX_SQL_LENGTH:
        raise ValueError(f"sql 超过最大长度限制 ({MAX_SQL_LENGTH} 字符)")

    body = text.rstrip(";").strip()
    if not body:
        raise ValueError("sql 不能为空")
    if ";" in _strip_sql_literals(body):
        raise ValueError("仅允许执行单条 SQL")

    return body


def _first_sql_keyword(query: str) -> str:
    match = re.match(r"\s*([a-zA-Z_]+)", query)
    return match.group(1).lower() if match else ""


def _validate_read_only_query(query: str) -> str:
    body = _validate_single_statement(query)
    sanitized = _strip_sql_literals(body)
    first = _first_sql_keyword(sanitized)
    if first not in {"select", "with"}:
        raise ValueError("db_query 仅允许 SELECT/CTE 查询")
    if _SQL_WRITE_KEYWORD_RE.search(sanitized):
        raise ValueError("db_query 检测到写操作关键字，已拒绝")
    return body


def _validate_execute_query(query: str) -> str:
    body = _validate_single_statement(query)
    sanitized = _strip_sql_literals(body)
    first = _first_sql_keyword(sanitized)
    if not first:
        raise ValueError("sql 语法无效")
    if first in {"select", "show", "explain"}:
        raise ValueError("db_execute 不允许只读 SQL，请改用 db_query")
    if first not in _ALLOWED_EXEC_KEYWORDS:
        raise ValueError(f"db_execute 不支持该 SQL 类型: {first}")

    if _SQL_DANGEROUS_EXEC_KEYWORD_RE.search(sanitized):
        raise ValueError("db_execute 禁止执行 DDL/管理语句")

    if first == "with" and not _SQL_DML_KEYWORD_RE.search(sanitized):
        raise ValueError("db_execute 的 WITH 语句必须包含 INSERT/UPDATE/DELETE/MERGE")

    dml_tables = {
        str(match.group(1) or "").strip().lower().split(".")[-1]
        for match in _DML_TARGET_TABLE_RE.finditer(sanitized)
        if str(match.group(1) or "").strip()
    }
    if not dml_tables:
        raise ValueError("db_execute 未检测到 DML 目标表")

    blocked_tables = sorted(table for table in dml_tables if table not in _DB_EXECUTE_ALLOWED_TABLES)
    if blocked_tables:
        raise ValueError(f"db_execute 禁止操作非白名单表: {', '.join(blocked_tables)}")

    return body


def _is_db_execute_enabled() -> bool:
    raw = str(os.getenv("AGENT_DB_EXECUTE_ENABLED", "0")).strip().lower()
    return raw in {"1", "true", "yes", "on"}





def _normalize_key(name: str, value: str) -> str:
    text = str(value or "").strip()
    if not text:
        raise ValueError(f"{name} 不能为空")
    if not KEY_RE.fullmatch(text):
        raise ValueError(f"{name} 格式非法: {text}")
    return text


def _normalize_status(status: str) -> str:
    text = str(status or "pending").strip().lower()
    if not text:
        return "pending"
    return text


def _as_json_text(value: Any) -> str:
    if value is None:
        return "{}"
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, str):
        stripped = value.strip()
        if not stripped:
            return "{}"
        try:
            loaded = json.loads(stripped)
            if isinstance(loaded, (dict, list)):
                return json.dumps(loaded, ensure_ascii=False)
        except json.JSONDecodeError:
            pass
        return json.dumps({"value": value}, ensure_ascii=False)
    return json.dumps({"value": value}, ensure_ascii=False)


def _fmt_dt(value: Any) -> str:
    if isinstance(value, datetime):
        local = value.astimezone()
        return local.strftime("%Y-%-m-%-d:%H:%M")
    return str(value or "")


def _fmt_dt_utc8_iso(value: Any) -> str:
    if isinstance(value, datetime):
        tz_utc8 = timezone(timedelta(hours=8))
        return value.astimezone(tz_utc8).isoformat()
    return str(value or "")


def _row_to_interaction(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "thread_id": str(row.get("thread_id", "")),
        "parent_id": row.get("parent_id"),
        "sender": str(row.get("sender", "")),
        "receiver": str(row.get("receiver", "")),
        "msg_type": str(row.get("msg_type", "")),
        "status": str(row.get("status", "")),
        "requires_review": bool(row.get("requires_review", False)),
        "reviewed_by": str(row.get("reviewed_by", "")),
        "review_note": str(row.get("review_note", "")),
        "reviewed_at": _fmt_dt(row.get("reviewed_at")),
        "payload": row.get("payload") if isinstance(row.get("payload"), (dict, list)) else {},
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def _row_to_prompt(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "prompt_key": str(row.get("prompt_key", "")),
        "title": str(row.get("title", "")),
        "agent_key": str(row.get("agent_key", "")),
        "tool_name": str(row.get("tool_name", "")),
        "prompt_text": str(row.get("prompt_text", "")),
        "variables": row.get("variables") if isinstance(row.get("variables"), (dict, list)) else {},
        "tags": row.get("tags") if isinstance(row.get("tags"), (dict, list)) else [],
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def _row_to_prompt_version(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "prompt_key": str(row.get("prompt_key", "")),
        "title": str(row.get("title", "")),
        "agent_key": str(row.get("agent_key", "")),
        "tool_name": str(row.get("tool_name", "")),
        "prompt_text": str(row.get("prompt_text", "")),
        "variables": row.get("variables") if isinstance(row.get("variables"), (dict, list)) else {},
        "tags": row.get("tags") if isinstance(row.get("tags"), (dict, list)) else [],
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "source_updated_at": _fmt_dt(row.get("source_updated_at")),
        "created_at": _fmt_dt(row.get("created_at")),
        "archived_at": _fmt_dt(row.get("archived_at")),
    }


def _row_to_task_trace(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "trace_id": str(row.get("trace_id", "")),
        "span_id": str(row.get("span_id", "")),
        "parent_span_id": str(row.get("parent_span_id", "")),
        "span_name": str(row.get("span_name", "")),
        "component": str(row.get("component", "")),
        "status": str(row.get("status", "running")),
        "input_payload": row.get("input_payload") if isinstance(row.get("input_payload"), (dict, list)) else {},
        "output_payload": row.get("output_payload") if isinstance(row.get("output_payload"), (dict, list)) else {},
        "error_text": str(row.get("error_text", "")),
        "metadata": row.get("metadata") if isinstance(row.get("metadata"), (dict, list)) else {},
        "started_at": _fmt_dt(row.get("started_at")),
        "finished_at": _fmt_dt(row.get("finished_at")),
        "duration_ms": int(row.get("duration_ms", 0) or 0),
    }


def create_trace_id() -> str:
    return f"trace_{uuid.uuid4().hex}"


def create_span_id() -> str:
    return f"span_{uuid.uuid4().hex}"


def _normalize_trace_status(status: str) -> str:
    value = str(status or "").strip().lower() or "running"
    if value not in _TRACE_STATUS_SET:
        raise ValueError(f"trace status 非法: {status}")
    return value


def start_task_trace_span(
    span_name: str,
    component: str,
    trace_id: str = "",
    parent_span_id: str = "",
    input_payload: Optional[Any] = None,
    metadata: Optional[Any] = None,
    span_id: str = "",
) -> dict[str, Any]:
    trace_text = str(trace_id or "").strip() or create_trace_id()
    span_text = str(span_id or "").strip() or create_span_id()
    parent_text = str(parent_span_id or "").strip()
    span_name_text = str(span_name or "").strip()
    component_text = str(component or "").strip()

    if not span_name_text:
        raise ValueError("span_name 不能为空")
    if not component_text:
        raise ValueError("component 不能为空")

    row = fetch_one(
        """
        INSERT INTO task_traces (
            trace_id, span_id, parent_span_id, span_name, component,
            status, input_payload, metadata, started_at
        )
        VALUES (%s, %s, %s, %s, %s, 'running', %s::jsonb, %s::jsonb, NOW())
        RETURNING id, trace_id, span_id, parent_span_id, span_name, component,
                  status, input_payload, output_payload, error_text, metadata,
                  started_at, finished_at, duration_ms
        """,
        (
            trace_text,
            span_text,
            parent_text,
            span_name_text,
            component_text,
            _as_json_text(input_payload),
            _as_json_text(metadata),
        ),
    )
    result = _row_to_task_trace(_require_row(row, "start_task_trace_span"))
    append_event(
        event_type="task_trace",
        action="start",
        result="ok",
        actor=component_text,
        target=trace_text,
        detail=span_name_text,
        extra={"span_id": span_text, "parent_span_id": parent_text},
    )
    return result


def finish_task_trace_span(
    span_id: str,
    status: str = "ok",
    output_payload: Optional[Any] = None,
    error_text: str = "",
    metadata: Optional[Any] = None,
) -> dict[str, Any]:
    span_text = str(span_id or "").strip()
    if not span_text:
        raise ValueError("span_id 不能为空")

    status_text = _normalize_trace_status(status)
    existing = fetch_one(
        """
        SELECT metadata
        FROM task_traces
        WHERE span_id = %s
        """,
        (span_text,),
    )
    if not existing:
        return {"ok": False, "message": f"trace span not found: {span_text}"}

    merged_metadata: Any = existing.get("metadata") if isinstance(existing, dict) else {}
    if isinstance(merged_metadata, dict) and isinstance(metadata, dict):
        merged_metadata = {**merged_metadata, **metadata}
    elif metadata is not None:
        merged_metadata = metadata

    row = fetch_one(
        """
        UPDATE task_traces
        SET status = %s,
            output_payload = %s::jsonb,
            error_text = %s,
            metadata = %s::jsonb,
            finished_at = NOW(),
            duration_ms = GREATEST(0, ROUND(EXTRACT(EPOCH FROM (NOW() - started_at)) * 1000)::INT)
        WHERE span_id = %s
        RETURNING id, trace_id, span_id, parent_span_id, span_name, component,
                  status, input_payload, output_payload, error_text, metadata,
                  started_at, finished_at, duration_ms
        """,
        (
            status_text,
            _as_json_text(output_payload),
            str(error_text or ""),
            _as_json_text(merged_metadata),
            span_text,
        ),
    )
    if not row:
        return {"ok": False, "message": f"trace span not found: {span_text}"}

    result = _row_to_task_trace(row)
    append_event(
        event_type="task_trace",
        action="finish",
        result=status_text,
        actor=result.get("component", ""),
        target=result.get("trace_id", ""),
        detail=result.get("span_name", ""),
        extra={"span_id": span_text, "duration_ms": result.get("duration_ms", 0)},
    )
    return {"ok": True, "span": result}


def list_task_traces(
    trace_id: str = "",
    component: str = "",
    status: str = "",
    limit: int = 100,
) -> list[dict[str, Any]]:
    where = []
    params: list[Any] = []

    if trace_id:
        where.append("trace_id = %s")
        params.append(str(trace_id).strip())
    if component:
        where.append("component = %s")
        params.append(str(component).strip())
    if status:
        where.append("status = %s")
        params.append(_normalize_trace_status(status))

    sql_text = """
        SELECT id, trace_id, span_id, parent_span_id, span_name, component,
               status, input_payload, output_payload, error_text, metadata,
               started_at, finished_at, duration_ms
        FROM task_traces
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY started_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_task_trace(row) for row in fetch_all(sql_text, params)]


def get_task_trace_span(span_id: str) -> Optional[dict[str, Any]]:
    span_text = str(span_id or "").strip()
    if not span_text:
        raise ValueError("span_id 不能为空")
    row = fetch_one(
        """
        SELECT id, trace_id, span_id, parent_span_id, span_name, component,
               status, input_payload, output_payload, error_text, metadata,
               started_at, finished_at, duration_ms
        FROM task_traces
        WHERE span_id = %s
        """,
        (span_text,),
    )
    return _row_to_task_trace(row) if row else None


def list_task_trace_spans(trace_id: str, limit: int = 500) -> list[dict[str, Any]]:
    trace_text = str(trace_id or "").strip()
    if not trace_text:
        raise ValueError("trace_id 不能为空")
    rows = fetch_all(
        """
        SELECT id, trace_id, span_id, parent_span_id, span_name, component,
               status, input_payload, output_payload, error_text, metadata,
               started_at, finished_at, duration_ms
        FROM task_traces
        WHERE trace_id = %s
        ORDER BY started_at ASC, id ASC
        LIMIT %s
        """,
        (trace_text, normalize_limit(limit, default=500)),
    )
    return [_row_to_task_trace(row) for row in rows]


def _row_to_card(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "card_key": str(row.get("card_key", "")),
        "title": str(row.get("title", "")),
        "description": str(row.get("description", "")),
        "command_template": str(row.get("command_template", "")),
        "args_schema": row.get("args_schema") if isinstance(row.get("args_schema"), (dict, list)) else {},
        "risk_level": str(row.get("risk_level", "normal")),
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "created_at": _fmt_dt_utc8_iso(row.get("created_at")),
        "updated_at": _fmt_dt_utc8_iso(row.get("updated_at")),
        "last_run_at": _fmt_dt_utc8_iso(row.get("last_run_at")),
        "run_count": int(row.get("run_count", 0) or 0),
    }


def _row_to_card_version(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "card_key": str(row.get("card_key", "")),
        "title": str(row.get("title", "")),
        "description": str(row.get("description", "")),
        "command_template": str(row.get("command_template", "")),
        "args_schema": row.get("args_schema") if isinstance(row.get("args_schema"), (dict, list)) else {},
        "risk_level": str(row.get("risk_level", "normal")),
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "source_updated_at": _fmt_dt_utc8_iso(row.get("source_updated_at")),
        "created_at": _fmt_dt_utc8_iso(row.get("created_at")),
        "archived_at": _fmt_dt_utc8_iso(row.get("archived_at")),
    }


def create_interaction(
    sender: str,
    receiver: str,
    msg_type: str,
    content: str,
    thread_id: str = "",
    parent_id: Optional[int] = None,
    requires_review: bool = False,
    metadata: Optional[Any] = None,
    status: str = "pending",
) -> dict[str, Any]:
    """创建 Agent 间交互记录并持久化到 PostgreSQL。

    Args:
        sender: 发送方标识。
        receiver: 接收方标识。
        msg_type: 消息类型。
        content: 消息内容。
        thread_id: 会话线程 ID。
        parent_id: 父消息 ID（用于嵌套回复）。
        requires_review: 是否需要人工审批。
        metadata: 附加元数据。
        status: 初始状态，默认 'pending'。

    Returns:
        标准化后的交互记录字典。

    Raises:
        RowMissingError: 数据库插入未返回结果时抛出。
    """
    sender_text = _normalize_key("sender", sender)
    receiver_text = str(receiver or "").strip()
    msg_type_text = _normalize_key("msg_type", msg_type)
    thread_id_text = str(thread_id or "").strip()
    status_text = _normalize_status(status)

    payload = {
        "content": str(content or ""),
        "metadata": metadata if isinstance(metadata, (dict, list)) else ({"value": metadata} if metadata is not None else {}),
    }

    row = fetch_one(
        """
        INSERT INTO agent_interactions (
            thread_id, parent_id, sender, receiver, msg_type, status,
            requires_review, payload, updated_at
        )
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s::jsonb, NOW())
        RETURNING id, thread_id, parent_id, sender, receiver, msg_type, status,
                  requires_review, reviewed_by, review_note, reviewed_at,
                  payload, created_at, updated_at
        """,
        (
            thread_id_text,
            parent_id,
            sender_text,
            receiver_text,
            msg_type_text,
            status_text,
            bool(requires_review),
            _as_json_text(payload),
        ),
    )

    result = _row_to_interaction(_require_row(row, "create_interaction"))
    append_event(
        event_type="agent_interaction",
        action="create",
        result="ok",
        actor=sender_text,
        target=receiver_text,
        detail=f"msg_type={msg_type_text}",
        extra={"interaction_id": result["id"], "thread_id": thread_id_text},
    )
    return result


def list_interactions(
    thread_id: str = "",
    sender: str = "",
    receiver: str = "",
    msg_type: str = "",
    status: str = "",
    requires_review: Optional[bool] = None,
    limit: int = 100,
) -> list[dict[str, Any]]:
    """按条件查询交互记录列表。

    Args:
        thread_id: 按会话线程过滤。
        sender: 按发送方过滤。
        receiver: 按接收方过滤。
        msg_type: 按消息类型过滤。
        status: 按状态过滤。
        requires_review: 按是否需审批过滤。
        limit: 返回最大条数。

    Returns:
        交互记录字典列表，按创建时间倒序。
    """
    where = []
    params: list[Any] = []

    if thread_id:
        where.append("thread_id = %s")
        params.append(str(thread_id).strip())
    if sender:
        where.append("sender = %s")
        params.append(str(sender).strip())
    if receiver:
        where.append("receiver = %s")
        params.append(str(receiver).strip())
    if msg_type:
        where.append("msg_type = %s")
        params.append(str(msg_type).strip())
    if status:
        where.append("status = %s")
        params.append(_normalize_status(status))
    if requires_review is not None:
        where.append("requires_review = %s")
        params.append(bool(requires_review))

    sql_text = """
        SELECT id, thread_id, parent_id, sender, receiver, msg_type, status,
               requires_review, reviewed_by, review_note, reviewed_at,
               payload, created_at, updated_at
        FROM agent_interactions
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY created_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_interaction(row) for row in fetch_all(sql_text, params)]


def review_interaction(interaction_id: int, status: str, reviewer: str = "", note: str = "") -> dict[str, Any]:
    try:
        iid = int(interaction_id)
    except (TypeError, ValueError) as exc:
        raise ValueError("interaction_id 非法") from exc

    status_text = _normalize_status(status)
    reviewer_text = str(reviewer or "").strip()
    note_text = str(note or "").strip()

    updated = fetch_one(
        """
        UPDATE agent_interactions
        SET status = %s,
            reviewed_by = %s,
            review_note = %s,
            reviewed_at = NOW(),
            updated_at = NOW()
        WHERE id = %s
        RETURNING id, thread_id, parent_id, sender, receiver, msg_type, status,
                  requires_review, reviewed_by, review_note, reviewed_at,
                  payload, created_at, updated_at
        """,
        (status_text, reviewer_text, note_text, iid),
    )
    if not updated:
        return {"ok": False, "message": f"interaction not found: {iid}"}

    result = _row_to_interaction(updated)
    append_event(
        event_type="agent_interaction",
        action="review",
        result="ok",
        actor=reviewer_text,
        target=str(iid),
        detail=status_text,
    )
    return {"ok": True, "interaction": result}


def save_prompt_template(
    prompt_key: str,
    title: str,
    prompt_text: str,
    agent_key: str = "",
    tool_name: str = "",
    variables: Optional[Any] = None,
    tags: Optional[Any] = None,
    enabled: bool = True,
    updated_by: str = "",
    description: str = "",
) -> dict[str, Any]:
    """保存或更新提示词模板（按 prompt_key 做 upsert）。

    Args:
        prompt_key: 提示词唯一标识。
        title: 提示词标题。
        prompt_text: 提示词正文。
        agent_key: 关联的 Agent 标识。
        tool_name: 关联的工具名称。
        variables: 模板变量定义。
        tags: 标签列表。
        enabled: 是否启用。
        updated_by: 更新人标识。

    Returns:
        标准化后的提示词模板字典。

    Raises:
        ValueError: prompt_text 为空或 prompt_key 格式非法时抛出。
    """
    key = _normalize_key("prompt_key", prompt_key)
    title_text = str(title or "").strip()
    prompt_body = str(prompt_text or "").strip()
    if not prompt_body:
        raise ValueError("prompt_text 不能为空")

    previous = fetch_one(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               created_at, updated_at
        FROM prompt_templates
        WHERE prompt_key = %s
        """,
        (key,),
    )

    if previous:
        execute(
            """
            INSERT INTO prompt_versions (
                prompt_key, title, agent_key, tool_name, prompt_text,
                variables, tags, enabled, created_by, updated_by,
                source_updated_at
            )
            VALUES (%s, %s, %s, %s, %s, %s::jsonb, %s::jsonb, %s, %s, %s, %s)
            """,
            (
                str(previous.get("prompt_key") or ""),
                str(previous.get("title") or ""),
                str(previous.get("agent_key") or ""),
                str(previous.get("tool_name") or ""),
                str(previous.get("prompt_text") or ""),
                _as_json_text(previous.get("variables")),
                _as_json_text(previous.get("tags") if previous.get("tags") is not None else []),
                bool(previous.get("enabled", True)),
                str(previous.get("created_by") or ""),
                str(previous.get("updated_by") or ""),
                previous.get("updated_at"),
            ),
        )

    row = fetch_one(
        """
        INSERT INTO prompt_templates (
            prompt_key, title, agent_key, tool_name, prompt_text,
            variables, tags, enabled, created_by, updated_by, updated_at
        )
        VALUES (%s, %s, %s, %s, %s, %s::jsonb, %s::jsonb, %s, %s, %s, NOW())
        ON CONFLICT (prompt_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            agent_key = EXCLUDED.agent_key,
            tool_name = EXCLUDED.tool_name,
            prompt_text = EXCLUDED.prompt_text,
            variables = EXCLUDED.variables,
            tags = EXCLUDED.tags,
            enabled = EXCLUDED.enabled,
            updated_by = EXCLUDED.updated_by,
            updated_at = NOW()
        RETURNING id, prompt_key, title, agent_key, tool_name, prompt_text,
                  variables, tags, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (
            key,
            title_text,
            str(agent_key or "").strip(),
            str(tool_name or "").strip(),
            prompt_body,
            _as_json_text(variables),
            _as_json_text(tags if tags is not None else []),
            bool(enabled),
            str(updated_by or ""),
            str(updated_by or ""),
        ),
    )

    result = _row_to_prompt(_require_row(row, "save_prompt_template"))
    append_event(
        event_type="prompt_template",
        action="save",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=result.get("tool_name", ""),
    )
    return result


def list_prompt_template_versions(prompt_key: str, limit: int = 20) -> list[dict[str, Any]]:
    key = _normalize_key("prompt_key", prompt_key)
    rows = fetch_all(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               source_updated_at, created_at, archived_at
        FROM prompt_versions
        WHERE prompt_key = %s
        ORDER BY id DESC
        LIMIT %s
        """,
        (key, normalize_limit(limit, default=20)),
    )
    return [_row_to_prompt_version(row) for row in rows]


def rollback_prompt_template(prompt_key: str, version_id: int, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("prompt_key", prompt_key)
    try:
        vid = int(version_id)
    except (TypeError, ValueError) as exc:
        raise ValueError("version_id 非法") from exc
    if vid <= 0:
        raise ValueError("version_id 非法")

    version_row = fetch_one(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               source_updated_at, created_at, archived_at
        FROM prompt_versions
        WHERE id = %s AND prompt_key = %s
        """,
        (vid, key),
    )
    if not version_row:
        return {"ok": False, "message": f"prompt version not found: {key}#{vid}"}

    prompt = save_prompt_template(
        prompt_key=key,
        title=str(version_row.get("title") or ""),
        prompt_text=str(version_row.get("prompt_text") or ""),
        agent_key=str(version_row.get("agent_key") or ""),
        tool_name=str(version_row.get("tool_name") or ""),
        variables=version_row.get("variables"),
        tags=version_row.get("tags"),
        enabled=bool(version_row.get("enabled", True)),
        updated_by=str(updated_by or ""),
    )
    append_event(
        event_type="prompt_template",
        action="rollback",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=f"version_id={vid}",
    )
    return {"ok": True, "prompt": prompt, "version": _row_to_prompt_version(version_row)}


def get_prompt_template(prompt_key: str) -> Optional[dict[str, Any]]:
    key = _normalize_key("prompt_key", prompt_key)
    row = fetch_one(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               created_at, updated_at
        FROM prompt_templates
        WHERE prompt_key = %s
        """,
        (key,),
    )
    return _row_to_prompt(row) if row else None


def list_prompt_templates(
    agent_key: str = "",
    tool_name: str = "",
    keyword: str = "",
    enabled_only: bool = False,
    limit: int = 100,
) -> list[dict[str, Any]]:
    where = []
    params: list[Any] = []

    if agent_key:
        where.append("agent_key = %s")
        params.append(str(agent_key).strip())
    if tool_name:
        where.append("tool_name = %s")
        params.append(str(tool_name).strip())
    if enabled_only:
        where.append("enabled = TRUE")
    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(LOWER(prompt_key) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(title) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(prompt_text) LIKE %s ESCAPE E'\\\\')"
        )
        params.extend([kw, kw, kw])

    sql_text = """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               created_at, updated_at
        FROM prompt_templates
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY updated_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_prompt(row) for row in fetch_all(sql_text, params)]


def set_prompt_template_enabled(prompt_key: str, enabled: bool, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("prompt_key", prompt_key)
    row = fetch_one(
        """
        UPDATE prompt_templates
        SET enabled = %s,
            updated_by = %s,
            updated_at = NOW()
        WHERE prompt_key = %s
        RETURNING id, prompt_key, title, agent_key, tool_name, prompt_text,
                  variables, tags, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (bool(enabled), str(updated_by or ""), key),
    )
    if not row:
        return {"ok": False, "message": f"prompt not found: {key}"}

    result = _row_to_prompt(row)
    append_event(
        event_type="prompt_template",
        action="toggle",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail="enabled" if enabled else "disabled",
    )
    return {"ok": True, "prompt": result}


def delete_prompt_templates(prompt_keys: list[str], updated_by: str = "") -> dict[str, Any]:
    """批量删除提示词模板。"""
    if not isinstance(prompt_keys, list):
        raise ValueError("prompt_keys 必须是数组")

    normalized: list[str] = []
    seen: set[str] = set()
    for raw in prompt_keys:
        key = _normalize_key("prompt_key", str(raw or ""))
        if key in seen:
            continue
        seen.add(key)
        normalized.append(key)

    if not normalized:
        raise ValueError("prompt_keys 不能为空")

    rows = fetch_all(
        """
        DELETE FROM prompt_templates
        WHERE prompt_key = ANY(%s::text[])
        RETURNING prompt_key
        """,
        (normalized,),
    )
    deleted_keys = [str(row.get("prompt_key", "")) for row in rows if str(row.get("prompt_key", "")).strip()]

    if deleted_keys:
        append_event(
            event_type="prompt_template",
            action="delete",
            result="ok",
            actor=str(updated_by or ""),
            target=",".join(deleted_keys[:10]),
            detail=f"deleted={len(deleted_keys)}",
            extra={"prompt_keys": deleted_keys},
        )

    return {
        "ok": True,
        "requested": len(normalized),
        "deleted": len(deleted_keys),
        "prompt_keys": deleted_keys,
    }


def save_command_card(
    card_key: str,
    title: str,
    command_template: str,
    description: str = "",
    args_schema: Optional[Any] = None,
    risk_level: str = "normal",
    enabled: bool = True,
    updated_by: str = "",
) -> dict[str, Any]:
    """保存或更新命令卡定义（按 card_key 做 upsert）。

    Args:
        card_key: 命令卡唯一标识。
        title: 命令卡标题。
        command_template: 命令模板（支持 {param} 占位符）。
        description: 命令卡描述。
        args_schema: 参数 schema 定义。
        risk_level: 风险等级（low/normal/high/critical）。
        enabled: 是否启用。
        updated_by: 更新人标识。

    Returns:
        标准化后的命令卡字典。

    Raises:
        ValueError: title 或 command_template 为空时抛出。
    """
    key = _normalize_key("card_key", card_key)
    title_text = str(title or "").strip()
    command_text = str(command_template or "").strip()
    if not title_text:
        raise ValueError("title 不能为空")
    if not command_text:
        raise ValueError("command_template 不能为空")

    previous = fetch_one(
        """
        SELECT id, card_key, title, description, command_template,
               args_schema, risk_level, enabled, created_by, updated_by,
               created_at, updated_at
        FROM command_cards
        WHERE card_key = %s
        """,
        (key,),
    )

    if previous:
        execute(
            """
            INSERT INTO command_card_versions (
                card_key, title, description, command_template, args_schema,
                risk_level, enabled, created_by, updated_by, source_updated_at
            )
            VALUES (%s, %s, %s, %s, %s::jsonb, %s, %s, %s, %s, %s)
            """,
            (
                str(previous.get("card_key") or ""),
                str(previous.get("title") or ""),
                str(previous.get("description") or ""),
                str(previous.get("command_template") or ""),
                _as_json_text(previous.get("args_schema")),
                str(previous.get("risk_level") or "normal"),
                bool(previous.get("enabled", True)),
                str(previous.get("created_by") or ""),
                str(previous.get("updated_by") or ""),
                previous.get("updated_at"),
            ),
        )

    row = fetch_one(
        """
        INSERT INTO command_cards (
            card_key, title, description, command_template, args_schema,
            risk_level, enabled, created_by, updated_by, updated_at
        )
        VALUES (%s, %s, %s, %s, %s::jsonb, %s, %s, %s, %s, NOW())
        ON CONFLICT (card_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            description = EXCLUDED.description,
            command_template = EXCLUDED.command_template,
            args_schema = EXCLUDED.args_schema,
            risk_level = EXCLUDED.risk_level,
            enabled = EXCLUDED.enabled,
            updated_by = EXCLUDED.updated_by,
            updated_at = NOW()
        RETURNING id, card_key, title, description, command_template,
                  args_schema, risk_level, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (
            key,
            title_text,
            str(description or ""),
            command_text,
            _as_json_text(args_schema),
            str(risk_level or "normal").strip().lower() or "normal",
            bool(enabled),
            str(updated_by or ""),
            str(updated_by or ""),
        ),
    )

    result = _row_to_card(_require_row(row, "save_command_card"))
    append_event(
        event_type="command_card",
        action="save",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=result.get("risk_level", "normal"),
    )
    return result


def list_command_card_versions(card_key: str, limit: int = 20) -> list[dict[str, Any]]:
    key = _normalize_key("card_key", card_key)
    rows = fetch_all(
        """
        SELECT id, card_key, title, description, command_template,
               args_schema, risk_level, enabled, created_by, updated_by,
               source_updated_at, created_at, archived_at
        FROM command_card_versions
        WHERE card_key = %s
        ORDER BY id DESC
        LIMIT %s
        """,
        (key, normalize_limit(limit, default=20)),
    )
    return [_row_to_card_version(row) for row in rows]


def rollback_command_card(card_key: str, version_id: int, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("card_key", card_key)
    try:
        vid = int(version_id)
    except (TypeError, ValueError) as exc:
        raise ValueError("version_id 非法") from exc
    if vid <= 0:
        raise ValueError("version_id 非法")

    version_row = fetch_one(
        """
        SELECT id, card_key, title, description, command_template,
               args_schema, risk_level, enabled, created_by, updated_by,
               source_updated_at, created_at, archived_at
        FROM command_card_versions
        WHERE id = %s AND card_key = %s
        """,
        (vid, key),
    )
    if not version_row:
        return {"ok": False, "message": f"command card version not found: {key}#{vid}"}

    card = save_command_card(
        card_key=key,
        title=str(version_row.get("title") or ""),
        command_template=str(version_row.get("command_template") or ""),
        description=str(version_row.get("description") or ""),
        args_schema=version_row.get("args_schema"),
        risk_level=str(version_row.get("risk_level") or "normal"),
        enabled=bool(version_row.get("enabled", True)),
        updated_by=str(updated_by or ""),
    )
    append_event(
        event_type="command_card",
        action="rollback",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=f"version_id={vid}",
    )
    return {"ok": True, "command_card": card, "version": _row_to_card_version(version_row)}


def get_command_card(card_key: str) -> Optional[dict[str, Any]]:
    key = _normalize_key("card_key", card_key)
    row = fetch_one(
        """
        SELECT id, card_key, title, description, command_template,
               args_schema, risk_level, enabled, created_by, updated_by,
               created_at, updated_at
        FROM command_cards
        WHERE card_key = %s
        """,
        (key,),
    )
    return _row_to_card(row) if row else None


def list_command_cards(
    keyword: str = "",
    risk_level: str = "",
    enabled_only: bool = False,
    limit: int = 100,
) -> list[dict[str, Any]]:
    where = []
    params: list[Any] = []

    if risk_level:
        where.append("c.risk_level = %s")
        params.append(str(risk_level).strip().lower())
    if enabled_only:
        where.append("c.enabled = TRUE")
    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(LOWER(c.card_key) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(c.title) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(c.description) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(c.command_template) LIKE %s ESCAPE E'\\\\')"
        )
        params.extend([kw, kw, kw, kw])

    sql_text = """
        SELECT c.id, c.card_key, c.title, c.description, c.command_template,
               c.args_schema, c.risk_level, c.enabled, c.created_by, c.updated_by,
               c.created_at, c.updated_at,
               stats.last_run_at, COALESCE(stats.run_count, 0) AS run_count
        FROM command_cards AS c
        LEFT JOIN (
            SELECT card_key,
                   MAX(COALESCE(executed_at, updated_at, created_at)) AS last_run_at,
                   COUNT(*)::BIGINT AS run_count
            FROM command_card_runs
            GROUP BY card_key
        ) AS stats ON stats.card_key = c.card_key
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY c.updated_at DESC, c.id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_card(row) for row in fetch_all(sql_text, params)]


def set_command_card_enabled(card_key: str, enabled: bool, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("card_key", card_key)
    row = fetch_one(
        """
        UPDATE command_cards
        SET enabled = %s,
            updated_by = %s,
            updated_at = NOW()
        WHERE card_key = %s
        RETURNING id, card_key, title, description, command_template,
                  args_schema, risk_level, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (bool(enabled), str(updated_by or ""), key),
    )
    if not row:
        return {"ok": False, "message": f"command card not found: {key}"}

    result = _row_to_card(row)
    append_event(
        event_type="command_card",
        action="toggle",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail="enabled" if enabled else "disabled",
    )
    return {"ok": True, "command_card": result}


def delete_command_cards(card_keys: list[str], updated_by: str = "") -> dict[str, Any]:
    if not isinstance(card_keys, list):
        raise ValueError("card_keys 必须是数组")

    normalized: list[str] = []
    seen: set[str] = set()
    for raw in card_keys:
        key = _normalize_key("card_key", str(raw or ""))
        if key in seen:
            continue
        seen.add(key)
        normalized.append(key)

    if not normalized:
        raise ValueError("card_keys 不能为空")

    rows = fetch_all(
        """
        DELETE FROM command_cards
        WHERE card_key = ANY(%s::text[])
        RETURNING card_key
        """,
        (normalized,),
    )
    deleted_keys = [str(row.get("card_key", "")) for row in rows if str(row.get("card_key", "")).strip()]

    if deleted_keys:
        append_event(
            event_type="command_card",
            action="delete",
            result="ok",
            actor=str(updated_by or ""),
            target=",".join(deleted_keys[:10]),
            detail=f"deleted={len(deleted_keys)}",
            extra={"card_keys": deleted_keys},
        )

    return {
        "ok": True,
        "requested": len(normalized),
        "deleted": len(deleted_keys),
        "card_keys": deleted_keys,
    }


# 便捷：允许 Agent 执行只读查询
def db_query(sql_text: str, limit: int = 200) -> list[dict[str, Any]]:
    query = _validate_read_only_query(sql_text)
    wrapped = f"SELECT * FROM ({query}) AS t LIMIT %s"
    with connect_cursor(row_as_dict=True, autocommit=False) as cur:
        cur.execute("SET LOCAL transaction_read_only = on")
        cur.execute(wrapped, (normalize_limit(limit, default=200),))
        return cur.fetchall()


# 便捷：允许 Agent 执行单条变更语句（测试阶段）
def db_execute(sql_text: str) -> dict[str, Any]:
    if not _is_db_execute_enabled():
        return {
            "ok": False,
            "error": "db_execute 已禁用：请设置 AGENT_DB_EXECUTE_ENABLED=1 后重试",
        }

    query = _validate_execute_query(sql_text)

    rowcount = execute(query)
    append_event(
        event_type="db",
        action="execute",
        result="ok",
        actor="agent",
        target="postgres",
        detail=query[:180],
        extra={"rowcount": rowcount},
    )
    return {"ok": True, "rowcount": rowcount}


# ─── Task ACK ───────────────────────────────────────────────────────

_ACK_STATUS_SET = {"pending", "acked", "in_progress", "done", "failed", "cancelled"}
_ACK_PRIORITY_SET = {"low", "normal", "high", "critical"}


def _row_to_ack(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "ack_key": str(row.get("ack_key", "")),
        "title": str(row.get("title", "")),
        "description": str(row.get("description", "")),
        "assigned_to": str(row.get("assigned_to", "")),
        "requested_by": str(row.get("requested_by", "")),
        "priority": str(row.get("priority", "normal")),
        "status": str(row.get("status", "pending")),
        "progress": int(row.get("progress", 0) or 0),
        "ack_message": str(row.get("ack_message", "")),
        "result_summary": str(row.get("result_summary", "")),
        "metadata": row.get("metadata") if isinstance(row.get("metadata"), (dict, list)) else {},
        "due_at": _fmt_dt(row.get("due_at")),
        "acked_at": _fmt_dt(row.get("acked_at")),
        "started_at": _fmt_dt(row.get("started_at")),
        "finished_at": _fmt_dt(row.get("finished_at")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


_ACK_RETURNING = """
    RETURNING id, ack_key, title, description, assigned_to, requested_by,
              priority, status, progress, ack_message, result_summary,
              metadata, due_at, acked_at, started_at, finished_at,
              created_at, updated_at
"""


def save_task_ack(
    ack_key: str,
    title: str = "",
    description: str = "",
    assigned_to: str = "",
    requested_by: str = "",
    priority: str = "normal",
    status: str = "pending",
    progress: int = 0,
    ack_message: str = "",
    result_summary: str = "",
    metadata: Optional[Any] = None,
    due_at: Optional[str] = None,
) -> dict[str, Any]:
    key = _normalize_key("ack_key", ack_key)
    pri = str(priority or "normal").strip().lower()
    if pri not in _ACK_PRIORITY_SET:
        pri = "normal"
    st = str(status or "pending").strip().lower()
    if st not in _ACK_STATUS_SET:
        st = "pending"
    prog = max(0, min(100, int(progress or 0)))

    row = fetch_one(
        f"""
        INSERT INTO task_acks (
            ack_key, title, description, assigned_to, requested_by,
            priority, status, progress, ack_message, result_summary,
            metadata, due_at
        )
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s::jsonb, %s::timestamptz)
        ON CONFLICT (ack_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            description = EXCLUDED.description,
            assigned_to = EXCLUDED.assigned_to,
            requested_by = EXCLUDED.requested_by,
            priority = EXCLUDED.priority,
            status = EXCLUDED.status,
            progress = EXCLUDED.progress,
            ack_message = EXCLUDED.ack_message,
            result_summary = EXCLUDED.result_summary,
            metadata = EXCLUDED.metadata,
            due_at = EXCLUDED.due_at,
            updated_at = NOW()
        {_ACK_RETURNING}
        """,
        (
            key,
            str(title or "").strip(),
            str(description or "").strip(),
            str(assigned_to or "").strip(),
            str(requested_by or "").strip(),
            pri, st, prog,
            str(ack_message or "").strip(),
            str(result_summary or "").strip(),
            _as_json_text(metadata),
            str(due_at or "").strip() or None,
        ),
    )
    result = _row_to_ack(_require_row(row, "save_task_ack"))
    append_event(
        event_type="task_ack", action="save", result="ok",
        actor=str(requested_by or ""), target=key,
        detail=f"status={st} priority={pri}",
    )
    return {"ok": True, "task_ack": result}


def list_task_acks(
    keyword: str = "",
    status: str = "",
    priority: str = "",
    assigned_to: str = "",
    limit: int = 200,
) -> list[dict[str, Any]]:
    where: list[str] = []
    params: list[Any] = []

    if status:
        where.append("status = %s")
        params.append(str(status).strip().lower())
    if priority:
        where.append("priority = %s")
        params.append(str(priority).strip().lower())
    if assigned_to:
        where.append("assigned_to = %s")
        params.append(str(assigned_to).strip())
    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(LOWER(ack_key) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(title) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(description) LIKE %s ESCAPE E'\\\\')"
        )
        params.extend([kw, kw, kw])

    sql = "SELECT * FROM task_acks"
    if where:
        sql += " WHERE " + " AND ".join(where)
    sql += " ORDER BY updated_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_ack(row) for row in fetch_all(sql, params)]


def update_task_ack_status(
    ack_key: str,
    status: str,
    progress: Optional[int] = None,
    ack_message: str = "",
    result_summary: str = "",
    updated_by: str = "",
) -> dict[str, Any]:
    key = _normalize_key("ack_key", ack_key)
    st = str(status or "").strip().lower()
    if st not in _ACK_STATUS_SET:
        raise ValueError(f"无效状态: {st}")

    sets = ["status = %s", "updated_at = NOW()"]
    params: list[Any] = [st]

    if st == "acked":
        sets.append("acked_at = COALESCE(acked_at, NOW())")
    elif st == "in_progress":
        sets.append("started_at = COALESCE(started_at, NOW())")
    elif st in ("done", "failed", "cancelled"):
        sets.append("finished_at = NOW()")

    if progress is not None:
        sets.append("progress = %s")
        params.append(max(0, min(100, int(progress))))
    if ack_message:
        sets.append("ack_message = %s")
        params.append(str(ack_message).strip())
    if result_summary:
        sets.append("result_summary = %s")
        params.append(str(result_summary).strip())

    params.append(key)
    row = fetch_one(
        f"UPDATE task_acks SET {', '.join(sets)} WHERE ack_key = %s {_ACK_RETURNING}",
        params,
    )
    if not row:
        return {"ok": False, "error": f"ACK not found: {key}"}

    result = _row_to_ack(row)
    append_event(
        event_type="task_ack", action="status", result="ok",
        actor=str(updated_by or ""), target=key, detail=st,
    )
    return {"ok": True, "task_ack": result}


def delete_task_acks(ack_keys: list[str], updated_by: str = "") -> dict[str, Any]:
    if not isinstance(ack_keys, list):
        raise ValueError("ack_keys 必须是数组")
    normalized = list({_normalize_key("ack_key", str(k or "")) for k in ack_keys})
    if not normalized:
        raise ValueError("ack_keys 不能为空")

    rows = fetch_all(
        "DELETE FROM task_acks WHERE ack_key = ANY(%s::text[]) RETURNING ack_key",
        (normalized,),
    )
    deleted = [str(r.get("ack_key", "")) for r in rows if str(r.get("ack_key", "")).strip()]
    if deleted:
        append_event(
            event_type="task_ack", action="delete", result="ok",
            actor=str(updated_by or ""), target=",".join(deleted[:10]),
            detail=f"deleted={len(deleted)}",
        )
    return {"ok": True, "requested": len(normalized), "deleted": len(deleted), "ack_keys": deleted}


# ─── Task DAG ───────────────────────────────────────────────────────

_DAG_STATUS_SET = {"draft", "ready", "running", "paused", "done", "failed"}
_NODE_STATUS_SET = {"pending", "running", "done", "failed", "skipped"}
_NODE_TYPE_SET = {"task", "gate", "check"}


def _row_to_dag(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "dag_key": str(row.get("dag_key", "")),
        "title": str(row.get("title", "")),
        "description": str(row.get("description", "")),
        "status": str(row.get("status", "draft")),
        "created_by": str(row.get("created_by", "")),
        "metadata": row.get("metadata") if isinstance(row.get("metadata"), (dict, list)) else {},
        "started_at": _fmt_dt(row.get("started_at")),
        "finished_at": _fmt_dt(row.get("finished_at")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def _row_to_dag_node(row: dict[str, Any]) -> dict[str, Any]:
    deps = row.get("depends_on")
    if not isinstance(deps, list):
        deps = []
    return {
        "id": int(row.get("id", 0)),
        "dag_key": str(row.get("dag_key", "")),
        "node_key": str(row.get("node_key", "")),
        "title": str(row.get("title", "")),
        "node_type": str(row.get("node_type", "task")),
        "assigned_to": str(row.get("assigned_to", "")),
        "depends_on": deps,
        "status": str(row.get("status", "pending")),
        "command_ref": str(row.get("command_ref", "")),
        "config": row.get("config") if isinstance(row.get("config"), (dict, list)) else {},
        "result": row.get("result") if isinstance(row.get("result"), (dict, list)) else {},
        "started_at": _fmt_dt(row.get("started_at")),
        "finished_at": _fmt_dt(row.get("finished_at")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


_DAG_RETURNING = """
    RETURNING id, dag_key, title, description, status, created_by,
              metadata, started_at, finished_at, created_at, updated_at
"""

_NODE_RETURNING = """
    RETURNING id, dag_key, node_key, title, node_type, assigned_to,
              depends_on, status, command_ref, config, result,
              started_at, finished_at, created_at, updated_at
"""


def save_task_dag(
    dag_key: str,
    title: str = "",
    description: str = "",
    status: str = "draft",
    created_by: str = "",
    metadata: Optional[Any] = None,
) -> dict[str, Any]:
    key = _normalize_key("dag_key", dag_key)
    st = str(status or "draft").strip().lower()
    if st not in _DAG_STATUS_SET:
        st = "draft"

    row = fetch_one(
        f"""
        INSERT INTO task_dags (dag_key, title, description, status, created_by, metadata)
        VALUES (%s, %s, %s, %s, %s, %s::jsonb)
        ON CONFLICT (dag_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            description = EXCLUDED.description,
            status = EXCLUDED.status,
            created_by = EXCLUDED.created_by,
            metadata = EXCLUDED.metadata,
            updated_at = NOW()
        {_DAG_RETURNING}
        """,
        (
            key,
            str(title or "").strip(),
            str(description or "").strip(),
            st,
            str(created_by or "").strip(),
            _as_json_text(metadata),
        ),
    )
    result = _row_to_dag(_require_row(row, "save_task_dag"))
    append_event(
        event_type="task_dag", action="save", result="ok",
        actor=str(created_by or ""), target=key,
    )
    return {"ok": True, "task_dag": result}


def list_task_dags(
    keyword: str = "",
    status: str = "",
    limit: int = 200,
) -> list[dict[str, Any]]:
    where: list[str] = []
    params: list[Any] = []

    if status:
        where.append("status = %s")
        params.append(str(status).strip().lower())
    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(LOWER(dag_key) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(title) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(description) LIKE %s ESCAPE E'\\\\')"
        )
        params.extend([kw, kw, kw])

    sql = "SELECT * FROM task_dags"
    if where:
        sql += " WHERE " + " AND ".join(where)
    sql += " ORDER BY updated_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    dags = [_row_to_dag(row) for row in fetch_all(sql, params)]

    # 附加每个 DAG 的节点统计
    if dags:
        dag_keys = [d["dag_key"] for d in dags]
        stats_rows = fetch_all(
            """
            SELECT dag_key, status, COUNT(*) AS cnt
            FROM task_dag_nodes
            WHERE dag_key = ANY(%s::text[])
            GROUP BY dag_key, status
            """,
            (dag_keys,),
        )
        stats: dict[str, dict[str, int]] = {}
        for sr in stats_rows:
            dk = str(sr.get("dag_key", ""))
            st_name = str(sr.get("status", ""))
            cnt = int(sr.get("cnt", 0))
            stats.setdefault(dk, {})[st_name] = cnt

        for d in dags:
            dk = d["dag_key"]
            node_stats = stats.get(dk, {})
            total = sum(node_stats.values())
            done = node_stats.get("done", 0)
            d["node_total"] = total
            d["node_done"] = done
            d["node_stats"] = node_stats

    return dags


def get_task_dag_detail(dag_key: str) -> Optional[dict[str, Any]]:
    key = _normalize_key("dag_key", dag_key)
    row = fetch_one("SELECT * FROM task_dags WHERE dag_key = %s", (key,))
    if not row:
        return None
    dag = _row_to_dag(row)
    nodes = fetch_all(
        "SELECT * FROM task_dag_nodes WHERE dag_key = %s ORDER BY id", (key,),
    )
    dag["nodes"] = [_row_to_dag_node(n) for n in nodes]
    return dag


def save_dag_node(
    dag_key: str,
    node_key: str,
    title: str = "",
    node_type: str = "task",
    assigned_to: str = "",
    depends_on: Optional[list[str]] = None,
    command_ref: str = "",
    config: Optional[Any] = None,
) -> dict[str, Any]:
    dk = _normalize_key("dag_key", dag_key)
    nk = _normalize_key("node_key", node_key)
    nt = str(node_type or "task").strip().lower()
    if nt not in _NODE_TYPE_SET:
        nt = "task"
    deps = list(depends_on) if isinstance(depends_on, list) else []

    row = fetch_one(
        f"""
        INSERT INTO task_dag_nodes (dag_key, node_key, title, node_type, assigned_to, depends_on, command_ref, config)
        VALUES (%s, %s, %s, %s, %s, %s::jsonb, %s, %s::jsonb)
        ON CONFLICT (dag_key, node_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            node_type = EXCLUDED.node_type,
            assigned_to = EXCLUDED.assigned_to,
            depends_on = EXCLUDED.depends_on,
            command_ref = EXCLUDED.command_ref,
            config = EXCLUDED.config,
            updated_at = NOW()
        {_NODE_RETURNING}
        """,
        (dk, nk, str(title or "").strip(), nt, str(assigned_to or "").strip(),
         _as_json_text(deps), str(command_ref or "").strip(), _as_json_text(config)),
    )
    result = _row_to_dag_node(_require_row(row, "save_dag_node"))
    append_event(
        event_type="task_dag", action="node_save", result="ok",
        target=f"{dk}/{nk}",
    )
    return {"ok": True, "node": result}


def update_dag_node_status(
    dag_key: str,
    node_key: str,
    status: str,
    result: Optional[Any] = None,
    updated_by: str = "",
) -> dict[str, Any]:
    dk = _normalize_key("dag_key", dag_key)
    nk = _normalize_key("node_key", node_key)
    st = str(status or "").strip().lower()
    if st not in _NODE_STATUS_SET:
        raise ValueError(f"无效节点状态: {st}")

    sets = ["status = %s", "updated_at = NOW()"]
    params: list[Any] = [st]

    if st == "running":
        sets.append("started_at = COALESCE(started_at, NOW())")
    elif st in ("done", "failed", "skipped"):
        sets.append("finished_at = NOW()")

    if result is not None:
        sets.append("result = %s::jsonb")
        params.append(_as_json_text(result))

    params.extend([dk, nk])
    row = fetch_one(
        f"UPDATE task_dag_nodes SET {', '.join(sets)} WHERE dag_key = %s AND node_key = %s {_NODE_RETURNING}",
        params,
    )
    if not row:
        return {"ok": False, "error": f"节点未找到: {dk}/{nk}"}

    node = _row_to_dag_node(row)
    append_event(
        event_type="task_dag", action="node_status", result="ok",
        actor=str(updated_by or ""), target=f"{dk}/{nk}", detail=st,
    )
    return {"ok": True, "node": node}


def delete_task_dags(dag_keys: list[str], updated_by: str = "") -> dict[str, Any]:
    if not isinstance(dag_keys, list):
        raise ValueError("dag_keys 必须是数组")
    normalized = list({_normalize_key("dag_key", str(k or "")) for k in dag_keys})
    if not normalized:
        raise ValueError("dag_keys 不能为空")

    # 先删节点再删主表
    fetch_all(
        "DELETE FROM task_dag_nodes WHERE dag_key = ANY(%s::text[]) RETURNING id",
        (normalized,),
    )
    rows = fetch_all(
        "DELETE FROM task_dags WHERE dag_key = ANY(%s::text[]) RETURNING dag_key",
        (normalized,),
    )
    deleted = [str(r.get("dag_key", "")) for r in rows if str(r.get("dag_key", "")).strip()]
    if deleted:
        append_event(
            event_type="task_dag", action="delete", result="ok",
            actor=str(updated_by or ""), target=",".join(deleted[:10]),
            detail=f"deleted={len(deleted)}",
        )
    return {"ok": True, "requested": len(normalized), "deleted": len(deleted), "dag_keys": deleted}
