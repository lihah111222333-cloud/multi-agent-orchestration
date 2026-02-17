import json
import tempfile
import unittest
from pathlib import Path

from config import settings


class SettingsSnapshotTests(unittest.TestCase):
    def test_save_architecture_creates_backup_and_snapshot(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            config_path = tmp / "config.json"
            backup_dir = tmp / "config_backups"

            old_raw = {
                "gateways": [
                    {
                        "id": "gateway_1",
                        "name": "旧网关",
                        "agents": [{"id": "agent_1", "name": "旧代理"}],
                    }
                ]
            }
            new_raw = {
                "gateways": [
                    {
                        "id": "gateway_2",
                        "name": "新网关",
                        "agents": [{"id": "agent_2", "name": "新代理", "capabilities": ["x"]}],
                    }
                ]
            }
            config_path.write_text(json.dumps(old_raw, ensure_ascii=False), encoding="utf-8")

            old_config_file = settings.CONFIG_FILE
            old_backup_dir = settings.CONFIG_BACKUP_DIR
            old_backup_enabled = settings.CONFIG_BACKUP_ENABLED
            old_backup_keep = settings.CONFIG_BACKUP_KEEP
            try:
                settings.CONFIG_FILE = config_path
                settings.CONFIG_BACKUP_DIR = backup_dir
                settings.CONFIG_BACKUP_ENABLED = True
                settings.CONFIG_BACKUP_KEEP = 5

                backup_path = settings.save_architecture(new_raw)
                self.assertTrue(backup_path)
                self.assertTrue(Path(backup_path).exists())

                loaded = settings.load_architecture_raw()
                self.assertEqual(loaded["gateways"][0]["id"], "gateway_2")

                snapshot = settings.load_architecture_snapshot()
                self.assertTrue(snapshot["hash"].startswith("sha256:"))
                self.assertEqual(snapshot["raw"]["gateways"][0]["id"], "gateway_2")
                self.assertIn("gateway_2", snapshot["gateway_map"])
            finally:
                settings.CONFIG_FILE = old_config_file
                settings.CONFIG_BACKUP_DIR = old_backup_dir
                settings.CONFIG_BACKUP_ENABLED = old_backup_enabled
                settings.CONFIG_BACKUP_KEEP = old_backup_keep


if __name__ == "__main__":
    unittest.main()
