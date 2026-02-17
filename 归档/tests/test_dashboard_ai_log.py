import json
import threading
import unittest
from contextlib import contextmanager
from unittest import mock
from urllib import request

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


def request_json(base_url: str, path: str) -> tuple[int, dict]:
    req = request.Request(f"{base_url}{path}", method="GET")
    with request.urlopen(req, timeout=5) as resp:
        body = resp.read().decode("utf-8")
        return int(resp.status), json.loads(body)


class DashboardAiLogApiTests(unittest.TestCase):
    def test_api_ai_log_uses_query_filters(self):
        logs = [
            {
                "ts": "2026-02-13 23:00:00",
                "level": "INFO",
                "logger": "httpx",
                "message": "HTTP Request: POST ...",
                "raw": "",
                "category": "api_request",
                "method": "POST",
                "url": "https://api.gpteamservices.com/v1/responses",
                "endpoint": "/v1/responses",
                "status_code": "200",
                "status_text": "OK",
                "model": "",
            }
        ]
        filters = {
            "levels": ["INFO"],
            "loggers": ["httpx"],
            "categories": ["api_request"],
            "endpoints": ["/v1/responses"],
            "status_codes": ["200"],
        }

        with run_dashboard_server() as base_url:
            with (
                mock.patch.object(dashboard, "query_ai_logs", return_value=logs) as query_mock,
                mock.patch.object(dashboard, "list_ai_filter_values", return_value=filters),
            ):
                code, payload = request_json(
                    base_url,
                    "/api/ai-log?limit=50&level=INFO&logger=httpx&category=api_request"
                    "&endpoint=%2Fv1%2Fresponses&status_code=200&keyword=test",
                )

        self.assertEqual(code, 200)
        self.assertTrue(payload.get("ok"))
        self.assertEqual(payload.get("logs"), logs)
        self.assertEqual(payload.get("filters"), filters)
        query_mock.assert_called_once_with(
            limit=50,
            level="INFO",
            logger_name="httpx",
            keyword="test",
            category="api_request",
            endpoint="/v1/responses",
            status_code="200",
        )

    def test_api_ai_log_export_returns_ndjson(self):
        logs = [
            {
                "ts": "2026-02-13 23:00:00",
                "level": "ERROR",
                "logger": "gateways.gateway",
                "message": "Error code: 404",
                "raw": "",
                "category": "api_error",
                "method": "",
                "url": "",
                "endpoint": "",
                "status_code": "404",
                "status_text": "error",
                "model": "",
            }
        ]

        with run_dashboard_server() as base_url:
            with mock.patch.object(dashboard, "query_ai_logs", return_value=logs):
                req = request.Request(f"{base_url}/api/ai-log/export?limit=20", method="GET")
                with request.urlopen(req, timeout=5) as resp:
                    body = resp.read().decode("utf-8")
                    content_type = resp.headers.get("Content-Type", "")

        self.assertIn("application/x-ndjson", content_type)
        self.assertIn('"status_code": "404"', body)


if __name__ == "__main__":
    unittest.main()
