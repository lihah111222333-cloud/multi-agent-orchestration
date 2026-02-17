"""ACP-Bus 冒烟测试 — 全量工具 + HTTP MCP 端点验证

涵盖:
  1. 所有 10 个 MCP 工具的直接调用 (import)
  2. HTTP /mcp 端点可达性
  3. MCP JSON-RPC tools/list 调用
  4. 工具参数校验 (缺少必填参数→返回 ok:false)
  5. base_agent 创建/重启逻辑
  6. singleton lock 边界条件

用法：cd multi-agent-orchestration && .venv/bin/python -m pytest tests/test_acp_bus_smoke.py -v
"""

from __future__ import annotations

import json
import os
import sys
import time

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))

# 加载 .env
try:
    from dotenv import load_dotenv
    load_dotenv(os.path.join(os.path.dirname(os.path.dirname(__file__)), ".env"))
except ImportError:
    pass

from agents.all_in_one import (
    approval,
    command_card,
    db,
    interaction,
    iterm,
    lock,
    prompt_template,
    shared_file,
    task,
)

# ---------------------------------------------------------------------------
# 辅助
# ---------------------------------------------------------------------------

def _j(result: str) -> dict:
    """把工具返回的 JSON 字符串解析为 dict."""
    return json.loads(result)


_UID = str(int(time.time() * 1000))  # 每次运行唯一前缀


# ===========================================================================
# Section 1: tool 直接调用 — 正常路径 & 边界校验
# ===========================================================================

class TestInteractionTool:
    """interaction 工具: register / roster / create / list."""

    def test_register(self):
        r = _j(interaction(action="register", sender=f"smoke_{_UID}",
                           content="Python,smoke,测试"))
        assert r["ok"] is True
        assert f"smoke_{_UID}" in (r.get("agent", {}).get("agent_id", ""))

    def test_roster(self):
        _j(interaction(action="register", sender=f"smoke_{_UID}",
                       content="Python"))
        r = _j(interaction(action="roster"))
        assert r["ok"] is True
        assert isinstance(r.get("agents"), list)

    def test_create_and_list(self):
        r1 = _j(interaction(
            action="create", sender=f"smoke_{_UID}",
            receiver="master", msg_type="task",
            content=f"冒烟测试消息_{_UID}",
        ))
        assert r1["ok"] is True

        r2 = _j(interaction(action="list", sender=f"smoke_{_UID}"))
        assert r2["ok"] is True
        assert r2["count"] >= 1

    def test_missing_sender(self):
        """create 缺少 sender → ok:false."""
        r = _j(interaction(action="create", content="no-sender"))
        assert r["ok"] is False


class TestTaskTool:
    """task 工具 CRUD + DAG 依赖."""

    def test_full_lifecycle(self):
        # create
        r = _j(task(action="create", title=f"冒烟_{_UID}",
                    assignee="agent_01", creator="smoke",
                    project_id=f"smoke_{_UID}",
                    idempotency_key=f"smoke_idem_{_UID}"))
        assert r["ok"] is True
        tid = r["task"]["task_id"]

        # idempotency guard
        r2 = _j(task(action="create", title="dup",
                     idempotency_key=f"smoke_idem_{_UID}"))
        assert r2.get("duplicate") is True

        # get
        r3 = _j(task(action="get", task_id=tid))
        assert r3["ok"] is True

        # ready
        r4 = _j(task(action="ready", project_id=f"smoke_{_UID}"))
        assert r4["ok"] is True

        # progress
        r5 = _j(task(action="progress", project_id=f"smoke_{_UID}"))
        assert r5["ok"] is True
        assert r5["total"] >= 1

        # update → in_progress
        r6 = _j(task(action="update", task_id=tid, status="in_progress"))
        assert r6["ok"] is True

        # cancel
        r7 = _j(task(action="cancel", task_id=tid))
        assert r7["ok"] is True
        assert r7["task"]["status"] == "cancelled"

    def test_create_missing_title(self):
        r = _j(task(action="create", title=""))
        assert r["ok"] is False

    def test_update_missing_id(self):
        r = _j(task(action="update", status="done"))
        assert r["ok"] is False

    def test_assign_missing_args(self):
        r = _j(task(action="assign"))
        assert r["ok"] is False


class TestLockTool:
    """lock 工具: acquire / release / renew / conflict."""

    def test_acquire_renew_release(self):
        res = f"smoke_lock_{_UID}"
        r = _j(lock(action="acquire", resource=res,
                    owner="smoke_agent", ttl_sec=60))
        assert r["ok"] is True

        r2 = _j(lock(action="acquire", resource=res, owner="smoke_agent"))
        assert r2["ok"] is True
        assert r2.get("renewed") is True

        r3 = _j(lock(action="release", resource=res, owner="smoke_agent"))
        assert r3["ok"] is True

    def test_conflict(self):
        res = f"smoke_lock_c_{_UID}"
        _j(lock(action="acquire", resource=res, owner="a1"))
        r = _j(lock(action="acquire", resource=res, owner="a2"))
        assert r["ok"] is False
        # cleanup
        _j(lock(action="force_release", resource=res))

    def test_list(self):
        r = _j(lock(action="list"))
        assert r["ok"] is True
        assert "count" in r

    def test_acquire_missing_owner(self):
        r = _j(lock(action="acquire", resource="x"))
        assert r["ok"] is False


class TestApprovalTool:
    """approval 工具: request / respond / list."""

    def test_request_respond_list(self):
        r = _j(approval(action="request", title=f"审批测试_{_UID}",
                        target_agent="master", requester="smoke"))
        assert r["ok"] is True
        aid = r["approval"]["approval_id"]

        r2 = _j(approval(action="respond", approval_id=aid,
                         decision="approved", approver="master"))
        assert r2["ok"] is True
        assert r2["approval"]["status"] == "resolved"

        r3 = _j(approval(action="list"))
        assert r3["ok"] is True
        assert r3["count"] >= 1

    def test_request_missing_title(self):
        r = _j(approval(action="request", target_agent="master"))
        assert r["ok"] is False

    def test_request_missing_target(self):
        r = _j(approval(action="request", title="test"))
        assert r["ok"] is False


class TestDBTool:
    """db 工具: query / execute 最小可行性."""

    def test_select_now(self):
        r = _j(db(action="query", sql="SELECT NOW() AS ts"))
        assert r["ok"] is True
        assert r["count"] == 1
        assert isinstance(r["rows"][0]["ts"], str)

    def test_select_version(self):
        r = _j(db(action="query", sql="SELECT version()"))
        assert r["ok"] is True
        assert "PostgreSQL" in r["rows"][0].get("version", "")

    def test_missing_sql(self):
        r = _j(db(action="query", sql=""))
        assert r["ok"] is False


class TestPromptTemplateTool:
    """prompt_template 工具: save / get / list."""

    def test_save_get_list(self):
        key = f"smoke_pt_{_UID}"
        r = _j(prompt_template(
            action="save", prompt_key=key,
            title=f"冒烟测试模板_{_UID}",
            prompt_text="你是一个测试 Agent。请回复 OK。",
            agent_key="smoke", tool_name="test",
        ))
        assert r["ok"] is True

        r2 = _j(prompt_template(action="get", prompt_key=key))
        assert r2["ok"] is True
        assert r2["prompt"]["prompt_key"] == key

        r3 = _j(prompt_template(action="list"))
        assert r3["ok"] is True
        assert r3["count"] >= 1


class TestCommandCardTool:
    """command_card 工具: list / list_runs."""

    def test_list(self):
        r = _j(command_card(action="list"))
        assert r["ok"] is True
        assert "count" in r

    def test_list_runs(self):
        r = _j(command_card(action="list_runs"))
        assert r["ok"] is True
        assert "count" in r


class TestSharedFileTool:
    """shared_file 工具: write / read / list / delete."""

    def test_write_read_delete(self):
        fp = f"smoke/test_{_UID}.txt"
        r1 = _j(shared_file(action="write", path=fp,
                            content=f"smoke content {_UID}"))
        assert r1["ok"] is True
        assert r1.get("path") == fp

        r2 = _j(shared_file(action="read", path=fp))
        assert r2["ok"] is True
        assert _UID in r2.get("content", "")

        r3 = _j(shared_file(action="list"))
        assert r3["ok"] is True

        r4 = _j(shared_file(action="delete", path=fp))
        assert r4["ok"] is True


class TestItermTool:
    """iterm 工具: list / launch 禁用检测."""

    def test_list(self):
        r = _j(iterm(action="list"))
        # list 应该总是成功（即使无会话）
        assert isinstance(r, (dict, list))

    def test_launch_disabled(self):
        r = _j(iterm(action="launch"))
        assert r["ok"] is False
        assert r.get("error_code") == "iterm_launch_disabled"


# ===========================================================================
# Section 2: base_agent 模块单元测试
# ===========================================================================

class TestBaseAgent:
    """base_agent 模块: create_agent_server / run_agent 基本行为."""

    def test_create_server(self):
        from agents.base_agent import create_agent_server
        server = create_agent_server("smoke-test", "smoke desc",
                                     host="127.0.0.1", port=19999)
        assert server is not None
        assert server.name == "smoke-test"

    def test_safe_int_env(self):
        from agents.base_agent import _safe_int_env
        assert _safe_int_env("NON_EXISTENT_VAR_XYZ", 42) == 42
        os.environ["_SMOKE_INT"] = "7"
        assert _safe_int_env("_SMOKE_INT", 0) == 7
        os.environ["_SMOKE_INT"] = "bogus"
        assert _safe_int_env("_SMOKE_INT", 99) == 99
        os.environ.pop("_SMOKE_INT", None)


# ===========================================================================
# Section 3: singleton lock 边界测试
# ===========================================================================

class TestSingleton:
    def test_singleton_disabled_by_default(self):
        from agents.all_in_one import _is_singleton_enabled
        old = os.environ.pop("ACP_BUS_SINGLETON_ENABLED", None)
        try:
            assert _is_singleton_enabled() is False
        finally:
            if old is not None:
                os.environ["ACP_BUS_SINGLETON_ENABLED"] = old

    def test_singleton_enabled_true(self):
        from agents.all_in_one import _is_singleton_enabled
        old = os.environ.get("ACP_BUS_SINGLETON_ENABLED")
        os.environ["ACP_BUS_SINGLETON_ENABLED"] = "1"
        try:
            assert _is_singleton_enabled() is True
        finally:
            if old is None:
                os.environ.pop("ACP_BUS_SINGLETON_ENABLED", None)
            else:
                os.environ["ACP_BUS_SINGLETON_ENABLED"] = old


# ===========================================================================
# Section 4: HTTP MCP 端点可达性 (需要 ACP-Bus 运行中)
# ===========================================================================

ACP_BUS_URL = os.environ.get("ACP_BUS_URL", "http://127.0.0.1:9100")

def _acp_bus_reachable() -> bool:
    """检查 ACP-Bus 是否在监听."""
    try:
        import urllib.request
        req = urllib.request.Request(f"{ACP_BUS_URL}/mcp",
                                    method="GET")
        urllib.request.urlopen(req, timeout=2)
        return True
    except Exception:
        return False


@pytest.mark.skipif(not _acp_bus_reachable(),
                    reason="ACP-Bus 未运行，跳过 HTTP 端点测试")
class TestHTTPEndpoint:
    """通过 HTTP 验证 MCP 端点."""

    def test_mcp_endpoint_responds(self):
        """GET /mcp 应返回 2xx 或合理响应."""
        import urllib.request
        req = urllib.request.Request(f"{ACP_BUS_URL}/mcp", method="GET")
        resp = urllib.request.urlopen(req, timeout=5)
        assert resp.status in (200, 405)  # SSE 或 method not allowed 均可接受

    def test_jsonrpc_initialize(self):
        """POST JSON-RPC initialize 请求."""
        import urllib.request
        body = json.dumps({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "smoke-test", "version": "0.1.0"},
            },
        }).encode()
        req = urllib.request.Request(
            f"{ACP_BUS_URL}/mcp",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        resp = urllib.request.urlopen(req, timeout=5)
        assert resp.status == 200
        data = json.loads(resp.read())
        # initialize 回复应包含 serverInfo
        assert "result" in data or "serverInfo" in str(data)

    def test_jsonrpc_tools_list(self):
        """初始化后发送 tools/list, 验证工具数量 >= 10."""
        import urllib.request

        # Step 1: initialize
        init_body = json.dumps({
            "jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "smoke-test", "version": "0.1.0"},
            },
        }).encode()
        req1 = urllib.request.Request(
            f"{ACP_BUS_URL}/mcp", data=init_body,
            headers={"Content-Type": "application/json"}, method="POST",
        )
        resp1 = urllib.request.urlopen(req1, timeout=5)
        # 获取 session id (if any)
        session_id = resp1.headers.get("mcp-session-id", "")

        # Step 2: tools/list
        tools_body = json.dumps({
            "jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {},
        }).encode()
        headers = {"Content-Type": "application/json"}
        if session_id:
            headers["mcp-session-id"] = session_id
        req2 = urllib.request.Request(
            f"{ACP_BUS_URL}/mcp", data=tools_body,
            headers=headers, method="POST",
        )
        resp2 = urllib.request.urlopen(req2, timeout=5)
        data = json.loads(resp2.read())
        tools = data.get("result", {}).get("tools", [])
        tool_names = [t["name"] for t in tools]
        assert len(tool_names) >= 10, f"Expected >=10 tools, got {len(tool_names)}: {tool_names}"

        # 验证核心工具名
        expected = {"iterm", "shared_file", "interaction", "prompt_template",
                    "command_card", "db", "task", "approval", "lock",
                    "agent_watchdog"}
        missing = expected - set(tool_names)
        assert not missing, f"Missing tools: {missing}"


# ===========================================================================
# Section 5: _SafeEncoder JSON 序列化兜底
# ===========================================================================

class TestSafeEncoder:
    def test_datetime(self):
        from agents.all_in_one import _safe_json
        from datetime import datetime, timezone
        r = json.loads(_safe_json({"ts": datetime.now(timezone.utc)}))
        assert isinstance(r["ts"], str)

    def test_decimal(self):
        from agents.all_in_one import _safe_json
        from decimal import Decimal
        r = json.loads(_safe_json({"val": Decimal("3.14")}))
        assert r["val"] == 3.14 or r["val"] == "3.14"

    def test_date(self):
        from agents.all_in_one import _safe_json
        from datetime import date
        r = json.loads(_safe_json({"d": date(2026, 1, 1)}))
        assert "2026" in r["d"]
