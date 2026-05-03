package mcp

import "encoding/json"

// Protocol version supported by the gateway's MCP client.
const ProtocolVersion = "2025-11-25"

// JSONRPC version constant.
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return e.Message
}

// Notification is a JSON-RPC 2.0 notification (no ID, expects no response).
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// InitializeRequestParams contains the parameters for the MCP initialize method.
type InitializeRequestParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCaps         `json:"capabilities"`
	ClientInfo      ImplementationInfo `json:"clientInfo"`
}

// ClientCaps declares the client capabilities the gateway supports.
type ClientCaps struct {
	Tools *struct{} `json:"tools,omitempty"`
}

// ImplementationInfo describes the client or server implementation.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result of a successful initialize call.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCaps         `json:"capabilities"`
	ServerInfo      ImplementationInfo `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerCaps declares the server's capabilities.
type ServerCaps struct {
	Tools *ToolsCap `json:"tools,omitempty"`
}

// ToolsCap indicates the server supports tools and optional list-change notifications.
type ToolsCap struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ListToolsResult is the result of a tools/list call.
type ListToolsResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// ToolDefinition is a tool as returned by an MCP server's tools/list.
type ToolDefinition struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

// CallToolParams contains the parameters for tools/call.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the result of a tools/call invocation.
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a content block in a tool result.
type ToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// PingParams is an empty params object for the ping method.
type PingParams struct{}
