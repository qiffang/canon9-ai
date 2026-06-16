package okf

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Issue struct {
	Severity Severity
	Path     string
	Message  string
}

type Result struct {
	FilesChecked int
	Issues       []Issue
}

func (r Result) ErrorCount() int {
	count := 0
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			count++
		}
	}
	return count
}

func (r Result) WarningCount() int {
	count := 0
	for _, issue := range r.Issues {
		if issue.Severity == SeverityWarning {
			count++
		}
	}
	return count
}

var markdownLinkRE = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]*)\)`)

var allowedMemoryTypes = map[string]bool{
	"semantic":    true,
	"episodic":    true,
	"procedural":  true,
	"prospective": true,
}

var recognizedTypes = map[string]bool{
	"concept":   true,
	"procedure": true,
	"decision":  true,
	"person":    true,
	"project":   true,
	"event":     true,
}

var allowedTrustTiers = map[string]bool{
	"T1": true,
	"T2": true,
	"T3": true,
}

var allowedConfidence = map[string]bool{
	"high":   true,
	"medium": true,
	"low":    true,
}

func ValidateBundle(root string, strict bool) (Result, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", root)
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}

	var result Result
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
		pageIssues, err := validatePage(root, path, rel, strict)
		if err != nil {
			return err
		}
		result.FilesChecked++
		result.Issues = append(result.Issues, pageIssues...)
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].Path != result.Issues[j].Path {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		if result.Issues[i].Severity != result.Issues[j].Severity {
			return result.Issues[i].Severity < result.Issues[j].Severity
		}
		return result.Issues[i].Message < result.Issues[j].Message
	})
	return result, nil
}

func validatePage(root, path, rel string, strict bool) ([]Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	frontmatter, body, ok, err := parseFrontmatter(string(data))
	if err != nil {
		return []Issue{{Severity: SeverityError, Path: rel, Message: err.Error()}}, nil
	}

	var issues []Issue
	if !ok {
		if !isStructuralIndex(rel) {
			issues = append(issues,
				Issue{Severity: SeverityError, Path: rel, Message: "missing YAML frontmatter with required type field"},
				Issue{Severity: SeverityWarning, Path: rel, Message: "missing engram9 required field: title"},
				Issue{Severity: SeverityWarning, Path: rel, Message: "missing engram9 required field: description"},
				Issue{Severity: SeverityWarning, Path: rel, Message: "missing engram9 required field: timestamp"},
				Issue{Severity: SeverityWarning, Path: rel, Message: "missing engram9 required field: memory_type"},
			)
		}
	} else {
		issues = append(issues, validateFrontmatter(rel, frontmatter, strict)...)
	}

	issues = append(issues, validateLinks(root, filepath.Dir(path), rel, body)...)
	return issues, nil
}

func validateFrontmatter(rel string, fields map[string][]string, strict bool) []Issue {
	var issues []Issue
	pageType := scalar(fields, "type")
	if pageType == "" {
		issues = append(issues, Issue{Severity: SeverityError, Path: rel, Message: "missing OKF required field: type"})
	} else if strict && !recognizedTypes[pageType] {
		issues = append(issues, Issue{Severity: SeverityWarning, Path: rel, Message: fmt.Sprintf("unknown engram9 type %q", pageType)})
	}

	for _, field := range []string{"title", "description", "timestamp", "memory_type"} {
		if scalar(fields, field) == "" {
			issues = append(issues, Issue{Severity: SeverityWarning, Path: rel, Message: "missing engram9 required field: " + field})
		}
	}

	if timestamp := scalar(fields, "timestamp"); timestamp != "" {
		if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
			issues = append(issues, Issue{Severity: SeverityError, Path: rel, Message: "invalid timestamp format: must be RFC3339 / ISO 8601"})
		}
	}

	if memoryType := scalar(fields, "memory_type"); memoryType != "" && !allowedMemoryTypes[memoryType] {
		issues = append(issues, Issue{Severity: SeverityError, Path: rel, Message: fmt.Sprintf("invalid memory_type %q", memoryType)})
	}

	if len(fields["source_events"]) == 0 {
		issues = append(issues, Issue{Severity: SeverityWarning, Path: rel, Message: "missing source_events provenance"})
	}
	if trustTier := scalar(fields, "trust_tier"); trustTier == "" {
		issues = append(issues, Issue{Severity: SeverityWarning, Path: rel, Message: "missing recommended field: trust_tier"})
	} else if !allowedTrustTiers[trustTier] {
		issues = append(issues, Issue{Severity: SeverityError, Path: rel, Message: fmt.Sprintf("invalid trust_tier %q", trustTier)})
	}
	if confidence := scalar(fields, "confidence"); confidence != "" && !allowedConfidence[confidence] {
		issues = append(issues, Issue{Severity: SeverityError, Path: rel, Message: fmt.Sprintf("invalid confidence %q", confidence)})
	}
	return issues
}

func isStructuralIndex(rel string) bool {
	return filepath.Base(filepath.FromSlash(rel)) == "index.md"
}

func parseFrontmatter(content string) (map[string][]string, string, bool, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.HasPrefix(content, "---\n") && content != "---" {
		return nil, content, false, nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, content, false, nil
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, "", true, fmt.Errorf("unterminated YAML frontmatter")
	}

	fields, err := parseYAMLFields(strings.Join(lines[1:end], "\n"))
	if err != nil {
		return nil, "", true, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	body := strings.Join(lines[end+1:], "\n")
	return fields, body, true, nil
}

func parseYAMLFields(frontmatter string) (map[string][]string, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return nil, err
	}
	fields := make(map[string][]string)
	for key, value := range raw {
		fields[key] = yamlValueStrings(value)
	}
	return fields, nil
}

func yamlValueStrings(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case int:
		return []string{fmt.Sprint(v)}
	case int64:
		return []string{fmt.Sprint(v)}
	case float64:
		return []string{fmt.Sprint(v)}
	case bool:
		return []string{fmt.Sprint(v)}
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			values = append(values, yamlValueStrings(item)...)
		}
		return values
	default:
		return nil
	}
}

func scalar(fields map[string][]string, key string) string {
	values := fields[key]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func validateLinks(root, pageDir, rel string, body string) []Issue {
	body = stripMarkdownCode(body)
	matches := markdownLinkRE.FindAllStringSubmatchIndex(body, -1)
	issues := make([]Issue, 0)
	for _, match := range matches {
		fullStart := match[0]
		if fullStart > 0 && body[fullStart-1] == '!' {
			continue
		}
		link := body[match[2]:match[3]]
		link = markdownLinkDestination(link)
		if link == "" || isExternalLink(link) {
			continue
		}
		link = strings.Split(link, "#")[0]
		link = strings.Split(link, "?")[0]
		link = decodeMarkdownDestinationPath(link)
		if link == "" {
			continue
		}
		if filepath.IsAbs(link) {
			continue
		}
		target := filepath.Clean(filepath.Join(pageDir, filepath.FromSlash(link)))
		if !isWithin(root, target) {
			continue
		}
		if _, err := os.Stat(target); err != nil {
			if os.IsNotExist(err) {
				issues = append(issues, Issue{Severity: SeverityWarning, Path: rel, Message: fmt.Sprintf("broken internal link: %s", link)})
			} else {
				issues = append(issues, Issue{Severity: SeverityWarning, Path: rel, Message: fmt.Sprintf("could not stat internal link %s: %v", link, err)})
			}
		}
	}
	return issues
}

func markdownLinkDestination(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "<") {
		if end := strings.Index(raw, ">"); end >= 0 {
			return strings.TrimSpace(raw[1:end])
		}
	}
	inSingle := false
	inDouble := false
	for i, r := range raw {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ' ', '\t', '\n':
			if !inSingle && !inDouble {
				return strings.TrimSpace(raw[:i])
			}
		}
	}
	return raw
}

func stripMarkdownCode(body string) string {
	var out []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			out = append(out, "")
			continue
		}
		if inFence {
			out = append(out, "")
			continue
		}
		out = append(out, stripInlineCode(line))
	}
	return strings.Join(out, "\n")
}

func stripInlineCode(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		runEnd := i
		for runEnd < len(line) && line[runEnd] == '`' {
			runEnd++
		}
		delimiter := line[i:runEnd]
		closeOffset := strings.Index(line[runEnd:], delimiter)
		if closeOffset == -1 {
			i = runEnd
			continue
		}
		i = runEnd + closeOffset + len(delimiter)
	}
	return b.String()
}

func decodeMarkdownDestinationPath(link string) string {
	link = strings.ReplaceAll(link, "%20", " ")
	link = strings.ReplaceAll(link, "%28", "(")
	link = strings.ReplaceAll(link, "%29", ")")
	link = strings.ReplaceAll(link, "%3C", "<")
	link = strings.ReplaceAll(link, "%3E", ">")
	link = strings.ReplaceAll(link, "%5C", "\\")
	return link
}

func isExternalLink(link string) bool {
	lower := strings.ToLower(link)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "tel:") ||
		strings.HasPrefix(lower, "#")
}

func isWithin(root, target string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
