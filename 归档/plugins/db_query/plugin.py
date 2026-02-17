"""DB query plugin tools."""

from __future__ import annotations

from agents.factory import ToolParam, ToolSpec

PLUGIN_NAME = "db_query"

PLUGIN_TOOLS = (
    ToolSpec(
        name="db_query",
        description="执行受限只读 SQL 查询（模拟）",
        params=(
            ToolParam("sql", str),
            ToolParam("limit", int, 200),
        ),
        response_builder=lambda values: (
            f"[db_query] 已执行只读查询: sql={values['sql']} | limit={values['limit']}"
        ),
    ),
)
