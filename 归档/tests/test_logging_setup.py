import logging
import unittest

import logging_setup
import system_log
from tests.pg_test_helper import isolated_pg_schema


class LoggingSetupTests(unittest.TestCase):
    def test_setup_uses_postgres_handler(self):
        with isolated_pg_schema("logging"):
            root = logging.getLogger()
            old_handlers = list(root.handlers)
            old_level = root.level

            for handler in old_handlers:
                root.removeHandler(handler)

            try:
                logging_setup.setup_global_logging(default_level="INFO")

                pg_handlers = [h for h in root.handlers if isinstance(h, logging_setup.PostgresLogHandler)]
                self.assertEqual(len(pg_handlers), 1)

                logger = logging.getLogger("logging_setup_test")
                logger.info("hello-pg-log")

                rows = system_log.query_logs(limit=20, logger_name="logging_setup_test", keyword="hello-pg-log")
                self.assertEqual(len(rows), 1)
                self.assertEqual(rows[0]["level"], "INFO")
            finally:
                for handler in list(root.handlers):
                    root.removeHandler(handler)
                    handler.close()

                for handler in old_handlers:
                    root.addHandler(handler)
                root.setLevel(old_level)


if __name__ == "__main__":
    unittest.main()
