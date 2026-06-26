package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrateLegacyMarkdownCommentsAndWikiLinks(t *testing.T) {
	root := t.TempDir()
	modTime := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	input := `<!-- compiled_from: evt_001, evt_002 -->
<!-- last_compiled: 2026-06-16T10:00:00Z -->
<!-- memory_type: semantic -->
<!-- trust_tier: 2 -->
# Commit Queue

The queue coordinates uploads.

See [[procedural/run-benchmark|Run benchmark]] and [[semantic/people/alice]].
Inline code ` + "`[[semantic/ignored]]`" + ` stays.

` + "```markdown" + `
[[semantic/ignored-too]]
` + "```" + `
`
	got, changed, err := MigrateLegacyMarkdown(root, "semantic/commit-queue.md", input, modTime, modTime)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	wantParts := []string{
		`type: "concept"`,
		`title: "Commit Queue"`,
		`description: "The queue coordinates uploads."`,
		`timestamp: "2026-06-16T10:00:00Z"`,
		`memory_type: "semantic"`,
		`  - "evt_001"`,
		`  - "evt_002"`,
		`trust_tier: "T2"`,
		`See [Run benchmark](../procedural/run-benchmark.md) and [Alice](people/alice.md).`,
		"`[[semantic/ignored]]`",
		"[[semantic/ignored-too]]",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in:\n%s", part, got)
		}
	}
	if strings.Contains(got, "<!-- compiled_from") || strings.Contains(got, "<!-- last_compiled") {
		t.Fatalf("legacy comments should be removed:\n%s", got)
	}

	writeFile(t, root, "semantic/commit-queue.md", got)
	writeFile(t, root, "procedural/run-benchmark.md", validOKFPage("procedure", "Run benchmark", "procedural"))
	writeFile(t, root, "semantic/people/alice.md", validOKFPage("person", "Alice", "semantic"))
	result, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("migrated content should validate strictly: %#v", result.Issues)
	}
}

func TestMigrateLegacyMarkdownStructuralIndexOnlyConvertsLinks(t *testing.T) {
	root := t.TempDir()
	input := "# Index\n\n- [[semantic/commit-queue|Commit Queue]]\n"
	got, changed, err := MigrateLegacyMarkdown(root, "index.md", input, time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected link conversion")
	}
	if strings.HasPrefix(got, "---") {
		t.Fatalf("structural index should not get frontmatter:\n%s", got)
	}
	if !strings.Contains(got, "[Commit Queue](semantic/commit-queue.md)") {
		t.Fatalf("index link not converted:\n%s", got)
	}
}

func TestMigrateLegacyMarkdownExistingFrontmatterOnlyConvertsLinks(t *testing.T) {
	root := t.TempDir()
	input := `---
type: concept
title: Existing
description: Existing page
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events: [evt_001]
trust_tier: T1
---
# Existing

See [[procedural/run-benchmark]].
`
	got, changed, err := MigrateLegacyMarkdown(root, "semantic/existing.md", input, time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected wikilink conversion")
	}
	if strings.Count(got, "---") != 2 {
		t.Fatalf("frontmatter should be preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "See [Run Benchmark](../procedural/run-benchmark.md).") {
		t.Fatalf("wikilink not converted:\n%s", got)
	}
}

func TestMigrateLegacyMarkdownEscapesSpaceDestinations(t *testing.T) {
	root := t.TempDir()
	input := "<!-- compiled_from: evt_001 -->\n# Source\n\nSee [[semantic/Project Alpha]].\n"
	got, changed, err := MigrateLegacyMarkdown(root, "semantic/source.md", input, time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	if !strings.Contains(got, "[Project Alpha](Project%20Alpha.md)") {
		t.Fatalf("space destination should be percent-escaped:\n%s", got)
	}
	if strings.Contains(got, "[[semantic/Project Alpha]]") {
		t.Fatalf("legacy wikilink should not remain in migrated output:\n%s", got)
	}
	writeFile(t, root, "semantic/source.md", got)
	writeFile(t, root, "semantic/Project Alpha.md", validOKFPage("concept", "Project Alpha", "semantic"))
	result, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("escaped migrated link should validate strictly: %#v", result.Issues)
	}
}

func TestMigrateLegacyMarkdownDescriptionTruncatesUTF8Safely(t *testing.T) {
	root := t.TempDir()
	longChinese := strings.Repeat("中国市场结构性行情持续演化", 20)
	input := "<!-- compiled_from: evt_001 -->\n# 中文页面\n\n" + longChinese + "\n"

	got, changed, err := MigrateLegacyMarkdown(root, "semantic/cjk.md", input, time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	if strings.Contains(got, `\x`) || strings.Contains(got, "�") {
		t.Fatalf("description should not contain broken UTF-8 escapes or replacement chars:\n%s", got)
	}
	if !strings.Contains(got, "中国市场结构性行情持续演化") {
		t.Fatalf("description should contain CJK text:\n%s", got)
	}
}

func TestMigrateLegacyBundleDryRunWriteBackupAndCheck(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "semantic/a.md", "<!-- compiled_from: evt_001 -->\n# A\n\nSee [[procedural/b]].\n")
	writeFile(t, root, "procedural/b.md", `---
type: procedure
title: B
description: B page
timestamp: "2026-06-16T12:00:00Z"
memory_type: procedural
source_events: [evt_002]
trust_tier: T1
---
# B
`)
	original, err := os.ReadFile(filepath.Join(root, "semantic/a.md"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := MigrateLegacyBundle(root, MigrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesChecked != 2 || result.ChangedCount() != 1 {
		t.Fatalf("unexpected dry run result: %#v", result)
	}
	afterDryRun, _ := os.ReadFile(filepath.Join(root, "semantic/a.md"))
	if string(afterDryRun) != string(original) {
		t.Fatal("dry run rewrote file")
	}

	result, err = MigrateLegacyBundle(root, MigrationOptions{Write: true, Backup: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedCount() != 1 {
		t.Fatalf("ChangedCount=%d, want 1", result.ChangedCount())
	}
	backup, err := os.ReadFile(filepath.Join(root, "semantic/a.md.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatal("backup should contain original file")
	}

	validateResult, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(validateResult.Issues) != 0 {
		t.Fatalf("written migration should validate strictly: %#v", validateResult.Issues)
	}

	result, err = MigrateLegacyBundle(root, MigrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedCount() != 0 {
		t.Fatalf("second migration should be idempotent: %#v", result.Changes)
	}
}

func validOKFPage(pageType, title, memoryType string) string {
	return `---
type: "` + pageType + `"
title: "` + title + `"
description: "` + title + ` page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: "` + memoryType + `"
source_events:
  - "evt_target"
trust_tier: "T1"
---
# ` + title + `
`
}

func TestMigrateLegacyMarkdownPlainMarkdownUnchanged(t *testing.T) {
	root := t.TempDir()
	input := "# Plain\n\nNo legacy metadata or links.\n"
	got, changed, err := MigrateLegacyMarkdown(root, "semantic/plain.md", input, time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("plain markdown without legacy markers should not change:\n%s", got)
	}
	if got != input {
		t.Fatal("unchanged file content should be preserved exactly")
	}
}
