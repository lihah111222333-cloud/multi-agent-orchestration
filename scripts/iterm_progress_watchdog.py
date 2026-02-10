#!/usr/bin/env python3
"""iTerm Agent 进度看门狗：无有效变化超时后自动唤醒。"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

ROOT_DIR = Path(__file__).resolve().parents[1]
if str(ROOT_DIR) not in sys.path:
    sys.path.insert(0, str(ROOT_DIR))

os.environ.setdefault("ITERM_IO_BRIDGE_DIRECT", "1")

from agents.iterm_bridge import read_iterm_output, send_iterm_input


EPHEMERAL_PATTERNS = [
    re.compile(r"^\? for shortcuts", re.IGNORECASE),
    re.compile(r"^›\s"),
    re.compile(r"^•Working\(", re.IGNORECASE),
    re.compile(r"^─ Worked for", re.IGNORECASE),
    re.compile(r"^Use /skills", re.IGNORECASE),
]

WORKING_TIMER_RE = re.compile(r"\(\d+[hms](?:\s+\d+[hms])?.*?interrupt\)")


@dataclass
class AgentProgressState:
    last_signature: str = ""
    last_change_ts: float = 0.0
    last_wake_ts: float = 0.0
    last_tail: str = ""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="iTerm Agent 进度看门狗")
    parser.add_argument(
        "--state-file",
        default=str(ROOT_DIR / "data" / "iterm_launch_state.json"),
        help="iTerm state 文件路径",
    )
    parser.add_argument(
        "--log-file",
        default=str(ROOT_DIR / "data" / "agent_progress_watch.log"),
        help="监控日志输出路径",
    )
    parser.add_argument("--interval-sec", type=int, default=60, help="轮询间隔秒数")
    parser.add_argument("--stall-sec", type=int, default=300, help="判定停滞秒数")
    parser.add_argument(
        "--wake-cooldown-sec",
        type=int,
        default=300,
        help="同一 Agent 两次唤醒最小间隔秒数",
    )
    parser.add_argument("--read-lines", type=int, default=35, help="每次读取最近输出行数")
    parser.add_argument(
        "--wake-text",
        default="继续，接着干。请继续当前任务，并用 3 行汇报：已完成 / 正在做 / 阻塞。",
        help="停滞时发送的唤醒词",
    )
    parser.add_argument("--run-once", action="store_true", help="仅执行单轮")
    return parser.parse_args()


def now_text() -> str:
    return datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def append_log(log_path: Path, line: str) -> None:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("a", encoding="utf-8") as handle:
        handle.write(line)
        handle.write("\n")


def normalize_line(line: Any) -> str:
    text = str(line or "").strip()
    if not text:
        return ""

    for pattern in EPHEMERAL_PATTERNS:
        if pattern.search(text):
            return ""

    text = WORKING_TIMER_RE.sub("", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text


def build_signature(lines: list[Any]) -> tuple[str, str, list[str]]:
    normalized = [normalize_line(line) for line in lines]
    filtered = [line for line in normalized if line]
    if not filtered:
        return "", "", []

    window = filtered[-8:]
    digest = hashlib.sha1("\n".join(window).encode("utf-8")).hexdigest()
    tail = window[-1]
    return digest, tail, window


def run_once(args: argparse.Namespace, states: dict[str, AgentProgressState], log_path: Path) -> None:
    timestamp = now_text()
    append_log(log_path, f"=== {timestamp} ===")

    payload = read_iterm_output(
        agent_id="",
        all_agents=True,
        read_lines=max(1, int(args.read_lines)),
        state_file=str(args.state_file),
    )

    if not payload.get("ok"):
        append_log(log_path, f"- monitor_error: {payload.get('error', 'unknown_error')}")
        append_log(log_path, "")
        return

    now_ts = time.time()
    rows = payload.get("results", [])

    for row in rows:
        agent_id = str(row.get("agent_id") or "")
        if not agent_id:
            continue

        state = states.setdefault(agent_id, AgentProgressState(last_change_ts=now_ts))

        if row.get("error"):
            append_log(log_path, f"- {agent_id}: io_error={row.get('error')}")
            continue

        signature, tail, filtered = build_signature(row.get("output", []))

        if signature and signature != state.last_signature:
            state.last_signature = signature
            state.last_change_ts = now_ts
            state.last_tail = tail
            append_log(log_path, f"- {agent_id}: changed | {tail[:180]}")
            continue

        idle_sec = now_ts - state.last_change_ts
        idle_min = int(idle_sec // 60)
        append_log(log_path, f"- {agent_id}: idle={idle_min}m | {state.last_tail[:180] if state.last_tail else '(no stable output)'}")

        should_wake = (
            idle_sec >= max(60, int(args.stall_sec))
            and (now_ts - state.last_wake_ts) >= max(60, int(args.wake_cooldown_sec))
        )

        if not should_wake:
            continue

        wake_text = str(args.wake_text or "").strip()
        if not wake_text:
            wake_text = "继续，接着干。"

        result = send_iterm_input(
            text=wake_text,
            agent_id=agent_id,
            all_agents=False,
            wait_sec=0.2,
            read_lines=8,
            state_file=str(args.state_file),
            append_enter=True,
        )

        state.last_wake_ts = now_ts
        wake_ok = bool(result.get("ok"))
        append_log(
            log_path,
            f"- {agent_id}: wake_sent={wake_ok} idle={idle_min}m text={wake_text[:120]}",
        )

    append_log(log_path, "")


def main() -> int:
    args = parse_args()
    states: dict[str, AgentProgressState] = {}
    log_path = Path(args.log_file)

    while True:
        run_once(args=args, states=states, log_path=log_path)
        if args.run_once:
            return 0
        time.sleep(max(5, int(args.interval_sec)))


if __name__ == "__main__":
    raise SystemExit(main())
