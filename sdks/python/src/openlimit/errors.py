"""Error types for the OpenLimit SDK."""


class APIError(Exception):
    """Raised when the API returns a non-2xx response."""

    def __init__(
        self,
        status: int,
        message: str,
        error_type: str = "unknown",
        code: str | None = None,
        request_id: str | None = None,
    ) -> None:
        super().__init__(message)
        self.status = status
        self.error_type = error_type
        self.code = code
        self.request_id = request_id


class TimeoutError(Exception):
    """Raised when a request exceeds the configured timeout."""

    def __init__(self, timeout_ms: int) -> None:
        super().__init__(f"Request timed out after {timeout_ms}ms")
        self.timeout_ms = timeout_ms
