#!/usr/bin/env bash
# ACP Bus — 启动 Agent MCP Server (HTTP 守护进程)
#
# 默认使用 streamable-http 传输，监听 127.0.0.1:9100
# 可通过环境变量覆盖:
#   ACP_BUS_HOST       (默认 127.0.0.1)
#   ACP_BUS_PORT       (默认 9100)
#   ACP_BUS_TRANSPORT  (默认 streamable-http, 可选 stdio)
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

export ACP_BUS_HOST="${ACP_BUS_HOST:-127.0.0.1}"
export ACP_BUS_PORT="${ACP_BUS_PORT:-9100}"
export ACP_BUS_TRANSPORT="${ACP_BUS_TRANSPORT:-streamable-http}"

exec "${VENV_PYTHON}" -m agents.all_in_one "$@"
