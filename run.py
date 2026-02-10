"""多Agent编排 — CLI 入口"""

import asyncio
import logging
import signal
import sys
import time

from dotenv import load_dotenv

load_dotenv()

from audit_log import append_event
from master import build_graph
from logging_setup import setup_global_logging
from utils import validate_config





def setup_signal_handlers():
    """设置信号处理，支持 Ctrl+C 优雅退出"""

    def handler(sig, frame):
        print("\n\n⚠️  收到中断信号，正在停止任务并清理资源...")
        raise KeyboardInterrupt

    signal.signal(signal.SIGINT, handler)
    signal.signal(signal.SIGTERM, handler)


async def run(task: str):
    """运行 Master 编排"""
    setup_global_logging()
    logger = logging.getLogger("run")

    # 启动前校验配置
    validate_config()

    logger.info("启动多Agent编排系统")
    logger.info("任务: %s", task)
    append_event(
        event_type="runtime",
        action="run_start",
        actor="cli",
        target="master",
        result="ok",
        detail=f"task={task[:120]}",
    )
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

        append_event(
            event_type="runtime",
            action="run_finish",
            actor="cli",
            target="master",
            result="ok",
            detail=f"elapsed={elapsed:.2f}s",
        )
        return result

    except Exception as e:
        elapsed = time.time() - start_time
        logger.error("编排执行失败 (%.1fs): %s", elapsed, e)
        append_event(
            event_type="runtime",
            action="run_finish",
            actor="cli",
            target="master",
            result="error",
            detail=f"elapsed={elapsed:.2f}s,error={e}",
        )
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
    try:
        asyncio.run(run(task))
    except KeyboardInterrupt:
        print("\n🛑 已中断执行")
        sys.exit(130)


if __name__ == "__main__":
    main()
