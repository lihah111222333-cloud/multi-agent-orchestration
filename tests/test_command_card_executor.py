import json
import unittest

import agent_ops_store
import command_card_executor
from tests.pg_test_helper import isolated_pg_schema


class CommandCardExecutorTests(unittest.TestCase):
    def test_high_risk_requires_review_then_execute(self):
        with isolated_pg_schema("cmdexec"):
            agent_ops_store.save_command_card(
                card_key="demo.high.v1",
                title="高风险示例",
                command_template="echo high-{name}",
                args_schema={"name": "str"},
                risk_level="high",
                enabled=True,
                updated_by="tester",
            )

            prepared = command_card_executor.prepare_command_card_run(
                card_key="demo.high.v1",
                params={"name": "ok"},
                requested_by="master",
            )
            self.assertTrue(prepared["ok"])
            self.assertTrue(prepared["needs_review"])
            run_id = prepared["run"]["id"]

            blocked = command_card_executor.execute_command_card_run(run_id=run_id, actor="master")
            self.assertFalse(blocked["ok"])
            self.assertIn("待审批", blocked["message"])

            reviewed = command_card_executor.review_command_card_run(
                run_id=run_id,
                decision="approved",
                reviewer="human",
                note="pass",
            )
            self.assertTrue(reviewed["ok"])
            self.assertEqual(reviewed["run"]["status"], "ready")

            executed = command_card_executor.execute_command_card_run(run_id=run_id, actor="master")
            self.assertTrue(executed["ok"])
            self.assertEqual(executed["run"]["status"], "success")
            self.assertIn("high-ok", executed["run"]["output"])

    def test_normal_risk_execute_command_card_directly(self):
        with isolated_pg_schema("cmdexec"):
            agent_ops_store.save_command_card(
                card_key="demo.normal.v1",
                title="普通示例",
                command_template="echo normal-{name}",
                args_schema={"name": "str"},
                risk_level="normal",
                enabled=True,
                updated_by="tester",
            )

            result = command_card_executor.execute_command_card(
                card_key="demo.normal.v1",
                params={"name": "go"},
                requested_by="master",
            )
            self.assertTrue(result["ok"])
            self.assertEqual(result["run"]["status"], "success")
            self.assertEqual(result["execution_mode"], "direct")
            self.assertEqual(result["run"]["execution_mode"], "direct")
            self.assertIn("normal-go", result["run"]["output"])

    def test_high_risk_auto_approve_is_blocked(self):
        with isolated_pg_schema("cmdexec"):
            agent_ops_store.save_command_card(
                card_key="demo.high.auto.v1",
                title="高风险自动审批禁用",
                command_template="echo high-auto-{name}",
                args_schema={"name": "str"},
                risk_level="critical",
                enabled=True,
                updated_by="tester",
            )

            result = command_card_executor.execute_command_card(
                card_key="demo.high.auto.v1",
                params={"name": "go"},
                requested_by="master",
                auto_approve=True,
                reviewer="robot",
                review_note="try-auto",
            )

            self.assertTrue(result["ok"])
            self.assertTrue(result["pending_review"])
            self.assertIn("禁止自动审批", result["message"])
            self.assertEqual(result["execution_mode"], "reviewed")
            self.assertEqual(result["run"]["status"], "pending_review")
            self.assertEqual(result["run"]["execution_mode"], "reviewed")

            run_id = result["run"]["id"]
            blocked = command_card_executor.execute_command_card_run(run_id=run_id, actor="master")
            self.assertFalse(blocked["ok"])
            self.assertEqual(blocked["execution_mode"], "reviewed")

    def test_template_params_are_shell_quoted(self):
        with isolated_pg_schema("cmdexec"):
            agent_ops_store.save_command_card(
                card_key="demo.quote.v1",
                title="转义示例",
                command_template="echo {name}",
                args_schema={"name": "str"},
                risk_level="normal",
                enabled=True,
                updated_by="tester",
            )

            result = command_card_executor.execute_command_card(
                card_key="demo.quote.v1",
                params={"name": "ok;echo hacked"},
                requested_by="master",
            )
            self.assertTrue(result["ok"])
            self.assertEqual(result["run"]["output"].strip(), "ok;echo hacked")

    def test_prepare_fails_on_missing_param(self):
        with isolated_pg_schema("cmdexec"):
            agent_ops_store.save_command_card(
                card_key="demo.param.v1",
                title="参数示例",
                command_template="echo {name}",
                args_schema={"name": "str"},
                risk_level="normal",
                enabled=True,
                updated_by="tester",
            )

            result = command_card_executor.prepare_command_card_run(
                card_key="demo.param.v1",
                params={},
                requested_by="master",
            )
            self.assertFalse(result["ok"])
            self.assertIn("缺少参数", result["message"])

    def test_shell_quoting_preserves_injection_like_payload_as_single_arg(self):
        with isolated_pg_schema("cmdexec"):
            agent_ops_store.save_command_card(
                card_key="demo.boundary.v1",
                title="注入边界示例",
                command_template='python3 -c "import json,sys;print(len(sys.argv));print(json.dumps(sys.argv[1], ensure_ascii=False))" {name}',
                args_schema={"name": "str"},
                risk_level="normal",
                enabled=True,
                updated_by="tester",
            )

            payloads = [
                "ok;echo hacked",
                "$(uname)",
                "a && b",
                "line1\nline2",
                "'quoted' \"double\"",
            ]

            for payload in payloads:
                result = command_card_executor.execute_command_card(
                    card_key="demo.boundary.v1",
                    params={"name": payload},
                    requested_by="master",
                )
                self.assertTrue(result["ok"])
                lines = result["run"]["output"].splitlines()
                self.assertGreaterEqual(len(lines), 2)
                self.assertEqual(lines[0], "2")
                self.assertEqual(lines[1], json.dumps(payload, ensure_ascii=False))


if __name__ == "__main__":
    unittest.main()
