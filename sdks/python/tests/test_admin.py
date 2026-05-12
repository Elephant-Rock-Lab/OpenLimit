"""Tests for the OpenLimit Python SDK admin client."""

import json
from unittest.mock import MagicMock, patch
import pytest

from openlimit import OpenLimitAdmin, APIError
from openlimit.types import (
    CreateKeyRequest,
    UsageFilters,
    UsageSummaryFilters,
)


# ── Helpers ────────────────────────────────────────────

def make_admin() -> OpenLimitAdmin:
    return OpenLimitAdmin(
        base_url="http://localhost:8080",
        admin_token="admin-secret-123",
    )


def mock_response(status: int, body) -> MagicMock:
    resp = MagicMock()
    resp.status = status
    resp.read.return_value = json.dumps(body).encode("utf-8")
    return resp


# ── TEST-22-02-01: list_projects() returns list of Project ──

class TestListProjects:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_returns_project_list(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, [
            {"id": "proj-1", "name": "Alpha", "created_at": "2026-01-01T00:00:00Z"},
            {"id": "proj-2", "name": "Beta", "created_at": "2026-02-01T00:00:00Z"},
        ])

        admin = make_admin()
        result = admin.list_projects()

        assert len(result) == 2
        assert result[0].id == "proj-1"
        assert result[0].name == "Alpha"
        assert result[1].id == "proj-2"

        # Verify correct endpoint
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "GET"
        assert "/admin/projects" in call_args[0][1]

        # Verify Bearer auth uses admin token
        headers = call_args[1].get("headers", {})
        assert headers.get("Authorization") == "Bearer admin-secret-123"


# ── TEST-22-02-02: create_project(name) sends POST, returns Project ──

class TestCreateProject:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_post_returns_project(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(201, {
            "id": "proj-new",
            "name": "My Project",
            "created_at": "2026-05-09T12:00:00Z",
        })

        admin = make_admin()
        result = admin.create_project("My Project")

        assert result.id == "proj-new"
        assert result.name == "My Project"
        assert result.created_at == "2026-05-09T12:00:00Z"

        # Verify POST method
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "POST"
        assert "/admin/projects" in call_args[0][1]

        # Verify body contains name
        body_arg = call_args[1].get("body", call_args[0][2] if len(call_args[0]) > 2 else None)
        sent_body = json.loads(body_arg)
        assert sent_body["name"] == "My Project"


# ── TEST-22-02-03: create_key(req) sends POST with project_id+name ──

class TestCreateKey:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_post_with_required_fields(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(201, {
            "id": "key-1",
            "key": "gw-abcdef1234567890",
            "key_prefix": "gw-abcd",
            "name": "my-key",
            "project_id": "proj-1",
        })

        admin = make_admin()
        req = CreateKeyRequest(project_id="proj-1", name="my-key")
        result = admin.create_key(req)

        assert result.id == "key-1"
        assert result.key == "gw-abcdef1234567890"
        assert result.key_prefix == "gw-abcd"
        assert result.name == "my-key"
        assert result.project_id == "proj-1"

        # Verify POST and body
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "POST"
        assert "/admin/keys" in call_args[0][1]

        body_arg = call_args[1].get("body", call_args[0][2] if len(call_args[0]) > 2 else None)
        sent_body = json.loads(body_arg)
        assert sent_body["project_id"] == "proj-1"
        assert sent_body["name"] == "my-key"


# ── TEST-22-02-04: list_keys(project_id=None) sends GET with optional filter ──

class TestListKeys:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_get_with_optional_filter(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, [
            {
                "id": "key-1",
                "project_id": "proj-1",
                "key_prefix": "gw-abcd",
                "name": "key-alpha",
                "allowed_models": [],
                "allowed_providers": [],
                "allowed_tools": [],
                "rpm_limit": 100,
                "tpm_limit": 0,
                "budget_limit_usd": 50.0,
                "budget_period": "monthly",
                "expires_at": None,
                "revoked_at": None,
                "created_at": "2026-01-01T00:00:00Z",
                "allow_mcp_server": False,
                "mcp_tool_name": "",
            },
        ])

        admin = make_admin()
        result = admin.list_keys(project_id="proj-1")

        assert len(result) == 1
        assert result[0].id == "key-1"
        assert result[0].project_id == "proj-1"
        assert result[0].rpm_limit == 100
        assert result[0].budget_limit_usd == 50.0

        # Verify GET with query param
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "GET"
        url = call_args[0][1]
        assert "/admin/keys" in url
        assert "project_id=proj-1" in url

    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_no_filter_sends_plain_get(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, [])

        admin = make_admin()
        admin.list_keys()

        call_args = mock_conn.request.call_args
        url = call_args[0][1]
        assert "?" not in url


# ── TEST-22-02-05: query_usage(filters=None) sends GET with query params ──

class TestQueryUsage:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_get_with_query_params(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, [
            {
                "id": 1,
                "request_id": "req-001",
                "project_id": "proj-1",
                "virtual_key_id": "key-1",
                "model": "gpt-4",
                "provider": "openai",
                "provider_model": "gpt-4-0613",
                "prompt_tokens": 100,
                "completion_tokens": 50,
                "total_tokens": 150,
                "cost_usd": 0.005,
                "cache_hit": False,
                "stream": False,
                "attempts": 1,
                "duration_ms": 250,
                "error": "",
                "created_at": "2026-05-09T10:00:00Z",
            },
        ])

        admin = make_admin()
        filters = UsageFilters(
            project_id="proj-1",
            model="gpt-4",
            limit=50,
        )
        result = admin.query_usage(filters)

        assert len(result) == 1
        assert result[0].id == 1
        assert result[0].model == "gpt-4"
        assert result[0].provider == "openai"
        assert result[0].cost_usd == 0.005

        # Verify GET with query params
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "GET"
        url = call_args[0][1]
        assert "/admin/usage" in url
        assert "project_id=proj-1" in url
        assert "model=gpt-4" in url
        assert "limit=50" in url

    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_no_filters_sends_plain_get(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, [])

        admin = make_admin()
        admin.query_usage()

        call_args = mock_conn.request.call_args
        url = call_args[0][1]
        assert "?" not in url


# ── TEST-22-02-06: usage_summary(filters=None) sends GET with period ──

class TestUsageSummary:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_get_with_period(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, [
            {
                "period": "2026-05-09T00:00:00Z",
                "model": "gpt-4",
                "provider": "openai",
                "request_count": 42,
                "prompt_tokens": 4200,
                "completion_tokens": 2100,
                "total_tokens": 6300,
                "cost_usd": 0.63,
            },
        ])

        admin = make_admin()
        filters = UsageSummaryFilters(project_id="proj-1", period="daily")
        result = admin.usage_summary(filters)

        assert len(result) == 1
        assert result[0].period == "2026-05-09T00:00:00Z"
        assert result[0].model == "gpt-4"
        assert result[0].request_count == 42
        assert result[0].cost_usd == 0.63

        # Verify GET with query params
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "GET"
        url = call_args[0][1]
        assert "/admin/usage/summary" in url
        assert "project_id=proj-1" in url
        assert "period=daily" in url


# ── TEST-22-02-07: quickstart() sends POST, returns {project, key} ──

class TestQuickstart:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_post_returns_project_and_key(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(201, {
            "project": {
                "id": "proj-qs",
                "name": "quickstart-2026-05-09",
                "created_at": "2026-05-09T12:00:00Z",
            },
            "key": {
                "id": "key-qs",
                "key": "gw-xyz1234567890",
                "key_prefix": "gw-xyz1",
                "name": "quickstart",
                "project_id": "proj-qs",
            },
        })

        admin = make_admin()
        result = admin.quickstart(name="my-quickstart", rpm_limit=100)

        assert result.project.id == "proj-qs"
        assert result.project.name == "quickstart-2026-05-09"
        assert result.key.id == "key-qs"
        assert result.key.key == "gw-xyz1234567890"
        assert result.key.name == "quickstart"

        # Verify POST
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "POST"
        assert "/admin/quickstart" in call_args[0][1]

        body_arg = call_args[1].get("body", call_args[0][2] if len(call_args[0]) > 2 else None)
        sent_body = json.loads(body_arg)
        assert sent_body["name"] == "my-quickstart"
        assert sent_body["rpm_limit"] == 100


# ── TEST-22-02-08: delete_project(id) sends DELETE ────────────────

class TestDeleteProject:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_delete_and_resolves(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        resp = MagicMock()
        resp.status = 204
        resp.read.return_value = b""
        mock_conn.getresponse.return_value = resp

        admin = make_admin()
        admin.delete_project("proj-123")

        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "DELETE"
        assert "/admin/projects/proj-123" in call_args[0][1]


# ── TEST-22-02-09: update_key(id, fields) sends PUT ──────────────

class TestUpdateKey:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_put_with_fields(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "id": "key-1",
            "project_id": "proj-1",
            "key_prefix": "gw-abcd",
            "name": "updated-key",
            "allowed_models": ["gpt-4"],
            "allowed_providers": [],
            "allowed_tools": [],
            "rpm_limit": 200,
            "tpm_limit": 0,
            "budget_limit_usd": 100,
            "budget_period": "monthly",
            "expires_at": None,
            "revoked_at": None,
            "created_at": "2026-05-09T00:00:00Z",
            "allow_mcp_server": False,
            "mcp_tool_name": "",
        })

        admin = make_admin()
        result = admin.update_key("key-1", {"rpm_limit": 200, "name": "updated-key"})

        assert result.rpm_limit == 200
        assert result.name == "updated-key"
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "PUT"
        assert "/admin/keys/key-1" in call_args[0][1]


# ── TEST-22-02-10: patch_key(id, fields) sends PATCH ─────────────

class TestPatchKey:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_patch_with_partial_fields(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        mock_conn.getresponse.return_value = mock_response(200, {
            "id": "key-1",
            "project_id": "proj-1",
            "key_prefix": "gw-abcd",
            "name": "my-key",
            "allowed_models": [],
            "allowed_providers": [],
            "allowed_tools": [],
            "rpm_limit": 500,
            "tpm_limit": 0,
            "budget_limit_usd": 0,
            "budget_period": "monthly",
            "expires_at": None,
            "revoked_at": None,
            "created_at": "2026-05-09T00:00:00Z",
            "allow_mcp_server": False,
            "mcp_tool_name": "",
        })

        admin = make_admin()
        result = admin.patch_key("key-1", {"rpm_limit": 500})

        assert result.rpm_limit == 500
        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "PATCH"
        assert "/admin/keys/key-1" in call_args[0][1]


# ── TEST-22-02-11: revoke_key(id) sends DELETE ───────────────────

class TestRevokeKey:
    @patch("openlimit.admin.http.client.HTTPConnection")
    def test_sends_delete_and_resolves(self, mock_conn_cls):
        mock_conn = MagicMock()
        mock_conn_cls.return_value = mock_conn
        resp = MagicMock()
        resp.status = 204
        resp.read.return_value = b""
        mock_conn.getresponse.return_value = resp

        admin = make_admin()
        admin.revoke_key("key-1")

        mock_conn.request.assert_called_once()
        call_args = mock_conn.request.call_args
        assert call_args[0][0] == "DELETE"
        assert "/admin/keys/key-1" in call_args[0][1]
