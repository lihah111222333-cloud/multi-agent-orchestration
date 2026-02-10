"""公共工具函数。"""

from __future__ import annotations

import asyncio
import logging
import os
from typing import Any, Optional

logger = logging.getLogger(__name__)


def extract_text(content: Any) -> str:
    """从 LLM 响应中提取文本（兼容 list / str 格式）。"""
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        texts = []
        for item in content:
            if isinstance(item, dict) and item.get("type") == "text":
                texts.append(item["text"])
            elif isinstance(item, str):
                texts.append(item)
        return "\n".join(texts) if texts else str(content)
    return str(content)


def as_int_env(name: str, default: int, min_value: int = 0) -> int:
    """读取并规范化整型环境变量。"""
    raw = os.getenv(name, str(default))
    try:
        value = int(raw)
    except (TypeError, ValueError):
        value = default
    return max(min_value, value)


def as_float_env(name: str, default: float, min_value: float = 0.0) -> float:
    """读取并规范化浮点型环境变量。"""
    raw = os.getenv(name, str(default))
    try:
        value = float(raw)
    except (TypeError, ValueError):
        value = default
    return max(min_value, value)


def build_chat_openai(
    model: str,
    temperature: float,
    base_url: Optional[str],
    max_retries: int,
    request_timeout: int,
) -> Any:
    """构建 ChatOpenAI 实例（延迟导入，便于测试替换）。"""
    from langchain_openai import ChatOpenAI

    return ChatOpenAI(
        model=model,
        temperature=temperature,
        base_url=base_url,
        max_retries=max_retries,
        request_timeout=request_timeout,
    )


def validate_config() -> None:
    """启动前校验关键配置。

    Raises:
        SystemExit: 配置校验失败时终止程序
    """
    from config.settings import OPENAI_API_KEY, OPENAI_BASE_URL, LLM_MODEL
    from db.postgres import ensure_schema

    errors = []

    if not OPENAI_API_KEY:
        errors.append(
            "OPENAI_API_KEY 未设置。"
            "请在 .env 文件中配置或设置环境变量。"
        )

    if not (os.getenv("POSTGRES_CONNECTION_STRING") or os.getenv("DATABASE_URL")):
        errors.append("POSTGRES_CONNECTION_STRING / DATABASE_URL 未设置。")

    if errors:
        logger.error("=" * 50)
        logger.error("❌ 配置校验失败:")
        for message in errors:
            logger.error("  - %s", message)
        logger.error("=" * 50)
        raise SystemExit(1)

    try:
        ensure_schema()
    except Exception as exc:
        logger.error("PostgreSQL 不可用: %s", exc)
        raise SystemExit(1) from exc

    base_url_display = OPENAI_BASE_URL or "https://api.openai.com/v1 (默认)"
    logger.info("配置校验通过: model=%s, base_url=%s", LLM_MODEL, base_url_display)


async def run_with_timeout(coro: Any, timeout: float, description: str = "操作") -> Any:
    """带超时的异步执行包装。"""
    try:
        return await asyncio.wait_for(coro, timeout=timeout)
    except asyncio.TimeoutError as exc:
        raise TimeoutError(f"{description} 超时 ({timeout}s)") from exc


def normalize_limit(limit: int, default: int = 100, max_value: int = 1000) -> int:
    """Clamp a query limit parameter to [1, max_value]."""
    try:
        value = int(limit)
    except (TypeError, ValueError):
        value = default
    return max(1, min(value, max_value))


def escape_like(value: str) -> str:
    """Escape special characters (%, _, \\\\) for SQL LIKE patterns."""
    return str(value).replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
