package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Session represents a connected MCP client session.
type Session struct {
	ID        string
	CreatedAt time.Time
	lastSeen  atomic.Int64 // UnixNano — safe to read/write under RLock
	notifier  chan json.RawMessage // buffered channel for SSE notifications
}

// SessionStore manages active MCP server sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	logger   *slog.Logger
}

// NewSessionStore creates a session store with the given TTL.
func NewSessionStore(ttl time.Duration, logger *slog.Logger) *SessionStore {
	if logger == nil {
		logger = slog.Default()
	}
	store := &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		logger:   logger.With("component", "mcp_session_store"),
	}
	if ttl > 0 {
		go store.evictLoop()
	}
	return store
}

// CreateOrGet creates a new session or returns an existing one if the ID is provided.
func (s *SessionStore) CreateOrGet(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id != "" {
		if sess, ok := s.sessions[id]; ok {
			sess.lastSeen.Store(time.Now().UnixNano())
		return sess
		}
	}

	if id == "" {
		id = generateSessionID()
	}

	sess := &Session{
		ID:        id,
		CreatedAt: time.Now(),
		notifier:  make(chan json.RawMessage, 32),
	}
	sess.lastSeen.Store(time.Now().UnixNano())
	s.sessions[id] = sess
	s.logger.Info("session created", "session_id", id)
	return sess
}

// Get returns a session by ID.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if ok {
		sess.lastSeen.Store(time.Now().UnixNano())
	}
	return sess, ok
}

// Remove removes a session.
func (s *SessionStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		close(sess.notifier)
		delete(s.sessions, id)
	}
}

// Count returns the number of active sessions.
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// Broadcast sends a notification to all active sessions.
func (s *SessionStore) Broadcast(method string, params any) {
	payload, err := json.Marshal(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		s.logger.Error("failed to marshal broadcast notification", "error", err)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sent := 0
	for _, sess := range s.sessions {
		select {
		case sess.notifier <- payload:
			sent++
		default:
			s.logger.Warn("session notification channel full, dropping", "session_id", sess.ID)
		}
	}
	s.logger.Debug("broadcast sent", "method", method, "sessions", sent)
}

// NotifyToolsChanged broadcasts a tools/list_changed notification to all sessions.
func (s *SessionStore) NotifyToolsChanged() {
	s.Broadcast("notifications/tools/list_changed", nil)
}

// evictLoop periodically removes expired sessions.
func (s *SessionStore) evictLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, sess := range s.sessions {
			lastSeen := time.Unix(0, sess.lastSeen.Load())
			if now.Sub(lastSeen) > s.ttl {
				close(sess.notifier)
				delete(s.sessions, id)
				s.logger.Info("session expired", "session_id", id)
			}
		}
		s.mu.Unlock()
	}
}

// ServeSSE writes server-initiated notifications as SSE events to the response writer.
// It blocks until the session is removed or the connection is closed.
func (sess *Session) ServeSSE(w http.ResponseWriter, flusher http.Flusher) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for payload := range sess.notifier {
		_, _ = writeSSE(w, payload)
		flusher.Flush()
	}
}

// writeSSE writes a server-sent event.
func writeSSE(w http.ResponseWriter, data json.RawMessage) (int, error) {
	n, _ := fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
	return n, nil
}

// generateSessionID generates a random session ID.
func generateSessionID() string {
	return "sess_" + randomHex(16)
}
