package mcp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"openlimit/internal/config"
)

// serverState tracks the connection state of an MCP server.
type serverState struct {
	name      string
	client    *Client
	config    config.MCPServerConfig
	connected bool
	lastPing  time.Time
	lastError error

	// debounce for tools/list_changed notifications
	refreshTimer *time.Timer
}

// Manager manages connections to all configured MCP servers.
// It handles initialization, health checking, reconnection, and tool catalog updates.
type Manager struct {
	servers  map[string]*serverState
	registry *Registry
	logger   *slog.Logger
	cfg      config.MCPConfig

	mu     sync.RWMutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewManager creates a new MCP server manager.
func NewManager(cfg config.MCPConfig, registry *Registry, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		servers:  make(map[string]*serverState),
		registry: registry,
		logger:   logger.With("component", "mcp_manager"),
		cfg:      cfg,
	}
}

// Start connects to all configured MCP servers and starts background health checks.
func (m *Manager) Start(ctx context.Context) error {
	if !m.cfg.Enabled || len(m.cfg.Servers) == 0 {
		m.logger.Info("MCP disabled or no servers configured")
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})

	// Connect to all servers
	for _, serverCfg := range m.cfg.Servers {
		state := m.connectServer(childCtx, serverCfg)
		m.mu.Lock()
		m.servers[serverCfg.Name] = state
		m.mu.Unlock()
	}

	// Start health check loop
	go m.healthCheckLoop(childCtx)

	// Start notification listener for each connected server
	for _, state := range m.servers {
		if state.connected {
			go m.listenNotifications(childCtx, state)
		}
	}

	m.logger.Info("MCP manager started",
		"servers", len(m.cfg.Servers),
		"connected", m.connectedCount(),
		"tools", m.registry.ToolCount(),
	)

	return nil
}

// Stop gracefully shuts down all MCP server connections.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, state := range m.servers {
		if state.client != nil {
			state.client.Close()
		}
		if state.refreshTimer != nil {
			state.refreshTimer.Stop()
		}
	}
	if m.done != nil {
		close(m.done)
	}
}

// Done returns a channel that is closed when the manager stops.
func (m *Manager) Done() <-chan struct{} {
	return m.done
}

// ServerStatus returns the status of all MCP servers.
func (m *Manager) ServerStatus() []ServerStatusInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ServerStatusInfo, 0, len(m.servers))
	for _, state := range m.servers {
		toolCount := 0
		if state.connected && state.client != nil {
			toolCount = len(state.client.Tools())
		}
		result = append(result, ServerStatusInfo{
			Name:      state.name,
			Status:    connectedStatus(state.connected),
			Tools:     toolCount,
			LastError: lastErrStr(state.lastError),
		})
	}
	return result
}

// GetClient returns the MCP client for a connected server, or nil.
func (m *Manager) GetClient(serverName string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.servers[serverName]
	if !ok || !state.connected {
		return nil
	}
	return state.client
}

// ServerStatusInfo describes the status of a single MCP server.
type ServerStatusInfo struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Tools     int    `json:"tools"`
	LastError string `json:"last_error,omitempty"`
}

// connectServer creates a client and attempts to connect to an MCP server.
func (m *Manager) connectServer(ctx context.Context, cfg config.MCPServerConfig) *serverState {
	state := &serverState{
		name:   cfg.Name,
		config: cfg,
	}

	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	prefix := cfg.ToolPrefix
	if prefix == "" {
		prefix = cfg.Name
	}

	client := NewClient(cfg.Name, cfg.URL, cfg.Headers, timeout, prefix, m.logger)

	if err := client.Initialize(ctx); err != nil {
		m.logger.Warn("MCP server connection failed",
			"server", cfg.Name,
			"error", err,
		)
		state.lastError = err
		state.client = client // keep client for reconnect attempts
		state.connected = false
		return state
	}

	state.client = client
	state.connected = true
	state.lastPing = time.Now()

	// Register tools in the registry
	tools := client.Tools()
	m.registry.ReplaceServerTools(cfg.Name, tools)

	m.logger.Info("MCP server connected",
		"server", cfg.Name,
		"tools", len(tools),
	)

	return state
}

// healthCheckLoop periodically pings all servers and handles reconnection.
func (m *Manager) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAllServers(ctx)
		}
	}
}

// checkAllServers pings each server and handles disconnects/reconnects.
func (m *Manager) checkAllServers(ctx context.Context) {
	m.mu.RLock()
	states := make([]*serverState, 0, len(m.servers))
	for _, state := range m.servers {
		states = append(states, state)
	}
	m.mu.RUnlock()

	for _, state := range states {
		if !state.connected {
			// Try to reconnect
			m.tryReconnect(ctx, state)
			continue
		}

		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := state.client.Ping(pingCtx)
		cancel()

		if err != nil {
			m.logger.Warn("MCP server ping failed, marking disconnected",
				"server", state.name,
				"error", err,
			)
			state.connected = false
			state.lastError = err
			m.registry.RemoveServerTools(state.name)
		} else {
			state.lastPing = time.Now()
			state.lastError = nil
		}
	}
}

// tryReconnect attempts to reconnect a disconnected server with exponential backoff.
func (m *Manager) tryReconnect(ctx context.Context, state *serverState) {
	prefix := state.config.ToolPrefix
	if prefix == "" {
		prefix = state.config.Name
	}

	timeout := time.Duration(state.config.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := NewClient(state.config.Name, state.config.URL, state.config.Headers, timeout, prefix, m.logger)

	if err := client.Initialize(ctx); err != nil {
		state.lastError = err
		return // Will retry on next health check cycle
	}

	state.client.Close() // close old client
	state.client = client
	state.connected = true
	state.lastPing = time.Now()
	state.lastError = nil

	tools := client.Tools()
	m.registry.ReplaceServerTools(state.config.Name, tools)

	m.logger.Info("MCP server reconnected",
		"server", state.config.Name,
		"tools", len(tools),
	)

	// Start notification listener for reconnected server
	go m.listenNotifications(ctx, state)
}

// listenNotifications listens for server-initiated notifications on a client's
// notification channel and handles tool list changes.
func (m *Manager) listenNotifications(ctx context.Context, state *serverState) {
	ch := state.client.Notifications()
	if ch == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case notif, ok := <-ch:
			if !ok {
				return
			}
			if notif.Method == "notifications/tools/list_changed" {
				m.scheduleRefresh(ctx, state)
			}
		}
	}
}

// scheduleRefresh debounces tool list refresh requests (at most once every 5 seconds).
func (m *Manager) scheduleRefresh(ctx context.Context, state *serverState) {
	m.mu.Lock()
	if state.refreshTimer != nil {
		state.refreshTimer.Stop()
	}
	state.refreshTimer = time.AfterFunc(5*time.Second, func() {
		if !state.connected {
			return
		}
		m.logger.Info("refreshing tools after list_changed notification", "server", state.name)
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := state.client.RefreshTools(refreshCtx); err != nil {
			m.logger.Warn("tool refresh failed", "server", state.name, "error", err)
			return
		}
		m.registry.ReplaceServerTools(state.name, state.client.Tools())
	})
	m.mu.Unlock()
}

func (m *Manager) connectedCount() int {
	count := 0
	for _, state := range m.servers {
		if state.connected {
			count++
		}
	}
	return count
}

func connectedStatus(connected bool) string {
	if connected {
		return "connected"
	}
	return "disconnected"
}

func lastErrStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
