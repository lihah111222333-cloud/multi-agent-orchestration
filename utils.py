"""公共工具函数"""

import logging
import asyncio
from typing import Any

logger = logging.getLogger(__name__)


def extract_text(content: Any) -> str:
    """从 LLM 响应中提取文本（兼容 list / str 格式）

    第三方 OpenAI API 可能返回 Responses API 格式（list of dicts），
    而标准 API 返回 str。此函数统一处理。

    Args:
        content: LLM response.content，可能是 str 或 list

    Returns:
        提取的文本字符串
    """
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


def validate_config():
    """启动前校验关键配置

    Raises:
        SystemExit: 配置校验失败时终止程序
    """
    from config.settings import OPENAI_API_KEY, OPENAI_BASE_URL, LLM_MODEL

    errors = []

    if not OPENAI_API_KEY:
        errors.append(
            "OPENAI_API_KEY 未设置。"
            "请在 .env 文件中配置或设置环境变量。"
        )

    if errors:
        logger.error("=" * 50)
        logger.error("❌ 配置校验失败:")
        for e in errors:
            logger.error(f"  - {e}")
        logger.error("=" * 50)
        raise SystemExit(1)

    # 信息性日志
    base_url_display = OPENAI_BASE_URL or "https://api.openai.com/v1 (默认)"
    logger.info(f"配置校验通过: model={LLM_MODEL}, base_url={base_url_display}")


async def run_with_timeout(coro, timeout: float, description: str = "操作"):
    """带超时的异步执行包装

    Args:
        coro: 要执行的协程
        timeout: 超时秒数
        description: 操作描述（用于错误信息）

    Returns:
        协程返回值

    Raises:
        TimeoutError: 超时时抛出，附带描述信息
    """
    try:
        return await asyncio.wait_for(coro, timeout=timeout)
    except asyncio.TimeoutError:
        raise TimeoutError(f"{description} 超时 ({timeout}s)")
