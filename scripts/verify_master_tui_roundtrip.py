#!/usr/bin/env python3
"""Verify the new Codex TUI orchestration chain via the live master session.

Flow:
1) Ensure child iTerm agent sessions are present.
2) Find the master iTerm session.
3) Ask master to run `python run.py "<marker>"`.
4) Confirm Begin/Update/End events are published in orchestration_tui_bus.
5) Optionally confirm master replies with `DONE <marker>`.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT_DIR = Path(__file__).resolve().parents[1]
if str(ROOT_DIR) not in sys.path:
    sys.path.insert(0, str(ROOT_DIR))

import tg_bridge
from agents.iterm_bridge import list_iterm_agent_sessions, read_session_screen, send_to_session
from orchestration_tui_bus import get_snapshot, list_events

DEFAULT_TIMEOUT_SEC = 240
DEFAULT_POLL_INTERVAL_SEC = 2.0
DEFAULT_READ_LINES = 120


def _utc_stamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")


def _screen_tail(session_id: str, lines: int) -> list[str]:
    screen = read_session_screen(session_id, lines=max(20, int(lines)))
    rows = screen.get("lines")
    if isinstance(rows, list):
        return [str(item) for item in rows]
    return []


def _build_prompt(marker: str) -> str:
    cmd = f"cd {ROOT_DIR} && ./.venv/bin/python run.py \"{marker}\""
    return (
        "请只做一件事：在本机终端执行这一条命令并等待完成，不要改代码，"
        f"完成后回复一行 DONE {marker}。\n"
        f"{cmd}"
    )


def _new_events(since_seq: int, limit: int = 1000) -> list[dict[str, Any]]:
    payload = list_events(limit=max(1, min(int(limit), 2000)), since_seq=max(0, int(since_seq)))
    rows = payload.get("events")
    if not isinstance(rows, list):
        return []
    return [item for item in rows if isinstance(item, dict)]


def _collect_run_events(run_id: str) -> list[dict[str, Any]]:
    all_rows = _new_events(since_seq=0, limit=2000)
    return [
        ev
        for ev in all_rows
        if isinstance(ev.get("payload"), dict) and str(ev["payload"].get("run_id") or "") == run_id
    ]


def run_verify(
    *,
    marker: str,
    timeout_sec: int,
    poll_interval_sec: float,
    read_lines: int,
    interrupt_before: bool,
    require_done_reply: bool,
) -> dict[str, Any]:
    child_info = list_iterm_agent_sessions()
    child_sessions = child_info.get("sessions") if isinstance(child_info, dict) else None
    child_count = len(child_sessions) if isinstance(child_sessions, list) else 0

    master = tg_bridge._find_master_session()
    if not master:
        return {
            "ok": False,
            "error": "master_not_found",
            "child_count": child_count,
            "checked_at": datetime.now(timezone.utc).isoformat(),
        }

    session_id = str(master.get("session_id") or "").strip()
    if not session_id:
        return {
            "ok": False,
            "error": "master_session_id_empty",
            "master": master,
            "child_count": child_count,
            "checked_at": datetime.now(timezone.utc).isoformat(),
        }

    baseline_seq = int(get_snapshot().get("seq") or 0)
    done_token = f"DONE {marker}"
    prompt = _build_prompt(marker)

    interrupt_ret = None
    if interrupt_before:
        interrupt_ret = send_to_session(session_id, "\x03")

    send_ret = send_to_session(session_id, prompt)
    submit_ret = send_to_session(session_id, "\r")

    run_id = ""
    begin_seen = False
    update_seen = False
    end_seen = False
    done_seen = False
    seen_seq: set[int] = set()
    matched_events: list[dict[str, Any]] = []

    deadline = time.time() + max(10, int(timeout_sec))
    while time.time() < deadline:
        for ev in _new_events(since_seq=max(0, baseline_seq - 2), limit=1500):
            seq = int(ev.get("seq") or 0)
            if seq in seen_seq:
                continue
            seen_seq.add(seq)

            payload = ev.get("payload") if isinstance(ev.get("payload"), dict) else {}
            event = str(ev.get("event") or "")
            source = str(ev.get("source") or "")

            if source == "run.py" and event == "BeginOrchestrationTaskState":
                details = str(payload.get("status_details") or "")
                if marker in details:
                    run_id = str(payload.get("run_id") or "")
                    begin_seen = True
                    matched_events.append(ev)
                    continue

            if run_id and source == "run.py" and str(payload.get("run_id") or "") == run_id:
                if event == "UpdateOrchestrationTaskState":
                    update_seen = True
                    matched_events.append(ev)
                elif event == "EndOrchestrationTaskState":
                    end_seen = True
                    matched_events.append(ev)

        tail = _screen_tail(session_id, read_lines)
        if done_token and done_token in "\n".join(tail[-40:]):
            done_seen = True

        if begin_seen and update_seen and end_seen and (done_seen or not require_done_reply):
            break
        time.sleep(max(0.2, float(poll_interval_sec)))

    final_tail = _screen_tail(session_id, read_lines + 40)
    run_events = _collect_run_events(run_id) if run_id else []

    ok = begin_seen and update_seen and end_seen and (done_seen or not require_done_reply)
    return {
        "ok": bool(ok),
        "marker": marker,
        "done_token": done_token,
        "done_seen": done_seen,
        "child_count": child_count,
        "master": master,
        "baseline_seq": baseline_seq,
        "seq_now": int(get_snapshot().get("seq") or 0),
        "run_id": run_id,
        "begin_seen": begin_seen,
        "update_seen": update_seen,
        "end_seen": end_seen,
        "interrupt_ret": interrupt_ret,
        "send_ret": send_ret,
        "submit_ret": submit_ret,
        "matched_events_tail": matched_events[-8:],
        "run_events_tail": run_events[-8:],
        "master_tail": final_tail[-35:],
        "checked_at": datetime.now(timezone.utc).isoformat(),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Verify master->run.py->TUI bus roundtrip")
    parser.add_argument("--marker", default="", help="Optional marker text; auto-generated when omitted")
    parser.add_argument("--timeout-sec", type=int, default=DEFAULT_TIMEOUT_SEC, help="Verification timeout seconds")
    parser.add_argument("--poll-interval-sec", type=float, default=DEFAULT_POLL_INTERVAL_SEC, help="Polling interval")
    parser.add_argument("--read-lines", type=int, default=DEFAULT_READ_LINES, help="Master screen tail lines")
    parser.add_argument("--no-interrupt", action="store_true", help="Do not send Ctrl+C before sending command")
    parser.add_argument(
        "--allow-missing-done",
        action="store_true",
        help="Allow success even if DONE token is not found on master screen",
    )
    args = parser.parse_args()

    marker = str(args.marker or "").strip() or f"TUI_E2E_RUN_{_utc_stamp()}"
    result = run_verify(
        marker=marker,
        timeout_sec=int(args.timeout_sec),
        poll_interval_sec=float(args.poll_interval_sec),
        read_lines=int(args.read_lines),
        interrupt_before=not bool(args.no_interrupt),
        require_done_reply=not bool(args.allow_missing_done),
    )
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result.get("ok") else 2


if __name__ == "__main__":
    raise SystemExit(main())
