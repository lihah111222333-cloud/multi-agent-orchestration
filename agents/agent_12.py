"""Agent 12 — 告警管理"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-12", "告警管理 Agent")


@mcp_server.tool()
def create_alert(name: str, condition: str) -> str:
    """创建告警规则"""
    return f"[Agent-12] 已创建告警 '{name}': 条件={condition}"


@mcp_server.tool()
def list_active_alerts() -> str:
    """列出活跃告警"""
    return "[Agent-12] 活跃告警: [WARNING] disk usage > 80%, [INFO] memory spike"


@mcp_server.tool()
def acknowledge_alert(alert_id: str) -> str:
    """确认告警"""
    return f"[Agent-12] 已确认告警 {alert_id}"


if __name__ == "__main__":
    run_agent(mcp_server)
