"""Agent 07 — 代码生成"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-07", "代码生成 Agent")


@mcp_server.tool()
def generate_code(description: str, language: str = "python") -> str:
    """根据描述生成代码"""
    return f"[Agent-07] 已生成 {language} 代码: # {description}"


@mcp_server.tool()
def review_code(code: str) -> str:
    """代码审查"""
    return f"[Agent-07] 代码审查完成: 发现 0 个严重问题, 2 个建议优化"


if __name__ == "__main__":
    run_agent(mcp_server)
