import os
import unittest
from unittest import mock

from agents import runtime_control


class RuntimeControlTests(unittest.TestCase):
    def tearDown(self):
        runtime_control.shutdown_runtime()

    def test_runtime_initialize_and_shutdown_with_gc_disabled(self):
        runtime_control.shutdown_runtime()

        with mock.patch.dict(os.environ, {"AGENT_GC_ENABLED": "0"}, clear=False):
            runtime_control.initialize_agent_runtime("agent_test")
            status = runtime_control.runtime_status()
            self.assertTrue(status["worker_ready"])
            self.assertTrue(status["runtime_ready"])
            self.assertFalse(status["gc_running"])

            result = runtime_control.run_in_agent_thread(lambda: "ok")
            self.assertEqual(result, "ok")

        runtime_control.shutdown_runtime()
        status = runtime_control.runtime_status()
        self.assertFalse(status["worker_ready"])
        self.assertFalse(status["gc_running"])
        self.assertFalse(status["runtime_ready"])

    def test_runtime_initialize_starts_gc_thread_when_enabled(self):
        runtime_control.shutdown_runtime()

        env = {
            "AGENT_GC_ENABLED": "1",
            "AGENT_GC_INTERVAL_SEC": "1",
            "AGENT_GC_GENERATION": "2",
            "AGENT_MEMORY_WARN_MB": "0",
        }
        with mock.patch.dict(os.environ, env, clear=False):
            runtime_control.initialize_agent_runtime("agent_gc")
            status = runtime_control.runtime_status()
            self.assertTrue(status["worker_ready"])
            self.assertTrue(status["runtime_ready"])
            self.assertTrue(status["gc_running"])


if __name__ == "__main__":
    unittest.main()
