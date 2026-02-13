#!/usr/bin/env python3
"""Print orchestration_tui_bus snapshot and recent events as JSON."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

ROOT_DIR = Path(__file__).resolve().parents[1]
if str(ROOT_DIR) not in sys.path:
    sys.path.insert(0, str(ROOT_DIR))

from orchestration_tui_bus import get_snapshot, list_events  # noqa: E402


def main() -> int:
    parser = argparse.ArgumentParser(description="Show orchestration_tui_bus state")
    parser.add_argument("--limit", type=int, default=120, help="Number of events to return")
    parser.add_argument("--since-seq", type=int, default=0, help="Only events with seq > since_seq")
    args = parser.parse_args()

    payload = {
        "snapshot": get_snapshot(),
        "events": list_events(limit=max(1, int(args.limit)), since_seq=max(0, int(args.since_seq))),
    }
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
