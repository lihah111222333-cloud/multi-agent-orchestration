"""Agent 基类 — MCP Server 工厂（支持崩溃自动重启）"""

import os
import sys
import time

from mcp.server.fastmcp import FastMCP


def _safe_int_env(key: str, default: int) -> int:
    """从环境变量读取整数，异常时返回默认值。"""
    try:
        return int(os.getenv(key, str(default)))
    except (TypeError, ValueError):
        return default


def create_agent_server(
    name: str,
    description: str = "",
    host: str = "127.0.0.1",
    port: int = 8000,
) -> FastMCP:
    """创建一个 MCP Agent Server 实例

    Args:
        name: Agent 名称，如 'agent-01'
        description: Agent 描述
        host: HTTP 模式监听地址（仅 streamable-http 传输时有效）
        port: HTTP 模式监听端口（仅 streamable-http 传输时有效）

    Returns:
        FastMCP 实例，可以在其上注册 tools
    """
    server = FastMCP(name, instructions=description, host=host, port=port)
    return server


def run_agent(server: FastMCP, transport: str = "stdio") -> None:
    """启动 Agent MCP Server，支持崩溃自动重启。

    Args:
        server: FastMCP 实例
        transport: 传输协议 ("stdio" | "streamable-http")

    - 正常退出 / KeyboardInterrupt / SystemExit 直接退出，不重试
    - 其它异常触发重试，指数退避 (2s → 最大 60s)
    - 最大重试次数由 ACP_BUS_MAX_RESTARTS 环境变量控制（默认 10）
    """
    max_restarts = _safe_int_env("ACP_BUS_MAX_RESTARTS", 10)
    attempt = 0

    while True:
        try:
            server.run(transport=transport)
            break  # 正常退出
        except (KeyboardInterrupt, SystemExit):
            break  # 主动退出，不重试
        except Exception as exc:
            attempt += 1
            if attempt > max_restarts:
                print(
                    f"[acp-bus] exceeded max restarts ({max_restarts}), giving up",
                    file=sys.stderr,
                )
                break
            delay = min(2 ** attempt, 60)
            print(
                f"[acp-bus] crash #{attempt}/{max_restarts}: {exc}, "
                f"restarting in {delay}s …",
                file=sys.stderr,
            )
            time.sleep(delay)
