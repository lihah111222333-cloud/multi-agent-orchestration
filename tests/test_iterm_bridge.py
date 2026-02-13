import asyncio
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import agents.iterm_bridge as bridge


class ItermBridgeTests(unittest.TestCase):
    def test_parse_agent_ids_normalizes_legacy_short_form(self):
        parsed = bridge._parse_agent_ids("a01, agent_02;A03")
        self.assertEqual(parsed, ["agent_01", "agent_02", "agent_03"])

    def test_build_agent_sessions_normalizes_legacy_state_ids(self):
        state = {
            "agents": [
                {"index": 1, "agent_id": "a01", "agent_name": "Agent 01", "session_id": "SID-01"},
                {"index": 2, "agent_id": "agent_02", "agent_name": "Agent 02", "session_id": "SID-02"},
            ]
        }

        sessions = bridge._build_agent_sessions(state)
        self.assertEqual([row.agent_id for row in sessions], ["agent_01", "agent_02"])

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

        def build_sessions_side_effect(current_state):
            sid = str(current_state.get("agents", [{}])[0].get("session_id", ""))
            if sid == "OLD-SID":
                return [stale_session]
            return [rebound_session]

        with mock.patch("agents.iterm_bridge._build_agent_sessions", side_effect=build_sessions_side_effect):
            with mock.patch("agents.iterm_bridge._resolve_targets", side_effect=lambda rows, _ids, all_agents: rows):
                with mock.patch("agents.iterm_bridge._run_iterm_io", side_effect=[first_rows, second_rows]) as run_io:
                    with mock.patch("agents.iterm_bridge._list_live_session_ids", return_value=("window-1", ["OLD-SID"])):
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

    def test_rebind_does_not_bind_agent_to_master_title_text(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            state = {
                "window_id": "window-old",
                "count": 1,
                "tab_count": 1,
                "agents": [
                    {
                        "index": 1,
                        "agent_id": "agent_01",
                        "agent_name": "Runtime Agent 01",
                        "session_label": "agent_01 | Runtime Agent 01",
                        "badge": "A01",
                        "session_id": "SID-STALE",
                    },
                ],
                "session_ids": ["SID-STALE"],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            live = [
                {
                    "session_id": "SID-MASTER",
                    "badge": "",
                    "agent_id": "",
                    "agent_name": "",
                    "agent_label": "",
                    "name": "A0-master 调度 agent_01 恢复",
                    "session_name": "A0-master 调度 agent_01 恢复",
                }
            ]

            with mock.patch("agents.iterm_bridge._list_live_sessions", return_value=("window-old", live)):
                result = bridge._rebind_state_sessions(state_path, state)

            self.assertTrue(result["rebound"])
            updated = json.loads(state_path.read_text(encoding="utf-8"))
            self.assertEqual(updated["agents"][0]["session_id"], "")
            self.assertEqual(updated["session_ids"], [])

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

    def test_list_rebinds_and_fills_missing_agents(self):
        stale_state = {
            "window_id": "window-old",
            "count": 2,
            "tab_count": 2,
            "agents": [],
            "session_ids": [],
        }
        rebound_state = {
            "window_id": "window-new",
            "count": 2,
            "tab_count": 2,
            "agents": [
                {
                    "index": 1,
                    "agent_id": "agent_01",
                    "agent_name": "Runtime Agent 01",
                    "session_id": "SID-01",
                },
                {
                    "index": 2,
                    "agent_id": "agent_02",
                    "agent_name": "Runtime Agent 02",
                    "session_id": "SID-02",
                },
            ],
            "session_ids": ["SID-01", "SID-02"],
        }

        with mock.patch("agents.iterm_bridge._normalize_state_file", return_value=Path("/tmp/state.json")):
            with mock.patch("agents.iterm_bridge._load_state", return_value=stale_state):
                with mock.patch(
                    "agents.iterm_bridge._refresh_state_via_rebind",
                    return_value=(rebound_state, True, 2, ""),
                ):
                    result = bridge.list_iterm_agent_sessions()

        self.assertTrue(result["ok"])
        self.assertTrue(result["state_rebound"])
        self.assertEqual(result["rebound_count"], 2)
        self.assertEqual(result["tab_count"], 2)
        self.assertEqual(len(result["sessions"]), 2)
        self.assertEqual(result["sessions"][0]["session_id"], "SID-01")

    def test_send_precheck_rebind_before_iterm_io(self):
        stale_state = {
            "window_id": "window-old",
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
            "window_id": "window-old",
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
        rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "NEW-SID",
                "sent": True,
                "read": True,
                "output": ["ok"],
                "error": "",
            }
        ]

        with mock.patch("agents.iterm_bridge._build_agent_sessions", side_effect=[[stale_session], [rebound_session]]):
            with mock.patch("agents.iterm_bridge._resolve_targets", side_effect=[[stale_session], [rebound_session]]):
                with mock.patch("agents.iterm_bridge._list_live_session_ids", return_value=("window-old", ["NEW-SID"])):
                    with mock.patch(
                        "agents.iterm_bridge._refresh_state_via_rebind",
                        return_value=(rebound_state, True, 1, ""),
                    ) as refresh:
                        with mock.patch("agents.iterm_bridge._run_iterm_io", return_value=rows) as run_io:
                            result = bridge._run_direct_with_optional_rebind(
                                state_path=Path("/tmp/state.json"),
                                state=stale_state,
                                target_agent_ids=["agent_01"],
                                all_agents=False,
                                text="pwd",
                                append_enter=True,
                                wait_sec=0.0,
                                read_lines=5,
                            )

        self.assertTrue(result["state_rebound"])
        self.assertEqual(result["rebound_count"], 1)
        self.assertEqual(result["targets"][0].session_id, "NEW-SID")
        self.assertEqual(result["rows"], rows)
        refresh.assert_called_once()
        run_io.assert_called_once()

    def test_save_state_is_atomic_replace(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            payload = {"ok": True, "items": [1, 2, 3]}

            with mock.patch("agents.iterm_bridge.os.replace") as replace_mock:
                bridge._save_state(state_path, payload)

            replace_mock.assert_called_once()
            src, dst = replace_mock.call_args.args
            self.assertTrue(str(src).endswith(".tmp-" + str(os.getpid())))
            self.assertEqual(Path(dst), state_path)

    def test_read_tail_lines_merges_soft_wrapped_rows(self):
        class _FakeLine:
            def __init__(self, text: str, hard_eol: bool):
                self.string = text
                self.hard_eol = hard_eol

        class _FakeScreen:
            def __init__(self, rows):
                self._rows = rows
                self.number_of_lines = len(rows)

            def line(self, index: int):
                return self._rows[index]

        class _FakeSession:
            def __init__(self, screen):
                self._screen = screen

            async def async_get_screen_contents(self):
                return self._screen

        rows = [
            _FakeLine("echo WRAP_MARKER_12345", False),
            _FakeLine("67890", True),
            _FakeLine("done", True),
        ]
        session = _FakeSession(_FakeScreen(rows))
        output = asyncio.run(bridge._read_tail_lines(session, lines=10))

        self.assertIn("echo WRAP_MARKER_1234567890", output)
        self.assertIn("done", output)

    def test_read_tail_lines_keeps_hard_eol_separate(self):
        class _FakeLine:
            def __init__(self, text: str, hard_eol: bool):
                self.string = text
                self.hard_eol = hard_eol

        class _FakeScreen:
            def __init__(self, rows):
                self._rows = rows
                self.number_of_lines = len(rows)

            def line(self, index: int):
                return self._rows[index]

        class _FakeSession:
            def __init__(self, screen):
                self._screen = screen

            async def async_get_screen_contents(self):
                return self._screen

        rows = [
            _FakeLine("line-one", True),
            _FakeLine("line-two", True),
        ]
        session = _FakeSession(_FakeScreen(rows))
        output = asyncio.run(bridge._read_tail_lines(session, lines=10))

        self.assertEqual(output, ["line-one", "line-two"])

    def test_rebind_preserves_sessions_when_api_returns_partial(self):
        """When live API returns fewer sessions than agents hold, don't clear."""
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            state = {
                "window_id": "window-old",
                "count": 4,
                "tab_count": 4,
                "agents": [
                    {"index": i, "agent_id": f"agent_{i:02d}",
                     "agent_name": f"Agent {i:02d}",
                     "session_label": f"agent_{i:02d} | Agent {i:02d}",
                     "badge": f"A{i:02d}",
                     "session_id": f"SID-{i:02d}"}
                    for i in range(1, 5)
                ],
                "session_ids": [f"SID-{i:02d}" for i in range(1, 5)],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            # API returns only 1 session (partial / incomplete result)
            live = [
                {
                    "session_id": "SID-NEW-01",
                    "badge": "",
                    "agent_id": "",
                    "agent_name": "",
                    "agent_label": "",
                    "name": "zsh",
                    "session_name": "zsh",
                }
            ]

            with mock.patch("agents.iterm_bridge._list_live_sessions",
                            return_value=("window-old", live)):
                result = bridge._rebind_state_sessions(state_path, state)

            updated = json.loads(state_path.read_text(encoding="utf-8"))
            # Agents should retain their original SIDs (not cleared)
            for i, agent in enumerate(updated["agents"], 1):
                self.assertEqual(
                    agent["session_id"], f"SID-{i:02d}",
                    f"agent_{i:02d} session_id should be preserved, not cleared"
                )

    def test_rebind_prefers_tab_count_over_count(self):
        """When count=0 but tab_count=4, expected_count should use tab_count."""
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            state = {
                "window_id": "window-old",
                "count": 0,
                "tab_count": 4,
                "agents": [
                    {"index": i, "agent_id": f"agent_{i:02d}",
                     "agent_name": f"Agent {i:02d}",
                     "session_label": f"agent_{i:02d} | Agent {i:02d}",
                     "badge": f"A{i:02d}",
                     "session_id": f"SID-{i:02d}"}
                    for i in range(1, 5)
                ],
                "session_ids": [f"SID-{i:02d}" for i in range(1, 5)],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            live = [
                {
                    "session_id": f"SID-{i:02d}",
                    "badge": f"A{i:02d}",
                    "agent_id": f"agent_{i:02d}",
                    "agent_name": f"Agent {i:02d}",
                    "agent_label": f"agent_{i:02d} | Agent {i:02d}",
                    "name": f"agent_{i:02d} | Agent {i:02d}",
                    "session_name": "node",
                }
                for i in range(1, 5)
            ]

            with mock.patch("agents.iterm_bridge._list_live_sessions",
                            return_value=("window-old", live)):
                result = bridge._rebind_state_sessions(state_path, state)

            updated = json.loads(state_path.read_text(encoding="utf-8"))
            # tab_count should be preserved at 4 (not dropped to 0)
            self.assertGreaterEqual(updated["tab_count"], 4)
            self.assertEqual(len(updated["agents"]), 4)

    def test_rebind_position_fallback_on_mass_restart(self):
        """When all sessions change (iTerm restart) and variables are lost,
        position-based Phase 2.5 should rebind by order instead of clearing."""
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            state = {
                "window_id": "window-old",
                "count": 4,
                "tab_count": 4,
                "agents": [
                    {"index": i, "agent_id": f"agent_{i:02d}",
                     "agent_name": f"Agent {i:02d}",
                     "session_label": f"agent_{i:02d} | Agent {i:02d}",
                     "badge": f"A{i:02d}",
                     "session_id": f"OLD-SID-{i:02d}"}
                    for i in range(1, 5)
                ],
                "session_ids": [f"OLD-SID-{i:02d}" for i in range(1, 5)],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            # All sessions changed (new UUIDs), no user variables set
            live = [
                {
                    "session_id": f"NEW-SID-{i:02d}",
                    "badge": "",
                    "agent_id": "",
                    "agent_name": "",
                    "agent_label": "",
                    "name": "zsh",
                    "session_name": "zsh",
                }
                for i in range(1, 5)
            ]

            with mock.patch("agents.iterm_bridge._list_live_sessions",
                            return_value=("window-old", live)), \
                 mock.patch("agents.iterm_bridge._async_reinject_variables"):
                result = bridge._rebind_state_sessions(state_path, state)

            self.assertTrue(result["rebound"])
            updated = json.loads(state_path.read_text(encoding="utf-8"))

            # Agents should be bound to new SIDs by position order
            for i, agent in enumerate(updated["agents"], 1):
                self.assertEqual(
                    agent["session_id"], f"NEW-SID-{i:02d}",
                    f"agent_{i:02d} should be position-matched to NEW-SID-{i:02d}"
                )

    def test_rebind_position_skipped_when_few_unresolved(self):
        """When only 1 of 4 agents is unresolved, position matching should NOT
        activate (minority unresolved)."""
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "iterm_launch_state.json"
            # 3 agents still have valid live sessions; only agent_04 is stale
            state = {
                "window_id": "window-old",
                "count": 4,
                "tab_count": 4,
                "agents": [
                    {"index": i, "agent_id": f"agent_{i:02d}",
                     "agent_name": f"Agent {i:02d}",
                     "session_label": f"agent_{i:02d} | Agent {i:02d}",
                     "badge": f"A{i:02d}",
                     "session_id": f"SID-{i:02d}"}
                    for i in range(1, 5)
                ],
                "session_ids": [f"SID-{i:02d}" for i in range(1, 5)],
            }
            state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")

            # 3 of 4 sessions are still alive; SID-04 is gone, NEW-EXTRA is new
            live = [
                {"session_id": f"SID-{i:02d}", "badge": f"A{i:02d}",
                 "agent_id": f"agent_{i:02d}", "agent_name": f"Agent {i:02d}",
                 "agent_label": f"agent_{i:02d} | Agent {i:02d}",
                 "name": f"agent_{i:02d}", "session_name": "node"}
                for i in range(1, 4)  # only SID-01..03 alive
            ] + [
                {"session_id": "NEW-EXTRA", "badge": "", "agent_id": "",
                 "agent_name": "", "agent_label": "", "name": "zsh",
                 "session_name": "zsh"}
            ]

            with mock.patch("agents.iterm_bridge._list_live_sessions",
                            return_value=("window-old", live)), \
                 mock.patch("agents.iterm_bridge._async_reinject_variables"):
                result = bridge._rebind_state_sessions(state_path, state)

            updated = json.loads(state_path.read_text(encoding="utf-8"))

            # agent_01..03 should keep their SIDs
            for i in range(1, 4):
                self.assertEqual(updated["agents"][i-1]["session_id"], f"SID-{i:02d}")

            # agent_04: stale SID-04 should be cleared (Phase 4), NOT
            # position-matched to NEW-EXTRA
            self.assertEqual(updated["agents"][3]["session_id"], "")

    def test_sync_tui_binding_warning_for_error(self):
        with mock.patch("orchestration_tui_bus.publish_binding_warning") as publish:
            bridge._sync_tui_binding_warning("state rebind failed: timeout")
        publish.assert_called_once_with("state rebind failed: timeout", source="iterm_bridge")

    def test_sync_tui_binding_warning_clears_on_skip_reason(self):
        with mock.patch("orchestration_tui_bus.publish_binding_warning") as publish:
            bridge._sync_tui_binding_warning("state rebind skipped: no_state_change")
        publish.assert_called_once_with(None, source="iterm_bridge")


if __name__ == "__main__":
    unittest.main()
