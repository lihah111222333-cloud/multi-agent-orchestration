import json
import tempfile
import unittest
from pathlib import Path

from iterm_tab_planner import (
    estimate_tabs_from_architecture,
    estimate_tabs_from_task,
    normalize_requested_tabs,
    planner_decide_tab_count,
)


class ItermTabPlannerTests(unittest.TestCase):
    def test_normalize_requested_tabs(self):
        self.assertEqual(normalize_requested_tabs(4), 4)
        self.assertEqual(normalize_requested_tabs(12), 12)

        with self.assertRaises(ValueError):
            normalize_requested_tabs(5)

    def test_estimate_from_task(self):
        tabs, _ = estimate_tabs_from_task("做一次多agent并发全链路压测与拓扑优化")
        self.assertIn(tabs, (4, 6, 8, 12))
        self.assertGreaterEqual(tabs, 4)

    def test_estimate_from_architecture(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            path = Path(tmpdir) / "config.json"
            raw = {
                "gateways": [
                    {
                        "id": "g1",
                        "name": "G1",
                        "agents": [{"id": f"a{i}"} for i in range(6)],
                    },
                    {
                        "id": "g2",
                        "name": "G2",
                        "agents": [{"id": f"b{i}"} for i in range(6)],
                    },
                ]
            }
            path.write_text(json.dumps(raw, ensure_ascii=False), encoding="utf-8")

            tabs, reason = estimate_tabs_from_architecture(path)
            self.assertEqual(tabs, 12)
            self.assertIn("agents=12", reason)

    def test_planner_decision_uses_max(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            path = Path(tmpdir) / "config.json"
            raw = {
                "gateways": [
                    {
                        "id": "g1",
                        "name": "G1",
                        "agents": [{"id": "a1"}, {"id": "a2"}],
                    }
                ]
            }
            path.write_text(json.dumps(raw, ensure_ascii=False), encoding="utf-8")

            decision = planner_decide_tab_count(
                task="请做一次多agent并发全链路压测与容灾演练",
                config_path=path,
            )
            self.assertIn(decision["tab_count"], (4, 6, 8, 12))
            self.assertGreaterEqual(decision["tab_count"], 4)
            self.assertIn("最终取 max", decision["reason"])


if __name__ == "__main__":
    unittest.main()
