#!/usr/bin/env python3
"""iTerm Agent 输入输出 CLI

示例：
- 群发输入并读取输出：
  python3 scripts/iterm_agent_io.py --action send --all --text "echo hello" --lines 10
- 对指定 agent 读取输出：
  python3 scripts/iterm_agent_io.py --action read --agent agent_01 --agent agent_02 --lines 20
- 列出会话：
  python3 scripts/iterm_agent_io.py --action list
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

ROOT_DIR = Path(__file__).resolve().parents[1]
if str(ROOT_DIR) not in sys.path:
    sys.path.insert(0, str(ROOT_DIR))

os.environ.setdefault("ITERM_IO_BRIDGE_DIRECT", "1")

from agents.iterm_bridge import list_iterm_agent_sessions, read_iterm_output, send_iterm_input


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="iTerm Agent 输入输出")
    parser.add_argument("--action", choices=["list", "send", "read"], required=True)
    parser.add_argument("--state-file", default="", help="state 文件路径，默认 data/iterm_launch_state.json")
    parser.add_argument("--agent", action="append", default=[], help="目标 agent_id，可重复")
    parser.add_argument("--all", action="store_true", help="作用于全部 agent")
    parser.add_argument("--text", default="", help="send 动作的输入文本")
    parser.add_argument("--wait-sec", type=float, default=0.4, help="send 后读取输出前等待秒数")
    parser.add_argument("--lines", type=int, default=20, help="读取最近输出行数")
    parser.add_argument("--output-file", default="", help="可选：结果写入文件")
    return parser.parse_args()


def _join_agent_args(agent_args: list[str]) -> str:
    cleaned = [str(item).strip() for item in agent_args if str(item).strip()]
    return ",".join(cleaned)


def main() -> int:
    import time as _t
    _t0 = _t.monotonic()
    args = parse_args()
    print(f"[iterm-io-perf] args parsed  elapsed={_t.monotonic()-_t0:.3f}s", file=sys.stderr, flush=True)
    # 关键环境变量诊断
    for _k in ("ITERM_IO_BRIDGE_DIRECT", "_ACP_BUS_PROCESS", "ITERM2_COOKIE", "ITERM2_KEY", "TERM_SESSION_ID"):
        print(f"[iterm-io-perf]   env {_k}={os.environ.get(_k, '<unset>')}", file=sys.stderr, flush=True)

    if args.action == "list":
        _ta = _t.monotonic()
        result = list_iterm_agent_sessions(state_file=args.state_file)
        print(f"[iterm-io-perf] list done  elapsed={_t.monotonic()-_ta:.3f}s  total={_t.monotonic()-_t0:.3f}s", file=sys.stderr, flush=True)
    elif args.action == "read":
        result = read_iterm_output(
            agent_id=_join_agent_args(args.agent),
            all_agents=bool(args.all),
            read_lines=max(0, int(args.lines)),
            state_file=args.state_file,
        )
    else:
        if not args.text:
            print(json.dumps({"ok": False, "error": "send 动作必须传 --text"}, ensure_ascii=False))
            return 1

        result = send_iterm_input(
            text=args.text,
            agent_id=_join_agent_args(args.agent),
            all_agents=bool(args.all),
            wait_sec=max(0.0, float(args.wait_sec)),
            read_lines=max(0, int(args.lines)),
            state_file=args.state_file,
            append_enter=True,
        )

    text = json.dumps(result, ensure_ascii=False, indent=2)
    if args.output_file:
        output_path = Path(args.output_file)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(text, encoding="utf-8")

    print(text)
    return 0 if result.get("ok") else 2


if __name__ == "__main__":
    raise SystemExit(main())
