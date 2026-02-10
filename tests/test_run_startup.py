import asyncio
import unittest
from unittest.mock import patch

import run as run_module


class _DummyGraph:
    async def ainvoke(self, _payload):
        return {"final_answer": "ok"}


class RunStartupTests(unittest.TestCase):
    def test_run_migrates_before_validate_config(self):
        call_order: list[str] = []

        def _mark_migrate():
            call_order.append("migrate")

        def _mark_validate():
            call_order.append("validate")

        with patch("run.setup_global_logging", return_value=None):
            with patch("run.ensure_schema", side_effect=_mark_migrate):
                with patch("run.validate_config", side_effect=_mark_validate):
                    with patch("run.build_graph", return_value=_DummyGraph()):
                        with patch("run.append_event", return_value=None):
                            with patch("run.time.time", side_effect=[100.0, 100.5]):
                                result = asyncio.run(run_module.run("demo task"))

        self.assertEqual(result["final_answer"], "ok")
        self.assertEqual(call_order[:2], ["migrate", "validate"])

    def test_run_stops_when_migration_fails(self):
        with patch("run.setup_global_logging", return_value=None):
            with patch("run.ensure_schema", side_effect=RuntimeError("migration failed")):
                with patch("run.validate_config", return_value=None) as mocked_validate:
                    with self.assertRaises(SystemExit):
                        asyncio.run(run_module.run("demo task"))

        mocked_validate.assert_not_called()


if __name__ == "__main__":
    unittest.main()
