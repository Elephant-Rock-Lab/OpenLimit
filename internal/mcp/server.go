package mcp

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/store"
	"openlimit/pkg/version"
)

// ChatResult is the result of executing a chat completion via a virtual key.
type ChatResult struct {
	Content      string
	Model        string
	Usage        *ChatUsage
	FinishReason string
}

// ChatUsage holds token counts from a chat completion.
type ChatUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ChatExecutor is a callback that executes a chat completion request using
// a virtual key's configuration. It is wired in server.go to use the existing
// routing/provider pipeline.
type ChatExecutor func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error)

// ServerHandler implements the MCP server mode: it accepts incoming MCP
// client connections and serves tools derived from virtual keys.
type ServerHandler struct {
	cfg          config.MCPServerModeConfig
	db           *sql.DB
	sessions     *SessionStore
	logger       *slog.Logger
	toolLister   ToolLister
	chatExecutor ChatExecutor
}

// ToolLister is a function that returns the current list of exposed MCP tools.
// It queries virtual_keys with allow_mcp_server=true and converts them to MCP tools.
type ToolLister func() ([]ToolDefinition, error)

// NewServerHandler creates an MCP server handler.
func NewServerHandler(cfg config.MCPServerModeConfig, db *sql.DB, toolLister ToolLister, logger *slog.Logger) *ServerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	ttl := time.Duration(cfg.SessionTTLSec) * time.Second
	if ttl <= 0 {
		ttl = 3600 * time.Second
	}
	return &ServerHandler{
		cfg:        cfg,
		db:         db,
		sessions:   NewSessionStore(ttl, logger),
		logger:     logger.With("component", "mcp_server"),
		toolLister: toolLister,
	}
}

// SetChatExecutor sets the callback used to execute chat completions for tool calls.
func (h *ServerHandler) SetChatExecutor(exec ChatExecutor) {
	h.chatExecutor = exec
}

// Sessions returns the session store (for triggering notifications from admin API).
func (h *ServerHandler) Sessions() *SessionStore {
	return h.sessions
}

// ToolLister returns the current tool list by calling the configured tool lister function.
// This is used by the admin API to inspect exposed tools.
func (h *ServerHandler) ToolLister() ([]ToolDefinition, error) {
	if h.toolLister == nil {
		return []ToolDefinition{}, nil
	}
	return h.toolLister()
}

// ServeHTTP handles incoming MCP requests (JSON-RPC 2.0 over HTTP).
func (h *ServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GET requests are for SSE (server-initiated notifications)
	if r.Method == http.MethodGet {
		h.handleSSE(w, r)
		return
	}

	// Authenticate the request
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse JSON-RPC request
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, nil, CodeParseError, "parse error")
		return
	}

	if req.JSONRPC != JSONRPCVersion {
		h.writeError(w, &req.ID, CodeInvalidRequest, "invalid jsonrpc version")
		return
	}

	// Manage session
	sessionID := r.Header.Get("Mcp-Session-Id")
	session := h.sessions.CreateOrGet(sessionID)
	w.Header().Set("Mcp-Session-Id", session.ID)

	// Dispatch method
	var result json.RawMessage
	var rpcErr *RPCError

	switch req.Method {
	case "initialize":
		result, rpcErr = h.handleInitialize(req)
	case "notifications/initialized":
		// No response for notifications
		w.WriteHeader(http.StatusNoContent)
		return
	case "ping":
		result, rpcErr = h.handlePing()
	case "tools/list":
		result, rpcErr = h.handleToolsList()
	case "tools/call":
		result, rpcErr = h.handleToolsCall(req)
	default:
		rpcErr = &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
	}

	w.Header().Set("Content-Type", "application/json")
	if rpcErr != nil {
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      req.ID,
			Error:   rpcErr,
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := Response{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result:  result,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleSSE handles GET requests for server-sent events (notifications).
func (h *ServerHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}

	session, ok := h.sessions.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	session.ServeSSE(w, flusher)
}

// handleInitialize handles the MCP initialize request.
func (h *ServerHandler) handleInitialize(req Request) (json.RawMessage, *RPCError) {
	var params InitializeRequestParams
	if err := parseParams(req.Params, &params); err != nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid initialize params"}
	}

	h.logger.Info("MCP client initializing",
		"client_info", params.ClientInfo.Name,
		"protocol_version", params.ProtocolVersion,
	)

	ver := version.Version
	if ver == "" {
		ver = "0.1.0"
	}

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCaps{
			Tools: &ToolsCap{ListChanged: true},
		},
		ServerInfo: ImplementationInfo{
			Name:    "openlimit-gateway",
			Version: ver,
		},
		Instructions: "This gateway exposes virtual keys as tools. Each tool runs a chat completion with the associated key's governance (budget, rate limits, allowed models).",
	}

	data, _ := json.Marshal(result)
	return data, nil
}

// handlePing handles the MCP ping method.
func (h *ServerHandler) handlePing() (json.RawMessage, *RPCError) {
	return json.RawMessage(`{}`), nil
}

// handleToolsList returns the list of tools exposed by this MCP server.
func (h *ServerHandler) handleToolsList() (json.RawMessage, *RPCError) {
	if h.toolLister == nil {
		data, _ := json.Marshal(ListToolsResult{Tools: []ToolDefinition{}})
		return data, nil
	}

	tools, err := h.toolLister()
	if err != nil {
		h.logger.Error("failed to list tools", "error", err)
		return nil, &RPCError{Code: CodeInternalError, Message: "failed to list tools"}
	}

	data, _ := json.Marshal(ListToolsResult{Tools: tools})
	return data, nil
}

// handleToolsCall handles the tools/call method.
// It resolves the tool name to a virtual key, validates it, executes
// a chat completion via the ChatExecutor callback, and returns the result.
func (h *ServerHandler) handleToolsCall(req Request) (json.RawMessage, *RPCError) {
	var params CallToolParams
	if err := parseParams(req.Params, &params); err != nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid tools/call params"}
	}
	if params.Name == "" {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "tool name is required"}
	}

	if h.chatExecutor == nil {
		return nil, &RPCError{Code: CodeInternalError, Message: "tool execution is not available"}
	}

	// Execute the tool call via the ChatExecutor callback
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := h.chatExecutor(ctx, params.Name, params.Arguments)
	if err != nil {
		h.logger.Error("tool execution failed", "tool", params.Name, "error", err)
		callResult := CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: fmt.Sprintf("tool execution failed: %s", err.Error())},
			},
			IsError: true,
		}
		data, _ := json.Marshal(callResult)
		return data, nil
	}

	// Build the MCP tool result
	callResult := CallToolResult{
		Content: []ToolContent{
			{Type: "text", Text: result.Content},
		},
	}
	data, _ := json.Marshal(callResult)
	return data, nil
}

// authenticate validates the incoming request against the configured auth mode.
func (h *ServerHandler) authenticate(r *http.Request) bool {
	switch h.cfg.Auth.Mode {
	case "none", "":
		return true
	case "bearer_token":
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return false
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.Auth.BearerToken)) == 1
	case "virtual_key":
		if h.db == nil {
			return false
		}
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return false
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if !strings.HasPrefix(token, "gw-") {
			return false
		}
		_, err := store.LookupVirtualKeyByToken(r.Context(), h.db, token)
		return err == nil
	default:
		return false
	}
}

// writeError writes a JSON-RPC error response.
func (h *ServerHandler) writeError(w http.ResponseWriter, id *int, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	resp := Response{
		JSONRPC: JSONRPCVersion,
		ID:      0,
		Error:   &RPCError{Code: code, Message: message},
	}
	if id != nil {
		resp.ID = *id
	}
	json.NewEncoder(w).Encode(resp)
}

// parseParams parses JSON-RPC params into a typed struct.
func parseParams(params any, target any) error {
	if params == nil {
		return nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
