"""Agent 06 — 翻译"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-06", "翻译 Agent")


@mcp_server.tool()
def translate(text: str, target_lang: str = "en") -> str:
    """翻译文本"""
    return f"[Agent-06] 已将文本翻译为 {target_lang}: '{text[:30]}...'"


@mcp_server.tool()
def detect_language(text: str) -> str:
    """检测语言"""
    return f"[Agent-06] 检测结果: 语言=zh-CN, 置信度=0.98"


if __name__ == "__main__":
    run_agent(mcp_server)
