"""Agent 08 — 报告生成"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-08", "报告生成 Agent")


@mcp_server.tool()
def generate_report(title: str, sections: str = "summary,details,conclusion") -> str:
    """生成结构化报告"""
    return f"[Agent-08] 已生成报告 '{title}', 包含章节: {sections}"


@mcp_server.tool()
def export_pdf(report_id: str) -> str:
    """导出 PDF"""
    return f"[Agent-08] 报告 {report_id} 已导出为 PDF"


if __name__ == "__main__":
    run_agent(mcp_server)
