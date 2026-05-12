package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: openlimit-mcp <command> [args]")
		fmt.Println("Commands: add, search")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "search":
		cmdSearch(os.Args[2:])
	case "add":
		cmdAdd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// cmdAdd is the CLI entry point for the add command.
func cmdAdd(args []string) {
	if err := runAdd(os.Stdout, os.Stderr, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// reorderForFlag moves flag arguments before positional arguments so that
// Go's flag package can parse them correctly (it stops at the first non-flag arg).
func reorderForFlag(args []string) []string {
	boolFlags := map[string]bool{
		"--ping": true, "--dry-run": true, "--live": true,
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !boolFlags[args[i]] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}

// runAdd contains the core logic for the add command, extracted for testability.
func runAdd(stdout, stderr io.Writer, args []string) error {
	args = reorderForFlag(args)

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	url := fs.String("url", "", "MCP server URL (required)")
	configPath := fs.String("config", "config.yaml", "config file path")
	prefix := fs.String("prefix", "", "tool prefix (default: server name)")
	timeout := fs.Int("timeout", 5, "connection timeout in seconds")
	ping := fs.Bool("ping", false, "ping server before listing tools")
	dryRun := fs.Bool("dry-run", false, "discover only, don't write config")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("Usage: openlimit-mcp add <name> --url <url>")
	}
	name := fs.Arg(0)

	// Validate name (AR-01)
	if !isValidName(name) {
		return fmt.Errorf("invalid name %q: must be alphanumeric with hyphens", name)
	}

	// Validate URL (AR-02)
	if *url == "" {
		return fmt.Errorf("--url is required")
	}
	if !strings.HasPrefix(*url, "http://") && !strings.HasPrefix(*url, "https://") {
		return fmt.Errorf("invalid URL %q: must start with http:// or https://", *url)
	}

	toolPrefix := *prefix
	if toolPrefix == "" {
		toolPrefix = name
	}

	// Check for duplicate (AR-03)
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}
	for _, s := range cfg.MCP.Servers {
		if s.Name == name {
			return fmt.Errorf("server %q already exists in config", name)
		}
	}

	// Connect and discover
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fmt.Fprintf(stdout, "Connecting to %s at %s...\n", name, *url)

	client := mcp.NewClient(name, *url, nil, time.Duration(*timeout)*time.Second, toolPrefix, logger)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	fmt.Fprintln(stdout, "Connected")

	if *ping {
		if err := client.Ping(ctx); err != nil {
			return fmt.Errorf("ping failed: %w", err)
		}
		fmt.Fprintln(stdout, "Ping OK")
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	fmt.Fprintf(stdout, "Discovered %d tools:\n", len(tools))
	for _, t := range tools {
		fmt.Fprintf(stdout, "  - %s\n", t.Name)
	}

	if *dryRun {
		fmt.Fprintln(stdout, "\n(dry run — config not written)")
		return nil
	}

	// Append to config
	newServer := config.MCPServerConfig{
		Name:       name,
		URL:        *url,
		TimeoutMS:  *timeout * 1000,
		ToolPrefix: toolPrefix,
	}

	if err := appendServerToConfig(*configPath, cfg, newServer); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Fprintf(stdout, "\nServer %q added to %s\n", name, *configPath)
	return nil
}

func isValidName(name string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`, name)
	return matched
}
