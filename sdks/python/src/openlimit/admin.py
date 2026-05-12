"""OpenLimit Admin API client — zero dependencies."""

from __future__ import annotations

import json
import http.client
import ssl
from urllib.parse import urlparse, urlencode
from typing import Any

from .types import (
    Project,
    VirtualKey,
    CreateKeyRequest,
    CreateKeyResponse,
    UsageEntry,
    UsageSummaryEntry,
    UsageFilters,
    UsageSummaryFilters,
    QuickstartResponse,
    _parse_project,
    _parse_virtual_key,
    _parse_usage_entry,
    _parse_usage_summary,
)
from .errors import APIError


class OpenLimitAdmin:
    """Synchronous admin client for the OpenLimit AI API Gateway.

    Uses admin bearer token authentication (separate from API keys).
    Zero third-party dependencies — uses only stdlib http.client.
    """

    def __init__(
        self,
        base_url: str,
        admin_token: str,
        timeout: int = 30,
        default_headers: dict[str, str] | None = None,
    ) -> None:
        parsed = urlparse(base_url)
        self._scheme = parsed.scheme or "http"
        self._host = parsed.hostname or "localhost"
        self._port = parsed.port or (443 if self._scheme == "https" else 80)
        self._base_path = parsed.path.rstrip("/")
        self._admin_token = admin_token
        self._timeout = timeout
        self._default_headers: dict[str, str] = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {admin_token}",
            **(default_headers or {}),
        }

    # ── Projects ───────────────────────────────────────

    def list_projects(self) -> list[Project]:
        """List all projects."""
        data = self._request("GET", "/admin/projects")
        return [_parse_project(p) for p in data]

    def create_project(self, name: str) -> Project:
        """Create a new project."""
        data = self._request("POST", "/admin/projects", {"name": name})
        return _parse_project(data)

    def delete_project(self, id: str) -> None:
        """Delete a project by ID."""
        self._request("DELETE", f"/admin/projects/{id}")

    # ── Keys ───────────────────────────────────────────

    def list_keys(self, project_id: str | None = None) -> list[VirtualKey]:
        """List virtual keys, optionally filtered by project_id."""
        path = "/admin/keys"
        if project_id is not None:
            path += f"?project_id={project_id}"
        data = self._request("GET", path)
        return [_parse_virtual_key(k) for k in data]

    def create_key(self, req: CreateKeyRequest) -> CreateKeyResponse:
        """Create a new virtual key."""
        body: dict[str, Any] = {
            "project_id": req.project_id,
            "name": req.name,
            "rpm_limit": req.rpm_limit,
            "tpm_limit": req.tpm_limit,
            "budget_limit_usd": req.budget_limit_usd,
            "allow_mcp_server": req.allow_mcp_server,
        }
        if req.allowed_models is not None:
            body["allowed_models"] = req.allowed_models
        if req.allowed_providers is not None:
            body["allowed_providers"] = req.allowed_providers
        if req.allowed_tools is not None:
            body["allowed_tools"] = req.allowed_tools
        if req.budget_period:
            body["budget_period"] = req.budget_period
        if req.mcp_tool_name:
            body["mcp_tool_name"] = req.mcp_tool_name

        data = self._request("POST", "/admin/keys", body)
        return CreateKeyResponse(
            id=data.get("id", ""),
            key=data.get("key", ""),
            key_prefix=data.get("key_prefix", ""),
            name=data.get("name", ""),
            project_id=data.get("project_id", ""),
        )

    def update_key(self, id: str, fields: dict[str, Any]) -> VirtualKey:
        """Full update of updatable key fields (PUT)."""
        data = self._request("PUT", f"/admin/keys/{id}", fields)
        return _parse_virtual_key(data)

    def patch_key(self, id: str, fields: dict[str, Any]) -> VirtualKey:
        """Partial update of key fields (PATCH)."""
        data = self._request("PATCH", f"/admin/keys/{id}", fields)
        return _parse_virtual_key(data)

    def revoke_key(self, id: str) -> None:
        """Revoke (soft-delete) a virtual key."""
        self._request("DELETE", f"/admin/keys/{id}")

    # ── Usage ──────────────────────────────────────────

    def query_usage(self, filters: UsageFilters | None = None) -> list[UsageEntry]:
        """Query raw usage log entries."""
        path = "/admin/usage"
        if filters:
            params = {}
            if filters.project_id:
                params["project_id"] = filters.project_id
            if filters.key_id:
                params["key_id"] = filters.key_id
            if filters.model:
                params["model"] = filters.model
            if filters.from_:
                params["from"] = filters.from_
            if filters.to:
                params["to"] = filters.to
            if filters.limit is not None:
                params["limit"] = str(filters.limit)
            if params:
                path += "?" + urlencode(params)
        data = self._request("GET", path)
        return [_parse_usage_entry(e) for e in data]

    def usage_summary(
        self, filters: UsageSummaryFilters | None = None
    ) -> list[UsageSummaryEntry]:
        """Query aggregated usage summary."""
        path = "/admin/usage/summary"
        if filters:
            params = {}
            if filters.project_id:
                params["project_id"] = filters.project_id
            if filters.period:
                params["period"] = filters.period
            if params:
                path += "?" + urlencode(params)
        data = self._request("GET", path)
        return [_parse_usage_summary(e) for e in data]

    # ── Quickstart ─────────────────────────────────────

    def quickstart(
        self,
        name: str | None = None,
        rpm_limit: int | None = None,
        budget_limit_usd: float | None = None,
    ) -> QuickstartResponse:
        """One-shot project + key creation for quick onboarding."""
        body: dict[str, Any] = {}
        if name is not None:
            body["name"] = name
        if rpm_limit is not None:
            body["rpm_limit"] = rpm_limit
        if budget_limit_usd is not None:
            body["budget_limit_usd"] = budget_limit_usd

        data = self._request("POST", "/admin/quickstart", body)
        return QuickstartResponse(
            project=_parse_project(data.get("project", {})),
            key=CreateKeyResponse(
                id=data.get("key", {}).get("id", ""),
                key=data.get("key", {}).get("key", ""),
                key_prefix=data.get("key", {}).get("key_prefix", ""),
                name=data.get("key", {}).get("name", ""),
                project_id=data.get("key", {}).get("project_id", ""),
            ),
        )

    # ── Internal ───────────────────────────────────────

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
        self, method: str, path: str, body: Any = None
    ) -> dict[str, Any] | list[Any]:
        headers = dict(self._default_headers)
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

            if resp.status == 204:
                return {}

            if resp.status >= 400:
                self._handle_error(resp.status, resp_body)

            return json.loads(resp_body)
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
