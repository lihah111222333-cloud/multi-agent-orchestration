#!/usr/bin/env python3
"""使用 iTerm Python API 启动多 Agent（支持 Tab / 等分 Pane 布局）"""

from __future__ import annotations

import argparse
import asyncio
import json
import shlex
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT_DIR = Path(__file__).resolve().parents[1]
if str(ROOT_DIR) not in sys.path:
    sys.path.insert(0, str(ROOT_DIR))

from iterm_tab_planner import planner_decide_tab_count


def _project_root() -> Path:
    return ROOT_DIR


def _default_template() -> str:
    return "codex"


def _default_identity_template() -> str:
    return "唤醒检查：你的固定身份是 {agent_id}（{agent_name}）。现在仅回复 ACK-{index_padded}。"


def _grid_for_count(tab_count: int) -> tuple[int, int]:
    grid_map = {
        4: (2, 2),
        6: (3, 2),
        8: (4, 2),
        12: (4, 3),
    }
    if tab_count not in grid_map:
        raise ValueError(f"不支持的 pane 数量: {tab_count}")
    return grid_map[tab_count]


def _build_start_command(template: str, index: int, agent_id: str, agent_name: str) -> str:
    values = {
        "index": index,
        "index_padded": f"{index:02d}",
        "agent_id": agent_id,
        "agent_id_quoted": shlex.quote(agent_id),
        "agent_name": agent_name,
        "agent_name_quoted": shlex.quote(agent_name),
    }
    return template.format(**values)


def _build_identity_prompt(template: str, index: int, agent_id: str, agent_name: str) -> str:
    values = {
        "index": index,
        "index_padded": f"{index:02d}",
        "agent_id": agent_id,
        "agent_name": agent_name,
    }
    try:
        return template.format(**values)
    except KeyError as exc:
        missing = str(exc).strip("'\"")
        raise ValueError(f"identity-template 存在未知变量: {missing}") from exc


def _build_shell_command(project_root: Path, start_cmd: str, work_dir: str = "") -> str:
    target = shlex.quote(work_dir) if work_dir else "$HOME"
    return (
        f"cd {target}; "
        f"exec {start_cmd}"
    )


def _build_session_label(agent_id: str, agent_name: str) -> str:
    return f"{agent_id} | {agent_name}"


def _build_badge(index: int) -> str:
    return f"A{index:02d}"


def _build_agent_entries(
    count: int,
    start_template: str,
    name_prefix: str,
    project_root: Path,
    inject_identity: bool,
    identity_template: str,
) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    template = str(identity_template or "").strip()

    for index in range(1, count + 1):
        agent_id = f"agent_{index:02d}"
        agent_name = f"{name_prefix} {index:02d}"
        start_cmd = _build_start_command(start_template, index, agent_id, agent_name)
        shell_cmd = _build_shell_command(project_root=project_root, start_cmd=start_cmd)

        identity_prompt = ""
        if inject_identity and template:
            identity_prompt = _build_identity_prompt(template, index, agent_id, agent_name)

        entries.append(
            {
                "index": index,
                "agent_id": agent_id,
                "agent_name": agent_name,
                "start_cmd": start_cmd,
                "shell_cmd": shell_cmd,
                "session_label": _build_session_label(agent_id, agent_name),
                "tab_label": _build_session_label(agent_id, agent_name),
                "badge": _build_badge(index),
                "identity_prompt": identity_prompt,
            }
        )
    return entries


def _write_launch_state(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


async def _split_balanced(base_session: Any, count: int, vertical: bool) -> list[Any]:
    sessions = [base_session]
    weights = [1.0]

    for _ in range(count - 1):
        index = max(range(len(weights)), key=lambda i: (weights[i], -i))
        source = sessions[index]
        new_session = await source.async_split_pane(vertical=vertical, before=False)

        half = weights[index] / 2
        weights[index] = half
        sessions.insert(index + 1, new_session)
        weights.insert(index + 1, half)

    return sessions


async def _build_grid_sessions(root_session: Any, cols: int, rows: int) -> list[Any]:
    col_sessions = await _split_balanced(root_session, cols, vertical=True)
    col_rows: list[list[Any]] = []
    for col in col_sessions:
        row_sessions = await _split_balanced(col, rows, vertical=False)
        col_rows.append(row_sessions)

    ordered: list[Any] = []
    for row in range(rows):
        for col in range(cols):
            ordered.append(col_rows[col][row])
    return ordered


async def _safe_iterm_call(target: Any, method: str, *args: Any) -> bool:
    func = getattr(target, method, None)
    if func is None:
        return False
    try:
        await func(*args)
        return True
    except Exception:
        return False


async def _decorate_session(entry: dict[str, Any], session: Any, tab: Any | None) -> dict[str, bool]:
    label_applied = await _safe_iterm_call(session, "async_set_name", str(entry.get("session_label", "") or ""))
    badge_applied = await _safe_iterm_call(session, "async_set_variable", "user.badge", str(entry.get("badge", "") or ""))
    agent_id_applied = await _safe_iterm_call(session, "async_set_variable", "user.agent_id", str(entry.get("agent_id", "") or ""))
    agent_name_applied = await _safe_iterm_call(
        session,
        "async_set_variable",
        "user.agent_name",
        str(entry.get("agent_name", "") or ""),
    )
    agent_label_applied = await _safe_iterm_call(
        session,
        "async_set_variable",
        "user.agent_label",
        str(entry.get("session_label", "") or ""),
    )

    tab_title_applied = False
    if tab is not None:
        tab_title_applied = await _safe_iterm_call(tab, "async_set_title", str(entry.get("tab_label", "") or ""))

    return {
        "label_applied": label_applied,
        "badge_applied": badge_applied,
        "agent_id_applied": agent_id_applied,
        "agent_name_applied": agent_name_applied,
        "agent_label_applied": agent_label_applied,
        "tab_title_applied": tab_title_applied,
    }


async def _inject_identity_prompts(
    session_rows: list[dict[str, Any]],
    *,
    identity_delay_sec: float,
) -> int:
    if not session_rows:
        return 0

    has_prompt = any(str(row.get("identity_prompt", "") or "").strip() for row in session_rows)
    if not has_prompt:
        return 0

    if identity_delay_sec > 0:
        await asyncio.sleep(identity_delay_sec)

    injected_count = 0
    for row in session_rows:
        session = row.get("session")
        prompt = str(row.get("identity_prompt", "") or "").strip()
        if session is None or not prompt:
            row["identity_injected"] = False
            continue

        try:
            await session.async_send_text(prompt)
            await asyncio.sleep(0.05)
            await session.async_send_text("\r")
            row["identity_injected"] = True
            injected_count += 1
        except Exception:
            row["identity_injected"] = False

    return injected_count


async def _reapply_labels_after_wakeup(
    session_rows: list[dict[str, Any]],
    *,
    relabel_delay_sec: float = 0.35,
) -> int:
    if not session_rows:
        return 0

    if relabel_delay_sec > 0:
        await asyncio.sleep(relabel_delay_sec)

    relabeled_count = 0
    for row in session_rows:
        session = row.get("session")
        entry = row.get("entry")
        tab = row.get("tab")
        if session is None or not isinstance(entry, dict):
            continue

        decorated = await _decorate_session(entry, session=session, tab=tab)
        row["label_applied"] = bool(row.get("label_applied", False) or decorated["label_applied"])
        row["badge_applied"] = bool(row.get("badge_applied", False) or decorated["badge_applied"])
        row["agent_id_applied"] = bool(row.get("agent_id_applied", False) or decorated["agent_id_applied"])
        row["agent_name_applied"] = bool(row.get("agent_name_applied", False) or decorated["agent_name_applied"])
        row["agent_label_applied"] = bool(row.get("agent_label_applied", False) or decorated["agent_label_applied"])
        row["tab_title_applied"] = bool(row.get("tab_title_applied", False) or decorated["tab_title_applied"])

        if decorated["label_applied"]:
            relabeled_count += 1

    return relabeled_count


def _run_iterm_tabs(entries: list[dict[str, Any]], identity_delay_sec: float) -> dict[str, Any]:
    import iterm2

    result: dict[str, Any] = {}

    async def main(connection):
        nonlocal result
        app = await iterm2.async_get_app(connection)

        had_window = app.current_terminal_window is not None
        window = app.current_terminal_window
        if window is None:
            window = await app.async_create_window()

        session_rows: list[dict[str, Any]] = []
        for idx, entry in enumerate(entries):
            if idx == 0 and (not had_window) and window.current_tab is not None:
                tab = window.current_tab
                session = tab.current_session
            else:
                tab = await window.async_create_tab()
                session = tab.current_session

            if session is None:
                continue

            decorated = await _decorate_session(entry, session=session, tab=tab)
            await session.async_send_text(str(entry.get("shell_cmd", "")) + "\n")

            session_rows.append(
                {
                    "agent_id": entry.get("agent_id", ""),
                    "session_id": session.session_id,
                    "identity_prompt": entry.get("identity_prompt", ""),
                    "identity_injected": False,
                    "label_applied": decorated["label_applied"],
                    "badge_applied": decorated["badge_applied"],
                    "agent_id_applied": decorated["agent_id_applied"],
                    "agent_name_applied": decorated["agent_name_applied"],
                    "agent_label_applied": decorated["agent_label_applied"],
                    "tab_title_applied": decorated["tab_title_applied"],
                    "session": session,
                    "entry": entry,
                    "tab": tab,
                }
            )

        identity_injected_count = await _inject_identity_prompts(session_rows, identity_delay_sec=identity_delay_sec)
        relabeled_count = await _reapply_labels_after_wakeup(session_rows)

        session_ids = [str(row.get("session_id", "") or "") for row in session_rows]
        result = {
            "window_id": getattr(window, "window_id", None),
            "session_ids": session_ids,
            "session_rows": [
                {
                    "agent_id": row.get("agent_id", ""),
                    "session_id": row.get("session_id", ""),
                    "identity_injected": bool(row.get("identity_injected", False)),
                    "label_applied": bool(row.get("label_applied", False)),
                    "badge_applied": bool(row.get("badge_applied", False)),
                    "agent_id_applied": bool(row.get("agent_id_applied", False)),
                    "agent_name_applied": bool(row.get("agent_name_applied", False)),
                    "agent_label_applied": bool(row.get("agent_label_applied", False)),
                    "tab_title_applied": bool(row.get("tab_title_applied", False)),
                }
                for row in session_rows
            ],
            "identity_injected_count": identity_injected_count,
            "relabel_applied_count": relabeled_count,
            "layout": {
                "mode": "tabs",
            },
        }

    iterm2.run_until_complete(main)
    return result


def _run_iterm_panes(entries: list[dict[str, Any]], pane_count: int, identity_delay_sec: float) -> dict[str, Any]:
    import iterm2

    cols, rows = _grid_for_count(pane_count)
    result: dict[str, Any] = {}

    async def _equalize_panes(connection) -> bool:
        try:
            await iterm2.MainMenu.async_select_menu_item(connection, "Arrange Split Panes Evenly")
            return True
        except Exception:
            return False

    async def main(connection):
        nonlocal result
        app = await iterm2.async_get_app(connection)

        window = app.current_terminal_window
        if window is None:
            window = await app.async_create_window()
            tab = window.current_tab
        else:
            tab = await window.async_create_tab()

        if tab is None or tab.current_session is None:
            raise RuntimeError("iTerm 窗口初始化失败，未获取到 session")

        root_session = tab.current_session
        sessions = await _build_grid_sessions(root_session, cols=cols, rows=rows)

        if len(sessions) < len(entries):
            raise RuntimeError(f"pane 数不足: got={len(sessions)}, expected={len(entries)}")

        await root_session.async_activate(select_tab=True, order_window_front=True)
        equalized = await _equalize_panes(connection)

        session_rows: list[dict[str, Any]] = []
        for session, entry in zip(sessions, entries):
            decorated = await _decorate_session(entry, session=session, tab=None)
            await session.async_send_text(str(entry.get("shell_cmd", "")) + "\n")
            session_rows.append(
                {
                    "agent_id": entry.get("agent_id", ""),
                    "session_id": session.session_id,
                    "identity_prompt": entry.get("identity_prompt", ""),
                    "identity_injected": False,
                    "label_applied": decorated["label_applied"],
                    "badge_applied": decorated["badge_applied"],
                    "agent_id_applied": decorated["agent_id_applied"],
                    "agent_name_applied": decorated["agent_name_applied"],
                    "agent_label_applied": decorated["agent_label_applied"],
                    "tab_title_applied": False,
                    "session": session,
                    "entry": entry,
                    "tab": None,
                }
            )

        identity_injected_count = await _inject_identity_prompts(session_rows, identity_delay_sec=identity_delay_sec)
        relabeled_count = await _reapply_labels_after_wakeup(session_rows)

        session_ids = [str(row.get("session_id", "") or "") for row in session_rows]
        result = {
            "window_id": getattr(window, "window_id", None),
            "session_ids": session_ids,
            "session_rows": [
                {
                    "agent_id": row.get("agent_id", ""),
                    "session_id": row.get("session_id", ""),
                    "identity_injected": bool(row.get("identity_injected", False)),
                    "label_applied": bool(row.get("label_applied", False)),
                    "badge_applied": bool(row.get("badge_applied", False)),
                    "agent_id_applied": bool(row.get("agent_id_applied", False)),
                    "agent_name_applied": bool(row.get("agent_name_applied", False)),
                    "agent_label_applied": bool(row.get("agent_label_applied", False)),
                    "tab_title_applied": False,
                }
                for row in session_rows
            ],
            "identity_injected_count": identity_injected_count,
            "relabel_applied_count": relabeled_count,
            "layout": {
                "mode": "panes",
                "cols": cols,
                "rows": rows,
                "equalized": equalized,
            },
        }

    iterm2.run_until_complete(main)
    return result


def _validate_start_template(template: str) -> None:
    text = str(template or "").strip()
    if not text:
        raise ValueError("start-template 不能为空")

    try:
        parts = shlex.split(text)
    except ValueError as exc:
        raise ValueError(f"start-template 解析失败: {exc}") from exc

    if not parts:
        raise ValueError("start-template 不能为空")

    command = Path(parts[0]).name.lower()
    if command in {"zsh", "bash", "sh", "fish"}:
        raise ValueError(
            "禁止使用 shell-only 启动命令（zsh/bash/sh/fish）。"
            "请改为可见前台程序，例如: codex --no-alt-screen \"...\""
        )


def parse_args() -> argparse.Namespace:
    root = _project_root()

    parser = argparse.ArgumentParser(description="iTerm 动态启动 Agent")
    parser.add_argument("--task", default="", help="任务描述（用于自动决定数量）")
    parser.add_argument("--tabs", type=int, default=None, help="手动指定数量，只允许 4/5/6/8/12")
    parser.add_argument("--min-tabs", type=int, default=4, help="最小数量（默认 4）")
    parser.add_argument("--max-tabs", type=int, default=12, help="最大数量（默认 12）")
    parser.add_argument("--config", default=str(root / "config.json"), help="拓扑配置路径")
    parser.add_argument("--name-prefix", default="Runtime Agent", help="Agent 名称前缀")
    parser.add_argument("--start-template", default=_default_template(), help="启动命令模板")
    parser.add_argument("--layout", choices=["panes", "tabs"], default="panes", help="布局模式")
    parser.add_argument("--dry-run", action="store_true", help="仅打印命令，不实际拉起")
    parser.add_argument(
        "--inject-identity",
        dest="inject_identity",
        action="store_true",
        default=True,
        help="启动后自动注入每个子代理身份提示（默认开启）",
    )
    parser.add_argument(
        "--no-inject-identity",
        dest="inject_identity",
        action="store_false",
        help="关闭身份注入",
    )
    parser.add_argument(
        "--identity-template",
        default=_default_identity_template(),
        help="身份注入提示词模板，可用变量: {index}/{index_padded}/{agent_id}/{agent_name}",
    )
    parser.add_argument("--identity-delay", type=float, default=1.6, help="身份注入前等待秒数（默认 1.6）")
    parser.add_argument(
        "--state-file",
        default=str(root / "data" / "iterm_launch_state.json"),
        help="启动状态输出文件",
    )

    args = parser.parse_args()
    try:
        _validate_start_template(args.start_template)
    except ValueError as exc:
        parser.error(str(exc))

    if float(args.identity_delay) < 0:
        parser.error("identity-delay 不能小于 0")

    return args


def main() -> int:
    args = parse_args()
    root = _project_root()

    decision = planner_decide_tab_count(
        task=args.task,
        config_path=Path(args.config),
        requested_tabs=args.tabs,
        min_tabs=args.min_tabs,
        max_tabs=args.max_tabs,
    )

    count = int(decision["tab_count"])
    entries = _build_agent_entries(
        count,
        start_template=args.start_template,
        name_prefix=args.name_prefix,
        project_root=root,
        inject_identity=bool(args.inject_identity),
        identity_template=args.identity_template,
    )

    print(f"[Planner] count={count}")
    print(f"[Planner] reason={decision['reason']}")
    print(f"[Planner] layout={args.layout}")
    print(f"[Planner] identity_inject={'on' if args.inject_identity else 'off'}")

    if args.dry_run:
        for entry in entries:
            print(f"\n--- target #{entry['index']} ({entry['agent_id']}) ---")
            print(f"label={entry['session_label']} badge={entry['badge']}")
            print(entry["shell_cmd"])
            if entry.get("identity_prompt"):
                print(f"identity_prompt={entry['identity_prompt']}")
        return 0

    identity_delay_sec = max(0.0, float(args.identity_delay))
    if args.layout == "panes":
        if count not in {4, 6, 8, 12}:
            raise SystemExit("panes 布局仅支持 4/6/8/12；若使用 5 个代理请改用 --layout tabs")
        launch_meta = _run_iterm_panes(entries, pane_count=count, identity_delay_sec=identity_delay_sec)
    else:
        launch_meta = _run_iterm_tabs(entries, identity_delay_sec=identity_delay_sec)

    session_ids = launch_meta.get("session_ids", [])
    session_rows = launch_meta.get("session_rows", [])
    payload = {
        "ts": datetime.now(timezone.utc).isoformat(),
        "count": count,
        "tab_count": count,
        "reason": decision["reason"],
        "task_tabs": decision.get("task_tabs"),
        "arch_tabs": decision.get("arch_tabs"),
        "layout": launch_meta.get("layout", {"mode": args.layout}),
        "identity_injected_count": int(launch_meta.get("identity_injected_count") or 0),
        "relabel_applied_count": int(launch_meta.get("relabel_applied_count") or 0),
        "agents": [
            {
                "index": entry["index"],
                "agent_id": entry["agent_id"],
                "agent_name": entry["agent_name"],
                "session_label": entry["session_label"],
                "badge": entry["badge"],
                "identity_prompt": entry["identity_prompt"],
                "session_id": session_ids[idx] if idx < len(session_ids) else "",
                "identity_injected": bool(session_rows[idx].get("identity_injected")) if idx < len(session_rows) else False,
                "label_applied": bool(session_rows[idx].get("label_applied")) if idx < len(session_rows) else False,
                "badge_applied": bool(session_rows[idx].get("badge_applied")) if idx < len(session_rows) else False,
                "agent_id_applied": bool(session_rows[idx].get("agent_id_applied")) if idx < len(session_rows) else False,
                "agent_name_applied": bool(session_rows[idx].get("agent_name_applied")) if idx < len(session_rows) else False,
                "agent_label_applied": bool(session_rows[idx].get("agent_label_applied")) if idx < len(session_rows) else False,
                "tab_title_applied": bool(session_rows[idx].get("tab_title_applied")) if idx < len(session_rows) else False,
            }
            for idx, entry in enumerate(entries)
        ],
        "window_id": launch_meta.get("window_id"),
        "session_ids": session_ids,
    }
    _write_launch_state(Path(args.state_file), payload)

    layout = payload.get("layout", {})
    if layout.get("mode") == "panes":
        print(
            f"[iTerm] launched panes={count}, grid={layout.get('cols')}x{layout.get('rows')}, "
            f"window_id={launch_meta.get('window_id')}"
        )
    else:
        print(f"[iTerm] launched tabs={count}, window_id={launch_meta.get('window_id')}")

    print(
        f"[iTerm] labels={sum(1 for row in payload['agents'] if row.get('label_applied'))}/{len(payload['agents'])}, "
        f"badges={sum(1 for row in payload['agents'] if row.get('badge_applied'))}/{len(payload['agents'])}, "
        f"identity={payload['identity_injected_count']}/{len(payload['agents'])}, "
        f"relabel={payload['relabel_applied_count']}/{len(payload['agents'])}"
    )
    print(f"[State] {args.state_file}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
