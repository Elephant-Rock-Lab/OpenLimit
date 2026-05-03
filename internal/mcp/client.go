package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Tool represents a namespaced tool discovered from an MCP server.
type Tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	ServerName   string         `json:"server_name"`
	RawName      string         `json:"raw_name"`
}

// Client connects to a single MCP server, handles the initialization handshake,
// and provides methods for listing tools and calling them.
type Client struct {
	name      string
	url       string
	headers   map[string]string
	timeout   time.Duration
	prefix    string
	transport *StreamableHTTP
	logger    *slog.Logger

	mu    sync.RWMutex
	tools []Tool
	ready bool
}

// NewClient creates a new MCP client for the given server.
func NewClient(name, url string, headers map[string]string, timeout time.Duration, prefix string, logger *slog.Logger) *Client {
	l := logger
	if l == nil {
		l = slog.Default()
	}
	return &Client{
		name:    name,
		url:     url,
		headers: headers,
		timeout: timeout,
		prefix:  prefix,
		logger:  l.With("mcp_server", name),
	}
}

// Initialize performs the MCP initialization handshake:
//  1. Send initialize request with protocol version and capabilities
//  2. Receive server capabilities
//  3. Send initialized notification
//
// After successful initialization, the client fetches the tool list.
func (c *Client) Initialize(ctx context.Context) error {
	c.transport = NewStreamableHTTP(c.url, c.headers, c.timeout, c.logger)

	// Step 1: Send initialize request
	params := InitializeRequestParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ClientCaps{
			Tools: &struct{}{},
		},
		ClientInfo: ImplementationInfo{
			Name:    "openlimit-gateway",
			Version: "1.0.0",
		},
	}

	resp, err := c.transport.SendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}

	// Parse initialize result
	var initResult InitializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	c.logger.Info("MCP server initialized",
		"server_info", initResult.ServerInfo.Name,
		"protocol_version", initResult.ProtocolVersion,
		"session_id", c.transport.SessionID(),
	)

	// Step 2: Send initialized notification
	if err := c.transport.SendNotification(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}

	// Step 3: Fetch tool list
	if err := c.refreshTools(ctx); err != nil {
		c.logger.Warn("failed to fetch tool list after initialization", "error", err)
		// Non-fatal: tools may be listed later
	}

	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()

	return nil
}

// ListTools returns the cached tool list from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.transport.SendRequest(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	// Namespace tools with the server's prefix
	tools := make([]Tool, len(result.Tools))
	for i, td := range result.Tools {
		tools[i] = Tool{
			Name:         c.prefix + "." + td.Name,
			Title:        td.Title,
			Description:  td.Description,
			InputSchema:  td.InputSchema,
			OutputSchema: td.OutputSchema,
			ServerName:   c.name,
			RawName:      td.Name,
		}
	}

	return tools, nil
}

// CallTool invokes a tool on the MCP server using the raw (un-prefixed) tool name.
func (c *Client) CallTool(ctx context.Context, rawName string, args map[string]any) (*CallToolResult, error) {
	params := CallToolParams{
		Name:      rawName,
		Arguments: args,
	}

	start := time.Now()
	resp, err := c.transport.SendRequest(ctx, "tools/call", params)
	elapsed := time.Since(start)

	if err != nil {
		c.logger.Error("tool call failed",
			"tool", rawName,
			"duration_ms", elapsed.Milliseconds(),
			"error", err,
		)
		return nil, err
	}
	if resp.Error != nil {
		c.logger.Error("tool call error",
			"tool", rawName,
			"duration_ms", elapsed.Milliseconds(),
			"code", resp.Error.Code,
			"message", resp.Error.Message,
		)
		return nil, resp.Error
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tool result: %w", err)
	}

	c.logger.Info("tool call completed",
		"tool", rawName,
		"duration_ms", elapsed.Milliseconds(),
		"is_error", result.IsError,
		"content_blocks", len(result.Content),
	)

	return &result, nil
}

// Ping sends a ping to check if the MCP server is alive.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.transport.SendRequest(ctx, "ping", PingParams{})
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("ping error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// Tools returns the cached (namespaced) tool list.
func (c *Client) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Tool, len(c.tools))
	copy(out, c.tools)
	return out
}

// Ready returns true if the client has completed initialization.
func (c *Client) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Name returns the server name.
func (c *Client) Name() string {
	return c.name
}

// Close shuts down the client's transport.
func (c *Client) Close() {
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()
	if c.transport != nil {
		c.transport.Close()
	}
}

// Notifications returns the notification channel from the transport.
func (c *Client) Notifications() <-chan *Notification {
	if c.transport == nil {
		return nil
	}
	return c.transport.Notifications()
}

// refreshTools fetches the tool list and updates the cache.
func (c *Client) refreshTools(ctx context.Context) error {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.tools = tools
	c.mu.Unlock()

	c.logger.Info("tool list refreshed", "tool_count", len(tools))
	return nil
}

// RefreshTools re-fetches the tool list from the server.
func (c *Client) RefreshTools(ctx context.Context) error {
	return c.refreshTools(ctx)
}
