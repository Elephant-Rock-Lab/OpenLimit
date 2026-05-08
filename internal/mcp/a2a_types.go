package mcp

import (
	"time"
)

// TaskState represents the state of an A2A task (A2A spec v1.0).
type TaskState string

const (
	TaskStateSubmitted TaskState = "submitted"
	TaskStateWorking   TaskState = "working"
	TaskStateCompleted TaskState = "completed"
	TaskStateFailed    TaskState = "failed"
	TaskStateCanceled  TaskState = "canceled"
)

// IsTerminal returns true if the task is in a terminal state.
func (s TaskState) IsTerminal() bool {
	return s == TaskStateCompleted || s == TaskStateFailed || s == TaskStateCanceled
}

// A2ATask represents an A2A task.
type A2ATask struct {
	ID            string         `json:"id"`
	ContextID     string         `json:"contextId"`
	Status        TaskState      `json:"status"`
	StatusMessage *A2AMessage    `json:"statusMessage,omitempty"`
	History       []A2AMessage   `json:"history"`
	Artifacts     []A2AArtifact  `json:"artifacts"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Model         string         `json:"model,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// A2AMessage represents a message with role and parts (A2A spec v1.0).
type A2AMessage struct {
	Role             string    `json:"role"`
	Parts            []A2APart `json:"parts"`
	MessageID        string    `json:"messageId"`
	ContextID        string    `json:"contextId,omitempty"`
	TaskID           string    `json:"taskId,omitempty"`
	ReferenceTaskIDs []string  `json:"referenceTaskIds,omitempty"`
}

// A2APart is a content part (text only for MVP).
// A2APart is a content part in the A2A protocol.
// Supports three types:
//   - "text": plain text content (Text field)
//   - "file": file reference (FileURI, FileMIMEType, FileBytes)
//   - "data": structured JSON data (Data field)
type A2APart struct {
	Type        string         `json:"type"`
	Text        string         `json:"text,omitempty"`
	FileURI     string         `json:"fileUri,omitempty"`     // URL or path to file
	FileMIMEType string        `json:"mimeType,omitempty"`   // MIME type of file
	FileBytes   string         `json:"bytes,omitempty"`      // Base64-encoded file content
	Data        map[string]any  `json:"data,omitempty"`       // Structured key-value data
}

// A2AArtifact represents a task output artifact (A2A spec v1.0).
type A2AArtifact struct {
	Parts     []A2APart `json:"parts"`
	Index     int       `json:"index"`
	LastChunk bool      `json:"lastChunk,omitempty"`
}

// AgentCard is the A2A discovery document served at /.well-known/agent.json.
type AgentCard struct {
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Version         string            `json:"version"`
	Description     string            `json:"description,omitempty"`
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    AgentCapabilities `json:"capabilities"`
	Skills          []AgentSkill      `json:"skills,omitempty"`
	Authentication  *AgentAuthInfo    `json:"authentication,omitempty"`
}

// AgentCapabilities declares what the agent supports.
type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
}

// AgentSkill describes a skill the agent offers.
type AgentSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AgentAuthInfo describes authentication requirements for the agent.
type AgentAuthInfo struct {
	Required bool   `json:"required"`
	Type     string `json:"type,omitempty"`
	Scheme   string `json:"scheme,omitempty"`
}

// ID generation helpers.
func newTaskID() string    { return "task_" + randomHex(16) }
func newMessageID() string { return "msg_" + randomHex(12) }
func newContextID() string { return "ctx_" + randomHex(12) }
