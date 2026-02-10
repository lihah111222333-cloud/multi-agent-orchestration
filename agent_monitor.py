"""Agent monitor helpers."""

from __future__ import annotations

from collections.abc import Iterable

ERROR_KEYWORDS = ("traceback", "error", "exception")
DISCONNECTED_KEYWORDS = ("timeout", "connection refused", "econnreset")
PROMPT_ONLY_MARKERS = ("$", "#", ">>>", "...", ">")


def _normalize_lines(lines: Iterable[object]) -> list[str]:
    """Normalize raw output lines into stripped text rows."""
    normalized: list[str] = []
    for item in lines:
        text = str(item).strip()
        if text:
            normalized.append(text)
    return normalized


def _is_prompt_only(lines: list[str]) -> bool:
    """Return whether all visible lines are shell/python prompts."""
    if not lines:
        return True
    return all(line in PROMPT_ONLY_MARKERS for line in lines)


def classify_status(
    lines: list[str],
    has_session: bool = True,
    stagnant_sec: int = 0,
) -> str:
    """Classify agent runtime status from output snippets.

    Args:
        lines: Recent terminal output lines.
        has_session: Whether backing session exists.
        stagnant_sec: Seconds since last output change.

    Returns:
        One of: running/idle/stuck/error/disconnected/unknown.
    """
    if not has_session:
        return "unknown"

    normalized = _normalize_lines(lines)
    if _is_prompt_only(normalized):
        return "idle"

    merged = "\n".join(normalized).lower()

    if any(keyword in merged for keyword in ERROR_KEYWORDS):
        return "error"

    if any(keyword in merged for keyword in DISCONNECTED_KEYWORDS):
        return "disconnected"

    if int(stagnant_sec) >= 60:
        return "stuck"

    return "running"

