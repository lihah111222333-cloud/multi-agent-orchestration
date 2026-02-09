"""Agent 01 — 数据采集"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-01", "数据采集 Agent")


@mcp_server.tool()
def fetch_market_data(symbol: str) -> str:
    """获取市场数据"""
    return f"[Agent-01] 已采集 {symbol} 的市场数据: price=100.5, volume=50000"


@mcp_server.tool()
def fetch_news(topic: str) -> str:
    """获取新闻数据"""
    return f"[Agent-01] 已采集关于 '{topic}' 的最新新闻 3 条"


if __name__ == "__main__":
    run_agent(mcp_server)
