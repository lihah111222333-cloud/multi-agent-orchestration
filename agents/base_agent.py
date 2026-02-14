"""Agent 基类 — MCP Server 工厂（支持崩溃自动重启）"""

import errno
import logging
import os
import sys
import time
import traceback

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
    stateless_http: bool = True,
) -> FastMCP:
    """创建一个 MCP Agent Server 实例

    Args:
        name: Agent 名称，如 'agent-01'
        description: Agent 描述
        host: HTTP 模式监听地址（仅 streamable-http 传输时有效）
        port: HTTP 模式监听端口（仅 streamable-http 传输时有效）
        stateless_http: 无状态模式，每个请求独立处理（默认 True，避免重启后 session 过期）

    Returns:
        FastMCP 实例，可以在其上注册 tools
    """
    server = FastMCP(name, instructions=description, host=host, port=port, stateless_http=stateless_http)
    return server


def run_agent(server: FastMCP, transport: str = "stdio") -> None:
    """启动 Agent MCP Server，支持崩溃自动重启。

    Args:
        server: FastMCP 实例
        transport: 传输协议 ("stdio" | "streamable-http")

    - 正常退出 / KeyboardInterrupt / SystemExit 直接退出，不重试
    - OSError(EBADF) 等 fd 表损坏直接退出，不重试（重启无法恢复）
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
            # fd 表损坏不可恢复，不浪费重启配额
            if isinstance(exc, OSError) and getattr(exc, "errno", None) == errno.EBADF:
                print(
                    "[acp-bus] fatal: Bad file descriptor – fd table corrupted, not retrying",
                    file=sys.stderr,
                )
                break
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
            # 结构化日志（PostgreSQL 落盘），方便事后排查
            try:
                _crash_logger = logging.getLogger("acp_bus")
                _crash_logger.error(
                    "[acp-bus] crash #%d/%d: %s\n%s",
                    attempt, max_restarts, exc,
                    traceback.format_exc(),
                )
            except Exception:
                pass
            # 写入消息总线异常日志
            try:
                from bus_log import record_bus_exception
                record_bus_exception(
                    category="crash_restart",
                    severity="critical",
                    source="run_agent",
                    message=f"crash #{attempt}/{max_restarts}: {exc}",
                    traceback=traceback.format_exc(),
                    extra={"attempt": attempt, "max_restarts": max_restarts, "delay_sec": delay},
                )
            except Exception:
                pass
            time.sleep(delay)
