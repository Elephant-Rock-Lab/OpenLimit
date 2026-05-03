package audit

import "time"

// Event types for audit logging.
const (
	EventProjectCreate   = "project.create"
	EventProjectDelete   = "project.delete"
	EventKeyCreate       = "key.create"
	EventKeyRevoke       = "key.revoke"
	EventKeyUpdate       = "key.update"
	EventKeyDecrypt      = "provider_key.decrypt"
	EventAuthFailure     = "auth.failure"
	EventAuthDenied      = "authorization.denied"
	EventGuardrailBlock  = "guardrail.block"
	EventConfigReload    = "config.reload"
	EventUserCreate      = "user.create"
	EventUserDelete      = "user.delete"
	EventUserUpdate      = "user.update_role"
	EventOIDCAuthSuccess = "oidc.auth_success"
	EventOIDCAuthFailure = "oidc.auth_failure"
)

// Event represents a single audit log entry.
type Event struct {
	ID        int64          // auto-increment, set by DB
	Timestamp time.Time      // set by logger if zero
	EventType string         // e.g., "project.create", "key.revoke"
	Actor     string         // "admin" / "token:abc1..." / "system"
	Action    string         // "create", "delete", "revoke", "decrypt", "block"
	Resource  string         // "project:proj_1", "key:vk_abc123"
	Outcome   string         // "success", "denied", "error"
	RequestID string         // from X-Request-ID (or empty for startup events)
	Metadata  map[string]any // free-form details (JSONB column)
}
