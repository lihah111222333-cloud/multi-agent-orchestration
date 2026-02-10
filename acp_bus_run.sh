#!/usr/bin/env bash
# ACP Bus — 启动 Agent MCP Server (stdio 传输)
# 用法: ./acp_bus_run.sh [--name codex]
#
# 热重载: kill -USR1 <pid>   (Python 端 SIGUSR1 handler 会 importlib.reload)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VENV_PYTHON="${SCRIPT_DIR}/.venv/bin/python3"

if [[ -f "${SCRIPT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${SCRIPT_DIR}/.env"
  set +a
fi

exec "${VENV_PYTHON}" -m agents.all_in_one "$@"
