"""Agent 09 — 系统监控"""

from agents.base_agent import create_agent_server, run_agent

mcp_server = create_agent_server("agent-09", "系统监控 Agent")


@mcp_server.tool()
def check_system_health(service: str) -> str:
    """检查服务健康状态"""
    return f"[Agent-09] {service} 状态: healthy, CPU=23%, MEM=45%"


@mcp_server.tool()
def get_metrics(service: str, period: str = "1h") -> str:
    """获取系统指标"""
    return f"[Agent-09] {service} {period} 指标: QPS=1200, P99=45ms, 错误率=0.01%"


if __name__ == "__main__":
    run_agent(mcp_server)
