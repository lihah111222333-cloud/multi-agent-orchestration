"""动态 Agent 运行入口"""

import argparse

from agents.factory import run_agent_by_key


def main() -> None:
    parser = argparse.ArgumentParser(description="动态 MCP Agent 入口")
    parser.add_argument("--id", required=True, help="agent key，例如 a13")
    parser.add_argument("--name", default="", help="agent 显示名称")
    parser.add_argument(
        "--plugins",
        default="",
        help="逗号分隔插件名，例如 http_fetch,db_query",
    )
    args = parser.parse_args()

    plugin_names = tuple(
        name.strip()
        for name in str(args.plugins or "").split(",")
        if name.strip()
    )
    run_agent_by_key(args.id, agent_name=args.name, plugin_names=plugin_names)


if __name__ == "__main__":
    main()
