package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validFrontmatter = `<!-- compiled_from: evt_001 -->
<!-- last_compiled: 2026-07-13T00:00:00Z -->
# Test Page

Content here.
`

func TestWikiValidatorPassesCleanStaging(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, prod, "wiki/semantic/a.md", validFrontmatter)
	writeTestFile(t, staging, "wiki/semantic/a.md", validFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
}

func TestWikiValidatorNoStagingWiki(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, prod, "wiki/semantic/a.md", validFrontmatter)
	// No staging wiki directory at all.

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for missing staging wiki, got %v", violations)
	}
}

func TestWikiValidatorRejectsMissingFrontmatter(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	noFrontmatter := "# Page without frontmatter\n\nJust content.\n"
	writeTestFile(t, staging, "wiki/semantic/a.md", noFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, vi := range violations {
		if vi.Path == "semantic/a.md" && contains(vi.Message, "frontmatter") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected frontmatter violation for semantic/a.md, got %v", violations)
	}
}

func TestWikiValidatorRejectsInvalidTaxonomy(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, staging, "wiki/random/a.md", validFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, vi := range violations {
		if vi.Path == "random/a.md" && contains(vi.Message, "taxonomy") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected taxonomy violation for random/a.md, got %v", violations)
	}
}

func TestWikiValidatorAllowsIndexMD(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, staging, "wiki/index.md", validFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	for _, vi := range violations {
		if vi.Path == "index.md" && contains(vi.Message, "taxonomy") {
			t.Fatalf("index.md should be allowed, got violation: %v", vi)
		}
	}
}

func TestWikiValidatorRejectsDiffBudgetExceeded(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	// Create a staging file larger than the budget.
	bigContent := validFrontmatter + string(make([]byte, 100))
	writeTestFile(t, staging, "wiki/semantic/big.md", bigContent)

	v := NewWikiValidator(10) // very small budget
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, vi := range violations {
		if contains(vi.Message, "diff budget") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected diff budget violation, got %v", violations)
	}
}

func TestWikiValidatorRejectsDeletedPages(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, prod, "wiki/semantic/a.md", validFrontmatter)
	writeTestFile(t, prod, "wiki/semantic/b.md", validFrontmatter)
	// Staging only has a.md — b.md is "deleted".
	writeTestFile(t, staging, "wiki/semantic/a.md", validFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, vi := range violations {
		if vi.Path == "semantic/b.md" && contains(vi.Message, "deleted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected deleted page violation for semantic/b.md, got %v", violations)
	}
}

func TestWikiValidatorAllowsDeleteWhenEnabled(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, prod, "wiki/semantic/a.md", validFrontmatter)
	writeTestFile(t, prod, "wiki/semantic/b.md", validFrontmatter)
	// Staging only has a.md — b.md is "deleted". With AllowDelete=true, no violation.
	writeTestFile(t, staging, "wiki/semantic/a.md", validFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging, ValidateOptions{AllowDelete: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, vi := range violations {
		if contains(vi.Message, "deleted") {
			t.Fatalf("expected no deleted page violation with AllowDelete=true, got: %v", vi)
		}
	}
}

func TestWikiValidatorAllowsMetaSidecars(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, staging, "wiki/.meta/index.json", `{"pages":[]}`)
	writeTestFile(t, staging, "wiki/semantic/a.md", validFrontmatter)

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	for _, vi := range violations {
		if vi.Path == ".meta/index.json" {
			t.Fatalf("expected .meta/ sidecar to be allowed, got violation: %v", vi)
		}
	}
}

func TestWikiValidatorRejectsNonMarkdownFiles(t *testing.T) {
	prod := t.TempDir()
	staging := t.TempDir()

	writeTestFile(t, staging, "wiki/semantic/foo.txt", "not markdown")

	v := NewWikiValidator(DefaultACPMaxDiffBytes)
	violations, err := v.Validate(prod, staging)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, vi := range violations {
		if vi.Path == "semantic/foo.txt" && contains(vi.Message, "non-Markdown") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected non-Markdown violation for semantic/foo.txt, got %v", violations)
	}
}

func TestIsValidWikiPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"semantic/a.md", true},
		{"episodic/b.md", true},
		{"procedural/c.md", true},
		{"prospective/d.md", true},
		{"index.md", true},
		{"random/e.md", false},
		{"a.md", false},
		{"semantic/nested/f.md", true},
	}
	for _, tt := range tests {
		if got := isValidWikiPath(tt.path); got != tt.want {
			t.Errorf("isValidWikiPath(%q)=%v, want %v", tt.path, got, tt.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
