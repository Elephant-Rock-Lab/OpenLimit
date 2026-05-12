package main

import (
	"fmt"
	"os"
	"path/filepath"

	"openlimit/internal/config"

	"gopkg.in/yaml.v3"
)

// appendServerToConfig appends an MCP server entry to the config file.
// It uses atomic write (temp file + rename) to prevent corruption.
func appendServerToConfig(path string, cfg config.Config, server config.MCPServerConfig) error {
	cfg.MCP.Servers = append(cfg.MCP.Servers, server)

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Atomic write: write to temp file, then rename
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "openlimit-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}
