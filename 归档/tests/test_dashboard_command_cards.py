import json
import threading
import unittest
from contextlib import contextmanager
from urllib import error, request

import agent_ops_store
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


class DashboardCommandCardTests(unittest.TestCase):
    def test_command_card_flow_via_dashboard_api(self):
        with isolated_pg_schema("dashcmd"):
            agent_ops_store.save_command_card(
                card_key="ops.test.high",
                title="高风险测试命令",
                command_template="echo HIGH-{name}",
                description="高风险测试",
                args_schema={"name": "str"},
                risk_level="high",
                enabled=True,
                updated_by="tester",
            )
            agent_ops_store.save_command_card(
                card_key="ops.test.normal",
                title="普通测试命令",
                command_template="echo NORMAL-{name}",
                description="普通测试",
                args_schema={"name": "str"},
                risk_level="normal",
                enabled=True,
                updated_by="tester",
            )

            with run_dashboard_server() as base_url:
                code, cards = request_json(base_url, "/api/command-cards?limit=20")
                self.assertEqual(code, 200)
                self.assertTrue(cards["ok"])
                keys = {row.get("card_key") for row in cards.get("cards", [])}
                self.assertIn("ops.test.high", keys)
                self.assertIn("ops.test.normal", keys)

                code, prepared = request_json(
                    base_url,
                    "/api/command-cards/execute",
                    method="POST",
                    payload={
                        "card_key": "ops.test.high",
                        "params": {"name": "alice"},
                        "requested_by": "dashboard",
                        "auto_approve": False,
                    },
                )
                self.assertEqual(code, 200)
                self.assertTrue(prepared["ok"])
                self.assertTrue(prepared["pending_review"])
                self.assertEqual(prepared["run"]["status"], "pending_review")
                run_id = int(prepared["run"]["id"])

                code, reviewed = request_json(
                    base_url,
                    "/api/command-card-runs/review",
                    method="POST",
                    payload={"run_id": run_id, "decision": "approved", "reviewer": "human"},
                )
                self.assertEqual(code, 200)
                self.assertTrue(reviewed["ok"])
                self.assertEqual(reviewed["run"]["status"], "ready")

                code, executed = request_json(
                    base_url,
                    "/api/command-card-runs/execute",
                    method="POST",
                    payload={"run_id": run_id, "actor": "dashboard"},
                )
                self.assertEqual(code, 200)
                self.assertTrue(executed["ok"])
                self.assertEqual(executed["run"]["status"], "success")
                self.assertIn("HIGH-alice", executed["run"]["output"])

                code, direct = request_json(
                    base_url,
                    "/api/command-cards/execute",
                    method="POST",
                    payload={
                        "card_key": "ops.test.normal",
                        "params": {"name": "bob"},
                        "requested_by": "dashboard",
                        "auto_approve": False,
                    },
                )
                self.assertEqual(code, 200)
                self.assertTrue(direct["ok"])
                self.assertEqual(direct["run"]["status"], "success")
                self.assertIn("NORMAL-bob", direct["run"]["output"])

                code, runs = request_json(base_url, "/api/command-card-runs?limit=10")
                self.assertEqual(code, 200)
                self.assertTrue(runs["ok"])
                run_ids = {int(row.get("id", 0)) for row in runs.get("runs", [])}
                self.assertIn(run_id, run_ids)

                code, cards_with_stats = request_json(base_url, "/api/command-cards?keyword=ops.test.normal&limit=20")
                self.assertEqual(code, 200)
                self.assertTrue(cards_with_stats["ok"])
                normal_card = next(
                    (row for row in cards_with_stats.get("cards", []) if row.get("card_key") == "ops.test.normal"),
                    None,
                )
                self.assertIsNotNone(normal_card)
                self.assertGreaterEqual(int(normal_card.get("run_count", 0)), 1)
                self.assertTrue(str(normal_card.get("last_run_at", "")).strip())

    def test_save_toggle_command_card_via_dashboard_api(self):
        with isolated_pg_schema("dashcmd"):
            with run_dashboard_server() as base_url:
                payload = {
                    "card_key": "launch.wjboot.workspace",
                    "title": "拉起工作区代理",
                    "description": "按等分窗格拉起代理",
                    "command_template": "python3 scripts/iterm_launch_agents.py --tabs {tabs} --layout panes",
                    "args_schema": {"tabs": "number"},
                    "risk_level": "normal",
                    "enabled": True,
                    "updated_by": "tester",
                }

                code, saved = request_json(
                    base_url,
                    "/api/command-cards",
                    method="POST",
                    payload=payload,
                )
                self.assertEqual(code, 200)
                self.assertTrue(saved["ok"])
                self.assertEqual(saved["command_card"]["card_key"], "launch.wjboot.workspace")

                code, cards = request_json(base_url, "/api/command-cards?keyword=launch.wjboot.workspace&limit=20")
                self.assertEqual(code, 200)
                self.assertTrue(cards["ok"])
                keys = {row.get("card_key") for row in cards.get("cards", [])}
                self.assertIn("launch.wjboot.workspace", keys)

                code, toggled = request_json(
                    base_url,
                    "/api/command-cards/toggle",
                    method="POST",
                    payload={"card_key": "launch.wjboot.workspace", "enabled": False, "updated_by": "tester"},
                )
                self.assertEqual(code, 200)
                self.assertTrue(toggled["ok"])
                self.assertFalse(toggled["command_card"]["enabled"])

                code, enabled_only = request_json(base_url, "/api/command-cards?enabled_only=1&limit=200")
                self.assertEqual(code, 200)
                self.assertTrue(enabled_only["ok"])
                enabled_keys = {row.get("card_key") for row in enabled_only.get("cards", [])}
                self.assertNotIn("launch.wjboot.workspace", enabled_keys)

                code, deleted = request_json(
                    base_url,
                    "/api/command-cards/delete",
                    method="POST",
                    payload={"card_keys": ["launch.wjboot.workspace"], "updated_by": "tester"},
                )
                self.assertEqual(code, 200)
                self.assertTrue(deleted["ok"])
                self.assertEqual(int(deleted.get("deleted", 0)), 1)

                code, after_delete = request_json(base_url, "/api/command-cards?keyword=launch.wjboot.workspace&limit=20")
                self.assertEqual(code, 200)
                self.assertTrue(after_delete["ok"])
                keys_after_delete = {row.get("card_key") for row in after_delete.get("cards", [])}
                self.assertNotIn("launch.wjboot.workspace", keys_after_delete)

    def test_reject_invalid_run_id(self):
        with isolated_pg_schema("dashcmd"):
            with run_dashboard_server() as base_url:
                code, reviewed = request_json(
                    base_url,
                    "/api/command-card-runs/review",
                    method="POST",
                    payload={"run_id": "abc", "decision": "approved", "reviewer": "human"},
                )
                self.assertEqual(code, 400)
                self.assertFalse(reviewed["ok"])
                self.assertIn("run_id", reviewed.get("error", ""))

                code, executed = request_json(
                    base_url,
                    "/api/command-card-runs/execute",
                    method="POST",
                    payload={"run_id": True, "actor": "dashboard"},
                )
                self.assertEqual(code, 400)
                self.assertFalse(executed["ok"])
                self.assertIn("run_id", executed.get("error", ""))


if __name__ == "__main__":
    unittest.main()
