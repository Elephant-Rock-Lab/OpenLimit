package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"openlimit/internal/config"
)

// VirtualKeyResolver resolves an A2A bearer token to a GovernanceIdentity
// via virtual key lookup. Returns IdentityProvider (not *openaiapi.GovernanceIdentity)
// to avoid circular imports — the openaiapi package handles the type assertion.
type VirtualKeyResolver interface {
	ResolveVirtualKey(ctx context.Context, token string) (IdentityProvider, error)
}

// A2A-specific JSON-RPC error codes (range -32000 to -32099).
const (
	CodeTaskNotFound            = -32001
	CodeTaskNotCancelable       = -32002
	CodeContentTypeNotSupported = -32003
	CodeMaxTasksReached         = -32004
)

// A2AHandler handles A2A v1.0 JSON-RPC requests and serves the agent card.
// It reuses the ChatExecutor callback from the MCP server (5B.3) to execute
// chat completions for incoming A2A messages.
type A2AHandler struct {
	cfg           config.A2AConfig
	store         TaskStore
	chatExecutor  ChatExecutor
	notifier      *TaskNotifier
	bridge        TaskBridgePublisher // optional bridge for multi-instance
	pushNotifier  *PushNotifier
	metrics       a2aMetricsRecorder
	defaultModel  string
	agentCardJSON []byte
	logger        *slog.Logger
	keyResolver   VirtualKeyResolver // optional: resolves virtual_key auth to identity

	// Worker pool
	workQueue    chan string
	maxWorkers   int
	wg           sync.WaitGroup
	cancelFuncs  map[string]context.CancelFunc
	cancelMu     sync.Mutex
	shuttingDown atomic.Bool
	closeOnce    sync.Once
}

// a2aMetricsRecorder records A2A task metrics.
type a2aMetricsRecorder interface {
	RecordA2ATaskCreated()
	RecordA2ATaskCompletion(status, model string, duration time.Duration)
}

// NewA2AHandler creates a new A2A handler with the given task store.
// The store is injected from outside (MemoryTaskStore or PersistentTaskStore).
func NewA2AHandler(cfg config.A2AConfig, chatExecutor ChatExecutor, store TaskStore, logger *slog.Logger) (*A2AHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "a2a")

	// Build agent card
	card := AgentCard{
		Name:            cfg.AgentCard.Name,
		URL:             cfg.URL,
		Version:         cfg.AgentCard.Version,
		Description:     cfg.AgentCard.Description,
		ProtocolVersion: "1.0",
		Capabilities: AgentCapabilities{
			Streaming:         true, // SSE streaming via GET /a2a/tasks/{id}/stream
			PushNotifications: true, // Webhook push notifications
		},
		Skills: []AgentSkill{
			{Name: "chat", Description: "General chat completion via the gateway"},
		},
	}
	if cfg.Authentication.Mode != "none" && cfg.Authentication.Mode != "" {
		card.Authentication = &AgentAuthInfo{
			Required: true,
			Type:     "bearer",
			Scheme:   "Bearer",
		}
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("marshal agent card: %w", err)
	}

	maxWorkers := cfg.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 10
	}

	h := &A2AHandler{
		cfg:           cfg,
		store:         store,
		chatExecutor:  chatExecutor,
		notifier:      NewTaskNotifier(),
		pushNotifier:  NewPushNotifier(logger),
		defaultModel:  cfg.DefaultModel,
		agentCardJSON: cardJSON,
		logger:        logger,
		workQueue:     make(chan string, maxWorkers*10),
		maxWorkers:    maxWorkers,
		cancelFuncs:   make(map[string]context.CancelFunc),
	}

	// Recover stale tasks from previous crash
	if recovered, err := store.RecoverStale(); err != nil {
		logger.Warn("failed to recover stale tasks", "error", err)
	} else if recovered > 0 {
		logger.Info("recovered stale A2A tasks from previous crash", "count", recovered)
	}

	// Start worker pool
	h.startWorkers()

	return h, nil
}

// Close is a legacy alias for Shutdown.
func (h *A2AHandler) Close() {
	h.Shutdown()
}

// Shutdown gracefully stops the worker pool, cancels in-flight tasks,
// stops the notifier, and closes the store. Safe to call multiple times.
func (h *A2AHandler) Shutdown() {
	h.closeOnce.Do(func() {
		// 1. Stop accepting new tasks
		h.shuttingDown.Store(true)
		close(h.workQueue)

		// 2. Cancel all in-flight tasks
		h.cancelMu.Lock()
		for _, cancel := range h.cancelFuncs {
			cancel()
		}
		h.cancelMu.Unlock()

		// 3. Wait for workers to finish
		h.wg.Wait()

		// 4a. Close Redis bridge (stop subscriber before notifier)
		if h.bridge != nil {
			h.bridge.Close()
		}

		// 4b. Close notifier (unsubscribe SSE watchers)
		if h.notifier != nil {
			h.notifier.Close()
		}

		// 5. Close store
		h.store.Close()

		h.logger.Info("A2A handler shut down")
	})
}

// startWorkers launches the worker goroutines.
func (h *A2AHandler) startWorkers() {
	for i := 0; i < h.maxWorkers; i++ {
		h.wg.Add(1)
		go h.worker()
	}
	h.logger.Info("A2A worker pool started", "workers", h.maxWorkers)
}

// worker reads task IDs from the work queue and executes them.
func (h *A2AHandler) worker() {
	defer h.wg.Done()
	for taskID := range h.workQueue {
		h.executeTask(taskID)
	}
}

// executeTask runs the chat completion for a single task.
func (h *A2AHandler) executeTask(taskID string) {
	// 1. Fetch task from store
	task, ok := h.store.Get(taskID)
	if !ok {
		h.logger.Warn("task not found in store, skipping", "task_id", taskID)
		return
	}

	// 2. Create cancellable context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	h.cancelMu.Lock()
	h.cancelFuncs[taskID] = cancel
	h.cancelMu.Unlock()
	defer func() {
		cancel()
		h.cancelMu.Lock()
		delete(h.cancelFuncs, taskID)
		h.cancelMu.Unlock()
	}()

	// 3. Update to working
	task.Status = TaskStateWorking
	h.store.Update(task)
	h.notify(task)

	// 4. Extract user text from history
	var userText string
	for _, msg := range task.History {
		for _, part := range msg.Parts {
			if part.Type == "text" && part.Text != "" {
				userText = part.Text
				break
			}
		}
		if userText != "" {
			break
		}
	}

	// 5. Execute chat completion
	execArgs := map[string]any{
		"model": h.defaultModel,
		"messages": []any{
			map[string]any{"role": "user", "content": userText},
		},
	}

	// Inject governance identity from task metadata (set during handleMessageSend)
	if identityRaw, ok := task.Metadata["governance_identity"]; ok {
		if idp, ok := identityRaw.(IdentityProvider); ok {
			execArgs["_governance_identity"] = idp
		}
	}

	start := time.Now()
	chatResult, err := h.chatExecutor(ctx, "a2a", execArgs)
	duration := time.Since(start)

	// 6. Update to terminal state
	if err != nil {
		var govErr GovernanceBlockedError
		var failText string
		if errors.As(err, &govErr) {
			failText = "Request blocked by governance policy: " + err.Error()
		} else if ctx.Err() == context.Canceled {
			task.Status = TaskStateCanceled
			failText = ""
		} else {
			failText = err.Error()
		}
		if failText != "" {
			failMsg := A2AMessage{
				Role:       "agent",
				Parts:      []A2APart{{Type: "text", Text: failText}},
				MessageID:  newMessageID(),
				ContextID:  task.ContextID,
			}
			task.Status = TaskStateFailed
			task.StatusMessage = &failMsg
			task.History = append(task.History, failMsg)
		}
		h.logger.Error("A2A task failed", "task_id", taskID, "error", err, "duration", duration)
	} else {
		task.Artifacts = append(task.Artifacts, A2AArtifact{
			Parts:     []A2APart{{Type: "text", Text: chatResult.Content}},
			Index:     len(task.Artifacts),
			LastChunk: true,
		})
		agentMsg := A2AMessage{
			Role:       "agent",
			Parts:      []A2APart{{Type: "text", Text: chatResult.Content}},
			MessageID:  newMessageID(),
			ContextID:  task.ContextID,
		}
		task.StatusMessage = &agentMsg
		task.History = append(task.History, agentMsg)
		task.Status = TaskStateCompleted
		task.Model = chatResult.Model
		task.UpdatedAt = time.Now()
		h.logger.Info("A2A task completed", "task_id", taskID, "model", chatResult.Model, "duration", duration)
	}

	h.store.Update(task)
	h.notify(task)

	// Record metrics
	if h.metrics != nil {
		model := task.Model
		if model == "" {
			model = h.defaultModel
		}
		h.metrics.RecordA2ATaskCompletion(string(task.Status), model, duration)
	}
}

// TaskBridgePublisher is the interface for publishing task updates across instances.
// Implemented by RedisTaskBridge and mock bridges in tests.
type TaskBridgePublisher interface {
	Publish(task *A2ATask)
	Start()
	Close()
}

// SetBridge sets the task bridge for multi-instance notification.
// Must be called before Start() / before any tasks are created.
func (h *A2AHandler) SetBridge(bridge TaskBridgePublisher) {
	h.bridge = bridge
}

// Notifier returns the underlying TaskNotifier for bridge wiring.
func (h *A2AHandler) Notifier() *TaskNotifier {
	return h.notifier
}

// notify sends task updates to the SSE notifier and push notification system.
func (h *A2AHandler) notify(task *A2ATask) {
	// SSE notifier (local)
	if h.notifier != nil {
		h.notifier.Notify(task)
	}
	// Redis bridge (cross-instance)
	if h.bridge != nil {
		h.bridge.Publish(task)
	}
	// Push notifications (best-effort)
	if h.pushNotifier != nil && task.Metadata != nil {
		if pushCfg := extractPushConfig(task); pushCfg != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.pushNotifier.Notify(ctx, task, pushCfg); err != nil {
				h.logger.Warn("push notification failed", "task_id", task.ID, "error", err)
			}
		}
	}
}

// extractPushConfig extracts push notification config from task metadata.
func extractPushConfig(task *A2ATask) *PushConfig {
	raw, ok := task.Metadata["pushNotification"]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var cfg PushConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	if cfg.URL == "" {
		return nil
	}
	return &cfg
}

// SetMetricsRecorder sets the metrics recorder for the handler.
func (h *A2AHandler) SetMetricsRecorder(m a2aMetricsRecorder) {
	h.metrics = m
}

// SetKeyResolver sets the virtual key resolver for virtual_key authentication mode.
func (h *A2AHandler) SetKeyResolver(r VirtualKeyResolver) {
	h.keyResolver = r
}

// SetAgentCardCapabilities updates the agent card capabilities and re-marshals it.
func (h *A2AHandler) SetAgentCardCapabilities(streaming, pushNotifications bool) {
	var card AgentCard
	json.Unmarshal(h.agentCardJSON, &card)
	card.Capabilities.Streaming = streaming
	card.Capabilities.PushNotifications = pushNotifications
	h.agentCardJSON, _ = json.Marshal(card)
}

// ServeHTTP implements http.Handler.
// Routes:
//
//	GET /.well-known/agent.json       → agent card
//	GET /a2a/tasks/{id}/stream       → SSE streaming
//	POST <endpoint>                   → JSON-RPC dispatch
func (h *A2AHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Agent card discovery
	if r.Method == http.MethodGet && r.URL.Path == "/.well-known/agent.json" {
		w.Header().Set("Content-Type", "application/json")
		w.Write(h.agentCardJSON)
		return
	}

	// SSE streaming for task updates
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/a2a/tasks/") && strings.HasSuffix(r.URL.Path, "/stream") {
		h.handleTaskStream(w, r)
		return
	}

	// A2A JSON-RPC endpoint
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	// Authenticate
	authIdentity, authed := h.authenticate(r)
	if !authed {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(Response{
			JSONRPC: JSONRPCVersion,
			Error:   &RPCError{Code: CodeInvalidRequest, Message: "unauthorized"},
		})
		return
	}

	// Store resolved identity for use in message/send
	if authIdentity != nil {
		r = r.WithContext(context.WithValue(r.Context(), a2aIdentityCtxKey{}, authIdentity))
	}

	// Decode JSON-RPC request
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, 0, CodeParseError, "parse error: "+err.Error())
		return
	}

	// Dispatch
	var result json.RawMessage
	var rpcErr *RPCError

	switch req.Method {
	case "message/send":
		result, rpcErr = h.handleMessageSend(r.Context(), req)
	case "tasks/get":
		result, rpcErr = h.handleTasksGet(req)
	case "tasks/cancel":
		result, rpcErr = h.handleTasksCancel(req)
	case "tasks/list":
		result, rpcErr = h.handleTasksList(req)
	default:
		rpcErr = &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
	}

	if rpcErr != nil {
		h.writeError(w, req.ID, rpcErr.Code, rpcErr.Message)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result:  result,
	})
}

// a2aIdentityCtxKey is the context key for resolved A2A identity.
type a2aIdentityCtxKey struct{}

// messageSendParams contains the parameters for message/send.
type messageSendParams struct {
	TaskID            string      `json:"taskId,omitempty"` // optional: continue existing task
	Message           A2AMessage  `json:"message"`
	ReturnImmediately *bool       `json:"returnImmediately,omitempty"`
	PushNotification  *PushConfig `json:"pushNotification,omitempty"`
}

// handleMessageSend processes a message/send request.
// When blocking_mode is true (or returnImmediately is false), it blocks until completion.
// Otherwise, it enqueues the task and returns immediately with status "submitted".
func (h *A2AHandler) handleMessageSend(ctx context.Context, req Request) (json.RawMessage, *RPCError) {
	var params messageSendParams
	if err := parseParams(req.Params, &params); err != nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}

	// Extract resolved identity from context (set by ServeHTTP after auth)
	var resolvedIdentity IdentityProvider
	if id, ok := ctx.Value(a2aIdentityCtxKey{}).(IdentityProvider); ok && id != nil {
		resolvedIdentity = id
	}

	// Extract text from parts (only text supported)
	var userText string
	for _, part := range params.Message.Parts {
		if part.Type == "text" {
			userText = part.Text
			break
		}
	}
	if userText == "" {
		return nil, &RPCError{
			Code:    CodeContentTypeNotSupported,
			Message: "only text parts are supported",
		}
	}

	// Determine blocking mode
	returnImmediately := !h.cfg.BlockingMode // default: non-blocking
	if params.ReturnImmediately != nil {
		returnImmediately = *params.ReturnImmediately
	}

	// Multi-turn: if taskId provided, load and append to existing task
	if params.TaskID != "" {
		existingTask, ok := h.store.Get(params.TaskID)
		if !ok {
			return nil, &RPCError{Code: CodeTaskNotFound, Message: "task not found: " + params.TaskID}
		}
		// If task is still working, return current state
		if existingTask.Status == TaskStateWorking {
			return marshalTaskResult(existingTask)
		}
		// Reset to submitted and append user message
		existingTask.Status = TaskStateSubmitted
		existingTask.History = append(existingTask.History, A2AMessage{
			Role:      params.Message.Role,
			Parts:     params.Message.Parts,
			MessageID: params.Message.MessageID,
			ContextID: params.Message.ContextID,
		})
		existingTask.UpdatedAt = time.Now()

		if !returnImmediately {
			return h.handleMessageSendBlockingWithMode(ctx, existingTask, false)
		}
		return h.enqueueNonBlocking(ctx, existingTask, false)
	}

	// Single-turn: create new task
	task := &A2ATask{
		ID:        newTaskID(),
		ContextID: params.Message.ContextID,
		Status:    TaskStateSubmitted,
		History: []A2AMessage{
			{
				Role:      params.Message.Role,
				Parts:     params.Message.Parts,
				MessageID: params.Message.MessageID,
				ContextID: params.Message.ContextID,
			},
		},
		Artifacts: []A2AArtifact{},
		Metadata:  map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if task.ContextID == "" {
		task.ContextID = newContextID()
	}

	// Store push config in metadata if provided
	if params.PushNotification != nil && params.PushNotification.URL != "" {
		task.Metadata["pushNotification"] = params.PushNotification
	}

	// Store resolved identity in metadata for governance (works for both blocking and non-blocking paths)
	if resolvedIdentity != nil {
		task.Metadata["governance_identity"] = resolvedIdentity
	}

	// Blocking mode: execute synchronously (backward compat)
	if !returnImmediately {
		return h.handleMessageSendBlocking(ctx, task)
	}

	return h.enqueueNonBlocking(ctx, task, true)
}

// enqueueNonBlocking stores the task and enqueues it for async processing.
func (h *A2AHandler) enqueueNonBlocking(ctx context.Context, task *A2ATask, isNew bool) (json.RawMessage, *RPCError) {
	if h.shuttingDown.Load() {
		return nil, &RPCError{Code: CodeMaxTasksReached, Message: "server is shutting down"}
	}

	select {
	case h.workQueue <- task.ID:
		// Enqueued successfully
	default:
		// Queue full — reject before storing
		return nil, &RPCError{Code: CodeMaxTasksReached, Message: "task queue full, try again later"}
	}

	// Multi-turn uses Update, single-turn uses Create
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	var storeErr error
	if isNew {
		storeErr = h.store.Create(task)
	} else {
		storeErr = h.store.Update(task)
	}
	if storeErr != nil {
		return nil, &RPCError{Code: CodeInternalError, Message: "failed to store task: " + storeErr.Error()}
	}

	if h.metrics != nil {
		h.metrics.RecordA2ATaskCreated()
	}

	return h.marshalTask(task)
}

// marshalTaskResult wraps a task in a JSON-RPC result.
func marshalTaskResult(task *A2ATask) (json.RawMessage, *RPCError) {
	result := struct {
		ID     string    `json:"id"`
		Status TaskState `json:"status"`
	}{
		ID:     task.ID,
		Status: task.Status,
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, &RPCError{Code: CodeInternalError, Message: err.Error()}
	}
	return data, nil
}

// handleMessageSendBlocking executes a task synchronously (legacy blocking mode).
func (h *A2AHandler) handleMessageSendBlocking(ctx context.Context, task *A2ATask) (json.RawMessage, *RPCError) {
	return h.handleMessageSendBlockingWithMode(ctx, task, true)
}

// handleMessageSendBlockingWithMode executes a task synchronously.
func (h *A2AHandler) handleMessageSendBlockingWithMode(ctx context.Context, task *A2ATask, isNew bool) (json.RawMessage, *RPCError) {
	var storeErr error
	if isNew {
		storeErr = h.store.Create(task)
	} else {
		storeErr = h.store.Update(task)
	}
	if storeErr != nil {
		return nil, &RPCError{Code: CodeMaxTasksReached, Message: "failed to store task: " + storeErr.Error()}
	}
	if h.metrics != nil {
		h.metrics.RecordA2ATaskCreated()
	}

	// Update to working
	task.Status = TaskStateWorking
	h.store.Update(task)

	// Execute chat completion synchronously
	execArgs := map[string]any{
		"model": h.defaultModel,
		"messages": []any{
			map[string]any{"role": "user", "content": extractTextFromHistory(task.History)},
		},
	}

	// Inject governance identity from task metadata
	if identityRaw, ok := task.Metadata["governance_identity"]; ok {
		if idp, ok := identityRaw.(IdentityProvider); ok {
			execArgs["_governance_identity"] = idp
		}
	}

	chatResult, err := h.chatExecutor(ctx, "a2a", execArgs)
	if err != nil {
		var govErr GovernanceBlockedError
		var failText string
		if errors.As(err, &govErr) {
			failText = "Request blocked by governance policy: " + err.Error()
		} else {
			failText = err.Error()
		}
		failMsg := A2AMessage{
			Role:       "agent",
			Parts:      []A2APart{{Type: "text", Text: failText}},
			MessageID:  newMessageID(),
			ContextID:  task.ContextID,
		}
		task.Status = TaskStateFailed
		task.StatusMessage = &failMsg
		task.History = append(task.History, failMsg)
		task.UpdatedAt = time.Now()
		h.store.Update(task)
		h.logger.Error("A2A task failed", "task_id", task.ID, "error", err)
		return h.marshalTask(task)
	}

	task.Artifacts = append(task.Artifacts, A2AArtifact{
		Parts:     []A2APart{{Type: "text", Text: chatResult.Content}},
		Index:     0,
		LastChunk: true,
	})
	// Append agent response to history for multi-turn continuity
	agentMsg := A2AMessage{
		Role:       "agent",
		Parts:      []A2APart{{Type: "text", Text: chatResult.Content}},
		MessageID:  newMessageID(),
		ContextID:  task.ContextID,
	}
	task.History = append(task.History, agentMsg)
	task.StatusMessage = &agentMsg
	task.Status = TaskStateCompleted
	task.Model = chatResult.Model
	task.UpdatedAt = time.Now()
	h.store.Update(task)

	h.logger.Info("A2A task completed (blocking)", "task_id", task.ID, "model", chatResult.Model)
	return h.marshalTask(task)
}

// extractTextFromHistory extracts text content from message history.
// For text parts, returns the text directly.
// For file parts, returns a description like "[file: <mime> <uri>]".
// For data parts, returns a JSON representation.
func extractTextFromHistory(history []A2AMessage) string {
	for _, msg := range history {
		for _, part := range msg.Parts {
			switch part.Type {
			case "text":
				if part.Text != "" {
					return part.Text
				}
			case "file":
				if part.FileURI != "" {
					return fmt.Sprintf("[file: %s %s]", part.FileMIMEType, part.FileURI)
				}
				if part.FileBytes != "" {
					return fmt.Sprintf("[file: %s <base64 %d bytes>]", part.FileMIMEType, len(part.FileBytes))
				}
			case "data":
				if part.Data != nil {
					jsonBytes, _ := json.Marshal(part.Data)
					return string(jsonBytes)
				}
			}
		}
	}
	return ""
}

// handleTasksGet returns a task by ID.
func (h *A2AHandler) handleTasksGet(req Request) (json.RawMessage, *RPCError) {
	var params struct {
		ID string `json:"id"`
	}
	if err := parseParams(req.Params, &params); err != nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if params.ID == "" {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "id is required"}
	}

	task, ok := h.store.Get(params.ID)
	if !ok {
		return nil, &RPCError{Code: CodeTaskNotFound, Message: "task not found: " + params.ID}
	}
	return h.marshalTask(task)
}

// handleTasksCancel cancels a task if not already in a terminal state.
func (h *A2AHandler) handleTasksCancel(req Request) (json.RawMessage, *RPCError) {
	var params struct {
		ID string `json:"id"`
	}
	if err := parseParams(req.Params, &params); err != nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if params.ID == "" {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "id is required"}
	}

	task, ok := h.store.Get(params.ID)
	if !ok {
		return nil, &RPCError{Code: CodeTaskNotFound, Message: "task not found: " + params.ID}
	}
	if task.Status.IsTerminal() {
		return nil, &RPCError{
			Code:    CodeTaskNotCancelable,
			Message: "task not cancelable: state is " + string(task.Status),
		}
	}

	// If working, cancel via context
	if task.Status == TaskStateWorking {
		h.cancelMu.Lock()
		if cancel, exists := h.cancelFuncs[params.ID]; exists {
			cancel()
		}
		h.cancelMu.Unlock()
	}

	// Directly update to canceled (for submitted tasks or as a fallback)
	task.Status = TaskStateCanceled
	h.store.Update(task)
	h.notify(task)

	return h.marshalTask(task)
}

// handleTasksList returns a paginated list of tasks.
func (h *A2AHandler) handleTasksList(req Request) (json.RawMessage, *RPCError) {
	var params struct {
		Filter struct {
			Status    string `json:"status"`
			ContextID string `json:"contextId"`
		} `json:"filter"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := parseParams(req.Params, &params); err != nil {
		return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}

	tasks, total, err := h.store.List(TaskListFilter{
		Status:    params.Filter.Status,
		ContextID: params.Filter.ContextID,
		Limit:     params.Limit,
		Offset:    params.Offset,
	})
	if err != nil {
		return nil, &RPCError{Code: CodeInternalError, Message: "list tasks: " + err.Error()}
	}

	type listResult struct {
		Tasks []*A2ATask `json:"tasks"`
		Total int        `json:"total"`
	}

	data, err := json.Marshal(listResult{Tasks: tasks, Total: total})
	if err != nil {
		return nil, &RPCError{Code: CodeInternalError, Message: "marshal result: " + err.Error()}
	}
	return json.RawMessage(data), nil
}

// handleTaskStream handles SSE streaming for task status updates.
// Endpoint: GET /a2a/tasks/{id}/stream
//
// Single-instance only: SSE watchers connect to the same gateway instance.
// For multi-instance deployments, clients should poll tasks/get.
func (h *A2AHandler) handleTaskStream(w http.ResponseWriter, r *http.Request) {
	// Authenticate (identity not needed for streaming — read-only)
	if _, authed := h.authenticate(r); !authed {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract task ID from path: /a2a/tasks/{id}/stream
	path := strings.TrimPrefix(r.URL.Path, "/a2a/tasks/")
	taskID := strings.TrimSuffix(path, "/stream")
	if taskID == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}

	// Check if task exists
	task, ok := h.store.Get(taskID)
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// If already terminal, send single event and close
	if task.Status.IsTerminal() {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fmt.Fprintf(w, "event: task_update\ndata: %s\n\n", h.mustMarshal(task))
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Subscribe to updates (subscribe-then-check pattern)
	ch := h.notifier.Subscribe(taskID)
	defer h.notifier.Unsubscribe(taskID, ch)

	// Re-check status after subscribing (prevent race)
	task, ok = h.store.Get(taskID)
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Heartbeat goroutine
	heartbeatCtx, heartbeatCancel := context.WithCancel(r.Context())
	defer heartbeatCancel()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}()

	// If already terminal after subscribe, send and close
	if task.Status.IsTerminal() {
		fmt.Fprintf(w, "event: task_update\ndata: %s\n\n", h.mustMarshal(task))
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Stream updates until terminal or disconnect
	for {
		select {
		case <-r.Context().Done():
			return
		case updated, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: task_update\ndata: %s\n\n", h.mustMarshal(updated))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if updated.Status.IsTerminal() {
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				return
			}
		}
	}
}

// mustMarshal marshals a task to JSON, returning empty object on error.
func (h *A2AHandler) mustMarshal(task *A2ATask) []byte {
	data, err := json.Marshal(task)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// marshalTask serializes a task to JSON-RPC result.
func (h *A2AHandler) marshalTask(task *A2ATask) (json.RawMessage, *RPCError) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, &RPCError{Code: CodeInternalError, Message: "marshal task: " + err.Error()}
	}
	return json.RawMessage(data), nil
}

// authenticate validates the incoming request against the configured auth mode.
// Returns (identity, true) on success. identity is non-nil only for virtual_key mode.
// Returns (nil, false) on authentication failure.
func (h *A2AHandler) authenticate(r *http.Request) (IdentityProvider, bool) {
	switch h.cfg.Authentication.Mode {
	case "virtual_key":
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return nil, false
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if !strings.HasPrefix(token, "gw-") {
			return nil, false
		}
		if h.keyResolver == nil {
			return nil, false
		}
		identity, err := h.keyResolver.ResolveVirtualKey(r.Context(), token)
		if err != nil {
			return nil, false
		}
		return identity, true
	case "bearer_token":
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return nil, false
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return nil, subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.Authentication.BearerToken)) == 1
	default: // "none" or empty
		return nil, true
	}
}

// writeError writes a JSON-RPC error response.
func (h *A2AHandler) writeError(w http.ResponseWriter, id, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	})
}
