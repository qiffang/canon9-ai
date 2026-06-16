package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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

func TestNotificationInitialized(t *testing.T) {
	// notifications/initialized should not produce a response.
	s := setupServer(t)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	reqData, _ := json.Marshal(req)

	var out bytes.Buffer
	input := bytes.NewReader(append(reqData, '\n'))
	if err := s.Serve(input, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for notification, got %q", out.String())
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
