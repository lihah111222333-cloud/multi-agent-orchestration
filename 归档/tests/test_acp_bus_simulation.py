#!/usr/bin/env python3
"""ACP Bus 全量模拟测试 — 通过 MCP HTTP 端点逐一调用所有工具，检测超时/错误/数据一致性。

用法:
    python3 tests/test_acp_bus_simulation.py [--base-url http://127.0.0.1:9100]

测试覆盖:
  1. interaction  (create / list / review / roster / register)
  2. shared_file  (write / read / list / delete)
  3. prompt_template (save / get / list / toggle)
  4. command_card (save / get / list / toggle)
  5. task         (create / list / get / update / assign / ready / progress / cancel)
  6. approval     (request / list / decide)
  7. lock         (acquire / status / release / list)
  8. db           (query / execute)
  9. orchestration_tui (begin / update / snapshot / end)
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid
from dataclasses import dataclass, field
from typing import Any

import requests

# ── 常量 ──────────────────────────────────────────────
DEFAULT_BASE_URL = "http://127.0.0.1:9100"
MCP_PATH = "/mcp"
TOOL_TIMEOUT = 30  # 单个工具调用超时秒数
INIT_TIMEOUT = 10  # 初始化超时秒数
WARN_THRESHOLD = 5.0  # 超过此秒数就报慢

# ── 结果收集 ─────────────────────────────────────────
@dataclass
class CallResult:
    tool: str
    action: str
    ok: bool
    elapsed: float
    error: str = ""
    detail: str = ""

@dataclass
class TestReport:
    results: list[CallResult] = field(default_factory=list)
    passed: int = 0
    failed: int = 0
    slow: int = 0

    def add(self, r: CallResult) -> None:
        self.results.append(r)
        if r.ok:
            self.passed += 1
        else:
            self.failed += 1
        if r.elapsed > WARN_THRESHOLD:
            self.slow += 1

    def summary(self) -> str:
        lines = [
            "",
            "=" * 70,
            f"  总计: {len(self.results)}  通过: {self.passed}  失败: {self.failed}  慢(>{WARN_THRESHOLD}s): {self.slow}",
            "=" * 70,
        ]
        if self.failed:
            lines.append("\n❌ 失败项:")
            for r in self.results:
                if not r.ok:
                    lines.append(f"  [{r.tool}.{r.action}] {r.elapsed:.2f}s — {r.error}")
        if self.slow:
            lines.append(f"\n🐢 慢调用(>{WARN_THRESHOLD}s):")
            for r in self.results:
                if r.elapsed > WARN_THRESHOLD:
                    tag = "FAIL" if not r.ok else "OK"
                    lines.append(f"  [{r.tool}.{r.action}] {r.elapsed:.2f}s [{tag}]")
        lines.append("")
        return "\n".join(lines)


# ── MCP 客户端 ────────────────────────────────────────
class MCPClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")
        self.endpoint = self.base_url + MCP_PATH
        self.session_id: str = ""
        self._req_id = 0
        self.http = requests.Session()

    def _next_id(self) -> int:
        self._req_id += 1
        return self._req_id

    def initialize(self) -> bool:
        """MCP 初始化握手"""
        try:
            resp = self.http.post(
                self.endpoint,
                json={
                    "jsonrpc": "2.0",
                    "id": self._next_id(),
                    "method": "initialize",
                    "params": {
                        "protocolVersion": "2024-11-05",
                        "capabilities": {},
                        "clientInfo": {"name": "sim-test", "version": "1.0"},
                    },
                },
                headers={
                    "Content-Type": "application/json",
                    "Accept": "application/json, text/event-stream",
                },
                timeout=INIT_TIMEOUT,
            )
            sid = resp.headers.get("Mcp-Session-Id", "")
            if sid:
                self.session_id = sid
                return True
            # 尝试从 body 获取
            return resp.status_code == 200
        except Exception as e:
            print(f"[FATAL] 初始化失败: {e}", file=sys.stderr)
            return False

    def call_tool(self, tool_name: str, arguments: dict[str, Any]) -> tuple[float, dict]:
        """调用 MCP 工具，返回 (elapsed_sec, response_body)"""
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        }
        if self.session_id:
            headers["Mcp-Session-Id"] = self.session_id

        payload = {
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": "tools/call",
            "params": {"name": tool_name, "arguments": arguments},
        }

        t0 = time.monotonic()
        try:
            resp = self.http.post(
                self.endpoint,
                json=payload,
                headers=headers,
                timeout=TOOL_TIMEOUT,
            )
            elapsed = time.monotonic() - t0

            # 解析 SSE 或直接 JSON
            ct = resp.headers.get("Content-Type", "")
            if "text/event-stream" in ct:
                # 解析 SSE 流中的最终 JSON
                body = _parse_sse_response(resp.text)
            else:
                body = resp.json()
            return elapsed, body

        except requests.Timeout:
            elapsed = time.monotonic() - t0
            return elapsed, {"error": f"HTTP timeout ({TOOL_TIMEOUT}s)"}
        except Exception as e:
            elapsed = time.monotonic() - t0
            return elapsed, {"error": str(e)}


def _parse_sse_response(text: str) -> dict:
    """从 SSE 流中提取 JSON-RPC 响应"""
    for line in text.strip().split("\n"):
        line = line.strip()
        if line.startswith("data:"):
            data_str = line[5:].strip()
            if data_str:
                try:
                    return json.loads(data_str)
                except json.JSONDecodeError:
                    continue
    # fallback: 尝试整体解析
    try:
        return json.loads(text)
    except Exception:
        return {"error": "无法解析 SSE 响应", "raw": text[:500]}


# ── 测试辅助 ─────────────────────────────────────────
def _check_result(report: TestReport, tool: str, action: str, elapsed: float, body: dict) -> Any:
    """检查工具调用结果，记录到 report，返回解析后的 tool content"""
    # JSON-RPC error
    if "error" in body and "result" not in body:
        report.add(CallResult(tool, action, False, elapsed, error=str(body["error"])))
        return None

    # 提取 MCP result.content
    result = body.get("result", {})
    content_list = result.get("content", [])
    if not content_list:
        report.add(CallResult(tool, action, False, elapsed, error="空 content"))
        return None

    text = content_list[0].get("text", "")
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError:
        parsed = {"raw": text}

    ok = parsed.get("ok", False) if isinstance(parsed, dict) else False
    if ok:
        report.add(CallResult(tool, action, True, elapsed))
    else:
        err = parsed.get("error", str(parsed)[:200]) if isinstance(parsed, dict) else text[:200]
        report.add(CallResult(tool, action, False, elapsed, error=str(err)))
    return parsed


def _log(emoji: str, tool: str, action: str, elapsed: float, extra: str = ""):
    slow = " 🐢" if elapsed > WARN_THRESHOLD else ""
    print(f"  {emoji} {tool}.{action}  {elapsed:.2f}s{slow}  {extra}")


# ── 各模块测试 ────────────────────────────────────────

def test_interaction(client: MCPClient, report: TestReport):
    print("\n── interaction ──")
    uid = uuid.uuid4().hex[:8]

    # create
    elapsed, body = client.call_tool("interaction", {
        "action": "create",
        "sender": f"sim_agent_{uid}",
        "receiver": "orchestrator",
        "msg_type": "task",
        "content": f"simulation test message {uid}",
        "thread_id": f"sim-thread-{uid}",
    })
    parsed = _check_result(report, "interaction", "create", elapsed, body)
    _log("📝", "interaction", "create", elapsed, f"id={parsed.get('interaction', {}).get('id', '?')}" if parsed else "")

    # list
    elapsed, body = client.call_tool("interaction", {
        "action": "list",
        "thread_id": f"sim-thread-{uid}",
        "limit": 10,
    })
    parsed = _check_result(report, "interaction", "list", elapsed, body)
    _log("📋", "interaction", "list", elapsed, f"count={parsed.get('count', '?')}" if parsed else "")

    # roster
    elapsed, body = client.call_tool("interaction", {"action": "roster"})
    parsed = _check_result(report, "interaction", "roster", elapsed, body)
    _log("👥", "interaction", "roster", elapsed, f"agents={parsed.get('count', '?')}" if parsed else "")

    # register
    elapsed, body = client.call_tool("interaction", {
        "action": "register",
        "sender": f"sim_agent_{uid}",
        "content": "Python,测试,模拟",
    })
    parsed = _check_result(report, "interaction", "register", elapsed, body)
    _log("📋", "interaction", "register", elapsed)


def test_shared_file(client: MCPClient, report: TestReport):
    print("\n── shared_file ──")
    uid = uuid.uuid4().hex[:8]
    fpath = f"sim-test/test_{uid}.txt"

    # write
    elapsed, body = client.call_tool("shared_file", {
        "action": "write",
        "path": fpath,
        "content": f"hello simulation {uid}",
    })
    parsed = _check_result(report, "shared_file", "write", elapsed, body)
    _log("✏️", "shared_file", "write", elapsed)

    # read
    elapsed, body = client.call_tool("shared_file", {
        "action": "read",
        "path": fpath,
    })
    parsed = _check_result(report, "shared_file", "read", elapsed, body)
    _log("📖", "shared_file", "read", elapsed)

    # list
    elapsed, body = client.call_tool("shared_file", {
        "action": "list",
        "path": "sim-test/",
        "limit": 10,
    })
    parsed = _check_result(report, "shared_file", "list", elapsed, body)
    _log("📋", "shared_file", "list", elapsed, f"count={parsed.get('count', '?')}" if parsed else "")

    # delete
    elapsed, body = client.call_tool("shared_file", {
        "action": "delete",
        "path": fpath,
    })
    parsed = _check_result(report, "shared_file", "delete", elapsed, body)
    _log("🗑️", "shared_file", "delete", elapsed)


def test_prompt_template(client: MCPClient, report: TestReport):
    print("\n── prompt_template ──")
    uid = uuid.uuid4().hex[:8]
    pkey = f"sim_prompt_{uid}"

    # save
    elapsed, body = client.call_tool("prompt_template", {
        "action": "save",
        "prompt_key": pkey,
        "title": f"Sim Prompt {uid}",
        "prompt_text": "你是一个测试 Agent。请回答：{{question}}",
        "agent_key": "sim_agent",
        "variables_json": json.dumps({"question": "string"}),
    })
    parsed = _check_result(report, "prompt_template", "save", elapsed, body)
    _log("💾", "prompt_template", "save", elapsed)

    # get
    elapsed, body = client.call_tool("prompt_template", {
        "action": "get",
        "prompt_key": pkey,
    })
    parsed = _check_result(report, "prompt_template", "get", elapsed, body)
    _log("📖", "prompt_template", "get", elapsed)

    # list
    elapsed, body = client.call_tool("prompt_template", {
        "action": "list",
        "keyword": "Sim Prompt",
        "limit": 10,
    })
    parsed = _check_result(report, "prompt_template", "list", elapsed, body)
    _log("📋", "prompt_template", "list", elapsed, f"count={parsed.get('count', '?')}" if parsed else "")

    # toggle (disable)
    elapsed, body = client.call_tool("prompt_template", {
        "action": "toggle",
        "prompt_key": pkey,
        "enabled": False,
    })
    parsed = _check_result(report, "prompt_template", "toggle", elapsed, body)
    _log("🔀", "prompt_template", "toggle", elapsed)


def test_command_card(client: MCPClient, report: TestReport):
    print("\n── command_card ──")
    uid = uuid.uuid4().hex[:8]
    ckey = f"sim.card.{uid}"

    # save
    elapsed, body = client.call_tool("command_card", {
        "action": "save",
        "card_key": ckey,
        "title": f"Sim Card {uid}",
        "command_template": "echo 'hello {{name}}'",
        "description": "模拟测试命令卡",
        "risk_level": "low",
    })
    parsed = _check_result(report, "command_card", "save", elapsed, body)
    _log("💾", "command_card", "save", elapsed)

    # get
    elapsed, body = client.call_tool("command_card", {
        "action": "get",
        "card_key": ckey,
    })
    parsed = _check_result(report, "command_card", "get", elapsed, body)
    _log("📖", "command_card", "get", elapsed)

    # list
    elapsed, body = client.call_tool("command_card", {
        "action": "list",
        "keyword": "Sim Card",
        "limit": 10,
    })
    parsed = _check_result(report, "command_card", "list", elapsed, body)
    _log("📋", "command_card", "list", elapsed, f"count={parsed.get('count', '?')}" if parsed else "")

    # toggle
    elapsed, body = client.call_tool("command_card", {
        "action": "toggle",
        "card_key": ckey,
        "enabled": False,
    })
    parsed = _check_result(report, "command_card", "toggle", elapsed, body)
    _log("🔀", "command_card", "toggle", elapsed)


def test_task(client: MCPClient, report: TestReport):
    print("\n── task ──")
    uid = uuid.uuid4().hex[:8]

    # create
    elapsed, body = client.call_tool("task", {
        "action": "create",
        "title": f"Sim Task {uid}",
        "description": "模拟测试任务",
        "assignee": "sim_agent",
        "creator": "sim_test",
        "priority": "normal",
        "project_id": f"sim-proj-{uid}",
    })
    parsed = _check_result(report, "task", "create", elapsed, body)
    task_id = parsed.get("task", {}).get("task_id", "") if parsed else ""
    _log("📝", "task", "create", elapsed, f"task_id={task_id}")

    if not task_id:
        return

    # list
    elapsed, body = client.call_tool("task", {
        "action": "list",
        "project_id": f"sim-proj-{uid}",
    })
    parsed = _check_result(report, "task", "list", elapsed, body)
    _log("📋", "task", "list", elapsed)

    # get
    elapsed, body = client.call_tool("task", {
        "action": "get",
        "task_id": task_id,
    })
    parsed = _check_result(report, "task", "get", elapsed, body)
    _log("📖", "task", "get", elapsed)

    # update
    elapsed, body = client.call_tool("task", {
        "action": "update",
        "task_id": task_id,
        "status": "in_progress",
        "result": "正在模拟执行",
    })
    parsed = _check_result(report, "task", "update", elapsed, body)
    _log("🔄", "task", "update", elapsed)

    # ready
    elapsed, body = client.call_tool("task", {
        "action": "ready",
        "project_id": f"sim-proj-{uid}",
    })
    parsed = _check_result(report, "task", "ready", elapsed, body)
    _log("✅", "task", "ready", elapsed)

    # progress
    elapsed, body = client.call_tool("task", {
        "action": "progress",
        "project_id": f"sim-proj-{uid}",
    })
    parsed = _check_result(report, "task", "progress", elapsed, body)
    _log("📊", "task", "progress", elapsed)

    # cancel
    elapsed, body = client.call_tool("task", {
        "action": "cancel",
        "task_id": task_id,
    })
    parsed = _check_result(report, "task", "cancel", elapsed, body)
    _log("❌", "task", "cancel", elapsed)


def test_approval(client: MCPClient, report: TestReport):
    print("\n── approval ──")
    uid = uuid.uuid4().hex[:8]

    # request
    elapsed, body = client.call_tool("approval", {
        "action": "request",
        "requester": f"sim_agent_{uid}",
        "target_agent": "orchestrator",
        "title": f"Sim Approval {uid}",
        "description": "需要审批的模拟操作",
        "options_json": json.dumps(["approve", "reject", "defer"]),
    })
    parsed = _check_result(report, "approval", "request", elapsed, body)
    approval_id = ""
    if parsed and isinstance(parsed.get("approval"), dict):
        approval_id = str(parsed["approval"].get("id", ""))
    _log("🔔", "approval", "request", elapsed, f"id={approval_id}")

    # list
    elapsed, body = client.call_tool("approval", {
        "action": "list",
        "status": "pending",
        "limit": 10,
    })
    parsed = _check_result(report, "approval", "list", elapsed, body)
    _log("📋", "approval", "list", elapsed, f"count={parsed.get('count', '?')}" if parsed else "")

    # decide
    if approval_id:
        elapsed, body = client.call_tool("approval", {
            "action": "decide",
            "interaction_id": int(approval_id) if approval_id.isdigit() else 0,
            "decision": "approved",
            "approver": "sim_test",
            "reason": "自动审批测试",
        })
        parsed = _check_result(report, "approval", "decide", elapsed, body)
        _log("✅", "approval", "decide", elapsed)


def test_lock(client: MCPClient, report: TestReport):
    print("\n── lock ──")
    uid = uuid.uuid4().hex[:8]
    resource = f"sim-resource-{uid}"

    # acquire
    elapsed, body = client.call_tool("lock", {
        "action": "acquire",
        "resource": resource,
        "owner": f"sim_agent_{uid}",
        "ttl_sec": 60,
    })
    parsed = _check_result(report, "lock", "acquire", elapsed, body)
    _log("🔒", "lock", "acquire", elapsed)

    # status
    elapsed, body = client.call_tool("lock", {
        "action": "status",
        "resource": resource,
    })
    parsed = _check_result(report, "lock", "status", elapsed, body)
    _log("📊", "lock", "status", elapsed)

    # list
    elapsed, body = client.call_tool("lock", {
        "action": "list",
    })
    parsed = _check_result(report, "lock", "list", elapsed, body)
    _log("📋", "lock", "list", elapsed)

    # release
    elapsed, body = client.call_tool("lock", {
        "action": "release",
        "resource": resource,
        "owner": f"sim_agent_{uid}",
    })
    parsed = _check_result(report, "lock", "release", elapsed, body)
    _log("🔓", "lock", "release", elapsed)


def test_db(client: MCPClient, report: TestReport):
    print("\n── db ──")

    # query
    elapsed, body = client.call_tool("db", {
        "action": "query",
        "sql": "SELECT 1 AS health_check",
        "limit": 1,
    })
    parsed = _check_result(report, "db", "query", elapsed, body)
    _log("🔍", "db", "query", elapsed, f"rows={parsed.get('count', '?')}" if parsed else "")


def test_orchestration_tui(client: MCPClient, report: TestReport):
    print("\n── orchestration_tui ──")
    uid = uuid.uuid4().hex[:8]
    run_id = f"sim-run-{uid}"

    # begin
    elapsed, body = client.call_tool("orchestration_tui", {
        "action": "begin",
        "run_id": run_id,
        "status_header": "模拟任务启动",
        "status_details": "正在初始化...",
        "source": "sim-test",
    })
    parsed = _check_result(report, "orchestration_tui", "begin", elapsed, body)
    _log("▶️", "orchestration_tui", "begin", elapsed)

    # update
    elapsed, body = client.call_tool("orchestration_tui", {
        "action": "update",
        "run_id": run_id,
        "status_header": "模拟任务执行中",
        "status_details": "进度 50%",
        "source": "sim-test",
    })
    parsed = _check_result(report, "orchestration_tui", "update", elapsed, body)
    _log("🔄", "orchestration_tui", "update", elapsed)

    # snapshot
    elapsed, body = client.call_tool("orchestration_tui", {
        "action": "snapshot",
    })
    parsed = _check_result(report, "orchestration_tui", "snapshot", elapsed, body)
    _log("📸", "orchestration_tui", "snapshot", elapsed)

    # end
    elapsed, body = client.call_tool("orchestration_tui", {
        "action": "end",
        "run_id": run_id,
        "status_header": "模拟任务完成",
        "source": "sim-test",
    })
    parsed = _check_result(report, "orchestration_tui", "end", elapsed, body)
    _log("⏹️", "orchestration_tui", "end", elapsed)


# ── 主函数 ────────────────────────────────────────────
def main():
    parser = argparse.ArgumentParser(description="ACP Bus 全量模拟测试")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    args = parser.parse_args()

    print(f"\n🚀 ACP Bus 模拟测试  {args.base_url}")
    print("=" * 70)

    client = MCPClient(args.base_url)
    if not client.initialize():
        print("❌ MCP 初始化失败，退出", file=sys.stderr)
        sys.exit(1)
    print(f"✅ MCP 初始化成功  session={client.session_id[:16]}...")

    report = TestReport()

    # 按模块逐一测试
    test_fns = [
        test_db,               # 先测 DB 连通性
        test_interaction,
        test_shared_file,
        test_prompt_template,
        test_command_card,
        test_task,
        test_approval,
        test_lock,
        test_orchestration_tui,
    ]

    for fn in test_fns:
        try:
            fn(client, report)
        except Exception as e:
            name = fn.__name__.replace("test_", "")
            print(f"\n  💥 {name} 模块异常: {e}")
            report.add(CallResult(name, "module_error", False, 0, error=str(e)))

    print(report.summary())

    # 输出 JSON 详细结果
    results_json = [
        {
            "tool": r.tool,
            "action": r.action,
            "ok": r.ok,
            "elapsed": round(r.elapsed, 3),
            "error": r.error,
        }
        for r in report.results
    ]
    print(json.dumps({"summary": {"total": len(report.results), "passed": report.passed,
                                   "failed": report.failed, "slow": report.slow},
                       "results": results_json}, ensure_ascii=False, indent=2))

    sys.exit(1 if report.failed else 0)


if __name__ == "__main__":
    main()
