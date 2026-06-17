package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiffang/engram9/internal/storage"
)

// MCPTools defines the tools exposed to MCP clients.
// These are the consumption surface for OKF bundles:
//   - search_concepts: keyword search across wiki pages
//   - read_concept: read a specific wiki page with metadata
//   - neighbors: list related pages from the wiki index
//   - write_memory: store new information into the memory system
//   - memory_status: get system statistics
var MCPTools = []mcpTool{
	{
		Name:        "search_concepts",
		Description: "Search across all knowledge pages by keyword. Returns matching lines with file paths. Use this to find relevant concepts before reading full pages.",
		InputSchema: mustJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query (case-insensitive text match across all wiki pages)",
				},
			},
			"required": []string{"query"},
		}),
	},
	{
		Name:        "read_concept",
		Description: "Read a specific knowledge page by path. Returns the full Markdown content and metadata (memory type, trust tier, source events, access history). Paths are relative to wiki/ (e.g., 'semantic/projects/db9.md').",
		InputSchema: mustJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path within the wiki (e.g., 'semantic/projects/db9.md')",
				},
			},
			"required": []string{"path"},
		}),
	},
	{
		Name:        "neighbors",
		Description: "List all knowledge pages in the wiki index, organized by memory type (semantic, episodic, procedural, prospective). Use this to discover available knowledge and find related concepts.",
		InputSchema: mustJSON(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
	},
	{
		Name:        "write_memory",
		Description: "Store new information into the memory system. The information is appended as a raw event. Call dream to consolidate pending events into wiki pages when the dream tool is available.",
		InputSchema: mustJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The information to remember",
				},
				"actor": map[string]any{
					"type":        "string",
					"description": "Who is providing this information (e.g., 'claude', 'codex', 'user')",
				},
				"source": map[string]any{
					"type":        "string",
					"description": "Where this came from (e.g., 'conversation', 'code_review', 'observation')",
				},
			},
			"required": []string{"text"},
		}),
	},
	{
		Name:        "memory_status",
		Description: "Get system statistics: total events, uncompiled events, active wiki pages, archived pages.",
		InputSchema: mustJSON(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
	},
}

var dreamTool = mcpTool{
	Name:        "dream",
	Description: "Run a memory dreaming cycle now: consolidate pending raw events into wiki pages, prune/archive where appropriate, and rebuild the wiki index. Available only in runtime data mode when -dream is enabled.",
	InputSchema: mustJSON(map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}),
}

func (s *Server) tools() []mcpTool {
	tools := make([]mcpTool, 0, len(MCPTools)+1)
	tools = append(tools, MCPTools...)
	if s.dreamer != nil {
		tools = append(tools, dreamTool)
	}
	return tools
}

// executeTool dispatches an MCP tool call to the storage layer.
func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (string, error) {
	switch name {
	case "search_concepts":
		return s.execSearchConcepts(args)
	case "read_concept":
		return s.execReadConcept(args)
	case "neighbors":
		return s.execNeighbors()
	case "write_memory":
		return s.execWriteMemory(args)
	case "memory_status":
		return s.execMemoryStatus()
	case "dream":
		return s.execDream(ctx)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) execSearchConcepts(args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}

	results, err := s.store.SearchWiki(query)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}
	if len(results) == 0 {
		return "No results found.", nil
	}

	// Format as readable text with paths and matching lines.
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d match(es):\n\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&sb, "- %s (line %d): %s\n", r.Path, r.Line, r.Content)
	}
	return sb.String(), nil
}

// validatePath rejects obviously malicious paths at the MCP layer before
// they reach the storage layer. Defense in depth — the store also validates.
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be relative, not absolute")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	if strings.HasPrefix(path, ".meta/") {
		return fmt.Errorf("cannot access metadata directly")
	}
	return nil
}

func (s *Server) execReadConcept(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path is required")
	}
	if err := validatePath(path); err != nil {
		return "", err
	}

	page, err := s.store.ReadWikiPage(path)
	if err != nil {
		return "", fmt.Errorf("read page: %w", err)
	}

	// Format as structured output.
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", path)
	if page.Meta != nil {
		if page.Meta.MemoryType != "" {
			fmt.Fprintf(&sb, "Memory type: %s\n", page.Meta.MemoryType)
		}
		if page.Meta.TrustTierMax > 0 {
			fmt.Fprintf(&sb, "Trust tier: T%d\n", page.Meta.TrustTierMax)
		}
		if len(page.Meta.SourceEvents) > 0 {
			fmt.Fprintf(&sb, "Sources: %s\n", strings.Join(page.Meta.SourceEvents, ", "))
		}
		if page.Meta.LastAccessed != "" {
			fmt.Fprintf(&sb, "Last accessed: %s\n", page.Meta.LastAccessed)
		}
		sb.WriteString("\n---\n\n")
	}
	sb.WriteString(page.Content)
	return sb.String(), nil
}

func (s *Server) execNeighbors() (string, error) {
	content, err := s.store.ReadWikiIndex()
	if err != nil {
		return "", fmt.Errorf("read index: %w", err)
	}
	if content == "" {
		return "Wiki is empty. No knowledge pages yet.", nil
	}
	return content, nil
}

func (s *Server) execWriteMemory(args map[string]any) (string, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return "", fmt.Errorf("text is required")
	}

	actor, _ := args["actor"].(string)
	source, _ := args["source"].(string)
	if actor == "" {
		actor = "mcp_client"
	}
	if source == "" {
		source = "mcp"
	}

	ev := storage.Event{
		Content:       text,
		Actor:         actor,
		Source:        source,
		Durability:    "long-term",
		Actionability: "informational",
		SourceType:    "assistant",
		EvidenceKind:  "direct_observation",
		TrustTier:     2,
	}

	eventID, err := s.store.AppendEvent(ev)
	if err != nil {
		return "", fmt.Errorf("append event: %w", err)
	}

	if s.dreamer != nil {
		return fmt.Sprintf("Stored as %s. Pending integration into wiki pages; call dream to consolidate now.", eventID), nil
	}
	return fmt.Sprintf("Stored as %s. Pending integration into wiki pages; run the HTTP server auto-dreaming flow or start engram9-mcp with -dream and call dream.", eventID), nil
}

func (s *Server) execMemoryStatus() (string, error) {
	stats, err := s.store.GetMemoryStats()
	if err != nil {
		return "", fmt.Errorf("get stats: %w", err)
	}

	if s.dreamer == nil {
		data, _ := json.MarshalIndent(stats, "", "  ")
		return string(data), nil
	}

	data, _ := json.Marshal(stats)
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("marshal stats: %w", err)
	}
	payload["dream"] = s.dreamer.Status()

	data, _ = json.MarshalIndent(payload, "", "  ")
	return string(data), nil
}

func (s *Server) execDream(ctx context.Context) (string, error) {
	if s.dreamer == nil {
		return "", fmt.Errorf("dream is unavailable; start engram9-mcp in runtime data mode with -dream")
	}
	result, err := s.dreamer.Dream(ctx)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
