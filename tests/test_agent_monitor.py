import unittest
from unittest.mock import Mock

from agent_monitor import classify_status, patrol_agents_once


class AgentMonitorTests(unittest.TestCase):
    def test_has_no_session_returns_unknown(self):
        status = classify_status(["Traceback: boom"], has_session=False, stagnant_sec=999)

        self.assertEqual(status, "unknown")

    def test_error_keyword_returns_error(self):
        status = classify_status(["worker failed with Exception: bad input"])

        self.assertEqual(status, "error")

    def test_disconnected_keyword_returns_disconnected(self):
        status = classify_status(["dial tcp: connection refused"])

        self.assertEqual(status, "disconnected")

    def test_empty_output_returns_idle(self):
        status = classify_status([])

        self.assertEqual(status, "idle")

    def test_prompt_only_returns_idle(self):
        status = classify_status(["$", "   ", ">>>"])

        self.assertEqual(status, "idle")

    def test_stagnant_non_idle_returns_stuck(self):
        status = classify_status(["processing tasks..."], stagnant_sec=60)

        self.assertEqual(status, "stuck")

    def test_stagnant_idle_stays_idle(self):
        status = classify_status(["#"], stagnant_sec=3600)

        self.assertEqual(status, "idle")

    def test_otherwise_returns_running(self):
        status = classify_status(["heartbeat ok", "processed 1 item"], stagnant_sec=12)

        self.assertEqual(status, "running")

    def test_patrol_success_produces_status_summary(self):
        list_sessions = Mock(
            return_value={
                "ok": True,
                "sessions": [
                    {
                        "agent_id": "agent_01",
                        "agent_name": "Agent 01",
                        "session_id": "s1",
                    },
                    {
                        "agent_id": "agent_02",
                        "agent_name": "Agent 02",
                        "session_id": "s2",
                    },
                ],
            }
        )
        read_output = Mock(
            return_value={
                "ok": True,
                "results": [
                    {
                        "agent_id": "agent_01",
                        "output": ["heartbeat ok", "processed 1 item"],
                        "error": "",
                    },
                    {
                        "agent_id": "agent_02",
                        "output": ["Traceback: boom"],
                        "error": "",
                    },
                ],
            }
        )

        snapshot = patrol_agents_once(
            list_sessions_func=list_sessions,
            read_output_func=read_output,
            read_lines=20,
            now_ts=100,
            status_memory={},
        )

        self.assertTrue(snapshot["ok"])
        self.assertEqual(snapshot["summary"]["total"], 2)
        self.assertEqual(snapshot["summary"]["running"], 1)
        self.assertEqual(snapshot["summary"]["error"], 1)
        by_id = {row["agent_id"]: row for row in snapshot["agents"]}
        self.assertEqual(by_id["agent_01"]["status"], "running")
        self.assertEqual(by_id["agent_02"]["status"], "error")
        list_sessions.assert_called_once_with()
        read_output.assert_called_once_with(all_agents=True, read_lines=20)

    def test_patrol_read_output_failed_returns_unknown_agents(self):
        list_sessions = Mock(
            return_value={
                "ok": True,
                "sessions": [
                    {
                        "agent_id": "agent_01",
                        "agent_name": "Agent 01",
                        "session_id": "s1",
                    }
                ],
            }
        )
        read_output = Mock(return_value={"ok": False, "error": "iTerm unavailable"})

        snapshot = patrol_agents_once(
            list_sessions_func=list_sessions,
            read_output_func=read_output,
            status_memory={},
        )

        self.assertFalse(snapshot["ok"])
        self.assertEqual(snapshot["source"], {"sessions_ok": True, "output_ok": False})
        self.assertEqual(snapshot["summary"]["unknown"], 1)
        self.assertEqual(snapshot["agents"][0]["status"], "unknown")
        self.assertIn("iTerm unavailable", snapshot["error"])

    def test_patrol_repeated_same_output_becomes_stuck(self):
        list_sessions = Mock(
            return_value={
                "ok": True,
                "sessions": [
                    {
                        "agent_id": "agent_01",
                        "agent_name": "Agent 01",
                        "session_id": "s1",
                    }
                ],
            }
        )
        read_output = Mock(
            return_value={
                "ok": True,
                "results": [
                    {
                        "agent_id": "agent_01",
                        "output": ["processing..."],
                        "error": "",
                    }
                ],
            }
        )
        memory: dict[str, dict[str, float | str]] = {}

        first = patrol_agents_once(
            list_sessions_func=list_sessions,
            read_output_func=read_output,
            now_ts=100,
            status_memory=memory,
        )
        second = patrol_agents_once(
            list_sessions_func=list_sessions,
            read_output_func=read_output,
            now_ts=170,
            status_memory=memory,
        )

        self.assertEqual(first["agents"][0]["status"], "running")
        self.assertEqual(first["agents"][0]["stagnant_sec"], 0)
        self.assertEqual(second["agents"][0]["status"], "stuck")
        self.assertEqual(second["agents"][0]["stagnant_sec"], 70)

    def test_patrol_row_error_forces_disconnected(self):
        list_sessions = Mock(
            return_value={
                "ok": True,
                "sessions": [
                    {
                        "agent_id": "agent_01",
                        "agent_name": "Agent 01",
                        "session_id": "s1",
                    }
                ],
            }
        )
        read_output = Mock(
            return_value={
                "ok": True,
                "results": [
                    {
                        "agent_id": "agent_01",
                        "output": ["still alive"],
                        "error": "session not found in iTerm",
                    }
                ],
            }
        )

        snapshot = patrol_agents_once(
            list_sessions_func=list_sessions,
            read_output_func=read_output,
            status_memory={},
        )

        self.assertEqual(snapshot["agents"][0]["status"], "disconnected")


if __name__ == "__main__":
    unittest.main()
