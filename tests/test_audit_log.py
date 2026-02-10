import unittest
from unittest import mock

import audit_log
from tests.pg_test_helper import isolated_pg_schema


class AuditLogTests(unittest.TestCase):
    def test_append_and_filter(self):
        with isolated_pg_schema("audit"):
            audit_log.append_event(
                event_type="topology_approval",
                action="create",
                result="pending",
                actor="master",
                target="req-1",
                detail="new request",
            )
            audit_log.append_event(
                event_type="topology_approval",
                action="approve",
                result="approved",
                actor="dashboard",
                target="req-1",
                detail="approved",
            )
            audit_log.append_event(
                event_type="runtime",
                action="run_start",
                result="ok",
                actor="cli",
                target="master",
                detail="task=demo",
            )

            rows = audit_log.query_events(limit=10, event_type="topology_approval")
            self.assertEqual(len(rows), 2)

            approved = audit_log.query_events(limit=10, result="approved")
            self.assertEqual(len(approved), 1)
            self.assertEqual(approved[0]["action"], "approve")

            filtered = audit_log.query_events(limit=10, actor="dashboard", keyword="approved")
            self.assertEqual(len(filtered), 1)
            self.assertEqual(filtered[0]["target"], "req-1")

            filter_values = audit_log.list_filter_values()
            self.assertIn("topology_approval", filter_values["event_types"])
            self.assertIn("approve", filter_values["actions"])

    def test_query_limit_handles_invalid_value(self):
        with isolated_pg_schema("audit"):
            audit_log.append_event(event_type="runtime", action="run_start")
            rows = audit_log.query_events(limit="not-a-number")
            self.assertEqual(len(rows), 1)


class AuditLogQueryEscapingTests(unittest.TestCase):
    def test_keyword_like_is_escaped(self):
        captured: dict[str, object] = {}

        def fake_fetch_all(sql_text, params=None):
            captured["sql"] = str(sql_text)
            captured["params"] = list(params or [])
            return []

        with mock.patch("audit_log.fetch_all", side_effect=fake_fetch_all):
            rows = audit_log.query_events(limit=20, keyword=r"A_%\\B")

        self.assertEqual(rows, [])
        sql_text = str(captured.get("sql", ""))
        self.assertIn("ESCAPE E'\\\\'", sql_text)

        from utils import escape_like
        expected_kw = f"%{escape_like(r'A_%\\B'.lower())}%"
        params = list(captured.get("params", []))
        self.assertIn(expected_kw, params)


if __name__ == "__main__":
    unittest.main()
