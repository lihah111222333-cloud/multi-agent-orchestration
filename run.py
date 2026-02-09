"""多Agent编排 — CLI 入口"""

import asyncio
import logging
import signal
import sys
import time

from dotenv import load_dotenv

load_dotenv()

from master import build_graph
from config.settings import LOG_LEVEL
from utils import validate_config


def setup_logging():
    """配置日志"""
    logging.basicConfig(
        level=getattr(logging, LOG_LEVEL, logging.INFO),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%H:%M:%S",
    )


def setup_signal_handlers():
    """设置信号处理，支持 Ctrl+C 优雅退出"""
    def handler(sig, frame):
        print("\n\n⚠️  收到中断信号，正在退出...")
        # 给 asyncio 一个机会清理
        sys.exit(130)

    signal.signal(signal.SIGINT, handler)
    signal.signal(signal.SIGTERM, handler)


async def run(task: str):
    """运行 Master 编排"""
    setup_logging()
    logger = logging.getLogger("run")

    # 启动前校验配置
    validate_config()

    logger.info("启动多Agent编排系统")
    logger.info(f"任务: {task}")
    print("=" * 60)
    print(f"🚀 任务: {task}")
    print("=" * 60)

    start_time = time.time()

    try:
        # 构建并运行编排图
        graph = build_graph()
        result = await graph.ainvoke({"task": task})

        elapsed = time.time() - start_time

        print("\n" + "=" * 60)
        print(result["final_answer"])
        print("=" * 60)
        print(f"\n⏱️  总耗时: {elapsed:.1f}s")

        return result

    except Exception as e:
        elapsed = time.time() - start_time
        logger.error(f"编排执行失败 ({elapsed:.1f}s): {e}")
        print(f"\n❌ 执行失败: {e}")
        sys.exit(1)


def main():
    """CLI 入口"""
    if len(sys.argv) < 2:
        print("用法: python3 run.py <任务描述>")
        print('示例: python3 run.py "分析系统运行状态并生成报告"')
        sys.exit(1)

    setup_signal_handlers()
    task = " ".join(sys.argv[1:])
    asyncio.run(run(task))


if __name__ == "__main__":
    main()
