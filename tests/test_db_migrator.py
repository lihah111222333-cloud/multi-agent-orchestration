import os
import tempfile
import unittest
import uuid
from pathlib import Path
from unittest.mock import patch

from db import postgres
from db.migrator import discover_migrations, parse_migration_filename
from tests.pg_test_helper import _resolve_test_conn, isolated_pg_schema


class DbMigratorTests(unittest.TestCase):
    def test_parse_migration_filename(self):
        version, name = parse_migration_filename("0001_init_schema.sql")
        self.assertEqual(version, 1)
        self.assertEqual(name, "init_schema")

        invalid_names = [
            "init_schema.sql",
            "0001-init_schema.sql",
            "0001_.sql",
            "0001_init_schema.sql.bak",
        ]
        for invalid_name in invalid_names:
            with self.subTest(invalid_name=invalid_name):
                with self.assertRaises(ValueError):
                    parse_migration_filename(invalid_name)

    def test_discover_migrations_sorts_by_version(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            folder = Path(tmpdir)
            (folder / "0003_add_index.sql").write_text("-- 0003", encoding="utf-8")
            (folder / "0001_init.sql").write_text("-- 0001", encoding="utf-8")
            (folder / "0002_seed_data.sql").write_text("-- 0002", encoding="utf-8")

            migrations = discover_migrations(folder)

        self.assertEqual([item.version for item in migrations], [1, 2, 3])
        self.assertEqual(
            [item.filename for item in migrations],
            ["0001_init.sql", "0002_seed_data.sql", "0003_add_index.sql"],
        )

    def test_discover_migrations_rejects_duplicate_versions(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            folder = Path(tmpdir)
            (folder / "0001_init.sql").write_text("-- init", encoding="utf-8")
            (folder / "0001_add_table.sql").write_text("-- duplicate", encoding="utf-8")

            with self.assertRaises(ValueError) as ctx:
                discover_migrations(folder)

        self.assertIn("duplicate migration version", str(ctx.exception))

    def test_discover_migrations_rejects_non_contiguous_versions(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            folder = Path(tmpdir)
            (folder / "0001_init.sql").write_text("-- init", encoding="utf-8")
            (folder / "0003_add_table.sql").write_text("-- gap", encoding="utf-8")

            with self.assertRaises(ValueError) as ctx:
                discover_migrations(folder)

        self.assertIn("expected version 2", str(ctx.exception))

    def test_ensure_schema_applies_sql_migrations_when_present(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            folder = Path(tmpdir)
            (folder / "0001_create_probe.sql").write_text(
                """
                CREATE TABLE IF NOT EXISTS migration_probe (
                    id INT PRIMARY KEY,
                    name TEXT NOT NULL
                );
                """.strip(),
                encoding="utf-8",
            )

            with patch.object(postgres, "MIGRATIONS_DIR", folder):
                with isolated_pg_schema("migrun"):
                    probe_row = postgres.fetch_one(
                        """
                        SELECT COUNT(*) AS cnt
                        FROM information_schema.tables
                        WHERE table_schema = current_schema()
                          AND table_name = 'migration_probe'
                        """
                    )
                    self.assertEqual(int(probe_row["cnt"]), 1)

                    migration_row = postgres.fetch_one("SELECT COUNT(*) AS cnt FROM schema_migrations")
                    self.assertEqual(int(migration_row["cnt"]), 1)

    def test_ensure_schema_raises_when_migration_dir_missing(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            missing = Path(tmpdir) / "missing"
            old_conn = os.getenv("POSTGRES_CONNECTION_STRING")
            old_schema = os.getenv("POSTGRES_SCHEMA")
            schema = f"migdirmiss_{uuid.uuid4().hex[:10]}"

            try:
                os.environ["POSTGRES_CONNECTION_STRING"] = _resolve_test_conn()
                os.environ["POSTGRES_SCHEMA"] = schema
                postgres.reset_schema_cache()

                with patch.object(postgres, "MIGRATIONS_DIR", missing):
                    with self.assertRaisesRegex(RuntimeError, "未找到 SQL migrations 目录"):
                        postgres.ensure_schema()
            finally:
                try:
                    postgres.drop_schema(schema)
                except Exception:
                    pass

                if old_conn is None:
                    os.environ.pop("POSTGRES_CONNECTION_STRING", None)
                else:
                    os.environ["POSTGRES_CONNECTION_STRING"] = old_conn

                if old_schema is None:
                    os.environ.pop("POSTGRES_SCHEMA", None)
                else:
                    os.environ["POSTGRES_SCHEMA"] = old_schema

                postgres.reset_schema_cache()

    def test_ensure_schema_raises_when_migration_dir_empty(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            folder = Path(tmpdir)
            old_conn = os.getenv("POSTGRES_CONNECTION_STRING")
            old_schema = os.getenv("POSTGRES_SCHEMA")
            schema = f"migdirempty_{uuid.uuid4().hex[:10]}"

            try:
                os.environ["POSTGRES_CONNECTION_STRING"] = _resolve_test_conn()
                os.environ["POSTGRES_SCHEMA"] = schema
                postgres.reset_schema_cache()

                with patch.object(postgres, "MIGRATIONS_DIR", folder):
                    with self.assertRaisesRegex(RuntimeError, "SQL migrations 目录为空"):
                        postgres.ensure_schema()
            finally:
                try:
                    postgres.drop_schema(schema)
                except Exception:
                    pass

                if old_conn is None:
                    os.environ.pop("POSTGRES_CONNECTION_STRING", None)
                else:
                    os.environ["POSTGRES_CONNECTION_STRING"] = old_conn

                if old_schema is None:
                    os.environ.pop("POSTGRES_SCHEMA", None)
                else:
                    os.environ["POSTGRES_SCHEMA"] = old_schema

                postgres.reset_schema_cache()


if __name__ == "__main__":
    unittest.main()
