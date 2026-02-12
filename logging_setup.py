"""统一日志初始化（控制台 + PostgreSQL）。"""

from __future__ import annotations

import logging
import os
from datetime import datetime, timezone

from db.postgres import ensure_schema
from system_log import append_log

class PostgresLogHandler(logging.Handler):
    """将系统日志写入 PostgreSQL。"""

    _EXCLUDED_LOGGER_PREFIXES = ("psycopg", "db.postgres", "system_log.")
    _EXCLUDED_LOGGER_EXACT = frozenset({"system_log", "logging_setup"})

    def emit(self, record: logging.LogRecord) -> None:  # pragma: no cover - 通过集成测试覆盖
        # 避免日志落库链路上的 logger 触发递归
        if record.name.startswith(self._EXCLUDED_LOGGER_PREFIXES) or record.name in self._EXCLUDED_LOGGER_EXACT:
            return

        try:
            event_time = datetime.fromtimestamp(record.created, tz=timezone.utc)
            append_log(
                level=record.levelname,
                logger_name=record.name,
                message=record.getMessage(),
                raw=self.format(record),
                ts=event_time,
            )
        except Exception:
            # 日志落库不能影响主流程
            self.handleError(record)


def setup_global_logging(default_level: str = "INFO") -> None:
    # Use the explicit call-site level first; callers can still pass
    # os.getenv("LOG_LEVEL", "...") when they want env-driven behavior.
    level_name = str(default_level or "").upper() or os.getenv("LOG_LEVEL", "INFO").upper()
    level = getattr(logging, level_name, logging.INFO)

    # Restore logging if a previous test/runtime called logging.disable(...).
    logging.disable(logging.NOTSET)

    root = logging.getLogger()
    root.setLevel(level)

    formatter = logging.Formatter(
        "%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )

    has_stream = any(isinstance(h, logging.StreamHandler) for h in root.handlers)
    has_pg = any(isinstance(h, PostgresLogHandler) for h in root.handlers)

    if not has_stream:
        stream = logging.StreamHandler()
        stream.setFormatter(formatter)
        stream.setLevel(level)
        root.addHandler(stream)
    else:
        for handler in root.handlers:
            handler.setLevel(level)

    # D12: DB 不可用时降级为仅控制台日志，不阻塞启动
    if not has_pg:
        try:
            ensure_schema()
            pg_handler = PostgresLogHandler()
            pg_handler.setFormatter(formatter)
            pg_handler.setLevel(level)
            root.addHandler(pg_handler)
        except Exception:
            import sys
            print(
                "[WARNING] PostgreSQL 日志初始化失败，降级为仅控制台日志",
                file=sys.stderr,
            )
