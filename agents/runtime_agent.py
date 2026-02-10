"""动态 Agent 运行入口"""

import argparse

from agents.factory import run_agent_by_key


def main() -> None:
    parser = argparse.ArgumentParser(description="动态 MCP Agent 入口")
    parser.add_argument("--id", required=True, help="agent key，例如 agent_13")
    parser.add_argument("--name", default="", help="agent 显示名称")
    args = parser.parse_args()

    run_agent_by_key(args.id, agent_name=args.name)


if __name__ == "__main__":
    main()
