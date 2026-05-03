package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"openlimit/internal/admin"
	openaiapi "openlimit/internal/api/openai"
	"openlimit/internal/audit"
	"openlimit/internal/auth"
	"openlimit/internal/billing"
	"openlimit/internal/cache"
	"openlimit/internal/config"
	"openlimit/internal/guardrails"
	"openlimit/internal/health"
	"openlimit/internal/kms"
	"openlimit/internal/lifecycle"
	"openlimit/internal/mcp"
	"openlimit/internal/metrics"
	oidcPkg "openlimit/internal/oidc"
	"openlimit/internal/providers"
	anthropicadapter "openlimit/internal/providers/anthropic"
	azureadapter "openlimit/internal/providers/azure"
	geminiadapter "openlimit/internal/providers/gemini"
	openaiadapter "openlimit/internal/providers/openai"
	rediscli "openlimit/internal/redis"
	"openlimit/internal/routing"
	openaischema "openlimit/internal/schema/openai"
	"openlimit/internal/tracing"
	"openlimit/internal/usage"
)

type Runtime struct {
	Server           *http.Server
	Tracker          *lifecycle.Tracker
	MetricsCollector *metrics.Collector
	Tracer           *tracing.Tracer
	MCPManager       *mcp.Manager
	A2AHandler       *mcp.A2AHandler
}

func New(cfg config.Config, logger *slog.Logger, db *sql.DB) *http.Server {
	return NewRuntime(cfg, logger, db).Server
}

func NewRuntime(cfg config.Config, logger *slog.Logger, db *sql.DB) *Runtime {
	tracker := lifecycle.NewTracker()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.Handler)

	// Prometheus metrics (created early for router and admin handler)
	metricsCollector := metrics.NewCollector(cfg.Telemetry.Metrics.Enabled)

	router := routing.New(cfg.Models, cfg.Providers, cfg.Routing, metricsCollector)

	// Health tracker — single instance shared by Router and Handler (AUTH-05).
	healthTracker := health.NewTracker(30 * time.Second)
	router.SetHealthTracker(healthTracker)
	var redisClient *rediscli.Client
	if cfg.Redis.Enabled {
		redisClient = rediscli.NewClient(
			cfg.Redis.Addr,
			cfg.Redis.Password,
			cfg.Redis.DB,
			cfg.Redis.MaxRetries,
			cfg.Redis.PoolSize,
			time.Duration(cfg.Redis.HealthCheckIntervalSec)*time.Second,
			logger,
			cfg.Redis.Cluster,
		)
		logger.Info("Redis client configured", "addr", cfg.Redis.Addr)
	}

	var exactCache cache.Cache
	if cfg.Cache.Exact.Enabled {
		if redisClient != nil && redisClient.Healthy() {
			ttl := time.Duration(cfg.Cache.Exact.TTLSeconds) * time.Second
			exactCache = cache.NewRedisCache(redisClient, ttl)
			logger.Info("exact cache: Redis-backed", "ttl", ttl)
		} else {
			exactCache = cache.NewExactLRU(cfg.Cache.Exact.MaxEntries)
			logger.Info("exact cache: in-memory LRU")
		}
	}

	// KMS fetcher for provider key encryption
	var kmsFetcher kms.KeyFetcher
	if cfg.KMS.Enabled {
		var err error
		kmsFetcher, err = kms.NewKeyFetcher(cfg.KMS.Type, cfg.KMS.KeyID, kms.VaultFetcherConfig{
			Addr:          cfg.KMS.Vault.Addr,
			Token:         cfg.KMS.Vault.Token,
			Namespace:     cfg.KMS.Vault.Namespace,
			TLSSkipVerify: cfg.KMS.Vault.TLSSkipVerify,
		})
		if err != nil {
			logger.Error("KMS initialization failed, provider keys with encrypted_value will not work",
				"error", err,
				"type", cfg.KMS.Type,
			)
			// kmsFetcher stays nil — encrypted keys will be skipped
		} else {
			logger.Info("KMS enabled", "type", cfg.KMS.Type)
		}
	}

	adapters := map[string]providers.Adapter{}
	keys := map[string]*providers.KeyRing{}
	for name, providerCfg := range cfg.Providers {
		switch providerCfg.Type {
		case "openai", "openai-compatible", "":
			adapters[name] = openaiadapter.New(name, providerCfg.BaseURL)
		case "anthropic":
			adapters[name] = anthropicadapter.New(name, providerCfg.BaseURL)
		case "gemini":
			adapters[name] = geminiadapter.New(name, providerCfg.BaseURL, providerCfg.GeminiModelMap)
		case "azure-openai":
			adapters[name] = azureadapter.New(name, providerCfg.AzureResource, providerCfg.AzureAPIVersion)
		}
		keyRing := providers.NewKeyRing(providerCfg, kmsFetcher)
		keys[name] = keyRing
		logProviderKeyWarnings(logger, name, providerCfg, keyRing)
	}

	// OIDC provider (created early for health check, even if admin is disabled)
	var oidcProvider *oidcPkg.Provider
	if cfg.Admin.OIDC.Enabled && db != nil {
		op, err := oidcPkg.NewProvider(oidcPkg.ProviderConfig{
			Issuer:      cfg.Admin.OIDC.Issuer,
			Audience:    cfg.Admin.OIDC.Audience,
			DefaultRole: cfg.Admin.OIDC.DefaultRole,
		}, logger)
		if err != nil {
			logger.Error("failed to initialize OIDC provider", "error", err)
		} else {
			oidcProvider = op
		}
	}

	mux.HandleFunc("GET /ready", health.ReadyHandlerWithOIDC(cfg, keys, tracker, oidcProvider))

	// MCP server manager
	var mcpRegistry *mcp.Registry
	var mcpManager *mcp.Manager
	var mcpExecutor *mcp.Executor
	if cfg.MCP.Enabled {
		mcpRegistry = mcp.NewRegistry()
		mcpManager = mcp.NewManager(cfg.MCP, mcpRegistry, logger)
		if err := mcpManager.Start(context.Background()); err != nil {
			logger.Warn("MCP manager start failed, continuing without MCP", "error", err)
		} else {
			maxRounds := cfg.MCP.MaxToolRounds
			if maxRounds <= 0 {
				maxRounds = 5
			}
			maxTotal := time.Duration(cfg.MCP.MaxTotalDurationSec) * time.Second
			if maxTotal <= 0 {
				maxTotal = 120 * time.Second
			}
			mcpExecutor = mcp.NewExecutor(mcpRegistry, mcpManager, maxRounds, maxTotal, nil, logger)
			logger.Info("MCP enabled",
				"servers", len(cfg.MCP.Servers),
				"tools", mcpRegistry.ToolCount(),
				"max_rounds", maxRounds,
			)
		}
	}

	// MCP server mode (external MCP clients connect to the gateway)
	var mcpServerHandler *mcp.ServerHandler
	if cfg.MCPServer.Enabled {
		var toolLister mcp.ToolLister
		if db != nil {
			toolLister = mcp.NewDBToolLister(db, logger)
		}
		mcpServerHandler = mcp.NewServerHandler(cfg.MCPServer, db, toolLister, logger)

		if cfg.MCPServer.IsSeparatePort() {
			// Start on separate listener
			separateMux := http.NewServeMux()
			separateMux.Handle(cfg.MCPServer.Path(), mcpServerHandler)
			go func() {
				logger.Info("MCP server listening", "addr", cfg.MCPServer.ListenAddr())
				if err := http.ListenAndServe(cfg.MCPServer.ListenAddr(), separateMux); err != nil {
					logger.Error("MCP server stopped", "error", err)
				}
			}()
		} else {
			// Serve on the same port as the main gateway
			mux.Handle(cfg.MCPServer.Path(), mcpServerHandler)
		}

		logger.Info("MCP server mode enabled",
			"endpoint", cfg.MCPServer.Endpoint,
			"auth_mode", cfg.MCPServer.Auth.Mode,
		)
	}

	// Admin API (only when admin is enabled and database is available)
	var auditLog *audit.Logger
	if cfg.Admin.Enabled && db != nil {
		auditLog = audit.NewLogger(db, logger, 1000)
	}

	// Audit provider key decryption
	if kmsFetcher != nil && auditLog != nil {
		for name := range keys {
			if keys[name].ActiveCount() > 0 {
				hasEncrypted := false
				for _, k := range cfg.Providers[name].Keys {
					if k.EncryptedValue != "" {
						hasEncrypted = true
						break
					}
				}
				if hasEncrypted {
					auditLog.Record(audit.Event{
						EventType: audit.EventKeyDecrypt,
						Actor:     "system",
						Action:    "decrypt",
						Resource:  "provider:" + name,
						Outcome:   "success",
						Metadata:  map[string]any{"active_keys": keys[name].ActiveCount()},
					})
				}
			}
		}
	}

	if cfg.Admin.Enabled && db != nil {
		adminHandler := admin.NewHandler(db, cfg, auditLog, metricsCollector)
		adminHandler.OnKeysChanged = func() {
			if mcpServerHandler != nil {
				mcpServerHandler.Sessions().NotifyToolsChanged()
			}
		}
		adminRoutes := http.NewServeMux()
		adminHandler.RegisterRoutes(adminRoutes)

		// MCP tools admin endpoint
		if mcpManager != nil {
			statusFn := func() []admin.ToolServerInfo {
				statuses := mcpManager.ServerStatus()
				result := make([]admin.ToolServerInfo, len(statuses))
				for i, s := range statuses {
					tools := mcpRegistry.ToolsByServer(s.Name)
					details := make([]admin.ToolDetail, len(tools))
					for j, t := range tools {
						details[j] = admin.ToolDetail{
							Name:        t.Name,
							Description: t.Description,
							InputSchema: t.InputSchema,
						}
					}
					result[i] = admin.ToolServerInfo{
						Name:      s.Name,
						Status:    s.Status,
						ToolCount: s.Tools,
						Tools:     details,
					}
				}
				return result
			}
			adminRoutes.HandleFunc("GET /admin/tools", admin.ToolsHandler(statusFn))
		}

		// MCP server tools admin endpoint
		if mcpServerHandler != nil && db != nil {
			mcpToolLister := func() ([]map[string]any, error) {
				tools, err := mcpServerHandler.ToolLister()
				if err != nil {
					return nil, err
				}
				result := make([]map[string]any, len(tools))
				for i, t := range tools {
					result[i] = map[string]any{
						"name":        t.Name,
						"description": t.Description,
						"inputSchema": t.InputSchema,
					}
				}
				return result, nil
			}
			adminRoutes.HandleFunc("GET /admin/mcp/tools", admin.MCPToolsHandler(mcpToolLister))
		}

		// OIDC provider (reuse from early creation)
		var oidcLookup oidcPkg.UserLookupFunc
		if oidcProvider != nil {
			oidcLookup = oidcPkg.DBLookup(db, cfg.Admin.OIDC.DefaultRole)
		}

		// Admin health endpoints (tracker-aware)
		adminRoutes.HandleFunc("GET /admin/health/providers", health.AdminProviderHealth(healthTracker))
		adminRoutes.HandleFunc("GET /admin/health/models", health.AdminModelHealth(healthTracker))

		mux.Handle("/admin/", admin.BearerAuth(cfg.Admin.BearerToken, auditLog, oidcProvider, oidcLookup, adminRoutes))
	}

	// Billing: price table and async usage writer
	var priceTable *billing.PriceTable
	var usageWriter *usage.Writer
	if db != nil {
		if len(cfg.Billing.Prices) > 0 {
			priceTable = billing.NewPriceTable(cfg.Billing.Prices)
		}
		usageWriter = usage.NewWriter(db, logger, 1000)
	}

	// Prometheus metrics (handler registration)
	mux.Handle("GET /metrics", metricsCollector.MetricsHandler())

	// OpenTelemetry tracing
	tracer, err := tracing.NewTracer(
		cfg.Telemetry.Tracing.Enabled,
		cfg.Telemetry.Tracing.Endpoint,
		cfg.Telemetry.Tracing.ServiceName,
		cfg.Telemetry.Tracing.SampleRate,
		logger,
	)
	if err != nil {
		logger.Warn("tracing initialization failed, continuing without tracing", "error", err)
		tracer = &tracing.Tracer{}
	}

	// Guardrails
	var guardrailPipeline *guardrails.Pipeline
	if cfg.Guardrails.Enabled {
		var err error
		guardrailPipeline, err = guardrails.BuildPipeline(cfg.Guardrails)
		if err != nil {
			logger.Warn("guardrail pipeline build failed, continuing without guardrails", "error", err)
			guardrailPipeline = guardrails.NewPipeline(nil, nil)
		}
		logger.Info("guardrails enabled",
			"input_stages", guardrailPipeline.InputStages(),
			"output_stages", guardrailPipeline.OutputStages(),
		)
	}

	if guardrailPipeline != nil && auditLog != nil {
		guardrailPipeline.SetAuditLogger(auditLog)
	}

	openAIHandler := openaiapi.NewHandler(cfg, logger, router, exactCache, adapters, keys, priceTable, usageWriter, metricsCollector, tracer, guardrailPipeline, mcpRegistry, mcpExecutor, redisClient)

	// Wire health tracker into handler (same instance as router — AUTH-05).
	openAIHandler.SetHealthTracker(healthTracker)

	// Wire MCP server executor: tool calls → chat completions pipeline
	var serverExec *mcp.ServerChatExecutor
	if mcpServerHandler != nil && db != nil {
		resolver := mcp.NewDBKeyResolver(db)
		serverExec = mcp.NewServerChatExecutor(resolver, openAIHandler, logger)
		mcpServerHandler.SetChatExecutor(serverExec.Execute)
	}

	// A2A server mode (Agent-to-Agent protocol v1.0)
	var a2aHandlerPtr *mcp.A2AHandler
	if cfg.A2A.Enabled {
		// A2A calls ExecuteForMCP directly — bypasses ServerChatExecutor tool-name
		// resolution. A2A uses nil identity (gateway-level auth, no per-key governance).
		a2aExec := func(ctx context.Context, toolName string, args map[string]any) (*mcp.ChatResult, error) {
			model, _ := args["model"].(string)
			if model == "" {
				model = cfg.A2A.DefaultModel
			}

			var messages []openaischema.ChatMessage
			if msgs, ok := args["messages"].([]any); ok {
				for _, m := range msgs {
					if mMap, ok := m.(map[string]any); ok {
						role, _ := mMap["role"].(string)
						content, _ := mMap["content"].(string)
						contentJSON, _ := json.Marshal(content)
						messages = append(messages, openaischema.ChatMessage{
							Role:    role,
							Content: contentJSON,
						})
					}
				}
			}

			resp, err := openAIHandler.ExecuteForMCP(ctx, openaischema.ChatCompletionRequest{
				Model:    model,
				Messages: messages,
			}, nil)
			if err != nil {
				return nil, err
			}

			result := &mcp.ChatResult{Model: resp.Model}
			if len(resp.Choices) > 0 {
				content := string(resp.Choices[0].Message.Content)
				if len(content) >= 2 && content[0] == '"' && content[len(content)-1] == '"' {
					content = content[1 : len(content)-1]
				}
				result.Content = content
				result.FinishReason = string(resp.Choices[0].FinishReason)
			}
			if resp.Usage != nil {
				result.Usage = &mcp.ChatUsage{
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
					TotalTokens:      resp.Usage.TotalTokens,
				}
			}
			return result, nil
		}

		// Create task store: Postgres-backed when DB available, in-memory otherwise
		var a2aStore mcp.TaskStore
		if db != nil {
			a2aStore = mcp.NewPersistentTaskStore(db)
			logger.Info("A2A task store: Postgres-backed")
		} else {
			maxTasks := cfg.A2A.MaxTasks
			if maxTasks <= 0 {
				maxTasks = 10000
			}
			ttlSec := cfg.A2A.TaskTTLSec
			if ttlSec <= 0 {
				ttlSec = 3600
			}
			a2aStore = mcp.NewMemoryTaskStore(maxTasks, time.Duration(ttlSec)*time.Second)
			logger.Info("A2A task store: in-memory (tasks lost on restart)")
		}

		a2aHandler, err := mcp.NewA2AHandler(cfg.A2A, a2aExec, a2aStore, logger)
		if err != nil {
			logger.Warn("A2A handler creation failed", "error", err)
		} else {
			a2aHandlerPtr = a2aHandler

			// Wire Redis bridge for multi-instance A2A SSE
			if redisClient != nil && redisClient.Healthy() {
				bridge := mcp.NewRedisTaskBridge(
					redisClient.Standalone(),
					"openlimit:a2a:task_updates",
					a2aHandler.Notifier(),
					mcp.NewInstanceID(),
					logger,
				)
				a2aHandler.SetBridge(bridge)
				bridge.Start()
				logger.Info("A2A Redis bridge enabled for multi-instance SSE")
			}

			// Wire metrics
			a2aHandler.SetMetricsRecorder(metricsCollector)

			// Agent card is always on the main server
			mux.Handle("/.well-known/agent.json", a2aHandler)

			if cfg.A2A.A2AIsSeparatePort() {
				separateMux := http.NewServeMux()
				separateMux.Handle(cfg.A2A.A2APath(), a2aHandler)
				go func() {
					logger.Info("A2A server listening", "addr", cfg.A2A.A2AListenAddr())
					if err := http.ListenAndServe(cfg.A2A.A2AListenAddr(), separateMux); err != nil {
						logger.Error("A2A server stopped", "error", err)
					}
				}()
			} else {
				mux.Handle(cfg.A2A.A2APath(), a2aHandler)
			}

			logger.Info("A2A server mode enabled",
				"endpoint", cfg.A2A.Endpoint,
				"default_model", cfg.A2A.DefaultModel,
				"auth_mode", cfg.A2A.Authentication.Mode,
			)
		}
	}
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /v1/chat/completions", openAIHandler.ChatCompletions)
	apiMux.HandleFunc("POST /v1/embeddings", openAIHandler.Embeddings)

	modelsHandler := openaiapi.NewModelsHandler(cfg, logger)
	apiMux.HandleFunc("GET /v1/models", modelsHandler.Models)

	// Auth middleware wraps the API endpoints (pass-through when disabled)
	authMW := auth.NewMiddleware(cfg.Auth, db)
	protectedAPI := authMW.Wrap(apiMux)

	// Route /v1/ to protected handler
	mux.Handle("/v1/", protectedAPI)

	handler := trackingMiddleware(tracker, metricsCollector, requestIDMiddleware(loggingMiddleware(logger, tracer.HTTPMiddleware(mux))))

	return &Runtime{
		Server: &http.Server{
			Addr:         cfg.Server.Address(),
			Handler:      handler,
			ReadTimeout:  durationMS(cfg.Server.ReadTimeoutMS),
			WriteTimeout: durationMS(cfg.Server.WriteTimeoutMS),
			IdleTimeout:  durationMS(cfg.Server.IdleTimeoutMS),
		},
		Tracker:          tracker,
		MetricsCollector: metricsCollector,
		Tracer:           tracer,
		MCPManager:       mcpManager,
		A2AHandler:       a2aHandlerPtr,
	}
}

func logProviderKeyWarnings(logger *slog.Logger, name string, providerCfg config.ProviderConfig, keyRing *providers.KeyRing) {
	for _, missing := range keyRing.MissingEnv() {
		logger.Warn("provider key environment variable is not set",
			"provider", name,
			"key_id", missing.KeyID,
			"env", missing.Env,
		)
	}
	if health.RequiresAuth(providerCfg) && keyRing.ActiveCount() == 0 {
		logger.Warn("provider requires authentication but has no active keys",
			"provider", name,
			"type", providerCfg.Type,
		)
	}
}

func durationMS(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}
