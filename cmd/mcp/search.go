package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/mcp"
)

// SearchResult is a catalog entry with a relevance score.
type SearchResult struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Server      string  `json:"server"`
	Category    string  `json:"category"`
	Score       float64 `json:"score"`
}

// SearchResponse is the JSON output envelope for search results.
type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
}

// tokenize splits s into lowercase tokens using the given delimiter.
// For whitespace splitting (query, description) callers pass " " and
// the function uses strings.Fields to handle any whitespace.
// For tool names callers pass "_" to split on underscores.
func tokenize(s string, splitOn string) []string {
	var parts []string
	if splitOn == " " {
		parts = strings.Fields(s)
	} else {
		parts = strings.Split(s, splitOn)
	}
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// scoreEntry computes a relevance score for a catalog entry against a query.
// AR-01/AR-05: name matches weight 2.0, description matches weight 1.0.
// Tokenization: query on whitespace, name on underscores, description on whitespace.
// All tokens lowercased; a match is a substring relation.
func scoreEntry(queryTerms []string, entry CatalogEntry) float64 {
	nameTokens := tokenize(entry.Name, "_")
	descTokens := tokenize(entry.Description, " ")

	var score float64
	for _, term := range queryTerms {
		// Check name tokens (weight 2.0)
		for _, nt := range nameTokens {
			if strings.Contains(nt, term) {
				score += 2.0
				break
			}
		}
		// Check description tokens (weight 1.0)
		for _, dt := range descTokens {
			if strings.Contains(dt, term) {
				score += 1.0
				break
			}
		}
	}
	return score
}

// searchCatalog searches the catalog for entries matching the query.
// AR-02: results sorted by score descending.
// AR-06: category filter (case-insensitive exact match).
func searchCatalog(query string, catalog []CatalogEntry, category string, limit int) []SearchResult {
	queryTerms := tokenize(query, " ")

	var results []SearchResult
	for _, entry := range catalog {
		// AR-06: filter by category if specified
		if category != "" && !strings.EqualFold(entry.Category, category) {
			continue
		}

		score := scoreEntry(queryTerms, entry)
		if score == 0 {
			continue
		}

		results = append(results, SearchResult{
			Name:        entry.Name,
			Description: entry.Description,
			Server:      entry.Server,
			Category:    entry.Category,
			Score:       score,
		})
	}

	// AR-02: sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Cap at limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// cmdSearch is the CLI entry point for the search command.
func cmdSearch(args []string) {
	if err := runSearch(os.Stdout, os.Stderr, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runSearch contains the core logic for the search command, extracted for testability.
func runSearch(stdout, stderr io.Writer, args []string) error {
	args = reorderForFlag(args)

	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text or json")
	limit := fs.Int("limit", 10, "maximum number of results")
	category := fs.String("category", "", "filter by category")
	live := fs.Bool("live", false, "include tools from configured MCP servers")
	configPath := fs.String("config", "config.yaml", "config file path (used with --live)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("Usage: openlimit-mcp search <query> [--format text|json] [--limit N] [--category cat] [--live]")
	}

	query := fs.Arg(0)

	// Build catalog — default is fully embedded (HB-03, HB-04)
	catalog := make([]CatalogEntry, len(defaultCatalog))
	copy(catalog, defaultCatalog)

	// --live: merge tools from configured MCP servers
	if *live {
		liveEntries, err := fetchLiveTools(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "Warning: live fetch failed: %v\n", err)
		} else {
			catalog = mergeCatalogs(catalog, liveEntries)
		}
	}

	results := searchCatalog(query, catalog, *category, *limit)

	// AR-03: exit 1 on no results
	if len(results) == 0 {
		return fmt.Errorf("no results found for %q", query)
	}

	// AR-04: output format
	switch *format {
	case "json":
		resp := SearchResponse{
			Query:   query,
			Results: results,
			Total:   len(results),
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	default: // text
		fmt.Fprintf(stdout, "Search results for %q (%d found):\n", query, len(results))
		for _, r := range results {
			fmt.Fprintf(stdout, "  %-25s [%s] %s (score: %.1f)\n", r.Name, r.Server, r.Description, r.Score)
		}
	}

	return nil
}

// fetchLiveTools connects to all configured MCP servers and fetches their tools.
func fetchLiveTools(configPath string) ([]CatalogEntry, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if len(cfg.MCP.Servers) == 0 {
		return nil, nil
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var entries []CatalogEntry

	for _, srv := range cfg.MCP.Servers {
		timeout := time.Duration(srv.TimeoutMS) * time.Millisecond
		client := mcp.NewClient(srv.Name, srv.URL, nil, timeout, srv.ToolPrefix, logger)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		if err := client.Initialize(ctx); err != nil {
			cancel()
			continue
		}

		tools, err := client.ListTools(ctx)
		cancel()
		client.Close()

		if err != nil {
			continue
		}

		for _, t := range tools {
			cat := matchCategory(t.RawName)
			entries = append(entries, CatalogEntry{
				Name:        t.RawName,
				Description: t.Description,
				Server:      t.ServerName,
				Category:    cat,
			})
		}
	}

	return entries, nil
}

// matchCategory tries to match a tool name to a catalog category.
// Returns the category from the first matching catalog entry, or "live".
func matchCategory(name string) string {
	for _, entry := range defaultCatalog {
		if entry.Name == name {
			return entry.Category
		}
	}
	return "live"
}

// mergeCatalogs merges live entries into the catalog, deduplicating by Name.
// Catalog entries take precedence over live entries for the same name.
func mergeCatalogs(catalog, live []CatalogEntry) []CatalogEntry {
	seen := make(map[string]bool, len(catalog))
	for _, e := range catalog {
		seen[e.Name] = true
	}

	result := make([]CatalogEntry, len(catalog))
	copy(result, catalog)

	for _, e := range live {
		if !seen[e.Name] {
			seen[e.Name] = true
			result = append(result, e)
		}
	}

	return result
}
