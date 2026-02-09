"""Master 编排器 — LangGraph StateGraph

编排流程:
1. dispatcher: 接收任务，分配子任务到 3 个 Gateway
2. gateway_1/2/3: 并行执行，每个 Gateway 调用其管理的 Agent
3. aggregator: 汇总所有 Gateway 的结果，生成最终输出
"""

import logging
import operator
import traceback
from typing import Annotated, TypedDict

from langgraph.graph import StateGraph, END
from langchain_openai import ChatOpenAI

from gateways.gateway import Gateway
from config.settings import (
    GATEWAY_AGENT_MAP, LLM_MODEL, LLM_TEMPERATURE,
    OPENAI_BASE_URL, LLM_TIMEOUT, LLM_MAX_RETRIES,
)
from utils import extract_text

logger = logging.getLogger(__name__)


def _create_llm() -> ChatOpenAI:
    """创建带重试和超时的 LLM 实例"""
    return ChatOpenAI(
        model=LLM_MODEL,
        temperature=LLM_TEMPERATURE,
        base_url=OPENAI_BASE_URL,
        max_retries=LLM_MAX_RETRIES,
        request_timeout=LLM_TIMEOUT,
    )


# ========================
# 状态定义
# ========================
class MasterState(TypedDict):
    """Master 编排的全局状态"""
    task: str                                        # 原始任务
    gateway_assignments: dict                        # 各 Gateway 的子任务
    results: Annotated[list, operator.add]            # 聚合结果（自动合并）
    final_answer: str                                # 最终输出


# ========================
# 初始化 3 个 Gateway
# ========================
def _create_gateways() -> dict[str, Gateway]:
    """根据配置创建 Gateway 实例"""
    gateways = {}
    for gw_name, gw_config in GATEWAY_AGENT_MAP.items():
        gateways[gw_name] = Gateway(
            name=gw_name,
            display_name=gw_config["name"],
            agent_configs=gw_config["agents"],
        )
    return gateways


GATEWAYS = _create_gateways()


# ========================
# 节点函数
# ========================
async def dispatcher(state: MasterState) -> dict:
    """Master Dispatcher: 将任务拆解并分配给 3 个 Gateway

    使用 LLM 智能拆分任务为 3 个子任务，分别对应：
    - Gateway 1: 数据分析相关
    - Gateway 2: 内容生成相关
    - Gateway 3: 系统运维相关

    如果 LLM 返回格式不规范，则将原始任务分配给所有 Gateway（降级策略）。
    """
    task = state["task"]
    logger.info(f"[Master] 收到任务: {task}")

    try:
        llm = _create_llm()
        prompt = f"""你是一个任务分配器。将以下任务拆分为 3 个子任务：

任务: {task}

请分别为以下 3 个团队生成子任务：
1. 数据分析团队（数据采集、清洗、分析、可视化）
2. 内容生成团队（文本生成、翻译、代码生成、报告）
3. 系统运维团队（监控、日志、部署、告警）

格式（严格用 | 分隔，不要有编号，只输出一行）:
数据分析子任务 | 内容生成子任务 | 系统运维子任务"""

        response = await llm.ainvoke(prompt)
        text = extract_text(response.content)
        parts = text.split("|")

        if len(parts) >= 3:
            assignments = {
                "gateway_1": parts[0].strip(),
                "gateway_2": parts[1].strip(),
                "gateway_3": parts[2].strip(),
            }
        else:
            # 降级：LLM 返回格式不符，将原始任务发给所有 Gateway
            logger.warning(
                f"[Master] LLM 返回格式不规范（期望 3 段，实际 {len(parts)} 段），"
                f"降级为全量分配。原始响应: {text[:200]}"
            )
            assignments = {
                "gateway_1": task,
                "gateway_2": task,
                "gateway_3": task,
            }

    except Exception as e:
        # 降级：LLM 调用失败，将原始任务发给所有 Gateway
        logger.error(f"[Master] Dispatcher LLM 调用失败: {e}")
        logger.debug(traceback.format_exc())
        assignments = {
            "gateway_1": task,
            "gateway_2": task,
            "gateway_3": task,
        }

    logger.info(f"[Master] 任务分配完成: {assignments}")
    return {"gateway_assignments": assignments}


async def gateway_1_node(state: MasterState) -> dict:
    """Gateway 1 节点: 数据分析"""
    task = state["gateway_assignments"]["gateway_1"]
    result = await GATEWAYS["gateway_1"].process(task)
    return {"results": [{"gateway": "gateway_1", "name": "数据分析", "output": result}]}


async def gateway_2_node(state: MasterState) -> dict:
    """Gateway 2 节点: 内容生成"""
    task = state["gateway_assignments"]["gateway_2"]
    result = await GATEWAYS["gateway_2"].process(task)
    return {"results": [{"gateway": "gateway_2", "name": "内容生成", "output": result}]}


async def gateway_3_node(state: MasterState) -> dict:
    """Gateway 3 节点: 系统运维"""
    task = state["gateway_assignments"]["gateway_3"]
    result = await GATEWAYS["gateway_3"].process(task)
    return {"results": [{"gateway": "gateway_3", "name": "系统运维", "output": result}]}


async def aggregator(state: MasterState) -> dict:
    """聚合器: 汇总所有 Gateway 的结果，生成最终报告"""
    logger.info("[Master] 开始聚合结果")

    results_text = "\n\n".join(
        f"### {r['name']} (via {r['gateway']})\n{r['output']}"
        for r in state["results"]
    )

    try:
        llm = _create_llm()
        prompt = f"""请将以下多个团队的执行结果整合为一份简洁的综合报告：

{results_text}

要求：
1. 提炼关键信息
2. 指出各团队的核心发现
3. 给出综合建议"""

        response = await llm.ainvoke(prompt)
        summary = extract_text(response.content)
        final = f"# 综合报告\n\n{summary}\n\n---\n\n## 详细结果\n\n{results_text}"

    except Exception as e:
        # 降级：LLM 汇总失败，直接返回原始结果
        logger.error(f"[Master] Aggregator LLM 调用失败: {e}")
        logger.debug(traceback.format_exc())
        final = f"# 综合报告（LLM 汇总失败，展示原始结果）\n\n{results_text}"

    logger.info("[Master] 聚合完成")
    return {"final_answer": final}


# ========================
# 构建 LangGraph
# ========================
def build_graph() -> StateGraph:
    """构建并编译 Master 编排图

    流程: dispatcher → [gateway_1, gateway_2, gateway_3] (并行) → aggregator → END
    """
    graph = StateGraph(MasterState)

    # 添加节点
    graph.add_node("dispatcher", dispatcher)
    graph.add_node("gateway_1", gateway_1_node)
    graph.add_node("gateway_2", gateway_2_node)
    graph.add_node("gateway_3", gateway_3_node)
    graph.add_node("aggregator", aggregator)

    # 入口
    graph.set_entry_point("dispatcher")

    # dispatcher 扇出到 3 个 Gateway（并行执行）
    graph.add_edge("dispatcher", "gateway_1")
    graph.add_edge("dispatcher", "gateway_2")
    graph.add_edge("dispatcher", "gateway_3")

    # 3 个 Gateway 汇聚到 aggregator
    graph.add_edge("gateway_1", "aggregator")
    graph.add_edge("gateway_2", "aggregator")
    graph.add_edge("gateway_3", "aggregator")

    # 终点
    graph.add_edge("aggregator", END)

    return graph.compile()
