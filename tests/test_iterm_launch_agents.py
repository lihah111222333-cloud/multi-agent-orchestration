import unittest
from pathlib import Path

import scripts.iterm_launch_agents as launcher


class ItermLaunchAgentsTests(unittest.TestCase):
    def test_build_identity_prompt(self):
        text = launcher._build_identity_prompt(
            "你是 {agent_id} / {agent_name}，回复 ACK-{index_padded}",
            index=3,
            agent_id="agent_03",
            agent_name="Codex Agent 03",
        )
        self.assertEqual(text, "你是 agent_03 / Codex Agent 03，回复 ACK-03")

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
            identity_template="身份={agent_id};ACK={index_padded}",
        )

        self.assertEqual(len(entries), 2)
        self.assertEqual(entries[0]["agent_id"], "agent_01")
        self.assertEqual(entries[0]["session_label"], "agent_01 | Codex Agent 01")
        self.assertEqual(entries[0]["badge"], "A01")
        self.assertEqual(entries[0]["identity_prompt"], "身份=agent_01;ACK=01")
        self.assertIn("cd /tmp/project", entries[0]["shell_cmd"])

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


if __name__ == "__main__":
    unittest.main()
