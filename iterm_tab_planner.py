"""iTerm Agent Tab 数量规划器

规则：
- 只允许 4/6/8/12
- 最少 4
- 可根据任务描述 + 当前拓扑自动决策
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Optional

ALLOWED_TAB_COUNTS = (4, 6, 8, 12)


def _allowed_in_range(min_tabs: int, max_tabs: int) -> list[int]:
    values = [x for x in ALLOWED_TAB_COUNTS if min_tabs <= x <= max_tabs]
    if not values:
        return [4]
    return values


def _pick_by_score(score: int, min_tabs: int, max_tabs: int) -> int:
    if score >= 8:
        target = 12
    elif score >= 5:
        target = 8
    elif score >= 3:
        target = 6
    else:
        target = 4

    allowed = _allowed_in_range(min_tabs, max_tabs)
    if target in allowed:
        return target

    # 兜底：选择不小于 target 的最小值，否则取最大值
    larger = [x for x in allowed if x >= target]
    if larger:
        return min(larger)
    return max(allowed)


def normalize_requested_tabs(requested_tabs: int, min_tabs: int = 4, max_tabs: int = 12) -> int:
    if requested_tabs not in ALLOWED_TAB_COUNTS:
        raise ValueError("tabs 仅允许 4/6/8/12")
    if requested_tabs < min_tabs:
        raise ValueError(f"tabs 不可小于最小值: {min_tabs}")
    if requested_tabs > max_tabs:
        raise ValueError(f"tabs 不可大于最大值: {max_tabs}")
    return requested_tabs


def estimate_tabs_from_task(task: str, min_tabs: int = 4, max_tabs: int = 12) -> tuple[int, str]:
    text = (task or "").strip().lower()
    if not text:
        return _pick_by_score(1, min_tabs, max_tabs), "任务为空，使用基础并发"

    score = 1
    length = len(text)
    if length > 40:
        score += 1
    if length > 120:
        score += 1
    if length > 240:
        score += 1

    heavy_keywords = [
        "多agent", "multi-agent", "并发", "全链路", "生产", "压测", "稳定性", "容灾", "审批",
        "拓扑", "编排", "自动化", "监控", "风控", "端到端", "integration", "deploy",
    ]
    medium_keywords = [
        "分析", "优化", "重构", "报告", "流程", "日志", "数据", "质量", "回归", "测试",
    ]

    for kw in heavy_keywords:
        if kw in text:
            score += 2
    for kw in medium_keywords:
        if kw in text:
            score += 1

    score = min(score, 12)
    tabs = _pick_by_score(score, min_tabs, max_tabs)
    return tabs, f"任务复杂度评分={score}"


def estimate_tabs_from_architecture(config_path: Path, min_tabs: int = 4, max_tabs: int = 12) -> tuple[int, str]:
    try:
        raw = json.loads(Path(config_path).read_text(encoding="utf-8"))
    except Exception:
        return _pick_by_score(1, min_tabs, max_tabs), "拓扑读取失败，使用基础并发"

    gateways = raw.get("gateways", [])
    gateway_count = len(gateways) if isinstance(gateways, list) else 0

    agent_count = 0
    if isinstance(gateways, list):
        for gw in gateways:
            agents = gw.get("agents", []) if isinstance(gw, dict) else []
            if isinstance(agents, list):
                agent_count += len(agents)

    # 用拓扑规模映射成 score
    score = 1
    if gateway_count >= 2 or agent_count >= 5:
        score = 3
    if gateway_count >= 3 or agent_count >= 7:
        score = 5
    if gateway_count >= 4 or agent_count >= 10:
        score = 8

    tabs = _pick_by_score(score, min_tabs, max_tabs)
    return tabs, f"拓扑规模 gateways={gateway_count}, agents={agent_count}"


def planner_decide_tab_count(
    task: str = "",
    config_path: Optional[Path] = None,
    requested_tabs: Optional[int] = None,
    min_tabs: int = 4,
    max_tabs: int = 12,
) -> dict:
    """返回 {tab_count, reason, task_tabs, arch_tabs}。"""

    min_tabs = max(min_tabs, 4)
    max_tabs = min(max_tabs, 12)

    if requested_tabs is not None:
        tabs = normalize_requested_tabs(requested_tabs, min_tabs=min_tabs, max_tabs=max_tabs)
        return {
            "tab_count": tabs,
            "reason": "使用手动指定 tab 数",
            "task_tabs": None,
            "arch_tabs": None,
        }

    task_tabs, task_reason = estimate_tabs_from_task(task, min_tabs=min_tabs, max_tabs=max_tabs)

    arch_tabs = None
    arch_reason = ""
    if config_path is not None:
        arch_tabs, arch_reason = estimate_tabs_from_architecture(config_path, min_tabs=min_tabs, max_tabs=max_tabs)

    candidates = [task_tabs]
    if arch_tabs is not None:
        candidates.append(arch_tabs)

    tab_count = max(candidates)
    reason_parts = [task_reason]
    if arch_reason:
        reason_parts.append(arch_reason)
    reason_parts.append(f"最终取 max -> {tab_count}")

    return {
        "tab_count": tab_count,
        "reason": "; ".join(reason_parts),
        "task_tabs": task_tabs,
        "arch_tabs": arch_tabs,
    }
