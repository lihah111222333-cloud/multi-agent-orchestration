"""命令卡执行器：模板渲染、审批流、执行与审计。"""

from __future__ import annotations

__all__ = [
    "prepare_command_card_run",
    "review_command_card_run",
    "execute_command_card_run",
    "execute_command_card",
    "get_command_card_run",
    "list_command_card_runs",
]

import json
import os
import re
import shlex
import subprocess
from datetime import datetime
from typing import Any, Optional

from agent_ops_store import (
    create_interaction,
    get_command_card,
    review_interaction,
)
from audit_log import append_event
from db.postgres import execute, fetch_all, fetch_one

APPROVAL_REQUIRED_RISKS = {"high", "critical"}
AUTO_APPROVE_ALLOWED_RISKS = {"low", "normal"}
DEFAULT_TIMEOUT_SEC = 120
MIN_TIMEOUT_SEC = 1
MAX_TIMEOUT_SEC = 3600
DEFAULT_OUTPUT_LIMIT = 20000
MIN_OUTPUT_LIMIT = 200
MAX_OUTPUT_LIMIT = 200000

_DANGEROUS_COMMAND_PATTERNS = (
    re.compile(r"(?:^|[;&|()\s])rm\s+-rf(?:\s|$)", re.IGNORECASE),
    re.compile(r"(?:^|[;&|()\s])shutdown(?:\s|$)", re.IGNORECASE),
    re.compile(r"(?:^|[;&|()\s])reboot(?:\s|$)", re.IGNORECASE),
    re.compile(r"curl[^\n|]*\|\s*(?:bash|sh)(?:\s|$)", re.IGNORECASE),
    re.compile(r"wget[^\n|]*\|\s*(?:bash|sh)(?:\s|$)", re.IGNORECASE),
)


def _detect_dangerous_pattern(command: str) -> str:
    text = str(command or "").strip()
    if not text:
        return ""

    for pattern in _DANGEROUS_COMMAND_PATTERNS:
        if pattern.search(text):
            return pattern.pattern

    return ""


def _require_row(row: Optional[dict[str, Any]], action: str) -> dict[str, Any]:
    if row is None:
        raise RuntimeError(f"{action} 执行失败：数据库未返回结果")
    return row


def _normalize_timeout(timeout_sec: Optional[int]) -> int:
    raw = timeout_sec
    if raw is None:
        raw = os.getenv("COMMAND_CARD_TIMEOUT_SEC", str(DEFAULT_TIMEOUT_SEC))
    try:
        value = int(raw)
    except (TypeError, ValueError):
        value = DEFAULT_TIMEOUT_SEC
    return max(MIN_TIMEOUT_SEC, min(value, MAX_TIMEOUT_SEC))


def _normalize_output_limit(value: Optional[int] = None) -> int:
    raw = value
    if raw is None:
        raw = os.getenv("COMMAND_CARD_OUTPUT_LIMIT", str(DEFAULT_OUTPUT_LIMIT))
    try:
        size = int(raw)
    except (TypeError, ValueError):
        size = DEFAULT_OUTPUT_LIMIT
    return max(MIN_OUTPUT_LIMIT, min(size, MAX_OUTPUT_LIMIT))


def _parse_params(params: Any) -> dict[str, Any]:
    if params is None:
        return {}
    if isinstance(params, dict):
        return params
    if isinstance(params, str):
        text = params.strip()
        if not text:
            return {}
        loaded = json.loads(text)
        if not isinstance(loaded, dict):
            raise ValueError("params_json 必须是 JSON 对象")
        return loaded
    raise ValueError("params 必须是 dict 或 JSON 字符串")


def _json_text(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False)


def _fmt_dt(value: Any) -> str:
    if isinstance(value, datetime):
        return value.isoformat()
    return str(value or "")


def _row_to_run(row: dict[str, Any]) -> dict[str, Any]:
    requires_review = bool(row.get("requires_review", False))
    return {
        "id": int(row.get("id", 0)),
        "card_key": str(row.get("card_key", "")),
        "requested_by": str(row.get("requested_by", "")),
        "params": row.get("params") if isinstance(row.get("params"), dict) else {},
        "rendered_command": str(row.get("rendered_command", "")),
        "risk_level": str(row.get("risk_level", "normal")),
        "status": str(row.get("status", "")),
        "requires_review": requires_review,
        "execution_mode": "reviewed" if requires_review else "direct",
        "interaction_id": row.get("interaction_id"),
        "output": str(row.get("output", "")),
        "error": str(row.get("error", "")),
        "exit_code": row.get("exit_code"),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
        "executed_at": _fmt_dt(row.get("executed_at")),
    }


def _get_run(run_id: int) -> Optional[dict[str, Any]]:
    row = fetch_one(
        """
        SELECT id, card_key, requested_by, params, rendered_command, risk_level,
               status, requires_review, interaction_id, output, error, exit_code,
               created_at, updated_at, executed_at
        FROM command_card_runs
        WHERE id = %s
        """,
        (int(run_id),),
    )
    return _row_to_run(row) if row else None


def _validate_params(schema: Any, params: dict[str, Any]) -> None:
    if not isinstance(schema, dict) or not schema:
        return

    required: list[str] = []
    expected_type: dict[str, str] = {}

    if isinstance(schema.get("properties"), dict):
        properties = schema.get("properties", {})
        required = [str(item) for item in schema.get("required", []) if str(item)]
        for key, value in properties.items():
            if isinstance(value, dict):
                t = str(value.get("type", "")).strip().lower()
                if t:
                    expected_type[str(key)] = t
            elif isinstance(value, str):
                expected_type[str(key)] = value.strip().lower()
    else:
        for key, value in schema.items():
            key_text = str(key)
            if isinstance(value, dict):
                if bool(value.get("required", True)):
                    required.append(key_text)
                t = str(value.get("type", "")).strip().lower()
                if t:
                    expected_type[key_text] = t
            else:
                required.append(key_text)
                if isinstance(value, str) and value.strip():
                    expected_type[key_text] = value.strip().lower()

    missing = [name for name in required if name not in params]
    if missing:
        raise ValueError(f"缺少参数: {', '.join(sorted(missing))}")

    for key, type_name in expected_type.items():
        if key not in params:
            continue
        value = params[key]
        if type_name in {"int", "integer"} and not isinstance(value, int):
            raise ValueError(f"参数 {key} 需要 int")
        if type_name in {"float", "number"} and not isinstance(value, (int, float)):
            raise ValueError(f"参数 {key} 需要 number")
        if type_name in {"bool", "boolean"} and not isinstance(value, bool):
            raise ValueError(f"参数 {key} 需要 bool")
        if type_name in {"str", "string"} and not isinstance(value, str):
            raise ValueError(f"参数 {key} 需要 string")


def _shell_quote(value: Any) -> str:
    if value is None:
        normalized = ""
    elif isinstance(value, bool):
        normalized = "true" if value else "false"
    else:
        normalized = str(value)
    return shlex.quote(normalized)


def _render_template(template: str, params: dict[str, Any]) -> str:
    safe_params = {key: _shell_quote(value) for key, value in params.items()}
    try:
        return str(template).format(**safe_params)
    except KeyError as exc:
        raise ValueError(f"命令模板缺少参数: {exc.args[0]}") from exc


def prepare_command_card_run(
    card_key: str,
    params: Any,
    requested_by: str = "agent",
    require_review: Optional[bool] = None,
) -> dict[str, Any]:
    key = str(card_key or "").strip()
    if not key:
        return {"ok": False, "message": "card_key 不能为空"}

    card = get_command_card(key)
    if not card:
        return {"ok": False, "message": f"命令卡不存在: {key}"}
    if not card.get("enabled", True):
        return {"ok": False, "message": f"命令卡已禁用: {key}"}

    try:
        params_obj = _parse_params(params)
    except ValueError as exc:
        return {"ok": False, "message": str(exc)}

    try:
        _validate_params(card.get("args_schema"), params_obj)
        rendered = _render_template(card.get("command_template", ""), params_obj)
    except ValueError as exc:
        return {"ok": False, "message": str(exc)}

    risk_level = str(card.get("risk_level", "normal")).strip().lower() or "normal"
    dangerous_pattern = _detect_dangerous_pattern(rendered)

    if require_review is not None:
        needs_review = bool(require_review)
    else:
        needs_review = (risk_level in APPROVAL_REQUIRED_RISKS) or bool(dangerous_pattern)

    status = "pending_review" if needs_review else "ready"

    row = fetch_one(
        """
        INSERT INTO command_card_runs (
            card_key, requested_by, params, rendered_command, risk_level,
            status, requires_review, interaction_id, output, error, exit_code,
            created_at, updated_at
        )
        VALUES (%s, %s, %s::jsonb, %s, %s, %s, %s, NULL, '', '', NULL, NOW(), NOW())
        RETURNING id, card_key, requested_by, params, rendered_command, risk_level,
                  status, requires_review, interaction_id, output, error, exit_code,
                  created_at, updated_at, executed_at
        """,
        (
            key,
            str(requested_by or "agent"),
            _json_text(params_obj),
            rendered,
            risk_level,
            status,
            needs_review,
        ),
    )

    run = _row_to_run(_require_row(row, "prepare_command_card_run"))
    interaction = None

    if needs_review:
        interaction = create_interaction(
            sender=str(requested_by or "agent"),
            receiver="human_reviewer",
            msg_type="command_card_review",
            content=f"card={key}\ncommand={rendered}\nparams={_json_text(params_obj)}",
            thread_id=f"cmdrun:{run['id']}",
            parent_id=None,
            requires_review=True,
            metadata={
                "run_id": run["id"],
                "card_key": key,
                "risk_level": risk_level,
                "dangerous_pattern": dangerous_pattern,
            },
            status="pending",
        )
        execute(
            "UPDATE command_card_runs SET interaction_id = %s, updated_at = NOW() WHERE id = %s",
            (interaction["id"], run["id"]),
        )
        run["interaction_id"] = interaction["id"]

    append_event(
        event_type="command_card_run",
        action="prepare",
        result="pending_review" if needs_review else "ready",
        actor=str(requested_by or "agent"),
        target=key,
        detail=f"run_id={run['id']}",
        extra={
            "risk_level": risk_level,
            "requires_review": needs_review,
            "dangerous_pattern": dangerous_pattern,
        },
    )

    return {
        "ok": True,
        "needs_review": needs_review,
        "dangerous_command": bool(dangerous_pattern),
        "dangerous_pattern": dangerous_pattern,
        "run": run,
        "interaction": interaction,
    }


def review_command_card_run(run_id: int, decision: str, reviewer: str = "", note: str = "") -> dict[str, Any]:
    run = _get_run(run_id)
    if not run:
        return {"ok": False, "message": f"run 不存在: {run_id}"}

    decision_text = str(decision or "").strip().lower()
    if decision_text not in {"approved", "rejected"}:
        return {"ok": False, "message": "decision 必须是 approved/rejected"}

    if run.get("interaction_id"):
        review_interaction(
            interaction_id=int(run["interaction_id"]),
            status=decision_text,
            reviewer=reviewer,
            note=note,
        )

    next_status = "ready" if decision_text == "approved" else "rejected"
    updated = fetch_one(
        """
        UPDATE command_card_runs
        SET status = %s,
            updated_at = NOW()
        WHERE id = %s
        RETURNING id, card_key, requested_by, params, rendered_command, risk_level,
                  status, requires_review, interaction_id, output, error, exit_code,
                  created_at, updated_at, executed_at
        """,
        (next_status, int(run_id)),
    )
    if not updated:
        return {"ok": False, "message": f"run 更新失败: {run_id}"}

    result = _row_to_run(updated)
    append_event(
        event_type="command_card_run",
        action="review",
        result=decision_text,
        actor=str(reviewer or ""),
        target=result.get("card_key", ""),
        detail=f"run_id={run_id}",
    )
    return {"ok": True, "run": result}


def _recover_stale_runs(timeout_sec: Optional[int] = None) -> int:
    """Mark runs stuck in 'running' beyond 2× timeout as failed (crash recovery)."""
    timeout = _normalize_timeout(timeout_sec)
    stale_threshold_sec = max(timeout * 2, 300)  # at least 5 minutes
    count = execute(
        """
        UPDATE command_card_runs
        SET status = 'failed',
            error  = '[timeout_recovery] process crash or timeout',
            exit_code = -3,
            updated_at = NOW()
        WHERE status = 'running'
          AND updated_at < NOW() - INTERVAL '%s seconds'
        """,
        (stale_threshold_sec,),
    )
    if count:
        append_event(
            event_type="command_card_run",
            action="recover_stale",
            result="ok",
            actor="system",
            target="command_card_runs",
            detail=f"recovered {count} stale running task(s)",
        )
    return count


def execute_command_card_run(
    run_id: int,
    actor: str = "agent",
    timeout_sec: Optional[int] = None,
) -> dict[str, Any]:
    # D1: 恢复因进程崩溃而永久卡在 running 的任务
    _recover_stale_runs(timeout_sec)

    run = _get_run(run_id)
    if not run:
        return {"ok": False, "message": f"run 不存在: {run_id}"}

    status = str(run.get("status", ""))
    execution_mode = str(run.get("execution_mode", "reviewed" if run.get("requires_review") else "direct"))
    if status == "pending_review":
        return {"ok": False, "message": f"run 仍待审批: {run_id}", "run": run, "execution_mode": execution_mode}
    if status == "rejected":
        return {"ok": False, "message": f"run 已拒绝: {run_id}", "run": run, "execution_mode": execution_mode}
    if status == "success":
        return {
            "ok": True,
            "run": run,
            "message": "已执行成功，无需重复执行",
            "execution_mode": execution_mode,
        }

    execute(
        "UPDATE command_card_runs SET status = 'running', updated_at = NOW() WHERE id = %s",
        (int(run_id),),
    )

    timeout = _normalize_timeout(timeout_sec)
    output_limit = _normalize_output_limit()
    cmd = str(run.get("rendered_command", "")).strip()
    if not cmd:
        return {"ok": False, "message": "空命令不可执行", "run": run, "execution_mode": execution_mode}

    try:
        argv = shlex.split(cmd)
    except ValueError as exc:
        stdout = ""
        stderr = f"[invalid_command] {exc}"
        exit_code = -2
        final_status = "failed"
    else:
        if not argv:
            stdout = ""
            stderr = "[invalid_command] empty argv"
            exit_code = -2
            final_status = "failed"
        else:
            try:
                proc = subprocess.run(
                    argv,
                    capture_output=True,
                    text=True,
                    timeout=timeout,
                    check=False,
                )
                stdout = (proc.stdout or "")[-output_limit:]
                stderr = (proc.stderr or "")[-output_limit:]
                exit_code = int(proc.returncode)
                final_status = "success" if exit_code == 0 else "failed"
            except FileNotFoundError as exc:
                stdout = ""
                stderr = f"[not_found] {exc}"
                exit_code = 127
                final_status = "failed"
            except subprocess.TimeoutExpired as exc:
                stdout = ((exc.stdout or "") if isinstance(exc.stdout, str) else "")[-output_limit:]
                stderr = ((exc.stderr or "") if isinstance(exc.stderr, str) else "")[-output_limit:]
                if stderr:
                    stderr += "\n"
                stderr += f"[timeout] command exceeded {timeout}s"
                exit_code = -1
                final_status = "failed"

    updated = fetch_one(
        """
        UPDATE command_card_runs
        SET status = %s,
            output = %s,
            error = %s,
            exit_code = %s,
            executed_at = NOW(),
            updated_at = NOW()
        WHERE id = %s
        RETURNING id, card_key, requested_by, params, rendered_command, risk_level,
                  status, requires_review, interaction_id, output, error, exit_code,
                  created_at, updated_at, executed_at
        """,
        (final_status, stdout, stderr, exit_code, int(run_id)),
    )
    if not updated:
        return {"ok": False, "message": f"run 更新失败: {run_id}"}

    result = _row_to_run(updated)
    execution_mode = str(result.get("execution_mode", "reviewed" if result.get("requires_review") else "direct"))
    append_event(
        event_type="command_card_run",
        action="execute",
        result=final_status,
        actor=str(actor or "agent"),
        target=result.get("card_key", ""),
        detail=f"run_id={run_id},exit_code={exit_code}",
    )
    return {"ok": final_status == "success", "run": result, "execution_mode": execution_mode}


def execute_command_card(
    card_key: str,
    params: Any,
    requested_by: str = "agent",
    auto_approve: bool = False,
    reviewer: str = "",
    review_note: str = "",
    timeout_sec: Optional[int] = None,
) -> dict[str, Any]:
    prepared = prepare_command_card_run(
        card_key=card_key,
        params=params,
        requested_by=requested_by,
        require_review=None,
    )
    if not prepared.get("ok"):
        return prepared

    run = prepared.get("run", {})
    run_id = int(run.get("id", 0))
    risk_level = str(run.get("risk_level", "normal")).strip().lower() or "normal"

    execution_mode = str(run.get("execution_mode", "reviewed" if run.get("requires_review") else "direct"))

    if prepared.get("needs_review") and not auto_approve:
        return {
            "ok": True,
            "pending_review": True,
            "run": run,
            "interaction": prepared.get("interaction"),
            "message": "命令已生成，等待人工审批",
            "execution_mode": execution_mode,
        }

    if prepared.get("needs_review") and auto_approve:
        dangerous_command = bool(prepared.get("dangerous_command"))
        if dangerous_command:
            return {
                "ok": True,
                "pending_review": True,
                "run": run,
                "interaction": prepared.get("interaction"),
                "message": "检测到危险命令模式，禁止自动审批，需人工审批",
                "execution_mode": execution_mode,
            }
        if risk_level not in AUTO_APPROVE_ALLOWED_RISKS:
            return {
                "ok": True,
                "pending_review": True,
                "run": run,
                "interaction": prepared.get("interaction"),
                "message": "高风险命令禁止自动审批，需人工审批",
                "execution_mode": execution_mode,
            }
        reviewed = review_command_card_run(
            run_id=run_id,
            decision="approved",
            reviewer=reviewer or requested_by,
            note=review_note,
        )
        if not reviewed.get("ok"):
            return reviewed

    return execute_command_card_run(run_id=run_id, actor=requested_by, timeout_sec=timeout_sec)


def get_command_card_run(run_id: int) -> Optional[dict[str, Any]]:
    return _get_run(run_id)


def list_command_card_runs(
    card_key: str = "",
    status: str = "",
    requested_by: str = "",
    limit: int = 100,
) -> list[dict[str, Any]]:
    where = []
    params: list[Any] = []

    if card_key:
        where.append("card_key = %s")
        params.append(str(card_key).strip())
    if status:
        where.append("status = %s")
        params.append(str(status).strip().lower())
    if requested_by:
        where.append("requested_by = %s")
        params.append(str(requested_by).strip())

    sql_text = """
        SELECT id, card_key, requested_by, params, rendered_command, risk_level,
               status, requires_review, interaction_id, output, error, exit_code,
               created_at, updated_at, executed_at
        FROM command_card_runs
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY created_at DESC, id DESC LIMIT %s"
    params.append(max(1, min(int(limit or 100), 1000)))

    return [_row_to_run(row) for row in fetch_all(sql_text, params)]
