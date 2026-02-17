import json
import threading
import unittest
from contextlib import contextmanager
from unittest.mock import patch
from urllib import error, request

import dashboard


@contextmanager
def run_dashboard_server():
    server = dashboard.http.server.ThreadingHTTPServer(("127.0.0.1", 0), dashboard.DashboardHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base_url = f"http://127.0.0.1:{server.server_port}"
    try:
        yield base_url
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def request_json(base_url: str, path: str, method: str = "GET", payload: dict | None = None) -> tuple[int, dict]:
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"

    req = request.Request(f"{base_url}{path}", data=data, headers=headers, method=method)
    try:
        with request.urlopen(req, timeout=5) as resp:
            body = resp.read().decode("utf-8")
            return int(resp.status), json.loads(body)
    except error.HTTPError as exc:
        try:
            body = exc.read().decode("utf-8")
        finally:
            exc.close()
        return int(exc.code), json.loads(body)


class DashboardAgentStatusApiTests(unittest.TestCase):
    def test_api_agent_status_reads_agent_status_table(self) -> None:
        rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "s1",
                "status": "running",
                "stagnant_sec": 4,
                "error": "",
                "output_tail": ["heartbeat ok"],
                "updated_at": "2026-02-10T12:00:00+00:00",
            },
            {
                "agent_id": "agent_02",
                "agent_name": "Agent 02",
                "session_id": "s2",
                "status": "error",
                "stagnant_sec": 18,
                "error": "Traceback: boom",
                "output_tail": ["Traceback: boom"],
                "updated_at": "2026-02-10T12:00:01+00:00",
            },
        ]

        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "ensure_agent_monitor_started", return_value=None):
                with patch.object(dashboard, "query_agent_status", return_value=rows) as query_mock:
                    code, payload = request_json(base_url, "/api/agent-status")

        self.assertTrue(query_mock.called)
        self.assertEqual(code, 200)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["summary"]["total"], 2)
        self.assertEqual(payload["summary"]["healthy"], 1)
        self.assertEqual(payload["summary"]["error"], 1)
        by_id = {row["agent_id"]: row for row in payload["agents"]}
        self.assertEqual(by_id["agent_01"]["status"], "running")
        self.assertEqual(by_id["agent_02"]["status"], "error")

    def test_api_agent_status_returns_503_when_table_missing(self) -> None:
        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "ensure_agent_monitor_started", return_value=None):
                with patch.object(
                    dashboard,
                    "query_agent_status",
                    side_effect=RuntimeError('relation "agent_status" does not exist'),
                ):
                    code, payload = request_json(base_url, "/api/agent-status")

        self.assertEqual(code, 503)
        self.assertFalse(payload["ok"])
        self.assertEqual(payload.get("error"), "agent_status_table_unavailable")
        self.assertEqual(payload["summary"]["total"], 0)

    def test_sse_stream_emits_initial_agent_status_event(self) -> None:
        class _OneShotQueue:
            def get(self, timeout=None):
                raise dashboard.queue.Empty

        class _OneShotWriter:
            def __init__(self):
                self.chunks: list[bytes] = []

            def write(self, data: bytes):
                self.chunks.append(data)

            def flush(self):
                if len(self.chunks) >= 3:
                    raise BrokenPipeError("stop after initial events")

        handler = dashboard.DashboardHandler.__new__(dashboard.DashboardHandler)
        sent_headers: list[tuple[str, str]] = []
        sent_codes: list[int] = []

        handler.send_response = lambda code: sent_codes.append(int(code))
        handler.send_header = lambda key, value: sent_headers.append((str(key), str(value)))
        handler.end_headers = lambda: None
        handler.wfile = _OneShotWriter()

        table_rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "s1",
                "status": "running",
                "stagnant_sec": 1,
                "error": "",
                "output_tail": ["heartbeat ok"],
                "updated_at": "2026-02-10T12:00:01+00:00",
            }
        ]

        with patch.object(dashboard.EVENT_BUS, "subscribe", return_value=_OneShotQueue()):
            with patch.object(dashboard.EVENT_BUS, "unsubscribe", return_value=None):
                with patch.object(dashboard, "query_agent_status", return_value=table_rows):
                    with patch.object(dashboard, "_safe_int", return_value=1):
                        with patch.object(dashboard, "ensure_agent_monitor_started", return_value=None):
                            handler._serve_event_stream()

        self.assertIn(200, sent_codes)
        body = b"".join(handler.wfile.chunks).decode("utf-8")
        self.assertIn("event: connected\n", body)
        self.assertIn("event: agent_status\n", body)
        self.assertIn('"agent_id": "agent_01"', body)


if __name__ == "__main__":
    unittest.main()
