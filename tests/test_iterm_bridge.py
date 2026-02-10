import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import agents.iterm_bridge as bridge


class ItermBridgeTests(unittest.TestCase):
    def test_send_prefers_subprocess_by_default(self):
        with mock.patch.dict(os.environ, {"ITERM_IO_BRIDGE_DIRECT": "0"}, clear=False):
            with mock.patch(
                "agents.iterm_bridge._run_io_via_subprocess",
                return_value={"ok": True, "action": "send"},
            ) as run_sub:
                result = bridge.send_iterm_input(text="hello", all_agents=True, read_lines=5)

        self.assertTrue(result["ok"])
        run_sub.assert_called_once()
        self.assertEqual(run_sub.call_args.kwargs["action"], "send")

    def test_read_prefers_subprocess_by_default(self):
        with mock.patch.dict(os.environ, {"ITERM_IO_BRIDGE_DIRECT": "0"}, clear=False):
            with mock.patch(
                "agents.iterm_bridge._run_io_via_subprocess",
                return_value={"ok": True, "action": "read"},
            ) as run_sub:
                result = bridge.read_iterm_output(all_agents=True, read_lines=5)

        self.assertTrue(result["ok"])
        run_sub.assert_called_once()
        self.assertEqual(run_sub.call_args.kwargs["action"], "read")

    def test_send_direct_mode_uses_iterm_api_path(self):
        session = bridge.AgentSession(index=1, agent_id="agent_01", agent_name="Agent 01", session_id="SID")
        rows = [{"agent_id": "agent_01", "error": "", "sent": True, "read": True, "output": []}]

        with mock.patch.dict(os.environ, {"ITERM_IO_BRIDGE_DIRECT": "1"}, clear=False):
            with mock.patch("agents.iterm_bridge._normalize_state_file", return_value=Path("/tmp/state.json")):
                with mock.patch("agents.iterm_bridge._load_state", return_value={"tab_count": 1}):
                    with mock.patch(
                        "agents.iterm_bridge._run_direct_with_optional_rebind",
                        return_value={
                            "targets": [session],
                            "rows": rows,
                            "state_rebound": False,
                            "rebound_count": 0,
                            "rebind_error": "",
                        },
                    ):
                        with mock.patch("agents.iterm_bridge._run_io_via_subprocess") as run_sub:
                            result = bridge.send_iterm_input(text="hello", all_agents=True, read_lines=5)

        self.assertTrue(result["ok"])
        self.assertEqual(result["target_count"], 1)
        self.assertFalse(result["state_rebound"])
        self.assertEqual(result["rebound_count"], 0)
        run_sub.assert_not_called()

    def test_send_rebinds_when_session_stale(self):
        state_path = Path("/tmp/state.json")
        stale_state = {
            "window_id": "window-1",
            "agents": [
                {
                    "index": 1,
                    "agent_id": "agent_01",
                    "agent_name": "Agent 01",
                    "session_id": "OLD-SID",
                }
            ],
            "session_ids": ["OLD-SID"],
        }
        rebound_state = {
            "window_id": "window-1",
            "agents": [
                {
                    "index": 1,
                    "agent_id": "agent_01",
                    "agent_name": "Agent 01",
                    "session_id": "NEW-SID",
                }
            ],
            "session_ids": ["NEW-SID"],
        }

        stale_session = bridge.AgentSession(index=1, agent_id="agent_01", agent_name="Agent 01", session_id="OLD-SID")
        rebound_session = bridge.AgentSession(index=1, agent_id="agent_01", agent_name="Agent 01", session_id="NEW-SID")

        first_rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "OLD-SID",
                "sent": False,
                "read": False,
                "output": [],
                "error": "session not found in iTerm",
            }
        ]
        second_rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "NEW-SID",
                "sent": True,
                "read": True,
                "output": ["READY"],
                "error": "",
            }
        ]

        with mock.patch("agents.iterm_bridge._build_agent_sessions", side_effect=[[stale_session], [rebound_session]]):
            with mock.patch("agents.iterm_bridge._resolve_targets", side_effect=[[stale_session], [rebound_session]]):
                with mock.patch("agents.iterm_bridge._run_iterm_io", side_effect=[first_rows, second_rows]) as run_io:
                    with mock.patch(
                        "agents.iterm_bridge._rebind_state_sessions",
                        return_value={
                            "state": rebound_state,
                            "rebound": True,
                            "rebound_count": 1,
                            "reason": "rebound_applied",
                        },
                    ) as rebind:
                        result = bridge._run_direct_with_optional_rebind(
                            state_path=state_path,
                            state=stale_state,
                            target_agent_ids=[],
                            all_agents=True,
                            text="hello",
                            append_enter=True,
                            wait_sec=0.3,
                            read_lines=10,
                        )

        self.assertEqual(run_io.call_count, 2)
        rebind.assert_called_once_with(state_path, stale_state)
        self.assertTrue(result["state_rebound"])
        self.assertEqual(result["rebound_count"], 1)
        self.assertEqual(result["rows"], second_rows)
        self.assertEqual(result["targets"][0].session_id, "NEW-SID")

    def test_rebind_state_updates_state_file(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            state = {
                "window_id": "window-old",
                "tab_count": 1,
                "agents": [
                    {
                        "index": 1,
                        "agent_id": "agent_01",
                        "agent_name": "Agent 01",
                        "session_id": "OLD-SID",
                    }
                ],
                "session_ids": ["OLD-SID"],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            with mock.patch(
                "agents.iterm_bridge._list_live_sessions",
                return_value=(
                    "window-new",
                    [
                        {
                            "session_id": "NEW-SID",
                            "badge": "A01",
                            "agent_id": "agent_01",
                            "agent_name": "Agent 01",
                            "agent_label": "agent_01 | Agent 01",
                            "name": "agent_01 | Agent 01",
                            "session_name": "node",
                        }
                    ],
                ),
            ):
                result = bridge._rebind_state_sessions(state_path, state)

            self.assertTrue(result["rebound"])
            self.assertGreaterEqual(result["rebound_count"], 2)

            updated = json.loads(state_path.read_text(encoding="utf-8"))
            self.assertEqual(updated["window_id"], "window-new")
            self.assertEqual(updated["session_ids"], ["NEW-SID"])
            self.assertEqual(updated["agents"][0]["session_id"], "NEW-SID")

    def test_rebind_prefers_identity_over_position(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            state = {
                "window_id": "window-old",
                "count": 2,
                "tab_count": 2,
                "agents": [
                    {
                        "index": 1,
                        "agent_id": "agent_01",
                        "agent_name": "Agent 01",
                        "session_label": "agent_01 | Agent 01",
                        "badge": "A01",
                        "session_id": "SID-A01",
                    },
                    {
                        "index": 2,
                        "agent_id": "agent_02",
                        "agent_name": "Agent 02",
                        "session_label": "agent_02 | Agent 02",
                        "badge": "A02",
                        "session_id": "SID-A02",
                    },
                ],
                "session_ids": ["SID-A01", "SID-A02"],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            live = [
                {
                    "session_id": "SID-A02",
                    "badge": "A02",
                    "agent_id": "agent_02",
                    "agent_name": "Agent 02",
                    "agent_label": "agent_02 | Agent 02",
                    "name": "agent_02 | Agent 02",
                    "session_name": "node",
                }
            ]

            with mock.patch("agents.iterm_bridge._list_live_sessions", return_value=("window-old", live)):
                result = bridge._rebind_state_sessions(state_path, state)

            self.assertTrue(result["rebound"])
            updated = json.loads(state_path.read_text(encoding="utf-8"))
            self.assertEqual(updated["agents"][0]["session_id"], "")
            self.assertEqual(updated["agents"][1]["session_id"], "SID-A02")
            self.assertEqual(updated["session_ids"], ["SID-A02"])
            self.assertEqual(updated["tab_count"], 2)

    def test_rebind_fail_keeps_original_rows(self):
        stale_state = {
            "window_id": "window-1",
            "agents": [
                {
                    "index": 1,
                    "agent_id": "agent_01",
                    "agent_name": "Agent 01",
                    "session_id": "OLD-SID",
                }
            ],
            "session_ids": ["OLD-SID"],
        }
        stale_session = bridge.AgentSession(index=1, agent_id="agent_01", agent_name="Agent 01", session_id="OLD-SID")
        stale_rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "OLD-SID",
                "sent": False,
                "read": False,
                "output": [],
                "error": "session not found in iTerm",
            }
        ]

        with mock.patch("agents.iterm_bridge._build_agent_sessions", return_value=[stale_session]):
            with mock.patch("agents.iterm_bridge._resolve_targets", return_value=[stale_session]):
                with mock.patch("agents.iterm_bridge._run_iterm_io", return_value=stale_rows):
                    with mock.patch(
                        "agents.iterm_bridge._rebind_state_sessions",
                        side_effect=RuntimeError("cannot rebind"),
                    ):
                        result = bridge._run_direct_with_optional_rebind(
                            state_path=Path("/tmp/state.json"),
                            state=stale_state,
                            target_agent_ids=[],
                            all_agents=True,
                            text=None,
                            append_enter=True,
                            wait_sec=0.0,
                            read_lines=5,
                        )

        self.assertFalse(result["state_rebound"])
        self.assertEqual(result["rebound_count"], 0)
        self.assertIn("cannot rebind", result["rebind_error"])
        self.assertEqual(result["rows"], stale_rows)

    def test_subprocess_bridge_sets_direct_mode_env(self):
        completed = mock.Mock(returncode=0, stdout='{"ok": true}', stderr="")
        with mock.patch("agents.iterm_bridge.subprocess.run", return_value=completed) as run_cmd:
            result = bridge._run_io_via_subprocess(
                action="send",
                text="hello",
                agent_id="agent_01",
                all_agents=False,
                wait_sec=0.2,
                read_lines=3,
                state_file="",
            )

        self.assertTrue(result["ok"])
        env = run_cmd.call_args.kwargs["env"]
        self.assertEqual(env.get("ITERM_IO_BRIDGE_DIRECT"), "1")


if __name__ == "__main__":
    unittest.main()
