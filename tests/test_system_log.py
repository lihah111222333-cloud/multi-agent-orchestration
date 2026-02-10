import unittest
from unittest import mock

import system_log
from tests.pg_test_helper import isolated_pg_schema


class SystemLogTests(unittest.TestCase):
    def test_query_and_filter(self):
        with isolated_pg_schema("syslog"):
            system_log.append_log(level="INFO", logger_name="run", message="start task", raw="raw-1")
            system_log.append_log(level="ERROR", logger_name="master", message="failed dispatch", raw="raw-2")
            system_log.append_log(level="WARNING", logger_name="dashboard", message="retry", raw="raw-3")

            rows = system_log.query_logs(limit=10)
            self.assertEqual(len(rows), 3)

            errors = system_log.query_logs(limit=10, level="ERROR")
            self.assertEqual(len(errors), 1)
            self.assertEqual(errors[0]["logger"], "master")

            by_logger = system_log.query_logs(limit=10, logger_name="run")
            self.assertEqual(len(by_logger), 1)
            self.assertEqual(by_logger[0]["level"], "INFO")

            with_keyword = system_log.query_logs(limit=10, keyword="dispatch")
            self.assertEqual(len(with_keyword), 1)
            self.assertEqual(with_keyword[0]["logger"], "master")

            invalid_limit = system_log.query_logs(limit="bad-value")
            self.assertEqual(len(invalid_limit), 3)

            filters = system_log.list_filter_values()
            self.assertIn("INFO", filters["levels"])
            self.assertIn("ERROR", filters["levels"])
            self.assertIn("run", filters["loggers"])
            self.assertIn("dashboard", filters["loggers"])


class SystemLogQueryEscapingTests(unittest.TestCase):
    def test_keyword_like_is_escaped(self):
        captured: dict[str, object] = {}

        def fake_fetch_all(sql_text, params=None):
            captured["sql"] = str(sql_text)
            captured["params"] = list(params or [])
            return []

        with mock.patch("system_log.fetch_all", side_effect=fake_fetch_all):
            rows = system_log.query_logs(limit=15, keyword=r"Err_%\\Case")

        self.assertEqual(rows, [])
        sql_text = str(captured.get("sql", ""))
        self.assertIn("ESCAPE E'\\\\'", sql_text)

        from utils import escape_like
        keyword = r"Err_%\\Case".lower()
        expected_kw = f"%{escape_like(keyword)}%"
        params = list(captured.get("params", []))
        self.assertIn(expected_kw, params)


if __name__ == "__main__":
    unittest.main()
