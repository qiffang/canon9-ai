package repo

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanGoFactsFull(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, root, "pkg/fuse/store.go", `package fuse

import "context"

type Store struct{}

type Reader interface {
	ReadAt([]byte, int64) (int, error)
}

func NewStore() *Store { return &Store{} }

// Flush persists buffered changes.
func (s *Store) Flush(ctx context.Context) error { return nil }

const DefaultLimit = 10

var DefaultStore = NewStore()
`)
	writeFile(t, root, "pkg/fuse/store_test.go", `package fuse

import "testing"

func TestStoreFlush(t *testing.T) {}
`)
	commitAll(t, root, "initial")

	bundle, err := Scan(ScanOptions{RepoPath: root, Scope: "pkg/fuse"})
	if err != nil {
		t.Fatal(err)
	}

	assertFact(t, bundle.Facts, "package", "fuse")
	assertFact(t, bundle.Facts, "import", "context")
	assertFact(t, bundle.Facts, "type", "Store")
	assertFact(t, bundle.Facts, "interface", "Reader")
	assertFact(t, bundle.Facts, "func", "NewStore")
	assertFact(t, bundle.Facts, "method", "*Store.Flush")
	assertFact(t, bundle.Facts, "test", "TestStoreFlush")
	assertFact(t, bundle.Facts, "const", "DefaultLimit")
	assertFact(t, bundle.Facts, "var", "DefaultStore")
	assertSnippet(t, bundle.Snippets, "type", "Store", "type Store struct")
	assertSnippet(t, bundle.Snippets, "interface", "Reader", "type Reader interface")
	assertSnippet(t, bundle.Snippets, "func", "NewStore", "func NewStore() *Store")
	assertSnippet(t, bundle.Snippets, "method", "*Store.Flush", "func (s *Store) Flush")
	assertSnippet(t, bundle.Snippets, "test", "TestStoreFlush", "func TestStoreFlush")
	assertSnippet(t, bundle.Snippets, "const", "DefaultLimit", "const DefaultLimit")
	assertSnippet(t, bundle.Snippets, "var", "DefaultStore", "var DefaultStore")
	flush := findFact(bundle.Facts, "method", "*Store.Flush")
	if flush == nil || flush.Doc != "Flush persists buffered changes." || flush.Receiver != "Store" {
		t.Fatalf("method doc/receiver not canonical: %+v", flush)
	}
	for _, fact := range bundle.Facts {
		if fact.Repo == "" || fact.CommitSHA == "" || fact.Path == "" || fact.ID == "" || fact.Anchor == "" {
			t.Fatalf("fact missing required provenance: %+v", fact)
		}
		if fact.Status != "present" {
			t.Fatalf("fact status=%q, want present: %+v", fact.Status, fact)
		}
		if fact.Kind != "file" && fact.Line == 0 {
			t.Fatalf("fact missing source line: %+v", fact)
		}
	}
	if bundle.Manifest.Version != FactsVersion {
		t.Fatalf("version=%q, want %q", bundle.Manifest.Version, FactsVersion)
	}
	if bundle.Manifest.Scope != "pkg/fuse" {
		t.Fatalf("scope=%q", bundle.Manifest.Scope)
	}
	if len(bundle.Manifest.Files) != 2 {
		t.Fatalf("files=%d, want 2", len(bundle.Manifest.Files))
	}
	if bundle.Manifest.HeadSHA == "" || len(bundle.Manifest.FactIDs) == 0 || len(bundle.Manifest.SnippetIDs) == 0 {
		t.Fatalf("manifest missing canonical IDs: %+v", bundle.Manifest)
	}
	for _, file := range bundle.Manifest.Files {
		if len(file.Anchors) == 0 {
			t.Fatalf("file manifest missing anchors: %+v", file)
		}
		if len(file.SnippetIDs) == 0 {
			t.Fatalf("file manifest missing snippet ids: %+v", file)
		}
	}
}

func TestScanSinceMarksDeletedAndChangedFiles(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, root, "pkg/fuse/a.go", "package fuse\n\ntype A struct{}\n")
	writeFile(t, root, "pkg/fuse/b.go", "package fuse\n\ntype B struct{}\n")
	base := commitAll(t, root, "initial")

	writeFile(t, root, "pkg/fuse/a.go", "package fuse\n\ntype A struct{}\nfunc Changed() {}\n")
	if err := os.Remove(filepath.Join(root, "pkg/fuse/b.go")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "change a delete b")

	bundle, err := Scan(ScanOptions{RepoPath: root, Scope: "pkg/fuse", Since: base})
	if err != nil {
		t.Fatal(err)
	}

	if bundle.Manifest.BaseSHA != base {
		t.Fatalf("base=%q, want %q", bundle.Manifest.BaseSHA, base)
	}
	if len(bundle.Manifest.Changed) != 1 || bundle.Manifest.Changed[0] != "pkg/fuse/a.go" {
		t.Fatalf("changed=%v", bundle.Manifest.Changed)
	}
	if len(bundle.Manifest.Deleted) != 1 || bundle.Manifest.Deleted[0] != "pkg/fuse/b.go" {
		t.Fatalf("deleted=%v", bundle.Manifest.Deleted)
	}
	assertFact(t, bundle.Facts, "func", "Changed")
	deleted := findFact(bundle.Facts, "file", "")
	if deleted == nil || deleted.Status != "deleted" || deleted.Path != "pkg/fuse/b.go" {
		t.Fatalf("deleted file fact missing: %+v", bundle.Facts)
	}
	for _, fact := range bundle.Facts {
		if fact.Path == "pkg/fuse/b.go" && fact.Kind != "file" {
			t.Fatalf("deleted file emitted stale symbol fact: %+v", fact)
		}
		if fact.Path == "pkg/fuse/b.go" && fact.Anchor == "" {
			t.Fatalf("deleted file missing tombstone anchor: %+v", fact)
		}
	}
}

func TestScanSinceHandlesPathsWithSpaces(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, root, "pkg/fuse/file with spaces.go", "package fuse\n\ntype A struct{}\n")
	base := commitAll(t, root, "initial")

	writeFile(t, root, "pkg/fuse/file with spaces.go", "package fuse\n\ntype A struct{}\nfunc Changed() {}\n")
	commitAll(t, root, "change spaced path")

	bundle, err := Scan(ScanOptions{RepoPath: root, Scope: "pkg/fuse", Since: base})
	if err != nil {
		t.Fatal(err)
	}

	if len(bundle.Manifest.Changed) != 1 || bundle.Manifest.Changed[0] != "pkg/fuse/file with spaces.go" {
		t.Fatalf("changed=%v", bundle.Manifest.Changed)
	}
	assertFact(t, bundle.Facts, "func", "Changed")
}

func TestWriteBundleWritesManifestAndFactsJSONL(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, root, "pkg/fuse/a.go", "package fuse\n\ntype A struct{}\n")
	commitAll(t, root, "initial")

	bundle, err := Scan(ScanOptions{RepoPath: root, Scope: "pkg/fuse"})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "facts")
	if err := WriteBundle(bundle, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "facts.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "snippets.jsonl")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "facts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var fact Fact
	if err := json.Unmarshal(data[:findNewline(data)], &fact); err != nil {
		t.Fatal(err)
	}
	if fact.CommitSHA == "" || fact.Path == "" {
		t.Fatalf("fact missing provenance: %+v", fact)
	}
	snippetData, err := os.ReadFile(filepath.Join(out, "snippets.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var snippet Snippet
	if err := json.Unmarshal(snippetData[:findNewline(snippetData)], &snippet); err != nil {
		t.Fatal(err)
	}
	if snippet.FactID == "" || snippet.Anchor == "" || snippet.Language != "go" || snippet.Content == "" {
		t.Fatalf("snippet missing required fields: %+v", snippet)
	}
}

func assertFact(t *testing.T, facts []Fact, kind, symbol string) {
	t.Helper()
	if fact := findFact(facts, kind, symbol); fact == nil {
		t.Fatalf("missing fact kind=%s symbol=%s in %+v", kind, symbol, facts)
	}
}

func assertSnippet(t *testing.T, snippets []Snippet, kind, symbol, wantCodePrefix string) {
	t.Helper()
	for _, snippet := range snippets {
		if snippet.Kind != kind || snippet.Symbol != symbol {
			continue
		}
		if snippet.ID == "" || snippet.FactID == "" || snippet.Anchor == "" || snippet.Language != "go" || snippet.Content == "" {
			t.Fatalf("snippet missing required fields: %+v", snippet)
		}
		if !strings.HasPrefix(snippet.Content, wantCodePrefix) {
			t.Fatalf("snippet code=%q, want prefix %q", snippet.Content, wantCodePrefix)
		}
		return
	}
	t.Fatalf("missing snippet kind=%s symbol=%s in %+v", kind, symbol, snippets)
}

func findFact(facts []Fact, kind, symbol string) *Fact {
	for i := range facts {
		if facts[i].Kind == kind && (symbol == "" || facts[i].Symbol == symbol) {
			return &facts[i]
		}
	}
	return nil
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	return root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, root, msg string) string {
	t.Helper()
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", msg)
	out := runGit(t, root, "rev-parse", "HEAD")
	return stringTrim(out)
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func stringTrim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

func findNewline(data []byte) int {
	for i, b := range data {
		if b == '\n' {
			return i
		}
	}
	return len(data)
}
