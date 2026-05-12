package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"openlimit/internal/config"
)

// RunInteractive runs the interactive config generation wizard.
// It prompts the user for provider type, API key, and optional database URL,
// then generates and writes the config file.
func RunInteractive(outputPath string, force bool) error {
	reader := bufio.NewScanner(os.Stdin)

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║     OpenLimit Gateway Config Wizard      ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Provider type
	fmt.Println("Select a provider type:")
	fmt.Println("  1) openai")
	fmt.Println("  2) anthropic")
	fmt.Println("  3) gemini")
	fmt.Println("  4) openai-compatible")
	fmt.Print("\nProvider [1-4]: ")
	providerType, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("reading provider type: %w", err)
	}

	typeMap := map[string]string{
		"1": "openai",
		"2": "anthropic",
		"3": "gemini",
		"4": "openai-compatible",
	}

	selectedType, ok := typeMap[providerType]
	if !ok {
		// Allow typing the name directly
		if _, exists := providerTemplates[providerType]; exists {
			selectedType = providerType
		} else {
			return fmt.Errorf("invalid provider selection %q", providerType)
		}
	}

	// Step 2: Provider name
	defaultName := selectedType
	fmt.Printf("\nProvider name [%s]: ", defaultName)
	providerName, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("reading provider name: %w", err)
	}
	if providerName == "" {
		providerName = defaultName
	}

	// Step 3: Base URL (for openai-compatible)
	var baseURL string
	if selectedType == "openai-compatible" {
		fmt.Print("\nBase URL (e.g., http://localhost:11434/v1): ")
		baseURL, err = readLine(reader)
		if err != nil {
			return fmt.Errorf("reading base URL: %w", err)
		}
		if baseURL == "" {
			return fmt.Errorf("base URL is required for openai-compatible provider type")
		}
	}

	// Step 4: API key
	fmt.Print("\nAPI key: ")
	fmt.Println("(warning: input will be visible in terminal)")
	apiKey, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("reading API key: %w", err)
	}
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}

	// Step 5: Database URL (optional)
	fmt.Print("\nDatabase URL (optional, press Enter to skip): ")
	databaseURL, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("reading database URL: %w", err)
	}

	// Step 6: Generate config
	input := InitInput{
		ProviderType: selectedType,
		ProviderName: providerName,
		APIKey:       apiKey,
		DatabaseURL:  databaseURL,
		OutputPath:   outputPath,
		BaseURL:      baseURL,
	}

	cfg, err := GenerateConfig(input)
	if err != nil {
		return fmt.Errorf("generating config: %w", err)
	}

	// Step 7: Write config
	if err := WriteConfig(cfg, outputPath, force); err != nil {
		return err
	}

	// Step 8: Success
	PrintSuccessMessage(cfg, outputPath)
	return nil
}

// RunNonInteractive reads environment variables and generates config without prompts.
// Required env vars: PROVIDER_TYPE, PROVIDER_KEY
// Optional env vars: PROVIDER_NAME, DATABASE_URL, ADMIN_TOKEN, BASE_URL, MODEL
func RunNonInteractive(outputPath string, force bool) error {
	providerType := os.Getenv("PROVIDER_TYPE")
	if providerType == "" {
		return fmt.Errorf("PROVIDER_TYPE environment variable is required")
	}

	apiKey := os.Getenv("PROVIDER_KEY")
	if apiKey == "" {
		return fmt.Errorf("PROVIDER_KEY environment variable is required")
	}

	providerName := os.Getenv("PROVIDER_NAME")
	if providerName == "" {
		providerName = providerType
	}

	databaseURL := os.Getenv("DATABASE_URL")
	baseURL := os.Getenv("BASE_URL")
	model := os.Getenv("MODEL")

	input := InitInput{
		ProviderType: providerType,
		ProviderName: providerName,
		APIKey:       apiKey,
		DatabaseURL:  databaseURL,
		OutputPath:   outputPath,
		BaseURL:      baseURL,
		Model:        model,
	}

	cfg, err := GenerateConfig(input)
	if err != nil {
		return fmt.Errorf("generating config: %w", err)
	}

	// Override admin token if provided via env
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken != "" {
		cfg.Admin.BearerToken = adminToken
	}

	if err := WriteConfig(cfg, outputPath, force); err != nil {
		return err
	}

	PrintSuccessMessage(cfg, outputPath)
	return nil
}

// WriteConfig writes the config as YAML to the specified path.
// Returns an error if the file exists and force is false.
func WriteConfig(cfg config.Config, path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config file %s already exists; use --force to overwrite", path)
		}
	}

	yamlBytes, err := ConfigToYAML(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config to YAML: %w", err)
	}

	// Ensure parent directory exists
	dir := ""
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		dir = path[:idx]
	} else if idx := strings.LastIndex(path, "\\"); idx >= 0 {
		dir = path[:idx]
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(path, yamlBytes, 0644); err != nil {
		return fmt.Errorf("writing config to %s: %w", path, err)
	}

	return nil
}

// PrintSuccessMessage prints the final success output with a curl command.
func PrintSuccessMessage(cfg config.Config, outputPath string) {
	fmt.Println()
	fmt.Println("✅ Configuration generated successfully!")
	fmt.Printf("   Config file: %s\n", outputPath)
	fmt.Printf("   Provider:    %s\n", providerSummary(cfg))
	fmt.Printf("   Auth:        %s\n", boolStr(cfg.Auth.Enabled))
	fmt.Printf("   Admin:       %s\n", boolStr(cfg.Admin.Enabled))

	if cfg.Admin.Enabled && cfg.Admin.BearerToken != "" {
		fmt.Println()
		fmt.Println("   Quick test command:")
		fmt.Printf("   curl -H \"Authorization: Bearer %s\" http://%s/v1/models\n",
			MaskKey(cfg.Admin.BearerToken), cfg.Server.Address())
	}

	fmt.Println()
	fmt.Println("   To start the gateway:")
	fmt.Printf("   go run ./cmd/gateway\n")
}

func readLine(scanner *bufio.Scanner) (string, error) {
	if !scanner.Scan() {
		return "", fmt.Errorf("EOF")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func providerSummary(cfg config.Config) string {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func boolStr(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}
