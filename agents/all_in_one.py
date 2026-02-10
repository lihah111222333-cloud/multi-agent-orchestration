"""All-in-One Agent — 将所有 Agent 的工具注册到单个 MCP Server"""

from __future__ import annotations

import json
from typing import Any

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
from agents.iterm_bridge import list_iterm_agent_sessions, read_iterm_output, send_iterm_input
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


def iterm_list_sessions(state_file: str = "") -> str:
    """列出 iTerm 已启动 agent 会话（来自 launch state + session_id）"""
    return json.dumps(list_iterm_agent_sessions(state_file=state_file), ensure_ascii=False)


def iterm_send_input(
    text: str,
    agent_id: str = "",
    all_agents: bool = False,
    wait_sec: float = 0.4,
    read_lines: int = 20,
    state_file: str = "",
) -> str:
    """向一个/多个 agent 会话发送输入，可选回读最近输出"""
    return json.dumps(
        send_iterm_input(
            text=text,
            agent_id=agent_id,
            all_agents=all_agents,
            wait_sec=wait_sec,
            read_lines=read_lines,
            state_file=state_file,
            append_enter=True,
        ),
        ensure_ascii=False,
    )


def iterm_read_output(
    agent_id: str = "",
    all_agents: bool = False,
    read_lines: int = 20,
    state_file: str = "",
) -> str:
    """读取一个/多个 agent 会话最近输出"""
    return json.dumps(
        read_iterm_output(
            agent_id=agent_id,
            all_agents=all_agents,
            read_lines=read_lines,
            state_file=state_file,
        ),
        ensure_ascii=False,
    )


def write_file(path: str, content: str) -> str:
    """写入共享文件（PostgreSQL）"""
    return json.dumps(write_shared_file(path=path, content=content, actor="mcp"), ensure_ascii=False)


def read_file(path: str) -> str:
    """读取共享文件（PostgreSQL）"""
    result = read_shared_file(path=path)
    return json.dumps(result or {"ok": False, "message": "not_found", "path": path}, ensure_ascii=False)


def list_files(path: str = "", limit: int = 200) -> str:
    """列出共享文件（PostgreSQL）"""
    rows = list_shared_files(prefix=path, limit=limit)
    return json.dumps({"ok": True, "count": len(rows), "files": rows}, ensure_ascii=False)


def delete_file(path: str) -> str:
    """删除共享文件（PostgreSQL）"""
    return json.dumps(delete_shared_file(path=path, actor="mcp"), ensure_ascii=False)


# ---- Agent 交互表 tools ----
def create_interaction(
    sender: str,
    receiver: str,
    msg_type: str,
    content: str,
    thread_id: str = "",
    parent_id: int | None = None,
    requires_review: bool = False,
    metadata_json: str = "",
    status: str = "pending",
) -> str:
    """创建 Agent 交互记录"""
    row = create_interaction_row(
        sender=sender,
        receiver=receiver,
        msg_type=msg_type,
        content=content,
        thread_id=thread_id,
        parent_id=parent_id,
        requires_review=requires_review,
        metadata=_parse_json(metadata_json, {}),
        status=status,
    )
    return json.dumps({"ok": True, "interaction": row}, ensure_ascii=False)


def list_interactions(
    thread_id: str = "",
    sender: str = "",
    receiver: str = "",
    msg_type: str = "",
    status: str = "",
    requires_review: bool | None = None,
    limit: int = 100,
) -> str:
    """查询 Agent 交互记录"""
    rows = list_interaction_rows(
        thread_id=thread_id,
        sender=sender,
        receiver=receiver,
        msg_type=msg_type,
        status=status,
        requires_review=requires_review,
        limit=limit,
    )
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


def review_interaction(interaction_id: int, status: str, reviewer: str = "", note: str = "") -> str:
    """审核 Agent 交互记录状态"""
    result = review_interaction_row(interaction_id=interaction_id, status=status, reviewer=reviewer, note=note)
    return json.dumps(result, ensure_ascii=False)


# ---- 提示词模板表 tools ----
def save_prompt_template(
    prompt_key: str,
    title: str,
    prompt_text: str,
    agent_key: str = "",
    tool_name: str = "",
    variables_json: str = "",
    tags_json: str = "",
    enabled: bool = True,
    updated_by: str = "mcp",
) -> str:
    """保存/更新提示词模板"""
    row = save_prompt_template_row(
        prompt_key=prompt_key,
        title=title,
        prompt_text=prompt_text,
        agent_key=agent_key,
        tool_name=tool_name,
        variables=_parse_json(variables_json, {}),
        tags=_parse_json(tags_json, []),
        enabled=enabled,
        updated_by=updated_by,
    )
    return json.dumps({"ok": True, "prompt": row}, ensure_ascii=False)


def get_prompt_template(prompt_key: str) -> str:
    """读取提示词模板"""
    row = get_prompt_template_row(prompt_key=prompt_key)
    return json.dumps({"ok": bool(row), "prompt": row}, ensure_ascii=False)


def list_prompt_templates(
    agent_key: str = "",
    tool_name: str = "",
    keyword: str = "",
    enabled_only: bool = False,
    limit: int = 100,
) -> str:
    """查询提示词模板"""
    rows = list_prompt_template_rows(
        agent_key=agent_key,
        tool_name=tool_name,
        keyword=keyword,
        enabled_only=enabled_only,
        limit=limit,
    )
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


def set_prompt_template_enabled(prompt_key: str, enabled: bool, updated_by: str = "mcp") -> str:
    """启用/停用提示词模板"""
    result = set_prompt_template_enabled_row(prompt_key=prompt_key, enabled=enabled, updated_by=updated_by)
    return json.dumps(result, ensure_ascii=False)


# ---- 命令卡表 tools ----
def save_command_card(
    card_key: str,
    title: str,
    command_template: str,
    description: str = "",
    args_schema_json: str = "",
    risk_level: str = "normal",
    enabled: bool = True,
    updated_by: str = "mcp",
) -> str:
    """保存/更新命令卡"""
    row = save_command_card_row(
        card_key=card_key,
        title=title,
        command_template=command_template,
        description=description,
        args_schema=_parse_json(args_schema_json, {}),
        risk_level=risk_level,
        enabled=enabled,
        updated_by=updated_by,
    )
    return json.dumps({"ok": True, "command_card": row}, ensure_ascii=False)


def get_command_card(card_key: str) -> str:
    """读取命令卡"""
    row = get_command_card_row(card_key=card_key)
    return json.dumps({"ok": bool(row), "command_card": row}, ensure_ascii=False)


def list_command_cards(
    keyword: str = "",
    risk_level: str = "",
    enabled_only: bool = False,
    limit: int = 100,
) -> str:
    """查询命令卡"""
    rows = list_command_card_rows(keyword=keyword, risk_level=risk_level, enabled_only=enabled_only, limit=limit)
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


def set_command_card_enabled(card_key: str, enabled: bool, updated_by: str = "mcp") -> str:
    """启用/停用命令卡"""
    result = set_command_card_enabled_row(card_key=card_key, enabled=enabled, updated_by=updated_by)
    return json.dumps(result, ensure_ascii=False)


# ---- 命令卡执行器 tools ----
def prepare_command_card_run(
    card_key: str,
    params_json: str = "",
    requested_by: str = "agent",
    require_review: bool | None = None,
) -> str:
    """准备命令卡执行（渲染命令，可触发待审批）"""
    result = prepare_command_card_run_flow(
        card_key=card_key,
        params=_parse_json(params_json, {}),
        requested_by=requested_by,
        require_review=require_review,
    )
    return json.dumps(result, ensure_ascii=False)


def review_command_card_run(run_id: int, decision: str, reviewer: str = "", note: str = "") -> str:
    """审核命令卡执行（approved/rejected）"""
    result = review_command_card_run_flow(run_id=run_id, decision=decision, reviewer=reviewer, note=note)
    return json.dumps(result, ensure_ascii=False)


def execute_command_card_run(run_id: int, actor: str = "agent", timeout_sec: int | None = None) -> str:
    """执行指定 run_id 的命令卡"""
    result = execute_command_card_run_flow(run_id=run_id, actor=actor, timeout_sec=timeout_sec)
    return json.dumps(result, ensure_ascii=False)


def execute_command_card(
    card_key: str,
    params_json: str = "",
    requested_by: str = "agent",
    auto_approve: bool = False,
    reviewer: str = "",
    review_note: str = "",
    timeout_sec: int | None = None,
) -> str:
    """一键执行命令卡：准备->（可选自动审批）->执行"""
    result = execute_command_card_flow(
        card_key=card_key,
        params=_parse_json(params_json, {}),
        requested_by=requested_by,
        auto_approve=auto_approve,
        reviewer=reviewer,
        review_note=review_note,
        timeout_sec=timeout_sec,
    )
    return json.dumps(result, ensure_ascii=False)


def get_command_card_run(run_id: int) -> str:
    """读取命令卡执行流水详情"""
    run = get_command_card_run_row(run_id=run_id)
    return json.dumps({"ok": bool(run), "run": run}, ensure_ascii=False)


def list_command_card_runs(
    card_key: str = "",
    status: str = "",
    requested_by: str = "",
    limit: int = 100,
) -> str:
    """查询命令卡执行流水"""
    rows = list_command_card_run_rows(card_key=card_key, status=status, requested_by=requested_by, limit=limit)
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


# ---- 通用 SQL tools（测试场景） ----
def db_query(sql: str, limit: int = 200) -> str:
    """执行只读 SQL（仅 SELECT）"""
    rows = db_query_sql(sql_text=sql, limit=limit)
    return json.dumps({"ok": True, "count": len(rows), "rows": rows}, ensure_ascii=False)


def db_execute(sql: str) -> str:
    """执行变更 SQL（测试场景）"""
    result = db_execute_sql(sql_text=sql)
    return json.dumps(result, ensure_ascii=False)


def main() -> None:
    from agents.base_agent import create_agent_server, run_agent
    from agents.factory import build_tool_callable
    from agents.runtime_control import initialize_agent_runtime
    from agents.specs import AGENT_SPECS

    initialize_agent_runtime("all-agents")

    server = create_agent_server("acp-bus", "多Agent编排 — 全量工具集 + iTerm I/O")

    for spec in AGENT_SPECS.values():
        for tool in spec.tools:
            callable_fn = build_tool_callable(tool)
            prefixed_name = f"{spec.key}__{tool.name}"
            callable_fn.__name__ = prefixed_name
            callable_fn.__doc__ = f"[{spec.description}] {tool.description}"
            server.tool()(callable_fn)

    server.tool()(iterm_list_sessions)
    server.tool()(iterm_send_input)
    server.tool()(iterm_read_output)

    server.tool()(write_file)
    server.tool()(read_file)
    server.tool()(list_files)
    server.tool()(delete_file)

    server.tool()(create_interaction)
    server.tool()(list_interactions)
    server.tool()(review_interaction)

    server.tool()(save_prompt_template)
    server.tool()(get_prompt_template)
    server.tool()(list_prompt_templates)
    server.tool()(set_prompt_template_enabled)

    server.tool()(save_command_card)
    server.tool()(get_command_card)
    server.tool()(list_command_cards)
    server.tool()(set_command_card_enabled)

    server.tool()(prepare_command_card_run)
    server.tool()(review_command_card_run)
    server.tool()(execute_command_card_run)
    server.tool()(execute_command_card)
    server.tool()(get_command_card_run)
    server.tool()(list_command_card_runs)

    server.tool()(db_query)
    server.tool()(db_execute)

    run_agent(server)


if __name__ == "__main__":
    main()
