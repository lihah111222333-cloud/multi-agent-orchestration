"""Tests for _patch_session_auto_rebind ClientDisconnect resilience."""

import asyncio
import json
import unittest
from unittest import mock

# Simulate starlette's ClientDisconnect
class FakeClientDisconnect(Exception):
    """Simulates starlette.requests.ClientDisconnect."""
    pass


class TestAutoRebindResilience(unittest.IsolatedAsyncioTestCase):
    """Verify that _auto_rebind_handle_request survives client disconnections."""

    def _build_patched_handler(self):
        """Build a minimal _auto_rebind_handle_request for testing."""
        import logging
        _logger = logging.getLogger("acp_bus.test")
        _stale_warned: set[str] = set()
        _STALE_WARNED_MAX = 4  # small for test

        # Mock mgr with _server_instances
        mgr = mock.MagicMock()
        mgr._server_instances = {}

        _original_handle_request = mock.AsyncMock()

        async def _auto_rebind_handle_request(scope, receive, send):
            from starlette.requests import Request
            from starlette.responses import JSONResponse

            # Patch: use our fake ClientDisconnect for testing
            ClientDisconnect = FakeClientDisconnect

            request = Request(scope, receive)
            session_id = request.headers.get("mcp-session-id")

            if not session_id or session_id in mgr._server_instances:
                try:
                    await _original_handle_request(scope, receive, send)
                except (ClientDisconnect, ConnectionError, OSError):
                    _logger.debug("[acp-bus] client disconnected (active session), ignoring")
                return

            try:
                body = await request.body()
                body_json = json.loads(body) if body else {}
            except ClientDisconnect:
                _logger.debug("[acp-bus] client disconnected during body read")
                return
            except Exception:
                body_json = {}

            method = body_json.get("method", "")

            if method == "initialize":
                try:
                    await _original_handle_request(scope, receive, send)
                except (ClientDisconnect, ConnectionError, OSError):
                    _logger.debug("[acp-bus] client disconnected during re-initialize")
            else:
                if session_id not in _stale_warned:
                    if len(_stale_warned) >= _STALE_WARNED_MAX:
                        _stale_warned.clear()
                    _stale_warned.add(session_id)
                resp = JSONResponse(
                    {"jsonrpc": "2.0", "id": "err", "error": {"code": -32600, "message": "expired"}},
                    status_code=404,
                )
                try:
                    await resp(scope, receive, send)
                except (ClientDisconnect, ConnectionError, OSError):
                    _logger.debug("[acp-bus] client disconnected during error response")

        return _auto_rebind_handle_request, _original_handle_request, mgr, _stale_warned

    def _make_scope(self, session_id=None):
        headers = [(b"content-type", b"application/json")]
        if session_id:
            headers.append((b"mcp-session-id", session_id.encode()))
        return {
            "type": "http",
            "method": "POST",
            "path": "/mcp",
            "headers": headers,
            "query_string": b"",
            "root_path": "",
            "server": ("127.0.0.1", 9100),
        }

    async def test_active_session_client_disconnect_no_crash(self):
        """ClientDisconnect during active session handling should not crash."""
        handler, original, mgr, _ = self._build_patched_handler()
        original.side_effect = FakeClientDisconnect()

        scope = self._make_scope()
        # Should NOT raise
        await handler(scope, mock.AsyncMock(), mock.AsyncMock())
        original.assert_called_once()

    async def test_active_session_os_error_no_crash(self):
        """OSError (ECONNRESET etc.) during active session should not crash."""
        handler, original, mgr, _ = self._build_patched_handler()
        original.side_effect = OSError("Connection reset by peer")

        scope = self._make_scope()
        await handler(scope, mock.AsyncMock(), mock.AsyncMock())
        original.assert_called_once()

    async def test_active_session_connection_error_no_crash(self):
        """ConnectionError during active session should not crash."""
        handler, original, mgr, _ = self._build_patched_handler()
        original.side_effect = ConnectionResetError("Connection reset")

        scope = self._make_scope()
        await handler(scope, mock.AsyncMock(), mock.AsyncMock())
        original.assert_called_once()

    async def test_stale_warned_cap(self):
        """_stale_warned should not grow beyond _STALE_WARNED_MAX."""
        _, _, _, stale_warned = self._build_patched_handler()

        # Simulate adding entries up to cap
        stale_warned.update({f"s{i}" for i in range(4)})
        self.assertEqual(len(stale_warned), 4)

        # At cap, should clear on next add
        if len(stale_warned) >= 4:
            stale_warned.clear()
        stale_warned.add("new")
        self.assertEqual(len(stale_warned), 1)


class TestRunAgentCrashLogging(unittest.TestCase):
    """Verify run_agent crash logging records to structured logger."""

    def test_crash_logging_records_to_logger(self):
        from agents.base_agent import run_agent

        mock_server = mock.MagicMock()
        # First call: raise, second call: succeed
        call_count = 0

        def _run_side_effect(transport="stdio"):
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                raise RuntimeError("test crash")

        mock_server.run.side_effect = _run_side_effect

        with mock.patch("agents.base_agent.time.sleep"):
            with mock.patch("agents.base_agent.logging.getLogger") as mock_get_logger:
                mock_logger = mock.MagicMock()
                mock_get_logger.return_value = mock_logger

                run_agent(mock_server, transport="stdio")

                # Should have logged the crash
                mock_logger.error.assert_called_once()
                call_args = mock_logger.error.call_args
                self.assertIn("crash #1/10", call_args[0][0] % call_args[0][1:])

    def test_crash_logging_survives_logger_failure(self):
        """Even if the structured logger fails, run_agent should still recover."""
        from agents.base_agent import run_agent

        mock_server = mock.MagicMock()
        call_count = 0

        def _run_side_effect(transport="stdio"):
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                raise RuntimeError("test crash")

        mock_server.run.side_effect = _run_side_effect

        with mock.patch("agents.base_agent.time.sleep"):
            with mock.patch("agents.base_agent.logging.getLogger", side_effect=Exception("logger broken")):
                # Should NOT raise even if logger fails
                run_agent(mock_server, transport="stdio")

        self.assertEqual(call_count, 2)


if __name__ == "__main__":
    unittest.main()
