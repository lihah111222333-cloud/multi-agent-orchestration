import asyncio
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, patch

import scripts.iterm_launch_agents as launcher


class ItermLaunchAgentsTests(unittest.TestCase):
    def test_build_identity_prompt(self):
        text = launcher._build_identity_prompt(
            "你是 {agent_id} / {agent_name}，回复 {agent_id}",
            index=3,
            agent_id="a03",
            agent_name="Codex Agent 03",
        )
        self.assertEqual(text, "你是 a03 / Codex Agent 03，回复 a03")

    def test_default_identity_prompt_uses_agent_id_direct_reply(self):
        text = launcher._build_identity_prompt(
            launcher._default_identity_template(),
            index=1,
            agent_id="a01",
            agent_name="Runtime Agent 01",
        )
        self.assertEqual(text, "唤醒检查：你的固定身份是 a01（Runtime Agent 01）。现在仅回复 a01。")

    def test_build_identity_prompt_unknown_variable(self):
        with self.assertRaises(ValueError):
            launcher._build_identity_prompt(
                "{unknown}",
                index=1,
                agent_id="agent_01",
                agent_name="Agent 01",
            )

    def test_build_agent_entries_adds_labels_and_identity(self):
        entries = launcher._build_agent_entries(
            2,
            start_template="codex --no-alt-screen",
            name_prefix="Codex Agent",
            project_root=Path("/tmp/project"),
            inject_identity=True,
            identity_template="身份={agent_id};回复={agent_id}",
        )

        self.assertEqual(len(entries), 2)
        self.assertEqual(entries[0]["agent_id"], "a01")
        self.assertEqual(entries[0]["session_label"], "a01 | Codex Agent 01")
        self.assertEqual(entries[0]["badge"], "A01")
        self.assertEqual(entries[0]["identity_prompt"], "身份=a01;回复=a01")
        self.assertIn("exec codex --no-alt-screen", entries[0]["shell_cmd"])

    def test_build_agent_entries_can_disable_identity(self):
        entries = launcher._build_agent_entries(
            1,
            start_template="codex --no-alt-screen",
            name_prefix="Codex Agent",
            project_root=Path("/tmp/project"),
            inject_identity=False,
            identity_template="身份={agent_id}",
        )

        self.assertEqual(entries[0]["identity_prompt"], "")

    def test_reapply_labels_after_wakeup(self):
        rows = [
            {
                "session": object(),
                "entry": {"agent_id": "a01"},
                "tab": object(),
                "label_applied": False,
                "badge_applied": False,
                "agent_id_applied": False,
                "agent_name_applied": False,
                "agent_label_applied": False,
                "tab_title_applied": False,
            }
        ]

        decorate_mock = AsyncMock(
            return_value={
                "label_applied": True,
                "badge_applied": True,
                "agent_id_applied": True,
                "agent_name_applied": True,
                "agent_label_applied": True,
                "tab_title_applied": True,
            }
        )

        with patch("scripts.iterm_launch_agents._decorate_session", decorate_mock):
            count = asyncio.run(launcher._reapply_labels_after_wakeup(rows, relabel_delay_sec=0))

        self.assertEqual(count, 1)
        self.assertTrue(rows[0]["label_applied"])
        self.assertTrue(rows[0]["badge_applied"])
        self.assertTrue(rows[0]["agent_id_applied"])
        self.assertTrue(rows[0]["agent_name_applied"])
        self.assertTrue(rows[0]["agent_label_applied"])
        self.assertTrue(rows[0]["tab_title_applied"])


if __name__ == "__main__":
    unittest.main()
