import asyncio
import unittest

from gateways.gateway import Gateway, GatewayExecutionError


class _DummyClient:
    def __init__(self, _agent_configs):
        self.agent_configs = _agent_configs

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, tb):
        return False

    async def get_tools(self):
        return ["tool_a"]


class _DummyMessage:
    def __init__(self, content):
        self.content = content


class _DummyAgent:
    def __init__(self, content):
        self.content = content
        self.last_payload = None

    async def ainvoke(self, payload):
        self.last_payload = payload
        return {"messages": [_DummyMessage(self.content)]}


class GatewayTests(unittest.TestCase):
    def test_process_success_returns_structured_result(self):
        gateway = Gateway(
            name="gateway_1",
            display_name="网关1",
            agent_configs={"agent_1": {}},
            llm_factory=lambda: object(),
            mcp_client_cls=_DummyClient,
            react_agent_builder=lambda llm, tools: _DummyAgent("done"),
            max_attempts=2,
        )

        result = asyncio.run(gateway.process("task"))

        self.assertTrue(result["success"])
        self.assertEqual(result["output"], "done")
        self.assertEqual(result["reason"], "ok")
        self.assertEqual(result["attempts"], 1)

    def test_process_retries_and_recovers(self):
        class RetryGateway(Gateway):
            def __init__(self):
                super().__init__(
                    name="gateway_1",
                    display_name="网关1",
                    agent_configs={"agent_1": {}},
                    llm_factory=lambda: object(),
                    mcp_client_cls=_DummyClient,
                    react_agent_builder=lambda llm, tools: _DummyAgent("unused"),
                    max_attempts=2,
                )
                self.calls = 0

            async def _do_process(self, _task: str) -> str:
                self.calls += 1
                if self.calls == 1:
                    raise GatewayExecutionError("temporary", "first failed")
                return "recovered"

        gateway = RetryGateway()
        result = asyncio.run(gateway.process("task"))

        self.assertTrue(result["success"])
        self.assertEqual(result["output"], "recovered")
        self.assertEqual(result["attempts"], 2)

    def test_build_effective_task_includes_dependency_hint(self):
        gateway = Gateway(
            name="gateway_1",
            display_name="网关1",
            agent_configs={"agent_1": {}},
            agent_meta={
                "agent_1": {
                    "capabilities": ["collect"],
                    "depends_on": [],
                },
                "agent_2": {
                    "capabilities": ["clean"],
                    "depends_on": ["agent_1"],
                },
            },
            llm_factory=lambda: object(),
            mcp_client_cls=_DummyClient,
            react_agent_builder=lambda llm, tools: _DummyAgent("done"),
            max_attempts=1,
        )

        text = gateway._build_effective_task("原始任务")

        self.assertIn("原始任务", text)
        self.assertIn("agent_2 依赖 agent_1", text)
        self.assertIn("collect", text)

    def test_invoke_with_tools_adds_system_constraints_message(self):
        capture_agent = _DummyAgent("done")
        gateway = Gateway(
            name="gateway_1",
            display_name="网关1",
            agent_configs={"agent_1": {}, "agent_2": {}},
            agent_meta={
                "agent_1": {"capabilities": ["collect"], "depends_on": []},
                "agent_2": {"capabilities": ["clean"], "depends_on": ["agent_1"]},
            },
            llm_factory=lambda: object(),
            mcp_client_cls=_DummyClient,
            react_agent_builder=lambda llm, tools: capture_agent,
            max_attempts=1,
        )

        output = asyncio.run(gateway._invoke_with_tools("原始任务", ["tool_a"]))

        self.assertEqual(output, "done")
        payload = capture_agent.last_payload
        self.assertIsInstance(payload, dict)
        messages = payload.get("messages", [])
        self.assertEqual(messages[0]["role"], "system")
        self.assertIn("执行约束", messages[0]["content"])
        self.assertIn("agent_2 依赖 agent_1", messages[0]["content"])
        self.assertEqual(messages[1]["role"], "user")
        self.assertEqual(messages[1]["content"], "原始任务")

    def test_process_failure_after_retries(self):
        class AlwaysFailGateway(Gateway):
            def __init__(self):
                super().__init__(
                    name="gateway_1",
                    display_name="网关1",
                    agent_configs={"agent_1": {}},
                    llm_factory=lambda: object(),
                    mcp_client_cls=_DummyClient,
                    react_agent_builder=lambda llm, tools: _DummyAgent("unused"),
                    max_attempts=2,
                )

            async def _do_process(self, _task: str) -> str:
                raise GatewayExecutionError("temporary", "still failed")

        gateway = AlwaysFailGateway()
        result = asyncio.run(gateway.process("task"))

        self.assertFalse(result["success"])
        self.assertEqual(result["reason"], "temporary")
        self.assertEqual(result["attempts"], 2)


if __name__ == "__main__":
    unittest.main()
