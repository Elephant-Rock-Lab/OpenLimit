"""SSE streaming parser for the OpenLimit SDK."""

from __future__ import annotations

from typing import Any, Generator
import json
from .types import ChatCompletionChunk, _parse_chunk


def parse_sse_lines(lines: list[str]) -> Generator[ChatCompletionChunk, None, None]:
    """Parse SSE text lines into ChatCompletionChunk objects."""
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith(":"):
            continue
        if stripped == "data: [DONE]":
            return
        if not stripped.startswith("data: "):
            continue
        json_str = stripped[6:]
        try:
            data: dict[str, Any] = json.loads(json_str)
            yield _parse_chunk(data)
        except (json.JSONDecodeError, KeyError):
            continue
