import os
import unittest
from unittest.mock import patch

import tg_bridge


class TgBridgeWatchdogTests(unittest.TestCase):
    def setUp(self) -> None:
        self._orig_thread = tg_bridge._watchdog_thread
        self._orig_stop_set = tg_bridge._watchdog_stop.is_set()
        with tg_bridge._watchdog_lock:
            self._orig_info = dict(tg_bridge._watchdog_info)

        tg_bridge._watchdog_thread = None
        tg_bridge._watchdog_stop.clear()
        with tg_bridge._watchdog_lock:
            tg_bridge._watchdog_info.clear()
            tg_bridge._watchdog_info.update(
                running=False,
                interval=tg_bridge._get_watchdog_interval(),
                last_nudge="",
                nudge_count=0,
                last_nudge_stats={},
            )

    def tearDown(self) -> None:
        tg_bridge._watchdog_thread = self._orig_thread
        if self._orig_stop_set:
            tg_bridge._watchdog_stop.set()
        else:
            tg_bridge._watchdog_stop.clear()
        with tg_bridge._watchdog_lock:
            tg_bridge._watchdog_info.clear()
            tg_bridge._watchdog_info.update(self._orig_info)

    def test_start_watchdog_sets_running_immediately(self) -> None:
        class DummyThread:
            def __init__(self, target=None, name=None, daemon=None):
                self._alive = False

            def start(self):
                self._alive = True

            def is_alive(self):
                return self._alive

            def join(self, timeout=None):
                self._alive = False

        with patch.object(tg_bridge.threading, "Thread", DummyThread):
            with patch.dict(os.environ, {"TG_WATCHDOG_INTERVAL": "45"}, clear=False):
                ok = tg_bridge.start_watchdog()
                info = tg_bridge.get_watchdog_info()

        self.assertTrue(ok)
        self.assertTrue(info["running"])
        self.assertEqual(info["interval"], 45)
        self.assertTrue(info["include_master"])
        self.assertTrue(tg_bridge.is_watchdog_running())

    def test_stop_watchdog_clears_running_flag(self) -> None:
        class DummyThread:
            def __init__(self, target=None, name=None, daemon=None):
                self._alive = False
                self.join_called = False

            def start(self):
                self._alive = True

            def is_alive(self):
                return self._alive

            def join(self, timeout=None):
                self.join_called = True
                self._alive = False

        with patch.object(tg_bridge.threading, "Thread", DummyThread):
            tg_bridge.start_watchdog()
            thread = tg_bridge._watchdog_thread
            tg_bridge.stop_watchdog(timeout=0.1)

        self.assertIsNotNone(thread)
        self.assertTrue(getattr(thread, "join_called", False))
        self.assertFalse(tg_bridge.get_watchdog_info()["running"])
        self.assertFalse(tg_bridge.is_watchdog_running())

    def test_do_nudge_includes_master_by_default(self) -> None:
        with patch.dict(os.environ, {}, clear=False):
            with patch.object(
                tg_bridge,
                "_find_master_session",
                return_value={"session_id": "SID-ACTIVE", "session_name": "主agent"},
            ):
                with patch(
                    "agents.iterm_bridge.list_iterm_agent_sessions",
                    return_value={"ok": True, "sessions": []},
                ):
                    with patch.object(tg_bridge, "_send_to_iterm_session") as send_mock:
                        with patch.object(tg_bridge, "send_message_to_tg") as tg_send_mock:
                            tg_bridge._do_nudge("hello")

        send_mock.assert_called_once_with("SID-ACTIVE", "hello")
        tg_send_mock.assert_called_once()

    def test_do_nudge_can_skip_master_when_disabled(self) -> None:
        with patch.dict(os.environ, {"TG_WATCHDOG_INCLUDE_MASTER": "0"}, clear=False):
            with patch.object(
                tg_bridge,
                "_find_master_session",
                return_value={"session_id": "SID-ACTIVE", "session_name": "主agent"},
            ):
                with patch(
                    "agents.iterm_bridge.list_iterm_agent_sessions",
                    return_value={"ok": True, "sessions": []},
                ):
                    with patch.object(tg_bridge, "_send_to_iterm_session") as send_mock:
                        tg_bridge._do_nudge("hello")

        send_mock.assert_not_called()

    def test_include_master_treats_empty_env_as_enabled(self) -> None:
        with patch.dict(os.environ, {"TG_WATCHDOG_INCLUDE_MASTER": ""}, clear=False):
            self.assertTrue(tg_bridge._should_include_master_watchdog_target())

    def test_find_master_session_matches_agent_typo_variants(self) -> None:
        live_sessions = [
            {"session_id": "SID-MASTER", "session_name": "主agnet", "name": "主agnet"},
        ]
        with patch.dict(os.environ, {"TG_MASTER_TAB_NAME": "主agent"}, clear=False):
            with patch("agents.iterm_bridge._list_live_sessions", return_value=("WIN-1", live_sessions)):
                found = tg_bridge._find_master_session()

        self.assertIsNotNone(found)
        self.assertEqual(found["session_id"], "SID-MASTER")

    def test_do_nudge_skips_worker_row_with_master_session_id(self) -> None:
        captured_targets: list[str] = []

        def _fake_run_iterm_io(*, targets, text, append_enter, wait_sec, read_lines):
            if targets:
                captured_targets.append(str(targets[0].session_id))
            return [{"error": ""}]

        sessions_payload = {
            "ok": True,
            "sessions": [
                {"agent_id": "agent_01", "agent_name": "Runtime Agent 01", "session_id": "SID-MASTER"},
                {"agent_id": "agent_02", "agent_name": "Runtime Agent 02", "session_id": "SID-WORKER"},
            ],
        }

        with patch.dict(os.environ, {"TG_WATCHDOG_INCLUDE_MASTER": "0"}, clear=False):
            with patch.object(
                tg_bridge,
                "_find_master_session",
                return_value={"session_id": "SID-MASTER", "session_name": "主agent"},
            ):
                with patch(
                    "agents.iterm_bridge.list_iterm_agent_sessions",
                    return_value=sessions_payload,
                ):
                    with patch("agents.iterm_bridge._run_iterm_io", side_effect=_fake_run_iterm_io):
                        with patch.object(tg_bridge, "_send_to_iterm_session") as send_mock:
                            with patch.object(tg_bridge, "send_message_to_tg"):
                                tg_bridge._do_nudge("hello")

        send_mock.assert_not_called()
        self.assertEqual(captured_targets, ["SID-WORKER"])
        info = tg_bridge.get_watchdog_info()
        stats = info.get("last_nudge_stats", {})
        self.assertEqual(stats.get("skipped_master_sid"), 1)
        self.assertEqual(stats.get("success"), 1)


if __name__ == "__main__":
    unittest.main()
