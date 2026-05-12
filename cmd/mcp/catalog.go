package main

// CatalogEntry represents a tool in the embedded catalog.
type CatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
	Category    string `json:"category"`
}

// defaultCatalog contains popular MCP tools organized by server and category.
// Covers filesystem, github, postgres, brave-search, puppeteer, slack,
// google-maps, memory, sequential-thinking, everything, fetch, and sqlite.
var defaultCatalog = []CatalogEntry{
	// ── filesystem (Category: files) ─────────────────────────────
	{Name: "read_file", Description: "Read contents of a file from the filesystem", Server: "filesystem", Category: "files"},
	{Name: "write_file", Description: "Write contents to a file on the filesystem", Server: "filesystem", Category: "files"},
	{Name: "list_directory", Description: "List files and directories in a path", Server: "filesystem", Category: "files"},
	{Name: "search_files", Description: "Search for files matching a pattern", Server: "filesystem", Category: "files"},
	{Name: "create_directory", Description: "Create a new directory on the filesystem", Server: "filesystem", Category: "files"},

	// ── github (Category: code) ──────────────────────────────────
	{Name: "create_issue", Description: "Create a new issue in a GitHub repository", Server: "github", Category: "code"},
	{Name: "list_repos", Description: "List repositories for a user or organization", Server: "github", Category: "code"},
	{Name: "search_code", Description: "Search code across GitHub repositories", Server: "github", Category: "code"},
	{Name: "create_pr", Description: "Create a pull request in a repository", Server: "github", Category: "code"},
	{Name: "list_pull_requests", Description: "List pull requests in a GitHub repository", Server: "github", Category: "code"},

	// ── postgres (Category: database) ────────────────────────────
	{Name: "query", Description: "Execute a SQL query on a PostgreSQL database", Server: "postgres", Category: "database"},
	{Name: "list_tables", Description: "List all tables in a PostgreSQL database", Server: "postgres", Category: "database"},
	{Name: "describe_table", Description: "Describe the schema of a database table", Server: "postgres", Category: "database"},
	{Name: "insert", Description: "Insert rows into a PostgreSQL table", Server: "postgres", Category: "database"},
	{Name: "update", Description: "Update rows in a PostgreSQL table", Server: "postgres", Category: "database"},

	// ── brave-search (Category: search) ──────────────────────────
	{Name: "web_search", Description: "Search the web using Brave Search", Server: "brave-search", Category: "search"},
	{Name: "get_page_content", Description: "Get the full content of a web page", Server: "brave-search", Category: "search"},

	// ── puppeteer (Category: browser) ────────────────────────────
	{Name: "navigate", Description: "Navigate a headless browser to a URL", Server: "puppeteer", Category: "browser"},
	{Name: "screenshot", Description: "Take a screenshot of the current browser page", Server: "puppeteer", Category: "browser"},
	{Name: "click", Description: "Click an element on the browser page", Server: "puppeteer", Category: "browser"},
	{Name: "fill", Description: "Fill in a form field on the browser page", Server: "puppeteer", Category: "browser"},
	{Name: "evaluate", Description: "Evaluate JavaScript in the browser context", Server: "puppeteer", Category: "browser"},

	// ── slack (Category: communication) ──────────────────────────
	{Name: "send_message", Description: "Send a message to a Slack channel", Server: "slack", Category: "communication"},
	{Name: "list_channels", Description: "List all channels in a Slack workspace", Server: "slack", Category: "communication"},
	{Name: "search_messages", Description: "Search messages in Slack", Server: "slack", Category: "communication"},
	{Name: "get_thread", Description: "Get a message thread from Slack", Server: "slack", Category: "communication"},

	// ── google-maps (Category: maps) ─────────────────────────────
	{Name: "geocode", Description: "Convert an address to geographic coordinates", Server: "google-maps", Category: "maps"},
	{Name: "directions", Description: "Get directions between two locations", Server: "google-maps", Category: "maps"},
	{Name: "places_search", Description: "Search for places near a location", Server: "google-maps", Category: "maps"},

	// ── memory (Category: memory) ────────────────────────────────
	{Name: "create_entity", Description: "Create a new entity in the knowledge graph", Server: "memory", Category: "memory"},
	{Name: "search_nodes", Description: "Search nodes in the knowledge graph", Server: "memory", Category: "memory"},
	{Name: "open_nodes", Description: "Open and read nodes from the knowledge graph", Server: "memory", Category: "memory"},
	{Name: "add_observations", Description: "Add observations to an entity in the knowledge graph", Server: "memory", Category: "memory"},

	// ── sequential-thinking (Category: thinking) ─────────────────
	{Name: "sequentialthinking", Description: "Run a sequential thinking process for problem solving", Server: "sequential-thinking", Category: "thinking"},

	// ── everything (Category: utility) ───────────────────────────
	{Name: "echo", Description: "Echo back the input message", Server: "everything", Category: "utility"},
	{Name: "sampleLLM", Description: "Sample responses from a language model", Server: "everything", Category: "utility"},
	{Name: "longRunningOperation", Description: "Perform a long-running operation and return status", Server: "everything", Category: "utility"},

	// ── fetch (Category: search) ─────────────────────────────────
	{Name: "fetch_webpage", Description: "Fetch the content of a web page by URL", Server: "fetch", Category: "search"},

	// ── sqlite (Category: database) ──────────────────────────────
	{Name: "read_query", Description: "Execute a read-only SQL query on SQLite", Server: "sqlite", Category: "database"},
	{Name: "write_query", Description: "Execute a write SQL query on SQLite", Server: "sqlite", Category: "database"},
	{Name: "create_table", Description: "Create a new table in SQLite database", Server: "sqlite", Category: "database"},
	{Name: "list_tables_sqlite", Description: "List all tables in an SQLite database", Server: "sqlite", Category: "database"},
}
