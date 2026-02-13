"""Tests for run_agent auto-restart logic in agents/base_agent.py"""

import os
import unittest
from unittest import mock

from agents.base_agent import run_agent, _safe_int_env


class _FakeServer:
    """Minimal stub for FastMCP."""

    def __init__(self, side_effects=None):
        self._side_effects = list(side_effects or [])
        self.call_count = 0

    def run(self, transport="stdio"):
        self.call_count += 1
        if self._side_effects:
            effect = self._side_effects.pop(0)
            if effect is not None:
                raise effect


class SafeIntEnvTests(unittest.TestCase):
    def test_default_value(self):
        self.assertEqual(_safe_int_env("__NONEXISTENT_KEY__", 42), 42)

    def test_valid_env(self):
        with mock.patch.dict(os.environ, {"__TEST_INT__": "7"}):
            self.assertEqual(_safe_int_env("__TEST_INT__", 0), 7)

    def test_invalid_env_returns_default(self):
        with mock.patch.dict(os.environ, {"__TEST_INT__": "abc"}):
            self.assertEqual(_safe_int_env("__TEST_INT__", 5), 5)


class RunAgentRestartTests(unittest.TestCase):
    """Test the auto-restart behaviour of run_agent()."""

    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_normal_exit_no_retry(self, mock_sleep):
        """Normal exit from server.run() should not trigger any restart."""
        server = _FakeServer(side_effects=[None])
        run_agent(server)
        self.assertEqual(server.call_count, 1)
        mock_sleep.assert_not_called()

    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_keyboard_interrupt_no_retry(self, mock_sleep):
        """KeyboardInterrupt should exit immediately, no retry."""
        server = _FakeServer(side_effects=[KeyboardInterrupt()])
        run_agent(server)
        self.assertEqual(server.call_count, 1)
        mock_sleep.assert_not_called()

    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_system_exit_no_retry(self, mock_sleep):
        """SystemExit should exit immediately, no retry."""
        server = _FakeServer(side_effects=[SystemExit(0)])
        run_agent(server)
        self.assertEqual(server.call_count, 1)
        mock_sleep.assert_not_called()

    @mock.patch.dict(os.environ, {"ACP_BUS_MAX_RESTARTS": "3"})
    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_crash_triggers_retry_with_backoff(self, mock_sleep):
        """Exception should trigger retries. After 2 crashes + 1 success = 3 calls."""
        server = _FakeServer(side_effects=[
            RuntimeError("boom"),
            RuntimeError("boom2"),
            None,  # success on 3rd attempt
        ])
        run_agent(server)
        self.assertEqual(server.call_count, 3)
        # backoff delays: 2^1=2, 2^2=4
        self.assertEqual(mock_sleep.call_args_list, [
            mock.call(2),
            mock.call(4),
        ])

    @mock.patch.dict(os.environ, {"ACP_BUS_MAX_RESTARTS": "2"})
    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_exceeds_max_restarts(self, mock_sleep):
        """After exceeding max restarts, run_agent gives up."""
        server = _FakeServer(side_effects=[
            RuntimeError("e1"),
            RuntimeError("e2"),
            RuntimeError("e3"),  # this is attempt 3, but max is 2
        ])
        run_agent(server)
        # attempt 1 crash, attempt 2 crash, attempt 3 > max → give up
        # so server.run is called 3 times total
        self.assertEqual(server.call_count, 3)
        self.assertEqual(mock_sleep.call_count, 2)

    @mock.patch.dict(os.environ, {"ACP_BUS_MAX_RESTARTS": "5"})
    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_backoff_caps_at_60(self, mock_sleep):
        """Backoff should cap at 60 seconds."""
        # Need 7 crashes: 2^1=2, 2^2=4, 2^3=8, 2^4=16, 2^5=32 → all < 60
        # With max_restarts=5, we get 5 crashes then give up
        server = _FakeServer(side_effects=[
            RuntimeError("e") for _ in range(6)
        ])
        run_agent(server)
        delays = [c.args[0] for c in mock_sleep.call_args_list]
        self.assertEqual(delays, [2, 4, 8, 16, 32])
        self.assertTrue(all(d <= 60 for d in delays))

    @mock.patch.dict(os.environ, {"ACP_BUS_MAX_RESTARTS": "5"})
    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_ebadf_exits_immediately_no_retry(self, mock_sleep):
        """OSError(EBADF) should exit immediately without retrying."""
        import errno as _errno
        ebadf = OSError(_errno.EBADF, "Bad file descriptor")
        server = _FakeServer(side_effects=[ebadf])
        run_agent(server)
        self.assertEqual(server.call_count, 1)
        mock_sleep.assert_not_called()

    @mock.patch.dict(os.environ, {"ACP_BUS_MAX_RESTARTS": "5"})
    @mock.patch("agents.base_agent.time.sleep", return_value=None)
    def test_non_ebadf_oserror_still_retries(self, mock_sleep):
        """Other OSErrors (e.g. ECONNRESET) should still trigger retry."""
        import errno as _errno
        err = OSError(_errno.ECONNRESET, "Connection reset")
        server = _FakeServer(side_effects=[err, None])
        run_agent(server)
        self.assertEqual(server.call_count, 2)
        self.assertEqual(mock_sleep.call_count, 1)


if __name__ == "__main__":
    unittest.main()
