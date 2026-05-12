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
class ResponseHeaders:
    """Operational headers returned by the gateway."""
    x_provider: str | None = None
    x_cache: str | None = None
    x_cost_usd: str | None = None
    x_ratelimit_limit: str | None = None
    x_ratelimit_remaining: str | None = None
    x_ratelimit_reset: str | None = None
    x_request_id: str | None = None


@dataclass
class ChatCompletionResponse:
    id: str = ""
    object: str = "chat.completion"
    created: int = 0
    model: str = ""
    choices: list[ChatChoice] = field(default_factory=list)
    usage: Usage | None = None
    headers: ResponseHeaders | None = None


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
    headers: ResponseHeaders | None = None


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


def _parse_headers(header_dict: dict[str, str]) -> ResponseHeaders:
    """Extract operational headers from HTTP response headers."""
    return ResponseHeaders(
        x_provider=header_dict.get("x-provider"),
        x_cache=header_dict.get("x-cache"),
        x_cost_usd=header_dict.get("x-cost-usd"),
        x_ratelimit_limit=header_dict.get("x-ratelimit-limit"),
        x_ratelimit_remaining=header_dict.get("x-ratelimit-remaining"),
        x_ratelimit_reset=header_dict.get("x-ratelimit-reset"),
        x_request_id=header_dict.get("x-request-id"),
    )


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
        headers=data.get("_headers"),  # injected by client
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
        headers=data.get("_headers"),  # injected by client
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


# ── Admin Types ───────────────────────────────────────


@dataclass
class Project:
    """Admin project (tenant)."""
    id: str = ""
    name: str = ""
    created_at: str = ""


@dataclass
class VirtualKey:
    """Virtual API key scoped to a project."""
    id: str = ""
    project_id: str = ""
    key_prefix: str = ""
    name: str = ""
    allowed_models: list[str] = field(default_factory=list)
    allowed_providers: list[str] = field(default_factory=list)
    allowed_tools: list[str] = field(default_factory=list)
    rpm_limit: int = 0
    tpm_limit: int = 0
    budget_limit_usd: float = 0.0
    budget_period: str = ""
    expires_at: str | None = None
    revoked_at: str | None = None
    created_at: str = ""
    allow_mcp_server: bool = False
    mcp_tool_name: str = ""


@dataclass
class CreateKeyRequest:
    """Request body for creating a virtual key."""
    project_id: str = ""
    name: str = ""
    allowed_models: list[str] | None = None
    allowed_providers: list[str] | None = None
    allowed_tools: list[str] | None = None
    rpm_limit: int = 0
    tpm_limit: int = 0
    budget_limit_usd: float = 0.0
    budget_period: str = ""
    allow_mcp_server: bool = False
    mcp_tool_name: str = ""


@dataclass
class CreateKeyResponse:
    """Response from creating a virtual key (includes raw key)."""
    id: str = ""
    key: str = ""
    key_prefix: str = ""
    name: str = ""
    project_id: str = ""


@dataclass
class UsageEntry:
    """Single usage log row."""
    id: int = 0
    request_id: str = ""
    project_id: str | None = None
    virtual_key_id: str | None = None
    model: str = ""
    provider: str = ""
    provider_model: str = ""
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0
    cost_usd: float = 0.0
    cache_hit: bool = False
    stream: bool = False
    attempts: int = 0
    duration_ms: int = 0
    error: str = ""
    created_at: str = ""


@dataclass
class UsageSummaryEntry:
    """Aggregated usage row."""
    period: str = ""
    model: str = ""
    provider: str = ""
    request_count: int = 0
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0
    cost_usd: float = 0.0


@dataclass
class UsageFilters:
    """Query parameters for usage endpoint."""
    project_id: str | None = None
    key_id: str | None = None
    model: str | None = None
    from_: str | None = None
    to: str | None = None
    limit: int | None = None


@dataclass
class UsageSummaryFilters:
    """Query parameters for usage summary endpoint."""
    project_id: str | None = None
    period: str | None = None


@dataclass
class QuickstartResponse:
    """Response from quickstart endpoint."""
    project: Project = field(default_factory=Project)
    key: CreateKeyResponse = field(default_factory=CreateKeyResponse)


# ── Admin Parse Helpers ──────────────────────────────


def _parse_project(data: dict[str, Any]) -> Project:
    return Project(
        id=data.get("id", ""),
        name=data.get("name", ""),
        created_at=data.get("created_at", ""),
    )


def _parse_virtual_key(data: dict[str, Any]) -> VirtualKey:
    return VirtualKey(
        id=data.get("id", ""),
        project_id=data.get("project_id", ""),
        key_prefix=data.get("key_prefix", ""),
        name=data.get("name", ""),
        allowed_models=data.get("allowed_models", []),
        allowed_providers=data.get("allowed_providers", []),
        allowed_tools=data.get("allowed_tools", []),
        rpm_limit=data.get("rpm_limit", 0),
        tpm_limit=data.get("tpm_limit", 0),
        budget_limit_usd=data.get("budget_limit_usd", 0.0),
        budget_period=data.get("budget_period", ""),
        expires_at=data.get("expires_at"),
        revoked_at=data.get("revoked_at"),
        created_at=data.get("created_at", ""),
        allow_mcp_server=data.get("allow_mcp_server", False),
        mcp_tool_name=data.get("mcp_tool_name", ""),
    )


def _parse_usage_entry(data: dict[str, Any]) -> UsageEntry:
    return UsageEntry(
        id=data.get("id", 0),
        request_id=data.get("request_id", ""),
        project_id=data.get("project_id"),
        virtual_key_id=data.get("virtual_key_id"),
        model=data.get("model", ""),
        provider=data.get("provider", ""),
        provider_model=data.get("provider_model", ""),
        prompt_tokens=data.get("prompt_tokens", 0),
        completion_tokens=data.get("completion_tokens", 0),
        total_tokens=data.get("total_tokens", 0),
        cost_usd=data.get("cost_usd", 0.0),
        cache_hit=data.get("cache_hit", False),
        stream=data.get("stream", False),
        attempts=data.get("attempts", 0),
        duration_ms=data.get("duration_ms", 0),
        error=data.get("error", ""),
        created_at=data.get("created_at", ""),
    )


def _parse_usage_summary(data: dict[str, Any]) -> UsageSummaryEntry:
    return UsageSummaryEntry(
        period=data.get("period", ""),
        model=data.get("model", ""),
        provider=data.get("provider", ""),
        request_count=data.get("request_count", 0),
        prompt_tokens=data.get("prompt_tokens", 0),
        completion_tokens=data.get("completion_tokens", 0),
        total_tokens=data.get("total_tokens", 0),
        cost_usd=data.get("cost_usd", 0.0),
    )
