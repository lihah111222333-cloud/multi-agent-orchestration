import asyncio
import unittest

from langchain_core.messages import HumanMessage, SystemMessage

import master


class _FakeResponse:
    def __init__(self, content: str):
        self.content = content


class _FakeLLM:
    def __init__(self, content: str):
        self._content = content
        self.last_prompt = None

    async def ainvoke(self, prompt):
        self.last_prompt = prompt
        return _FakeResponse(self._content)


class MasterTests(unittest.TestCase):
    def test_parse_assignments_supports_markdown_formats(self):
        gateways = {"gateway_1": {}, "gateway_2": {}}
        text = """
```text
- gateway_1|旧任务
```
- `gateway_1|子任务A`
> 1) gateway_2:|子任务B
"""

        assignments = master._parse_assignments(text, gateways)

        self.assertEqual(assignments["gateway_1"], "子任务A")
        self.assertEqual(assignments["gateway_2"], "子任务B")

    def test_fallback_assignments_marks_degraded_hint(self):
        assignments = master._fallback_assignments("原始任务", {"gateway_1": {}, "gateway_2": {}})

        self.assertIn("降级模式", assignments["gateway_1"])
        self.assertIn("降级模式", assignments["gateway_2"])

    def test_extract_json_uses_balanced_braces(self):
        text = '前缀 {"gateways": [{"id": "g1", "name": "n", "agents": [{"id": "a1"}]}]} 后缀 {not_json}'
        data = master._extract_json(text)

        self.assertIsInstance(data, dict)
        self.assertIn("gateways", data)

    def test_extract_json_skips_broken_braces_and_handles_string_braces(self):
        text = (
            "前缀 {oops] 无效段 "
            '{"meta":"brace { in string }","gateways":[{"id":"g1","name":"n","agents":[{"id":"a1"}]}]} '
            "后缀"
        )
        data = master._extract_json(text)

        self.assertIsInstance(data, dict)
        self.assertEqual(data["gateways"][0]["id"], "g1")

    def test_extract_json_returns_first_complete_object(self):
        text = (
            '说明 {"gateways":[{"id":"g1","agents":[{"id":"a1"}],"meta":{"ok":true}}]} '
            '{"gateways":[{"id":"g2","agents":[{"id":"a2"}]}]}'
        )

        data = master._extract_json(text)

        self.assertIsInstance(data, dict)
        self.assertEqual(data["gateways"][0]["id"], "g1")

    def test_topology_messages_use_system_user_roles(self):
        messages = master._build_topology_messages(
            "任务文本",
            {"gateways": [{"id": "gateway_1", "agents": [{"id": "agent_1"}]}]},
        )

        self.assertEqual(len(messages), 2)
        self.assertIsInstance(messages[0], SystemMessage)
        self.assertIsInstance(messages[1], HumanMessage)
        self.assertIn("<TASK>", messages[1].content)
        self.assertIn("<ARCH>", messages[1].content)

    def test_topology_prompt_trims_architecture_snapshot(self):
        old_limit = master.PROMPT_ARCH_MAX_CHARS
        master.PROMPT_ARCH_MAX_CHARS = 120
        try:
            huge_arch = {
                "gateways": [
                    {
                        "id": "gateway_1",
                        "name": "Gateway 1",
                        "agents": [{"id": f"agent_{idx:02d}"} for idx in range(40)],
                    }
                ]
            }
            prompt = master._build_topology_prompt("任务", huge_arch)
        finally:
            master.PROMPT_ARCH_MAX_CHARS = old_limit

        self.assertIn("拓扑快照已截断", prompt)

    def test_score_output_quality_penalizes_repeated_content(self):
        repeated = "\n".join(["同一句重复" for _ in range(12)])
        diverse = "\n".join(
            [
                "模块A: 指标正常",
                "模块B: 延迟上升",
                "模块C: 发现错误栈",
                "模块D: 建议限流",
                "模块E: 建议扩容",
                "模块F: 风险可控",
                "模块G: 继续监控",
                "模块H: 已回滚异常变更",
                "模块I: 需要补充压测",
                "模块J: 建议优化缓存",
            ]
        )

        repeated_score = master._score_output_quality(repeated)
        diverse_score = master._score_output_quality(diverse)

        self.assertLess(repeated_score, diverse_score)

    def test_dispatcher_supports_injected_llm(self):
        gateways = {"gateway_1": {}, "gateway_2": {}}
        gateway_agent_map = {
            "gateway_1": {"name": "网关1", "description": ""},
            "gateway_2": {"name": "网关2", "description": ""},
        }
        fake_llm = _FakeLLM("gateway_1|任务A\ngateway_2|任务B")

        dispatcher = master._make_dispatcher(
            gateway_agent_map=gateway_agent_map,
            gateways=gateways,
            current_architecture={"gateways": []},
            llm_factory=lambda: fake_llm,
            topology_hash="sha256:test",
        )

        old_flag = master.TOPOLOGY_PROPOSAL_ENABLED
        master.TOPOLOGY_PROPOSAL_ENABLED = False
        try:
            result = asyncio.run(dispatcher({"task": "test"}))
        finally:
            master.TOPOLOGY_PROPOSAL_ENABLED = old_flag

        self.assertFalse(result["dispatch_degraded"])
        self.assertEqual(result["topology_hash"], "sha256:test")
        self.assertEqual(result["gateway_assignments"]["gateway_1"], "任务A")
        self.assertEqual(result["gateway_assignments"]["gateway_2"], "任务B")
        self.assertIsInstance(fake_llm.last_prompt[0], SystemMessage)
        self.assertIsInstance(fake_llm.last_prompt[1], HumanMessage)

    def test_aggregator_returns_failure_summary_without_llm(self):
        called = {"count": 0}

        def llm_factory():
            called["count"] += 1
            raise AssertionError("should not build llm when all gateways failed")

        node = master._make_aggregator(llm_factory)
        state = {
            "results": [
                {
                    "gateway": "gateway_1",
                    "name": "网关1",
                    "success": False,
                    "output": "",
                    "error": "timeout",
                    "reason": "timeout",
                    "attempts": 2,
                }
            ]
        }

        result = asyncio.run(node(state))

        self.assertEqual(called["count"], 0)
        self.assertIn("所有网关执行失败", result["final_answer"])

    def test_aggregator_summary_is_truncated_when_too_long(self):
        old_limit = master.AGGREGATOR_MAX_WORDS
        master.AGGREGATOR_MAX_WORDS = 6
        try:
            node = master._make_aggregator(lambda: _FakeLLM("one two three four five six seven eight"))
            state = {
                "results": [
                    {
                        "gateway": "gateway_1",
                        "name": "网关1",
                        "success": True,
                        "output": "有效输出",
                        "error": "",
                        "reason": "ok",
                        "attempts": 1,
                        "quality_score": 100,
                    }
                ]
            }

            result = asyncio.run(node(state))
        finally:
            master.AGGREGATOR_MAX_WORDS = old_limit

        self.assertIn("内容已截断", result["final_answer"])
        self.assertNotIn("eight", result["final_answer"])

    def test_aggregator_prompt_contains_hard_800_char_constraint(self):
        old_limit = master.AGGREGATOR_MAX_WORDS
        master.AGGREGATOR_MAX_WORDS = 1200
        fake_llm = _FakeLLM("简短总结")
        try:
            node = master._make_aggregator(lambda: fake_llm)
            state = {
                "results": [
                    {
                        "gateway": "gateway_1",
                        "name": "网关1",
                        "success": True,
                        "output": "有效输出",
                        "error": "",
                        "reason": "ok",
                        "attempts": 1,
                        "quality_score": 100,
                    }
                ]
            }

            asyncio.run(node(state))
        finally:
            master.AGGREGATOR_MAX_WORDS = old_limit

        self.assertIsNotNone(fake_llm.last_prompt)
        self.assertIn("<=800字", fake_llm.last_prompt[0][1])
        self.assertIn("<=800字", fake_llm.last_prompt[1][1])


if __name__ == "__main__":
    unittest.main()
