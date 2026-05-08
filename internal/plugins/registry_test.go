package plugins

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockGuardrail is a test GuardrailPlugin that uppercases text.
type mockGuardrail struct{}

func (m *mockGuardrail) Name() string                { return "mock-guardrail" }
func (m *mockGuardrail) Type() string                { return "guardrail" }
func (m *mockGuardrail) Init(config map[string]any) error { return nil }
func (m *mockGuardrail) ProcessInput(ctx GuardrailContext) (GuardrailContext, error) {
	ctx.Message = "IN:" + ctx.Message
	return ctx, nil
}
func (m *mockGuardrail) ProcessOutput(ctx GuardrailContext) (GuardrailContext, error) {
	ctx.Message = "OUT:" + ctx.Message
	return ctx, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	Reset()
	g := &mockGuardrail{}
	Register(g)

	found := Lookup("mock-guardrail")
	if found == nil {
		t.Fatal("expected to find registered plugin")
	}
	if found.Name() != "mock-guardrail" {
		t.Errorf("name = %q, want %q", found.Name(), "mock-guardrail")
	}
}

func TestRegistry_LookupGuardrail(t *testing.T) {
	Reset()
	Register(&mockGuardrail{})

	gp, ok := LookupGuardrail("mock-guardrail")
	if !ok {
		t.Fatal("expected to find guardrail plugin")
	}

	ctx := NewGuardrailContext("hello")
	result, err := gp.ProcessInput(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Message != "IN:hello" {
		t.Errorf("message = %q, want %q", result.Message, "IN:hello")
	}
}

func TestRegistry_LookupNotFound(t *testing.T) {
	Reset()
	if p := Lookup("nonexistent"); p != nil {
		t.Error("expected nil for nonexistent plugin")
	}
}

func TestRegistry_List(t *testing.T) {
	Reset()
	Register(&mockGuardrail{})

	list := List()
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
}

func TestRegistry_ListByType(t *testing.T) {
	Reset()
	Register(&mockGuardrail{})
	Register(&HeaderInjectorPlugin{})
	_ = Lookup("header-injector").Init(map[string]any{})

	guardrails := ListByType("guardrail")
	if len(guardrails) != 1 {
		t.Fatalf("expected 1 guardrail plugin, got %d", len(guardrails))
	}

	middlewares := ListByType("middleware")
	if len(middlewares) != 1 {
		t.Fatalf("expected 1 middleware plugin, got %d", len(middlewares))
	}
}

func TestHeaderInjector_Middleware(t *testing.T) {
	Reset()
	h := &HeaderInjectorPlugin{}
	if err := h.Init(map[string]any{
		"headers": map[string]any{
			"X-Request-Source": "openlimit",
			"Host":             "evil.com", // should be ignored (not X-)
		},
	}); err != nil {
		t.Fatal(err)
	}
	Register(h)

	called := false
	handler := h.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if v := r.Header.Get("X-Request-Source"); v != "openlimit" {
			t.Errorf("X-Request-Source = %q, want %q", v, "openlimit")
		}
		if v := r.Header.Get("Host"); v == "evil.com" {
			t.Error("Host header should not have been injected")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("handler was not called")
	}
}

func TestGuardrailContext_New(t *testing.T) {
	ctx := NewGuardrailContext("test")
	if ctx.Message != "test" {
		t.Errorf("message = %q, want %q", ctx.Message, "test")
	}
	if ctx.Metadata == nil {
		t.Error("metadata should not be nil")
	}
	if ctx.Blocked {
		t.Error("should not be blocked by default")
	}
}
