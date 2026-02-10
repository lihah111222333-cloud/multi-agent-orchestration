"""Agent 基类 — MCP Server 工厂"""

from mcp.server.fastmcp import FastMCP


def create_agent_server(name: str, description: str = "") -> FastMCP:
    """创建一个 MCP Agent Server 实例

    Args:
        name: Agent 名称，如 'agent-01'
        description: Agent 描述

    Returns:
        FastMCP 实例，可以在其上注册 tools
    """
    server = FastMCP(name, instructions=description)
    return server


def run_agent(server: FastMCP) -> None:
    """启动 Agent MCP Server (stdio 传输)"""
    server.run(transport="stdio")
