import unittest
from unittest import mock

import ai_log
import system_log
from tests.pg_test_helper import isolated_pg_schema


class AiLogTests(unittest.TestCase):
    def test_query_ai_logs_and_filters(self):
        with isolated_pg_schema("ailog"):
            system_log.append_log(
                level="INFO",
                logger_name="httpx",
                message='HTTP Request: POST https://api.gpteamservices.com/v1/responses "HTTP/1.1 200 OK"',
                raw="raw-httpx",
            )
            system_log.append_log(
                level="WARNING",
                logger_name="gateways.gateway",
                message="[主控 Gateway] 已临时关闭 use_previous_response_id 以兼容当前网关",
                raw="raw-fallback",
            )
            system_log.append_log(
                level="ERROR",
                logger_name="gateways.gateway",
                message=(
                    "[主控 Gateway] 执行异常: Error code: 404 - "
                    "{'error': {'message': \"Item with id 'rs_xxx' not found. "
                    "Items are not persisted when `store` is set to false.\"}}"
                ),
                raw="raw-error",
            )
            system_log.append_log(
                level="INFO",
                logger_name="worker.other",
                message="unrelated message",
                raw="raw-other",
            )

            rows = ai_log.query_ai_logs(limit=10)
            self.assertEqual(len(rows), 3)

            categories = {row["category"] for row in rows}
            self.assertIn("api_request", categories)
            self.assertIn("compat_fallback", categories)
            self.assertIn("api_error", categories)

            by_endpoint = ai_log.query_ai_logs(limit=10, endpoint="/responses")
            self.assertEqual(len(by_endpoint), 1)
            self.assertEqual(by_endpoint[0]["status_code"], "200")

            by_status = ai_log.query_ai_logs(limit=10, status_code="404")
            self.assertEqual(len(by_status), 1)
            self.assertEqual(by_status[0]["category"], "api_error")

            by_category = ai_log.query_ai_logs(limit=10, category="compat_fallback")
            self.assertEqual(len(by_category), 1)
            self.assertIn("use_previous_response_id", by_category[0]["message"])

            filters = ai_log.list_ai_filter_values(limit=20)
            self.assertIn("INFO", filters["levels"])
            self.assertIn("httpx", filters["loggers"])
            self.assertIn("api_request", filters["categories"])
            self.assertIn("/v1/responses", filters["endpoints"])
            self.assertIn("404", filters["status_codes"])


class AiLogQueryEscapingTests(unittest.TestCase):
    def test_keyword_like_is_escaped(self):
        captured: dict[str, object] = {}

        def fake_fetch_all(sql_text, params=None):
            captured["sql"] = str(sql_text)
            captured["params"] = list(params or [])
            return []

        with mock.patch("ai_log.fetch_all", side_effect=fake_fetch_all):
            rows = ai_log.query_ai_logs(limit=15, keyword=r"Err_%\\Case")

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
