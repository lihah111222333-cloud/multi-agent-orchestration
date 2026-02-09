"""Agent 10 — 日志分析"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-10", "日志分析 Agent")


@mcp_server.tool()
def search_logs(query: str, time_range: str = "1h") -> str:
    """搜索日志"""
    return f"[Agent-10] 搜索 '{query}' 在 {time_range} 内: 找到 42 条匹配"


@mcp_server.tool()
def analyze_errors(service: str) -> str:
    """分析错误日志"""
    return f"[Agent-10] {service} 错误分析: TimeoutError 占 60%, ConnectionError 占 25%"


if __name__ == "__main__":
    run_agent(mcp_server)
