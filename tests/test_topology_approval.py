import json
import os
import tempfile
import threading
import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock

from config import settings
from db import postgres
from tests.pg_test_helper import isolated_pg_schema
import topology_approval as approval


class TopologyApprovalTests(unittest.TestCase):
    def test_approval_id_rule_is_hex_16(self):
        self.assertTrue(approval.is_valid_approval_id("abcdef1234567890"))
        self.assertFalse(approval.is_valid_approval_id("abcdef1234"))
        self.assertFalse(approval.is_valid_approval_id("ABCDEF1234567890"))

    def test_create_and_approve_flow(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                current = {
                    "gateways": [
                        {
                            "id": "gateway_1",
                            "name": "网关1",
                            "agents": [{"id": "agent_1", "name": "代理1"}],
                        }
                    ]
                }
                config_path.write_text(json.dumps(current, ensure_ascii=False), encoding="utf-8")
                env_path.write_text("TOPOLOGY_APPROVAL_TTL_SEC=120\n", encoding="utf-8")

                backup_dir = tmp / "config_backups"

                old_config = settings.CONFIG_FILE
                old_backup_dir = settings.CONFIG_BACKUP_DIR
                old_backup_enabled = settings.CONFIG_BACKUP_ENABLED
                old_env_file = approval.ENV_FILE
                try:
                    settings.CONFIG_FILE = config_path
                    settings.CONFIG_BACKUP_DIR = backup_dir
                    settings.CONFIG_BACKUP_ENABLED = True
                    approval.ENV_FILE = env_path

                    proposed = {
                        "gateways": [
                            {
                                "id": "gateway_1",
                                "name": "网关1",
                                "agents": [
                                    {"id": "agent_1", "name": "代理1"},
                                    {"id": "agent_2", "name": "代理2"},
                                ],
                            }
                        ]
                    }

                    created = approval.create_approval(proposed, requested_by="master", reason="test")
                    self.assertTrue(created["ok"])
                    req_id = created["request"]["id"]
                    self.assertRegex(req_id, r"^[a-f0-9]{16}$")

                    pending = approval.list_approvals(status="pending", limit=10)
                    self.assertEqual(len(pending), 1)
                    self.assertEqual(pending[0]["id"], req_id)

                    approved = approval.approve_approval(req_id, reviewer="tester")
                    self.assertTrue(approved["ok"])
                    self.assertTrue(bool(approved.get("config_backup")))
                    self.assertTrue(Path(approved["config_backup"]).exists())

                    applied = settings.load_architecture_raw()
                    self.assertEqual(len(applied["gateways"][0]["agents"]), 2)
                finally:
                    settings.CONFIG_FILE = old_config
                    settings.CONFIG_BACKUP_DIR = old_backup_dir
                    settings.CONFIG_BACKUP_ENABLED = old_backup_enabled
                    approval.ENV_FILE = old_env_file

    def test_concurrent_approve_is_atomic_single_winner(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                current = {
                    "gateways": [
                        {
                            "id": "gateway_1",
                            "name": "网关1",
                            "agents": [{"id": "agent_1", "name": "代理1"}],
                        }
                    ]
                }
                config_path.write_text(json.dumps(current, ensure_ascii=False), encoding="utf-8")
                env_path.write_text("TOPOLOGY_APPROVAL_TTL_SEC=120\n", encoding="utf-8")

                backup_dir = tmp / "config_backups"

                old_config = settings.CONFIG_FILE
                old_backup_dir = settings.CONFIG_BACKUP_DIR
                old_backup_enabled = settings.CONFIG_BACKUP_ENABLED
                old_env_file = approval.ENV_FILE
                try:
                    settings.CONFIG_FILE = config_path
                    settings.CONFIG_BACKUP_DIR = backup_dir
                    settings.CONFIG_BACKUP_ENABLED = True
                    approval.ENV_FILE = env_path

                    proposed = {
                        "gateways": [
                            {
                                "id": "gateway_1",
                                "name": "网关1",
                                "agents": [
                                    {"id": "agent_1", "name": "代理1"},
                                    {"id": "agent_2", "name": "代理2"},
                                ],
                            }
                        ]
                    }

                    created = approval.create_approval(proposed, requested_by="master", reason="concurrency")
                    self.assertTrue(created["ok"])
                    req_id = created["request"]["id"]

                    start_barrier = threading.Barrier(3)
                    call_counter = {"count": 0}
                    counter_lock = threading.Lock()
                    original_save = approval.save_architecture

                    def tracked_save(data):
                        time.sleep(0.1)
                        with counter_lock:
                            call_counter["count"] += 1
                        return original_save(data)

                    def approve_worker(name: str):
                        start_barrier.wait(timeout=5)
                        return approval.approve_approval(req_id, reviewer=name)

                    with mock.patch("topology_approval.save_architecture", side_effect=tracked_save):
                        with ThreadPoolExecutor(max_workers=2) as executor:
                            f1 = executor.submit(approve_worker, "reviewer_1")
                            f2 = executor.submit(approve_worker, "reviewer_2")
                            start_barrier.wait(timeout=5)
                            results = [f1.result(timeout=10), f2.result(timeout=10)]

                    success = [item for item in results if item.get("ok")]
                    failed = [item for item in results if not item.get("ok")]

                    self.assertEqual(len(success), 1)
                    self.assertEqual(len(failed), 1)
                    self.assertEqual(call_counter["count"], 1)
                    self.assertIn("审批单状态不可批准", failed[0].get("message", ""))

                    row = postgres.fetch_one("SELECT status FROM topology_approvals WHERE id = %s", (req_id,))
                    self.assertIsNotNone(row)
                    self.assertEqual(row["status"], "approved")
                finally:
                    settings.CONFIG_FILE = old_config
                    settings.CONFIG_BACKUP_DIR = old_backup_dir
                    settings.CONFIG_BACKUP_ENABLED = old_backup_enabled
                    approval.ENV_FILE = old_env_file

    def test_concurrent_expired_request_state_race_is_consistent(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                config_path.write_text(json.dumps({"gateways": []}, ensure_ascii=False), encoding="utf-8")
                env_path.write_text("TOPOLOGY_APPROVAL_TTL_SEC=120\n", encoding="utf-8")

                now = datetime.now(timezone.utc)
                req_id = "req_expired_race"

                postgres.execute(
                    """
                    INSERT INTO topology_approvals (
                        id, status, requested_by, reason, created_at, expire_at,
                        reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
                    )
                    VALUES (%s, %s, %s, %s, %s, %s, NULL, '', '', %s, %s::jsonb)
                    """,
                    (
                        req_id,
                        "pending",
                        "master",
                        "expired-race",
                        (now - timedelta(seconds=120)).isoformat(),
                        (now - timedelta(seconds=60)).isoformat(),
                        "hash-expired-race",
                        json.dumps({"gateways": [{"id": "g1", "agents": [{"id": "a1"}]}]}, ensure_ascii=False),
                    ),
                )

                old_config = settings.CONFIG_FILE
                old_env_file = approval.ENV_FILE
                try:
                    settings.CONFIG_FILE = config_path
                    approval.ENV_FILE = env_path

                    start_barrier = threading.Barrier(3)

                    def approve_worker():
                        start_barrier.wait(timeout=5)
                        return approval.approve_approval(req_id, reviewer="approve_racer")

                    def reject_worker():
                        start_barrier.wait(timeout=5)
                        return approval.reject_approval(req_id, reviewer="reject_racer")

                    with ThreadPoolExecutor(max_workers=2) as executor:
                        f1 = executor.submit(approve_worker)
                        f2 = executor.submit(reject_worker)
                        start_barrier.wait(timeout=5)
                        results = [f1.result(timeout=10), f2.result(timeout=10)]

                    self.assertTrue(all(not item.get("ok") for item in results))

                    row = postgres.fetch_one(
                        "SELECT status, reviewer, review_note FROM topology_approvals WHERE id = %s",
                        (req_id,),
                    )
                    self.assertIsNotNone(row)
                    self.assertEqual(row["status"], "expired")
                    self.assertEqual(row["reviewer"], "system")
                    self.assertEqual(row["review_note"], "审批超时自动过期")

                    expire_events = postgres.fetch_all(
                        """
                        SELECT id
                        FROM audit_events
                        WHERE event_type = 'topology_approval'
                          AND action = 'expire'
                          AND target = %s
                        """,
                        (req_id,),
                    )
                    self.assertEqual(len(expire_events), 1)
                finally:
                    settings.CONFIG_FILE = old_config
                    approval.ENV_FILE = old_env_file

    def test_expire_is_persisted_even_when_approve_fails(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                config_path.write_text(json.dumps({"gateways": []}, ensure_ascii=False), encoding="utf-8")
                env_path.write_text("TOPOLOGY_APPROVAL_TTL_SEC=120\n", encoding="utf-8")

                expired_at = (datetime.now(timezone.utc) - timedelta(seconds=60)).isoformat()
                postgres.execute(
                    """
                    INSERT INTO topology_approvals (
                        id, status, requested_by, reason, created_at, expire_at,
                        reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
                    )
                    VALUES (%s, %s, %s, %s, %s, %s, NULL, '', '', %s, %s::jsonb)
                    """,
                    (
                        "req_expired_1",
                        "pending",
                        "master",
                        "seed",
                        "2026-01-01T00:00:00+00:00",
                        expired_at,
                        "hash1",
                        json.dumps({"gateways": []}, ensure_ascii=False),
                    ),
                )

                old_config = settings.CONFIG_FILE
                old_env_file = approval.ENV_FILE
                try:
                    settings.CONFIG_FILE = config_path
                    approval.ENV_FILE = env_path

                    result = approval.approve_approval("not_exists", reviewer="tester")
                    self.assertFalse(result["ok"])

                    req = postgres.fetch_one(
                        "SELECT status, reviewer FROM topology_approvals WHERE id = %s",
                        ("req_expired_1",),
                    )
                    self.assertIsNotNone(req)
                    self.assertEqual(req["status"], "expired")
                    self.assertEqual(req["reviewer"], "system")
                finally:
                    settings.CONFIG_FILE = old_config
                    approval.ENV_FILE = old_env_file

    def test_default_ttl_reads_from_env(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                config_path.write_text(json.dumps({"gateways": []}, ensure_ascii=False), encoding="utf-8")
                env_path.write_text("TOPOLOGY_APPROVAL_TTL_SEC=180\n", encoding="utf-8")

                old_config = settings.CONFIG_FILE
                old_env_file = approval.ENV_FILE
                old_ttl = os.getenv("TOPOLOGY_APPROVAL_TTL_SEC")
                try:
                    os.environ["TOPOLOGY_APPROVAL_TTL_SEC"] = "180"
                    settings.CONFIG_FILE = config_path
                    approval.ENV_FILE = env_path

                    proposed = {
                        "gateways": [
                            {
                                "id": "gateway_new",
                                "name": "网关新",
                                "agents": [{"id": "agent_1", "name": "代理1"}],
                            }
                        ]
                    }

                    created = approval.create_approval(proposed, requested_by="master", reason="ttl test", ttl_sec=None)
                    self.assertTrue(created["ok"])

                    req = created["request"]
                    created_at = datetime.fromisoformat(req["created_at"])
                    expire_at = datetime.fromisoformat(req["expire_at"])
                    ttl_sec = int((expire_at - created_at).total_seconds())

                    self.assertGreaterEqual(ttl_sec, 179)
                    self.assertLessEqual(ttl_sec, 181)
                finally:
                    if old_ttl is None:
                        os.environ.pop("TOPOLOGY_APPROVAL_TTL_SEC", None)
                    else:
                        os.environ["TOPOLOGY_APPROVAL_TTL_SEC"] = old_ttl
                    settings.CONFIG_FILE = old_config
                    approval.ENV_FILE = old_env_file

    def test_create_approval_rejects_invalid_architecture(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                config_path.write_text(
                    json.dumps({"gateways": [{"id": "gateway_1", "name": "网关1", "agents": [{"id": "agent_1"}]}]}, ensure_ascii=False),
                    encoding="utf-8",
                )
                env_path.write_text("TOPOLOGY_APPROVAL_TTL_SEC=120\n", encoding="utf-8")

                old_config = settings.CONFIG_FILE
                old_env_file = approval.ENV_FILE
                try:
                    settings.CONFIG_FILE = config_path
                    approval.ENV_FILE = env_path

                    result = approval.create_approval(
                        {"gateways": []},
                        requested_by="master",
                        reason="invalid topology",
                    )
                    self.assertFalse(result["ok"])
                    self.assertEqual(result["reason"], "invalid_architecture")
                finally:
                    settings.CONFIG_FILE = old_config
                    approval.ENV_FILE = old_env_file

    def test_archive_completed_requests(self):
        with isolated_pg_schema("approval"):
            with tempfile.TemporaryDirectory() as tmpdir:
                tmp = Path(tmpdir)
                config_path = tmp / "config.json"
                env_path = tmp / ".env"

                config_path.write_text(json.dumps({"gateways": []}, ensure_ascii=False), encoding="utf-8")
                env_path.write_text("TOPOLOGY_APPROVAL_ARCHIVE_DAYS=1\n", encoding="utf-8")

                old_config = settings.CONFIG_FILE
                old_env_file = approval.ENV_FILE
                try:
                    settings.CONFIG_FILE = config_path
                    approval.ENV_FILE = env_path

                    postgres.execute(
                        """
                        INSERT INTO topology_approvals (
                            id, status, requested_by, reason, created_at, expire_at,
                            reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
                        )
                        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s::jsonb)
                        """,
                        (
                            "req_old",
                            "approved",
                            "master",
                            "old",
                            "2026-01-01T00:00:00+00:00",
                            "2026-01-02T00:00:00+00:00",
                            "2026-01-01T01:00:00+00:00",
                            "tester",
                            "ok",
                            "hash-old",
                            json.dumps({"gateways": []}, ensure_ascii=False),
                        ),
                    )

                    postgres.execute(
                        """
                        INSERT INTO topology_approvals (
                            id, status, requested_by, reason, created_at, expire_at,
                            reviewed_at, reviewer, review_note, arch_hash, proposed_architecture
                        )
                        VALUES (%s, %s, %s, %s, %s, %s, NULL, '', '', %s, %s::jsonb)
                        """,
                        (
                            "req_new",
                            "pending",
                            "master",
                            "new",
                            datetime.now(timezone.utc).isoformat(),
                            (datetime.now(timezone.utc) + timedelta(seconds=60)).isoformat(),
                            "hash-new",
                            json.dumps({"gateways": []}, ensure_ascii=False),
                        ),
                    )

                    rows = approval.list_approvals(limit=10)
                    ids = {row["id"] for row in rows}

                    self.assertIn("req_new", ids)
                    self.assertNotIn("req_old", ids)

                    archived = postgres.fetch_one(
                        "SELECT id FROM topology_approval_archives WHERE id = %s",
                        ("req_old",),
                    )
                    self.assertIsNotNone(archived)
                finally:
                    settings.CONFIG_FILE = old_config
                    approval.ENV_FILE = old_env_file


if __name__ == "__main__":
    unittest.main()
