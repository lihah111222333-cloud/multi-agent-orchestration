"""直接调用 all_in_one 工具函数进行复测（绕过 MCP bus）。

用法：cd multi-agent-orchestration && .venv/bin/python -m pytest tests/test_tools_direct.py -v
"""
from __future__ import annotations
import json, os, sys
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))

# 加载 .env
try:
    from dotenv import load_dotenv
    load_dotenv(os.path.join(os.path.dirname(os.path.dirname(__file__)), ".env"))
except ImportError:
    pass

from agents.all_in_one import (
    interaction, task, db, command_card, lock, approval, iterm,
)


class TestInteraction:
    def test_register_and_roster(self):
        # register
        r = json.loads(interaction(action="register", sender="test_agent_01",
                                   content="Python,测试,代码审查"))
        assert r["ok"] is True
        assert "skills" in r["agent"]
        assert "Python" in r["agent"]["skills"]

        # roster should include registered agent
        r2 = json.loads(interaction(action="roster"))
        assert r2["ok"] is True
        agents = r2.get("agents", [])
        found = [a for a in agents if a.get("agent_id") == "test_agent_01"]
        assert len(found) >= 1
        assert "Python" in found[0].get("skills", [])


class TestTask:
    def test_create_get_ready_progress_cancel(self):
        import time
        uid = str(int(time.time() * 1000))
        # create
        r = json.loads(task(action="create", title=f"测试任务_{uid}",
                           assignee="agent_01", project_id=f"test_proj_{uid}",
                           idempotency_key=f"idem_{uid}"))
        assert r["ok"] is True
        tid = r["task"]["task_id"]

        # idempotency: duplicate create returns existing
        r2 = json.loads(task(action="create", title="测试任务2",
                            idempotency_key=f"idem_{uid}"))
        assert r2["ok"] is True
        assert r2.get("duplicate") is True
        assert r2["task"]["task_id"] == tid

        # get
        r3 = json.loads(task(action="get", task_id=tid))
        assert r3["ok"] is True
        assert r3["task"]["title"] == f"测试任务_{uid}"

        # ready (no deps, should be ready)
        r4 = json.loads(task(action="ready", project_id=f"test_proj_{uid}"))
        assert r4["ok"] is True
        ready_ids = [t["task_id"] for t in r4["tasks"]]
        assert tid in ready_ids

        # progress
        r5 = json.loads(task(action="progress", project_id=f"test_proj_{uid}"))
        assert r5["ok"] is True
        assert "total" in r5

        # cancel
        r6 = json.loads(task(action="cancel", task_id=tid))
        assert r6["ok"] is True
        assert r6["task"]["status"] == "cancelled"


class TestLock:
    def test_acquire_release(self):
        # acquire
        r = json.loads(lock(action="acquire", resource="test_res_1",
                           owner="agent_01", ttl_sec=60))
        assert r["ok"] is True

        # same owner renew
        r2 = json.loads(lock(action="acquire", resource="test_res_1",
                            owner="agent_01"))
        assert r2["ok"] is True
        assert r2.get("renewed") is True

        # different owner blocked
        r3 = json.loads(lock(action="acquire", resource="test_res_1",
                            owner="agent_02"))
        assert r3["ok"] is False

        # release
        r4 = json.loads(lock(action="release", resource="test_res_1",
                            owner="agent_01"))
        assert r4["ok"] is True

        # list
        r5 = json.loads(lock(action="list"))
        assert r5["ok"] is True


class TestDB:
    def test_query_with_datetime(self):
        """db query with NOW() should not crash with datetime serialization error."""
        r = json.loads(db(action="query", sql="SELECT NOW() as ts"))
        assert r["ok"] is True
        assert r["count"] == 1
        # ts should be a string (ISO format), not crash
        assert isinstance(r["rows"][0]["ts"], str)


class TestCommandCard:
    def test_list_and_list_runs(self):
        r = json.loads(command_card(action="list"))
        assert r["ok"] is True
        assert "count" in r

        r2 = json.loads(command_card(action="list_runs"))
        assert r2["ok"] is True
        assert "count" in r2


class TestIterm:
    def test_launch_action_is_disabled(self):
        r = json.loads(iterm(action="launch"))
        assert r["ok"] is False
        assert r["error_code"] == "iterm_launch_disabled"
        assert "launch.wjboot.workspace" in r["error"]
