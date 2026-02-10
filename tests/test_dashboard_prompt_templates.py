import json
import threading
import unittest
from contextlib import contextmanager
from urllib import error, request

import dashboard
from tests.pg_test_helper import isolated_pg_schema


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


class DashboardPromptTemplateTests(unittest.TestCase):
    def test_seed_list_toggle_prompt_templates(self):
        with isolated_pg_schema("dashprompt"):
            with run_dashboard_server() as base_url:
                code, seeded = request_json(
                    base_url,
                    "/api/prompt-templates/seed",
                    method="POST",
                    payload={"overwrite": False, "updated_by": "tester"},
                )
                self.assertEqual(code, 200)
                self.assertTrue(seeded["ok"])
                self.assertGreaterEqual(int(seeded.get("inserted", 0)), 1)

                code, templates = request_json(base_url, "/api/prompt-templates?limit=200")
                self.assertEqual(code, 200)
                self.assertTrue(templates["ok"])
                keys = {row.get("prompt_key") for row in templates.get("templates", [])}
                self.assertIn("orch.review.plan_dag", keys)

                code, toggled = request_json(
                    base_url,
                    "/api/prompt-templates/toggle",
                    method="POST",
                    payload={"prompt_key": "orch.review.plan_dag", "enabled": False, "updated_by": "tester"},
                )
                self.assertEqual(code, 200)
                self.assertTrue(toggled["ok"])
                self.assertFalse(toggled["prompt"]["enabled"])

                code, enabled_only = request_json(base_url, "/api/prompt-templates?enabled_only=1&limit=200")
                self.assertEqual(code, 200)
                enabled_keys = {row.get("prompt_key") for row in enabled_only.get("templates", [])}
                self.assertNotIn("orch.review.plan_dag", enabled_keys)

    def test_save_and_rollback_prompt_template(self):
        with isolated_pg_schema("dashprompt"):
            with run_dashboard_server() as base_url:
                payload_v1 = {
                    "prompt_key": "custom.ops.daily",
                    "title": "日常巡检模板",
                    "agent_key": "master",
                    "tool_name": "task",
                    "prompt_text": "v1: 输出当天进度与阻塞项",
                    "variables": {"DATE": "日期"},
                    "tags": ["custom", "daily"],
                    "enabled": True,
                    "updated_by": "tester",
                }
                code, saved_v1 = request_json(base_url, "/api/prompt-templates", method="POST", payload=payload_v1)
                self.assertEqual(code, 200)
                self.assertTrue(saved_v1["ok"])
                self.assertEqual(saved_v1["prompt"]["prompt_key"], "custom.ops.daily")

                payload_v2 = dict(payload_v1)
                payload_v2["prompt_text"] = "v2: 输出当天进度、阻塞项和风险建议"
                code, saved_v2 = request_json(base_url, "/api/prompt-templates", method="POST", payload=payload_v2)
                self.assertEqual(code, 200)
                self.assertTrue(saved_v2["ok"])
                self.assertIn("v2", saved_v2["prompt"]["prompt_text"])

                code, versions = request_json(base_url, "/api/prompt-versions?prompt_key=custom.ops.daily&limit=10")
                self.assertEqual(code, 200)
                self.assertTrue(versions["ok"])
                self.assertGreaterEqual(len(versions.get("versions", [])), 1)
                version_id = int(versions["versions"][0]["id"])

                code, rollback = request_json(
                    base_url,
                    "/api/prompt-templates/rollback",
                    method="POST",
                    payload={
                        "prompt_key": "custom.ops.daily",
                        "version_id": version_id,
                        "updated_by": "tester",
                    },
                )
                self.assertEqual(code, 200)
                self.assertTrue(rollback["ok"])
                self.assertIn("v1", rollback["prompt"]["prompt_text"])


if __name__ == "__main__":
    unittest.main()
