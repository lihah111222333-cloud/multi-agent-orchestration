"""拓扑变更审批流（PostgreSQL 持久化）。

- Master 负责提交拓扑草案
- 人工在 Dashboard 审批（通过/拒绝）
- 仅审批通过后写入 config.json 生效

GO:migrate → go/internal/store/topology_approval.go
GO:target  → Go sqlx + connect_cursor → pgxpool.Pool.Begin()
GO:notes:
  - _row_to_request → Go struct ApprovalRequest `db:"col"`
  - _transition_approval → Go 事务 (pgx.Tx)
  - _arch_hash → Go crypto/sha256
  - APPROVAL_ID_RE → Go regexp.MustCompile
  - _expire_requests / _archive_completed_requests → Go 等价 SQL
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Optional

from dotenv import dotenv_values

from audit_log import append_event
from config.settings import load_architecture_raw, save_architecture
from db.postgres import connect_cursor, execute, fetch_all, fetch_one
from utils import normalize_limit

__all__ = [
    "APPROVAL_ID_HEX_LEN",
    "is_valid_approval_id",
    "list_approvals",
    "get_approval",
    "create_approval",
    "approve_approval",
    "reject_approval",
]

# 环境文件路径（用于动态读取审批 TTL/归档策略）
ENV_FILE = Path(__file__).parent / ".env"

DEFAULT_TTL_SEC = 120
MIN_TTL_SEC = 30
DEFAULT_ARCHIVE_DAYS = 30
MIN_ARCHIVE_DAYS = 1
APPROVAL_ID_HEX_LEN = 16
APPROVAL_ID_RE = re.compile(rf"^[a-f0-9]{{{APPROVAL_ID_HEX_LEN}}}$")
APPROVAL_EXPIRE_NOTE = "审批超时自动过期"
APPROVAL_EXPIRE_ACTOR = "system"





def is_valid_approval_id(value: str) -> bool:
    return APPROVAL_ID_RE.fullmatch(str(value or "")) is not None


def _now() -> datetime:
    return datetime.now(timezone.utc)


def _iso(dt: datetime) -> str:
    return dt.isoformat()


def _arch_hash(architecture: dict) -> str:
    raw = json.dumps(architecture, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()


def _resolve_env_int(env_key: str, default: int, min_value: int) -> int:
    """从环境变量 / .env 文件决议一个整型配置，并 clamp 到 min_value。"""
    env_value = os.getenv(env_key)

    file_value = None
    if ENV_FILE.exists():
        loaded = dotenv_values(ENV_FILE)
        file_value = loaded.get(env_key)

    raw = env_value if env_value not in (None, "") else (file_value or str(default))

    try:
        value = int(raw)
    except (TypeError, ValueError):
        value = default
    return max(value, min_value)


def _resolve_default_ttl_sec() -> int:
    return _resolve_env_int("TOPOLOGY_APPROVAL_TTL_SEC", DEFAULT_TTL_SEC, MIN_TTL_SEC)


def _resolve_archive_days() -> int:
    return _resolve_env_int("TOPOLOGY_APPROVAL_ARCHIVE_DAYS", DEFAULT_ARCHIVE_DAYS, MIN_ARCHIVE_DAYS)


def _normalize_ttl_sec(ttl_sec: Optional[int]) -> int:
    if ttl_sec is None:
        return _resolve_default_ttl_sec()
    try:
        value = int(ttl_sec)
    except (TypeError, ValueError):
        value = _resolve_default_ttl_sec()
    return max(value, MIN_TTL_SEC)


def _is_valid_architecture(architecture: dict) -> bool:
    if not isinstance(architecture, dict):
        return False

    gateways = architecture.get("gateways")
    if not isinstance(gateways, list) or not gateways:
        return False

    for gateway in gateways:
        if not isinstance(gateway, dict):
            return False
        if not str(gateway.get("id", "")).strip():
            return False

        agents = gateway.get("agents")
        if not isinstance(agents, list) or not agents:
            return False

        for agent in agents:
            if not isinstance(agent, dict):
                return False
            if not str(agent.get("id", "")).strip():
                return False

    return True


def _as_architecture(value: Any) -> dict:
    if isinstance(value, dict):
        return value
    if isinstance(value, str):
        try:
            loaded = json.loads(value)
            if isinstance(loaded, dict):
                return loaded
        except json.JSONDecodeError:
            pass
    return {"gateways": []}


def _fmt_dt(value: Any) -> str:
    if isinstance(value, datetime):
        return value.isoformat()
    return str(value or "")


def _row_to_request(row: dict[str, Any]) -> dict:
    return {
        "id": str(row.get("id", "")),
        "status": str(row.get("status", "")),
        "requested_by": str(row.get("requested_by", "")),
        "reason": str(row.get("reason", "")),
        "created_at": _fmt_dt(row.get("created_at")),
        "expire_at": _fmt_dt(row.get("expire_at")),
        "reviewed_at": _fmt_dt(row.get("reviewed_at")),
        "reviewer": str(row.get("reviewer", "")),
        "review_note": str(row.get("review_note", "")),
        "arch_hash": str(row.get("arch_hash", "")),
        "proposed_architecture": _as_architecture(row.get("proposed_architecture")),
    }


def _get_request_row(approval_id: str) -> Optional[dict[str, Any]]:
    return fetch_one(
        """
        SELECT id, status, requested_by, reason, created_at, expire_at,
               reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
        FROM topology_approvals
        WHERE id = %s
        """,
        (approval_id,),
    )


def _expire_request_in_tx(cur, approval_id: str) -> Optional[dict[str, Any]]:
    cur.execute(
        """
        UPDATE topology_approvals
        SET status = 'expired',
            reviewed_at = NOW(),
            reviewer = %s,
            review_note = %s
        WHERE id = %s
          AND status = 'pending'
          AND expire_at < NOW()
        RETURNING id, status, requested_by, reason, created_at, expire_at,
                  reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
        """,
        (
            APPROVAL_EXPIRE_ACTOR,
            APPROVAL_EXPIRE_NOTE,
            approval_id,
        ),
    )
    return cur.fetchone()


def _expire_requests() -> int:
    rows = fetch_all(
        """
        UPDATE topology_approvals
        SET status = 'expired',
            reviewed_at = NOW(),
            reviewer = %s,
            review_note = %s
        WHERE status = 'pending'
          AND expire_at < NOW()
        RETURNING id, reason
        """
        ,
        (
            APPROVAL_EXPIRE_ACTOR,
            APPROVAL_EXPIRE_NOTE,
        ),
    )
    if not rows:
        return 0

    for row in rows:
        append_event(
            event_type="topology_approval",
            action="expire",
            result="expired",
            actor=APPROVAL_EXPIRE_ACTOR,
            target=str(row.get("id", "")),
            detail=str(row.get("reason", "")),
        )

    return len(rows)


def _archive_completed_requests() -> int:
    archive_days = _resolve_archive_days()
    rows = fetch_all(
        """
        WITH moved AS (
            DELETE FROM topology_approvals
            WHERE status IN ('approved', 'rejected', 'expired')
              AND COALESCE(reviewed_at, created_at) < NOW() - (%s * INTERVAL '1 day')
            RETURNING id, status, requested_by, reason, created_at, expire_at,
                      reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
        )
        INSERT INTO topology_approval_archives (
            id, status, requested_by, reason, created_at, expire_at,
            reviewed_at, reviewer, review_note, arch_hash, proposed_architecture, archived_at
        )
        SELECT id, status, requested_by, reason, created_at, expire_at,
               reviewed_at, reviewer, review_note, arch_hash, proposed_architecture, NOW()
        FROM moved
        ON CONFLICT (id) DO NOTHING
        RETURNING id
        """,
        (archive_days,),
    )

    count = len(rows)
    if count > 0:
        append_event(
            event_type="topology_approval",
            action="archive",
            result="ok",
            actor="system",
            target="archive",
            detail=f"archived={count}",
        )
    return count


def list_approvals(status: str = "", limit: int = 50) -> list[dict]:
    """查询审批单列表，自动处理过期和归档。

    Args:
        status: 按状态过滤（pending/approved/rejected/expired）。
        limit: 返回最大条数。

    Returns:
        审批单字典列表，按创建时间倒序。
    """
    _expire_requests()
    _archive_completed_requests()

    max_items = normalize_limit(limit, default=50)
    where = []
    params: list[Any] = []
    if status:
        where.append("status = %s")
        params.append(status)

    sql_text = """
        SELECT id, status, requested_by, reason, created_at, expire_at,
               reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
        FROM topology_approvals
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY created_at DESC LIMIT %s"
    params.append(max_items)

    return [_row_to_request(row) for row in fetch_all(sql_text, params)]


def get_approval(approval_id: str) -> Optional[dict]:
    """根据 ID 获取单个审批单详情。

    自动处理过期和归档。

    Args:
        approval_id: 审批单 ID（16 位十六进制字符串）。

    Returns:
        审批单字典，不存在则返回 None。
    """
    _expire_requests()
    _archive_completed_requests()

    row = _get_request_row(approval_id)
    return _row_to_request(row) if row else None


def create_approval(
    proposed_architecture: dict,
    requested_by: str = "master",
    reason: str = "",
    ttl_sec: Optional[int] = None,
) -> dict:
    """提交拓扑变更审批申请。

    自动跳过与当前拓扑相同的提案，复用已有的同样哈希待审批单。

    Args:
        proposed_architecture: 提案拓扑 JSON 结构。
        requested_by: 申请人标识。
        reason: 申请原因。
        ttl_sec: 审批单过期秒数；None 则读取环境变量。

    Returns:
        包含 ok、deduped、request 等字段的结果字典。
    """
    _expire_requests()
    _archive_completed_requests()

    if not _is_valid_architecture(proposed_architecture):
        append_event(
            event_type="topology_approval",
            action="create",
            result="invalid_input",
            actor=requested_by,
            target="architecture",
            detail="提案拓扑格式无效",
        )
        return {
            "ok": False,
            "reason": "invalid_architecture",
            "message": "提案拓扑格式无效",
        }

    current = load_architecture_raw()
    proposed_hash = _arch_hash(proposed_architecture)
    current_hash = _arch_hash(current)

    if proposed_hash == current_hash:
        append_event(
            event_type="topology_approval",
            action="create",
            result="skipped",
            actor=requested_by,
            target="architecture",
            detail="提案与当前拓扑一致",
        )
        return {
            "ok": False,
            "reason": "no_change",
            "message": "提案与当前拓扑一致，无需审批",
        }

    dup = fetch_one(
        """
        SELECT id, status, requested_by, reason, created_at, expire_at,
               reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
        FROM topology_approvals
        WHERE status = 'pending' AND arch_hash = %s
        ORDER BY created_at DESC
        LIMIT 1
        """,
        (proposed_hash,),
    )
    if dup:
        append_event(
            event_type="topology_approval",
            action="create",
            result="deduped",
            actor=requested_by,
            target=str(dup.get("id", "")),
            detail="复用已有待审批拓扑",
        )
        return {
            "ok": True,
            "deduped": True,
            "request": _row_to_request(dup),
        }

    ttl_value = _normalize_ttl_sec(ttl_sec)
    now = _now()
    approval_id = uuid.uuid4().hex[:APPROVAL_ID_HEX_LEN]
    expire_at = now + timedelta(seconds=ttl_value)

    execute(
        """
        INSERT INTO topology_approvals (
            id, status, requested_by, reason, created_at, expire_at,
            reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
        )
        VALUES (%s, 'pending', %s, %s, %s, %s, NULL, '', '', %s, %s::jsonb)
        """,
        (
            approval_id,
            requested_by,
            reason,
            now,
            expire_at,
            proposed_hash,
            json.dumps(proposed_architecture, ensure_ascii=False),
        ),
    )

    row = _get_request_row(approval_id)
    request = _row_to_request(row) if row else {
        "id": approval_id,
        "status": "pending",
        "requested_by": requested_by,
        "reason": reason,
        "created_at": _iso(now),
        "expire_at": _iso(expire_at),
        "reviewed_at": "",
        "reviewer": "",
        "review_note": "",
        "arch_hash": proposed_hash,
        "proposed_architecture": proposed_architecture,
    }

    append_event(
        event_type="topology_approval",
        action="create",
        result="pending",
        actor=requested_by,
        target=request["id"],
        detail=reason,
        extra={"ttl_sec": ttl_value},
    )
    return {"ok": True, "deduped": False, "request": request}


def _transition_approval(
    approval_id: str,
    target_status: str,
    reviewer: str = "human",
    note: str = "",
) -> dict:
    """审批状态转换通用逻辑（approve / reject 共用）。"""
    action = "approve" if target_status == "approved" else "reject"
    fail_verb = "审批" if action == "approve" else "拒绝"
    state_verb = "批准" if action == "approve" else "拒绝"

    _expire_requests()
    _archive_completed_requests()

    request: Optional[dict] = None
    transition_result = ""
    failure_status = ""
    backup_path = ""

    try:
        with connect_cursor(row_as_dict=True, autocommit=False) as cur:
            cur.execute(
                """
                UPDATE topology_approvals
                SET status = %s,
                    reviewed_at = NOW(),
                    reviewer = %s,
                    review_note = %s
                WHERE id = %s
                  AND status = 'pending'
                  AND expire_at >= NOW()
                RETURNING id, status, requested_by, reason, created_at, expire_at,
                          reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
                """,
                (target_status, reviewer, note, approval_id),
            )
            target = cur.fetchone()
            if target is not None:
                if target_status == "approved":
                    backup_path = save_architecture(
                        _as_architecture(target.get("proposed_architecture"))
                    )
                request = _row_to_request(target)
                transition_result = target_status
            else:
                expired_row = _expire_request_in_tx(cur, approval_id)
                if expired_row is not None:
                    request = _row_to_request(expired_row)
                    transition_result = "expired"
                    failure_status = "expired"
                else:
                    cur.execute(
                        "SELECT status FROM topology_approvals WHERE id = %s",
                        (approval_id,),
                    )
                    current = cur.fetchone()
                    if current is None:
                        transition_result = "not_found"
                    else:
                        failure_status = str(current.get("status", ""))
                        transition_result = "invalid_state"
    except Exception as exc:
        append_event(
            event_type="topology_approval",
            action=action,
            result="error",
            actor=reviewer,
            target=approval_id,
            detail=str(exc),
        )
        return {"ok": False, "message": f"{fail_verb}失败: {exc}"}

    if transition_result == "not_found":
        append_event(
            event_type="topology_approval",
            action=action,
            result="not_found",
            actor=reviewer,
            target=approval_id,
            detail="审批单不存在",
        )
        return {"ok": False, "message": f"审批单不存在: {approval_id}"}

    if transition_result in {"invalid_state", "expired"}:
        if transition_result == "expired":
            append_event(
                event_type="topology_approval",
                action="expire",
                result="expired",
                actor=APPROVAL_EXPIRE_ACTOR,
                target=approval_id,
                detail=str((request or {}).get("reason", "")),
            )
        append_event(
            event_type="topology_approval",
            action=action,
            result="invalid_state",
            actor=reviewer,
            target=approval_id,
            detail=f"当前状态: {failure_status}",
        )
        return {"ok": False, "message": f"审批单状态不可{state_verb}: {failure_status}"}

    if transition_result != target_status or request is None:
        append_event(
            event_type="topology_approval",
            action=action,
            result="error",
            actor=reviewer,
            target=approval_id,
            detail=f"{fail_verb}状态转换失败",
        )
        return {"ok": False, "message": f"{fail_verb}失败: {fail_verb}状态转换失败"}

    extra = {"config_backup": backup_path} if backup_path else None
    append_event(
        event_type="topology_approval",
        action=action,
        result=target_status,
        actor=reviewer,
        target=approval_id,
        detail=note,
        extra=extra,
    )

    result: dict[str, Any] = {"ok": True, "request": request}
    if target_status == "approved":
        result["config_backup"] = backup_path
    return result


def approve_approval(approval_id: str, reviewer: str = "human", note: str = "") -> dict:
    """批准待审批拓扑变更。"""
    return _transition_approval(approval_id, "approved", reviewer, note)


def reject_approval(approval_id: str, reviewer: str = "human", note: str = "") -> dict:
    """拒绝待审批拓扑变更。"""
    return _transition_approval(approval_id, "rejected", reviewer, note)
