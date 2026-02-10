import tempfile
import unittest
from pathlib import Path

from db.migrator import discover_migrations, parse_migration_filename


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


if __name__ == "__main__":
    unittest.main()
