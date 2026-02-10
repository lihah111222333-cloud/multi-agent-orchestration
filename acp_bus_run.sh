#!/usr/bin/env bash
# ACP Bus — 启动全量 Agent MCP Server (25 tools, stdio 传输)
# 用法: ./acp_bus_run.sh [--name codex]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VENV_PYTHON="${SCRIPT_DIR}/.venv/bin/python3"

exec "${VENV_PYTHON}" -m agents.all_in_one "$@"
