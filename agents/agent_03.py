"""Agent 03 — 数据分析"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-03", "数据分析 Agent")


@mcp_server.tool()
def statistical_analysis(dataset: str) -> str:
    """执行统计分析"""
    return f"[Agent-03] '{dataset}' 统计结果: mean=45.2, std=12.3, median=43.0"


@mcp_server.tool()
def trend_analysis(dataset: str, period: str = "7d") -> str:
    """执行趋势分析"""
    return f"[Agent-03] '{dataset}' {period} 趋势: 上升 3.2%"


if __name__ == "__main__":
    run_agent(mcp_server)
