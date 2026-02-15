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

    # 根因修复：每次 migrate_up 后自动同步所有序列，防止 backup restore 导致 PK 重复
    resync_sequences(postgres_module=postgres_module)

    return applied_count


def resync_sequences(*, postgres_module: Any | None = None) -> int:
    """将当前 schema 下所有 SERIAL/BIGSERIAL 序列重置为 MAX(id)+1。

    根因：pg_dump restore 会导入带显式 id 的行，但序列仍从 START WITH 1 开始，
    后续 INSERT 依赖 nextval() 时会与已有行冲突，抛出 duplicate key violation。

    Returns:
        修复的序列数量。
    """
    if postgres_module is None:
        postgres_module = _load_postgres_module()
    _require_driver(postgres_module)

    schema_name = postgres_module.get_schema_name()

    # 查询当前 schema 下所有拥有 sequence 的 (table, column) 对
    _FIND_SEQUENCES_SQL = """
        SELECT
            t.relname   AS table_name,
            a.attname   AS column_name,
            s.relname   AS sequence_name
        FROM pg_class s
        JOIN pg_namespace n  ON n.oid = s.relnamespace
        JOIN pg_depend d     ON d.objid = s.oid AND d.deptype = 'a'
        JOIN pg_class t      ON t.oid = d.refobjid
        JOIN pg_attribute a  ON a.attrelid = t.oid AND a.attnum = d.refobjsubid
        WHERE n.nspname = %s
          AND s.relkind = 'S'
    """

    fixed = 0
    with postgres_module.psycopg.connect(
        postgres_module.get_connection_string(),
        autocommit=True,
    ) as conn:
        with conn.cursor() as cur:
            _set_search_path(cur, postgres_module)
            cur.execute(_FIND_SEQUENCES_SQL, (schema_name,))
            seq_rows = cur.fetchall() or []

            for row in seq_rows:
                tbl = row[0] if isinstance(row, (list, tuple)) else row["table_name"]
                col = row[1] if isinstance(row, (list, tuple)) else row["column_name"]
                seq = row[2] if isinstance(row, (list, tuple)) else row["sequence_name"]

                # 取表中当前最大值
                cur.execute(
                    postgres_module.sql.SQL(
                        "SELECT COALESCE(MAX({}), 0) FROM {}.{}"
                    ).format(
                        postgres_module.sql.Identifier(col),
                        postgres_module.sql.Identifier(schema_name),
                        postgres_module.sql.Identifier(tbl),
                    )
                )
                max_row = cur.fetchone()
                max_val = int((max_row[0] if isinstance(max_row, (list, tuple)) else max_row.get("coalesce", 0)) or 0)

                # 取序列当前值
                cur.execute(
                    postgres_module.sql.SQL(
                        "SELECT last_value, is_called FROM {}.{}"
                    ).format(
                        postgres_module.sql.Identifier(schema_name),
                        postgres_module.sql.Identifier(seq),
                    )
                )
                seq_info = cur.fetchone()
                last_val = int((seq_info[0] if isinstance(seq_info, (list, tuple)) else seq_info.get("last_value", 0)) or 0)
                is_called = (seq_info[1] if isinstance(seq_info, (list, tuple)) else seq_info.get("is_called", False))

                # 仅在序列落后于数据时修复
                effective_next = last_val + 1 if is_called else last_val
                if max_val >= effective_next:
                    new_val = max_val + 1
                    cur.execute(
                        postgres_module.sql.SQL(
                            "SELECT setval({}, %s, false)"
                        ).format(
                            postgres_module.sql.Literal(f"{schema_name}.{seq}"),
                        ),
                        (new_val,),
                    )
                    _logger.info(
                        "resync seq %s.%s: %s -> %s (table %s.%s max=%s)",
                        schema_name, seq, last_val, new_val, schema_name, tbl, max_val,
                    )
                    fixed += 1

    if fixed:
        _logger.info("resynced %s sequence(s) in schema %s", fixed, schema_name)
    return fixed


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
