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


if __name__ == "__main__":
    unittest.main()
