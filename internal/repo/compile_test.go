package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompilePackagePage(t *testing.T) {
	outDir := t.TempDir()
	compiler := NewCompiler(outDir)

	input := &CompileInput{
		Facts: []Fact{
			{ID: "f1", Kind: "type", Name: "CommitQueue", Package: "pkg/fuse", Path: "pkg/fuse/commit_queue.go", Line: 42, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", Anchor: "pkg/fuse/commit_queue.go:42:CommitQueue", Doc: "CommitQueue manages async file uploads."},
			{ID: "f2", Kind: "func", Name: "NewCommitQueue", Package: "pkg/fuse", Path: "pkg/fuse/commit_queue.go", Line: 80, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", Signature: "func NewCommitQueue(workers int) *CommitQueue", Anchor: "pkg/fuse/commit_queue.go:80:NewCommitQueue"},
			{ID: "f3", Kind: "method", Name: "Enqueue", Package: "pkg/fuse", Path: "pkg/fuse/commit_queue.go", Line: 100, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", Receiver: "CommitQueue", Signature: "func (q *CommitQueue) Enqueue(entry *Entry) error", Anchor: "pkg/fuse/commit_queue.go:100:Enqueue"},
			{ID: "f4", Kind: "test", Name: "TestCommitQueueEnqueue", Package: "pkg/fuse", Path: "pkg/fuse/commit_queue_test.go", Line: 15, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", TestTarget: "CommitQueue.Enqueue", Anchor: "pkg/fuse/commit_queue_test.go:15:TestCommitQueueEnqueue"},
			{ID: "f5", Kind: "const", Name: "DefaultWorkers", Package: "pkg/fuse", Path: "pkg/fuse/commit_queue.go", Line: 10, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", Anchor: "pkg/fuse/commit_queue.go:10:DefaultWorkers"},
			{ID: "f6", Kind: "interface", Name: "Uploader", Package: "pkg/fuse", Path: "pkg/fuse/uploader.go", Line: 5, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", Anchor: "pkg/fuse/uploader.go:5:Uploader", Doc: "Uploader defines the upload contract."},
		},
		Snippets: []Snippet{
			{ID: "s1", FactID: "f1", Path: "pkg/fuse/commit_queue.go", StartLine: 42, EndLine: 55, CommitSHA: "abc12345", Repo: "mem9-ai/drive9", Language: "go", Content: "type CommitQueue struct {\n\tworkers int\n\tpending chan *Entry\n}", Anchor: "pkg/fuse/commit_queue.go:42-55"},
		},
		Manifest: Manifest{
			Repo:    "mem9-ai/drive9",
			HeadSHA: "abc12345deadbeef",
			Scope:   "pkg/fuse",
			Files: []FileManifest{
				{Path: "pkg/fuse/commit_queue.go", Hash: "sha256:aaa"},
				{Path: "pkg/fuse/uploader.go", Hash: "sha256:bbb"},
			},
		},
	}

	output, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if output.PageCount != 1 {
		t.Fatalf("expected 1 page, got %d", output.PageCount)
	}

	page := output.Pages[0]

	if !strings.HasPrefix(page.Slug, "semantic/repo/") {
		t.Errorf("slug should start with semantic/repo/, got %s", page.Slug)
	}

	if !strings.Contains(page.Content, "type: concept") {
		t.Error("missing OKF type: concept frontmatter")
	}
	if !strings.Contains(page.Content, "commit_sha:") {
		t.Error("missing commit_sha in frontmatter")
	}
	if !strings.Contains(page.Content, "memory_type: semantic") {
		t.Error("missing memory_type in frontmatter")
	}
	if strings.Contains(page.Content, "timestamp:") {
		t.Error("frontmatter should not contain non-deterministic timestamp")
	}

	if !strings.Contains(page.Content, "## Interfaces") {
		t.Error("missing Interfaces section")
	}
	if !strings.Contains(page.Content, "## Types") {
		t.Error("missing Types section")
	}
	if !strings.Contains(page.Content, "## Functions") {
		t.Error("missing Functions section")
	}
	if !strings.Contains(page.Content, "## Tests") {
		t.Error("missing Tests section")
	}
	if !strings.Contains(page.Content, "## Constants & Variables") {
		t.Error("missing Constants & Variables section")
	}

	// Doc summaries rendered.
	if !strings.Contains(page.Content, "CommitQueue manages async file uploads.") {
		t.Error("missing doc summary for CommitQueue")
	}
	if !strings.Contains(page.Content, "Uploader defines the upload contract.") {
		t.Error("missing doc summary for Uploader")
	}

	// Source anchors present.
	if !strings.Contains(page.Content, "commit_queue.go:42") {
		t.Error("missing source location for CommitQueue")
	}

	// Code snippet embedded.
	if !strings.Contains(page.Content, "type CommitQueue struct") {
		t.Error("missing code snippet")
	}
	if !strings.Contains(page.Content, "```go") {
		t.Error("missing go code fence")
	}

	if len(page.SourceAnchors) == 0 {
		t.Error("SourceAnchors should not be empty")
	}

	pagePath := filepath.Join(outDir, page.Slug)
	if _, err := os.Stat(pagePath); os.IsNotExist(err) {
		t.Errorf("page file not written: %s", pagePath)
	}
}

func TestCompileMultiplePackages(t *testing.T) {
	outDir := t.TempDir()
	compiler := NewCompiler(outDir)

	input := &CompileInput{
		Facts: []Fact{
			{ID: "f1", Kind: "type", Name: "Store", Package: "pkg/fuse", Path: "pkg/fuse/store.go", Line: 10, CommitSHA: "abc", Repo: "test"},
			{ID: "f2", Kind: "type", Name: "Server", Package: "pkg/server", Path: "pkg/server/server.go", Line: 20, CommitSHA: "abc", Repo: "test"},
		},
		Manifest: Manifest{Repo: "test", HeadSHA: "abc12345", Scope: "pkg"},
	}

	output, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if output.PageCount != 2 {
		t.Fatalf("expected 2 pages, got %d", output.PageCount)
	}

	slugs := make(map[string]bool)
	for _, p := range output.Pages {
		slugs[p.Slug] = true
	}
	if len(slugs) != 2 {
		t.Error("expected 2 unique slugs")
	}
}

func TestCompileEmptyInput(t *testing.T) {
	outDir := t.TempDir()
	compiler := NewCompiler(outDir)

	input := &CompileInput{
		Manifest: Manifest{Repo: "test", HeadSHA: "abc"},
	}

	output, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if output.PageCount != 0 {
		t.Errorf("expected 0 pages for empty input, got %d", output.PageCount)
	}
}

func TestLoadInput(t *testing.T) {
	dir := t.TempDir()

	manifest := Manifest{Repo: "test", HeadSHA: "abc123", Scope: "pkg/fuse"}
	mdata, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "manifest.json"), mdata, 0644)

	fact := Fact{ID: "f1", Kind: "type", Name: "Foo", Package: "pkg/fuse", Path: "pkg/fuse/foo.go", Line: 1, CommitSHA: "abc123", Repo: "test"}
	fdata, _ := json.Marshal(fact)
	os.WriteFile(filepath.Join(dir, "facts.jsonl"), append(fdata, '\n'), 0644)

	snippet := Snippet{ID: "s1", FactID: "f1", Path: "pkg/fuse/foo.go", StartLine: 1, EndLine: 5, CommitSHA: "abc123", Language: "go", Content: "type Foo struct{}"}
	sdata, _ := json.Marshal(snippet)
	os.WriteFile(filepath.Join(dir, "snippets.jsonl"), append(sdata, '\n'), 0644)

	input, err := LoadInput(dir)
	if err != nil {
		t.Fatalf("LoadInput failed: %v", err)
	}
	if len(input.Facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(input.Facts))
	}
	if len(input.Snippets) != 1 {
		t.Errorf("expected 1 snippet, got %d", len(input.Snippets))
	}
	if input.Manifest.HeadSHA != "abc123" {
		t.Errorf("expected HeadSHA abc123, got %s", input.Manifest.HeadSHA)
	}
}

func TestPackageSlug(t *testing.T) {
	tests := []struct {
		pkg   string
		scope string
		want  string
	}{
		{"pkg/fuse", "pkg/fuse", "semantic/repo/pkg-fuse.md"},
		{"github.com/foo/bar/pkg/fuse", "pkg/fuse", "semantic/repo/pkg-fuse.md"},
		{"pkg/server", "pkg", "semantic/repo/pkg-server.md"},
	}
	for _, tt := range tests {
		got := packageSlug(tt.pkg, tt.scope)
		if got != tt.want {
			t.Errorf("packageSlug(%q, %q) = %q, want %q", tt.pkg, tt.scope, got, tt.want)
		}
	}
}

func TestFilterKind(t *testing.T) {
	facts := []Fact{
		{Kind: "type", Name: "A"},
		{Kind: "func", Name: "B"},
		{Kind: "type", Name: "C"},
	}
	types := filterKind(facts, "type")
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}

func TestFirstSentence(t *testing.T) {
	if got := firstSentence("CommitQueue manages uploads. It is cool."); got != "CommitQueue manages uploads." {
		t.Errorf("got %q", got)
	}
	if got := firstSentence("No period"); got != "No period" {
		t.Errorf("got %q", got)
	}
}

func TestMethodsGroupedUnderType(t *testing.T) {
	outDir := t.TempDir()
	compiler := NewCompiler(outDir)

	// Use pointer receiver "*Queue" to test that filterMethods strips the prefix.
	input := &CompileInput{
		Facts: []Fact{
			{ID: "f1", Kind: "type", Name: "Queue", Package: "pkg/q", Path: "q.go", Line: 1, CommitSHA: "a", Repo: "r"},
			{ID: "f2", Kind: "method", Name: "Push", Package: "pkg/q", Path: "q.go", Line: 10, CommitSHA: "a", Repo: "r", Receiver: "*Queue", Signature: "func (q *Queue) Push(v int)"},
			{ID: "f3", Kind: "method", Name: "Pop", Package: "pkg/q", Path: "q.go", Line: 20, CommitSHA: "a", Repo: "r", Receiver: "Queue", Signature: "func (q *Queue) Pop() int"},
		},
		Manifest: Manifest{Repo: "r", HeadSHA: "aaa", Scope: "pkg/q"},
	}

	output, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	page := output.Pages[0]
	if !strings.Contains(page.Content, "**Methods:**") {
		t.Error("methods should be grouped under their type")
	}
	if !strings.Contains(page.Content, "Push") || !strings.Contains(page.Content, "Pop") {
		t.Error("methods Push and Pop should appear")
	}
}

func TestFactNameFallback(t *testing.T) {
	f := Fact{Symbol: "MySymbol"}
	if got := factName(f); got != "MySymbol" {
		t.Errorf("expected MySymbol, got %s", got)
	}
	f2 := Fact{Name: "MyName", Symbol: "MySymbol"}
	if got := factName(f2); got != "MyName" {
		t.Errorf("expected MyName, got %s", got)
	}
}

func TestFactAnchorFallback(t *testing.T) {
	f := Fact{ID: "f1", Anchor: "pkg/foo.go:10:Bar"}
	if got := factAnchor(f); got != "pkg/foo.go:10:Bar" {
		t.Errorf("expected anchor, got %s", got)
	}
	f2 := Fact{ID: "f2"}
	if got := factAnchor(f2); got != "f2" {
		t.Errorf("expected ID fallback, got %s", got)
	}
}

func TestTestTargetFallback(t *testing.T) {
	outDir := t.TempDir()
	compiler := NewCompiler(outDir)

	input := &CompileInput{
		Facts: []Fact{
			{ID: "f1", Kind: "test", Name: "TestFoo", Package: "pkg/a", Path: "a_test.go", Line: 1, CommitSHA: "a", Repo: "r", TestTarget: "Foo"},
			{ID: "f2", Kind: "test", Name: "TestBar", Package: "pkg/a", Path: "a_test.go", Line: 10, CommitSHA: "a", Repo: "r", Target: "Bar"},
			{ID: "f3", Kind: "test", Name: "TestBaz", Package: "pkg/a", Path: "a_test.go", Line: 20, CommitSHA: "a", Repo: "r"},
		},
		Manifest: Manifest{Repo: "r", HeadSHA: "aaa", Scope: "pkg/a"},
	}

	output, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	page := output.Pages[0]
	if !strings.Contains(page.Content, "tests `Foo`") {
		t.Error("TestTarget should be used first")
	}
	if !strings.Contains(page.Content, "tests `Bar`") {
		t.Error("Target should be used as fallback")
	}
	if !strings.Contains(page.Content, "tests `(package-level)`") {
		t.Error("should fall back to (package-level)")
	}
}
