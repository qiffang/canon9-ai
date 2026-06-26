package okf

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type MigrationOptions struct {
	Write  bool
	Backup bool
	Now    func() time.Time
}

type MigrationChange struct {
	Path string
}

type MigrationResult struct {
	FilesChecked int
	Changes      []MigrationChange
}

func (r MigrationResult) ChangedCount() int {
	return len(r.Changes)
}

func MigrateLegacyBundle(root string, opts MigrationOptions) (MigrationResult, error) {
	info, err := os.Stat(root)
	if err != nil {
		return MigrationResult{}, err
	}
	if !info.IsDir() {
		return MigrationResult{}, fmt.Errorf("%s is not a directory", root)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return MigrationResult{}, err
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}

	var result MigrationResult
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		changed, err := migrateFile(root, path, rel, opts)
		if err != nil {
			return err
		}
		result.FilesChecked++
		if changed {
			result.Changes = append(result.Changes, MigrationChange{Path: rel})
		}
		return nil
	})
	if err != nil {
		return MigrationResult{}, err
	}
	sort.Slice(result.Changes, func(i, j int) bool { return result.Changes[i].Path < result.Changes[j].Path })
	return result, nil
}

func migrateFile(root, path, rel string, opts MigrationOptions) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	migrated, changed, err := MigrateLegacyMarkdown(root, rel, string(data), info.ModTime().UTC(), opts.Now())
	if err != nil {
		return false, fmt.Errorf("%s: %w", rel, err)
	}
	if !changed || !opts.Write {
		return changed, nil
	}
	if opts.Backup {
		backup := path + ".bak"
		if _, err := os.Stat(backup); err == nil {
			return false, fmt.Errorf("%s: backup already exists: %s", rel, filepath.Base(backup))
		} else if !os.IsNotExist(err) {
			return false, err
		}
		if err := os.WriteFile(backup, data, info.Mode().Perm()); err != nil {
			return false, fmt.Errorf("write backup: %w", err)
		}
	}
	return true, writeFileAtomic(path, []byte(migrated), info.Mode().Perm())
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func MigrateLegacyMarkdown(root, rel, content string, modTime, now time.Time) (string, bool, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	frontmatter, body, hasFrontmatter, err := parseFrontmatter(content)
	if err != nil {
		return "", false, err
	}
	_ = frontmatter

	if hasFrontmatter {
		convertedBody := convertWikiLinks(root, rel, body)
		if convertedBody == body {
			return content, false, nil
		}
		prefix, _, err := splitFrontmatterRaw(content)
		if err != nil {
			return "", false, err
		}
		return prefix + convertedBody, true, nil
	}

	legacy, stripped := extractLegacyComments(content)
	convertedBody := convertWikiLinks(root, rel, stripped)
	if len(legacy) == 0 && convertedBody == stripped {
		return content, false, nil
	}
	if isStructuralIndex(rel) {
		changed := convertedBody != content
		return convertedBody, changed, nil
	}

	fields := frontmatterFromLegacy(rel, legacy, convertedBody, modTime, now)
	migrated := renderFrontmatter(fields) + "\n" + strings.TrimLeft(convertedBody, "\n")
	return migrated, migrated != content, nil
}

type frontmatterField struct {
	Key    string
	Values []string
}

func frontmatterFromLegacy(rel string, legacy map[string][]string, body string, modTime, now time.Time) []frontmatterField {
	pageType := firstLegacy(legacy, "type")
	if pageType == "" {
		pageType = inferTypeFromPath(rel)
	}
	title := firstLegacy(legacy, "title")
	if title == "" {
		title = extractTitle(body, rel)
	}
	description := firstLegacy(legacy, "description")
	if description == "" {
		description = extractDescription(body, title)
	}
	timestamp := firstLegacy(legacy, "last_compiled", "timestamp")
	if timestamp == "" {
		if !modTime.IsZero() {
			timestamp = modTime.UTC().Format(time.RFC3339)
		} else {
			timestamp = now.UTC().Format(time.RFC3339)
		}
	}
	memoryType := firstLegacy(legacy, "memory_type")
	if memoryType == "" {
		memoryType = inferMemoryTypeFromPath(rel)
	}
	sourceEvents := legacyValues(legacy, "compiled_from", "source_events")
	if len(sourceEvents) == 0 {
		sourceEvents = []string{"legacy:" + rel}
	}
	trustTier := normalizeTrustTier(firstLegacy(legacy, "trust_tier"))
	if trustTier == "" {
		trustTier = "T3"
	}

	fields := []frontmatterField{
		{Key: "type", Values: []string{pageType}},
		{Key: "title", Values: []string{title}},
		{Key: "description", Values: []string{description}},
		{Key: "timestamp", Values: []string{timestamp}},
		{Key: "memory_type", Values: []string{memoryType}},
		{Key: "source_events", Values: sourceEvents},
		{Key: "trust_tier", Values: []string{trustTier}},
	}
	if confidence := firstLegacy(legacy, "confidence"); confidence != "" {
		fields = append(fields, frontmatterField{Key: "confidence", Values: []string{confidence}})
	}
	return fields
}

func renderFrontmatter(fields []frontmatterField) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, field := range fields {
		if len(field.Values) == 0 {
			continue
		}
		if len(field.Values) == 1 {
			b.WriteString(field.Key)
			b.WriteString(": ")
			b.WriteString(yamlScalar(field.Values[0]))
			b.WriteByte('\n')
			continue
		}
		b.WriteString(field.Key)
		b.WriteString(":\n")
		for _, value := range field.Values {
			b.WriteString("  - ")
			b.WriteString(yamlScalar(value))
			b.WriteByte('\n')
		}
	}
	b.WriteString("---\n")
	return b.String()
}

func yamlScalar(value string) string {
	return strconv.Quote(value)
}

var legacyCommentRE = regexp.MustCompile(`^<!--\s*([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*?)\s*-->\s*$`)

func extractLegacyComments(content string) (map[string][]string, string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	legacy := make(map[string][]string)
	keep := make([]string, 0, len(lines))
	inHeader := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inHeader && trimmed == "" {
			continue
		}
		if inHeader {
			match := legacyCommentRE.FindStringSubmatch(trimmed)
			if len(match) == 3 {
				key := normalizeLegacyKey(match[1])
				if isLegacyMetadataKey(key) {
					legacy[key] = append(legacy[key], splitLegacyValues(match[2])...)
					continue
				}
			}
			inHeader = false
		}
		keep = append(keep, line)
	}
	return legacy, strings.Join(keep, "\n")
}

func normalizeLegacyKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(key, "-", "_"))
}

func isLegacyMetadataKey(key string) bool {
	switch key {
	case "compiled_from", "source_events", "last_compiled", "timestamp", "memory_type", "trust_tier", "confidence", "type", "title", "description":
		return true
	default:
		return false
	}
}

func splitLegacyValues(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' })
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func firstLegacy(legacy map[string][]string, keys ...string) string {
	for _, key := range keys {
		values := legacy[key]
		if len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
	}
	return ""
}

func legacyValues(legacy map[string][]string, keys ...string) []string {
	seen := make(map[string]bool)
	var values []string
	for _, key := range keys {
		for _, value := range legacy[key] {
			value = strings.TrimSpace(value)
			if value != "" && !seen[value] {
				seen[value] = true
				values = append(values, value)
			}
		}
	}
	return values
}

func normalizeTrustTier(value string) string {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "T") {
		return value
	}
	return "T" + value
}

func inferTypeFromPath(rel string) string {
	memoryType := inferMemoryTypeFromPath(rel)
	switch memoryType {
	case "procedural":
		return "procedure"
	case "episodic", "prospective":
		return "event"
	default:
		if strings.Contains(rel, "/people/") || strings.Contains(rel, "/person/") {
			return "person"
		}
		return "concept"
	}
}

func inferMemoryTypeFromPath(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) > 0 && parts[0] == "archive" && len(parts) > 1 {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return "semantic"
	}
	switch parts[0] {
	case "semantic", "episodic", "procedural", "prospective":
		return parts[0]
	default:
		return "semantic"
	}
}

func extractTitle(body, rel string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
	}
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.Title(base)
}

func extractDescription(body, title string) string {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if isFenceDelimiter(trimmed) {
			inFence = !inFence
			continue
		}
		if inFence || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		trimmed = strings.Join(strings.Fields(trimmed), " ")
		if trimmed == "" {
			continue
		}
		return truncateRunes(trimmed, 160)
	}
	return title
}

func isFenceDelimiter(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func splitFrontmatterRaw(content string) (string, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", content, nil
	}
	lines := strings.Split(content, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			prefix := strings.Join(lines[:i+1], "\n") + "\n"
			body := strings.Join(lines[i+1:], "\n")
			return prefix, body, nil
		}
	}
	return "", "", fmt.Errorf("unterminated YAML frontmatter")
}

var wikiLinkRE = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)

func convertWikiLinks(root, rel, body string) string {
	var out []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		out = append(out, convertWikiLinksOutsideInlineCode(root, rel, line))
	}
	return strings.Join(out, "\n")
}

func convertWikiLinksOutsideInlineCode(root, rel, line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '`' {
			run := countBackticks(line[i:])
			end := findMatchingBackticks(line, i+run, run)
			if end < 0 {
				b.WriteString(line[i:])
				break
			}
			b.WriteString(line[i : end+run])
			i = end + run
			continue
		}
		next := strings.IndexByte(line[i:], '`')
		segmentEnd := len(line)
		if next >= 0 {
			segmentEnd = i + next
		}
		b.WriteString(convertWikiLinksInText(root, rel, line[i:segmentEnd]))
		i = segmentEnd
	}
	return b.String()
}

func countBackticks(s string) int {
	count := 0
	for count < len(s) && s[count] == '`' {
		count++
	}
	return count
}

func findMatchingBackticks(line string, start, run int) int {
	needle := strings.Repeat("`", run)
	idx := strings.Index(line[start:], needle)
	if idx < 0 {
		return -1
	}
	return start + idx
}

func convertWikiLinksInText(root, rel, text string) string {
	return wikiLinkRE.ReplaceAllStringFunc(text, func(match string) string {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "[["), "]]"))
		if inner == "" {
			return match
		}
		target, label, _ := strings.Cut(inner, "|")
		target = strings.TrimSpace(target)
		label = strings.TrimSpace(label)
		if target == "" {
			return match
		}
		if label == "" {
			label = labelFromWikiTarget(target)
		}
		href := markdownHrefForWikiTarget(root, rel, target)
		return "[" + escapeMarkdownLinkText(label) + "](" + href + ")"
	})
}

func markdownHrefForWikiTarget(root, rel, target string) string {
	pathPart, suffix := splitLinkSuffix(target)
	pathPart = strings.TrimSpace(pathPart)
	if pathPart == "" || isExternalLink(pathPart) {
		return target
	}
	if !strings.HasSuffix(filepath.Base(pathPart), ".md") {
		pathPart += ".md"
	}
	pathPart = strings.TrimPrefix(pathPart, "/")
	fromDir := filepath.Dir(filepath.Join(root, filepath.FromSlash(rel)))
	var href string
	if isBundleRootPath(pathPart) {
		targetPath := filepath.Join(root, filepath.FromSlash(pathPart))
		if relPath, err := filepath.Rel(fromDir, targetPath); err == nil {
			href = filepath.ToSlash(relPath)
		}
	}
	if href == "" {
		href = filepath.ToSlash(filepath.Clean(filepath.FromSlash(pathPart)))
	}
	if href == "." {
		href = filepath.Base(pathPart)
	}
	return escapeMarkdownDestination(href + suffix)
}

func escapeMarkdownDestination(href string) string {
	var b strings.Builder
	for _, r := range href {
		switch r {
		case ' ':
			b.WriteString("%20")
		case '(':
			b.WriteString("%28")
		case ')':
			b.WriteString("%29")
		case '<':
			b.WriteString("%3C")
		case '>':
			b.WriteString("%3E")
		case '\\':
			b.WriteString("%5C")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitLinkSuffix(target string) (string, string) {
	for _, sep := range []string{"#", "?"} {
		if idx := strings.Index(target, sep); idx >= 0 {
			return target[:idx], target[idx:]
		}
	}
	return target, ""
}

func isBundleRootPath(path string) bool {
	first := strings.Split(filepath.ToSlash(strings.TrimPrefix(path, "/")), "/")[0]
	switch first {
	case "semantic", "episodic", "procedural", "prospective", "archive":
		return true
	default:
		return false
	}
}

func labelFromWikiTarget(target string) string {
	pathPart, _ := splitLinkSuffix(target)
	base := strings.TrimSuffix(filepath.Base(pathPart), filepath.Ext(pathPart))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if base == "" {
		return target
	}
	return strings.Title(base)
}

func escapeMarkdownLinkText(label string) string {
	label = strings.ReplaceAll(label, "[", "\\[")
	label = strings.ReplaceAll(label, "]", "\\]")
	return label
}
