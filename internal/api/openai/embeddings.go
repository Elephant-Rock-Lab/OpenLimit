package openaiapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openlimit/internal/auth"
	"openlimit/internal/providers"
	"openlimit/internal/requestid"
	"openlimit/internal/routing"
)

// EmbeddingsRequest represents an OpenAI-format embeddings request.
type EmbeddingsRequest struct {
	Model          string `json:"model"`
	Input          any    `json:"input"`
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     int    `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

// EmbeddingsResponse represents an OpenAI-format embeddings response.
type EmbeddingsResponse struct {
	Object string           `json:"object"`
	Data   []EmbeddingData  `json:"data"`
	Model  string           `json:"model"`
	Usage  *EmbeddingsUsage `json:"usage,omitempty"`
}

// EmbeddingData represents a single embedding in the response.
type EmbeddingData struct {
	Object    string `json:"object"`
	Embedding any    `json:"embedding"`
	Index     int    `json:"index"`
}

// EmbeddingsUsage represents token usage for an embeddings request.
type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// EmbeddingsHandlerFunc returns an http.HandlerFunc for POST /v1/embeddings.
func (h *Handler) EmbeddingsHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.Embeddings(w, r)
	}
}

// Embeddings handles POST /v1/embeddings by proxying to the configured provider.
func (h *Handler) Embeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	var req EmbeddingsRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON request body")
		return
	}

	if req.Model == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	if req.Input == nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "input is required")
		return
	}

	requestID := requestid.FromContext(r.Context())
	start := time.Now()

	// Route to provider
	plan, err := h.router.Plan(req.Model)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "model_not_found", err.Error())
		return
	}

	// Data residency filtering (consistent with chat completions)
	residency := strings.TrimSpace(r.Header.Get("X-Data-Residency"))
	if residency != "" {
		filtered := routing.FilterByResidency(plan.Targets, residency)
		if len(filtered) == 0 {
			writeError(w, r, http.StatusForbidden, "residency_denied", fmt.Sprintf("no providers available in data residency region %q", residency))
			return
		}
		plan.Targets = filtered
	}

	// Execute with retry across plan targets
	resp, target, attempts, execErr := h.executeEmbeddingsPlan(r.Context(), req, bodyBytes, plan)
	if execErr != nil {
		h.logger.Error("embeddings proxy failed",
			"request_id", requestID,
			"model", req.Model,
			"attempts", attempts,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", execErr,
		)

		var httpErr *providers.HTTPError
		if errors.As(execErr, &httpErr) {
			// Forward provider error body to client
			var errBody struct {
				Error *struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			msg := httpErr.Body
			if json.Unmarshal([]byte(httpErr.Body), &errBody) == nil && errBody.Error != nil && errBody.Error.Message != "" {
				msg = errBody.Error.Message
			}
			writeError(w, r, httpErr.StatusCode, "provider_error", msg)
			return
		}

		writeError(w, r, http.StatusBadGateway, "provider_error", execErr.Error())
		return
	}

	authCtx := auth.FromContext(r.Context())
	projectID := ""
	virtualKeyID := ""
	if authCtx != nil {
		projectID = authCtx.ProjectID
		virtualKeyID = authCtx.VirtualKeyID
	}

	h.logger.Info("embeddings proxied",
		"request_id", requestID,
		"model", req.Model,
		"provider", target.Provider,
		"provider_model", target.Model,
		"attempts", attempts,
		"duration_ms", time.Since(start).Milliseconds(),
		"project_id", projectID,
		"virtual_key_id", virtualKeyID,
	)

	w.Header().Set("X-Provider", target.Provider)
	writeJSON(w, http.StatusOK, resp)
}

// executeEmbeddingsPlan tries each target in the plan with retries.
func (h *Handler) executeEmbeddingsPlan(ctx context.Context, req EmbeddingsRequest, bodyBytes []byte, plan *routing.Plan) (*EmbeddingsResponse, providers.Target, int, error) {
	attemptsPerTarget := h.cfg.Routing.Defaults.Retry.Attempts
	if attemptsPerTarget <= 0 {
		attemptsPerTarget = 1
	}

	totalAttempts := 0
	var lastErr error

	for i, target := range plan.Targets {
		if i > 0 {
			h.metrics.RecordFallback(plan.Targets[i-1].Provider, target.Provider)
		}

		if _, ok := h.adapters[target.Provider]; !ok {
			lastErr = fmt.Errorf("provider adapter is not configured: %s", target.Provider)
			continue
		}
		keyRing := h.keys[target.Provider]

		for attempt := 1; attempt <= attemptsPerTarget; attempt++ {
			key, keyErr := h.nextProviderKey(target.Provider, keyRing)
			if keyErr != nil {
				lastErr = keyErr
				break
			}
			totalAttempts++

			// Build provider request with resolved model
			providerReq := make(map[string]any)
			if err := json.Unmarshal(bodyBytes, &providerReq); err != nil {
				return nil, providers.Target{}, totalAttempts, fmt.Errorf("failed to parse request: %w", err)
			}
			providerReq["model"] = target.Model

			providerBody, err := json.Marshal(providerReq)
			if err != nil {
				return nil, providers.Target{}, totalAttempts, fmt.Errorf("failed to marshal provider request: %w", err)
			}

			// Determine base URL: target override > provider config > default
			baseURL := h.getProviderBaseURL(target.Provider)
			if target.BaseURL != "" {
				baseURL = target.BaseURL
			}

			callCtx, cancel := ctx, func() {}
			if h.cfg.Routing.Defaults.TimeoutMS > 0 {
				callCtx, cancel = context.WithTimeout(ctx, time.Duration(h.cfg.Routing.Defaults.TimeoutMS)*time.Millisecond)
			}

			resp, callErr := callProviderEmbeddings(callCtx, baseURL, providerBody, key)
			cancel()

			if callErr == nil {
				return resp, target, totalAttempts, nil
			}

			lastErr = callErr
			if !providers.IsRetryable(callErr) || attempt == attemptsPerTarget {
				break
			}
			time.Sleep(backoff(attempt, h.cfg.Routing.Defaults.Retry))
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no provider targets available")
	}
	return nil, providers.Target{}, totalAttempts, lastErr
}

// callProviderEmbeddings sends the embeddings request to the provider and parses the response.
func callProviderEmbeddings(ctx context.Context, baseURL string, body []byte, key providers.ProviderKey) (*EmbeddingsResponse, error) {
	url := strings.TrimRight(baseURL, "/") + "/embeddings"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key.Value != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key.Value)
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", providers.ErrRetryable, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read provider response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var embeddingsResp EmbeddingsResponse
	if err := json.Unmarshal(data, &embeddingsResp); err != nil {
		return nil, fmt.Errorf("decode provider response: %w", err)
	}

	return &embeddingsResp, nil
}

// getProviderBaseURL returns the base URL for a given provider.
// It checks the provider config first, then falls back to the adapter if available.
func (h *Handler) getProviderBaseURL(providerName string) string {
	if pc, ok := h.cfg.Providers[providerName]; ok && pc.BaseURL != "" {
		return pc.BaseURL
	}
	return "https://api.openai.com/v1"
}
