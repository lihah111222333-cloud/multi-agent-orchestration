"""PostgreSQL 访问封装（强制数据库持久化，无文件回退）。"""

from __future__ import annotations

import atexit
import logging
import os
import re
import threading
from collections.abc import Generator
from contextlib import contextmanager
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
_SCHEMA_READY: bool = False  # fast-path flag (no lock needed for reads)
_SCHEMA_READY_KEY: Optional[tuple[str, str]] = None

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
    global _SCHEMA_READY, _SCHEMA_READY_KEY
    with _INIT_LOCK:
        _SCHEMA_READY = False
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


def ensure_schema() -> None:
    """Thin wrapper: run schema migrations once per schema key."""
    global _SCHEMA_READY, _SCHEMA_READY_KEY

    # Fast path — no lock, no _schema_key() computation
    if _SCHEMA_READY:
        return

    _require_driver()
    key = _schema_key()
    if _SCHEMA_READY_KEY == key:
        _SCHEMA_READY = True
        return

    with _INIT_LOCK:
        if _SCHEMA_READY:
            return
        key = _schema_key()
        if _SCHEMA_READY_KEY == key:
            _SCHEMA_READY = True
            return

        from db.migrator import MIGRATIONS_DIR, migrate_up

        applied_migrations = migrate_up()
        if not applied_migrations:
            _logger.debug("schema already up-to-date, no pending sql migrations in %s", MIGRATIONS_DIR)

        _SCHEMA_READY_KEY = key
        _SCHEMA_READY = True


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
