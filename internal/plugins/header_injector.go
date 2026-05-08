package plugins

import (
	"fmt"
	"net/http"
	"strings"
)

// HeaderInjectorPlugin is an example MiddlewarePlugin that injects
// custom headers into every request.
//
// Configuration:
//
//	plugins:
//	  - name: header-injector
//	    config:
//	      headers:
//	        X-Custom-Header: "my-value"
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
