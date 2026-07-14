package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/qiffang/engram9/internal/storage"
)

// ToolExecutor executes tool calls against the storage layer.
type ToolExecutor struct {
	store storage.Store
}

func NewToolExecutor(store storage.Store) *ToolExecutor {
	return &ToolExecutor{store: store}
}

// Execute runs a tool call and returns the result as a string.
func (te *ToolExecutor) Execute(name string, input json.RawMessage) (string, error) {
	switch name {
	case "append_event":
		return te.appendEvent(input)
	case "read_events_since":
		return te.readEventsSince(input)
	case "read_wiki_index":
		return te.readWikiIndex()
	case "read_wiki_page":
		return te.readWikiPage(input)
	case "search_wiki":
		return te.searchWiki(input)
	case "write_wiki_page":
		return te.writeWikiPage(input)
	case "archive_wiki_page":
		return te.archiveWikiPage(input)
	case "rebuild_index":
		return te.rebuildIndex()
	case "get_memory_stats":
		return te.getMemoryStats()
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (te *ToolExecutor) appendEvent(input json.RawMessage) (string, error) {
	var ev storage.Event
	if err := json.Unmarshal(input, &ev); err != nil {
		return "", fmt.Errorf("parse append_event input: %w", err)
	}
	id, err := te.store.AppendEvent(ev)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"event_id": "%s"}`, id), nil
}

func (te *ToolExecutor) readEventsSince(input json.RawMessage) (string, error) {
	var params struct {
		Cursor uint64 `json:"cursor"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse read_events_since input: %w", err)
	}
	page, err := te.store.ReadEventsSince(params.Cursor)
	if err != nil {
		return "", err
	}
	// Truncate event content to avoid exceeding LLM API per-message size limits.
	// Integrations already processed full content into wiki pages; compile only
	// needs metadata and a summary to do cross-referencing and index rebuild.
	const maxContentLen = 2000
	for i := range page.Events {
		if len(page.Events[i].Content) > maxContentLen {
			page.Events[i].Content = page.Events[i].Content[:maxContentLen] + "\n... [truncated for compile]"
		}
	}
	data, _ := json.Marshal(page)
	return string(data), nil
}

func (te *ToolExecutor) readWikiIndex() (string, error) {
	content, err := te.store.ReadWikiIndex()
	if err != nil {
		return "", err
	}
	if content == "" {
		return "_No wiki index yet. Wiki is empty._", nil
	}
	return content, nil
}

func (te *ToolExecutor) readWikiPage(input json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse read_wiki_page input: %w", err)
	}
	page, err := te.store.ReadWikiPage(params.Path)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(page)
	return string(data), nil
}

func (te *ToolExecutor) searchWiki(input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse search_wiki input: %w", err)
	}
	results, err := te.store.SearchWiki(params.Query)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No results found.", nil
	}
	data, _ := json.Marshal(results)
	return string(data), nil
}

// EnsureFrontmatter injects compiled_from and last_compiled frontmatter
// comments if they are missing from the content.
func EnsureFrontmatter(content string, sourceEvents []string, now time.Time) string {
	hasCompiledFrom := strings.Contains(content, "<!-- compiled_from:")
	hasLastCompiled := strings.Contains(content, "<!-- last_compiled:")

	if hasCompiledFrom && hasLastCompiled {
		return content
	}

	var lines []string
	if !hasCompiledFrom {
		src := "ingest"
		if len(sourceEvents) > 0 {
			src = strings.Join(sourceEvents, ", ")
		}
		lines = append(lines, fmt.Sprintf("<!-- compiled_from: %s -->", src))
	}
	if !hasLastCompiled {
		lines = append(lines, fmt.Sprintf("<!-- last_compiled: %s -->", now.UTC().Format(time.RFC3339)))
	}

	if len(lines) == 0 {
		return content
	}
	return strings.Join(lines, "\n") + "\n" + content
}

func (te *ToolExecutor) writeWikiPage(input json.RawMessage) (string, error) {
	var params struct {
		Path         string   `json:"path"`
		Content      string   `json:"content"`
		SourceEvents []string `json:"source_events,omitempty"`
		TrustTier    int      `json:"trust_tier,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse write_wiki_page input: %w", err)
	}
	if !IsValidWikiPath(params.Path) {
		return "", fmt.Errorf("invalid wiki path %q: must start with semantic/, episodic/, procedural/, prospective/, or be index.md", params.Path)
	}
	params.Content = EnsureFrontmatter(params.Content, params.SourceEvents, time.Now())
	if err := te.store.WriteWikiPageWithMeta(params.Path, params.Content, params.SourceEvents, params.TrustTier); err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"status": "ok", "path": "%s"}`, params.Path), nil
}

func (te *ToolExecutor) archiveWikiPage(input json.RawMessage) (string, error) {
	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse archive_wiki_page input: %w", err)
	}
	if err := te.store.ArchiveWikiPage(params.Path, params.Reason); err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"status": "archived", "path": "%s"}`, params.Path), nil
}

func (te *ToolExecutor) rebuildIndex() (string, error) {
	if err := te.store.RebuildIndex(); err != nil {
		return "", err
	}
	return `{"status": "index rebuilt"}`, nil
}

func (te *ToolExecutor) getMemoryStats() (string, error) {
	stats, err := te.store.GetMemoryStats()
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(stats)
	return string(data), nil
}
