# Plugins

**Audience:** Developers, platform engineers

OpenLimit supports a plugin system that lets you extend the gateway with custom guardrails, HTTP middleware, and provider adapters — without modifying core source code.

---

## Overview

Plugins are Go code that implements one of three extension interfaces:

| Type | Interface | Purpose |
|:-----|:----------|:---------|
| **Guardrail** | `GuardrailPlugin` | Custom input/output guardrail stages |
| **Middleware** | `MiddlewarePlugin` | HTTP request/response middleware |
| **Provider** | `ProviderPlugin` | Custom provider adapters |

Plugins are registered at init time via `plugins.Register()` and activated through the `plugins` section of `gateway.yaml`.

---

## Plugin Architecture

```
gateway.yaml                  Go Code
───────────                   ────────
plugins:                      func init() {
  - name: my-plugin             plugins.Register(&MyPlugin{})
    type: guardrail           }
    config:
      option: value           ↓
                              Gateway loads plugin config
                              from yaml, calls Init()
                              ↓
guardrails:                   Guardrail pipeline calls
  input:                      ProcessInput() / ProcessOutput()
    - type: plugin
      config:
        name: my-plugin
```

**Registration** happens at init time via `plugins.Register()` in a Go `init()` function.

**Activation** happens via config — the `plugins` array in `gateway.yaml` tells the gateway which plugins to initialize and with what configuration.

**Integration** happens at the point of use — guardrail plugins are wired into the guardrail pipeline, middleware plugins wrap HTTP handlers, provider plugins create adapters.

---

## Plugin Interfaces

All plugins implement the base `Plugin` interface:

```go
type Plugin interface {
    Name() string                          // Unique identifier
    Type() string                          // "guardrail", "middleware", or "provider"
    Init(config map[string]any) error      // Initialize with config
}
```

### GuardrailPlugin

Custom guardrail stages that process input before it's sent to the provider, and output after it's received.

```go
type GuardrailPlugin interface {
    Plugin
    ProcessInput(ctx GuardrailContext) (GuardrailContext, error)
    ProcessOutput(ctx GuardrailContext) (GuardrailContext, error)
}
```

### MiddlewarePlugin

HTTP middleware that wraps request handlers.

```go
type MiddlewarePlugin interface {
    Plugin
    Middleware() func(http.Handler) http.Handler
}
```

### ProviderPlugin

Custom provider adapters for non-standard AI providers.

```go
type ProviderPlugin interface {
    Plugin
    CreateAdapter(config map[string]any) (ProviderAdapter, error)
}
```

---

## GuardrailContext

The `GuardrailContext` carries data through the guardrail pipeline:

```go
type GuardrailContext struct {
    Message     string         // Text content being processed
    Metadata    map[string]any // Arbitrary key-value data
    Blocked     bool           // Set to true to block the request
    BlockReason string         // Why the request was blocked
    Modified    bool           // Set to true if content was changed
}
```

**Blocking:** Set `Blocked = true` and provide a `BlockReason`. The pipeline will reject the request with a 400 error.

**Modifying:** Change `Message` and set `Modified = true`. The modified content replaces the original.

**Passing:** Return the context unchanged.

---

## Writing a Guardrail Plugin

Here's a complete example — a length guardrail that blocks messages outside configurable bounds:

```go
package plugins

import "fmt"

type LengthGuardrailPlugin struct {
    minLength int
    maxLength int
}

func (p *LengthGuardrailPlugin) Name() string { return "example-length-guardrail" }
func (p *LengthGuardrailPlugin) Type() string { return "guardrail" }

func (p *LengthGuardrailPlugin) Init(config map[string]any) error {
    p.minLength = 0
    p.maxLength = 10000

    if config == nil {
        return nil
    }
    if v, ok := config["min_length"]; ok {
        if f, ok := v.(float64); ok {  // JSON numbers decode as float64
            p.minLength = int(f)
        }
    }
    if v, ok := config["max_length"]; ok {
        if f, ok := v.(float64); ok {
            p.maxLength = int(f)
        }
    }
    return nil
}

func (p *LengthGuardrailPlugin) ProcessInput(ctx GuardrailContext) (GuardrailContext, error) {
    return p.checkLength(ctx, "input"), nil
}

func (p *LengthGuardrailPlugin) ProcessOutput(ctx GuardrailContext) (GuardrailContext, error) {
    return p.checkLength(ctx, "output"), nil
}

func (p *LengthGuardrailPlugin) checkLength(ctx GuardrailContext, dir string) GuardrailContext {
    n := len(ctx.Message)
    if n < p.minLength {
        ctx.Blocked = true
        ctx.BlockReason = fmt.Sprintf("%s message too short: %d < %d", dir, n, p.minLength)
    } else if p.maxLength > 0 && n > p.maxLength {
        ctx.Blocked = true
        ctx.BlockReason = fmt.Sprintf("%s message too long: %d > %d", dir, n, p.maxLength)
    }
    return ctx
}
```

See the full source at `internal/plugins/example_length_guardrail.go`.

---

## Writing a Middleware Plugin

The built-in `HeaderInjectorPlugin` demonstrates the middleware pattern:

```go
type HeaderInjectorPlugin struct {
    headers map[string]string
}

func (h *HeaderInjectorPlugin) Name() string { return "header-injector" }
func (h *HeaderInjectorPlugin) Type() string { return "middleware" }

func (h *HeaderInjectorPlugin) Init(config map[string]any) error {
    h.headers = map[string]string{}
    if raw, ok := config["headers"]; ok {
        if m, ok := raw.(map[string]any); ok {
            for k, v := range m {
                h.headers[k] = fmt.Sprintf("%v", v)
            }
        }
    }
    return nil
}

func (h *HeaderInjectorPlugin) Middleware() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            for k, v := range h.headers {
                if !strings.HasPrefix(k, "X-") && !strings.HasPrefix(k, "x-") {
                    continue // Only inject X- headers for safety
                }
                r.Header.Set(k, v)
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

See the full source at `internal/plugins/header_injector.go`.

---

## Configuration

### Step 1: Register the plugin

Add a `plugins` section to your `gateway.yaml`:

```yaml
plugins:
  - name: example-length-guardrail
    type: guardrail
    config:
      min_length: 10
      max_length: 5000
```

Each plugin entry has three fields:

| Field | Required | Description |
|:------|:---------|:------------|
| `name` | Yes | Must match the plugin's `Name()` return value |
| `type` | Yes | `"guardrail"`, `"middleware"`, or `"provider"` |
| `config` | No | Plugin-specific configuration (passed to `Init()`) |

### Step 2: Wire into the guardrail pipeline

For guardrail plugins, reference them in the `guardrails` section using stage type `plugin`:

```yaml
guardrails:
  input:
    - type: plugin
      config:
        name: example-length-guardrail
  output:
    - type: plugin
      config:
        name: example-length-guardrail
```

The `name` in the guardrail stage config must match the plugin's registered name.

### Multiple plugins

```yaml
plugins:
  - name: example-length-guardrail
    type: guardrail
    config:
      min_length: 10
      max_length: 5000

  - name: header-injector
    type: middleware
    config:
      headers:
        X-Custom-Header: "my-value"

guardrails:
  input:
    - type: keyword
      config:
        keywords: ["forbidden", "blocked"]
    - type: plugin
      config:
        name: example-length-guardrail
  output:
    - type: plugin
      config:
        name: example-length-guardrail
```

Plugin stages execute in order alongside built-in guardrail stages (PII, regex, keyword, length, webhook, json_schema).

---

## Best Practices

1. **Always handle nil config.** `Init(nil)` should work and use sensible defaults.
2. **Type-assert config values carefully.** JSON/YAML numbers decode as `float64`, not `int`. Use the `v.(float64)` pattern.
3. **Be thread-safe.** Plugins may be called concurrently. Use `sync.Mutex` if your plugin has mutable state.
4. **Name uniquely.** Plugin names must be unique across the registry. Use a prefix like `mycompany-` to avoid collisions.
5. **Return errors from Init.** If configuration is invalid, return an error — the gateway will fail to start, which is better than running with bad config.
6. **Keep ProcessInput/ProcessOutput fast.** These run on every request. Avoid network calls or heavy computation.
7. **Set BlockReason when blocking.** Users need to know why their request was blocked. Include the actual values in the reason string.

---

## Built-in Examples

| Plugin | File | Type | Description |
|:-------|:-----|:-----|:------------|
| HeaderInjector | `internal/plugins/header_injector.go` | Middleware | Injects custom X-* headers into requests |
| LengthGuardrail | `internal/plugins/example_length_guardrail.go` | Guardrail | Blocks messages outside configurable length bounds |

---

## See Also

- [Configuration](configuration.md) — Full config reference including plugins section
- [Governance](governance.md) — Guardrail pipeline and built-in guardrail types
- [API Reference](api-reference.md) — Error responses when guardrails block requests
