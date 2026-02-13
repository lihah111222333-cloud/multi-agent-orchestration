"""公共工具函数。"""

from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import Any, Optional

logger = logging.getLogger(__name__)

_TRUE_VALUES = {"1", "true", "yes", "on"}
_FALSE_VALUES = {"0", "false", "no", "off"}


def _parse_bool_env(name: str) -> Optional[bool]:
    raw = str(os.getenv(name, "") or "").strip().lower()
    if raw in _TRUE_VALUES:
        return True
    if raw in _FALSE_VALUES:
        return False
    return None


def _bool_env(name: str, default: bool) -> bool:
    parsed = _parse_bool_env(name)
    return default if parsed is None else parsed


def _is_third_party_base_url(base_url: Optional[str]) -> bool:
    if not base_url:
        return False
    return "api.openai.com" not in str(base_url).strip().lower()


def _sanitize_responses_input_items(
    items: list[Any],
    *,
    strip_ids: bool,
    drop_reasoning: bool,
) -> list[Any]:
    sanitized: list[Any] = []
    for item in items:
        if not isinstance(item, dict):
            sanitized.append(item)
            continue

        item_type = str(item.get("type") or "")
        if drop_reasoning and item_type == "reasoning":
            continue

        cleaned_item: dict[str, Any] = {}
        for key, value in item.items():
            if strip_ids and key == "id":
                continue

            if key == "content" and isinstance(value, list):
                cleaned_content: list[Any] = []
                for block in value:
                    if not isinstance(block, dict):
                        cleaned_content.append(block)
                        continue

                    block_type = str(block.get("type") or "")
                    if drop_reasoning and block_type == "reasoning":
                        continue

                    cleaned_block = {
                        block_key: block_value
                        for block_key, block_value in block.items()
                        if not (strip_ids and block_key == "id")
                    }
                    cleaned_content.append(cleaned_block)

                cleaned_item[key] = cleaned_content
                continue

            cleaned_item[key] = value

        sanitized.append(cleaned_item)

    return sanitized


def _load_chat_openai_class() -> type:
    from langchain_openai import ChatOpenAI

    return ChatOpenAI


def _parse_json_env(name: str) -> Any:
    raw = str(os.getenv(name, "") or "").strip()
    if not raw:
        return None
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        logger.warning("%s 不是合法 JSON，已忽略", name)
        return None


def extract_text(content: Any) -> str:
    """从 LLM 响应中提取文本（支持 list / str 格式）。"""
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
    chat_openai_cls = _load_chat_openai_class()

    responses_env = str(os.getenv("OPENAI_USE_RESPONSES_API", "") or "").strip().lower()
    if responses_env in {"1", "true", "yes", "on"}:
        use_responses_api = True
    elif responses_env in {"0", "false", "no", "off"}:
        use_responses_api = False
    else:
        model_text = str(model or "").strip().lower()
        # 对 GPT-5 / Codex 模型优先走 Responses API，适配第三方中转网关。
        use_responses_api = "gpt-5" in model_text or "codex" in model_text

    kwargs: dict[str, Any] = {
        "model": model,
        "temperature": temperature,
        "base_url": base_url,
        "max_retries": max_retries,
        "request_timeout": request_timeout,
        "use_responses_api": use_responses_api,
    }

    reasoning_effort = str(os.getenv("OPENAI_REASONING_EFFORT", "") or "").strip().lower()
    if use_responses_api and reasoning_effort in {"low", "medium", "high"}:
        kwargs["reasoning"] = {"effort": reasoning_effort}

    prev_resp_env = str(os.getenv("OPENAI_USE_PREVIOUS_RESPONSE_ID", "") or "").strip().lower()
    if prev_resp_env in {"1", "true", "yes", "on"}:
        kwargs["use_previous_response_id"] = True
    elif prev_resp_env in {"0", "false", "no", "off"}:
        kwargs["use_previous_response_id"] = False
    elif use_responses_api:
        # 某些第三方网关不持久化 response item，默认关闭链式 response_id 续写。
        kwargs["use_previous_response_id"] = False

    output_version = str(os.getenv("OPENAI_OUTPUT_VERSION", "") or "").strip()
    if use_responses_api and output_version:
        kwargs["output_version"] = output_version

    store_env = str(os.getenv("OPENAI_RESPONSES_STORE", "") or "").strip().lower()
    if use_responses_api:
        if store_env in {"0", "false", "no", "off"}:
            kwargs["store"] = False
        else:
            # 默认开启 store，兼容需要引用 response item 的多轮工具调用链路。
            kwargs["store"] = True

    if use_responses_api:
        responses_model_kwargs: dict[str, Any] = {}

        background = _parse_bool_env("OPENAI_RESPONSES_BACKGROUND")
        if background is not None:
            responses_model_kwargs["background"] = background

        conversation_id = str(os.getenv("OPENAI_RESPONSES_CONVERSATION_ID", "") or "").strip()
        if conversation_id:
            responses_model_kwargs["conversation"] = {"id": conversation_id}

        context_management = _parse_json_env("OPENAI_RESPONSES_CONTEXT_MANAGEMENT")
        if isinstance(context_management, list):
            responses_model_kwargs["context_management"] = context_management

        include_raw = str(os.getenv("OPENAI_RESPONSES_INCLUDE", "") or "").strip()
        if include_raw:
            include_values = [item.strip() for item in include_raw.split(",") if item.strip()]
            if include_values:
                kwargs["include"] = include_values

        verbosity = str(os.getenv("OPENAI_RESPONSES_VERBOSITY", "") or "").strip().lower()
        if verbosity in {"low", "medium", "high"}:
            responses_model_kwargs["text"] = {"verbosity": verbosity}

        if responses_model_kwargs:
            kwargs["model_kwargs"] = responses_model_kwargs

        class _ResponsesCompatChatOpenAI(chat_openai_cls):
            def _get_request_payload(self, input_: Any, *, stop: Optional[list[str]] = None, **inner_kwargs: Any) -> dict:
                payload = super()._get_request_payload(input_, stop=stop, **inner_kwargs)
                if not isinstance(payload, dict):
                    return payload

                input_items = payload.get("input")
                if not isinstance(input_items, list):
                    return payload

                base_url_value = getattr(self, "openai_api_base", None) or base_url
                default_sanitize = _is_third_party_base_url(base_url_value)
                sanitize_input = _bool_env("OPENAI_RESPONSES_SANITIZE_INPUT", default_sanitize)
                if not sanitize_input:
                    return payload

                strip_ids = _bool_env("OPENAI_RESPONSES_STRIP_INPUT_IDS", True)
                drop_reasoning = _bool_env("OPENAI_RESPONSES_DROP_REASONING_INPUT", default_sanitize)
                payload["input"] = _sanitize_responses_input_items(
                    input_items,
                    strip_ids=strip_ids,
                    drop_reasoning=drop_reasoning,
                )
                return payload

        chat_openai_cls = _ResponsesCompatChatOpenAI

    return chat_openai_cls(**kwargs)


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

    if not os.getenv("POSTGRES_CONNECTION_STRING"):
        errors.append("POSTGRES_CONNECTION_STRING 未设置。")

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
