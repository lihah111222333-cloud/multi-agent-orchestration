"""多Agent编排 — CLI 入口"""

import asyncio
import logging
import signal
import sys
import time

from dotenv import load_dotenv

load_dotenv()

from agent_ops_store import create_trace_id, finish_task_trace_span, start_task_trace_span
from audit_log import append_event
from db.postgres import ensure_schema
from master import build_graph
from logging_setup import setup_global_logging
from orchestration_tui_bus import publish_begin, publish_end, publish_update
from utils import validate_config





def setup_signal_handlers() -> None:
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

    # 启动优先执行数据库迁移
    try:
        ensure_schema()
    except Exception as exc:
        logger.error("数据库迁移失败: %s", exc)
        raise SystemExit(1) from exc

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
    trace_id = create_trace_id()
    run_id = f"run-{trace_id}"
    root_span = start_task_trace_span(
        trace_id=trace_id,
        span_name="run.session",
        component="run",
        input_payload={"task": task[:2000]},
        metadata={"entry": "cli"},
    )

    def _publish_tui_status(
        action: str,
        *,
        status_header: str | None = None,
        status_details: str | None = None,
    ) -> None:
        try:
            if action == "begin":
                publish_begin(
                    run_id=run_id,
                    status_header=status_header,
                    status_details=status_details,
                    source="run.py",
                )
            elif action == "update":
                publish_update(
                    run_id=run_id,
                    status_header=status_header,
                    status_details=status_details,
                    source="run.py",
                )
            elif action == "end":
                publish_end(run_id=run_id, source="run.py")
        except Exception as exc:
            logger.debug("publish_tui_status ignored: %s", exc)

    _publish_tui_status(
        "begin",
        status_header="Running orchestration",
        status_details=f"task={task[:120]}",
    )

    try:
        # 构建并运行编排图
        _publish_tui_status("update", status_details="phase=build_graph")
        graph = build_graph()
        _publish_tui_status("update", status_details="phase=invoke_graph")
        result = await graph.ainvoke(
            {
                "task": task,
                "trace_id": trace_id,
                "root_span_id": str(root_span.get("span_id", "")),
            }
        )

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
        finish_task_trace_span(
            span_id=str(root_span.get("span_id", "")),
            status="ok",
            output_payload={
                "trace_id": trace_id,
                "final_answer": str(result.get("final_answer", ""))[:4000],
            },
            metadata={"elapsed_sec": round(elapsed, 3)},
        )
        _publish_tui_status(
            "update",
            status_header="Orchestration completed",
            status_details=f"elapsed={elapsed:.2f}s",
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
        finish_task_trace_span(
            span_id=str(root_span.get("span_id", "")),
            status="error",
            output_payload={"trace_id": trace_id},
            error_text=str(e),
            metadata={"elapsed_sec": round(elapsed, 3)},
        )
        _publish_tui_status(
            "update",
            status_header="Orchestration failed",
            status_details=f"elapsed={elapsed:.2f}s,error={str(e)[:200]}",
        )
        raise RuntimeError(str(e)) from e
    finally:
        _publish_tui_status("end")


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
    except Exception as exc:
        print(f"\n❌ 执行失败: {exc}")
        sys.exit(1)


if __name__ == "__main__":
    main()
