import os
import unittest
from unittest import mock

from db import postgres


class _DummyPool:
    created = []

    def __init__(self, **kwargs):
        self.kwargs = kwargs
        self.open_calls = 0
        self.close_calls = 0
        _DummyPool.created.append(self)

    def open(self, wait=True):
        self.open_calls += 1

    def close(self):
        self.close_calls += 1


class PostgresPoolTests(unittest.TestCase):
    def tearDown(self):
        postgres.close_pool()
        postgres.reset_schema_cache()
        _DummyPool.created.clear()

    def test_pool_enabled_requires_dependency(self):
        with mock.patch.dict(os.environ, {"POSTGRES_POOL_ENABLED": "1"}, clear=False):
            with mock.patch("db.postgres.PsycopgConnectionPool", None):
                self.assertFalse(postgres._pool_enabled())

    def test_get_pool_reuses_existing_pool(self):
        with mock.patch.dict(
            os.environ,
            {
                "POSTGRES_POOL_ENABLED": "1",
                "POSTGRES_POOL_MIN_SIZE": "1",
                "POSTGRES_POOL_MAX_SIZE": "3",
                "POSTGRES_POOL_TIMEOUT_SEC": "2",
            },
            clear=False,
        ):
            with mock.patch("db.postgres.PsycopgConnectionPool", _DummyPool):
                with mock.patch("db.postgres.get_connection_string", return_value="postgresql://u:p@localhost:5432/db"):
                    with mock.patch("db.postgres.get_schema_name", return_value="public"):
                        first = postgres._get_pool()
                        second = postgres._get_pool()

        self.assertIs(first, second)
        self.assertEqual(len(_DummyPool.created), 1)
        self.assertEqual(first.open_calls, 1)
        self.assertEqual(first.kwargs["min_size"], 1)
        self.assertEqual(first.kwargs["max_size"], 3)

    def test_reset_schema_cache_closes_pool(self):
        with mock.patch.dict(os.environ, {"POSTGRES_POOL_ENABLED": "1"}, clear=False):
            with mock.patch("db.postgres.PsycopgConnectionPool", _DummyPool):
                with mock.patch("db.postgres.get_connection_string", return_value="postgresql://u:p@localhost:5432/db"):
                    with mock.patch("db.postgres.get_schema_name", return_value="public"):
                        pool = postgres._get_pool()
                        postgres.reset_schema_cache()

        self.assertGreaterEqual(pool.close_calls, 1)


if __name__ == "__main__":
    unittest.main()
