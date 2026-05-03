package metrics

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TEST-7C-04-01 is covered in metrics_test.go (TestNewCollector_Enabled)
// where RecordGatewayError is called and gateway_errors_total is verified
// in the metrics output. This avoids duplicate Collector registration.

// TEST-7C-04-02: OpenAPI YAML is valid YAML (parse check).
func TestOpenAPIYAMLIsValid(t *testing.T) {
	yamlPath := "../../docs/openapi/admin-api.yaml"

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("failed to read OpenAPI YAML: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("OpenAPI YAML is not valid YAML: %v", err)
	}

	// Verify required OpenAPI fields
	if _, ok := parsed["openapi"]; !ok {
		t.Error("missing 'openapi' field")
	}
	if _, ok := parsed["info"]; !ok {
		t.Error("missing 'info' field")
	}
	if _, ok := parsed["paths"]; !ok {
		t.Error("missing 'paths' field")
	}

	// Verify OpenAPI version
	if v, _ := parsed["openapi"].(string); v == "" {
		t.Error("openapi field is empty")
	}
}

// TEST-7C-04-03: OpenAPI spec contains paths matching all admin mux.Handle patterns.
func TestOpenAPIEndpointCoverage(t *testing.T) {
	yamlPath := "../../docs/openapi/admin-api.yaml"

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("failed to read OpenAPI YAML: %v", err)
	}

	var parsed struct {
		Paths map[string]map[string]interface{} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	// All admin endpoints registered in handler.go RegisterRoutes.
	// RBAC endpoints (POST/GET/DELETE /admin/users, PUT /admin/users/{id}/role)
	// are conditionally registered but must still appear in the spec.
	expectedEndpoints := []struct {
		path   string
		method string
	}{
		{"/admin/projects", "post"},
		{"/admin/projects", "get"},
		{"/admin/projects/{id}", "delete"},
		{"/admin/keys", "post"},
		{"/admin/keys", "get"},
		{"/admin/keys/{id}", "delete"},
		{"/admin/usage", "get"},
		{"/admin/usage/summary", "get"},
		{"/admin/audit", "get"},
		{"/admin/quickstart", "post"},
		{"/admin/users", "post"},
		{"/admin/users", "get"},
		{"/admin/users/{id}", "delete"},
		{"/admin/users/{id}/role", "put"},
		{"/admin/tools", "get"},
		{"/admin/mcp/tools", "get"},
	}

	missing := 0
	for _, ep := range expectedEndpoints {
		pathItem, ok := parsed.Paths[ep.path]
		if !ok {
			t.Errorf("missing path %q in OpenAPI spec", ep.path)
			missing++
			continue
		}
		if _, ok := pathItem[ep.method]; !ok {
			t.Errorf("missing %s %q in OpenAPI spec", ep.method, ep.path)
			missing++
		}
	}

	if missing > 0 {
		t.Errorf("%d endpoint(s) missing from OpenAPI spec", missing)
	}

	// Verify all paths are admin paths
	pathCount := 0
	for p := range parsed.Paths {
		if !strings.HasPrefix(p, "/admin/") {
			t.Errorf("unexpected non-admin path in spec: %q", p)
		}
		pathCount++
	}

	// 13 unique paths in the spec (matching all admin mux.Handle registrations + quickstart)
	if pathCount != 13 {
		t.Errorf("expected 13 unique paths, got %d", pathCount)
	}
}
