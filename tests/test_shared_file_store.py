import unittest

import shared_file_store
from tests.pg_test_helper import isolated_pg_schema


class SharedFileStoreTests(unittest.TestCase):
    def test_list_files_prefix_escapes_like_wildcards(self):
        with isolated_pg_schema("shared"):
            shared_file_store.write_file("workspace_1/a.txt", "one", actor="tester")
            shared_file_store.write_file("workspaceX/a.txt", "two", actor="tester")

            rows = shared_file_store.list_files("workspace_", limit=10)
            paths = {row["path"] for row in rows}
            self.assertIn("workspace_1/a.txt", paths)
            self.assertNotIn("workspaceX/a.txt", paths)

    def test_write_read_list_delete(self):
        with isolated_pg_schema("shared"):
            saved = shared_file_store.write_file("workspace/a.txt", "hello", actor="tester")
            self.assertEqual(saved["path"], "workspace/a.txt")
            self.assertEqual(saved["content"], "hello")

            loaded = shared_file_store.read_file("workspace/a.txt")
            self.assertIsNotNone(loaded)
            self.assertEqual(loaded["updated_by"], "tester")

            rows = shared_file_store.list_files("workspace", limit=10)
            self.assertEqual(len(rows), 1)
            self.assertEqual(rows[0]["path"], "workspace/a.txt")

            removed = shared_file_store.delete_file("workspace/a.txt", actor="tester")
            self.assertTrue(removed["deleted"])

            missing = shared_file_store.read_file("workspace/a.txt")
            self.assertIsNone(missing)


if __name__ == "__main__":
    unittest.main()
