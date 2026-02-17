#!/usr/bin/env python3
"""
E2E Test: LSP Dynamic Tool Invocation
======================================
验证 agent 在执行 Go 项目检查任务时是否真正调用 LSP 工具。

测试流程:
  1. 启动 app-server (go build + spawn)
  2. WebSocket 连接 → initialize
  3. thread/start → 创建 agent 线程
  4. 等待 MCP startup complete
  5. turn/start → 发送"检查 Go 文件的 LSP 诊断"任务
  6. 监听 lsp/tool/called 通知
  7. 断言至少收到一条 lsp/tool/called 通知

成功标准: 至少收到 1 条 lsp/tool/called 通知 (agent 实际调用了 LSP 工具)
"""

import json
import asyncio
import subprocess
import sys
import os
import signal
import time

try:
    import websockets
except ImportError:
    subprocess.check_call([sys.executable, "-m", "pip", "install", "websockets", "-q"])
    import websockets


# ── 配置 ──
APP_SERVER_BIN = None  # 会在 build 阶段填充
WS_URI = "ws://127.0.0.1:4501"  # 用 4501 避免冲突
WS_HOST = "127.0.0.1:4501"
GO_PROJECT_DIR = os.path.expanduser(
    "~/Desktop/wj/multi-agent-orchestration/go-agent-v2"
)
GO_MODULE_DIR = os.path.expanduser(
    "~/Desktop/wj/multi-agent-orchestration/go-agent-v2"
)
# 选一个真实 Go 文件让 agent 检查
TARGET_FILE = "internal/apiserver/server.go"

TIMEOUT_MCP_STARTUP = 60  # 秒
TIMEOUT_TURN = 120  # 秒 (agent 需要时间思考 + 调用工具)


class TestResult:
    def __init__(self):
        self.passed = False
        self.lsp_calls = []  # 收到的 lsp/tool/called 通知
        self.errors = []
        self.events = []  # 所有事件


# ── 步骤 1: 构建 app-server ──
def build_server():
    print("🔨 [1/6] Building app-server...")
    bin_path = "/tmp/test-app-server-lsp"
    result = subprocess.run(
        ["go", "build", "-o", bin_path, "./cmd/app-server/"],
        cwd=GO_MODULE_DIR,
        capture_output=True,
        text=True,
        timeout=120,
    )
    if result.returncode != 0:
        print(f"❌ Build failed:\n{result.stderr}")
        sys.exit(1)
    print("   ✅ Build OK")
    return bin_path


# ── 步骤 2: 启动 app-server ──
def start_server(bin_path):
    print(f"🚀 [2/6] Starting app-server on {WS_HOST}...")
    env = os.environ.copy()
    env["NO_PROXY"] = "*"
    env["no_proxy"] = "*"
    for k in ["ALL_PROXY", "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"]:
        env.pop(k, None)

    proc = subprocess.Popen(
        [bin_path, "--listen", WS_URI],
        cwd=GO_PROJECT_DIR,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        env=env,
    )
    # 等服务器就绪
    time.sleep(3)
    if proc.poll() is not None:
        out = proc.stdout.read().decode() if proc.stdout else ""
        print(f"❌ app-server exited early (code={proc.returncode}):\n{out}")
        sys.exit(1)
    print("   ✅ Server running (pid={0})".format(proc.pid))
    return proc


# ── 步骤 3-6: WebSocket 测试 ──
async def run_test():
    test = TestResult()

    env = os.environ.copy()
    env["NO_PROXY"] = "*"
    env["no_proxy"] = "*"

    # WebSocket 连接 (带重试)
    ws = None
    for attempt in range(5):
        try:
            ws = await websockets.connect(WS_URI, ping_interval=None)
            break
        except (ConnectionRefusedError, OSError) as e:
            print(f"   Retry {attempt+1}/5: {e}")
            await asyncio.sleep(1)
    if ws is None:
        test.errors.append("Cannot connect to server after 5 retries")
        return test

    try:
        # 3. initialize
        print("🔗 [3/6] Connecting + initialize...")
        await ws.send(json.dumps({
            "jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {"clientInfo": {"name": "e2e-lsp-test", "version": "1.0"}}
        }))
        resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        assert resp.get("id") == 1, f"Bad init response: {resp}"
        print("   ✅ Initialized")

        # 4. thread/start
        print("🧵 [4/6] Creating thread...")
        await ws.send(json.dumps({
            "jsonrpc": "2.0", "id": 2, "method": "thread/start",
            "params": {
                "model": "",
                "cwd": GO_PROJECT_DIR,
            }
        }))

        thread_id = None
        mcp_ready = False
        try:
            while True:
                msg = json.loads(await asyncio.wait_for(ws.recv(), timeout=TIMEOUT_MCP_STARTUP))
                test.events.append(msg)

                if msg.get("id") == 2:
                    thread_id = msg.get("result", {}).get("thread", {}).get("id", "")
                    print(f"   ✅ Thread: {thread_id}")

                method = msg.get("method", "")
                if method == "agent/event/mcp_startup_complete":
                    mcp_ready = True
                    print("   ✅ MCP startup complete")
                    break

                # Also break on turn/completed if agent auto-responds
                if method in ("turn/completed", "agent/event/turn_completed"):
                    mcp_ready = True
                    break

        except asyncio.TimeoutError:
            print("   ⚠️ MCP startup timeout, proceeding anyway")

        if not thread_id:
            test.errors.append("No thread ID received")
            return test

        # 5. turn/start — 让 agent 用 LSP 检查 Go 文件
        print("📤 [5/6] Sending coding task to trigger LSP...")
        prompt = (
            f"I need you to use your lsp_open_file tool to open the file {TARGET_FILE}, "
            f"then use lsp_diagnostics to check for errors. "
            f"Please call these tools now."
        )
        await ws.send(json.dumps({
            "jsonrpc": "2.0", "id": 3, "method": "turn/start",
            "params": {
                "threadId": thread_id,
                "input": [
                    {"type": "text", "text": prompt}
                ],
            }
        }))
        print(f"   Prompt: {prompt[:80]}...")

        # 6. 监听 — 等待 lsp/tool/called 或 turn 完成
        print("👂 [6/6] Listening for lsp/tool/called notifications...")
        turn_done = False
        try:
            while True:
                msg = json.loads(await asyncio.wait_for(ws.recv(), timeout=TIMEOUT_TURN))
                test.events.append(msg)
                method = msg.get("method", "")
                params = msg.get("params", {})

                # ★ 关键: 捕获 LSP 工具调用通知
                if method == "lsp/tool/called":
                    test.lsp_calls.append(params)
                    tool = params.get("tool", "?")
                    elapsed = params.get("elapsedMs", "?")
                    total = params.get("totalCalls", "?")
                    print(f"   🔧 LSP CALL: {tool} ({elapsed}ms) #{total}")

                # Agent 消息 delta (简化输出)
                if method in ("agent/event/agent_message_content_delta", "item/agentMessage/delta"):
                    delta = params.get("delta", "")
                    if delta:
                        sys.stdout.write(delta)
                        sys.stdout.flush()

                # Dynamic tool call event from agent
                if method == "agent/event/dynamic_tool_call":
                    tool_name = params.get("name", params.get("tool", "?"))
                    print(f"\n   📞 Agent calling dynamic tool: {tool_name}")

                if method in ("turn/completed", "agent/event/turn_completed"):
                    turn_done = True
                    print(f"\n   ✅ Turn completed")
                    break

        except asyncio.TimeoutError:
            print(f"\n   ⏱️ Timeout after {TIMEOUT_TURN}s")

    finally:
        await ws.close()

    # ── 判定结果 ──
    if len(test.lsp_calls) > 0:
        test.passed = True
    else:
        test.errors.append("No lsp/tool/called notifications received")

    return test


def main():
    # Build
    bin_path = build_server()

    # Start server
    server_proc = start_server(bin_path)

    try:
        # Run test
        test = asyncio.run(run_test())

        # Report
        print("\n" + "=" * 60)
        print("E2E TEST RESULTS")
        print("=" * 60)
        print(f"LSP tool calls received: {len(test.lsp_calls)}")
        for c in test.lsp_calls:
            print(f"  - {c.get('tool')}: {c.get('elapsedMs')}ms (#{c.get('totalCalls')})")

        if test.errors:
            print(f"Errors: {test.errors}")

        print(f"Total events: {len(test.events)}")

        if test.passed:
            print("\n🎉 PASS — Agent successfully called LSP tools!")
            return 0
        else:
            print("\n❌ FAIL — Agent did NOT call any LSP tools")
            print("  Possible reasons:")
            print("  - Agent chose not to use LSP tools for this task")
            print("  - Dynamic tools were not injected correctly")
            print("  - Turn timed out before agent could call tools")
            return 1

    finally:
        # Cleanup: kill server
        print("\n🧹 Cleaning up...")
        server_proc.send_signal(signal.SIGINT)
        try:
            server_proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server_proc.kill()
        print("   Server stopped")


if __name__ == "__main__":
    sys.exit(main())
