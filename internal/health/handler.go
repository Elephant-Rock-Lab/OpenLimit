package health

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/lifecycle"
	"openlimit/internal/providers"
	"openlimit/pkg/version"
)

type Response struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
}

type ReadyResponse struct {
	Status       string                    `json:"status"`
	Version      string                    `json:"version"`
	Timestamp    string                    `json:"timestamp"`
	ShuttingDown bool                      `json:"shutting_down"`
	InFlight     int64                     `json:"in_flight"`
	Providers    []ProviderReadinessStatus `json:"providers"`
	KMS          *KMSStatus                `json:"kms,omitempty"`
	OIDC         *OIDCStatus               `json:"oidc,omitempty"`
}

type KMSStatus struct {
	Type  string `json:"type"`
	Ready bool   `json:"ready"`
}

type OIDCStatus struct {
	Enabled bool   `json:"enabled"`
	Issuer  string `json:"issuer,omitempty"`
	Ready   bool   `json:"ready"`
}

type ProviderReadinessStatus struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Ready          bool     `json:"ready"`
	RequiresAuth   bool     `json:"requires_auth"`
	ConfiguredKeys int      `json:"configured_keys"`
	ActiveKeys     int      `json:"active_keys"`
	MissingEnv     []string `json:"missing_env,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(Response{
		Status:    "ok",
		Version:   version.Version,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// AdminProviderHealth returns the health status of all tracked providers.
// Returns JSON array of provider health entries.
func AdminProviderHealth(tracker *Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		entries := tracker.GetAll()

		// Group by provider
		type providerHealth struct {
			Name                string `json:"name"`
			Healthy             bool   `json:"healthy"`
			LastSuccess         string `json:"last_success,omitempty"`
			LastFailure         string `json:"last_failure,omitempty"`
			ConsecutiveFailures int    `json:"consecutive_failures"`
		}

		seen := map[string]*providerHealth{}
		for _, e := range entries {
			p, ok := seen[e.Provider]
			if !ok {
				p = &providerHealth{Name: e.Provider}
				seen[e.Provider] = p
			}
			if e.ConsecutiveFailures > p.ConsecutiveFailures {
				p.ConsecutiveFailures = e.ConsecutiveFailures
			}
			if !e.LastSuccess.IsZero() {
				p.LastSuccess = e.LastSuccess.UTC().Format(time.RFC3339)
			}
			if !e.LastFailure.IsZero() {
				p.LastFailure = e.LastFailure.UTC().Format(time.RFC3339)
			}
		}

		result := make([]providerHealth, 0, len(seen))
		for _, p := range seen {
			p.Healthy = p.ConsecutiveFailures == 0
			result = append(result, *p)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

		if result == nil {
			result = []providerHealth{}
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}

// AdminModelHealth returns per-model health data.
func AdminModelHealth(tracker *Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		entries := tracker.GetAll()

		type modelHealth struct {
			Provider            string `json:"provider"`
			Model               string `json:"model"`
			Region              string `json:"region"`
			Healthy             bool   `json:"healthy"`
			LastSuccess         string `json:"last_success,omitempty"`
			LastFailure         string `json:"last_failure,omitempty"`
			ConsecutiveFailures int    `json:"consecutive_failures"`
		}

		result := make([]modelHealth, 0, len(entries))
		for _, e := range entries {
			h := modelHealth{
				Provider:            e.Provider,
				Model:               e.Model,
				Region:              e.Region,
				Healthy:             e.ConsecutiveFailures == 0,
				ConsecutiveFailures: e.ConsecutiveFailures,
			}
			if !e.LastSuccess.IsZero() {
				h.LastSuccess = e.LastSuccess.UTC().Format(time.RFC3339)
			}
			if !e.LastFailure.IsZero() {
				h.LastFailure = e.LastFailure.UTC().Format(time.RFC3339)
			}
			result = append(result, h)
		}

		if result == nil {
			result = []modelHealth{}
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}

// OIDCReadiness checks if OIDC is ready.
type OIDCReadiness interface {
	Healthy() bool
	Issuer() string
}

func ReadyHandler(cfg config.Config, keys map[string]*providers.KeyRing, tracker *lifecycle.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ReadyHandlerWithOIDC(cfg, keys, tracker, nil)(w, r)
	}
}

func ReadyHandlerWithOIDC(cfg config.Config, keys map[string]*providers.KeyRing, tracker *lifecycle.Tracker, oidcProvider OIDCReadiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statuses := providerStatuses(cfg, keys)
		shuttingDown := tracker != nil && tracker.IsShuttingDown()
		status := "ready"
		code := http.StatusOK
		if shuttingDown {
			status = "shutting_down"
			code = http.StatusServiceUnavailable
		}
		for _, provider := range statuses {
			if !provider.Ready {
				status = "not_ready"
				code = http.StatusServiceUnavailable
				break
			}
		}

		inFlight := int64(0)
		if tracker != nil {
			inFlight = tracker.InFlight()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(ReadyResponse{
			Status:       status,
			Version:      version.Version,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			ShuttingDown: shuttingDown,
			InFlight:     inFlight,
			Providers:    statuses,
			KMS:          kmsStatus(cfg),
			OIDC:         oidcStatus(cfg, oidcProvider),
		})
	}
}

func kmsStatus(cfg config.Config) *KMSStatus {
	if !cfg.KMS.Enabled {
		return nil
	}
	return &KMSStatus{
		Type:  cfg.KMS.Type,
		Ready: true, // If we got this far, KMS initialized successfully
	}
}

func oidcStatus(cfg config.Config, provider OIDCReadiness) *OIDCStatus {
	if !cfg.Admin.OIDC.Enabled {
		return nil
	}
	status := &OIDCStatus{
		Enabled: true,
		Issuer:  cfg.Admin.OIDC.Issuer,
	}
	if provider != nil {
		status.Ready = provider.Healthy()
	}
	return status
}

func providerStatuses(cfg config.Config, keys map[string]*providers.KeyRing) []ProviderReadinessStatus {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	statuses := make([]ProviderReadinessStatus, 0, len(names))
	for _, name := range names {
		provider := cfg.Providers[name]
		keyRing := keys[name]
		missing := missingEnvNames(keyRing)
		requiresAuth := RequiresAuth(provider)
		activeKeys := keyRing.ActiveCount()
		ready := true
		reason := ""
		if requiresAuth && activeKeys == 0 {
			ready = false
			reason = "provider requires auth but has no active keys"
		}

		statuses = append(statuses, ProviderReadinessStatus{
			Name:           name,
			Type:           provider.Type,
			Ready:          ready,
			RequiresAuth:   requiresAuth,
			ConfiguredKeys: keyRing.ConfiguredCount(),
			ActiveKeys:     activeKeys,
			MissingEnv:     missing,
			Reason:         reason,
		})
	}
	return statuses
}

func missingEnvNames(keyRing *providers.KeyRing) []string {
	if keyRing == nil {
		return nil
	}
	missing := keyRing.MissingEnv()
	out := make([]string, 0, len(missing))
	for _, item := range missing {
		out = append(out, item.Env)
	}
	sort.Strings(out)
	return out
}

func RequiresAuth(provider config.ProviderConfig) bool {
	switch provider.Type {
	case "openai", "anthropic", "gemini", "azure-openai", "":
		return true
	default:
		return false
	}
}
