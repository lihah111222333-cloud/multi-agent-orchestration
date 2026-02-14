"""MCP 库运行时补丁 — 修复无状态模式下客户端断连导致的崩溃。

问题描述:
  MCP `_handle_message` 在收到 stream Exception 后调用 `send_log_message`，
  但此时 `_write_stream` 可能已关闭，触发 `ClosedResourceError` →
  `ExceptionGroup` → "Stateless session crashed"。

修复内容:
  1. 包裹 `_handle_message` 中的 `send_log_message` 调用
  2. 让 `run_stateless_server` 能捕获 `BaseExceptionGroup`

用法:
  在 ACP Bus 启动前调用 `apply_mcp_patches()`。
"""

from __future__ import annotations

import logging

_logger = logging.getLogger("mcp_patches")
_patched = False


def apply_mcp_patches() -> None:
    """Apply runtime patches to MCP library. Safe to call multiple times."""
    global _patched
    if _patched:
        return
    _patched = True

    _patch_handle_message()
    _patch_stateless_server()
    _logger.info("[mcp-patches] MCP ClientDisconnect patches applied")


def _patch_handle_message() -> None:
    """Wrap send_log_message in _handle_message to tolerate closed streams."""
    try:
        import anyio
        from mcp.server.lowlevel import server as _server_mod

        _original = _server_mod.Server._handle_message

        async def _safe_handle_message(self, message, session, lifespan_context, raise_exceptions=False):
            import warnings
            with warnings.catch_warnings(record=True) as w:
                match message:
                    case _server_mod.RequestResponder(request=_server_mod.types.ClientRequest(root=req)) as responder:
                        with responder:
                            await self._handle_request(message, req, session, lifespan_context, raise_exceptions)
                    case _server_mod.types.ClientNotification(root=notify):
                        await self._handle_notification(notify)
                    case Exception():
                        _server_mod.logger.error(f"Received exception from stream: {message}")
                        try:
                            await session.send_log_message(
                                level="error",
                                data="Internal Server Error",
                                logger="mcp.server.exception_handler",
                            )
                        except (anyio.ClosedResourceError, anyio.BrokenResourceError):
                            _server_mod.logger.debug(
                                "Could not send error log: write stream closed"
                            )
                        if raise_exceptions:
                            raise message

                for warning_item in w:
                    _server_mod.logger.info(
                        "Warning: %s: %s",
                        warning_item.category.__name__,
                        warning_item.message,
                    )

        _server_mod.Server._handle_message = _safe_handle_message
        _logger.debug("[mcp-patches] _handle_message patched")
    except Exception:
        _logger.warning("[mcp-patches] failed to patch _handle_message", exc_info=True)


def _patch_stateless_server() -> None:
    """Make run_stateless_server catch BaseExceptionGroup."""
    try:
        from mcp.server import streamable_http_manager as _mgr_mod

        _original_handle = _mgr_mod.StreamableHTTPSessionManager._handle_stateless_request

        async def _safe_handle_stateless(self, scope, receive, send):
            import anyio
            from anyio.abc import TaskStatus

            _mgr_mod.logger.debug("Stateless mode: Creating new transport for this request")
            from mcp.server.streamable_http import StreamableHTTPServerTransport
            http_transport = StreamableHTTPServerTransport(
                mcp_session_id=None,
                is_json_response_enabled=self.json_response,
                event_store=None,
                security_settings=self.security_settings,
            )

            async def run_stateless_server(*, task_status: TaskStatus[None] = anyio.TASK_STATUS_IGNORED):
                async with http_transport.connect() as streams:
                    read_stream, write_stream = streams
                    task_status.started()
                    try:
                        await self.app.run(
                            read_stream,
                            write_stream,
                            self.app.create_initialization_options(),
                            stateless=True,
                        )
                    except (Exception, BaseExceptionGroup):
                        _mgr_mod.logger.exception("Stateless session crashed")

            assert self._task_group is not None
            await self._task_group.start(run_stateless_server)
            await http_transport.handle_request(scope, receive, send)
            await http_transport.terminate()

        _mgr_mod.StreamableHTTPSessionManager._handle_stateless_request = _safe_handle_stateless
        _logger.debug("[mcp-patches] _handle_stateless_request patched")
    except Exception:
        _logger.warning("[mcp-patches] failed to patch stateless handler", exc_info=True)
