"""Agent 运行时控制

目标：
- 每个 Agent 进程维持 1 个工作线程执行工具逻辑
- 后台定期触发 gc.collect，降低长时间运行的内存压力
"""

from __future__ import annotations

import atexit
import gc
import logging
import os
import sys
import threading
from concurrent.futures import ThreadPoolExecutor
from typing import Callable, Optional, TypeVar

logger = logging.getLogger(__name__)

T = TypeVar("T")

_EXECUTOR: Optional[ThreadPoolExecutor] = None
_EXECUTOR_LOCK = threading.Lock()
_GC_THREAD: Optional[threading.Thread] = None
_GC_LOCK = threading.Lock()
_GC_STOP_EVENT = threading.Event()
_RUNTIME_READY = False
_RUNTIME_LOCK = threading.Lock()
_WORKER_THREAD_ID: Optional[int] = None


def _as_bool(raw: str, default: bool = True) -> bool:
    if raw is None:
        return default
    text = str(raw).strip().lower()
    if text in ("1", "true", "yes", "on"):
        return True
    if text in ("0", "false", "no", "off"):
        return False
    return default


def _as_float(raw: str, default: float, min_value: float) -> float:
    try:
        value = float(raw)
    except (TypeError, ValueError):
        value = default
    return max(value, min_value)


def _get_executor() -> ThreadPoolExecutor:
    global _EXECUTOR
    with _EXECUTOR_LOCK:
        if _EXECUTOR is None:
            _EXECUTOR = ThreadPoolExecutor(max_workers=1, thread_name_prefix="agent-worker")
        return _EXECUTOR


def _memory_mb() -> Optional[float]:
    try:
        import resource

        rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
        if sys.platform == "darwin":
            return rss / (1024 * 1024)
        return rss / 1024
    except Exception:
        return None


def _mark_worker_thread() -> None:
    global _WORKER_THREAD_ID
    _WORKER_THREAD_ID = threading.get_ident()


def run_in_agent_thread(fn: Callable[[], T]) -> T:
    """在 Agent 唯一工作线程中执行函数，并同步返回结果。"""

    # 避免同线程重入时把任务再次提交到单线程池导致死锁
    if _WORKER_THREAD_ID is not None and threading.get_ident() == _WORKER_THREAD_ID:
        return fn()

    executor = _get_executor()
    future = executor.submit(fn)
    return future.result()


def _gc_loop(agent_id: str, interval_sec: float, generation: int, warn_mb: float) -> None:
    logger.info("[%s] 启动 GC 线程: interval=%.1fs generation=%s", agent_id, interval_sec, generation)
    while not _GC_STOP_EVENT.wait(interval_sec):
        try:
            collected = gc.collect(generation)
            if warn_mb > 0:
                usage = _memory_mb()
                if usage is not None and usage >= warn_mb:
                    logger.warning(
                        "[%s] 内存告警: %.1fMB >= %.1fMB, collected=%s",
                        agent_id,
                        usage,
                        warn_mb,
                        collected,
                    )
        except Exception as e:
            logger.warning("[%s] GC 线程异常: %s", agent_id, e)


def start_periodic_gc(agent_id: str) -> None:
    global _GC_THREAD

    enabled = _as_bool(os.getenv("AGENT_GC_ENABLED", "1"), default=True)
    if not enabled:
        logger.info("[%s] AGENT_GC_ENABLED=0，跳过 GC 线程", agent_id)
        return

    interval_sec = _as_float(os.getenv("AGENT_GC_INTERVAL_SEC", "30"), default=30.0, min_value=1.0)
    raw_generation_text = os.getenv("AGENT_GC_GENERATION", "2")
    try:
        raw_generation = int(raw_generation_text)
    except (TypeError, ValueError):
        raw_generation = 2
    generation = max(0, min(raw_generation, 2))
    warn_mb = _as_float(os.getenv("AGENT_MEMORY_WARN_MB", "0"), default=0.0, min_value=0.0)

    if raw_generation != generation:
        logger.warning(
            "[%s] AGENT_GC_GENERATION=%s 超出范围，已自动修正为 %s",
            agent_id,
            raw_generation,
            generation,
        )

    with _GC_LOCK:
        if _GC_THREAD is not None and _GC_THREAD.is_alive():
            return

        _GC_STOP_EVENT.clear()
        thread = threading.Thread(
            target=_gc_loop,
            args=(agent_id, interval_sec, generation, warn_mb),
            name=f"{agent_id}-gc",
            daemon=True,
        )
        thread.start()
        _GC_THREAD = thread


def initialize_agent_runtime(agent_id: str) -> None:
    """初始化 Agent 运行时：预热 1 工作线程 + 启动 GC 线程。"""

    global _RUNTIME_READY
    with _RUNTIME_LOCK:
        if _RUNTIME_READY:
            return

        run_in_agent_thread(_mark_worker_thread)
        start_periodic_gc(agent_id)
        _RUNTIME_READY = True


def shutdown_runtime() -> None:
    """优雅停止（供退出钩子和测试使用）。"""

    global _EXECUTOR, _GC_THREAD, _RUNTIME_READY, _WORKER_THREAD_ID

    with _RUNTIME_LOCK:
        _GC_STOP_EVENT.set()
        with _GC_LOCK:
            if _GC_THREAD is not None and _GC_THREAD.is_alive():
                _GC_THREAD.join(timeout=1.0)
            _GC_THREAD = None

        with _EXECUTOR_LOCK:
            if _EXECUTOR is not None:
                _EXECUTOR.shutdown(wait=False, cancel_futures=True)
                _EXECUTOR = None

        _WORKER_THREAD_ID = None
        _RUNTIME_READY = False


def runtime_status() -> dict:
    return {
        "worker_ready": _EXECUTOR is not None,
        "gc_running": _GC_THREAD is not None and _GC_THREAD.is_alive(),
        "runtime_ready": _RUNTIME_READY,
    }


atexit.register(shutdown_runtime)
