import inspect
import unittest
from unittest import mock

from agents.factory import AgentSpec, ToolParam, ToolSpec, build_tool_callable, collect_tool_specs, get_agent_spec_by_key


class AgentFactoryTests(unittest.TestCase):
    def test_build_tool_callable_keeps_signature_defaults(self):
        spec = ToolSpec(
            name="sample_tool",
            description="sample tool",
            params=(
                ToolParam("symbol", str),
                ToolParam("top_k", int, 3),
            ),
            response_template="symbol={symbol}, top_k={top_k}",
        )

        fn = build_tool_callable(spec)
        sig = inspect.signature(fn)

        self.assertEqual(list(sig.parameters.keys()), ["symbol", "top_k"])
        self.assertEqual(sig.parameters["top_k"].default, 3)
        self.assertEqual(fn("BTC"), "symbol=BTC, top_k=3")

    def test_unknown_agent_key_falls_back_to_dynamic_spec(self):
        agent_spec = get_agent_spec_by_key("agent_99", agent_name="动态代理")
        tools = {tool.name: build_tool_callable(tool) for tool in agent_spec.tools}

        self.assertIn("execute_task", tools)
        self.assertIn("report_status", tools)

        result_1 = tools["execute_task"]("整理日报", "p0")
        result_2 = tools["report_status"]("ready")

        self.assertEqual(result_1, "[agent_99] 已处理任务: 整理日报 | context=p0")
        self.assertEqual(result_2, "[agent_99] 状态: ready")

    def test_build_tool_callable_uses_agent_thread_runtime(self):
        spec = ToolSpec(
            name="threaded_tool",
            description="threaded tool",
            params=(ToolParam("task", str),),
            response_template="done={task}",
        )

        fn = build_tool_callable(spec)

        with mock.patch("agents.factory.run_in_agent_thread", side_effect=lambda callable_fn: callable_fn()) as mocked:
            output = fn("sync")

        self.assertEqual(output, "done=sync")
        mocked.assert_called_once()

    def test_collect_tool_specs_merges_declared_plugins(self):
        spec = AgentSpec(
            key="agent_test",
            server_name="agent-test",
            description="for test",
            tools=(
                ToolSpec(
                    name="base_tool",
                    description="base",
                    params=(ToolParam("x", str),),
                    response_template="{x}",
                ),
            ),
            plugins=("http_fetch", "db_query"),
        )

        tools = collect_tool_specs(spec)
        tool_names = [tool.name for tool in tools]

        self.assertEqual(tool_names, ["base_tool", "http_fetch", "db_query"])

    def test_collect_tool_specs_rejects_duplicated_tool_name(self):
        spec = AgentSpec(
            key="agent_test",
            server_name="agent-test",
            description="for test",
            tools=(
                ToolSpec(
                    name="http_fetch",
                    description="base",
                    params=(ToolParam("url", str),),
                    response_template="{url}",
                ),
            ),
            plugins=("http_fetch",),
        )

        with self.assertRaisesRegex(ValueError, "插件工具名重复"):
            collect_tool_specs(spec)

    def test_get_agent_spec_by_key_accepts_plugin_names(self):
        agent_spec = get_agent_spec_by_key("agent_99", agent_name="动态代理", plugin_names=("http_fetch",))

        self.assertEqual(agent_spec.plugins, ("http_fetch",))


if __name__ == "__main__":
    unittest.main()
