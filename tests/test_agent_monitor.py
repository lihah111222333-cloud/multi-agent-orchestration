import unittest

from agent_monitor import classify_status


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


if __name__ == "__main__":
    unittest.main()
