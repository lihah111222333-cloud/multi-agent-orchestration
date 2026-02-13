import tempfile
import unittest
from pathlib import Path
from unittest import mock

import orchestration_tui_bus as bus


class OrchestrationTuiBusTests(unittest.TestCase):
    def test_begin_update_end_lifecycle(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "orchestration_tui_bus.json"
            lock_path = Path(tmpdir) / ".orchestration_tui_bus.lock"
            with mock.patch.object(bus, "STATE_PATH", state_path), mock.patch.object(bus, "LOCK_PATH", lock_path):
                bus.reset_state(source="test")
                bus.publish_begin(
                    run_id="run-001",
                    status_header="Running orchestration",
                    status_details="phase=plan",
                    source="test",
                )
                bus.publish_update(
                    run_id="run-001",
                    status_header=None,
                    status_details="phase=execute",
                    source="test",
                )

                snapshot = bus.get_snapshot()
                self.assertTrue(snapshot["running"])
                self.assertEqual(snapshot["active_count"], 1)
                self.assertEqual(snapshot["active_runs"][0]["run_id"], "run-001")
                self.assertEqual(snapshot["active_runs"][0]["status_header"], "Running orchestration")
                self.assertEqual(snapshot["active_runs"][0]["status_details"], "phase=execute")

                bus.publish_end("run-001", source="test")
                ended = bus.get_snapshot()
                self.assertFalse(ended["running"])
                self.assertEqual(ended["active_count"], 0)

    def test_binding_warning_set_and_clear(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "orchestration_tui_bus.json"
            lock_path = Path(tmpdir) / ".orchestration_tui_bus.lock"
            with mock.patch.object(bus, "STATE_PATH", state_path), mock.patch.object(bus, "LOCK_PATH", lock_path):
                bus.reset_state(source="test")
                bus.publish_binding_warning("session rebound detected", source="test")
                snapshot = bus.get_snapshot()
                self.assertEqual(snapshot["binding_warning"], "session rebound detected")

                bus.publish_binding_warning(None, source="test")
                cleared = bus.get_snapshot()
                self.assertIsNone(cleared["binding_warning"])

    def test_legacy_running_maps_to_legacy_run_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "orchestration_tui_bus.json"
            lock_path = Path(tmpdir) / ".orchestration_tui_bus.lock"
            with mock.patch.object(bus, "STATE_PATH", state_path), mock.patch.object(bus, "LOCK_PATH", lock_path):
                bus.reset_state(source="test")
                bus.publish_legacy_state(True, status_header="Legacy run", source="test")
                snapshot = bus.get_snapshot()
                self.assertTrue(snapshot["running"])
                self.assertEqual(snapshot["active_runs"][0]["run_id"], bus.LEGACY_RUN_ID)

                bus.publish_legacy_state(False, source="test")
                ended = bus.get_snapshot()
                self.assertFalse(ended["running"])

    def test_legacy_bool_string_false_is_treated_as_false(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "orchestration_tui_bus.json"
            lock_path = Path(tmpdir) / ".orchestration_tui_bus.lock"
            with mock.patch.object(bus, "STATE_PATH", state_path), mock.patch.object(bus, "LOCK_PATH", lock_path):
                bus.reset_state(source="test")
                bus.publish_legacy_state(True, status_header="Legacy run", source="test")
                bus.publish_legacy_state("false", source="test")
                snapshot = bus.get_snapshot()
                self.assertFalse(snapshot["running"])

    def test_list_events_tolerates_invalid_limit_and_since(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "orchestration_tui_bus.json"
            lock_path = Path(tmpdir) / ".orchestration_tui_bus.lock"
            with mock.patch.object(bus, "STATE_PATH", state_path), mock.patch.object(bus, "LOCK_PATH", lock_path):
                bus.reset_state(source="test")
                bus.publish_begin("run-001", source="test")
                payload = bus.list_events(limit="abc", since_seq="xyz")
                self.assertTrue(payload["ok"])
                self.assertGreaterEqual(payload["count"], 1)


if __name__ == "__main__":
    unittest.main()
