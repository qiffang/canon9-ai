package okf

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestValidateBundleValid(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "semantic/a.md", `---
type: concept
title: "A"
description: "A page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events:
  - evt_001
trust_tier: T1
confidence: high
---
# A

See [B](../procedural/b.md "B page") and [external](https://example.com).
`)
	writeFile(t, root, "procedural/b.md", `---
type: procedure
title: "B"
description: "B page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: procedural
source_events: [evt_002]
trust_tier: T2
custom_field: tolerated
---
# B
`)

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesChecked != 2 {
		t.Fatalf("FilesChecked=%d, want 2", result.FilesChecked)
	}
	if result.ErrorCount() != 0 || result.WarningCount() != 0 {
		t.Fatalf("unexpected issues: %#v", result.Issues)
	}
}

func TestValidateBundleReportsHardErrors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "bad.md", `---
title: "Missing type"
description: "Bad page"
timestamp: "not-a-time"
memory_type: wrong
trust_tier: TX
confidence: certain
---
# Bad
`)

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount() != 5 {
		t.Fatalf("ErrorCount=%d, issues=%#v", result.ErrorCount(), result.Issues)
	}
}

func TestValidateBundleWarningsAndStrictUnknownType(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "page.md", `---
type: custom
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
---
# Page

See [missing](missing.md).
`)

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount() != 0 {
		t.Fatalf("non-strict unknown type should not error: %#v", result.Issues)
	}
	if result.WarningCount() != 5 {
		t.Fatalf("WarningCount=%d, issues=%#v", result.WarningCount(), result.Issues)
	}

	strictResult, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if strictResult.WarningCount() != 6 {
		t.Fatalf("strict WarningCount=%d, issues=%#v", strictResult.WarningCount(), strictResult.Issues)
	}
}

func TestValidateBundleAcceptsCRLFAndBlockScalars(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "semantic/a.md", "---\r\ntype: concept\r\ntitle: A\r\ndescription: |\r\n  Multi-line description.\r\ntimestamp: \"2026-06-16T12:00:00Z\"\r\nmemory_type: semantic\r\nsource_events:\r\n  - evt_001\r\ntrust_tier: T1\r\n---\r\n# A\r\n")

	result, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("valid CRLF/block-scalar frontmatter should pass: %#v", result.Issues)
	}
}

func TestValidateBundleIgnoresLinksInsideCode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "semantic/a.md", `---
type: concept
title: "A"
description: "A page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events:
  - evt_001
trust_tier: T1
---
# A

Inline `+"``[text](missing.md)``"+` should not be validated.

`+"```markdown"+`
[also missing](missing-too.md)
`+"```"+`
`)

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("code links should be ignored: %#v", result.Issues)
	}
}

func TestValidateBundleToleratesLinksOutsideBundle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "semantic/a.md", `---
type: concept
title: "A"
description: "A page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events:
  - evt_001
trust_tier: T1
---
# A

See [docs](../../docs/okf-compatibility.md).
`)

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("outside-bundle link should be tolerated: %#v", result.Issues)
	}
}

func TestValidateBundleStructuralIndexNoFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "# Index\n\n- [A](semantic/a.md)\n")
	writeFile(t, root, "semantic/a.md", `---
type: concept
title: "A"
description: "A page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events:
  - evt_001
trust_tier: T1
---
# A
`)

	result, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("structural index.md should not require frontmatter: %#v", result.Issues)
	}
}

func TestValidateBundleMissingFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "page.md", "# Page\n")

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount() != 1 {
		t.Fatalf("ErrorCount=%d, issues=%#v", result.ErrorCount(), result.Issues)
	}
	if result.WarningCount() != 4 {
		t.Fatalf("WarningCount=%d, issues=%#v", result.WarningCount(), result.Issues)
	}
}

func TestValidateBundleCategorySubIndexRelativeLinks(t *testing.T) {
	root := t.TempDir()
	// Root index with wiki-root-relative links.
	writeFile(t, root, "index.md", "# Index\n\n- [A](semantic/projects/a.md)\n")
	// Category sub-index with category-relative links (not semantic/projects/a.md).
	writeFile(t, root, "semantic/index.md", "# Semantic\n\n- [A](projects/a.md)\n")
	writeFile(t, root, "semantic/projects/a.md", `---
type: concept
title: "A"
description: "A page"
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events:
  - evt_001
trust_tier: T1
---
# A
`)

	result, err := ValidateBundle(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("category sub-index relative links should resolve: %#v", result.Issues)
	}
}

func TestValidateBundleCategorySubIndexBrokenLink(t *testing.T) {
	root := t.TempDir()
	// Category sub-index with a genuinely broken link.
	writeFile(t, root, "semantic/index.md", "# Semantic\n\n- [Missing](nonexistent/page.md)\n")

	result, err := ValidateBundle(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.WarningCount() != 1 {
		t.Fatalf("should detect 1 broken link, got issues=%#v", result.Issues)
	}
}
