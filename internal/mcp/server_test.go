package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiffang/engram9/internal/dream"
	"github.com/qiffang/engram9/internal/storage"
)

// helper: send a JSON-RPC request and return the parsed response.
func call(t *testing.T, s *Server, method string, id int, params any) jsonRPCResponse {
	t.Helper()

	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		paramsJSON = data
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  paramsJSON,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var out bytes.Buffer
	input := bytes.NewReader(append(reqData, '\n'))
	if err := s.Serve(input, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	line := strings.TrimSpace(out.String())
	if line == "" {
		// Notification methods return no response.
		return jsonRPCResponse{}
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", line, err)
	}
	return resp
}

func setupServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return NewServer(store)
}

type fakeDreamer struct {
	calls  int
	result dream.Result
	status dream.Status
	err    error
}

func (f *fakeDreamer) Dream(context.Context) (dream.Result, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeDreamer) Status() dream.Status {
	return f.status
}

type staticCompiler struct {
	summary   string
	newCursor uint64
}

func (c *staticCompiler) Compile(context.Context, uint64) (string, uint64, error) {
	return c.summary, c.newCursor, nil
}

func TestInitialize(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "initialize", 1, map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	data, _ := json.Marshal(resp.Result)
	var result initializeResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol version = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
	if result.ServerInfo.Name != "engram9-mcp" {
		t.Errorf("server name = %q, want engram9-mcp", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Fatal("expected tools capability")
	}
}

func TestToolsList(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/list", 1, nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	data, _ := json.Marshal(resp.Result)
	var result toolsListResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	names := make(map[string]bool)
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	expected := []string{"search_concepts", "read_concept", "neighbors", "write_memory", "memory_status"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestToolsList_WithDreamer(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store, WithDreamer(&fakeDreamer{}))

	resp := call(t, s, "tools/list", 1, nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	data, _ := json.Marshal(resp.Result)
	var result toolsListResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	found := false
	for _, tool := range result.Tools {
		if tool.Name == "dream" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected dream tool when dreamer is configured")
	}
}

func TestPing(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "ping", 1, nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestMethodNotFound(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "nonexistent/method", 1, nil)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

// sendNotification sends a JSON-RPC notification (no "id" field) and returns stdout.
func sendNotification(t *testing.T, s *Server, method string) string {
	t.Helper()
	// Build JSON manually to ensure no "id" key at all.
	raw := fmt.Sprintf(`{"jsonrpc":"2.0","method":"%s"}`, method)

	var out bytes.Buffer
	input := bytes.NewReader([]byte(raw + "\n"))
	if err := s.Serve(input, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	return out.String()
}

func TestNotificationInitialized(t *testing.T) {
	s := setupServer(t)
	got := sendNotification(t, s, "notifications/initialized")
	if got != "" {
		t.Errorf("expected no output for notification, got %q", got)
	}
}

func TestNotificationPing_NoResponse(t *testing.T) {
	// A ping without "id" is a notification — must not get a response.
	s := setupServer(t)
	got := sendNotification(t, s, "ping")
	if got != "" {
		t.Errorf("expected no output for notification ping, got %q", got)
	}
}

func TestNotificationUnknownMethod_NoResponse(t *testing.T) {
	s := setupServer(t)
	got := sendNotification(t, s, "some/unknown")
	if got != "" {
		t.Errorf("expected no output for unknown notification, got %q", got)
	}
}

func TestSearchConcepts_Empty(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "search_concepts",
		"arguments": map[string]any{"query": "nonexistent"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	if result.Content[0].Text != "No results found." {
		t.Errorf("unexpected text: %q", result.Content[0].Text)
	}
}

func TestSearchConcepts_MissingQuery(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "search_concepts",
		"arguments": map[string]any{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for missing query")
	}
}

func TestWriteMemoryAndSearch(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store)

	// Write a wiki page directly so we can search it.
	if err := store.WriteWikiPage("semantic/test-concept.md", "# Test\n\nAlice prefers dark mode."); err != nil {
		t.Fatalf("WriteWikiPage: %v", err)
	}
	if err := store.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}

	// Search should find it.
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "search_concepts",
		"arguments": map[string]any{"query": "dark mode"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("search error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "dark mode") {
		t.Errorf("expected 'dark mode' in results, got %q", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "semantic/test-concept.md") {
		t.Errorf("expected path in results, got %q", result.Content[0].Text)
	}
}

func TestReadConcept(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store)

	content := "# Commit Queue\n\nThe commit queue manages async uploads."
	if err := store.WriteWikiPageWithMeta("semantic/commit-queue.md", content, []string{"evt_042"}, 1); err != nil {
		t.Fatalf("WriteWikiPage: %v", err)
	}

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "read_concept",
		"arguments": map[string]any{"path": "semantic/commit-queue.md"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("read error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "commit queue manages async") {
		t.Errorf("expected content, got %q", text)
	}
	if !strings.Contains(text, "Memory type: semantic") {
		t.Errorf("expected memory type in metadata, got %q", text)
	}
	if !strings.Contains(text, "Trust tier: T1") {
		t.Errorf("expected trust tier in metadata, got %q", text)
	}
	if !strings.Contains(text, "evt_042") {
		t.Errorf("expected source event in metadata, got %q", text)
	}
}

func TestReadConcept_NotFound(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "read_concept",
		"arguments": map[string]any{"path": "semantic/does-not-exist.md"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for non-existent page")
	}
}

func TestReadConcept_PathTraversal(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "read_concept",
		"arguments": map[string]any{"path": "../../../etc/passwd"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for path traversal")
	}
	if !strings.Contains(result.Content[0].Text, "traversal") {
		t.Errorf("expected traversal error message, got %q", result.Content[0].Text)
	}
}

func TestReadConcept_AbsolutePath(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "read_concept",
		"arguments": map[string]any{"path": "/etc/passwd"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for absolute path")
	}
}

func TestReadConcept_MetaAccess(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "read_concept",
		"arguments": map[string]any{"path": ".meta/semantic/test.json"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for .meta access")
	}
}

func TestNeighbors_Empty(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "neighbors",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "empty") {
		t.Errorf("expected empty message, got %q", result.Content[0].Text)
	}
}

func TestNeighbors_WithPages(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store)

	store.WriteWikiPage("semantic/db9.md", "# DB9\n\nDatabase layer.")
	store.WriteWikiPage("procedural/deploy.md", "# Deploy\n\nHow to deploy.")
	store.RebuildIndex()

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "neighbors",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	text := result.Content[0].Text
	if !strings.Contains(text, "semantic") {
		t.Errorf("expected semantic section, got %q", text)
	}
	if !strings.Contains(text, "procedural") {
		t.Errorf("expected procedural section, got %q", text)
	}
}

func TestWriteMemory(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store)

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name": "write_memory",
		"arguments": map[string]any{
			"text":   "Bob switched to Neovim",
			"actor":  "claude",
			"source": "conversation",
		},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("write error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "evt_") {
		t.Errorf("expected event ID in response, got %q", result.Content[0].Text)
	}

	// Verify event was persisted.
	events, err := store.ReadRecentEvents(1)
	if err != nil {
		t.Fatalf("ReadRecentEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Content != "Bob switched to Neovim" {
		t.Errorf("event content = %q, want 'Bob switched to Neovim'", events[0].Content)
	}
	if events[0].Actor != "claude" {
		t.Errorf("actor = %q, want 'claude'", events[0].Actor)
	}
}

func TestWriteMemory_WithDreamerHintsDreamTool(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store, WithDreamer(&fakeDreamer{}))

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "write_memory",
		"arguments": map[string]any{"text": "Bob switched to Neovim"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("write error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "call dream") {
		t.Fatalf("expected dream hint, got %q", result.Content[0].Text)
	}
}

func TestWriteMemory_DefaultActorSource(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store)

	call(t, s, "tools/call", 1, map[string]any{
		"name": "write_memory",
		"arguments": map[string]any{
			"text": "Some info without actor/source",
		},
	})

	events, _ := store.ReadRecentEvents(1)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Actor != "mcp_client" {
		t.Errorf("default actor = %q, want 'mcp_client'", events[0].Actor)
	}
	if events[0].Source != "mcp" {
		t.Errorf("default source = %q, want 'mcp'", events[0].Source)
	}
}

func TestWriteMemory_MissingText(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "write_memory",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for missing text")
	}
}

func TestMemoryStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	s := NewServer(store)

	// Append an event and create a page.
	store.AppendEvent(storage.Event{Content: "test", Durability: "long-term", SourceType: "user", EvidenceKind: "user_statement", TrustTier: 1})
	store.WriteWikiPage("semantic/test.md", "# Test")

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "memory_status",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("error: %s", result.Content[0].Text)
	}

	var stats storage.MemoryStats
	if err := json.Unmarshal([]byte(result.Content[0].Text), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if stats.EventCount != 1 {
		t.Errorf("event_count = %d, want 1", stats.EventCount)
	}
	if stats.WikiPageCount != 1 {
		t.Errorf("wiki_page_count = %d, want 1", stats.WikiPageCount)
	}
}

func TestMemoryStatus_WithDreamerIncludesDreamStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	dreamer := &fakeDreamer{status: dream.Status{
		LastStartCursor:     1,
		LastNewCursor:       3,
		LastProcessedEvents: 2,
		LastFinishedAt:      "2026-06-17T03:00:01Z",
	}}
	s := NewServer(store, WithDreamer(dreamer))

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "memory_status",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("error: %s", result.Content[0].Text)
	}

	var payload struct {
		EventCount int `json:"event_count"`
		Dream      struct {
			LastStartCursor     uint64 `json:"last_start_cursor"`
			LastNewCursor       uint64 `json:"last_new_cursor"`
			LastProcessedEvents int    `json:"last_processed_events"`
			LastFinishedAt      string `json:"last_finished_at"`
		} `json:"dream"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Dream.LastStartCursor != 1 || payload.Dream.LastNewCursor != 3 || payload.Dream.LastProcessedEvents != 2 {
		t.Fatalf("unexpected dream status: %+v", payload.Dream)
	}
	if payload.Dream.LastFinishedAt == "" {
		t.Fatal("expected last_finished_at")
	}
}

func TestDreamTool(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	dreamer := &fakeDreamer{result: dream.Result{
		StartCursor:     0,
		NewCursor:       2,
		ProcessedEvents: 2,
		Summary:         "compiled two events",
	}}
	s := NewServer(store, WithDreamer(dreamer))

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "dream",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("dream error: %s", result.Content[0].Text)
	}
	if dreamer.calls != 1 {
		t.Fatalf("dream calls = %d, want 1", dreamer.calls)
	}
	if !strings.Contains(result.Content[0].Text, `"processed_events": 2`) {
		t.Fatalf("expected processed_events in result, got %q", result.Content[0].Text)
	}
}

func TestDreamTool_UnavailableWithoutDreamer(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "dream",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Fatal("expected isError=true for dream without dreamer")
	}
	if !strings.Contains(result.Content[0].Text, "-dream") {
		t.Fatalf("expected -dream hint, got %q", result.Content[0].Text)
	}
}

func TestWriteDreamStatusFlow(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	dreamer := dream.NewRunner(store, &staticCompiler{
		summary:   "compiled pending memory",
		newCursor: 1,
	})
	s := NewServer(store, WithDreamer(dreamer))

	writeResp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "write_memory",
		"arguments": map[string]any{"text": "Alice prefers dark mode"},
	})
	writeData, _ := json.Marshal(writeResp.Result)
	var writeResult toolsCallResult
	json.Unmarshal(writeData, &writeResult)
	if writeResult.IsError {
		t.Fatalf("write error: %s", writeResult.Content[0].Text)
	}

	dreamResp := call(t, s, "tools/call", 2, map[string]any{
		"name":      "dream",
		"arguments": map[string]any{},
	})
	dreamData, _ := json.Marshal(dreamResp.Result)
	var dreamResult toolsCallResult
	json.Unmarshal(dreamData, &dreamResult)
	if dreamResult.IsError {
		t.Fatalf("dream error: %s", dreamResult.Content[0].Text)
	}

	statusResp := call(t, s, "tools/call", 3, map[string]any{
		"name":      "memory_status",
		"arguments": map[string]any{},
	})
	statusData, _ := json.Marshal(statusResp.Result)
	var statusResult toolsCallResult
	json.Unmarshal(statusData, &statusResult)
	if statusResult.IsError {
		t.Fatalf("status error: %s", statusResult.Content[0].Text)
	}

	var payload struct {
		EventCount      int `json:"event_count"`
		UncompiledCount int `json:"uncompiled_count"`
		Dream           struct {
			LastNewCursor       uint64 `json:"last_new_cursor"`
			LastProcessedEvents int    `json:"last_processed_events"`
			LastSummary         string `json:"last_summary"`
		} `json:"dream"`
	}
	if err := json.Unmarshal([]byte(statusResult.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if payload.EventCount != 1 || payload.UncompiledCount != 0 {
		t.Fatalf("unexpected memory counts: %+v", payload)
	}
	if payload.Dream.LastNewCursor != 1 || payload.Dream.LastProcessedEvents != 1 || payload.Dream.LastSummary != "compiled pending memory" {
		t.Fatalf("unexpected dream status: %+v", payload.Dream)
	}
}

func TestUnknownTool(t *testing.T) {
	s := setupServer(t)
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for unknown tool")
	}
}

func TestInvalidJSON(t *testing.T) {
	s := setupServer(t)

	var out bytes.Buffer
	input := bytes.NewReader([]byte("this is not json\n"))
	s.Serve(input, &out)

	line := strings.TrimSpace(out.String())
	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestBundleMode_SearchFindsPages(t *testing.T) {
	// Create a fake OKF bundle directory with .md files at the root.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "semantic"), 0755)
	os.WriteFile(filepath.Join(dir, "semantic", "test-concept.md"), []byte("# Test\n\nAlice prefers dark mode."), 0644)
	os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Index\n\n- [test-concept](semantic/test-concept.md)"), 0644)

	store, err := storage.NewBundleFS(dir)
	if err != nil {
		t.Fatalf("NewBundleFS: %v", err)
	}
	s := NewServer(store)

	// Search should find content.
	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "search_concepts",
		"arguments": map[string]any{"query": "dark mode"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("search error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "dark mode") {
		t.Errorf("expected 'dark mode' in results, got %q", result.Content[0].Text)
	}
}

func TestBundleMode_ReadConcept(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "semantic"), 0755)
	os.WriteFile(filepath.Join(dir, "semantic", "commit-queue.md"), []byte("# Commit Queue\n\nAsync uploads."), 0644)

	store, err := storage.NewBundleFS(dir)
	if err != nil {
		t.Fatalf("NewBundleFS: %v", err)
	}
	s := NewServer(store)

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "read_concept",
		"arguments": map[string]any{"path": "semantic/commit-queue.md"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("read error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Async uploads") {
		t.Errorf("expected content, got %q", result.Content[0].Text)
	}
}

func TestBundleMode_Neighbors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Wiki Index\n\n## semantic\n\n- db9"), 0644)

	store, err := storage.NewBundleFS(dir)
	if err != nil {
		t.Fatalf("NewBundleFS: %v", err)
	}
	s := NewServer(store)

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "neighbors",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !strings.Contains(result.Content[0].Text, "semantic") {
		t.Errorf("expected index content, got %q", result.Content[0].Text)
	}
}

func TestBundleMode_WriteMemoryRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewBundleFS(dir)
	if err != nil {
		t.Fatalf("NewBundleFS: %v", err)
	}
	s := NewServer(store)

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "write_memory",
		"arguments": map[string]any{"text": "should fail"},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if !result.IsError {
		t.Error("expected isError=true for write to bundle store")
	}
	if !strings.Contains(result.Content[0].Text, "read-only") {
		t.Errorf("expected read-only error, got %q", result.Content[0].Text)
	}
}

func TestBundleMode_MemoryStatusArchiveCount(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "semantic"), 0755)
	os.MkdirAll(filepath.Join(dir, "archive", "semantic"), 0755)
	os.WriteFile(filepath.Join(dir, "semantic", "active.md"), []byte("# Active"), 0644)
	os.WriteFile(filepath.Join(dir, "archive", "semantic", "old.md"), []byte("# Old"), 0644)

	store, err := storage.NewBundleFS(dir)
	if err != nil {
		t.Fatalf("NewBundleFS: %v", err)
	}
	s := NewServer(store)

	resp := call(t, s, "tools/call", 1, map[string]any{
		"name":      "memory_status",
		"arguments": map[string]any{},
	})
	data, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	json.Unmarshal(data, &result)

	if result.IsError {
		t.Fatalf("error: %s", result.Content[0].Text)
	}

	var stats storage.MemoryStats
	if err := json.Unmarshal([]byte(result.Content[0].Text), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if stats.WikiPageCount != 1 {
		t.Errorf("wiki_page_count = %d, want 1", stats.WikiPageCount)
	}
	if stats.ArchivedPageCount != 1 {
		t.Errorf("archived_page_count = %d, want 1", stats.ArchivedPageCount)
	}
}

func TestBundleMode_NoDirCreation(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewBundleFS(dir)
	if err != nil {
		t.Fatalf("NewBundleFS: %v", err)
	}
	_ = NewServer(store)

	// Verify no wiki/ or raw/ directories were created.
	if _, err := os.Stat(filepath.Join(dir, "wiki")); !os.IsNotExist(err) {
		t.Error("bundle mode should not create wiki/ directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "raw")); !os.IsNotExist(err) {
		t.Error("bundle mode should not create raw/ directory")
	}
}

func TestMultipleRequests(t *testing.T) {
	s := setupServer(t)

	// Send two requests in sequence.
	var reqBuf bytes.Buffer
	for i := 1; i <= 2; i++ {
		req := jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(fmt.Sprintf("%d", i)),
			Method:  "ping",
		}
		data, _ := json.Marshal(req)
		reqBuf.Write(data)
		reqBuf.WriteByte('\n')
	}

	var out bytes.Buffer
	if err := s.Serve(&reqBuf, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out.String())
	}
}
