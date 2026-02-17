"""tests/test_pg_autostart.py — db/pg_autostart.py 单元测试（纯 mock，不依赖真实 PG）。"""

import os
import unittest
from unittest import mock

from db import pg_autostart


class TestEnvBool(unittest.TestCase):
    def test_default_true(self):
        with mock.patch.dict(os.environ, {}, clear=False):
            os.environ.pop("PG_AUTOSTART_ENABLED", None)
            self.assertTrue(pg_autostart._env_bool("PG_AUTOSTART_ENABLED", default=True))

    def test_explicit_zero(self):
        with mock.patch.dict(os.environ, {"PG_AUTOSTART_ENABLED": "0"}, clear=False):
            self.assertFalse(pg_autostart._env_bool("PG_AUTOSTART_ENABLED"))

    def test_explicit_false(self):
        with mock.patch.dict(os.environ, {"PG_AUTOSTART_ENABLED": "false"}, clear=False):
            self.assertFalse(pg_autostart._env_bool("PG_AUTOSTART_ENABLED"))


class TestParseConnString(unittest.TestCase):
    def test_standard_url(self):
        with mock.patch.dict(os.environ, {
            "POSTGRES_CONNECTION_STRING": "postgresql://wjbot:pw123@localhost:54320/wjbotexport"
        }):
            info = pg_autostart._parse_conn_string()
            self.assertEqual(info["host"], "localhost")
            self.assertEqual(info["port"], "54320")
            self.assertEqual(info["user"], "wjbot")
            self.assertEqual(info["password"], "pw123")
            self.assertEqual(info["dbname"], "wjbotexport")

    def test_empty(self):
        with mock.patch.dict(os.environ, {}, clear=False):
            os.environ.pop("POSTGRES_CONNECTION_STRING", None)
            self.assertEqual(pg_autostart._parse_conn_string(), {})


class TestAutoStartPostgres(unittest.TestCase):
    def setUp(self):
        # 重置 _started 标志，让每个测试都能运行
        pg_autostart._started = False

    def tearDown(self):
        pg_autostart._started = False

    def test_disabled_by_env(self):
        """PG_AUTOSTART_ENABLED=0 时应跳过。"""
        with mock.patch.dict(os.environ, {"PG_AUTOSTART_ENABLED": "0"}, clear=False):
            with mock.patch.object(pg_autostart, "_pg_isready") as m:
                pg_autostart.auto_start_postgres()
                m.assert_not_called()

    def test_skip_remote_host(self):
        """非 localhost 连接不应尝试启动。"""
        with mock.patch.dict(os.environ, {
            "PG_AUTOSTART_ENABLED": "1",
            "POSTGRES_CONNECTION_STRING": "postgresql://u:p@remote-server:5432/db",
        }, clear=False):
            with mock.patch.object(pg_autostart, "_start_pg") as m:
                pg_autostart.auto_start_postgres()
                m.assert_not_called()

    def test_already_running(self):
        """PG 已在线时不调用 _start_pg。"""
        with mock.patch.dict(os.environ, {
            "PG_AUTOSTART_ENABLED": "1",
            "POSTGRES_CONNECTION_STRING": "postgresql://u:p@localhost:54320/db",
        }, clear=False):
            with mock.patch.object(pg_autostart, "_pg_isready", return_value=True):
                with mock.patch.object(pg_autostart, "_start_pg") as m:
                    pg_autostart.auto_start_postgres()
                    m.assert_not_called()

    def test_starts_when_down(self):
        """PG 未运行时应启动并等待。"""
        with mock.patch.dict(os.environ, {
            "PG_AUTOSTART_ENABLED": "1",
            "POSTGRES_CONNECTION_STRING": "postgresql://u:p@localhost:54320/db",
            "PG_AUTOSTART_TIMEOUT": "2",
        }, clear=False):
            ready_calls = [False, False, True]  # 第三次检测才在线
            with mock.patch.object(pg_autostart, "_pg_isready", side_effect=lambda *a: ready_calls.pop(0) if ready_calls else True):
                with mock.patch.object(pg_autostart, "_get_data_dir", return_value="/fake/pgdata"):
                    with mock.patch.object(pg_autostart, "_clean_stale_pid"):
                        with mock.patch.object(pg_autostart, "_start_pg", return_value=True) as start_mock:
                            with mock.patch.object(pg_autostart, "_ensure_user_and_db"):
                                with mock.patch.object(pg_autostart, "_db_has_data", return_value=True):
                                    pg_autostart.auto_start_postgres()
                                    start_mock.assert_called_once_with("/fake/pgdata")

    def test_idempotent(self):
        """连续调用只执行一次。"""
        with mock.patch.dict(os.environ, {
            "PG_AUTOSTART_ENABLED": "1",
            "POSTGRES_CONNECTION_STRING": "postgresql://u:p@localhost:54320/db",
        }, clear=False):
            with mock.patch.object(pg_autostart, "_pg_isready", return_value=True):
                pg_autostart.auto_start_postgres()
                pg_autostart.auto_start_postgres()
                # 第二次应直接跳过

    def test_no_data_dir_skips(self):
        """找不到数据目录时应跳过。"""
        with mock.patch.dict(os.environ, {
            "PG_AUTOSTART_ENABLED": "1",
            "POSTGRES_CONNECTION_STRING": "postgresql://u:p@localhost:54320/db",
        }, clear=False):
            with mock.patch.object(pg_autostart, "_pg_isready", return_value=False):
                with mock.patch.object(pg_autostart, "_get_data_dir", return_value=None):
                    with mock.patch.object(pg_autostart, "_start_pg") as m:
                        pg_autostart.auto_start_postgres()
                        m.assert_not_called()

    def test_restores_backup_when_empty(self):
        """数据库为空时应自动恢复备份。"""
        with mock.patch.dict(os.environ, {
            "PG_AUTOSTART_ENABLED": "1",
            "POSTGRES_CONNECTION_STRING": "postgresql://u:p@localhost:54320/mydb",
            "PG_AUTOSTART_TIMEOUT": "1",
        }, clear=False):
            with mock.patch.object(pg_autostart, "_pg_isready", return_value=False):
                with mock.patch.object(pg_autostart, "_get_data_dir", return_value="/fake/pgdata"):
                    with mock.patch.object(pg_autostart, "_clean_stale_pid"):
                        with mock.patch.object(pg_autostart, "_start_pg", return_value=True):
                            with mock.patch.object(pg_autostart, "_wait_for_ready", return_value=True):
                                with mock.patch.object(pg_autostart, "_ensure_user_and_db"):
                                    with mock.patch.object(pg_autostart, "_db_has_data", return_value=False):
                                        with mock.patch.object(pg_autostart, "_find_latest_backup", return_value="/fake/backup.sql"):
                                            with mock.patch.object(pg_autostart, "_restore_backup") as restore_mock:
                                                pg_autostart.auto_start_postgres()
                                                restore_mock.assert_called_once()


class TestCleanStalePid(unittest.TestCase):
    def test_no_pid_file(self):
        with mock.patch("os.path.exists", return_value=False):
            self.assertFalse(pg_autostart._clean_stale_pid("/fake"))

    def test_process_alive(self):
        """进程存在时不清理。"""
        with mock.patch("os.path.exists", return_value=True):
            with mock.patch("builtins.open", mock.mock_open(read_data="12345\n")):
                with mock.patch("os.kill"):  # 不抛异常 = 进程存在
                    self.assertFalse(pg_autostart._clean_stale_pid("/fake"))

    def test_process_dead(self):
        """进程不存在时清理 PID 文件。"""
        with mock.patch("os.path.exists", return_value=True):
            with mock.patch("builtins.open", mock.mock_open(read_data="99999\n")):
                with mock.patch("os.kill", side_effect=ProcessLookupError):
                    with mock.patch("os.remove") as rm:
                        result = pg_autostart._clean_stale_pid("/fake")
                        self.assertTrue(result)
                        rm.assert_called_once_with("/fake/postmaster.pid")


class TestFindPgBin(unittest.TestCase):
    def test_homebrew_path(self):
        with mock.patch("os.path.isfile", side_effect=lambda p: "postgresql@16" in p):
            with mock.patch("os.access", return_value=True):
                result = pg_autostart._find_pg_bin("pg_isready")
                self.assertIn("postgresql@16", result)


if __name__ == "__main__":
    unittest.main()
