"""bus_log 模块测试 — 消息总线异常日志写入与查询。"""

import unittest
from unittest import mock

import bus_log
from tests.pg_test_helper import isolated_pg_schema


class BusLogRecordAndQueryTests(unittest.TestCase):
    """record_bus_exception + query_bus_exceptions 功能验证。"""

    def test_record_and_query_all(self):
        with isolated_pg_schema("buslog"):
            bus_log.record_bus_exception(
                category="tool_timeout",
                severity="error",
                source="_make_hot_reloadable",
                message="tool iterm timeout after 90.1s",
                tool_name="iterm",
                extra={"elapsed_sec": 90.1, "limit_sec": 90},
            )
            bus_log.record_bus_exception(
                category="client_disconnect",
                severity="warning",
                source="_patch_session_auto_rebind",
                message="client disconnected (active session)",
            )
            bus_log.record_bus_exception(
                category="crash_restart",
                severity="critical",
                source="run_agent",
                message="crash #1/10: RuntimeError boom",
                traceback="Traceback ...\nRuntimeError: boom",
                extra={"attempt": 1, "max_restarts": 10},
            )

            # 全量查询
            rows = bus_log.query_bus_exceptions(limit=10)
            self.assertEqual(len(rows), 3)

            # 按 category 过滤
            timeouts = bus_log.query_bus_exceptions(limit=10, category="tool_timeout")
            self.assertEqual(len(timeouts), 1)
            self.assertEqual(timeouts[0]["tool_name"], "iterm")
            self.assertEqual(timeouts[0]["severity"], "error")

            # 按 severity 过滤
            crits = bus_log.query_bus_exceptions(limit=10, severity="critical")
            self.assertEqual(len(crits), 1)
            self.assertEqual(crits[0]["category"], "crash_restart")

            # 按 keyword 过滤
            kw_results = bus_log.query_bus_exceptions(limit=10, keyword="disconnect")
            self.assertEqual(len(kw_results), 1)
            self.assertEqual(kw_results[0]["category"], "client_disconnect")

    def test_record_with_invalid_category_normalizes(self):
        with isolated_pg_schema("buslog"):
            rec = bus_log.record_bus_exception(
                category="bogus_category",
                severity="bogus_severity",
                source="test",
                message="testing normalization",
            )
            self.assertEqual(rec["category"], "unknown")
            self.assertEqual(rec["severity"], "error")

            rows = bus_log.query_bus_exceptions(limit=5)
            self.assertEqual(len(rows), 1)
            self.assertEqual(rows[0]["category"], "unknown")

    def test_list_categories(self):
        with isolated_pg_schema("buslog"):
            bus_log.record_bus_exception(category="tool_error", severity="error", source="t1", message="e1")
            bus_log.record_bus_exception(category="session_stale", severity="warning", source="t2", message="e2")

            cats = bus_log.list_bus_categories()
            self.assertIn("tool_error", cats["categories"])
            self.assertIn("session_stale", cats["categories"])
            self.assertIn("error", cats["severities"])
            self.assertIn("warning", cats["severities"])


class BusLogSafetyTests(unittest.TestCase):
    """record_bus_exception 内部异常不传播。"""

    def test_record_does_not_raise_on_db_failure(self):
        with mock.patch("bus_log.execute", side_effect=RuntimeError("DB down")):
            # 不应抛出任何异常
            rec = bus_log.record_bus_exception(
                category="tool_error",
                severity="error",
                source="test",
                message="should not raise",
            )
            self.assertEqual(rec["category"], "tool_error")


class BusLogKeywordEscapingTests(unittest.TestCase):
    """关键词中的特殊字符不导致 SQL 错误。"""

    def test_keyword_with_special_chars(self):
        captured = {}

        def fake_fetch_all(sql, params=None):
            captured["sql"] = str(sql)
            captured["params"] = list(params or [])
            return []

        with mock.patch("bus_log.fetch_all", side_effect=fake_fetch_all):
            rows = bus_log.query_bus_exceptions(limit=10, keyword=r"Err_%\\Case")

        self.assertEqual(rows, [])
        self.assertIn("ESCAPE E'\\\\'", captured.get("sql", ""))


if __name__ == "__main__":
    unittest.main()
