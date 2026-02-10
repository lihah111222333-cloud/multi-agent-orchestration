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
    def setUp(self) -> None:
        dashboard._AGENT_STATUS_MEMORY.clear()

    def test_api_agent_status_prefers_agent_status_table_when_available(self) -> None:
        table_rows = [
            {
                "agent_id": "agent_01",
                "agent_name": "Agent 01",
                "session_id": "s1",
                "status": "running",
                "stagnant_sec": 4,
                "error": "",
                "output_tail": ["heartbeat ok", "processed 1 item"],
            },
            {
                "agent_id": "agent_02",
                "agent_name": "Agent 02",
                "session_id": "s2",
                "status": "error",
                "stagnant_sec": 18,
                "error": "Traceback: boom",
                "output_tail": ["Traceback: boom"],
            },
        ]

        legacy_sessions_payload = {
            "ok": True,
            "sessions": [
                {
                    "agent_id": "agent_99",
                    "agent_name": "Legacy Agent",
                    "session_id": "legacy-s1",
                }
            ],
        }
        legacy_outputs_payload = {
            "ok": True,
            "results": [
                {
                    "agent_id": "agent_99",
                    "output": ["legacy output"],
                    "error": "",
                }
            ],
        }

        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "fetch_all", return_value=table_rows, create=True) as fetch_all_mock:
                with patch.object(dashboard, "list_iterm_agent_sessions", return_value=legacy_sessions_payload):
                    with patch.object(dashboard, "read_iterm_output", return_value=legacy_outputs_payload):
                        code, payload = request_json(base_url, "/api/agent-status")

        self.assertTrue(fetch_all_mock.called)
        self.assertEqual(code, 200)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["summary"]["total"], 2)
        self.assertEqual(payload["summary"]["healthy"], 1)
        self.assertEqual(payload["summary"]["unhealthy"], 1)

        by_id = {row["agent_id"]: row for row in payload["agents"]}
        self.assertIn("agent_01", by_id)
        self.assertIn("agent_02", by_id)
        self.assertNotIn("agent_99", by_id)
        self.assertEqual(by_id["agent_01"]["status"], "running")
        self.assertEqual(by_id["agent_02"]["status"], "error")

    def test_api_agent_status_falls_back_when_agent_status_table_missing(self) -> None:
        sessions_payload = {
            "ok": True,
            "sessions": [
                {
                    "agent_id": "agent_01",
                    "agent_name": "Agent 01",
                    "session_id": "s1",
                },
                {
                    "agent_id": "agent_02",
                    "agent_name": "Agent 02",
                    "session_id": "s2",
                },
            ],
        }
        outputs_payload = {
            "ok": True,
            "results": [
                {
                    "agent_id": "agent_01",
                    "output": ["heartbeat ok", "processed 1 item"],
                    "error": "",
                },
                {
                    "agent_id": "agent_02",
                    "output": ["Traceback: boom"],
                    "error": "",
                },
            ],
        }

        with run_dashboard_server() as base_url:
            with patch.object(
                dashboard,
                "fetch_all",
                side_effect=RuntimeError('relation "agent_status" does not exist'),
                create=True,
            ) as fetch_all_mock:
                with patch.object(dashboard, "list_iterm_agent_sessions", return_value=sessions_payload):
                    with patch.object(dashboard, "read_iterm_output", return_value=outputs_payload):
                        code, payload = request_json(base_url, "/api/agent-status")

        self.assertTrue(fetch_all_mock.called)
        self.assertEqual(code, 200)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["summary"]["total"], 2)
        by_id = {row["agent_id"]: row for row in payload["agents"]}
        self.assertEqual(by_id["agent_01"]["status"], "running")
        self.assertEqual(by_id["agent_02"]["status"], "error")

    def test_api_agent_status_success(self) -> None:
        sessions_payload = {
            "ok": True,
            "sessions": [
                {
                    "agent_id": "agent_01",
                    "agent_name": "Agent 01",
                    "session_id": "s1",
                },
                {
                    "agent_id": "agent_02",
                    "agent_name": "Agent 02",
                    "session_id": "s2",
                },
            ],
        }
        outputs_payload = {
            "ok": True,
            "results": [
                {
                    "agent_id": "agent_01",
                    "output": ["heartbeat ok", "processed 1 item"],
                    "error": "",
                },
                {
                    "agent_id": "agent_02",
                    "output": ["Traceback: boom"],
                    "error": "",
                },
            ],
        }

        with run_dashboard_server() as base_url:
            with patch.object(
                dashboard,
                "fetch_all",
                side_effect=RuntimeError('relation "agent_status" does not exist'),
                create=True,
            ):
                with patch.object(dashboard, "list_iterm_agent_sessions", return_value=sessions_payload):
                    with patch.object(dashboard, "read_iterm_output", return_value=outputs_payload):
                        code, payload = request_json(base_url, "/api/agent-status?lines=20")

        self.assertEqual(code, 200)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["summary"]["total"], 2)
        by_id = {row["agent_id"]: row for row in payload["agents"]}
        self.assertEqual(by_id["agent_01"]["status"], "running")
        self.assertEqual(by_id["agent_02"]["status"], "error")

    def test_api_agent_status_returns_503_when_session_list_fails(self) -> None:
        with run_dashboard_server() as base_url:
            with patch.object(
                dashboard,
                "fetch_all",
                side_effect=RuntimeError('relation "agent_status" does not exist'),
                create=True,
            ):
                with patch.object(
                    dashboard,
                    "list_iterm_agent_sessions",
                    return_value={"ok": False, "error": "state file missing"},
                ):
                    code, payload = request_json(base_url, "/api/agent-status")

        self.assertEqual(code, 503)
        self.assertFalse(payload["ok"])
        self.assertIn("state file missing", payload.get("error", ""))

    def test_api_agent_status_returns_503_when_output_read_fails(self) -> None:
        sessions_payload = {
            "ok": True,
            "sessions": [
                {
                    "agent_id": "agent_01",
                    "agent_name": "Agent 01",
                    "session_id": "s1",
                }
            ],
        }

        with run_dashboard_server() as base_url:
            with patch.object(
                dashboard,
                "fetch_all",
                side_effect=RuntimeError('relation "agent_status" does not exist'),
                create=True,
            ):
                with patch.object(dashboard, "list_iterm_agent_sessions", return_value=sessions_payload):
                    with patch.object(
                        dashboard,
                        "read_iterm_output",
                        return_value={"ok": False, "error": "iTerm unavailable"},
                    ):
                        code, payload = request_json(base_url, "/api/agent-status")

        self.assertEqual(code, 503)
        self.assertFalse(payload["ok"])
        self.assertEqual(payload["summary"]["unknown"], 1)
        self.assertIn("iTerm unavailable", payload.get("error", ""))


if __name__ == "__main__":
    unittest.main()
