package repo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Compiler generates OKF concept pages from repo scan facts and snippets.
type Compiler struct {
	outputDir string
}

// NewCompiler creates a compiler that writes pages to outputDir.
func NewCompiler(outputDir string) *Compiler {
	return &Compiler{outputDir: outputDir}
}

// LoadInput reads facts.jsonl, snippets.jsonl, and manifest.json from scanDir.
func LoadInput(scanDir string) (*CompileInput, error) {
	manifest, err := loadManifest(filepath.Join(scanDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	facts, err := loadJSONL[Fact](filepath.Join(scanDir, "facts.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("load facts: %w", err)
	}
	snippets, err := loadJSONL[Snippet](filepath.Join(scanDir, "snippets.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("load snippets: %w", err)
	}
	return &CompileInput{
		Facts:    facts,
		Snippets: snippets,
		Manifest: *manifest,
	}, nil
}

// Compile generates concept pages from the input facts and snippets.
// It groups facts by package, generates per-package concept pages with
// source-grounded summaries, core code snippets, and source anchors.
//
// This is a deterministic compiler — no LLM calls.
func (c *Compiler) Compile(input *CompileInput) (*CompileOutput, error) {
	if err := os.MkdirAll(c.outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	snippetsByFact := make(map[string][]Snippet)
	for _, s := range input.Snippets {
		snippetsByFact[s.FactID] = append(snippetsByFact[s.FactID], s)
	}

	pkgFacts := make(map[string][]Fact)
	for _, f := range input.Facts {
		pkg := f.Package
		if pkg == "" {
			pkg = packageFromPath(f.Path)
		}
		pkgFacts[pkg] = append(pkgFacts[pkg], f)
	}

	var pages []ConceptPage
	for pkg, facts := range pkgFacts {
		page := c.compilePackage(pkg, facts, snippetsByFact, input.Manifest)
		pages = append(pages, page)
	}

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Slug < pages[j].Slug
	})

	for _, page := range pages {
		pagePath := filepath.Join(c.outputDir, page.Slug)
		if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
			return nil, fmt.Errorf("create page dir: %w", err)
		}
		if err := os.WriteFile(pagePath, []byte(page.Content), 0644); err != nil {
			return nil, fmt.Errorf("write page %s: %w", page.Slug, err)
		}
	}

	return &CompileOutput{
		Pages:     pages,
		Manifest:  input.Manifest,
		PageCount: len(pages),
	}, nil
}

func (c *Compiler) compilePackage(pkg string, facts []Fact, snippetsByFact map[string][]Snippet, manifest Manifest) ConceptPage {
	sort.Slice(facts, func(i, j int) bool {
		pi, pj := kindPriority(facts[i].Kind), kindPriority(facts[j].Kind)
		if pi != pj {
			return pi < pj
		}
		return factName(facts[i]) < factName(facts[j])
	})

	slug := packageSlug(pkg, manifest.Scope)
	title := fmt.Sprintf("Package %s", pkg)
	commitSHA := manifest.HeadSHA

	var anchors []string
	var b strings.Builder

	// YAML frontmatter — deterministic (no time.Now).
	b.WriteString("---\n")
	b.WriteString("type: concept\n")
	b.WriteString(fmt.Sprintf("title: %q\n", title))
	b.WriteString("memory_type: semantic\n")
	b.WriteString(fmt.Sprintf("commit_sha: %q\n", commitSHA))
	b.WriteString(fmt.Sprintf("repo: %q\n", manifest.Repo))
	b.WriteString(fmt.Sprintf("scope: %q\n", manifest.Scope))
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# %s\n\n", title))

	// Source files.
	files := uniqueFiles(facts)
	b.WriteString("## Source Files\n\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("- `%s` (commit: `%.8s`)\n", f, commitSHA))
	}
	b.WriteString("\n")

	// Interfaces.
	interfaces := filterKind(facts, "interface")
	if len(interfaces) > 0 {
		b.WriteString("## Interfaces\n\n")
		for _, f := range interfaces {
			anchor := factAnchor(f)
			anchors = append(anchors, anchor)
			b.WriteString(fmt.Sprintf("### %s\n\n", factName(f)))
			if f.Doc != "" {
				b.WriteString(firstSentence(f.Doc) + "\n\n")
			}
			b.WriteString(fmt.Sprintf("- **Source**: `%s:%d` [%s]\n", f.Path, f.Line, anchor))
			writeSnippets(&b, f.ID, snippetsByFact)
			b.WriteString("\n")
		}
	}

	// Types.
	types := filterKind(facts, "type")
	if len(types) > 0 {
		b.WriteString("## Types\n\n")
		for _, f := range types {
			anchor := factAnchor(f)
			anchors = append(anchors, anchor)
			b.WriteString(fmt.Sprintf("### %s\n\n", factName(f)))
			if f.Doc != "" {
				b.WriteString(firstSentence(f.Doc) + "\n\n")
			}
			if f.Signature != "" {
				b.WriteString(fmt.Sprintf("- **Signature**: `%s`\n", f.Signature))
			}
			b.WriteString(fmt.Sprintf("- **Source**: `%s:%d` [%s]\n", f.Path, f.Line, anchor))
			if len(f.Implements) > 0 {
				b.WriteString(fmt.Sprintf("- **Implements**: %s\n", strings.Join(f.Implements, ", ")))
			}
			writeSnippets(&b, f.ID, snippetsByFact)

			methods := filterMethods(facts, factName(f))
			if len(methods) > 0 {
				b.WriteString("\n**Methods:**\n\n")
				for _, m := range methods {
					manchor := factAnchor(m)
					anchors = append(anchors, manchor)
					sig := m.Signature
					if sig == "" {
						sig = factName(m)
					}
					b.WriteString(fmt.Sprintf("- `%s` — `%s:%d` [%s]\n", sig, m.Path, m.Line, manchor))
					if m.Doc != "" {
						b.WriteString(fmt.Sprintf("  %s\n", firstSentence(m.Doc)))
					}
				}
			}
			b.WriteString("\n")
		}
	}

	// Functions.
	funcs := filterKind(facts, "func")
	if len(funcs) > 0 {
		b.WriteString("## Functions\n\n")
		for _, f := range funcs {
			anchor := factAnchor(f)
			anchors = append(anchors, anchor)
			sig := f.Signature
			if sig == "" {
				sig = factName(f)
			}
			b.WriteString(fmt.Sprintf("### %s\n\n", factName(f)))
			if f.Doc != "" {
				b.WriteString(firstSentence(f.Doc) + "\n\n")
			}
			b.WriteString(fmt.Sprintf("- **Signature**: `%s`\n", sig))
			b.WriteString(fmt.Sprintf("- **Source**: `%s:%d` [%s]\n", f.Path, f.Line, anchor))
			writeSnippets(&b, f.ID, snippetsByFact)
			b.WriteString("\n")
		}
	}

	// Constants and vars.
	consts := filterKind(facts, "const")
	vars := filterKind(facts, "var")
	if len(consts) > 0 || len(vars) > 0 {
		b.WriteString("## Constants & Variables\n\n")
		for _, f := range append(consts, vars...) {
			anchor := factAnchor(f)
			anchors = append(anchors, anchor)
			b.WriteString(fmt.Sprintf("- `%s` (%s) — `%s:%d` [%s]\n", factName(f), f.Kind, f.Path, f.Line, anchor))
		}
		b.WriteString("\n")
	}

	// Tests.
	tests := filterKind(facts, "test")
	if len(tests) > 0 {
		b.WriteString("## Tests\n\n")
		for _, f := range tests {
			anchor := factAnchor(f)
			anchors = append(anchors, anchor)
			target := f.TestTarget
			if target == "" {
				target = f.Target
			}
			if target == "" {
				target = "(package-level)"
			}
			b.WriteString(fmt.Sprintf("- `%s` → tests `%s` — `%s:%d` [%s]\n", factName(f), target, f.Path, f.Line, anchor))
		}
		b.WriteString("\n")
	}

	// Imports/Dependencies.
	imports := filterKind(facts, "import")
	if len(imports) > 0 {
		b.WriteString("## Dependencies\n\n")
		for _, f := range imports {
			if len(f.Imports) > 0 {
				b.WriteString(fmt.Sprintf("File `%s` imports:\n", f.Path))
				for _, imp := range f.Imports {
					b.WriteString(fmt.Sprintf("- `%s`\n", imp))
				}
				b.WriteString("\n")
			}
		}
	}

	desc := fmt.Sprintf("Go package %s — %d types, %d funcs, %d tests", pkg,
		len(types), len(funcs), len(tests))

	return ConceptPage{
		Slug:          slug,
		Title:         title,
		Description:   desc,
		Content:       b.String(),
		SourceAnchors: anchors,
	}
}

// --- Helpers ---

func factName(f Fact) string {
	if f.Name != "" {
		return f.Name
	}
	return f.Symbol
}

func factAnchor(f Fact) string {
	if f.Anchor != "" {
		return f.Anchor
	}
	return f.ID
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		return s[:idx+1]
	}
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func packageFromPath(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return "(root)"
	}
	return dir
}

func loadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func loadJSONL[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var items []T
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("unmarshal line: %w", err)
		}
		items = append(items, item)
	}
	return items, scanner.Err()
}

func kindPriority(kind string) int {
	switch kind {
	case "package":
		return 0
	case "interface":
		return 1
	case "type":
		return 2
	case "func":
		return 3
	case "method":
		return 4
	case "test":
		return 5
	case "const":
		return 6
	case "var":
		return 7
	case "import":
		return 8
	default:
		return 9
	}
}

func packageSlug(pkg, scope string) string {
	name := pkg
	if scope != "" && strings.Contains(pkg, scope) {
		idx := strings.Index(pkg, scope)
		name = pkg[idx:]
	}
	parts := strings.Split(name, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	slug := strings.Join(parts, "-")
	slug = strings.ReplaceAll(slug, ".", "-")
	return fmt.Sprintf("semantic/repo/%s.md", slug)
}

func uniqueFiles(facts []Fact) []string {
	seen := make(map[string]bool)
	var files []string
	for _, f := range facts {
		if f.Path != "" && !seen[f.Path] {
			seen[f.Path] = true
			files = append(files, f.Path)
		}
	}
	sort.Strings(files)
	return files
}

func filterKind(facts []Fact, kind string) []Fact {
	var out []Fact
	for _, f := range facts {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func filterMethods(facts []Fact, typeName string) []Fact {
	var out []Fact
	for _, f := range facts {
		if f.Kind == "method" {
			recv := strings.TrimPrefix(f.Receiver, "*")
			if recv == typeName {
				out = append(out, f)
			}
		}
	}
	return out
}

func writeSnippets(b *strings.Builder, factID string, snippetsByFact map[string][]Snippet) {
	snippets := snippetsByFact[factID]
	if len(snippets) == 0 {
		return
	}
	for _, s := range snippets {
		lang := "go"
		if s.Language != "" {
			lang = s.Language
		}
		code := s.Content
		if code == "" {
			code = "(snippet content not available)"
		}
		anchor := s.Anchor
		if anchor == "" {
			anchor = s.ID
		}
		b.WriteString(fmt.Sprintf("\n```%s\n// Source: %s:%d-%d [%s]\n%s\n```\n",
			lang, s.Path, s.StartLine, s.EndLine, anchor, code))
	}
}
