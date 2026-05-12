"""Tests for the OpenLimit Python SDK."""

import json
import io
from unittest.mock import MagicMock, patch
import pytest

from openlimit import OpenLimitClient, APIError, TimeoutError, ResponseHeaders
from openlimit.types import (
    ChatCompletionRequest,
    ChatCompletionResponse,
    ChatMessage,
    EmbeddingsRequest,
)


# ── Helpers ────────────────────────────────────────────

def make_client() -> OpenLimitClient:
    return OpenLimitClient(
        base_url="http://localhost:8080",
        api_key="test-key-123",
    )


def mock_response(status: int, body: dict, headers: list[tuple[str, str]] | None = None) -> MagicMock:
    resp = MagicMock()
    resp.status = status
    resp.read.return_value = json.dumps(body).encode("utf-8")
    resp.getheaders.return_value = headers or []
    return resp


# ── TEST-15-01-01: Non-streaming chat completion ───────

class TestChatCompletion:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_returns_response(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "id": "chatcmpl-123",
            "object": "chat.completion",
            "created": 1234567890,
            "model": "gpt-4",
            "choices": [
                {
                    "index": 0,
                    "message": {"role": "assistant", "content": "Hello!"},
                    "finish_reason": "stop",
                }
            ],
            "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
        })

        client = make_client()
        result = client.chat_completion(ChatCompletionRequest(
            model="gpt-4",
            messages=[ChatMessage(role="user", content="Hi")],
        ))

        assert result.id == "chatcmpl-123"
        assert result.choices[0].message.content == "Hello!"
        mock_conn.request.assert_called_once()


# ── TEST-15-01-02: Streaming chat completion ───────────

class TestChatCompletionStream:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_yields_chunks(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn

        sse_data = (
            'data: {"id":"c-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}\n'
            '\n'
            'data: {"id":"c-1","object":"chat.completion.chunk","created":2,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}\n'
            '\n'
            'data: [DONE]\n'
        ).encode("utf-8")

        resp = MagicMock()
        resp.status = 200
        resp.read = MagicMock(side_effect=[sse_data, b""])
        mock_conn.getresponse.return_value = resp

        client = make_client()
        chunks = list(client.chat_completion_stream(ChatCompletionRequest(
            model="gpt-4",
            messages=[ChatMessage(role="user", content="Hi")],
        )))

        assert len(chunks) == 2
        assert chunks[0].choices[0].delta.content == "Hi"
        assert chunks[1].choices[0].delta.content == "!"
        assert chunks[1].choices[0].finish_reason == "stop"


# ── TEST-15-01-03: Embeddings ──────────────────────────

class TestEmbeddings:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_returns_vectors(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "object": "list",
            "data": [{"object": "embedding", "embedding": [0.1, 0.2, 0.3], "index": 0}],
            "model": "text-embedding-3-small",
            "usage": {"prompt_tokens": 5, "completion_tokens": 0, "total_tokens": 5},
        })

        client = make_client()
        result = client.embeddings(EmbeddingsRequest(
            model="text-embedding-3-small",
            input="Hello world",
        ))

        assert len(result.data) == 1
        assert result.data[0].embedding == [0.1, 0.2, 0.3]


# ── TEST-15-01-04: Models listing ──────────────────────

class TestModels:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_returns_model_list(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "object": "list",
            "data": [
                {"id": "gpt-4", "object": "model", "created": 1, "owned_by": "openai"},
                {"id": "claude-3", "object": "model", "created": 2, "owned_by": "anthropic"},
            ],
        })

        client = make_client()
        result = client.models()

        assert len(result.data) == 2
        assert result.data[0].id == "gpt-4"
        assert result.data[1].id == "claude-3"


# ── TEST-15-01-05: Health check ────────────────────────

class TestHealth:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_returns_health_status(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "status": "ok",
            "version": "v1.2.0",
        })

        client = make_client()
        result = client.health()

        assert result.status == "ok"
        assert result.version == "v1.2.0"

        # Verify Authorization header was NOT sent
        call_args = mock_conn.request.call_args
        headers = call_args.kwargs.get("headers", call_args[3] if len(call_args.args) > 3 else {})
        assert "Authorization" not in headers


# ── TEST-15-01-06: Error handling ──────────────────────

class TestErrorHandling:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_throws_api_error(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(401, {
            "error": {
                "message": "Invalid API key",
                "type": "authentication_error",
                "code": "invalid_api_key",
                "request_id": "req-123",
            },
        })

        client = make_client()
        with pytest.raises(APIError) as exc_info:
            client.chat_completion(ChatCompletionRequest(
                model="gpt-4",
                messages=[ChatMessage(role="user", content="Hi")],
            ))

        assert exc_info.value.status == 401
        assert exc_info.value.error_type == "authentication_error"
        assert exc_info.value.request_id == "req-123"


# ── TEST-22-04-01: chat_completion() returns response with headers ────

class TestChatCompletionHeaders:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_returns_response_with_headers(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "id": "chatcmpl-hdr",
            "object": "chat.completion",
            "created": 1234567890,
            "model": "gpt-4",
            "choices": [
                {
                    "index": 0,
                    "message": {"role": "assistant", "content": "Hello!"},
                    "finish_reason": "stop",
                }
            ],
            "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
        }, headers=[
            ("X-Provider", "openai"),
            ("X-Cache", "MISS"),
            ("X-Cost-USD", "0.000450"),
            ("X-RateLimit-Limit", "1000"),
            ("X-RateLimit-Remaining", "999"),
            ("X-RateLimit-Reset", "1700000000"),
            ("X-Request-ID", "req-abc-123"),
        ])

        client = make_client()
        result = client.chat_completion(ChatCompletionRequest(
            model="gpt-4",
            messages=[ChatMessage(role="user", content="Hi")],
        ))

        assert result.id == "chatcmpl-hdr"
        assert result.headers is not None
        assert result.headers.x_provider == "openai"
        assert result.headers.x_cache == "MISS"
        assert result.headers.x_cost_usd == "0.000450"
        assert result.headers.x_ratelimit_limit == "1000"
        assert result.headers.x_ratelimit_remaining == "999"
        assert result.headers.x_ratelimit_reset == "1700000000"
        assert result.headers.x_request_id == "req-abc-123"


# ── TEST-22-04-02: embeddings() returns response with headers ─────────

class TestEmbeddingsHeaders:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_returns_response_with_headers(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "object": "list",
            "data": [{"object": "embedding", "embedding": [0.1, 0.2, 0.3], "index": 0}],
            "model": "text-embedding-3-small",
            "usage": {"prompt_tokens": 5, "completion_tokens": 0, "total_tokens": 5},
        }, headers=[
            ("X-Provider", "openai"),
            ("X-Cache", "HIT"),
            ("X-Cost-USD", "0.000010"),
            ("X-RateLimit-Limit", "500"),
            ("X-RateLimit-Remaining", "499"),
            ("X-RateLimit-Reset", "1700000001"),
            ("X-Request-ID", "req-emb-456"),
        ])

        client = make_client()
        result = client.embeddings(EmbeddingsRequest(
            model="text-embedding-3-small",
            input="Hello world",
        ))

        assert len(result.data) == 1
        assert result.headers is not None
        assert result.headers.x_provider == "openai"
        assert result.headers.x_cache == "HIT"
        assert result.headers.x_cost_usd == "0.000010"
        assert result.headers.x_ratelimit_limit == "500"
        assert result.headers.x_ratelimit_remaining == "499"
        assert result.headers.x_ratelimit_reset == "1700000001"
        assert result.headers.x_request_id == "req-emb-456"


# ── TEST-22-04-03: No X-* headers → all None fields ──────────────────

class TestNoHeaders:
    @patch("openlimit.client.http.client.HTTPConnection")
    def test_no_headers_all_none(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        # Response with no X-* headers (empty header list)
        mock_conn.getresponse.return_value = mock_response(200, {
            "id": "chatcmpl-nohdr",
            "object": "chat.completion",
            "created": 1234567890,
            "model": "gpt-4",
            "choices": [
                {
                    "index": 0,
                    "message": {"role": "assistant", "content": "Hi"},
                    "finish_reason": "stop",
                }
            ],
            "usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
        }, headers=[])

        client = make_client()
        result = client.chat_completion(ChatCompletionRequest(
            model="gpt-4",
            messages=[ChatMessage(role="user", content="Hi")],
        ))

        assert result.id == "chatcmpl-nohdr"
        assert result.headers is not None
        assert result.headers.x_provider is None
        assert result.headers.x_cache is None
        assert result.headers.x_cost_usd is None
        assert result.headers.x_ratelimit_limit is None
        assert result.headers.x_ratelimit_remaining is None
        assert result.headers.x_ratelimit_reset is None
        assert result.headers.x_request_id is None
