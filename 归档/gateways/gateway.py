"""Gateway — MCP Client 路由层

每个 Gateway 管理一组 Agent (MCP Server)，负责：
1. 连接 Agent 的 MCP Server
2. 获取所有可用工具
3. 使用 LLM 选择合适的工具执行任务
4. 返回结构化结果（成功/失败、原因、重试次数）

P0 健壮性增强：
- 连接池复用（避免每次任务冷启动子进程）
- 依赖 DAG 顺序执行（基于 depends_on）
- 运行时自愈（心跳探活失败自动重建 runtime）
"""

from __future__ import annotations

import asyncio
import logging
import time
import traceback
from dataclasses import dataclass
from contextlib import suppress
from typing import Any, Callable, Optional

from config.settings import (
    GATEWAY_MAX_ATTEMPTS,
    GATEWAY_TIMEOUT,
    LLM_MAX_RETRIES,
    LLM_MODEL,
    LLM_TEMPERATURE,
    LLM_TIMEOUT,
    OPENAI_BASE_URL,
)
from utils import as_int_env, build_chat_openai, extract_text

logger = logging.getLogger(__name__)

__all__ = ["Gateway", "GatewayResult", "GatewayExecutionError"]


GATEWAY_POOL_IDLE_SEC = as_int_env("GATEWAY_POOL_IDLE_SEC", 120, min_value=5)
GATEWAY_HEALTHCHECK_SEC = as_int_env("GATEWAY_HEALTHCHECK_SEC", 20, min_value=5)


async def _finish_trace_span_async(
    span_id: str,
    status: str,
    output_payload: Optional[dict[str, Any]] = None,
    error_text: str = "",
    metadata: Optional[dict[str, Any]] = None,
) -> None:
    with suppress(Exception):
        from agent_ops_store import finish_task_trace_span

        finish_task_trace_span(
            span_id=span_id,
            status=status,
            output_payload=output_payload,
            error_text=error_text,
            metadata=metadata,
        )


class GatewayExecutionError(RuntimeError):
    def __init__(self, reason: str, message: str):
        super().__init__(message)
        self.reason = reason


@dataclass(frozen=True)
class GatewayResult:
    success: bool
    output: str = ""
    error: str = ""
    reason: str = "ok"
    attempts: int = 1
    runtime_restarts: int = 0

    def to_dict(self) -> dict[str, Any]:
        return {
            "success": self.success,
            "output": self.output,
            "error": self.error,
            "reason": self.reason,
            "attempts": self.attempts,
            "runtime_restarts": self.runtime_restarts,
        }


def _default_llm_factory() -> Any:
    return build_chat_openai(
        model=LLM_MODEL,
        temperature=LLM_TEMPERATURE,
        base_url=OPENAI_BASE_URL,
        max_retries=LLM_MAX_RETRIES,
        request_timeout=LLM_TIMEOUT,
    )


def _default_mcp_client_cls() -> type:
    from langchain_mcp_adapters.client import MultiServerMCPClient

    return MultiServerMCPClient


def _default_react_agent_builder(llm: Any, tools: list[Any]) -> Any:
    from langgraph.prebuilt import create_react_agent

    return create_react_agent(llm, tools)


class Gateway:
    """Gateway 管理一组 Agent，将任务路由到合适的 Agent 执行"""

    def __init__(
        self,
        name: str,
        display_name: str,
        agent_configs: dict,
        agent_meta: Optional[dict[str, dict[str, Any]]] = None,
        llm_factory: Optional[Callable[[], Any]] = None,
        mcp_client_cls: Optional[type] = None,
        react_agent_builder: Optional[Callable[[Any, list], Any]] = None,
        max_attempts: Optional[int] = None,
    ):
        self.name = name
        self.display_name = display_name
        self.agent_configs = agent_configs
        self.agent_meta = agent_meta or {}
        self.llm_factory = llm_factory or _default_llm_factory
        self.mcp_client_cls = mcp_client_cls or _default_mcp_client_cls()
        self.react_agent_builder = react_agent_builder or _default_react_agent_builder

        attempts = max_attempts if max_attempts is not None else GATEWAY_MAX_ATTEMPTS
        self.max_attempts = max(1, int(attempts))

        self._runtime_lock: Optional[asyncio.Lock] = None
        self._execute_lock: Optional[asyncio.Lock] = None
        self._loop: Optional[asyncio.AbstractEventLoop] = None

        self._client_ctx = None
        self._client = None
        self._tools: list[Any] = []
        self._llm: Optional[Any] = None

        self._last_used_ts = 0.0
        self._last_probe_ts = 0.0
        self._runtime_restarts = 0

    def _ensure_async_primitives(self) -> None:
        loop = asyncio.get_running_loop()
        if self._loop is loop and self._runtime_lock is not None and self._execute_lock is not None:
            return

        self._loop = loop
        self._runtime_lock = asyncio.Lock()
        self._execute_lock = asyncio.Lock()

    def _runtime_lock_obj(self) -> asyncio.Lock:
        if self._runtime_lock is None:
            raise RuntimeError("runtime lock is not initialized")
        return self._runtime_lock

    def _execute_lock_obj(self) -> asyncio.Lock:
        if self._execute_lock is None:
            raise RuntimeError("execute lock is not initialized")
        return self._execute_lock

    def _get_llm(self) -> Any:
        if self._llm is None:
            self._llm = self.llm_factory()
        return self._llm

    def _dependency_hint(self) -> str:
        if not self.agent_meta:
            return ""

        lines = []

        dependency_rows = []
        for agent_id, meta in self.agent_meta.items():
            deps = [str(item).strip() for item in meta.get("depends_on", []) if str(item).strip()]
            if deps:
                dependency_rows.append(f"- {agent_id} 依赖 {', '.join(deps)}")

        if dependency_rows:
            lines.append("请尽量遵循以下执行依赖顺序：")
            lines.extend(dependency_rows)

        capability_rows = []
        for agent_id, meta in self.agent_meta.items():
            caps = [str(item).strip() for item in meta.get("capabilities", []) if str(item).strip()]
            if caps:
                capability_rows.append(f"- {agent_id}: {', '.join(caps)}")

        if capability_rows:
            lines.append("可用能力提示：")
            lines.extend(capability_rows)

        return "\n".join(lines).strip()

    def _build_effective_task(self, task: str) -> str:
        hint = self._dependency_hint()
        if not hint:
            return task
        return f"{task}\n\n[网关执行约束]\n{hint}"

    def _topological_layers(self, nodes: list[str], depends_map: dict[str, list[str]]) -> list[list[str]]:
        in_degree: dict[str, int] = {node: 0 for node in nodes}
        graph: dict[str, list[str]] = {node: [] for node in nodes}

        for node in nodes:
            for dep in depends_map.get(node, []):
                if dep not in in_degree:
                    continue
                graph[dep].append(node)
                in_degree[node] += 1

        layers: list[list[str]] = []
        ready = [node for node, degree in in_degree.items() if degree == 0]

        visited = 0
        while ready:
            current_layer = sorted(ready)
            layers.append(current_layer)
            next_ready = []
            for node in current_layer:
                visited += 1
                for nxt in graph[node]:
                    in_degree[nxt] -= 1
                    if in_degree[nxt] == 0:
                        next_ready.append(nxt)
            ready = next_ready

        if visited != len(nodes):
            return []
        return layers

    def _extract_tool_agent_id(self, tool: Any) -> str:
        known_ids = list(self.agent_configs.keys())

        metadata = getattr(tool, "metadata", None)
        if isinstance(metadata, dict):
            for key in ("agent_id", "server_name", "mcp_server_name", "server"):
                value = metadata.get(key)
                if isinstance(value, str):
                    value = value.strip()
                    if value in known_ids:
                        return value

        name = str(getattr(tool, "name", "") or "")
        for agent_id in known_ids:
            if agent_id in name:
                return agent_id

        desc = str(getattr(tool, "description", "") or "")
        for agent_id in known_ids:
            if agent_id in desc:
                return agent_id

        return ""

    def _group_tools_by_agent(self, tools: list[Any]) -> dict[str, list[Any]]:
        grouped: dict[str, list[Any]] = {agent_id: [] for agent_id in self.agent_configs.keys()}
        unknown: list[Any] = []

        for tool in tools:
            agent_id = self._extract_tool_agent_id(tool)
            if agent_id and agent_id in grouped:
                grouped[agent_id].append(tool)
            else:
                unknown.append(tool)

        if unknown:
            logger.debug("[%s] %d tools 无法匹配到具体 agent，分发到所有 agent", self.display_name, len(unknown))
            for agent_id in grouped:
                grouped[agent_id].extend(unknown)

        return grouped

    def _build_execution_messages(self, task: str) -> list[dict[str, str]]:
        hint = self._dependency_hint()

        system_parts = [
            "你是网关执行代理。请优先调用提供的工具完成任务，并输出可执行结论。",
            "如工具输出冲突，请标注冲突并给出保守建议。",
        ]
        if hint:
            system_parts.append(f"必须遵循以下执行约束（高优先级）：\n{hint}")

        system_text = "\n\n".join(system_parts)
        return [
            {"role": "system", "content": system_text},
            {"role": "user", "content": str(task or "")},
        ]

    def _extract_message_text(self, message: Any) -> str:
        if message is None:
            return ""

        if isinstance(message, dict):
            content = message.get("content")
        else:
            content = getattr(message, "content", None)

        return extract_text(content).strip()

    def _extract_output_from_messages(self, messages: list[Any]) -> str:
        """Prefer the latest non-empty message text.

        Some providers/tools return an empty final assistant content while
        the meaningful text is in an earlier message in the same trace.
        """
        for message in reversed(messages):
            text = self._extract_message_text(message)
            if text:
                return text
        return ""

    def _apply_responses_runtime_fallback(self, error_text: str) -> bool:
        """对 Responses API 常见兼容错误做保守降级。

        目标是避免第三方网关在 tool-calling 场景下因状态项不持久化导致整次失败。
        """
        llm = self._llm
        if llm is None:
            return False

        text = str(error_text or "").lower()
        changed = False

        not_found_item = "items are not persisted when `store` is set to false" in text
        missing_tool_call = "no tool call found for function call output with call_id" in text

        if (not_found_item or missing_tool_call) and bool(getattr(llm, "use_previous_response_id", False)):
            try:
                llm.use_previous_response_id = False
                changed = True
                logger.warning("[%s] 已临时关闭 use_previous_response_id 以兼容当前网关", self.display_name)
            except Exception:
                logger.debug("关闭 use_previous_response_id 失败", exc_info=True)

        if not_found_item and hasattr(llm, "store") and getattr(llm, "store", None) is not True:
            try:
                llm.store = True
                changed = True
                logger.warning("[%s] 已临时启用 Responses store 以重试", self.display_name)
            except Exception:
                logger.debug("开启 store 失败", exc_info=True)

        return changed

    async def _invoke_with_tools(self, task: str, tools: list[Any]) -> str:
        if not tools:
            raise GatewayExecutionError("no_tools", "当前阶段无可用工具")

        llm = self._get_llm()
        agent = self.react_agent_builder(llm, tools)
        messages = self._build_execution_messages(task)
        result = await agent.ainvoke({"messages": messages})

        output_messages = result.get("messages", []) if isinstance(result, dict) else []
        if not output_messages:
            raise GatewayExecutionError("empty_response", "LLM 返回为空")

        output = self._extract_output_from_messages(output_messages)
        if not output.strip():
            raise GatewayExecutionError("empty_output", "LLM 返回空文本")
        return output

    def _select_execution_plan(self, tools: list[Any]) -> list[list[Any]]:
        if not self.agent_meta:
            return [tools]

        nodes = list(self.agent_configs.keys())
        depends_map: dict[str, list[str]] = {}
        has_dep = False
        for agent_id in nodes:
            deps = [
                str(item).strip()
                for item in self.agent_meta.get(agent_id, {}).get("depends_on", [])
                if str(item).strip() in self.agent_configs
            ]
            if deps:
                has_dep = True
            depends_map[agent_id] = deps

        if not has_dep:
            return [tools]

        layers = self._topological_layers(nodes, depends_map)
        if not layers:
            logger.warning("[%s] agent depends_on 存在环路，回退单阶段执行", self.display_name)
            return [tools]

        grouped = self._group_tools_by_agent(tools)
        planned: list[list[Any]] = []
        for layer in layers:
            stage_tools: list[Any] = []
            for agent_id in layer:
                stage_tools.extend(grouped.get(agent_id, []))
            if stage_tools:
                planned.append(stage_tools)

        return planned or [tools]

    async def _open_runtime_locked(self) -> None:
        if self._client is not None:
            return

        client = self.mcp_client_cls(self.agent_configs)
        try:
            # langchain-mcp-adapters>=0.1.0 推荐直接实例化后调用 get_tools。
            tools = await client.get_tools()
        except Exception:
            # D9: 连接失败时清理半初始化的 runtime，确保下次重试
            try:
                await self._close_client_like(client)
            except Exception:
                logger.debug("清理半初始化 client 异常", exc_info=True)
            raise

        self._client_ctx = None
        self._client = client
        self._tools = tools
        self._last_probe_ts = time.monotonic()
        self._last_used_ts = time.monotonic()

        if not self._tools:
            await self._close_runtime_locked(reason="no_tools_after_open")
            raise GatewayExecutionError("no_tools", f"未能从 {len(self.agent_configs)} 个 Agent 加载任何工具")

        logger.info(
            "[%s] Runtime 已建立: tools=%s agents=%s",
            self.display_name,
            len(self._tools),
            len(self.agent_configs),
        )

    async def _close_client_like(self, client: Any) -> None:
        if client is None:
            return

        aclose = getattr(client, "aclose", None)
        if callable(aclose):
            result = aclose()
            if asyncio.iscoroutine(result):
                await result
            return

        close = getattr(client, "close", None)
        if callable(close):
            result = close()
            if asyncio.iscoroutine(result):
                await result
            return

        aexit = getattr(client, "__aexit__", None)
        if callable(aexit):
            await aexit(None, None, None)

    async def _close_runtime_locked(self, reason: str) -> None:
        if self._client_ctx is not None:
            try:
                await self._client_ctx.__aexit__(None, None, None)
            except Exception:
                logger.debug("Runtime 关闭异常", exc_info=True)
        elif self._client is not None:
            try:
                await self._close_client_like(self._client)
            except Exception:
                logger.debug("Runtime 关闭异常", exc_info=True)

        self._client_ctx = None
        self._client = None
        self._tools = []
        self._last_probe_ts = 0.0
        self._last_used_ts = 0.0

        if reason:
            logger.info("[%s] Runtime 已关闭: reason=%s", self.display_name, reason)

    async def _refresh_runtime_locked(self, reason: str) -> None:
        self._runtime_restarts += 1
        await self._close_runtime_locked(reason=f"restart:{reason}")
        await self._open_runtime_locked()

    async def _ensure_runtime_locked(self) -> None:
        now = time.monotonic()
        if self._client is None:
            await self._open_runtime_locked()
            return

        if self._last_used_ts and (now - self._last_used_ts) > GATEWAY_POOL_IDLE_SEC:
            await self._close_runtime_locked(reason="idle_timeout")
            await self._open_runtime_locked()
            return

        if (now - self._last_probe_ts) >= GATEWAY_HEALTHCHECK_SEC:
            try:
                tools = await self._client.get_tools()
                if not tools:
                    raise RuntimeError("probe returned 0 tools")
                self._tools = tools
                self._last_probe_ts = now
            except Exception as e:
                logger.warning("[%s] Runtime 探活失败，准备重建: %s", self.display_name, e)
                await self._refresh_runtime_locked(reason="probe_failed")

    async def _execute_with_plan(self, task: str) -> str:
        effective_task = self._build_effective_task(task)

        async with self._runtime_lock_obj():
            await self._ensure_runtime_locked()
            tools = list(self._tools)

        plan = self._select_execution_plan(tools)

        if len(plan) == 1:
            output = await self._invoke_with_tools(effective_task, plan[0])
            async with self._runtime_lock_obj():
                self._last_used_ts = time.monotonic()
            return output

        stage_outputs = []
        for index, stage_tools in enumerate(plan, start=1):
            stage_prompt = f"{effective_task}\n\n[阶段执行 {index}/{len(plan)}]"
            if stage_outputs:
                previous = "\n\n".join(stage_outputs[-2:])
                stage_prompt += f"\n\n[上阶段结果]\n{previous}"

            stage_output = await self._invoke_with_tools(stage_prompt, stage_tools)
            stage_outputs.append(f"阶段{index}: {stage_output}")

        async with self._runtime_lock_obj():
            self._last_used_ts = time.monotonic()

        return "\n\n".join(stage_outputs)

    async def _do_process(self, task: str) -> str:
        return await self._execute_with_plan(task)

    async def process(self, task: str) -> dict[str, Any]:
        """处理任务（带超时、重试、自愈保护）"""

        self._ensure_async_primitives()

        parent_trace_id = ""
        parent_span_id = ""
        if isinstance(task, str):
            text = task.strip()
            if text.startswith("["):
                header, sep, rest = text.partition("\n")
                if sep and header.startswith("[") and header.endswith("]"):
                    raw_pairs = header[1:-1]
                    for pair in raw_pairs.split(";"):
                        if "=" not in pair:
                            continue
                        key, value = pair.split("=", 1)
                        key = key.strip().lower()
                        value = value.strip()
                        if key == "trace_id":
                            parent_trace_id = value
                        elif key == "parent_span_id":
                            parent_span_id = value
                    task = rest

        span = None
        if parent_trace_id:
            with suppress(Exception):
                from agent_ops_store import start_task_trace_span

                span = start_task_trace_span(
                    trace_id=parent_trace_id,
                    parent_span_id=parent_span_id,
                    span_name="gateway.process",
                    component=f"gateway:{self.name}",
                    input_payload={"task": str(task or "")[:1000]},
                    metadata={"display_name": self.display_name, "max_attempts": self.max_attempts},
                )

        async with self._execute_lock_obj():
            last_error = ""
            last_reason = "unknown"

            for attempt in range(1, self.max_attempts + 1):
                try:
                    output = await asyncio.wait_for(self._do_process(task), timeout=GATEWAY_TIMEOUT)
                    if span:
                        await _finish_trace_span_async(
                            span_id=str(span.get("span_id", "")),
                            status="ok",
                            output_payload={"output": output[:4000]},
                            metadata={"attempt": attempt, "runtime_restarts": self._runtime_restarts},
                        )
                    return GatewayResult(
                        success=True,
                        output=output,
                        reason="ok",
                        attempts=attempt,
                        runtime_restarts=self._runtime_restarts,
                    ).to_dict()
                except asyncio.TimeoutError:
                    last_reason = "timeout"
                    last_error = f"[{self.display_name}] 执行超时 ({GATEWAY_TIMEOUT}s)"
                except GatewayExecutionError as e:
                    last_reason = e.reason
                    last_error = f"[{self.display_name}] 任务执行失败: {e}"
                    self._apply_responses_runtime_fallback(str(e))
                except Exception as e:
                    last_reason = "exception"
                    last_error = f"[{self.display_name}] 执行异常: {e}"
                    self._apply_responses_runtime_fallback(str(e))

                logger.error(last_error)
                logger.debug(traceback.format_exc())

                try:
                    async with self._runtime_lock_obj():
                        await self._refresh_runtime_locked(reason=last_reason)
                except Exception as refresh_error:
                    logger.warning("[%s] Runtime 重建失败: %s", self.display_name, refresh_error)

                if attempt < self.max_attempts:
                    logger.warning(
                        "[%s] 尝试 %s/%s 失败，已触发 runtime 重建",
                        self.display_name,
                        attempt,
                        self.max_attempts,
                    )

            if span:
                await _finish_trace_span_async(
                    span_id=str(span.get("span_id", "")),
                    status="error",
                    error_text=last_error,
                    metadata={"reason": last_reason, "runtime_restarts": self._runtime_restarts},
                )

            return GatewayResult(
                success=False,
                error=last_error,
                reason=last_reason,
                attempts=self.max_attempts,
                runtime_restarts=self._runtime_restarts,
            ).to_dict()

    async def close(self) -> None:
        self._ensure_async_primitives()
        async with self._runtime_lock_obj():
            await self._close_runtime_locked(reason="explicit_close")

    def __repr__(self) -> str:
        return (
            f"Gateway(name={self.name}, display={self.display_name}, "
            f"agents={list(self.agent_configs.keys())})"
        )
