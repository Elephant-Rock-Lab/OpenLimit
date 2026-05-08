"""Type definitions for the OpenLimit SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class ChatMessage:
    role: str
    content: str
    name: str | None = None
    tool_call_id: str | None = None


@dataclass
class ToolCallFunction:
    name: str
    arguments: str


@dataclass
class ToolCall:
    id: str
    type: str = "function"
    function: ToolCallFunction | None = None


@dataclass
class ChatCompletionRequest:
    model: str
    messages: list[ChatMessage]
    temperature: float | None = None
    top_p: float | None = None
    max_tokens: int | None = None
    stream: bool = False
    stop: str | list[str] | None = None
    presence_penalty: float | None = None
    frequency_penalty: float | None = None
    user: str | None = None
    data_residency: str | None = None


@dataclass
class Usage:
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


@dataclass
class ChatChoice:
    index: int = 0
    message: ChatMessage | None = None
    finish_reason: str = ""


@dataclass
class ChatCompletionResponse:
    id: str = ""
    object: str = "chat.completion"
    created: int = 0
    model: str = ""
    choices: list[ChatChoice] = field(default_factory=list)
    usage: Usage | None = None


@dataclass
class ChunkDelta:
    role: str | None = None
    content: str | None = None


@dataclass
class ChunkChoice:
    index: int = 0
    delta: ChunkDelta = field(default_factory=ChunkDelta)
    finish_reason: str | None = None


@dataclass
class ChatCompletionChunk:
    id: str = ""
    object: str = "chat.completion.chunk"
    created: int = 0
    model: str = ""
    choices: list[ChunkChoice] = field(default_factory=list)


@dataclass
class EmbeddingsRequest:
    model: str
    input: str | list[str]
    encoding_format: str | None = None
    dimensions: int | None = None
    user: str | None = None


@dataclass
class EmbeddingData:
    object: str = "embedding"
    embedding: list[float] = field(default_factory=list)
    index: int = 0


@dataclass
class EmbeddingsResponse:
    object: str = "list"
    data: list[EmbeddingData] = field(default_factory=list)
    model: str = ""
    usage: Usage | None = None


@dataclass
class ModelInfo:
    id: str = ""
    object: str = "model"
    created: int = 0
    owned_by: str = ""


@dataclass
class ModelsResponse:
    object: str = "list"
    data: list[ModelInfo] = field(default_factory=list)


@dataclass
class HealthResponse:
    status: str = ""
    version: str | None = None
    uptime_seconds: float | None = None


@dataclass
class ErrorBody:
    message: str = ""
    type: str = ""
    code: str | None = None
    request_id: str | None = None


@dataclass
class ErrorResponse:
    error: ErrorBody = field(default_factory=ErrorBody)


def _parse_chat_response(data: dict[str, Any]) -> ChatCompletionResponse:
    usage = None
    if "usage" in data and data["usage"]:
        u = data["usage"]
        usage = Usage(
            prompt_tokens=u.get("prompt_tokens", 0),
            completion_tokens=u.get("completion_tokens", 0),
            total_tokens=u.get("total_tokens", 0),
        )
    choices = []
    for c in data.get("choices", []):
        msg = c.get("message", {})
        choices.append(ChatChoice(
            index=c.get("index", 0),
            message=ChatMessage(
                role=msg.get("role", ""),
                content=msg.get("content", ""),
            ),
            finish_reason=c.get("finish_reason", ""),
        ))
    return ChatCompletionResponse(
        id=data.get("id", ""),
        created=data.get("created", 0),
        model=data.get("model", ""),
        choices=choices,
        usage=usage,
    )


def _parse_chunk(data: dict[str, Any]) -> ChatCompletionChunk:
    choices = []
    for c in data.get("choices", []):
        delta = c.get("delta", {})
        choices.append(ChunkChoice(
            index=c.get("index", 0),
            delta=ChunkDelta(
                role=delta.get("role"),
                content=delta.get("content"),
            ),
            finish_reason=c.get("finish_reason"),
        ))
    return ChatCompletionChunk(
        id=data.get("id", ""),
        created=data.get("created", 0),
        model=data.get("model", ""),
        choices=choices,
    )


def _parse_embeddings(data: dict[str, Any]) -> EmbeddingsResponse:
    items = []
    for d in data.get("data", []):
        items.append(EmbeddingData(
            object=d.get("object", "embedding"),
            embedding=d.get("embedding", []),
            index=d.get("index", 0),
        ))
    usage = None
    if "usage" in data and data["usage"]:
        u = data["usage"]
        usage = Usage(
            prompt_tokens=u.get("prompt_tokens", 0),
            completion_tokens=u.get("completion_tokens", 0),
            total_tokens=u.get("total_tokens", 0),
        )
    return EmbeddingsResponse(
        data=items,
        model=data.get("model", ""),
        usage=usage,
    )


def _parse_models(data: dict[str, Any]) -> ModelsResponse:
    items = []
    for m in data.get("data", []):
        items.append(ModelInfo(
            id=m.get("id", ""),
            created=m.get("created", 0),
            owned_by=m.get("owned_by", ""),
        ))
    return ModelsResponse(data=items)
