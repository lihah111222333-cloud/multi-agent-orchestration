"""Agent 02 — 数据清洗"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-02", "数据清洗 Agent")


@mcp_server.tool()
def clean_data(dataset: str) -> str:
    """清洗原始数据，去除异常值"""
    return f"[Agent-02] 已清洗数据集 '{dataset}': 去除 12 条异常记录"


@mcp_server.tool()
def normalize_data(dataset: str, method: str = "z-score") -> str:
    """归一化数据"""
    return f"[Agent-02] 已对 '{dataset}' 执行 {method} 归一化"


if __name__ == "__main__":
    run_agent(mcp_server)
