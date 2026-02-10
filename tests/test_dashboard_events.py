import json
import unittest

import dashboard


class DashboardEventTests(unittest.TestCase):
    def test_event_bus_publish_to_subscriber(self):
        bus = dashboard.DashboardEventBus(queue_size=4)
        channel = bus.subscribe()
        try:
            sent = bus.publish("sync", {"scope": ["audit"]})
            received = channel.get_nowait()
        finally:
            bus.unsubscribe(channel)

        self.assertEqual(received["event"], "sync")
        self.assertEqual(received["payload"]["scope"], ["audit"])
        self.assertEqual(received["id"], sent["id"])

    def test_event_bus_drops_oldest_when_queue_full(self):
        bus = dashboard.DashboardEventBus(queue_size=1)
        channel = bus.subscribe()
        try:
            bus.publish("first", {})
            bus.publish("second", {})
            received = channel.get_nowait()
        finally:
            bus.unsubscribe(channel)

        self.assertEqual(received["event"], "second")

    def test_encode_sse_event(self):
        raw = dashboard._encode_sse_event(
            {
                "id": 42,
                "event": "sync",
                "ts": "2026-02-10T00:00:00+00:00",
                "payload": {"scope": ["approvals"]},
            }
        )
        text = raw.decode("utf-8")
        self.assertIn("id: 42\n", text)
        self.assertIn("event: sync\n", text)

        data_lines = [line for line in text.splitlines() if line.startswith("data: ")]
        self.assertEqual(len(data_lines), 1)
        payload = json.loads(data_lines[0][len("data: ") :])
        self.assertEqual(payload["payload"]["scope"], ["approvals"])

    def test_publish_agent_status_event(self):
        bus = dashboard.DashboardEventBus(queue_size=4)
        old_bus = dashboard.EVENT_BUS
        dashboard.EVENT_BUS = bus
        channel = bus.subscribe()
        try:
            dashboard._publish_agent_status_event(
                {
                    "ok": True,
                    "ts": "2026-02-10T00:00:00+00:00",
                    "summary": {"total": 1, "healthy": 1, "unhealthy": 0, "running": 1, "idle": 0, "stuck": 0, "error": 0, "disconnected": 0, "unknown": 0},
                    "agents": [{"agent_id": "agent_01", "status": "running"}],
                    "source": {"db_ok": True},
                }
            )
            received = channel.get_nowait()
        finally:
            bus.unsubscribe(channel)
            dashboard.EVENT_BUS = old_bus

        self.assertEqual(received["event"], "agent_status")
        self.assertTrue(received["payload"]["ok"])
        self.assertEqual(received["payload"]["summary"]["running"], 1)

    def test_ensure_agent_monitor_started_starts_once(self):
        class DummyThread:
            def __init__(self, target=None, name=None, daemon=None):
                self._alive = False

            def start(self):
                self._alive = True

            def is_alive(self):
                return self._alive

            def join(self, timeout=None):
                self._alive = False

        original_thread_cls = dashboard.threading.Thread
        original_thread = dashboard._AGENT_MONITOR_THREAD
        try:
            dashboard.threading.Thread = DummyThread
            dashboard._AGENT_MONITOR_THREAD = None
            dashboard._AGENT_MONITOR_STOP_EVENT.clear()

            dashboard.ensure_agent_monitor_started()
            first_thread = dashboard._AGENT_MONITOR_THREAD
            dashboard.ensure_agent_monitor_started()
            second_thread = dashboard._AGENT_MONITOR_THREAD

            self.assertIsNotNone(first_thread)
            self.assertIs(first_thread, second_thread)
            self.assertTrue(first_thread.is_alive())
        finally:
            dashboard.threading.Thread = original_thread_cls
            dashboard._AGENT_MONITOR_THREAD = original_thread


if __name__ == "__main__":
    unittest.main()
