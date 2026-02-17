"""HTTP fetch plugin tools."""

from __future__ import annotations

from agents.factory import ToolParam, ToolSpec

PLUGIN_NAME = "http_fetch"

PLUGIN_TOOLS = (
    ToolSpec(
        name="http_fetch",
        description="执行受限 HTTP GET 请求（模拟）",
        params=(
            ToolParam("url", str),
            ToolParam("timeout_sec", int, 5),
        ),
        response_builder=lambda values: (
            f"[http_fetch] 已请求 {values['url']} (timeout={values['timeout_sec']}s), status=200"
        ),
    ),
)
