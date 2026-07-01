package repo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func Scan(opts ScanOptions) (*Bundle, error) {
	repoRoot, err := gitOutput(opts.RepoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve git root: %w", err)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	head, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}
	head = strings.TrimSpace(head)
	scope, err := cleanScope(opts.Scope)
	if err != nil {
		return nil, err
	}
	repoName := detectRepoName(repoRoot)

	files, deleted, err := scanFileSet(repoRoot, scope, opts.Since)
	if err != nil {
		return nil, err
	}

	var facts []Fact
	var snippets []Snippet
	fileManifests := make([]FileManifest, 0, len(files)+len(deleted))
	for _, rel := range files {
		if filepath.Ext(rel) != ".go" {
			continue
		}
		fullPath := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		fileHash := hexSHA256(data)
		fileFacts, fileSnippets, err := scanGoFile(repoName, head, rel, fileHash, data)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fileFacts...)
		snippets = append(snippets, fileSnippets...)
		fileManifests = append(fileManifests, fileManifest(rel, "present", fileHash, fileFacts, fileSnippets))
	}
	for _, rel := range deleted {
		if filepath.Ext(rel) != ".go" {
			continue
		}
		fact := Fact{
			Kind:      "file",
			Status:    "deleted",
			Repo:      repoName,
			CommitSHA: head,
			Path:      rel,
			Symbol:    rel,
		}
		fact.Anchor = sourceAnchor(fact)
		fact.ID = factID(fact)
		facts = append(facts, fact)
		fileManifests = append(fileManifests, FileManifest{
			Path:    rel,
			Status:  "deleted",
			FactIDs: []string{fact.ID},
			Symbols: []string{rel},
			Anchors: []string{fact.Anchor},
		})
	}

	sortFacts(facts)
	sortSnippets(snippets)
	sort.Slice(fileManifests, func(i, j int) bool {
		return fileManifests[i].Path < fileManifests[j].Path
	})
	fileHashes := make(map[string]string, len(fileManifests))
	for _, file := range fileManifests {
		if file.Hash != "" {
			fileHashes[file.Path] = file.Hash
		}
	}

	manifest := Manifest{
		Version:    FactsVersion,
		Repo:       repoName,
		Scope:      scope,
		BaseSHA:    strings.TrimSpace(opts.Since),
		HeadSHA:    head,
		FileHash:   fileHashes,
		FactIDs:    factIDs(facts),
		SnippetIDs: snippetIDs(snippets),
		Files:      fileManifests,
		Changed:    files,
		Deleted:    deleted,
	}
	if manifest.BaseSHA == "" {
		manifest.Changed = nil
		manifest.Deleted = nil
	}
	return &Bundle{Manifest: manifest, Facts: facts, Snippets: snippets}, nil
}

func WriteBundle(bundle *Bundle, outDir string) error {
	if outDir == "" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(bundle)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	manifestData, err := json.MarshalIndent(bundle.Manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), append(manifestData, '\n'), 0o644); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(outDir, "facts.jsonl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	for _, fact := range bundle.Facts {
		if err := enc.Encode(fact); err != nil {
			return err
		}
	}
	snippetFile, err := os.OpenFile(filepath.Join(outDir, "snippets.jsonl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer snippetFile.Close()
	snippetEnc := json.NewEncoder(snippetFile)
	for _, snippet := range bundle.Snippets {
		if err := snippetEnc.Encode(snippet); err != nil {
			return err
		}
	}
	return nil
}

func scanGoFile(repoName, commit, rel, fileHash string, data []byte) ([]Fact, []Snippet, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, data, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	var facts []Fact
	var snippets []Snippet
	add := func(f Fact) Fact {
		f.Status = "present"
		f.Repo = repoName
		f.CommitSHA = commit
		f.Path = rel
		f.Package = file.Name.Name
		f.FileHash = fileHash
		f.Anchor = sourceAnchor(f)
		f.ID = factID(f)
		facts = append(facts, f)
		return f
	}
	importTargets := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		target, _ := strconv.Unquote(spec.Path.Value)
		importTargets = append(importTargets, target)
	}
	sort.Strings(importTargets)
	add(Fact{
		Kind:    "package",
		Line:    fset.Position(file.Package).Line,
		Symbol:  file.Name.Name,
		Name:    file.Name.Name,
		Imports: importTargets,
	})
	for _, spec := range file.Imports {
		target, _ := strconv.Unquote(spec.Path.Value)
		pos := fset.Position(spec.Pos())
		add(Fact{
			Kind:    "import",
			Line:    pos.Line,
			Symbol:  target,
			Name:    target,
			Target:  target,
			Imports: []string{target},
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, spec := range d.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					kind := "type"
					if _, ok := typeSpec.Type.(*ast.InterfaceType); ok {
						kind = "interface"
					}
					fact := add(Fact{
						Kind:      kind,
						Line:      fset.Position(typeSpec.Pos()).Line,
						EndLine:   fset.Position(typeSpec.End()).Line,
						Symbol:    typeSpec.Name.Name,
						Name:      typeSpec.Name.Name,
						Signature: nodeString(fset, data, typeSpec.Type.Pos(), typeSpec.Type.End()),
						Doc:       docText(typeSpec.Doc, d.Doc),
						Exported:  ast.IsExported(typeSpec.Name.Name),
					})
					snippets = append(snippets, snippetForRange(fset, data, fact, fileHash, d.Pos(), d.End()))
				}
			case token.CONST, token.VAR:
				kind := strings.ToLower(d.Tok.String())
				for _, spec := range d.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range valueSpec.Names {
						fact := add(Fact{
							Kind:      kind,
							Line:      fset.Position(name.Pos()).Line,
							EndLine:   fset.Position(valueSpec.End()).Line,
							Symbol:    name.Name,
							Name:      name.Name,
							Signature: nodeString(fset, data, valueSpec.Pos(), valueSpec.End()),
							Doc:       docText(valueSpec.Doc, d.Doc),
							Exported:  ast.IsExported(name.Name),
						})
						snippets = append(snippets, snippetForRange(fset, data, fact, fileHash, d.Pos(), d.End()))
					}
				}
			}
		case *ast.FuncDecl:
			kind := "func"
			receiver := ""
			symbol := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				receiverSymbol := nodeString(fset, data, d.Recv.List[0].Type.Pos(), d.Recv.List[0].Type.End())
				receiver = receiverName(receiverSymbol)
				symbol = receiverSymbol + "." + d.Name.Name
			} else if isGoTest(rel, d.Name.Name) {
				kind = "test"
			}
			fact := add(Fact{
				Kind:       kind,
				Line:       fset.Position(d.Pos()).Line,
				EndLine:    fset.Position(d.End()).Line,
				Symbol:     symbol,
				Name:       d.Name.Name,
				Receiver:   receiver,
				Signature:  funcSignature(fset, data, d),
				Doc:        docText(d.Doc),
				Exported:   ast.IsExported(d.Name.Name),
				TestTarget: testTarget(d.Name.Name),
			})
			snippets = append(snippets, snippetForRange(fset, data, fact, fileHash, d.Pos(), d.End()))
		}
	}
	return facts, snippets, nil
}

func scanFileSet(repoRoot, scope, since string) ([]string, []string, error) {
	if strings.TrimSpace(since) == "" {
		out, err := gitOutputRaw(repoRoot, "ls-files", "-z", "--", scope)
		if err != nil {
			return nil, nil, fmt.Errorf("list files: %w", err)
		}
		return splitNULPaths(out), nil, nil
	}
	out, err := gitOutputRaw(repoRoot, "diff", "--name-status", "-z", "--find-renames", since+"..HEAD", "--", scope)
	if err != nil {
		return nil, nil, fmt.Errorf("diff files: %w", err)
	}
	changed := map[string]bool{}
	deleted := map[string]bool{}
	fields := splitNULFields(out)
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			continue
		}
		switch status[0] {
		case 'D':
			if i >= len(fields) {
				return nil, nil, fmt.Errorf("malformed git diff entry for status %q", status)
			}
			deleted[filepath.ToSlash(fields[i])] = true
			i++
		case 'R', 'C':
			if i+1 >= len(fields) {
				return nil, nil, fmt.Errorf("malformed git diff entry for status %q", status)
			}
			deleted[filepath.ToSlash(fields[i])] = true
			changed[filepath.ToSlash(fields[i+1])] = true
			i += 2
		default:
			if i >= len(fields) {
				return nil, nil, fmt.Errorf("malformed git diff entry for status %q", status)
			}
			changed[filepath.ToSlash(fields[i])] = true
			i++
		}
	}
	return sortedKeys(changed), sortedKeys(deleted), nil
}

func cleanScope(scope string) (string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "."
	}
	scope = filepath.ToSlash(filepath.Clean(scope))
	if filepath.IsAbs(scope) || scope == ".." || strings.HasPrefix(scope, "../") {
		return "", fmt.Errorf("scope must be repo-relative: %s", scope)
	}
	if scope == "." {
		return ".", nil
	}
	return strings.TrimPrefix(scope, "./"), nil
}

func gitOutput(dir string, args ...string) (string, error) {
	out, err := gitOutputRaw(dir, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitOutputRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func detectRepoName(repoRoot string) string {
	out, err := gitOutput(repoRoot, "config", "--get", "remote.origin.url")
	if err == nil {
		remote := strings.TrimSpace(out)
		remote = strings.TrimSuffix(remote, ".git")
		remote = strings.TrimRight(remote, "/")
		if slash := strings.LastIndex(remote, "/"); slash >= 0 && slash < len(remote)-1 {
			return remote[slash+1:]
		}
		if colon := strings.LastIndex(remote, ":"); colon >= 0 && colon < len(remote)-1 {
			return remote[colon+1:]
		}
	}
	return filepath.Base(repoRoot)
}

func fileManifest(path, status, hash string, facts []Fact, snippets []Snippet) FileManifest {
	ids := make([]string, 0, len(facts))
	snippetIDs := make([]string, 0, len(snippets))
	symbols := make([]string, 0, len(facts))
	anchors := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.ID)
		if fact.Symbol != "" {
			symbols = append(symbols, fact.Symbol)
		}
		if fact.Anchor != "" {
			anchors = append(anchors, fact.Anchor)
		}
	}
	for _, snippet := range snippets {
		snippetIDs = append(snippetIDs, snippet.ID)
	}
	sort.Strings(ids)
	sort.Strings(snippetIDs)
	sort.Strings(symbols)
	sort.Strings(anchors)
	return FileManifest{Path: path, Status: status, Hash: hash, FactIDs: ids, SnippetIDs: snippetIDs, Symbols: symbols, Anchors: anchors}
}

func factID(f Fact) string {
	key := strings.Join([]string{f.Repo, f.CommitSHA, f.Path, f.Kind, f.Symbol, strconv.Itoa(f.Line), f.Status}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return "fact_" + hex.EncodeToString(sum[:8])
}

func snippetForRange(fset *token.FileSet, data []byte, f Fact, fileHash string, start, end token.Pos) Snippet {
	startLine := f.Line
	endLine := f.EndLine
	if pos := fset.Position(start); pos.IsValid() {
		startLine = pos.Line
	}
	if pos := fset.Position(end); pos.IsValid() {
		endLine = pos.Line
	}
	return snippetForFact(f, fileHash, startLine, endLine, nodeString(fset, data, start, end))
}

func snippetForFact(f Fact, fileHash string, startLine, endLine int, code string) Snippet {
	snippet := Snippet{
		FactID:    f.ID,
		Kind:      f.Kind,
		Repo:      f.Repo,
		CommitSHA: f.CommitSHA,
		Path:      f.Path,
		StartLine: startLine,
		EndLine:   endLine,
		Symbol:    f.Symbol,
		Anchor:    f.Anchor,
		Language:  "go",
		FileHash:  fileHash,
		Content:   code,
	}
	snippet.ID = snippetID(snippet)
	return snippet
}

func snippetID(s Snippet) string {
	key := strings.Join([]string{s.Repo, s.CommitSHA, s.Path, s.Kind, s.Symbol, strconv.Itoa(s.StartLine), strconv.Itoa(s.EndLine), s.FileHash}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return "snippet_" + hex.EncodeToString(sum[:8])
}

func sourceAnchor(f Fact) string {
	if f.Status == "deleted" {
		return strings.Join([]string{f.Repo, f.CommitSHA, f.Path, "deleted"}, ":")
	}
	return fmt.Sprintf("%s:%s:%s:%d:%s", f.Repo, f.CommitSHA, f.Path, f.Line, f.Symbol)
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nodeString(fset *token.FileSet, data []byte, start, end token.Pos) string {
	file := fset.File(start)
	if file == nil {
		return ""
	}
	startOffset := file.Offset(start)
	endOffset := file.Offset(end)
	if startOffset < 0 || endOffset > len(data) || startOffset > endOffset {
		return ""
	}
	return strings.TrimSpace(string(bytes.TrimSpace(data[startOffset:endOffset])))
}

func funcSignature(fset *token.FileSet, data []byte, decl *ast.FuncDecl) string {
	return nodeString(fset, data, decl.Pos(), decl.Type.End())
}

func docText(groups ...*ast.CommentGroup) string {
	for _, group := range groups {
		if group == nil {
			continue
		}
		text := strings.TrimSpace(group.Text())
		if text != "" {
			return text
		}
	}
	return ""
}

func receiverName(receiver string) string {
	receiver = strings.TrimSpace(receiver)
	receiver = strings.TrimPrefix(receiver, "*")
	if dot := strings.LastIndex(receiver, "."); dot >= 0 && dot < len(receiver)-1 {
		return receiver[dot+1:]
	}
	return receiver
}

func testTarget(name string) string {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return name[len(prefix):]
		}
	}
	return ""
}

func isGoTest(path, name string) bool {
	if !strings.HasSuffix(path, "_test.go") {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Fuzz")
}

func splitNULPaths(out []byte) []string {
	fields := splitNULFields(out)
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			paths = append(paths, filepath.ToSlash(field))
		}
	}
	sort.Strings(paths)
	return paths
}

func splitNULFields(out []byte) []string {
	raw := bytes.Split(out, []byte{0})
	fields := make([]string, 0, len(raw))
	for _, field := range raw {
		if len(field) == 0 {
			continue
		}
		fields = append(fields, string(field))
	}
	return fields
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func factIDs(facts []Fact) []string {
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.ID)
	}
	sort.Strings(ids)
	return ids
}

func snippetIDs(snippets []Snippet) []string {
	ids := make([]string, 0, len(snippets))
	for _, snippet := range snippets {
		ids = append(ids, snippet.ID)
	}
	sort.Strings(ids)
	return ids
}

func sortFacts(facts []Fact) {
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Path != facts[j].Path {
			return facts[i].Path < facts[j].Path
		}
		if facts[i].Line != facts[j].Line {
			return facts[i].Line < facts[j].Line
		}
		if facts[i].Kind != facts[j].Kind {
			return facts[i].Kind < facts[j].Kind
		}
		return facts[i].Symbol < facts[j].Symbol
	})
}

func sortSnippets(snippets []Snippet) {
	sort.Slice(snippets, func(i, j int) bool {
		if snippets[i].Path != snippets[j].Path {
			return snippets[i].Path < snippets[j].Path
		}
		if snippets[i].StartLine != snippets[j].StartLine {
			return snippets[i].StartLine < snippets[j].StartLine
		}
		if snippets[i].Kind != snippets[j].Kind {
			return snippets[i].Kind < snippets[j].Kind
		}
		return snippets[i].Symbol < snippets[j].Symbol
	})
}
