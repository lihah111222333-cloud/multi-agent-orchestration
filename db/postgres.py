"""PostgreSQL 访问封装（强制数据库持久化，无文件回退）。"""

from __future__ import annotations

import atexit
import logging
import os
import re
import threading
from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path
from typing import Any, Iterable, Optional

from utils import as_float_env, as_int_env

try:
    import psycopg
    from psycopg import sql
    from psycopg.rows import dict_row
except ImportError:  # pragma: no cover
    psycopg = None  # type: ignore[assignment]
    sql = None  # type: ignore[assignment]
    dict_row = None  # type: ignore[assignment]

try:
    from psycopg_pool import ConnectionPool as PsycopgConnectionPool
except ImportError:  # pragma: no cover
    PsycopgConnectionPool = None

__all__ = [
    "get_connection_string",
    "get_schema_name",
    "reset_schema_cache",
    "close_pool",
    "ensure_schema",
    "connect_cursor",
    "execute",
    "fetch_all",
    "fetch_one",
    "drop_schema",
]

_IDENTIFIER_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
_logger = logging.getLogger(__name__)
_INIT_LOCK = threading.Lock()
_SCHEMA_READY_KEY: Optional[tuple[str, str]] = None
MIGRATIONS_DIR = Path(__file__).with_name("migrations")

_POOL_LOCK = threading.Lock()
_POOL: Optional[Any] = None
_POOL_KEY: Optional[tuple[str, str]] = None


def _require_driver() -> None:
    if psycopg is None:
        raise RuntimeError("缺少 psycopg，请先安装: pip install 'psycopg[binary]'")


def get_connection_string() -> str:
    """Return the PostgreSQL connection string from POSTGRES_CONNECTION_STRING.

    Raises:
        RuntimeError: If POSTGRES_CONNECTION_STRING is not set.
    """
    conn = os.getenv("POSTGRES_CONNECTION_STRING")
    if conn:
        return conn
    raise RuntimeError("未配置 POSTGRES_CONNECTION_STRING")


def get_schema_name() -> str:
    """Return the PostgreSQL schema name (default: 'public').

    Raises:
        RuntimeError: If the schema name contains invalid characters.
    """
    schema = (os.getenv("POSTGRES_SCHEMA") or "public").strip()
    if not _IDENTIFIER_RE.match(schema):
        raise RuntimeError(f"POSTGRES_SCHEMA 非法: {schema}")
    return schema


def reset_schema_cache() -> None:
    """Clear the schema-ready cache and close the connection pool."""
    global _SCHEMA_READY_KEY
    with _INIT_LOCK:
        _SCHEMA_READY_KEY = None
    close_pool()


def _schema_key() -> tuple[str, str]:
    return get_connection_string(), get_schema_name()


def _pool_enabled() -> bool:
    if PsycopgConnectionPool is None:
        return False
    return str(os.getenv("POSTGRES_POOL_ENABLED", "1")).strip().lower() not in {"0", "false", "no", "off"}


def close_pool() -> None:
    """Close the connection pool if active (atexit-safe)."""
    global _POOL, _POOL_KEY
    with _POOL_LOCK:
        if _POOL is not None:
            try:
                _POOL.close()
            except Exception:
                try:
                    _logger.debug("连接池关闭异常", exc_info=True)
                except Exception:
                    pass  # logging may be torn down at interpreter shutdown
            _POOL = None
            _POOL_KEY = None


def _get_pool() -> Optional[Any]:
    global _POOL, _POOL_KEY

    if not _pool_enabled():
        return None

    key = _schema_key()
    with _POOL_LOCK:
        if _POOL is not None and _POOL_KEY == key:
            return _POOL

        if _POOL is not None:
            try:
                _POOL.close()
            except Exception:
                _logger.debug("连接池关闭异常(重建)", exc_info=True)
            _POOL = None
            _POOL_KEY = None

        min_size = as_int_env("POSTGRES_POOL_MIN_SIZE", 1, min_value=1)
        max_size = as_int_env("POSTGRES_POOL_MAX_SIZE", 10, min_value=min_size)
        timeout = as_float_env("POSTGRES_POOL_TIMEOUT_SEC", 10.0, min_value=0.1)
        schema = get_schema_name()

        def _configure(conn: Any) -> None:
            previous_autocommit = bool(getattr(conn, "autocommit", False))
            try:
                conn.autocommit = True
                conn.execute(sql.SQL("SET search_path TO {}, public").format(sql.Identifier(schema)))
            finally:
                conn.autocommit = previous_autocommit

        pool = PsycopgConnectionPool(
            conninfo=get_connection_string(),
            min_size=min_size,
            max_size=max_size,
            timeout=timeout,
            configure=_configure,
            open=False,
        )
        pool.open(wait=False)

        _POOL = pool
        _POOL_KEY = key
        return _POOL


def _set_search_path(cur: Any) -> None:
    schema = get_schema_name()
    cur.execute(sql.SQL("SET search_path TO {}, public").format(sql.Identifier(schema)))


def _apply_sql_migrations(cur: Any) -> int:
    """Apply SQL migrations from `db/migrations` if present.

    Migration files must follow `NNNN_name.sql` naming and contiguous ordering.
    """
    if not MIGRATIONS_DIR.exists():
        return 0

    from db.migrator import discover_migrations

    migrations = discover_migrations(MIGRATIONS_DIR)
    if not migrations:
        return 0

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

    cur.execute("SELECT version FROM schema_migrations")
    applied_rows = cur.fetchall() or []
    applied_versions = {
        int(row["version"]) if isinstance(row, dict) else int(row[0])
        for row in applied_rows
    }

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

    return applied_count


def ensure_schema() -> None:
    """Ensure the target schema and all application tables exist (idempotent, thread-safe)."""
    global _SCHEMA_READY_KEY

    _require_driver()
    key = _schema_key()
    if _SCHEMA_READY_KEY == key:
        return

    with _INIT_LOCK:
        key = _schema_key()
        if _SCHEMA_READY_KEY == key:
            return

        with psycopg.connect(get_connection_string(), autocommit=True) as conn:
            with conn.cursor() as cur:
                cur.execute(sql.SQL("CREATE SCHEMA IF NOT EXISTS {}").format(sql.Identifier(get_schema_name())))
                _set_search_path(cur)
                applied_migrations = _apply_sql_migrations(cur)
                if applied_migrations:
                    _logger.info("applied %s sql migration(s) from %s", applied_migrations, MIGRATIONS_DIR)

                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS audit_events (
                        id BIGSERIAL PRIMARY KEY,
                        ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        event_type TEXT NOT NULL,
                        action TEXT NOT NULL,
                        result TEXT NOT NULL,
                        actor TEXT NOT NULL DEFAULT '',
                        target TEXT NOT NULL DEFAULT '',
                        detail TEXT NOT NULL DEFAULT '',
                        level TEXT NOT NULL DEFAULT 'INFO',
                        extra JSONB
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_audit_events_ts ON audit_events (ts DESC)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_audit_events_event_type ON audit_events (event_type)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_audit_events_action ON audit_events (action)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_audit_events_result ON audit_events (result)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events (actor)")

                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS system_logs (
                        id BIGSERIAL PRIMARY KEY,
                        ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        level TEXT NOT NULL,
                        logger TEXT NOT NULL,
                        message TEXT NOT NULL,
                        raw TEXT NOT NULL DEFAULT ''
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_system_logs_ts ON system_logs (ts DESC)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_system_logs_level ON system_logs (level)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_system_logs_logger ON system_logs (logger)")

                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS topology_approvals (
                        id TEXT PRIMARY KEY,
                        status TEXT NOT NULL,
                        requested_by TEXT NOT NULL DEFAULT '',
                        reason TEXT NOT NULL DEFAULT '',
                        created_at TIMESTAMPTZ NOT NULL,
                        expire_at TIMESTAMPTZ NOT NULL,
                        reviewed_at TIMESTAMPTZ,
                        reviewer TEXT NOT NULL DEFAULT '',
                        review_note TEXT NOT NULL DEFAULT '',
                        arch_hash TEXT NOT NULL,
                        proposed_architecture JSONB NOT NULL
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_topology_approvals_status_created_at ON topology_approvals (status, created_at DESC)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_topology_approvals_arch_hash ON topology_approvals (arch_hash)")

                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS topology_approval_archives (
                        id TEXT PRIMARY KEY,
                        status TEXT NOT NULL,
                        requested_by TEXT NOT NULL DEFAULT '',
                        reason TEXT NOT NULL DEFAULT '',
                        created_at TIMESTAMPTZ NOT NULL,
                        expire_at TIMESTAMPTZ NOT NULL,
                        reviewed_at TIMESTAMPTZ,
                        reviewer TEXT NOT NULL DEFAULT '',
                        review_note TEXT NOT NULL DEFAULT '',
                        arch_hash TEXT NOT NULL,
                        proposed_architecture JSONB NOT NULL,
                        archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_topology_approval_archives_archived_at ON topology_approval_archives (archived_at DESC)")

                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS shared_files (
                        path TEXT PRIMARY KEY,
                        content TEXT NOT NULL,
                        updated_by TEXT NOT NULL DEFAULT '',
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_shared_files_updated_at ON shared_files (updated_at DESC)")

                # Dashboard 提示词配置表
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS prompts (
                        id BIGSERIAL PRIMARY KEY,
                        agent_key TEXT NOT NULL,
                        tool_name TEXT NOT NULL,
                        prompt_text TEXT NOT NULL DEFAULT '',
                        is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
                        sort_order INT NOT NULL DEFAULT 0,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        UNIQUE (agent_key, tool_name)
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_prompts_agent_key ON prompts (agent_key)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_prompts_sort_order ON prompts (sort_order, agent_key)")

                # Agent 交互表
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS agent_interactions (
                        id BIGSERIAL PRIMARY KEY,
                        thread_id TEXT NOT NULL DEFAULT '',
                        parent_id BIGINT,
                        sender TEXT NOT NULL,
                        receiver TEXT NOT NULL DEFAULT '',
                        msg_type TEXT NOT NULL DEFAULT 'task',
                        status TEXT NOT NULL DEFAULT 'pending',
                        requires_review BOOLEAN NOT NULL DEFAULT FALSE,
                        reviewed_by TEXT NOT NULL DEFAULT '',
                        review_note TEXT NOT NULL DEFAULT '',
                        reviewed_at TIMESTAMPTZ,
                        payload JSONB NOT NULL DEFAULT '{}'::jsonb,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_agent_interactions_thread_created ON agent_interactions (thread_id, created_at DESC)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_agent_interactions_sender_receiver ON agent_interactions (sender, receiver)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_agent_interactions_status_review ON agent_interactions (status, requires_review, created_at DESC)")

                # Agent 提示词模板表
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS prompt_templates (
                        id BIGSERIAL PRIMARY KEY,
                        prompt_key TEXT NOT NULL UNIQUE,
                        title TEXT NOT NULL DEFAULT '',
                        agent_key TEXT NOT NULL DEFAULT '',
                        tool_name TEXT NOT NULL DEFAULT '',
                        prompt_text TEXT NOT NULL,
                        variables JSONB,
                        tags JSONB,
                        enabled BOOLEAN NOT NULL DEFAULT TRUE,
                        created_by TEXT NOT NULL DEFAULT '',
                        updated_by TEXT NOT NULL DEFAULT '',
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_prompt_templates_agent_tool ON prompt_templates (agent_key, tool_name)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_prompt_templates_enabled ON prompt_templates (enabled, updated_at DESC)")

                # Agent 提示词模板版本归档表
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS prompt_template_versions (
                        id BIGSERIAL PRIMARY KEY,
                        prompt_key TEXT NOT NULL,
                        title TEXT NOT NULL DEFAULT '',
                        agent_key TEXT NOT NULL DEFAULT '',
                        tool_name TEXT NOT NULL DEFAULT '',
                        prompt_text TEXT NOT NULL,
                        variables JSONB,
                        tags JSONB,
                        enabled BOOLEAN NOT NULL DEFAULT TRUE,
                        created_by TEXT NOT NULL DEFAULT '',
                        updated_by TEXT NOT NULL DEFAULT '',
                        source_updated_at TIMESTAMPTZ,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                    """
                )
                cur.execute(
                    "CREATE INDEX IF NOT EXISTS idx_prompt_template_versions_key_id ON prompt_template_versions (prompt_key, id DESC)"
                )

                # 命令卡表
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS command_cards (
                        id BIGSERIAL PRIMARY KEY,
                        card_key TEXT NOT NULL UNIQUE,
                        title TEXT NOT NULL,
                        description TEXT NOT NULL DEFAULT '',
                        command_template TEXT NOT NULL,
                        args_schema JSONB,
                        risk_level TEXT NOT NULL DEFAULT 'normal',
                        enabled BOOLEAN NOT NULL DEFAULT TRUE,
                        created_by TEXT NOT NULL DEFAULT '',
                        updated_by TEXT NOT NULL DEFAULT '',
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_command_cards_risk_enabled ON command_cards (risk_level, enabled, updated_at DESC)")

                # 命令卡执行流水表
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS command_card_runs (
                        id BIGSERIAL PRIMARY KEY,
                        card_key TEXT NOT NULL,
                        requested_by TEXT NOT NULL DEFAULT '',
                        params JSONB NOT NULL DEFAULT '{}'::jsonb,
                        rendered_command TEXT NOT NULL,
                        risk_level TEXT NOT NULL DEFAULT 'normal',
                        status TEXT NOT NULL DEFAULT 'pending_review',
                        requires_review BOOLEAN NOT NULL DEFAULT TRUE,
                        interaction_id BIGINT,
                        output TEXT NOT NULL DEFAULT '',
                        error TEXT NOT NULL DEFAULT '',
                        exit_code INT,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                        executed_at TIMESTAMPTZ
                    )
                    """
                )
                cur.execute("CREATE INDEX IF NOT EXISTS idx_command_card_runs_status_created ON command_card_runs (status, created_at DESC)")
                cur.execute("CREATE INDEX IF NOT EXISTS idx_command_card_runs_card_key ON command_card_runs (card_key, created_at DESC)")

        _SCHEMA_READY_KEY = key


@contextmanager
def _open_cursor(
    conn: Any, row_as_dict: bool, read_only: bool, *, set_search_path: bool = True,
) -> Generator[Any, None, None]:
    """Create a cursor with optional search_path and read_only setup."""
    kwargs: dict[str, Any] = {"row_factory": dict_row} if row_as_dict else {}
    with conn.cursor(**kwargs) as cur:
        if set_search_path:
            _set_search_path(cur)
        if read_only:
            cur.execute("SET TRANSACTION READ ONLY")
        yield cur


@contextmanager
def connect_cursor(
    row_as_dict: bool = False,
    *,
    autocommit: bool = True,
    read_only: bool = False,
) -> Generator[Any, None, None]:
    """Yield a database cursor within a managed connection.

    Args:
        row_as_dict: If True, rows are returned as dicts.
        autocommit: Connection autocommit mode (default True).
        read_only: If True, set the transaction to READ ONLY (requires autocommit=False).

    Raises:
        ValueError: If read_only=True with autocommit=True.
    """
    _require_driver()
    ensure_schema()

    if read_only and autocommit:
        raise ValueError("read_only 事务要求 autocommit=False")

    pool = _get_pool()

    if pool is not None:
        with pool.connection() as conn:
            conn.autocommit = autocommit
            # Pool connections have search_path pre-set via configure callback
            with _open_cursor(conn, row_as_dict, read_only, set_search_path=False) as cur:
                yield cur
        return

    with psycopg.connect(get_connection_string(), autocommit=autocommit) as conn:
        with _open_cursor(conn, row_as_dict, read_only) as cur:
            yield cur


def execute(query: str, params: Optional[Iterable[Any]] = None) -> int:
    """Execute a query and return the number of affected rows."""
    with connect_cursor(row_as_dict=False) as cur:
        cur.execute(query, tuple(params or ()))
        return cur.rowcount


def fetch_all(query: str, params: Optional[Iterable[Any]] = None) -> list[dict[str, Any]]:
    """Execute a query and return all rows as a list of dicts."""
    with connect_cursor(row_as_dict=True) as cur:
        cur.execute(query, tuple(params or ()))
        return cur.fetchall()


def fetch_one(query: str, params: Optional[Iterable[Any]] = None) -> Optional[dict[str, Any]]:
    """Execute a query and return the first row as a dict, or None."""
    with connect_cursor(row_as_dict=True) as cur:
        cur.execute(query, tuple(params or ()))
        return cur.fetchone()


def drop_schema(schema_name: Optional[str] = None) -> None:
    """Drop a schema and all its objects (CASCADE). USE WITH EXTREME CAUTION.

    Args:
        schema_name: Schema to drop. Defaults to the current POSTGRES_SCHEMA.
    """
    _require_driver()
    target = (schema_name or get_schema_name()).strip()
    if not _IDENTIFIER_RE.match(target):
        raise RuntimeError(f"schema 名非法: {target}")

    _logger.warning("正在删除 schema: %s (CASCADE)", target)
    with psycopg.connect(get_connection_string(), autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.execute(sql.SQL("DROP SCHEMA IF EXISTS {} CASCADE").format(sql.Identifier(target)))

    reset_schema_cache()


atexit.register(close_pool)
