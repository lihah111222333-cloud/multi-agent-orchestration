"""ACP-Bus 健壮性修复验证测试。

覆盖:
  - _atomic_write_json  原子写入正确性
  - _locked_json_rw     上下文管理器基本流程 + 并发安全
  - task / approval ID  UUID 唯一性
  - _make_hot_reloadable 间接调用正确性
"""
from __future__ import annotations

import json
import os
import sys
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))

from agents.all_in_one import (
    _atomic_write_json,
    _locked_json_rw,
    _make_hot_reloadable,
    _HOT_RELOAD_TOOL_NAMES,
    task,
    approval,
    lock,
    interaction,
)


class TestAtomicWriteJson(unittest.TestCase):
    """_atomic_write_json: 先写 tmp 再 os.replace。"""

    def test_write_and_read_back(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "test.json"
            data = {"key": "value", "num": 42, "中文": "测试"}
            _atomic_write_json(path, data)
            self.assertTrue(path.exists())
            loaded = json.loads(path.read_text("utf-8"))
            self.assertEqual(loaded["key"], "value")
            self.assertEqual(loaded["num"], 42)
            self.assertEqual(loaded["中文"], "测试")

    def test_no_tmp_left_behind(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "clean.json"
            _atomic_write_json(path, [1, 2, 3])
            files = list(Path(td).iterdir())
            self.assertEqual(len(files), 1)
            self.assertEqual(files[0].name, "clean.json")

    def test_overwrite_existing(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "overwrite.json"
            _atomic_write_json(path, {"v": 1})
            _atomic_write_json(path, {"v": 2})
            loaded = json.loads(path.read_text("utf-8"))
            self.assertEqual(loaded["v"], 2)


class TestLockedJsonRW(unittest.TestCase):
    """_locked_json_rw: 带 flock 的读-改-写上下文管理器。"""

    def test_basic_list_rw(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "list.json"
            # 文件不存在，使用 default
            with _locked_json_rw(path, default=list) as (data, save):
                self.assertEqual(data, [])
                data.append("item1")
                save(data)
            # 文件现在存在
            with _locked_json_rw(path, default=list) as (data, save):
                self.assertEqual(data, ["item1"])

    def test_basic_dict_rw(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "dict.json"
            with _locked_json_rw(path, default=dict) as (data, save):
                self.assertEqual(data, {})
                data["key"] = "value"
                save(data)
            with _locked_json_rw(path, default=dict) as (data, save):
                self.assertEqual(data["key"], "value")

    def test_corrupted_file_uses_default(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "bad.json"
            path.write_text("not-valid-json{{", "utf-8")
            with _locked_json_rw(path, default=list) as (data, save):
                self.assertEqual(data, [])

    def test_concurrent_writes_no_data_loss(self):
        """多线程并发写入同一文件，验证无数据丢失。"""
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "concurrent.json"
            _atomic_write_json(path, [])  # 初始化

            num_threads = 10
            items_per_thread = 5
            errors = []

            def worker(thread_id):
                try:
                    for i in range(items_per_thread):
                        with _locked_json_rw(path, default=list) as (data, save):
                            data.append(f"t{thread_id}-{i}")
                            save(data)
                except Exception as e:
                    errors.append(e)

            threads = [threading.Thread(target=worker, args=(tid,)) for tid in range(num_threads)]
            for t in threads:
                t.start()
            for t in threads:
                t.join(timeout=10)

            self.assertEqual(errors, [], f"Worker errors: {errors}")

            with _locked_json_rw(path, default=list) as (data, _save):
                self.assertEqual(
                    len(data), num_threads * items_per_thread,
                    f"Expected {num_threads * items_per_thread} items, got {len(data)}: data loss detected",
                )


class TestUUIDIds(unittest.TestCase):
    """task / approval ID 使用 uuid，不再碰撞。"""

    def test_task_ids_unique(self):
        """生成 100 个 task，ID 全部唯一。"""
        with tempfile.TemporaryDirectory() as td:
            store = Path(td) / "agent_tasks.json"
            _atomic_write_json(store, [])

            ids = set()
            # Patch the store path
            with mock.patch("agents.all_in_one.Path") as MockPath:
                MockPath.__file__ = Path(__file__)
                MockPath.return_value.resolve.return_value.parents.__getitem__ = lambda s, i: Path(td).parent

            # Simpler approach: just call task() and check the id format
            for _ in range(100):
                result = json.loads(task(action="create", title=f"test-{_}"))
                if result["ok"]:
                    tid = result["task"]["task_id"]
                    self.assertTrue(tid.startswith("T-"), f"ID should start with T-: {tid}")
                    self.assertNotIn(tid, ids, f"Duplicate ID: {tid}")
                    ids.add(tid)

            self.assertEqual(len(ids), 100)

    def test_approval_id_format(self):
        """验证 approval ID 使用新格式。"""
        result = json.loads(approval(
            action="request",
            title="test-approval",
            target_agent="master",
            requester="agent_01",
        ))
        self.assertTrue(result["ok"])
        aid = result["approval"]["approval_id"]
        self.assertTrue(aid.startswith("A-"), f"ID should start with A-: {aid}")
        self.assertEqual(len(aid), 14)  # "A-" + 12 hex chars = 14


class TestHotReloadable(unittest.TestCase):
    """_make_hot_reloadable: 间接调用包装器。"""

    def test_wrapper_has_correct_metadata(self):
        for name in _HOT_RELOAD_TOOL_NAMES:
            wrapper = _make_hot_reloadable(name)
            self.assertEqual(wrapper.__name__, name)
            self.assertIsNotNone(wrapper.__doc__, f"{name} wrapper should have a docstring")
            self.assertIsNotNone(
                getattr(wrapper, "__signature__", None),
                f"{name} wrapper should have a __signature__",
            )

    def test_wrapper_delegates_to_current_globals(self):
        """包装器通过 globals() 调用，支持运行时替换。"""
        import agents.all_in_one as mod

        original_iterm = mod.iterm
        wrapper = _make_hot_reloadable("iterm")

        # 调用包装器应委托给 globals()["iterm"]
        result = json.loads(wrapper(action="launch"))
        self.assertFalse(result["ok"])
        self.assertEqual(result["error_code"], "iterm_launch_disabled")


class TestLockTool(unittest.TestCase):
    """lock 工具使用 _locked_json_rw。"""

    def test_acquire_release_cycle(self):
        r = json.loads(lock(action="acquire", resource="robustness_test_res", owner="test_agent", ttl_sec=60))
        self.assertTrue(r["ok"])

        r2 = json.loads(lock(action="release", resource="robustness_test_res", owner="test_agent"))
        self.assertTrue(r2["ok"])


class TestInteractionRegister(unittest.TestCase):
    """interaction register 使用 _locked_json_rw + _atomic_write_json。"""

    def test_register_succeeds(self):
        r = json.loads(interaction(action="register", sender="robustness_test_agent", content="Python,测试"))
        self.assertTrue(r["ok"])
        self.assertIn("Python", r["agent"]["skills"])


if __name__ == "__main__":
    unittest.main()
