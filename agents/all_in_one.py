"""All-in-One Agent — 将所有 Agent 的工具注册到单个 MCP Server"""

from __future__ import annotations

import json
import inspect
from typing import Any
from datetime import datetime, date, timezone
from decimal import Decimal


class _SafeEncoder(json.JSONEncoder):
    """JSON encoder that handles datetime, date, Decimal, etc."""
    def default(self, o):
        if isinstance(o, (datetime, date)):
            return o.isoformat()
        if isinstance(o, Decimal):
            return float(o)
        if isinstance(o, bytes):
            return o.decode("utf-8", errors="replace")
        return super().default(o)


def _safe_json(obj, **kw) -> str:
    """json.dumps with safe encoder for DB rows."""
    return json.dumps(obj, ensure_ascii=False, cls=_SafeEncoder, **kw)

from agent_ops_store import (
    create_interaction as create_interaction_row,
    db_execute as db_execute_sql,
    db_query as db_query_sql,
    get_command_card as get_command_card_row,
    get_prompt_template as get_prompt_template_row,
    list_command_cards as list_command_card_rows,
    list_interactions as list_interaction_rows,
    list_prompt_templates as list_prompt_template_rows,
    review_interaction as review_interaction_row,
    save_command_card as save_command_card_row,
    save_prompt_template as save_prompt_template_row,
    set_command_card_enabled as set_command_card_enabled_row,
    set_prompt_template_enabled as set_prompt_template_enabled_row,
)
from command_card_executor import (
    execute_command_card as execute_command_card_flow,
    execute_command_card_run as execute_command_card_run_flow,
    get_command_card_run as get_command_card_run_row,
    list_command_card_runs as list_command_card_run_rows,
    prepare_command_card_run as prepare_command_card_run_flow,
    review_command_card_run as review_command_card_run_flow,
)
from agents.iterm_bridge import list_iterm_agent_sessions as list_iterm_sessions, read_iterm_output, send_iterm_input
from shared_file_store import (
    delete_file as delete_shared_file,
    list_files as list_shared_files,
    read_file as read_shared_file,
    write_file as write_shared_file,
)


def _parse_json(value: str, fallback: Any) -> Any:
    text = str(value or "").strip()
    if not text:
        return fallback
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return fallback

def _get_project_root() -> str:
    """自动检测项目根目录。"""
    from pathlib import Path
    return str(Path(__file__).resolve().parents[1])


def iterm(
    action: str = "list",
    text: str = "",
    agent_id: str = "",
    agent_name: str = "",
    all_agents: bool = False,
    wait_sec: float = 0.4,
    read_lines: int = 20,
    launch_cmd: str = "codex",
    work_dir: str = "",
    task: str = "",
) -> str:
    """iTerm Agent 会话统一管理工具。

    Args:
        action:
          - "list"       列出所有 Agent 会话
          - "send"       向 Agent 发送输入（需要 text）
          - "read"       读取 Agent 最近输出
          - "clean"      清理已断开的死会话
          - "unregister" 注销指定 agent（需要 agent_id）
          - "clear_all"  清空所有会话记录
        text: 发送的输入内容（send 时必填）
        agent_id: 目标 agent ID（send/read/unregister 时可指定）
        all_agents: 是否对所有 agent 操作（send/read 时）
        wait_sec: 发送后等待秒数（send 时）
        read_lines: 读取行数（send/read 时）
        launch_cmd/work_dir/task: 保留入参，仅兼容调用方；请改用 command_card `launch.wjboot.workspace`
    """
    action = action.strip().lower()

    # ---- list (自动清理空 session_id 的死记录) ----
    if action == "list":
        from pathlib import Path as _P
        sp = _P(__file__).resolve().parents[1] / "data" / "iterm_launch_state.json"
        state_file = str(sp)
        try:
            if sp.exists():
                st = json.loads(sp.read_text("utf-8"))
                before = len(st.get("agents", []))
                st["agents"] = [a for a in st.get("agents", []) if a.get("session_id", "").strip()]
                if len(st["agents"]) < before:
                    st["count"] = len(st["agents"])
                    st["session_ids"] = [a["session_id"] for a in st["agents"] if a.get("session_id")]
                    sp.write_text(json.dumps(st, ensure_ascii=False, indent=2), "utf-8")
        except Exception:
            pass
        return json.dumps(
            list_iterm_sessions(state_file=state_file),
            ensure_ascii=False,
        )

    # ---- send ----
    if action == "send":
        if not text.strip():
            return json.dumps({"ok": False, "error": "send 需要 text 参数"}, ensure_ascii=False)
        return json.dumps(
            send_iterm_input(
                text=text, agent_id=agent_id, all_agents=all_agents,
                wait_sec=wait_sec, read_lines=read_lines, append_enter=True,
            ), ensure_ascii=False,
        )

    # ---- read ----
    if action == "read":
        return json.dumps(
            read_iterm_output(
                agent_id=agent_id, all_agents=all_agents, read_lines=read_lines,
            ), ensure_ascii=False,
        )

    # ---- launch ----
    if action == "launch":
        return json.dumps(
            {
                "ok": False,
                "error_code": "iterm_launch_disabled",
                "error": (
                    "iterm(action='launch') 已禁用；"
                    "请使用 command_card: launch.wjboot.workspace"
                ),
            },
            ensure_ascii=False,
        )

    # ---- clean / unregister / clear_all（会话管理）----
    from pathlib import Path
    state_path = Path(__file__).resolve().parents[1] / "data" / "iterm_launch_state.json"
    try:
        state = json.loads(state_path.read_text("utf-8")) if state_path.exists() else {}
    except Exception:
        state = {}
    agents = state.get("agents", [])

    if action == "clear_all":
        state["agents"], state["count"], state["session_ids"] = [], 0, []
        state_path.write_text(json.dumps(state, ensure_ascii=False, indent=2), "utf-8")
        return json.dumps({"ok": True, "message": "已清空所有记录", "removed": len(agents)}, ensure_ascii=False)

    if action == "unregister":
        if not agent_id.strip():
            return json.dumps({"ok": False, "error": "需要指定 agent_id"}, ensure_ascii=False)
        before = len(agents)
        state["agents"] = [a for a in agents if a.get("agent_id") != agent_id.strip()]
        state["count"] = len(state["agents"])
        state_path.write_text(json.dumps(state, ensure_ascii=False, indent=2), "utf-8")
        return json.dumps({"ok": True, "message": f"已注销 {agent_id}", "removed": before - len(state["agents"])}, ensure_ascii=False)

    # clean (default for unknown actions)
    before = len(agents)
    live_ids = set()
    try:
        from agents.iterm_bridge import _list_live_sessions
        _, live_sessions = _list_live_sessions()
        live_ids = {s.get("session_id") for s in live_sessions if s.get("session_id")}
    except Exception:
        pass

    cleaned = [a for a in agents
               if a.get("session_id", "").strip()
               and (not live_ids or a["session_id"] in live_ids)]

    state["agents"] = cleaned
    state["count"] = len(cleaned)
    state["session_ids"] = [a["session_id"] for a in cleaned if a.get("session_id")]
    state_path.write_text(json.dumps(state, ensure_ascii=False, indent=2), "utf-8")
    return json.dumps({"ok": True, "message": f"清理完成，移除 {before - len(cleaned)} 个死会话",
                       "removed": before - len(cleaned), "remaining": len(cleaned)}, ensure_ascii=False)


def shared_file(action: str = "list", path: str = "", content: str = "", limit: int = 200) -> str:
    """共享文件管理工具（PostgreSQL 存储）。

    Args:
        action:
          - "write"  写入文件（需要 path + content）
          - "read"   读取文件（需要 path）
          - "list"   列出文件（可选 path 前缀过滤）
          - "delete" 删除文件（需要 path）
        path: 文件路径
        content: 文件内容（write 时必填）
        limit: 列表数量限制（list 时）
    """
    action = action.strip().lower()

    if action == "write":
        if not path.strip():
            return json.dumps({"ok": False, "error": "write 需要 path"}, ensure_ascii=False)
        return json.dumps(write_shared_file(path=path, content=content, actor="mcp"), ensure_ascii=False)

    if action == "read":
        if not path.strip():
            return json.dumps({"ok": False, "error": "read 需要 path"}, ensure_ascii=False)
        result = read_shared_file(path=path)
        return json.dumps(result or {"ok": False, "message": "not_found", "path": path}, ensure_ascii=False)

    if action == "delete":
        if not path.strip():
            return json.dumps({"ok": False, "error": "delete 需要 path"}, ensure_ascii=False)
        return json.dumps(delete_shared_file(path=path, actor="mcp"), ensure_ascii=False)

    # list (default)
    rows = list_shared_files(prefix=path, limit=limit)
    return json.dumps({"ok": True, "count": len(rows), "files": rows}, ensure_ascii=False)


# ---- Agent 交互记录 ----
def interaction(
    action: str = "list",
    sender: str = "",
    receiver: str = "",
    msg_type: str = "",
    content: str = "",
    thread_id: str = "",
    parent_id: int | None = None,
    requires_review: bool = False,
    metadata_json: str = "",
    status: str = "",
    interaction_id: int | None = None,
    reviewer: str = "",
    note: str = "",
    limit: int = 100,
) -> str:
    """Agent 交互记录管理工具。

    Args:
        action:
          - "create"  创建交互记录（需要 sender, receiver, msg_type, content）
          - "list"    查询交互记录（可选 thread_id/sender/receiver 等过滤）
          - "review"  审核交互记录（需要 interaction_id + status）
          - "roster"  获取所有已知 Agent 角色/ID 列表（用于发现其他 Agent）
          - "register" 注册 Agent 能力声明（需要 sender + content 填写技能描述）
        sender/receiver: 发送方/接收方
        msg_type: 消息类型
        content: 消息内容（register 时为技能描述，如 "Python,数据分析,代码审查"）
        thread_id: 会话线程 ID
        parent_id: 父消息 ID
        requires_review: 是否需要审核
        metadata_json: 元数据 JSON 字符串
        status: 状态（create 时默认 pending，review 时为新状态）
        interaction_id: 交互 ID（review 时必填）
        reviewer: 审核人（review 时）
        note: 审核备注（review 时）
        limit: 列表数量限制
    """
    action = action.strip().lower()

    if action == "register":
        if not sender.strip():
            return json.dumps({"ok": False, "error": "register 需要 sender (agent_id)"}, ensure_ascii=False)
        from pathlib import Path as _P
        reg_path = _P(__file__).resolve().parents[1] / "data" / "agent_registry.json"
        reg_path.parent.mkdir(parents=True, exist_ok=True)
        try:
            registry = json.loads(reg_path.read_text("utf-8")) if reg_path.exists() else {}
        except Exception:
            registry = {}
        skills = [s.strip() for s in content.split(",") if s.strip()] if content.strip() else []
        registry[sender.strip()] = {
            "agent_id": sender.strip(),
            "agent_name": receiver.strip() or sender.strip(),
            "skills": skills,
            "registered_at": __import__("datetime").datetime.now(__import__("datetime").timezone.utc).isoformat(),
        }
        reg_path.write_text(json.dumps(registry, ensure_ascii=False, indent=2), "utf-8")
        return json.dumps({"ok": True, "agent": registry[sender.strip()]}, ensure_ascii=False)

    if action == "roster":
        roster = []
        # 从 agent_registry 获取能力声明
        agent_skills = {}
        try:
            from pathlib import Path as _P
            reg_path = _P(__file__).resolve().parents[1] / "data" / "agent_registry.json"
            if reg_path.exists():
                registry = json.loads(reg_path.read_text("utf-8"))
                for aid, info in registry.items():
                    agent_skills[aid] = info.get("skills", [])
        except Exception:
            pass
        # 从 iTerm state 获取在线 agent
        try:
            from pathlib import Path as _P
            sp = _P(__file__).resolve().parents[1] / "data" / "iterm_launch_state.json"
            if sp.exists():
                st = json.loads(sp.read_text("utf-8"))
                for a in st.get("agents", []):
                    aid = a.get("agent_id", "")
                    roster.append({
                        "agent_id": aid,
                        "agent_name": a.get("agent_name", ""),
                        "session_id": a.get("session_id", ""),
                        "skills": agent_skills.get(aid, []),
                        "source": "iterm",
                        "online": bool(a.get("session_id", "").strip()),
                    })
        except Exception:
            pass
        # 从 DB 交互记录中提取曾出现的 agent（补充离线角色）
        try:
            known_ids = {r["agent_id"] for r in roster}
            rows = list_interaction_rows(limit=500)
            for row in rows:
                for field in ("sender", "receiver"):
                    aid = row.get(field, "")
                    if aid and aid not in known_ids:
                        roster.append({"agent_id": aid, "agent_name": aid, "skills": agent_skills.get(aid, []), "source": "db", "online": False})
                        known_ids.add(aid)
        except Exception:
            pass
        # 补充 registry 中有但未出现在 iTerm/DB 的 agent
        known_ids = {r["agent_id"] for r in roster}
        for aid, info in agent_skills.items():
            if aid not in known_ids:
                roster.append({"agent_id": aid, "agent_name": aid, "skills": info, "source": "registry", "online": False})
        # 始终包含 master
        if not any(r.get("agent_id") == "master" for r in roster):
            roster.insert(0, {"agent_id": "master", "agent_name": "主 Agent", "skills": ["编排", "任务分配", "审批"], "source": "builtin", "online": True})
        return json.dumps({"ok": True, "count": len(roster), "agents": roster}, ensure_ascii=False)

    if action == "create":
        row = create_interaction_row(
            sender=sender, receiver=receiver, msg_type=msg_type, content=content,
            thread_id=thread_id, parent_id=parent_id, requires_review=requires_review,
            metadata=_parse_json(metadata_json, {}), status=status or "pending",
        )
        return json.dumps({"ok": True, "interaction": row}, ensure_ascii=False)

    if action == "review":
        if not interaction_id:
            return json.dumps({"ok": False, "error": "review 需要 interaction_id"}, ensure_ascii=False)
        result = review_interaction_row(interaction_id=interaction_id, status=status, reviewer=reviewer, note=note)
        return json.dumps(result, ensure_ascii=False)

    # list (default)
    rows = list_interaction_rows(
        thread_id=thread_id, sender=sender, receiver=receiver,
        msg_type=msg_type, status=status, requires_review=requires_review if action != "list" else None,
        limit=limit,
    )
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


# ---- 提示词模板 ----
def prompt_template(
    action: str = "list",
    prompt_key: str = "",
    title: str = "",
    prompt_text: str = "",
    agent_key: str = "",
    tool_name: str = "",
    variables_json: str = "",
    tags_json: str = "",
    enabled: bool = True,
    keyword: str = "",
    enabled_only: bool = False,
    updated_by: str = "mcp",
    limit: int = 100,
) -> str:
    """提示词模板管理工具。

    Args:
        action:
          - "save"    保存/更新模板（需要 prompt_key, title, prompt_text）
          - "get"     读取模板（需要 prompt_key）
          - "list"    查询模板列表
          - "toggle"  启用/停用模板（需要 prompt_key + enabled）
        prompt_key: 模板唯一标识
        title: 标题
        prompt_text: 提示词正文
        agent_key: Agent 标识（过滤用）
        tool_name: 工具名（过滤用）
        variables_json: 变量 JSON
        tags_json: 标签 JSON
        enabled: 是否启用
        keyword: 搜索关键词
        enabled_only: 只查启用的
        limit: 列表数量限制
    """
    action = action.strip().lower()

    if action == "save":
        row = save_prompt_template_row(
            prompt_key=prompt_key, title=title, prompt_text=prompt_text,
            agent_key=agent_key, tool_name=tool_name,
            variables=_parse_json(variables_json, {}), tags=_parse_json(tags_json, []),
            enabled=enabled, updated_by=updated_by,
        )
        return json.dumps({"ok": True, "prompt": row}, ensure_ascii=False)

    if action == "get":
        row = get_prompt_template_row(prompt_key=prompt_key)
        return json.dumps({"ok": bool(row), "prompt": row}, ensure_ascii=False)

    if action == "toggle":
        result = set_prompt_template_enabled_row(prompt_key=prompt_key, enabled=enabled, updated_by=updated_by)
        return json.dumps(result, ensure_ascii=False)

    # list (default)
    rows = list_prompt_template_rows(
        agent_key=agent_key, tool_name=tool_name, keyword=keyword,
        enabled_only=enabled_only, limit=limit,
    )
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


# ---- 命令卡（含执行） ----
def command_card(
    action: str = "list",
    card_key: str = "",
    title: str = "",
    command_template: str = "",
    description: str = "",
    args_schema_json: str = "",
    risk_level: str = "",
    enabled: bool = True,
    keyword: str = "",
    enabled_only: bool = False,
    updated_by: str = "mcp",
    params_json: str = "",
    requested_by: str = "",
    require_review: bool | None = None,
    auto_approve: bool = False,
    run_id: int | None = None,
    decision: str = "",
    reviewer: str = "",
    review_note: str = "",
    actor: str = "agent",
    timeout_sec: int | None = None,
    status: str = "",
    limit: int = 100,
) -> str:
    """命令卡管理与执行工具。

    Args:
        action:
          - "save"      保存/更新命令卡
          - "get"       读取命令卡
          - "list"      查询命令卡列表
          - "toggle"    启用/停用命令卡
          - "prepare"   准备执行（渲染命令，可触发审批）
          - "review"    审核执行（需要 run_id + decision）
          - "exec_run"  执行指定 run_id
          - "exec"      一键执行（准备→可选审批→执行）
          - "get_run"   查看执行详情（需要 run_id）
          - "list_runs" 查询执行流水
        card_key: 命令卡唯一标识
        title/command_template/description: 命令卡内容
        params_json: 执行参数 JSON
        run_id: 执行流水 ID
        decision: 审核决定（approved/rejected）
        auto_approve: 一键执行时是否自动审批
        timeout_sec: 执行超时秒数
    """
    action = action.strip().lower()

    # 命令卡 CRUD
    if action == "save":
        row = save_command_card_row(
            card_key=card_key, title=title, command_template=command_template,
            description=description, args_schema=_parse_json(args_schema_json, {}),
            risk_level=risk_level, enabled=enabled, updated_by=updated_by,
        )
        return json.dumps({"ok": True, "command_card": row}, ensure_ascii=False)

    if action == "get":
        row = get_command_card_row(card_key=card_key)
        return json.dumps({"ok": bool(row), "command_card": row}, ensure_ascii=False)

    if action == "toggle":
        result = set_command_card_enabled_row(card_key=card_key, enabled=enabled, updated_by=updated_by)
        return json.dumps(result, ensure_ascii=False)

    # 命令卡执行
    if action == "prepare":
        result = prepare_command_card_run_flow(
            card_key=card_key, params=_parse_json(params_json, {}),
            requested_by=requested_by, require_review=require_review,
        )
        return _safe_json(result)

    if action == "review_run" or action == "review":
        if not run_id:
            return json.dumps({"ok": False, "error": "review 需要 run_id"}, ensure_ascii=False)
        result = review_command_card_run_flow(run_id=run_id, decision=decision, reviewer=reviewer, note=review_note)
        return _safe_json(result)

    if action == "exec_run":
        if not run_id:
            return json.dumps({"ok": False, "error": "exec_run 需要 run_id"}, ensure_ascii=False)
        result = execute_command_card_run_flow(run_id=run_id, actor=actor, timeout_sec=timeout_sec)
        return _safe_json(result)

    if action == "exec":
        result = execute_command_card_flow(
            card_key=card_key, params=_parse_json(params_json, {}),
            requested_by=requested_by, auto_approve=auto_approve,
            reviewer=reviewer, review_note=review_note, timeout_sec=timeout_sec,
        )
        return _safe_json(result)

    if action == "get_run":
        if not run_id:
            return json.dumps({"ok": False, "error": "get_run 需要 run_id"}, ensure_ascii=False)
        run = get_command_card_run_row(run_id=run_id)
        return _safe_json({"ok": bool(run), "run": run})

    if action == "list_runs":
        rows = list_command_card_run_rows(card_key=card_key, status=status, requested_by=requested_by, limit=limit)
        return _safe_json({"ok": True, "count": len(rows), "rows": rows})

    # list (default)
    rows = list_command_card_rows(keyword=keyword, risk_level=risk_level, enabled_only=enabled_only, limit=limit)
    return _safe_json({"ok": True, "count": len(rows), "rows": rows})


# ---- 通用 SQL ----
def db(action: str = "query", sql: str = "", limit: int = 200) -> str:
    """数据库操作工具。

    Args:
        action:
          - "query"   只读查询（SELECT）
          - "execute" 变更操作（INSERT/UPDATE/DELETE）
        sql: SQL 语句
        limit: 查询结果限制（query 时）
    """
    action = action.strip().lower()
    if not sql.strip():
        return json.dumps({"ok": False, "error": "需要 sql 参数"}, ensure_ascii=False)

    if action == "execute":
        result = db_execute_sql(sql_text=sql)
        return _safe_json(result)

    # query (default)
    rows = db_query_sql(sql_text=sql, limit=limit)
    return _safe_json({"ok": True, "count": len(rows), "rows": rows})


# ---- 任务管理 ----
def task(
    action: str = "list",
    task_id: str = "",
    title: str = "",
    description: str = "",
    assignee: str = "",
    creator: str = "",
    priority: str = "normal",
    status: str = "",
    result: str = "",
    depends_on: str = "",
    project_id: str = "",
    timeout_sec: int = 0,
    max_retries: int = 0,
    idempotency_key: str = "",
    limit: int = 100,
) -> str:
    """Agent 任务管理工具。支持任务依赖(DAG)、项目分组、超时重试。

    Args:
        action:
          - "create"   创建任务
          - "list"     查询任务（可按 assignee/status/priority/project_id 过滤）
          - "get"      获取任务详情
          - "update"   更新状态/结果（失败时自动重试）
          - "assign"   分配/转派任务
          - "ready"    查询所有依赖已完成、可以开始的任务
          - "progress" 项目级进度统计
          - "cancel"   取消任务
        task_id: 任务 ID
        title: 任务标题
        description: 任务描述
        assignee: 负责人 agent_id
        creator: 创建者 agent_id
        priority: 优先级（low/normal/high/critical）
        status: 状态（pending/in_progress/blocked/done/failed/cancelled）
        result: 任务结果/产出摘要
        depends_on: 依赖的 task_id，逗号分隔，如 "T001,T002"
        project_id: 项目分组标识
        timeout_sec: 超时秒数（0=不限）
        max_retries: 最大重试次数（0=不重试）
        limit: 列表数量限制
    """
    from pathlib import Path
    from datetime import datetime, timezone
    store = Path(__file__).resolve().parents[1] / "data" / "agent_tasks.json"
    store.parent.mkdir(parents=True, exist_ok=True)

    try:
        tasks = json.loads(store.read_text("utf-8")) if store.exists() else []
    except Exception:
        tasks = []

    action = action.strip().lower()
    _save = lambda: store.write_text(json.dumps(tasks, ensure_ascii=False, indent=2), "utf-8")

    if action == "create":
        if not title.strip():
            return json.dumps({"ok": False, "error": "create 需要 title"}, ensure_ascii=False)
        # 幂等键防重复
        ikey = idempotency_key.strip()
        if ikey:
            dup = next((t for t in tasks if t.get("idempotency_key") == ikey), None)
            if dup:
                return json.dumps({"ok": True, "task": dup, "duplicate": True}, ensure_ascii=False)
        now = datetime.now(timezone.utc).isoformat()
        deps = [d.strip() for d in depends_on.split(",") if d.strip()] if depends_on.strip() else []
        new_task = {
            "task_id": f"T{int(datetime.now(timezone.utc).timestamp()*1000) % 100000000}",
            "title": title.strip(),
            "description": description.strip(),
            "creator": creator.strip() or "unknown",
            "assignee": assignee.strip(),
            "priority": priority.strip() or "normal",
            "status": "pending",
            "result": "",
            "project_id": project_id.strip(),
            "depends_on": deps,
            "timeout_sec": max(0, int(timeout_sec)),
            "max_retries": max(0, int(max_retries)),
            "retry_count": 0,
            "idempotency_key": ikey,
            "created_at": now,
            "updated_at": now,
        }
        tasks.append(new_task)
        _save()
        return json.dumps({"ok": True, "task": new_task}, ensure_ascii=False)

    if action == "get":
        if not task_id.strip():
            return json.dumps({"ok": False, "error": "get 需要 task_id"}, ensure_ascii=False)
        t = next((t for t in tasks if t.get("task_id") == task_id.strip()), None)
        return json.dumps({"ok": bool(t), "task": t}, ensure_ascii=False)

    if action == "update":
        if not task_id.strip():
            return json.dumps({"ok": False, "error": "update 需要 task_id"}, ensure_ascii=False)
        for t in tasks:
            if t.get("task_id") == task_id.strip():
                auto_retried = False
                if status.strip():
                    if status.strip() == "failed" and t.get("max_retries", 0) > t.get("retry_count", 0):
                        t["retry_count"] = t.get("retry_count", 0) + 1
                        t["status"] = "pending"
                        t["result"] = f"[重试 {t['retry_count']}/{t['max_retries']}] {result.strip()}"
                        auto_retried = True
                    else:
                        t["status"] = status.strip()
                if result.strip() and not auto_retried:
                    t["result"] = result.strip()
                if description.strip():
                    t["description"] = description.strip()
                t["updated_at"] = datetime.now(timezone.utc).isoformat()
                _save()
                return json.dumps({"ok": True, "task": t, "auto_retried": auto_retried}, ensure_ascii=False)
        return json.dumps({"ok": False, "error": f"未找到 task_id={task_id}"}, ensure_ascii=False)

    if action == "assign":
        if not task_id.strip() or not assignee.strip():
            return json.dumps({"ok": False, "error": "assign 需要 task_id + assignee"}, ensure_ascii=False)
        for t in tasks:
            if t.get("task_id") == task_id.strip():
                t["assignee"] = assignee.strip()
                t["updated_at"] = datetime.now(timezone.utc).isoformat()
                _save()
                return json.dumps({"ok": True, "task": t}, ensure_ascii=False)
        return json.dumps({"ok": False, "error": f"未找到 task_id={task_id}"}, ensure_ascii=False)

    if action == "cancel":
        if not task_id.strip():
            return json.dumps({"ok": False, "error": "cancel 需要 task_id"}, ensure_ascii=False)
        for t in tasks:
            if t.get("task_id") == task_id.strip():
                t["status"] = "cancelled"
                t["updated_at"] = datetime.now(timezone.utc).isoformat()
                _save()
                return json.dumps({"ok": True, "task": t}, ensure_ascii=False)
        return json.dumps({"ok": False, "error": f"未找到 task_id={task_id}"}, ensure_ascii=False)

    if action == "ready":
        done_ids = {t.get("task_id") for t in tasks if t.get("status") in ("done", "cancelled")}
        ready_tasks = []
        for t in tasks:
            if t.get("status") != "pending":
                continue
            deps = t.get("depends_on", [])
            if not deps or all(d in done_ids for d in deps):
                ready_tasks.append(t)
        if project_id.strip():
            ready_tasks = [t for t in ready_tasks if t.get("project_id") == project_id.strip()]
        return json.dumps({"ok": True, "count": len(ready_tasks), "tasks": ready_tasks}, ensure_ascii=False)

    if action == "progress":
        target = tasks
        if project_id.strip():
            target = [t for t in tasks if t.get("project_id") == project_id.strip()]
        total = len(target)
        if total == 0:
            return json.dumps({"ok": True, "total": 0, "message": "无任务"}, ensure_ascii=False)
        by_status = {}
        for t in target:
            s = t.get("status", "unknown")
            by_status[s] = by_status.get(s, 0) + 1
        done = by_status.get("done", 0) + by_status.get("cancelled", 0)
        pct = round(done / total * 100, 1)
        return json.dumps({"ok": True, "total": total, "progress_pct": pct, "by_status": by_status}, ensure_ascii=False)

    # list (default)
    filtered = tasks
    if assignee.strip():
        filtered = [t for t in filtered if t.get("assignee") == assignee.strip()]
    if status.strip():
        filtered = [t for t in filtered if t.get("status") == status.strip()]
    if priority.strip() and priority.strip() != "normal":
        filtered = [t for t in filtered if t.get("priority") == priority.strip()]
    if project_id.strip():
        filtered = [t for t in filtered if t.get("project_id") == project_id.strip()]
    return json.dumps({"ok": True, "count": len(filtered[:limit]), "tasks": filtered[:limit]}, ensure_ascii=False)


# ---- 审批/错误处理 ----
def approval(
    action: str = "list",
    approval_id: str = "",
    requester: str = "",
    approver: str = "",
    target_agent: str = "",
    title: str = "",
    description: str = "",
    options_json: str = "",
    decision: str = "",
    reason: str = "",
    status: str = "",
    limit: int = 100,
) -> str:
    """审批与错误处理工具。Agent 遇到错误/需要决策时，向指定 Agent 发起审批请求。

    Args:
        action:
          - "request"  发起审批请求（需要 title, target_agent, 可选 options_json）
          - "respond"  回复审批（需要 approval_id + decision）
          - "list"     查询审批列表（可按 target_agent/status 过滤）
          - "get"      获取审批详情（需要 approval_id）
        approval_id: 审批 ID（respond/get 时必填）
        requester: 发起方 agent_id
        approver: 审批方 agent_id（respond 时记录谁做的决定）
        target_agent: 目标审批人 agent_id（request 时指定谁来审批）
        title: 审批标题（错误描述/决策问题）
        description: 详细描述（错误堆栈/上下文）
        options_json: 可选方案 JSON 数组，如 '["重试","跳过","中止"]'
        decision: 审批决定（respond 时必填）
        reason: 决定理由
        status: 过滤状态（pending/approved/rejected/resolved）
        limit: 列表数量限制
    """
    from pathlib import Path
    from datetime import datetime, timezone
    store = Path(__file__).resolve().parents[1] / "data" / "agent_approvals.json"
    store.parent.mkdir(parents=True, exist_ok=True)

    try:
        items = json.loads(store.read_text("utf-8")) if store.exists() else []
    except Exception:
        items = []

    action = action.strip().lower()

    if action == "request":
        if not title.strip():
            return json.dumps({"ok": False, "error": "request 需要 title"}, ensure_ascii=False)
        if not target_agent.strip():
            return json.dumps({"ok": False, "error": "request 需要 target_agent（指定谁来审批）"}, ensure_ascii=False)
        now = datetime.now(timezone.utc).isoformat()
        new_item = {
            "approval_id": f"A{int(datetime.now(timezone.utc).timestamp()*1000) % 100000000}",
            "requester": requester.strip() or "unknown",
            "target_agent": target_agent.strip(),
            "title": title.strip(),
            "description": description.strip(),
            "options": _parse_json(options_json, []),
            "status": "pending",
            "decision": "",
            "approver": "",
            "reason": "",
            "created_at": now,
            "resolved_at": "",
        }
        items.append(new_item)
        store.write_text(json.dumps(items, ensure_ascii=False, indent=2), "utf-8")
        return json.dumps({"ok": True, "approval": new_item}, ensure_ascii=False)

    if action == "respond":
        if not approval_id.strip() or not decision.strip():
            return json.dumps({"ok": False, "error": "respond 需要 approval_id + decision"}, ensure_ascii=False)
        for item in items:
            if item.get("approval_id") == approval_id.strip():
                item["decision"] = decision.strip()
                item["approver"] = approver.strip() or "unknown"
                item["reason"] = reason.strip()
                item["status"] = "resolved"
                item["resolved_at"] = datetime.now(timezone.utc).isoformat()
                store.write_text(json.dumps(items, ensure_ascii=False, indent=2), "utf-8")
                return json.dumps({"ok": True, "approval": item}, ensure_ascii=False)
        return json.dumps({"ok": False, "error": f"未找到 approval_id={approval_id}"}, ensure_ascii=False)

    if action == "get":
        if not approval_id.strip():
            return json.dumps({"ok": False, "error": "get 需要 approval_id"}, ensure_ascii=False)
        item = next((i for i in items if i.get("approval_id") == approval_id.strip()), None)
        return json.dumps({"ok": bool(item), "approval": item}, ensure_ascii=False)

    # list (default)
    filtered = items
    if target_agent.strip():
        filtered = [i for i in filtered if i.get("target_agent") == target_agent.strip()]
    if requester.strip():
        filtered = [i for i in filtered if i.get("requester") == requester.strip()]
    if status.strip():
        filtered = [i for i in filtered if i.get("status") == status.strip()]
    return json.dumps({"ok": True, "count": len(filtered[:limit]), "approvals": filtered[:limit]}, ensure_ascii=False)


# ---- 资源锁/租约 ----
def lock(
    action: str = "list",
    resource: str = "",
    owner: str = "",
    ttl_sec: int = 300,
) -> str:
    """资源锁/租约工具。防止多个 Agent 抢同一任务或重复写入。

    Args:
        action:
          - "acquire"       获取锁（需要 resource + owner，可选 ttl_sec）
          - "release"       释放锁（需要 resource + owner）
          - "list"          列出所有活跃锁
          - "force_release" 强制释放（需要 resource）
        resource: 资源标识（如 task_id, file_path）
        owner: 锁持有者 agent_id
        ttl_sec: 锁生存时间（秒），默认 300，到期自动失效
    """
    from pathlib import Path
    from datetime import datetime, timezone, timedelta
    store = Path(__file__).resolve().parents[1] / "data" / "agent_locks.json"
    store.parent.mkdir(parents=True, exist_ok=True)
    try:
        locks = json.loads(store.read_text("utf-8")) if store.exists() else {}
    except Exception:
        locks = {}

    now = datetime.now(timezone.utc)
    now_iso = now.isoformat()
    # 自动清理过期锁
    expired = [k for k, v in locks.items()
               if v.get("expires_at") and datetime.fromisoformat(v["expires_at"]) < now]
    for k in expired:
        del locks[k]

    action = action.strip().lower()
    _save = lambda: store.write_text(json.dumps(locks, ensure_ascii=False, indent=2), "utf-8")

    if action == "acquire":
        if not resource.strip() or not owner.strip():
            return json.dumps({"ok": False, "error": "acquire 需要 resource + owner"}, ensure_ascii=False)
        r = resource.strip()
        existing = locks.get(r)
        if existing:
            if existing.get("owner") == owner.strip():
                existing["expires_at"] = (now + timedelta(seconds=max(30, int(ttl_sec)))).isoformat()
                existing["renewed_at"] = now_iso
                _save()
                return json.dumps({"ok": True, "lock": existing, "renewed": True}, ensure_ascii=False)
            return json.dumps({"ok": False, "error": f"资源 {r} 已被 {existing['owner']} 锁定",
                               "lock": existing}, ensure_ascii=False)
        locks[r] = {"resource": r, "owner": owner.strip(), "acquired_at": now_iso,
                     "expires_at": (now + timedelta(seconds=max(30, int(ttl_sec)))).isoformat()}
        _save()
        return json.dumps({"ok": True, "lock": locks[r]}, ensure_ascii=False)

    if action == "release":
        if not resource.strip() or not owner.strip():
            return json.dumps({"ok": False, "error": "release 需要 resource + owner"}, ensure_ascii=False)
        r = resource.strip()
        existing = locks.get(r)
        if not existing:
            return json.dumps({"ok": True, "message": f"资源 {r} 未被锁定"}, ensure_ascii=False)
        if existing.get("owner") != owner.strip():
            return json.dumps({"ok": False, "error": f"资源 {r} 由 {existing['owner']} 持有"}, ensure_ascii=False)
        del locks[r]
        _save()
        return json.dumps({"ok": True, "message": f"已释放 {r}"}, ensure_ascii=False)

    if action == "force_release":
        if not resource.strip():
            return json.dumps({"ok": False, "error": "force_release 需要 resource"}, ensure_ascii=False)
        removed = locks.pop(resource.strip(), None)
        _save()
        return json.dumps({"ok": True, "message": f"已强制释放 {resource.strip()}",
                           "was_held_by": removed.get("owner") if removed else None}, ensure_ascii=False)

    # list (default)
    _save()
    return json.dumps({"ok": True, "count": len(locks), "locks": list(locks.values()),
                        "expired_cleaned": len(expired)}, ensure_ascii=False)


# ---- 看门狗定时唤醒 tool ----
def agent_watchdog(action: str = "start", interval_sec: int = 120, prompt: str = "") -> str:
    """看门狗定时器 — 防止 Agent 对话中断。定期向所有 Agent 发送唤醒提示词。

    Args:
        action: "start" 启动, "stop" 停止, "status" 查看状态
        interval_sec: 唤醒间隔（秒），最小30秒，默认120秒（仅 start 时生效）
        prompt: 自定义唤醒提示词，留空使用默认（仅 start 时生效）
    """
    import os
    from tg_bridge import start_watchdog, stop_watchdog, is_watchdog_running, get_watchdog_info

    action = action.strip().lower()

    if action == "stop":
        stop_watchdog()
        return json.dumps({"ok": True, "message": "看门狗已停止", **get_watchdog_info()}, ensure_ascii=False)

    if action == "status":
        return json.dumps({"ok": True, **get_watchdog_info()}, ensure_ascii=False)

    # start
    if interval_sec:
        os.environ["TG_WATCHDOG_INTERVAL"] = str(max(30, int(interval_sec)))
    if prompt.strip():
        os.environ["TG_WATCHDOG_PROMPT"] = prompt.strip()

    if is_watchdog_running():
        return json.dumps({"ok": True, "message": "看门狗已在运行", **get_watchdog_info()}, ensure_ascii=False)

    start_watchdog()
    return json.dumps({"ok": True, "message": f"看门狗已启动，每 {max(30, int(interval_sec))}s 唤醒", **get_watchdog_info()}, ensure_ascii=False)


def _setup_hot_reload():
    """设置 SIGUSR1 信号处理器，收到信号时热重载工具代码。"""
    import os, signal, importlib, sys

    def _reload_handler(signum, frame):
        try:
            mod = sys.modules[__name__]
            importlib.reload(mod)
            for name in ('iterm', 'shared_file', 'interaction', 'prompt_template',
                         'command_card', 'db', 'task', 'approval', 'lock', 'agent_watchdog'):
                if hasattr(mod, name):
                    globals()[name] = getattr(mod, name)
            print(f"[hot-reload] 重载成功 ✓", file=sys.stderr)
        except Exception as e:
            print(f"[hot-reload] 重载失败: {e}", file=sys.stderr)

    signal.signal(signal.SIGUSR1, _reload_handler)
    print(f"[acp-bus] PID={os.getpid()} SIGUSR1 热重载已注册", file=sys.stderr)


def main() -> None:
    import os
    # 确保加载 .env，让 MCP 进程获得 DB 连接串等环境变量
    try:
        from dotenv import load_dotenv
        from pathlib import Path
        env_path = Path(__file__).resolve().parents[1] / ".env"
        load_dotenv(env_path, override=False)
    except ImportError:
        pass

    from agents.base_agent import create_agent_server, run_agent
    from agents.runtime_control import initialize_agent_runtime

    # SIGUSR1 热重载（PID 锁由 acp_bus_run.sh 守护进程管理）
    _setup_hot_reload()

    initialize_agent_runtime("all-agents")

    server = create_agent_server("acp-bus", "多Agent编排 — 系统级工具集 + iTerm I/O")

    server.tool()(iterm)
    server.tool()(shared_file)
    server.tool()(interaction)
    server.tool()(prompt_template)
    server.tool()(command_card)
    server.tool()(db)
    server.tool()(task)
    server.tool()(approval)
    server.tool()(lock)
    server.tool()(agent_watchdog)

    run_agent(server)


if __name__ == "__main__":
    main()
