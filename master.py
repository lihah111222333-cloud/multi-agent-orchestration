"""Master 编排器 — LangGraph StateGraph

流程: dispatcher → [dynamic gateways] → aggregator
- 运行拓扑来自任务启动时的快照
- Master 可自动提出新拓扑草案（需人工审批后生效）
"""

from __future__ import annotations

import asyncio
import json
import logging
import operator
import os
import re
import subprocess
import sys
import traceback
from contextlib import suppress
from pathlib import Path
from typing import Annotated, Any, Callable, Coroutine, Optional, TypedDict

from langchain_core.messages import HumanMessage, SystemMessage

from audit_log import append_event
from config.settings import (
    LLM_MAX_RETRIES,
    LLM_MODEL,
    LLM_TEMPERATURE,
    LLM_TIMEOUT,
    OPENAI_BASE_URL,
    load_architecture_snapshot,
)
from gateways.gateway import Gateway
from topology_approval import create_approval
from utils import as_int_env, build_chat_openai, extract_text

logger = logging.getLogger(__name__)

__all__ = ["MasterState", "build_graph", "aggregator"]


TOPOLOGY_PROPOSAL_ENABLED = os.getenv("TOPOLOGY_PROPOSAL_ENABLED", "1") == "1"
GATEWAY_MIN_QUALITY_SCORE = as_int_env("GATEWAY_MIN_QUALITY_SCORE", 25, min_value=0)
PROMPT_TASK_MAX_CHARS = as_int_env("PROMPT_TASK_MAX_CHARS", 2000, min_value=200)
PROMPT_ARCH_MAX_CHARS = as_int_env("PROMPT_ARCH_MAX_CHARS", 6000, min_value=500)
AGGREGATOR_MAX_WORDS = as_int_env("AGGREGATOR_MAX_WORDS", 800, min_value=200)
_ASSIGNMENT_LIST_PREFIX_RE = re.compile(r"^\s*(?:[-*+]|(?:\d+)[\.)])\s*")
_SUMMARY_UNIT_RE = re.compile(r"[A-Za-z0-9_]+|[\u4e00-\u9fff]")
_LOW_SIGNAL_OUTPUT_RE = re.compile(r"^\s*\[[^\]]+\]\s*已处理任务[:：]")
_PYTEST_SUMMARY_RE = re.compile(
    r"(\d+\s+passed(?:,\s*\d+\s+warnings?)?.*|no tests ran.*|FAILED.*|ERROR.*)",
    re.IGNORECASE,
)
_CODECHECK_KEYWORDS = (
    "代码检查",
    "code check",
    "code review",
    "审查代码",
    "静态检查",
    "测试覆盖",
    "lint",
    "review",
)


def _start_trace_span(
    state: MasterState,
    span_name: str,
    component: str,
    input_payload: Optional[dict[str, Any]] = None,
    metadata: Optional[dict[str, Any]] = None,
) -> Optional[dict[str, Any]]:
    trace_id = str(state.get("trace_id", "")).strip()
    if not trace_id:
        return None

    with suppress(Exception):
        from agent_ops_store import start_task_trace_span

        return start_task_trace_span(
            trace_id=trace_id,
            parent_span_id=str(state.get("root_span_id", "")).strip(),
            span_name=span_name,
            component=component,
            input_payload=input_payload,
            metadata=metadata,
        )
    return None


def _finish_trace_span(
    span: Optional[dict[str, Any]],
    status: str,
    output_payload: Optional[dict[str, Any]] = None,
    error_text: str = "",
    metadata: Optional[dict[str, Any]] = None,
) -> None:
    if not span:
        return
    span_id = str(span.get("span_id", "")).strip()
    if not span_id:
        return

    with suppress(Exception):
        from agent_ops_store import finish_task_trace_span

        finish_task_trace_span(
            span_id=span_id,
            status=status,
            output_payload=output_payload,
            error_text=error_text,
            metadata=metadata,
        )


def _create_llm() -> Any:
    return build_chat_openai(
        model=LLM_MODEL,
        temperature=LLM_TEMPERATURE,
        base_url=OPENAI_BASE_URL,
        max_retries=LLM_MAX_RETRIES,
        request_timeout=LLM_TIMEOUT,
    )


class MasterState(TypedDict):
    task: str
    gateway_assignments: dict
    results: Annotated[list, operator.add]
    final_answer: str
    dispatch_degraded: bool
    topology_hash: str
    trace_id: str
    root_span_id: str


def _create_gateways(gateway_agent_map: dict, gateway_cls: type = Gateway) -> dict:
    gateways = {}
    for gw_name, gw_config in gateway_agent_map.items():
        gateways[gw_name] = gateway_cls(
            name=gw_name,
            display_name=gw_config["name"],
            agent_configs=gw_config["agents"],
            agent_meta=gw_config.get("agent_meta", {}),
        )
    return gateways


def _gateway_prompt_brief(gw_id: str, gw: dict) -> str:
    desc = gw.get("description", "")
    caps = [str(item) for item in gw.get("capabilities", []) if str(item).strip()]
    cap_text = ", ".join(caps[:8]) if caps else "未声明"

    agents = gw.get("agent_meta", {})
    dependency_rows = []
    for agent_id, meta in agents.items():
        deps = [str(item).strip() for item in meta.get("depends_on", []) if str(item).strip()]
        if deps:
            dependency_rows.append(f"{agent_id}->{'+'.join(deps)}")
    dep_text = "; ".join(dependency_rows[:6]) if dependency_rows else "无"

    return f"- {gw_id}: {gw['name']} ({desc}) | capabilities={cap_text} | depends={dep_text}"


def _trim_task_text(task: str, max_chars: int = PROMPT_TASK_MAX_CHARS) -> str:
    text = str(task or "").strip()
    if len(text) <= max_chars:
        return text
    return text[:max_chars] + "\n...(任务文本已截断)"


def _build_dispatcher_prompt(task: str, gateway_agent_map: dict) -> str:
    lines = [f"任务: {_trim_task_text(task)}", ""]
    lines.append("可用网关:")

    for gw_id, gw in gateway_agent_map.items():
        lines.append(_gateway_prompt_brief(gw_id, gw))

    return "\n".join(lines)


def _build_dispatcher_messages(task: str, gateway_agent_map: dict) -> list[Any]:
    system_text = (
        "你是任务分配器。"
        "必须将任务拆分给网关，并仅输出多行 `gateway_id|子任务`。"
        "不要输出解释、代码块或额外标点。"
        "优先按网关能力拆分，避免重复结论；如有依赖提示，应体现上下游顺序。"
    )
    user_text = _build_dispatcher_prompt(task, gateway_agent_map)
    return [SystemMessage(content=system_text), HumanMessage(content=user_text)]


def _trim_architecture_text(current_architecture: dict, max_chars: Optional[int] = None) -> str:
    limit = PROMPT_ARCH_MAX_CHARS if max_chars is None else int(max_chars)
    normalized = json.dumps(current_architecture, ensure_ascii=False)
    if len(normalized) <= limit:
        return normalized
    return normalized[:limit] + "\n...(拓扑快照已截断)"


def _build_topology_prompt(task: str, current_architecture: dict) -> str:
    task_text = _trim_task_text(task)
    architecture_text = _trim_architecture_text(current_architecture)

    return (
        "任务描述（用户输入）:\n"
        "<TASK>\n"
        f"{task_text}\n"
        "</TASK>\n\n"
        "当前拓扑快照:\n"
        "<ARCH>\n"
        f"{architecture_text}\n"
        "</ARCH>"
    )


def _build_topology_messages(task: str, current_architecture: dict) -> list[Any]:
    system_text = (
        "你是拓扑规划器。请根据任务复杂度给出建议拓扑，仅输出 JSON。"
        "输出格式必须为 {\"gateways\":[...]}，至少 1 个 gateway，且每个 gateway 至少 1 个 agent。"
        "gateway/agent id 只能包含小写字母、数字、下划线。"
        "不要输出解释性文字。"
    )
    user_text = _build_topology_prompt(task, current_architecture)
    return [SystemMessage(content=system_text), HumanMessage(content=user_text)]


def _extract_json(text: str) -> Optional[dict]:
    """从任意文本中提取首个合法 JSON 对象。

    使用括号匹配算法处理嵌套结构，适活于 LLM 输出中常常包含额外文字的场景。

    Args:
        text: 待提取的原始文本。

    Returns:
        解析得到的 dict，或未找到时返回 None。
    """
    src = str(text or "")

    for start, ch in enumerate(src):
        if ch != "{":
            continue

        stack = ["}"]
        in_string = False
        escaped = False

        for idx in range(start + 1, len(src)):
            current = src[idx]

            if in_string:
                if escaped:
                    escaped = False
                elif current == "\\":
                    escaped = True
                elif current == '"':
                    in_string = False
                continue

            if current == '"':
                in_string = True
                continue

            if current == "{":
                stack.append("}")
                continue

            if current == "[":
                stack.append("]")
                continue

            if current not in "}]":
                continue

            expected = stack.pop() if stack else None
            if current != expected:
                break

            if stack:
                continue

            candidate = src[start : idx + 1]
            try:
                parsed = json.loads(candidate)
            except json.JSONDecodeError:
                break

            if isinstance(parsed, dict):
                return parsed
            break

    return None


def _aggregator_output_limit() -> int:
    return max(1, min(int(AGGREGATOR_MAX_WORDS), 800))


def _truncate_summary_text(text: str, max_units: int = AGGREGATOR_MAX_WORDS) -> str:
    normalized = str(text or "").strip()
    if not normalized or max_units <= 0:
        return ""

    matches = list(_SUMMARY_UNIT_RE.finditer(normalized))
    if len(matches) <= max_units:
        return normalized

    cutoff = matches[max_units - 1].end()
    clipped = normalized[:cutoff].rstrip()
    return f"{clipped}\n...(内容已截断，已限制在 {max_units} 字/词以内)"


def _sanitize_topology(raw: dict) -> Optional[dict]:
    """清洗和规范化 LLM 返回的拓扑提案。

    去重、补默认 ID、过滤无效条目，确保输出符合系统预期的拓扑格式。

    Args:
        raw: LLM 生成的原始拓扑字典。

    Returns:
        规范化后的拓扑字典，或无效时返回 None。
    """
    if not isinstance(raw, dict):
        return None

    gateways = raw.get("gateways")
    if not isinstance(gateways, list) or not gateways:
        return None

    result_gateways = []
    gw_ids = set()

    for idx, gw in enumerate(gateways, start=1):
        if not isinstance(gw, dict):
            continue

        gw_id = str(gw.get("id", "")).strip() or f"gateway_{idx}"
        if gw_id in gw_ids:
            continue
        gw_ids.add(gw_id)

        gw_name = str(gw.get("name", "")).strip() or gw_id
        gw_desc = str(gw.get("description", "")).strip()
        gw_caps = gw.get("capabilities", []) if isinstance(gw.get("capabilities"), list) else []

        agents = gw.get("agents")
        if not isinstance(agents, list) or not agents:
            continue

        normalized_agents = []
        agent_ids = set()
        for j, agent in enumerate(agents, start=1):
            if not isinstance(agent, dict):
                continue
            agent_id = str(agent.get("id", "")).strip() or f"{gw_id}_agent_{j}"
            if agent_id in agent_ids:
                continue
            agent_ids.add(agent_id)

            agent_name = str(agent.get("name", "")).strip() or agent_id
            capabilities = agent.get("capabilities", []) if isinstance(agent.get("capabilities"), list) else []
            depends_on = agent.get("depends_on", []) if isinstance(agent.get("depends_on"), list) else []
            normalized_agents.append(
                {
                    "id": agent_id,
                    "name": agent_name,
                    "capabilities": [str(item) for item in capabilities if str(item).strip()],
                    "depends_on": [str(item) for item in depends_on if str(item).strip()],
                }
            )

        if not normalized_agents:
            continue

        result_gateways.append(
            {
                "id": gw_id,
                "name": gw_name,
                "description": gw_desc,
                "capabilities": [str(item) for item in gw_caps if str(item).strip()],
                "agents": normalized_agents,
            }
        )

    if not result_gateways:
        return None
    return {"gateways": result_gateways}


async def _maybe_submit_topology_proposal(
    task: str,
    current_architecture: dict,
    llm_factory: Callable[[], Any],
) -> None:
    if not TOPOLOGY_PROPOSAL_ENABLED:
        return

    try:
        llm = llm_factory()
        messages = _build_topology_messages(task, current_architecture)
        response = await llm.ainvoke(messages)
        text = extract_text(response.content)

        raw = _extract_json(text)
        proposed = _sanitize_topology(raw) if raw else None
        if not proposed:
            logger.warning("[Master] 拓扑草案解析失败，跳过审批提案")
            append_event(
                event_type="topology_proposal",
                action="submit",
                result="parse_failed",
                actor="master",
                target="architecture",
                detail=f"task={task[:120]}",
            )
            return

        res = create_approval(
            proposed_architecture=proposed,
            requested_by="master",
            reason=f"auto proposal for task: {task[:120]}",
        )
        if not res.get("ok"):
            logger.info("[Master] 拓扑提案跳过: %s", res.get('message', res.get('reason')))
            append_event(
                event_type="topology_proposal",
                action="submit",
                result="skipped",
                actor="master",
                target="architecture",
                detail=res.get("message", res.get("reason", "")),
            )
            return

        req = res.get("request", {})
        deduped = res.get("deduped", False)
        tag = "复用" if deduped else "新建"
        logger.info("[Master] 拓扑审批单%s: id=%s", tag, req.get('id', '-'))
        append_event(
            event_type="topology_proposal",
            action="submit",
            result="deduped" if deduped else "pending",
            actor="master",
            target=req.get("id", ""),
            detail=f"task={task[:120]}",
        )
    except Exception as e:
        logger.warning("[Master] 生成拓扑审批单失败: %s", e)
        logger.debug(traceback.format_exc())
        append_event(
            event_type="topology_proposal",
            action="submit",
            result="error",
            actor="master",
            target="architecture",
            detail=str(e),
        )


def _degraded_task(task: str) -> str:
    return (
        f"{task}\n\n"
        "[降级模式] Dispatcher 失败，请尽量给出互补信息并避免重复结论。"
    )


def _fallback_assignments(task: str, gateways: dict) -> dict:
    return {gw_id: _degraded_task(task) for gw_id in gateways.keys()}


def _spawn_background_task(
    coro: Coroutine[Any, Any, None],
    label: str,
    task_registry: Optional[set[asyncio.Task]] = None,
) -> None:
    task = asyncio.create_task(coro)

    if task_registry is not None:
        task_registry.add(task)

    def _on_done(done: asyncio.Task) -> None:
        if task_registry is not None:
            task_registry.discard(done)
        try:
            done.result()
        except Exception as e:
            logger.warning("[Master] 后台任务失败 (%s): %s", label, e)

    task.add_done_callback(_on_done)


def _normalize_assignment_line(line: str) -> str:
    text = line.strip()
    if not text:
        return ""

    if text.startswith("```"):
        return ""

    text = _ASSIGNMENT_LIST_PREFIX_RE.sub("", text).strip()
    if text.startswith(">"):
        text = text[1:].strip()
        text = _ASSIGNMENT_LIST_PREFIX_RE.sub("", text).strip()

    if text.startswith("`") and text.endswith("`") and len(text) >= 2:
        text = text[1:-1].strip()

    return text


def _parse_assignments(text: str, gateways: dict) -> dict:
    assignments = {}
    for raw_line in text.splitlines():
        line = _normalize_assignment_line(raw_line)
        if not line or "|" not in line:
            continue

        gw_id, sub_task = line.split("|", 1)
        gw_id = gw_id.strip().strip("`")
        sub_task = sub_task.strip().strip("`")

        if gw_id.endswith(":"):
            gw_id = gw_id[:-1].strip()

        if gw_id in gateways and sub_task:
            assignments[gw_id] = sub_task
    return assignments


def _score_output_quality(text: str) -> int:
    """对网关输出文本质量评分（0–100）。

    评分维度包括文本长度、行数、内容多样性、是否含错误关键词。
    低于 GATEWAY_MIN_QUALITY_SCORE 的输出将在聚合时被过滤。

    Args:
        text: 网关输出文本。

    Returns:
        0–100 的质量分数。
    """
    value = str(text or "").strip()
    if not value:
        return 0

    score = min(60, len(value) // 20)

    lines = [line for line in value.splitlines() if line.strip()]
    score += min(20, len(lines) * 2)

    lower = value.lower()
    for token in ("超时", "失败", "error", "exception", "无法", "unknown"):
        if token in lower:
            score -= 20
            break

    tokens = [item.group(0).lower() for item in _SUMMARY_UNIT_RE.finditer(value)]
    unique_tokens = set(tokens)

    if len(unique_tokens) >= 20:
        score += 10

    if len(tokens) >= 20:
        unique_token_ratio = len(unique_tokens) / len(tokens)
        if unique_token_ratio < 0.30:
            score -= 20
        elif unique_token_ratio < 0.45:
            score -= 10

    normalized_lines = [" ".join(line.lower().split()) for line in lines]
    if len(normalized_lines) >= 4:
        unique_line_ratio = len(set(normalized_lines)) / len(normalized_lines)
        if unique_line_ratio < 0.50:
            score -= 20
        elif unique_line_ratio < 0.70:
            score -= 10

    return max(0, min(score, 100))


def _make_dispatcher(
    gateway_agent_map: dict,
    gateways: dict,
    current_architecture: dict,
    llm_factory: Callable[[], Any],
    topology_hash: str,
    task_registry: Optional[set[asyncio.Task]] = None,
) -> Callable[[MasterState], Coroutine[Any, Any, dict]]:
    async def dispatcher(state: MasterState) -> dict:
        task = state["task"]
        span = _start_trace_span(
            state,
            span_name="master.dispatcher",
            component="master",
            input_payload={"task": task[:1000]},
            metadata={"gateway_count": len(gateways)},
        )
        logger.info("[Master] 收到任务: %s", task)

        if TOPOLOGY_PROPOSAL_ENABLED:
            _spawn_background_task(
                _maybe_submit_topology_proposal(task, current_architecture, llm_factory),
                label="topology_proposal",
                task_registry=task_registry,
            )

        dispatch_degraded = False
        try:
            llm = llm_factory()
            messages = _build_dispatcher_messages(task, gateway_agent_map)
            response = await llm.ainvoke(messages)
            text = extract_text(response.content)
            assignments = _parse_assignments(text, gateways)

            missing = [gw_id for gw_id in gateways if gw_id not in assignments]
            if missing:
                logger.warning("[Master] 分配缺失 %s，降级回退", missing)
                for gw_id in missing:
                    assignments[gw_id] = _degraded_task(task)
                dispatch_degraded = True

        except Exception as e:
            logger.error("[Master] Dispatcher 调用失败: %s", e)
            logger.debug(traceback.format_exc())
            assignments = _fallback_assignments(task, gateways)
            dispatch_degraded = True
            _finish_trace_span(
                span,
                status="error",
                output_payload={"assignment_count": len(assignments)},
                error_text=str(e),
                metadata={"dispatch_degraded": dispatch_degraded},
            )
        else:
            _finish_trace_span(
                span,
                status="ok",
                output_payload={"assignment_count": len(assignments), "assignments": assignments},
                metadata={"dispatch_degraded": dispatch_degraded},
            )

        logger.info("[Master] 分配完成: %s", assignments)
        return {
            "gateway_assignments": assignments,
            "dispatch_degraded": dispatch_degraded,
            "topology_hash": topology_hash,
            "trace_id": str(state.get("trace_id", "")),
            "root_span_id": str(state.get("root_span_id", "")),
        }

    return dispatcher


def _make_gateway_node(gw_id: str, gateway_agent_map: dict, gateways: dict) -> Callable[[MasterState], Coroutine[Any, Any, dict]]:
    async def _gateway_node(state: MasterState) -> dict:
        sub_task = (state.get("gateway_assignments") or {}).get(gw_id, state["task"])
        span = _start_trace_span(
            state,
            span_name="master.gateway_node",
            component=f"master:{gw_id}",
            input_payload={"sub_task": str(sub_task)[:1000]},
            metadata={"gateway": gw_id},
        )

        gateway_task = sub_task
        if span:
            trace_id = str(state.get("trace_id", "")).strip()
            parent_span_id = str(span.get("span_id", "")).strip()
            if trace_id and parent_span_id:
                gateway_task = f"[trace_id={trace_id};parent_span_id={parent_span_id}]\n{sub_task}"

        try:
            gateway_result = await gateways[gw_id].process(gateway_task)
        except Exception as e:
            _finish_trace_span(span, status="error", error_text=str(e), metadata={"gateway": gw_id})
            raise

        success = bool(gateway_result.get("success", False))
        output = str(gateway_result.get("output", ""))
        error = str(gateway_result.get("error", ""))
        reason = str(gateway_result.get("reason", "unknown"))
        attempts = int(gateway_result.get("attempts", 1))

        quality_score = _score_output_quality(output)

        if not success:
            append_event(
                event_type="gateway",
                action="process",
                result="error",
                actor=gw_id,
                target="task",
                detail=f"reason={reason}, attempts={attempts}, error={error[:120]}",
            )

        _finish_trace_span(
            span,
            status="ok" if success else "error",
            output_payload={
                "success": success,
                "reason": reason,
                "attempts": attempts,
                "quality_score": quality_score,
                "output": output[:3000],
            },
            error_text=error,
            metadata={"gateway": gw_id},
        )

        return {
            "results": [
                {
                    "gateway": gw_id,
                    "name": gateway_agent_map[gw_id]["name"],
                    "success": success,
                    "output": output,
                    "error": error,
                    "reason": reason,
                    "attempts": attempts,
                    "quality_score": quality_score,
                    "dispatch_degraded": bool(state.get("dispatch_degraded", False)),
                    "trace_span_id": str(span.get("span_id", "")) if span else "",
                }
            ]
        }

    return _gateway_node


def _dedupe_success_results(rows: list[dict]) -> list[dict]:
    seen = set()
    deduped = []
    for row in rows:
        normalized = " ".join(str(row.get("output", "")).split())
        if not normalized:
            continue
        if normalized in seen:
            continue
        seen.add(normalized)
        deduped.append(row)
    return deduped


def _make_aggregator(llm_factory: Callable[[], Any]) -> Callable[[MasterState], Coroutine[Any, Any, dict]]:
    async def aggregator(state: MasterState) -> dict:
        span = _start_trace_span(
            state,
            span_name="master.aggregator",
            component="master",
            input_payload={"result_count": len(list(state.get("results", [])))},
        )
        logger.info("[Master] 开始聚合结果")

        rows = list(state.get("results", []))
        success_rows = [row for row in rows if row.get("success")]
        failed_rows = [row for row in rows if not row.get("success")]

        quality_filtered = [row for row in success_rows if int(row.get("quality_score", 0)) >= GATEWAY_MIN_QUALITY_SCORE]
        selected_success_rows = quality_filtered if quality_filtered else success_rows
        unique_success_rows = _dedupe_success_results(selected_success_rows)

        if not unique_success_rows:
            failure_text = "\n".join(
                f"- {row.get('name', row.get('gateway', 'unknown'))}: "
                f"{row.get('reason', 'error')} / {row.get('error', '')}"
                for row in failed_rows
            )
            final = "# 综合报告（所有网关执行失败）\n\n" + (failure_text or "无可用结果")
            logger.warning("[Master] 无成功网关结果，返回失败汇总")
            _finish_trace_span(
                span,
                status="error",
                output_payload={"success_rows": 0, "failed_rows": len(failed_rows)},
                error_text="all gateway failed",
            )
            return {
                "final_answer": final,
                "trace_id": str(state.get("trace_id", "")),
                "root_span_id": str(state.get("root_span_id", "")),
            }

        success_text = "\n\n".join(
            f"### {r['name']} (via {r['gateway']}, quality={r.get('quality_score', 0)})\n{r['output']}"
            for r in unique_success_rows
        )
        failure_text = "\n".join(
            f"- {row.get('name', row.get('gateway', 'unknown'))}: {row.get('reason', 'error')}"
            for row in failed_rows
        )

        degraded_note = ""
        if state.get("dispatch_degraded"):
            degraded_note = "\n\n备注：本次 Dispatcher 进入降级模式，分配质量可能下降。"

        topology_note = ""
        if state.get("topology_hash"):
            topology_note = f"\n\n拓扑快照：`{state.get('topology_hash')}`"

        output_limit = _aggregator_output_limit()

        try:
            llm = llm_factory()
            system_text = (
                "你是聚合器。请将多个团队结果合并为结构化结论。"
                "输出要简洁，避免重复，明确风险与建议。"
                "最终输出长度必须 <=800字。"
                f"若可控，请进一步压缩到 {output_limit} 字以内。"
            )
            user_text = (
                "请将以下多个团队的执行结果整合为一份简洁的综合报告：\n\n"
                f"{success_text}\n\n"
                "要求：\n"
                "1. 提炼关键信息\n"
                "2. 指出各团队的核心发现\n"
                "3. 给出综合建议\n"
                "4. 避免重复结论，优先输出互补信息\n"
                f"5. 总字数必须 <=800字（硬性约束），并尽量控制在 {output_limit} 字以内"
            )
            if failure_text:
                user_text += f"\n\n以下网关失败，仅供你在结论里说明风险：\n{failure_text}"

            response = await llm.ainvoke([("system", system_text), ("human", user_text)])
            summary = extract_text(response.content)
        except Exception as e:
            logger.error("[Master] Aggregator 调用失败: %s", e)
            logger.debug(traceback.format_exc())
            summary = "LLM 汇总失败，已回退为原始结果拼接。"

        summary = _truncate_summary_text(summary, output_limit)

        details = success_text
        if failure_text:
            details += f"\n\n## 失败网关\n{failure_text}"

        final = f"# 综合报告\n\n{summary}{degraded_note}{topology_note}\n\n---\n\n## 详细结果\n\n{details}"

        logger.info("[Master] 聚合完成")
        _finish_trace_span(
            span,
            status="ok",
            output_payload={
                "success_rows": len(success_rows),
                "failed_rows": len(failed_rows),
                "final_answer": final[:3000],
            },
        )
        return {
            "final_answer": final,
            "trace_id": str(state.get("trace_id", "")),
            "root_span_id": str(state.get("root_span_id", "")),
        }

    return aggregator


async def aggregator(state: MasterState) -> dict:
    """Module-level convenience wrapper (used by tests / direct import)."""
    return await _make_aggregator(_create_llm)(state)


def build_graph(
    llm_factory: Optional[Callable[[], Any]] = None,
    gateway_cls: type = Gateway,
) -> Any:
    """构建并编译 Master 编排图（完全动态）"""

    from langgraph.graph import END, StateGraph

    snapshot = load_architecture_snapshot()
    gateway_agent_map = snapshot["gateway_map"]
    current_architecture = snapshot["raw"]
    topology_hash = snapshot["hash"]

    gateways = _create_gateways(gateway_agent_map, gateway_cls=gateway_cls)
    llm_provider = llm_factory or _create_llm
    background_tasks: set[asyncio.Task] = set()

    if not gateways:
        raise RuntimeError("没有可用 Gateway，请先提供可审批生效的拓扑")

    append_event(
        event_type="topology",
        action="snapshot",
        result="ok",
        actor="master",
        target=topology_hash,
        detail="build_graph",
    )

    graph = StateGraph(MasterState)
    graph.add_node(
        "dispatcher",
        _make_dispatcher(
            gateway_agent_map,
            gateways,
            current_architecture,
            llm_provider,
            topology_hash,
            task_registry=background_tasks,
        ),
    )
    graph.add_node("aggregator", _make_aggregator(llm_provider))

    gateway_nodes = []
    for gw_id in gateways.keys():
        node_name = f"gateway__{gw_id}"
        graph.add_node(node_name, _make_gateway_node(gw_id, gateway_agent_map, gateways))
        gateway_nodes.append(node_name)

    graph.set_entry_point("dispatcher")

    for node_name in gateway_nodes:
        graph.add_edge("dispatcher", node_name)
        graph.add_edge(node_name, "aggregator")

    graph.add_edge("aggregator", END)
    return graph.compile()
