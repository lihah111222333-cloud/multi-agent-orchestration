"""Agent 05 — 文本生成"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-05", "文本生成 Agent")


@mcp_server.tool()
def write_article(topic: str, word_count: int = 500) -> str:
    """撰写文章"""
    return f"[Agent-05] 已撰写关于 '{topic}' 的文章, 约 {word_count} 字"


@mcp_server.tool()
def summarize_text(text: str) -> str:
    """摘要生成"""
    return f"[Agent-05] 已生成摘要: {text[:50]}..."


if __name__ == "__main__":
    run_agent(mcp_server)
