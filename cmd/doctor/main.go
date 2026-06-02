package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"openlimit/internal/config"
	"openlimit/internal/providers"
)

// CheckResult holds the result of a single diagnostic check.
type CheckResult struct {
	Name    string
	Status  string // "PASS", "FAIL", "WARN"
	Message string
	Suggest string // actionable suggestion for FAIL/WARN
}

func main() {
	configPath := flag.String("config", "", "path to config file (default: auto-detect)")
	flag.Parse()

	results, err := runDoctor(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ doctor failed: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Println("OpenLimit Config Doctor")
	fmt.Println("═══════════════════════")
	fmt.Println()

	passCount := 0
	failCount := 0
	warnCount := 0
	for _, r := range results {
		switch r.Status {
		case "PASS":
			fmt.Printf("  ✓ %s\n", r.Name)
			passCount++
		case "FAIL":
			fmt.Printf("  ✗ %s\n", r.Name)
			fmt.Printf("    %s\n", r.Message)
			if r.Suggest != "" {
				fmt.Printf("    → %s\n", r.Suggest)
			}
			failCount++
		case "WARN":
			fmt.Printf("  ⚠ %s\n", r.Name)
			fmt.Printf("    %s\n", r.Message)
			if r.Suggest != "" {
				fmt.Printf("    → %s\n", r.Suggest)
			}
			warnCount++
		}
	}

	fmt.Println()
	fmt.Printf("Results: %d passed, %d warnings, %d failures\n", passCount, warnCount, failCount)

	if failCount > 0 {
		os.Exit(1)
	}
}

func runDoctor(configPath string) ([]CheckResult, error) {
	var results []CheckResult

	// 1. Load config
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("cannot load config: %w", err)
	}

	// 2. Check providers exist
	results = append(results, checkProvidersExist(cfg)...)

	// 3. Check provider resolution
	results = append(results, checkProviderResolution(cfg)...)

	// 4. Check model routes reference valid providers
	results = append(results, checkModelRoutes(cfg)...)

	// 5. Check provider keys are configured
	results = append(results, checkProviderKeys(cfg)...)

	return results, nil
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		// Try common paths
		for _, p := range []string{"config.yaml", "config.yml", "openlimit.yaml", "openlimit.yml"} {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}

	if path == "" {
		// No config file found — use defaults
		cfg := config.Default()
		return &cfg, nil
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func checkProvidersExist(cfg *config.Config) []CheckResult {
	var results []CheckResult

	if len(cfg.Providers) == 0 {
		results = append(results, CheckResult{
			Name:    "Providers configured",
			Status:  "WARN",
			Message: "No providers configured in config",
			Suggest: "Add at least one provider to your config file. Try: openlimit init",
		})
	} else {
		results = append(results, CheckResult{
			Name:   fmt.Sprintf("Providers configured (%d)", len(cfg.Providers)),
			Status: "PASS",
		})
	}

	return results
}

func checkProviderResolution(cfg *config.Config) []CheckResult {
	var results []CheckResult

	for name, pc := range cfg.Providers {
		// Try registry resolution
		resolved := providers.ApplyDefaults(name, map[string]interface{}{
			"type":     pc.Type,
			"base_url": pc.BaseURL,
		})

		typ, _ := resolved["type"].(string)
		baseURL, _ := resolved["base_url"].(string)

		if typ == "" && pc.Type == "" {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("Provider %q type resolution", name),
				Status:  "FAIL",
				Message: "No type specified and not in provider registry",
				Suggest: fmt.Sprintf("Set 'type' to a known adapter (openai, anthropic, etc.) or use a registry name (deepseek, together_ai, etc.) for provider %q", name),
			})
		} else if baseURL == "" && pc.BaseURL == "" {
			// Type resolved but no base_url
			def, inRegistry := providers.LookupDefault(name)
			if !inRegistry {
				results = append(results, CheckResult{
					Name:    fmt.Sprintf("Provider %q base_url", name),
					Status:  "FAIL",
					Message: "No base_url configured and provider not in registry",
					Suggest: fmt.Sprintf("Add 'base_url' for provider %q, or use a registry name (e.g., deepseek, groq, together_ai)", name),
				})
			} else {
				results = append(results, CheckResult{
					Name:    fmt.Sprintf("Provider %q base_url from registry", name),
					Status:  "PASS",
					Message: fmt.Sprintf("Using %s", def.BaseURL),
				})
			}
		} else {
			results = append(results, CheckResult{
				Name:   fmt.Sprintf("Provider %q resolves", name),
				Status: "PASS",
			})
		}
	}

	return results
}

func checkModelRoutes(cfg *config.Config) []CheckResult {
	var results []CheckResult

	providerNames := map[string]bool{}
	for name := range cfg.Providers {
		providerNames[name] = true
	}

	danglingRoutes := 0
	for modelName, mc := range cfg.Models {
		for _, route := range mc.Routes {
			if !providerNames[route.Provider] {
				danglingRoutes++
				results = append(results, CheckResult{
					Name:    fmt.Sprintf("Model %q route to %q", modelName, route.Provider),
					Status:  "FAIL",
					Message: fmt.Sprintf("Route references provider %q which is not configured", route.Provider),
					Suggest: fmt.Sprintf("Add provider %q to your config or remove this route", route.Provider),
				})
			}
		}
	}

	if danglingRoutes == 0 && len(cfg.Models) > 0 {
		results = append(results, CheckResult{
			Name:   fmt.Sprintf("Model routes valid (%d models)", len(cfg.Models)),
			Status: "PASS",
		})
	}

	return results
}

func checkProviderKeys(cfg *config.Config) []CheckResult {
	var results []CheckResult

	noKeyProviders := 0
	for name, pc := range cfg.Providers {
		if len(pc.Keys) == 0 {
			noKeyProviders++
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("Provider %q has API key", name),
				Status:  "WARN",
				Message: "No keys configured for this provider",
				Suggest: fmt.Sprintf("Add a key entry for provider %q in your config", name),
			})
		} else {
			// Check if env-referenced keys exist
			missingEnvs := []string{}
			for _, k := range pc.Keys {
				if k.Env != "" && os.Getenv(k.Env) == "" {
					missingEnvs = append(missingEnvs, k.Env)
				}
			}
			if len(missingEnvs) > 0 {
				results = append(results, CheckResult{
					Name:    fmt.Sprintf("Provider %q key env vars", name),
					Status:  "FAIL",
					Message: fmt.Sprintf("Missing env vars: %s", strings.Join(missingEnvs, ", ")),
					Suggest: fmt.Sprintf("Set the environment variables: export %s=your-api-key", strings.Join(missingEnvs, "=... && export ")),
				})
			} else {
				results = append(results, CheckResult{
					Name:   fmt.Sprintf("Provider %q keys", name),
					Status: "PASS",
				})
			}
		}
	}

	if noKeyProviders == 0 && len(cfg.Providers) > 0 {
		results = append(results, CheckResult{
			Name:   "All providers have keys",
			Status: "PASS",
		})
	}

	return results
}
