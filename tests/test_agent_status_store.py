import unittest

from db import postgres
from tests.pg_test_helper import isolated_pg_schema

import agent_status_store


class AgentStatusStoreTests(unittest.TestCase):
    def test_migration_creates_agent_status_table(self):
        with isolated_pg_schema("agentstatus"):
            row = postgres.fetch_one(
                """
                SELECT COUNT(*) AS cnt
                FROM information_schema.tables
                WHERE table_schema = current_schema()
                  AND table_name = 'agent_status'
                """
            )
            self.assertEqual(int(row["cnt"]), 1)

    def test_upsert_agent_status_insert_and_update(self):
        with isolated_pg_schema("agentstatus"):
            first = agent_status_store.upsert_agent_status(
                agent_id="agent_02",
                agent_name="Agent 02",
                session_id="session-02",
                status="running",
                stagnant_sec=3,
                error="",
                output_tail=["heartbeat ok", "processed 1 item"],
            )
            self.assertEqual(first["agent_id"], "agent_02")
            self.assertEqual(first["status"], "running")
            self.assertEqual(first["output_tail"], ["heartbeat ok", "processed 1 item"])

            second = agent_status_store.upsert_agent_status(
                agent_id="agent_02",
                agent_name="Agent 02",
                session_id="session-02",
                status="error",
                stagnant_sec=60,
                error="Traceback: boom",
                output_tail=["Traceback: boom"],
            )
            self.assertEqual(second["agent_id"], "agent_02")
            self.assertEqual(second["status"], "error")
            self.assertEqual(second["stagnant_sec"], 60)
            self.assertEqual(second["error"], "Traceback: boom")
            self.assertEqual(second["output_tail"], ["Traceback: boom"])

            rows = agent_status_store.query_agent_status(agent_id="agent_02", limit=10)
            self.assertEqual(len(rows), 1)
            self.assertEqual(rows[0]["status"], "error")

    def test_query_agent_status_by_status(self):
        with isolated_pg_schema("agentstatus"):
            agent_status_store.upsert_agent_status(
                agent_id="agent_01",
                agent_name="Agent 01",
                session_id="session-01",
                status="running",
                stagnant_sec=1,
                error="",
                output_tail=["heartbeat ok"],
            )
            agent_status_store.upsert_agent_status(
                agent_id="agent_03",
                agent_name="Agent 03",
                session_id="",
                status="unknown",
                stagnant_sec=0,
                error="session not found",
                output_tail=[],
            )

            unknown_rows = agent_status_store.query_agent_status(status="unknown", limit=10)
            self.assertEqual(len(unknown_rows), 1)
            self.assertEqual(unknown_rows[0]["agent_id"], "agent_03")

    def test_upsert_rejects_invalid_status(self):
        with isolated_pg_schema("agentstatus"):
            with self.assertRaises(ValueError):
                agent_status_store.upsert_agent_status(
                    agent_id="agent_04",
                    agent_name="Agent 04",
                    session_id="session-04",
                    status="bad-status",
                    stagnant_sec=0,
                    error="",
                    output_tail=[],
                )


if __name__ == "__main__":
    unittest.main()
