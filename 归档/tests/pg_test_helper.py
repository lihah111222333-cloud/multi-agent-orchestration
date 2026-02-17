import os
import uuid
from contextlib import contextmanager

from db import postgres


def _resolve_test_conn() -> str:
    return (
        os.getenv("TEST_POSTGRES_CONNECTION_STRING")
        or os.getenv("POSTGRES_CONNECTION_STRING")
        or "postgresql://wjbot:wjbot123456@localhost:54320/wjbotexport"
    )


@contextmanager
def isolated_pg_schema(prefix: str = "test"):
    old_conn = os.getenv("POSTGRES_CONNECTION_STRING")
    old_schema = os.getenv("POSTGRES_SCHEMA")

    schema = f"{prefix}_{uuid.uuid4().hex[:10]}"
    os.environ["POSTGRES_CONNECTION_STRING"] = _resolve_test_conn()
    os.environ["POSTGRES_SCHEMA"] = schema

    postgres.reset_schema_cache()
    postgres.ensure_schema()

    try:
        yield schema
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
