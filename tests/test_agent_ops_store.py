import unittest
from contextlib import ExitStack
from unittest.mock import patch

import agent_ops_store
from tests.pg_test_helper import isolated_pg_schema


class AgentOpsStoreTests(unittest.TestCase):
    def test_save_command_card_raises_explicit_error_when_db_returns_no_row(self):
        with patch("agent_ops_store.fetch_one", return_value=None):
            with self.assertRaises(agent_ops_store.RowMissingError):
                agent_ops_store.save_command_card(
                    card_key="deploy.bluegreen.v1",
                    title="蓝绿部署",
                    command_template="deploy --service {service}",
                )

    def test_interaction_create_list_review(self):
        with isolated_pg_schema("ops"):
            created = agent_ops_store.create_interaction(
                sender="master",
                receiver="agent_01",
                msg_type="task",
                content="collect data",
                thread_id="t-1",
                requires_review=True,
                metadata={"priority": "high"},
            )
            self.assertGreater(created["id"], 0)
            self.assertEqual(created["sender"], "master")
            self.assertTrue(created["requires_review"])

            rows = agent_ops_store.list_interactions(thread_id="t-1", limit=10)
            self.assertEqual(len(rows), 1)
            self.assertEqual(rows[0]["receiver"], "agent_01")

            reviewed = agent_ops_store.review_interaction(
                interaction_id=created["id"],
                status="approved",
                reviewer="human",
                note="ok",
            )
            self.assertTrue(reviewed["ok"])
            self.assertEqual(reviewed["interaction"]["status"], "approved")

    def test_prompt_template_crud(self):
        with isolated_pg_schema("ops"):
            saved = agent_ops_store.save_prompt_template(
                prompt_key="agent_01.fetch_market_data.v1",
                title="市场采集模板",
                prompt_text="先校验 symbol 再执行采集",
                agent_key="agent_01",
                tool_name="fetch_market_data",
                variables={"symbol": "BTC"},
                tags=["market", "data"],
                enabled=True,
                updated_by="tester",
            )
            self.assertEqual(saved["agent_key"], "agent_01")

            got = agent_ops_store.get_prompt_template("agent_01.fetch_market_data.v1")
            self.assertIsNotNone(got)
            assert got is not None
            self.assertEqual(got["tool_name"], "fetch_market_data")

            rows = agent_ops_store.list_prompt_templates(agent_key="agent_01", limit=10)
            self.assertEqual(len(rows), 1)

            toggled = agent_ops_store.set_prompt_template_enabled(
                prompt_key="agent_01.fetch_market_data.v1",
                enabled=False,
                updated_by="tester",
            )
            self.assertTrue(toggled["ok"])
            self.assertFalse(toggled["prompt"]["enabled"])

    def test_prompt_keyword_uses_literal_like_match(self):
        with isolated_pg_schema("ops"):
            agent_ops_store.save_prompt_template(
                prompt_key="agent_01.literal.v1",
                title="rate 100%",
                prompt_text="literal percent",
                agent_key="agent_01",
                tool_name="fetch_market_data",
                updated_by="tester",
            )
            agent_ops_store.save_prompt_template(
                prompt_key="agent_01.wildcard.v1",
                title="rate 100X",
                prompt_text="wildcard sample",
                agent_key="agent_01",
                tool_name="fetch_market_data",
                updated_by="tester",
            )

            rows = agent_ops_store.list_prompt_templates(keyword="100%", limit=10)
            keys = {row["prompt_key"] for row in rows}
            self.assertIn("agent_01.literal.v1", keys)
            self.assertNotIn("agent_01.wildcard.v1", keys)

    def test_db_query_and_db_execute_sql_guards(self):
        with isolated_pg_schema("ops"):
            rows = agent_ops_store.db_query("SELECT 1 AS v", limit=5)
            self.assertEqual(rows[0]["v"], 1)

            with patch("agent_ops_store.connect_cursor") as connect_cursor_mock:
                cur = connect_cursor_mock.return_value.__enter__.return_value
                cur.fetchall.return_value = [{"v": 1}]
                rows = agent_ops_store.db_query("SELECT 1 AS v", limit=3)
                self.assertEqual(rows, [{"v": 1}])
                connect_cursor_mock.assert_called_once_with(row_as_dict=True, autocommit=False)
                cur.execute.assert_any_call("SET LOCAL transaction_read_only = on")

            with self.assertRaises(ValueError):
                agent_ops_store.db_query("SELECT 1; DROP TABLE prompt_templates")

            with self.assertRaises(ValueError):
                agent_ops_store.db_query("WITH t AS (DELETE FROM prompt_templates RETURNING id) SELECT * FROM t")

            with patch.dict("os.environ", {"AGENT_DB_EXECUTE_ENABLED": "1"}, clear=False):
                with self.assertRaises(ValueError):
                    agent_ops_store.db_execute("SELECT 1")

                with self.assertRaises(ValueError):
                    agent_ops_store.db_execute("CREATE TABLE IF NOT EXISTS tmp_sql_guard(id INT)")

    def test_db_execute_flag_off_returns_explicit_error(self):
        with isolated_pg_schema("ops"):
            with ExitStack() as stack:
                stack.enter_context(patch.dict("os.environ", {"AGENT_DB_EXECUTE_ENABLED": "0"}, clear=False))
                execute_mock = stack.enter_context(patch("agent_ops_store.execute"))
                result = agent_ops_store.db_execute("UPDATE prompt_templates SET enabled = TRUE WHERE 1 = 0")

            self.assertFalse(result["ok"])
            self.assertIn("AGENT_DB_EXECUTE_ENABLED=1", result.get("error", ""))
            execute_mock.assert_not_called()

    def test_db_execute_flag_on_allows_dml(self):
        with isolated_pg_schema("ops"):
            with ExitStack() as stack:
                stack.enter_context(patch.dict("os.environ", {"AGENT_DB_EXECUTE_ENABLED": "1"}, clear=False))
                execute_mock = stack.enter_context(patch("agent_ops_store.execute", return_value=0))
                append_mock = stack.enter_context(patch("agent_ops_store.append_event"))
                result = agent_ops_store.db_execute("UPDATE prompt_templates SET enabled = TRUE WHERE 1 = 0")

            self.assertTrue(result["ok"])
            self.assertEqual(result["rowcount"], 0)
            execute_mock.assert_called_once_with("UPDATE prompt_templates SET enabled = TRUE WHERE 1 = 0")
            append_mock.assert_called_once()

    def test_command_card_crud(self):
        with isolated_pg_schema("ops"):
            saved = agent_ops_store.save_command_card(
                card_key="deploy.bluegreen.v1",
                title="蓝绿部署",
                command_template="deploy --service {service} --version {version} --mode bluegreen",
                description="生产蓝绿部署命令",
                args_schema={"service": "str", "version": "str"},
                risk_level="high",
                enabled=True,
                updated_by="tester",
            )
            self.assertEqual(saved["risk_level"], "high")

            got = agent_ops_store.get_command_card("deploy.bluegreen.v1")
            self.assertIsNotNone(got)
            assert got is not None
            self.assertIn("bluegreen", got["command_template"])

            rows = agent_ops_store.list_command_cards(risk_level="high", enabled_only=True, limit=10)
            self.assertEqual(len(rows), 1)

            toggled = agent_ops_store.set_command_card_enabled(
                card_key="deploy.bluegreen.v1",
                enabled=False,
                updated_by="tester",
            )
            self.assertTrue(toggled["ok"])
            self.assertFalse(toggled["command_card"]["enabled"])


if __name__ == "__main__":
    unittest.main()
