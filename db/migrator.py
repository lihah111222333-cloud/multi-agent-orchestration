"""PostgreSQL schema migration runner."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any
import logging
import re

_MIGRATION_FILENAME_RE = re.compile(r"^(\d{4})_([a-z0-9_]+)\.sql$")
_logger = logging.getLogger(__name__)
MIGRATIONS_DIR = Path(__file__).with_name("migrations")


@dataclass(frozen=True)
class Migration:
    version: int
    name: str
    filename: str
    path: Path


def _load_postgres_module() -> Any:
    from db import postgres

    return postgres


def _require_driver(postgres_module: Any) -> None:
    if postgres_module.psycopg is None:
        raise RuntimeError("缺少 psycopg，请先安装: pip install 'psycopg[binary]'")


def _resolve_migrations_dir(directory: str | Path | None) -> Path:
    folder = Path(directory or MIGRATIONS_DIR)
    if not folder.exists() or not folder.is_dir():
        raise RuntimeError(f"未找到 SQL migrations 目录: {folder}")
    return folder


def _set_search_path(cur: Any, postgres_module: Any) -> None:
    schema_name = postgres_module.get_schema_name()
    cur.execute(
        postgres_module.sql.SQL("SET search_path TO {}, public").format(
            postgres_module.sql.Identifier(schema_name)
        )
    )


def _ensure_schema_migrations_table(cur: Any) -> None:
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version INT PRIMARY KEY,
            name TEXT NOT NULL,
            filename TEXT NOT NULL,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )
        """
    )


def _read_applied_versions(cur: Any) -> set[int]:
    cur.execute("SELECT version FROM schema_migrations")
    applied_rows = cur.fetchall() or []
    return {
        int(row["version"]) if isinstance(row, dict) else int(row[0])
        for row in applied_rows
    }


def parse_migration_filename(filename: str) -> tuple[int, str]:
    """Parse migration filename into version and name.

    Expected format: NNNN_name.sql (name uses lowercase letters, numbers, underscores).
    """
    text = str(filename or "").strip()
    match = _MIGRATION_FILENAME_RE.fullmatch(text)
    if match is None:
        raise ValueError(f"invalid migration filename: {filename}")

    version = int(match.group(1))
    name = match.group(2)
    if not name:
        raise ValueError(f"invalid migration filename: {filename}")
    return version, name


def discover_migrations(directory: str | Path) -> list[Migration]:
    """Discover migrations from a directory and validate ordering rules."""
    folder = Path(directory)
    if not folder.exists() or not folder.is_dir():
        raise ValueError(f"migration directory not found: {folder}")

    migrations: list[Migration] = []
    seen_versions: dict[int, str] = {}

    for path in folder.iterdir():
        if not path.is_file() or path.suffix.lower() != ".sql":
            continue

        version, name = parse_migration_filename(path.name)
        if version in seen_versions:
            existing = seen_versions[version]
            raise ValueError(
                f"duplicate migration version {version:04d}: {existing} and {path.name}"
            )

        seen_versions[version] = path.name
        migrations.append(
            Migration(
                version=version,
                name=name,
                filename=path.name,
                path=path,
            )
        )

    migrations.sort(key=lambda item: item.version)

    for index, migration in enumerate(migrations):
        expected = index + 1
        if migration.version != expected:
            raise ValueError(
                f"non-contiguous migration versions: expected version {expected}, got {migration.version}"
            )

    return migrations


def migrate_up(directory: str | Path | None = None) -> int:
    """Apply all pending SQL migrations and return applied count."""
    postgres_module = _load_postgres_module()
    _require_driver(postgres_module)

    migrations_dir = _resolve_migrations_dir(directory)
    migrations = discover_migrations(migrations_dir)
    if not migrations:
        raise RuntimeError(f"SQL migrations 目录为空: {migrations_dir}")

    with postgres_module.psycopg.connect(
        postgres_module.get_connection_string(),
        autocommit=True,
    ) as conn:
        with conn.cursor() as cur:
            cur.execute(
                postgres_module.sql.SQL("CREATE SCHEMA IF NOT EXISTS {}").format(
                    postgres_module.sql.Identifier(postgres_module.get_schema_name())
                )
            )
            _set_search_path(cur, postgres_module)
            _ensure_schema_migrations_table(cur)
            applied_versions = _read_applied_versions(cur)

            applied_count = 0
            for migration in migrations:
                if migration.version in applied_versions:
                    continue

                sql_text = migration.path.read_text(encoding="utf-8").strip()
                if not sql_text:
                    raise ValueError(f"migration SQL 为空: {migration.filename}")

                cur.execute(sql_text)
                cur.execute(
                    """
                    INSERT INTO schema_migrations (version, name, filename)
                    VALUES (%s, %s, %s)
                    """,
                    (migration.version, migration.name, migration.filename),
                )
                applied_count += 1

    if applied_count:
        _logger.info("applied %s sql migration(s) from %s", applied_count, migrations_dir)

    return applied_count


def current_version() -> int:
    """Return current schema migration version (0 if uninitialized)."""
    postgres_module = _load_postgres_module()
    _require_driver(postgres_module)

    schema_name = postgres_module.get_schema_name()

    with postgres_module.psycopg.connect(
        postgres_module.get_connection_string(),
        autocommit=True,
    ) as conn:
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT COUNT(*)
                FROM information_schema.tables
                WHERE table_schema = %s
                  AND table_name = 'schema_migrations'
                """,
                (schema_name,),
            )
            row = cur.fetchone()
            if row is None or int(row[0]) == 0:
                return 0

            cur.execute(
                postgres_module.sql.SQL(
                    "SELECT COALESCE(MAX(version), 0) FROM {}.schema_migrations"
                ).format(postgres_module.sql.Identifier(schema_name))
            )
            version_row = cur.fetchone()
            if version_row is None:
                return 0
            return int(version_row[0] or 0)


def migrate_down(target_version: int) -> int:
    """Down migrations are intentionally unsupported in current phase."""
    raise NotImplementedError(f"当前版本不支持向下迁移: target_version={target_version}")
