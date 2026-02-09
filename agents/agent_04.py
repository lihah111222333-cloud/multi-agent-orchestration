"""Agent 04 — 数据可视化"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-04", "数据可视化 Agent")


@mcp_server.tool()
def create_chart(data: str, chart_type: str = "line") -> str:
    """创建图表"""
    return f"[Agent-04] 已生成 {chart_type} 图表: data={data}"


@mcp_server.tool()
def create_dashboard(title: str) -> str:
    """创建数据看板"""
    return f"[Agent-04] 已创建看板 '{title}' 包含 4 个组件"


if __name__ == "__main__":
    run_agent(mcp_server)
