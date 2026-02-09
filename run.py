"""多Agent编排 — CLI 入口"""

import asyncio
import logging
import sys

from dotenv import load_dotenv

load_dotenv()

from master import build_graph
from config.settings import LOG_LEVEL


def setup_logging():
    """配置日志"""
    logging.basicConfig(
        level=getattr(logging, LOG_LEVEL, logging.INFO),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%H:%M:%S",
    )


async def run(task: str):
    """运行 Master 编排"""
    setup_logging()
    logger = logging.getLogger("run")

    logger.info(f"启动多Agent编排系统")
    logger.info(f"任务: {task}")
    print("=" * 60)
    print(f"🚀 任务: {task}")
    print("=" * 60)

    # 构建并运行编排图
    graph = build_graph()
    result = await graph.ainvoke({"task": task})

    print("\n" + "=" * 60)
    print(result["final_answer"])
    print("=" * 60)

    return result


def main():
    """CLI 入口"""
    if len(sys.argv) < 2:
        print("用法: python3 run.py <任务描述>")
        print('示例: python3 run.py "分析系统运行状态并生成报告"')
        sys.exit(1)

    task = " ".join(sys.argv[1:])
    asyncio.run(run(task))


if __name__ == "__main__":
    main()
