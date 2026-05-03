# Agent Protocols

**What you'll learn:** How to use OpenLimit's agent-native features — MCP client mode (tool discovery and governance), MCP server mode (expose virtual keys as tools), and A2A 1.0 (agent-to-agent communication).

All agent protocol features are fully opt-in with zero overhead when disabled.

---

## MCP Client Mode

Connect to external MCP (Model Context Protocol) servers, discover their tools, and merge them into chat completion requests with governance.

### Enable MCP client

```yaml
mcp:
  enabled: true
  max_tool_rounds: 5              # max agent loop rounds
  tool_timeout_ms: 10000          # per-tool execution timeout
  max_total_duration_seconds: 120  # cumulative timeout
  auto_inject_tools: false        # only merge when request has tools
  tool_conflict_strategy: "skip"  # "skip" or "error"
  servers:
    - name: weather
      url: "http://localhost:3001/mcp"
      tool_prefix: "weather"
      timeout_ms: 5000
    - name: github
      url: "http://localhost:3002/mcp"
      headers:
        Authorization: "Bearer ghp_xxx"
      tool_prefix: "github"
      timeout_ms: 10000
```

### How MCP client works

1. Gateway connects to each MCP server at startup, performs the JSON-RPC 2.0 initialization handshake
2. Discovers tools via `tools/list`, namespaces them with `tool_prefix` (`weather.get_forecast`, `github.create_issue`)
3. Background health checks (30s ping) with auto-reconnect on failure
4. Debounced tool refresh on `notifications/tools/list_changed` (5s per server)
5. When a chat request arrives, permitted MCP tools (filtered by virtual key `allowed_tools`) are merged into the request
6. Non-streaming: if the LLM responds with MCP tool calls, the gateway executes them and feeds results back
7. Multi-round loop continues until no more MCP tool calls, or max rounds/cumulative timeout reached

### Tool governance via virtual keys

Control which MCP tools each consumer can access:

```bash
curl -X POST http://localhost:8080/admin/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "proj_1",
    "name": "weather-key",
    "allowed_tools": ["weather.*"]
  }'
```

Glob patterns are supported: `weather.*` matches all weather tools, `github.create_issue` matches exactly one tool.

### Tool merge behavior

- Caller tools + permitted MCP tools are merged into the provider request
- `tool_choice: "none"` skips MCP tool injection entirely
- `tool_choice: "required"` + streaming + MCP tools → rejected with HTTP 400
- Duplicate tool names: caller wins by default. Configure `tool_conflict_strategy` to `"error"` for strict mode

### Multi-round agent loop

For non-streaming requests, the gateway implements an automatic agent loop:

1. Send request to provider (with merged tools)
2. If response contains tool calls → execute them via MCP servers
3. Feed tool results back to the LLM
4. Repeat until no more tool calls, or limits reached

Limits:
- `max_tool_rounds`: maximum number of round-trips (default: 5)
- `max_total_duration_seconds`: cumulative timeout across all rounds (default: 120s)
- Per-tool timeout: `tool_timeout_ms` (default: 10000ms)

### Admin endpoints

```bash
# List all MCP tools and server status
curl http://localhost:8080/admin/tools \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Filter by server
curl "http://localhost:8080/admin/tools?server=weather" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### MCP client limitations

- **HTTP transport only** — MCP servers must expose Streamable HTTP. Use `supergateway` to bridge stdio servers.
- **Streaming skips tool interception** — Tool calls in streaming responses pass through to the client.
- **No MCP resources, prompts, sampling, or elicitation** — Only the tools capability is implemented.
- **Parallel tool execution** — Multiple tool calls within one round execute concurrently. Stateful MCP servers may have issues.
- **No tool approval workflow** — Tools are auto-executed based on virtual key permissions.
- **No client TLS** — Server authentication via headers only.

---

## MCP Server Mode

Expose virtual keys as MCP tools so external MCP clients (Claude Desktop, Cursor, etc.) can invoke them directly.

### Enable MCP server mode

```yaml
mcp_server:
  enabled: true
  endpoint: "/mcp"                # path on main server, or ":8081" for separate port
  authentication:
    mode: "bearer_token"          # "none", "bearer_token", or "virtual_key"
    bearer_token: "my-secret"
  session_ttl_seconds: 3600
```

### Create a virtual key as MCP tool

```bash
curl -X POST http://localhost:8080/admin/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "proj_1",
    "name": "Chat Agent",
    "allow_mcp_server": true,
    "mcp_tool_name": "chat_agent",
    "allowed_models": ["gpt-4o", "gpt-4*"]
  }'
```

### How MCP server mode works

1. External MCP client connects to the gateway's MCP endpoint
2. Gateway performs the initialization handshake, creates a session
3. `tools/list` returns all virtual keys with `allow_mcp_server=true` as MCP tools
4. `tools/call` resolves the tool name → virtual key → validates model → executes chat completion
5. Response is returned as an MCP tool result with content blocks
6. When keys are created/revoked via admin API, `notifications/tools/list_changed` is broadcast to active sessions

### Tool naming

Priority order for tool names:
1. `mcp_tool_name` override (if set on the key)
2. Sanitized `key.name` (lowercase alphanumeric + underscore, max 64 chars)
3. `vk_<id[:8]>` fallback

Duplicate tool names get a numeric suffix.

### Authentication modes

| Mode | Description |
|---|---|
| `none` | No authentication — any client can connect |
| `bearer_token` | Static token from config |
| `virtual_key` | Clients authenticate with a virtual API key |

### MCP server admin endpoints

```bash
# List exposed tools
curl http://localhost:8080/admin/mcp/tools \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### MCP server limitations

- **Non-streaming only** — `tools/call` executes synchronously and returns a single result.
- **Virtual key must be active** — Revoked or expired keys are excluded from tool discovery.
- **No MCP resources, prompts, or sampling** — Only the tools capability.
- **Session notifications require SSE** — Client must establish SSE connection for `notifications/tools/list_changed`.

---

## A2A 1.0 Protocol

Agent-to-Agent protocol v1.0 enables external agents to discover and interact with the gateway.

### Enable A2A

```yaml
a2a:
  enabled: true
  endpoint: "/a2a"                     # path on main server, or ":8082" for separate port
  url: "http://localhost:8080"         # public URL for agent card
  authentication:
    mode: "bearer_token"               # "none" or "bearer_token"
    bearer_token: "a2a-secret"
  default_model: "gpt-4o-mini"         # model used for A2A messages
  blocking_mode: false                 # false = return immediately (default)
  max_workers: 10                      # concurrent background task executors
  task_ttl_seconds: 3600               # in-memory task TTL (unused with Postgres)
  max_tasks: 10000                     # max concurrent tasks (in-memory only)
  agent_card:
    name: "OpenLimit Gateway"
    version: "1.0.0"
    description: "AI Gateway with A2A support"
```

### Agent Card

External agents discover capabilities at:

```
GET /.well-known/agent.json
```

Returns the agent card with capabilities, skills, and authentication requirements.

### Task lifecycle

1. Agent sends `message/send` with user message
2. Gateway creates a task, enqueues it, and returns `submitted` (non-blocking mode)
3. Background worker picks up the task, executes chat completion
4. Client polls `tasks/get` or subscribes to SSE for real-time updates
5. Agent can cancel via `tasks/cancel` or list via `tasks/list`

### Blocking vs non-blocking

- **`blocking_mode: false`** (default): Returns immediately with `submitted` status. Client polls or streams for results.
- **`blocking_mode: true`**: Blocks until the task completes. Preserved for backward compatibility.

### SSE streaming

Real-time task updates via Server-Sent Events:

```bash
curl -N http://localhost:8080/a2a/tasks/task_abc123/stream \
  -H "Authorization: Bearer a2a-secret"
```

Events: `task_update` (status changes), `done` (terminal state), heartbeat every 15s.

### Push notifications

Include `pushNotification` in `message/send` params:

```json
{
  "message": {...},
  "pushNotification": {
    "url": "https://client.example.com/a2a/callback",
    "authentication": {"type": "bearer", "token": "secret"}
  }
}
```

Best-effort with 3 retries and exponential backoff. Order is not guaranteed.

### A2A request examples

```bash
# Send a message
curl -X POST http://localhost:8080/a2a \
  -H "Authorization: Bearer a2a-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "message/send",
    "id": 1,
    "params": {
      "message": {
        "role": "user",
        "messageId": "msg-001",
        "parts": [{"type": "text", "text": "What is the weather in Tokyo?"}]
      }
    }
  }'

# Get task status
curl -X POST http://localhost:8080/a2a \
  -H "Authorization: Bearer a2a-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tasks/get",
    "id": 2,
    "params": {"id": "task_abc123"}
  }'

# Cancel a task
curl -X POST http://localhost:8080/a2a \
  -H "Authorization: Bearer a2a-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tasks/cancel",
    "id": 3,
    "params": {"id": "task_abc123"}
  }'
```

### A2A error codes

| Code | Name | Meaning |
|---|---|---|
| -32001 | `TaskNotFound` | Task ID does not exist |
| -32002 | `TaskNotCancelable` | Task is already completed or failed |
| -32003 | `ContentTypeNotSupported` | Message contains unsupported content type |
| -32004 | `MaxTasksReached` | In-memory task limit reached |

### A2A limitations

- **Text parts only** — File and data parts in messages are ignored.
- **No multi-turn** — Each `message/send` creates a new task with no context carry-over.
- **Single default model** — No per-skill model routing.
- **SSE is single-instance** — For multi-instance, clients should poll `tasks/get`.
- **Push notifications are best-effort** — 3 retries, then give up. No dead-letter queue.
- **In-memory fallback** — Without a database, tasks are stored in memory and lost on restart.

---

## Prometheus metrics

Agent protocols expose dedicated metrics:

| Metric | Description |
|---|---|
| `mcp_server_connections` | Active MCP server connections |
| `mcp_tool_calls_total` | Total MCP tool calls |
| `mcp_tool_call_duration_seconds` | MCP tool call latency |
| `mcp_max_rounds_exceeded_total` | Agent loop max rounds reached |
| `gateway_a2a_tasks_created_total` | Total A2A tasks created |
| `gateway_a2a_task_completions_total` | A2A tasks completed by status |
| `gateway_a2a_task_duration_seconds` | A2A task execution duration |

---

## Next steps

- **[Governance](governance.md)** — Configure virtual key tool permissions
- **[Configuration](configuration.md)** — Full MCP and A2A YAML reference
- **[API Reference](api-reference.md)** — Admin endpoints for tool management
- **[Observability](observability.md)** — Monitor agent protocol metrics
