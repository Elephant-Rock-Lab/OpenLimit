"""OpenLimit API Gateway client — zero dependencies."""

from __future__ import annotations

import json
import http.client
import ssl
from urllib.parse import urlparse
from typing import Any, Generator

from .types import (
    ChatCompletionRequest,
    ChatCompletionResponse,
    ChatCompletionChunk,
    EmbeddingsRequest,
    EmbeddingsResponse,
    ModelsResponse,
    HealthResponse,
    ResponseHeaders,
    _parse_chat_response,
    _parse_embeddings,
    _parse_headers,
    _parse_models,
)
from .errors import APIError, TimeoutError
from .streaming import parse_sse_lines


class OpenLimitClient:
    """Synchronous client for the OpenLimit AI API Gateway.

    Zero third-party dependencies — uses only stdlib http.client.
    """

    def __init__(
        self,
        base_url: str,
        api_key: str,
        timeout: int = 30,
        default_headers: dict[str, str] | None = None,
    ) -> None:
        parsed = urlparse(base_url)
        self._scheme = parsed.scheme or "http"
        self._host = parsed.hostname or "localhost"
        self._port = parsed.port or (443 if self._scheme == "https" else 80)
        self._base_path = parsed.path.rstrip("/")
        self._api_key = api_key
        self._timeout = timeout
        self._default_headers: dict[str, str] = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {api_key}",
            **(default_headers or {}),
        }

    # ── Chat Completions ──────────────────────────────

    def chat_completion(
        self, req: ChatCompletionRequest
    ) -> ChatCompletionResponse:
        """Create a non-streaming chat completion."""
        body = {"model": req.model, "messages": self._serialize_messages(req.messages), "stream": False}
        if req.temperature is not None:
            body["temperature"] = req.temperature
        if req.max_tokens is not None:
            body["max_tokens"] = req.max_tokens
        if req.data_residency:
            body["data_residency"] = req.data_residency

        data, raw_headers = self._request("POST", "/v1/chat/completions", body)
        headers = _parse_headers(raw_headers)
        data["_headers"] = headers
        return _parse_chat_response(data)

    def chat_completion_stream(
        self, req: ChatCompletionRequest
    ) -> Generator[ChatCompletionChunk, None, None]:
        """Create a streaming chat completion (yields chunks)."""
        body = {"model": req.model, "messages": self._serialize_messages(req.messages), "stream": True}
        if req.temperature is not None:
            body["temperature"] = req.temperature

        for line in self._stream_request("POST", "/v1/chat/completions", body):
            yield from parse_sse_lines([line])

    # ── Embeddings ────────────────────────────────────

    def embeddings(self, req: EmbeddingsRequest) -> EmbeddingsResponse:
        """Create embeddings for the given input."""
        body: dict[str, Any] = {"model": req.model, "input": req.input}
        if req.encoding_format:
            body["encoding_format"] = req.encoding_format
        if req.dimensions:
            body["dimensions"] = req.dimensions

        data, raw_headers = self._request("POST", "/v1/embeddings", body)
        headers = _parse_headers(raw_headers)
        data["_headers"] = headers
        return _parse_embeddings(data)

    # ── Models ────────────────────────────────────────

    def models(self) -> ModelsResponse:
        """List available models."""
        data, _ = self._request("GET", "/v1/models")
        return _parse_models(data)

    # ── Health ────────────────────────────────────────

    def health(self) -> HealthResponse:
        """Check gateway health (no auth required)."""
        data, _ = self._request("GET", "/health", auth=False)
        return HealthResponse(
            status=data.get("status", ""),
            version=data.get("version"),
            uptime_seconds=data.get("uptime_seconds"),
        )

    # ── Internal ──────────────────────────────────────

    def _serialize_messages(self, messages: list[ChatCompletionRequest] | list[Any]) -> list[dict[str, Any]]:
        result = []
        for msg in messages:
            if hasattr(msg, "role") and hasattr(msg, "content"):
                result.append({"role": msg.role, "content": msg.content})
            else:
                result.append(msg)
        return result

    def _make_connection(self) -> http.client.HTTPConnection:
        if self._scheme == "https":
            ctx = ssl.create_default_context()
            return http.client.HTTPSConnection(
                self._host, self._port, timeout=self._timeout, context=ctx
            )
        return http.client.HTTPConnection(
            self._host, self._port, timeout=self._timeout
        )

    def _request(
        self, method: str, path: str, body: Any = None, auth: bool = True
    ) -> tuple[dict[str, Any], dict[str, str]]:
        headers = dict(self._default_headers)
        if not auth:
            headers.pop("Authorization", None)

        conn = self._make_connection()
        try:
            conn.request(
                method,
                f"{self._base_path}{path}",
                body=json.dumps(body) if body else None,
                headers=headers,
            )
            resp = conn.getresponse()
            resp_body = resp.read().decode("utf-8")

            # Extract response headers (lowercased keys for easy lookup)
            raw_headers: dict[str, str] = {}
            for k, v in resp.getheaders():
                raw_headers[k.lower()] = v

            if resp.status >= 400:
                self._handle_error(resp.status, resp_body)

            return json.loads(resp_body), raw_headers
        finally:
            conn.close()

    def _stream_request(
        self, method: str, path: str, body: Any
    ) -> Generator[str, None, None]:
        headers = dict(self._default_headers)
        conn = self._make_connection()
        try:
            conn.request(
                method,
                f"{self._base_path}{path}",
                body=json.dumps(body),
                headers=headers,
            )
            resp = conn.getresponse()

            if resp.status >= 400:
                resp_body = resp.read().decode("utf-8")
                self._handle_error(resp.status, resp_body)

            buffer = ""
            while True:
                chunk = resp.read(4096)
                if not chunk:
                    break
                buffer += chunk.decode("utf-8")
                while "\n" in buffer:
                    line, buffer = buffer.split("\n", 1)
                    yield line
            if buffer.strip():
                yield buffer
        finally:
            conn.close()

    def _handle_error(self, status: int, body: str) -> None:
        try:
            data = json.loads(body)
            err = data.get("error", {})
            raise APIError(
                status=status,
                message=err.get("message", f"HTTP {status}"),
                error_type=err.get("type", "unknown"),
                code=err.get("code"),
                request_id=err.get("request_id"),
            )
        except json.JSONDecodeError:
            raise APIError(
                status=status,
                message=f"HTTP {status}: {body[:200]}",
                error_type="http_error",
            )
