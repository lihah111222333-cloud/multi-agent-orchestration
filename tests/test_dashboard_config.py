import json
import threading
import tempfile
import unittest
from contextlib import contextmanager
from pathlib import Path
from unittest.mock import patch
from urllib import error, request

import dashboard
import topology_approval


class DashboardConfigTests(unittest.TestCase):
    def test_safe_bool(self):
        self.assertTrue(dashboard._safe_bool("1"))
        self.assertTrue(dashboard._safe_bool("true"))
        self.assertFalse(dashboard._safe_bool("0"))
        self.assertFalse(dashboard._safe_bool("false"))

    def test_parse_json_object(self):
        self.assertEqual(dashboard._parse_json_object('{"a":1}', "params"), {"a": 1})
        self.assertEqual(dashboard._parse_json_object({}, "params"), {})
        with self.assertRaises(ValueError):
            dashboard._parse_json_object("[]", "params")

    def test_parse_required_int(self):
        self.assertEqual(dashboard._parse_required_int("12", "run_id", 1, 100), 12)
        with self.assertRaises(ValueError):
            dashboard._parse_required_int("", "run_id", 1, 100)
        with self.assertRaises(ValueError):
            dashboard._parse_required_int("abc", "run_id", 1, 100)
        with self.assertRaises(ValueError):
            dashboard._parse_required_int(True, "run_id", 1, 100)

    def test_sanitize_rejects_unknown_key(self):
        with self.assertRaises(ValueError):
            dashboard._sanitize_config_updates({"UNSAFE_KEY": "1"})

    def test_sanitize_accepts_sse_sync_sec(self):
        data = dashboard._sanitize_config_updates({"DASHBOARD_SSE_SYNC_SEC": "3"})
        self.assertEqual(data["DASHBOARD_SSE_SYNC_SEC"], "3")

    def test_sanitize_rejects_invalid_number(self):
        with self.assertRaises(ValueError):
            dashboard._sanitize_config_updates({"LLM_TIMEOUT": "not-number"})

    def test_save_config_returns_updated_keys(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            env_path = Path(tmpdir) / ".env"
            old_env_file = dashboard.ENV_FILE
            try:
                dashboard.ENV_FILE = env_path
                updated = dashboard.save_config({"LLM_TIMEOUT": "33"})
            finally:
                dashboard.ENV_FILE = old_env_file

            self.assertEqual(updated, ["LLM_TIMEOUT"])
            text = env_path.read_text(encoding="utf-8")
            self.assertIn("LLM_TIMEOUT='33'", text)

    def test_render_html_contains_architecture_section(self):
        html = dashboard.render_html()
        self.assertIn('id="agent-health-stat"', html)
        self.assertIn('id="agent-status-summary"', html)

    def test_build_system_prompt_contains_system_tools(self):
        result = dashboard._build_system_prompt()
        text = result['prompt_text']
        # Should NOT contain agent-prefixed tools
        self.assertNotIn('agent_01__', text)
        self.assertNotIn('Agent 专属工具', text)
        # System-level tools
        self.assertIn('iterm_list_sessions', text)
        self.assertIn('write_file', text)
        self.assertIn('create_interaction', text)
        self.assertIn('save_prompt_template', text)
        self.assertIn('save_command_card', text)
        self.assertIn('execute_command_card', text)
        self.assertIn('db_query', text)
        self.assertIn('db_execute', text)
        # Section header
        self.assertIn('系统级工具', text)

    def test_build_system_prompt_tool_count(self):
        result = dashboard._build_system_prompt()
        text = result['prompt_text']
        # Count lines starting with "- **" which represent tool entries
        tool_lines = [line for line in text.splitlines() if line.strip().startswith('- **')]
        self.assertEqual(len(tool_lines), 26, f"Expected 26 system tools, got {len(tool_lines)}")



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


class DashboardApiValidationTests(unittest.TestCase):
    def test_api_config_get_includes_ignored_keys_field(self):
        with run_dashboard_server() as base_url:
            code, payload = request_json(base_url, "/api/config")

        self.assertEqual(code, 200)
        self.assertIn("ignored_keys", payload)
        self.assertIsInstance(payload["ignored_keys"], list)

    def test_api_config_rejects_non_defaults_key(self):
        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "append_event"):
                code, payload = request_json(
                    base_url,
                    "/api/config",
                    method="POST",
                    payload={"UNSAFE_KEY": "1"},
                )

        self.assertEqual(code, 400)
        self.assertFalse(payload["ok"])
        self.assertIn("不允许", payload.get("error", ""))
        self.assertIn("不允许", payload.get("error_detail", ""))

    def test_ready_includes_db_latency_ms(self):
        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "_check_dashboard_ready", return_value=(True, "")):
                code, payload = request_json(base_url, "/ready")

        self.assertEqual(code, 200)
        self.assertTrue(payload.get("ok"))
        self.assertIn("db_latency_ms", payload)
        self.assertIsInstance(payload["db_latency_ms"], int)
        self.assertGreaterEqual(payload["db_latency_ms"], 0)

    def test_api_topology_approvals_rejects_invalid_length_id(self):
        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "approve_approval") as approve_mock:
                code, payload = request_json(
                    base_url,
                    "/api/topology/approvals/abcdef1234/approve",
                    method="POST",
                    payload={},
                )

        self.assertEqual(code, 400)
        self.assertFalse(payload["ok"])
        self.assertIn("invalid approval id", payload.get("error", ""))
        approve_mock.assert_not_called()

    def test_api_topology_approvals_accepts_16_hex_id(self):
        with run_dashboard_server() as base_url:
            with patch.object(dashboard, "approve_approval", return_value={"ok": True}) as approve_mock:
                with patch.object(dashboard, "_publish_dashboard_event"):
                    code, payload = request_json(
                        base_url,
                        "/api/topology/approvals/abcdef1234567890/approve",
                        method="POST",
                        payload={},
                    )

        self.assertEqual(code, 200)
        self.assertTrue(payload["ok"])
        approve_mock.assert_called_once_with(approval_id="abcdef1234567890", reviewer="dashboard")

    def test_dashboard_uses_topology_approval_id_validator(self):
        self.assertIs(dashboard.is_valid_approval_id, topology_approval.is_valid_approval_id)


if __name__ == "__main__":
    unittest.main()
