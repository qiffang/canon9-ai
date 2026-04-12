package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FS implements Store using local filesystem.
// raw/ → JSONL file, wiki/ → markdown files, .meta/ → sidecar JSON.
type FS struct {
	dataDir string

	mu     sync.Mutex
	events []Event // in-memory cache loaded from JSONL
	cursor uint64  // compile cursor (last processed event index)
}

// NewFS creates a filesystem-backed Store rooted at dataDir.
// It initializes the directory structure and loads existing events.
func NewFS(dataDir string) (*FS, error) {
	dirs := []string{
		filepath.Join(dataDir, "raw"),
		filepath.Join(dataDir, "wiki"),
		filepath.Join(dataDir, "wiki", ".meta"),
		filepath.Join(dataDir, "wiki", "semantic"),
		filepath.Join(dataDir, "wiki", "episodic"),
		filepath.Join(dataDir, "wiki", "procedural"),
		filepath.Join(dataDir, "wiki", "prospective"),
		filepath.Join(dataDir, "wiki", "archive"),
		filepath.Join(dataDir, "wiki", "archive", "semantic"),
		filepath.Join(dataDir, "wiki", "archive", "episodic"),
		filepath.Join(dataDir, "wiki", "archive", "procedural"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	fs := &FS{dataDir: dataDir}
	if err := fs.loadEvents(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (f *FS) rawPath() string  { return filepath.Join(f.dataDir, "raw", "events.jsonl") }
func (f *FS) wikiDir() string  { return filepath.Join(f.dataDir, "wiki") }
func (f *FS) metaDir() string  { return filepath.Join(f.dataDir, "wiki", ".meta") }
func (f *FS) indexPath() string { return filepath.Join(f.dataDir, "wiki", "index.md") }

func (f *FS) loadEvents() error {
	path := f.rawPath()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open events: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB line buffer
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue // skip malformed lines
		}
		f.events = append(f.events, ev)
	}
	return scanner.Err()
}

// AppendEvent writes a new event to the raw log.
func (f *FS) AppendEvent(ev Event) (string, error) {
	if ev.ID == "" {
		ev.ID = GenerateEventID()
	}
	if ev.Timestamp == "" {
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return "", fmt.Errorf("marshal event: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	file, err := os.OpenFile(f.rawPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open raw log: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(data, '\n')); err != nil {
		return "", fmt.Errorf("write event: %w", err)
	}

	f.events = append(f.events, ev)
	return ev.ID, nil
}

// ReadEventsSince returns events after the cursor position (0-indexed).
func (f *FS) ReadEventsSince(cursor uint64) (*EventsPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if cursor >= uint64(len(f.events)) {
		return &EventsPage{NewCursor: uint64(len(f.events))}, nil
	}

	events := make([]Event, len(f.events)-int(cursor))
	copy(events, f.events[cursor:])
	return &EventsPage{
		Events:    events,
		NewCursor: uint64(len(f.events)),
	}, nil
}

// ReadWikiIndex returns the contents of wiki/index.md.
func (f *FS) ReadWikiIndex() (string, error) {
	data, err := os.ReadFile(f.indexPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read index: %w", err)
	}
	return string(data), nil
}

// ReadWikiPage reads a wiki page and its sidecar.
func (f *FS) ReadWikiPage(path string) (*WikiPage, error) {
	if err := validateWikiPath(path); err != nil {
		return nil, err
	}

	fullPath := filepath.Join(f.wikiDir(), path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read page %s: %w", path, err)
	}

	meta := f.readSidecar(path)

	// Update access telemetry.
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	meta.LastAccessed = now.Format(time.RFC3339)
	if len(meta.AccessDates) == 0 || meta.AccessDates[len(meta.AccessDates)-1] != today {
		meta.AccessDates = append(meta.AccessDates, today)
	}
	_ = f.writeSidecar(path, meta)

	return &WikiPage{
		Path:    path,
		Content: string(content),
		Meta:    meta,
	}, nil
}

// SearchWiki does a case-insensitive text search across active wiki pages.
func (f *FS) SearchWiki(query string) ([]SearchResult, error) {
	var results []SearchResult
	queryLower := strings.ToLower(query)

	err := filepath.Walk(f.wikiDir(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".meta" || name == "archive" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		relPath, _ := filepath.Rel(f.wikiDir(), path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), queryLower) {
				results = append(results, SearchResult{
					Path:    relPath,
					Line:    i + 1,
					Content: line,
				})
			}
		}
		return nil
	})

	return results, err
}

// WriteWikiPage creates or updates a wiki page and its sidecar.
func (f *FS) WriteWikiPage(path string, content string) error {
	if err := validateWikiPath(path); err != nil {
		return err
	}

	fullPath := filepath.Join(f.wikiDir(), path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write page %s: %w", path, err)
	}

	// Create sidecar if it doesn't exist.
	meta := f.readSidecar(path)
	if meta.CreatedAt == "" {
		now := time.Now().UTC().Format(time.RFC3339)
		meta.CreatedAt = now
		meta.LastAccessed = now
		meta.MemoryType = inferMemoryType(path)
		_ = f.writeSidecar(path, meta)
	}

	return nil
}

// ArchiveWikiPage moves a page to archive/ and updates its sidecar.
func (f *FS) ArchiveWikiPage(path string, reason string) error {
	if err := validateWikiPath(path); err != nil {
		return err
	}

	srcPath := filepath.Join(f.wikiDir(), path)
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("page %s does not exist", path)
	}

	// Determine archive destination: wiki/archive/{original_path}
	dstPath := filepath.Join(f.wikiDir(), "archive", path)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("move to archive: %w", err)
	}

	// Update sidecar.
	meta := f.readSidecar(path)
	now := time.Now().UTC().Format(time.RFC3339)
	meta.ArchivedAt = now
	meta.ArchiveReason = reason

	// Move sidecar to archive location too.
	archiveSidecarPath := "archive/" + path
	_ = f.writeSidecar(archiveSidecarPath, meta)
	_ = f.deleteSidecar(path)

	return nil
}

// RebuildIndex scans all active wiki pages and regenerates index.md.
func (f *FS) RebuildIndex() error {
	var sections []string
	categories := []string{"semantic", "episodic", "procedural", "prospective"}

	for _, cat := range categories {
		catDir := filepath.Join(f.wikiDir(), cat)
		var pages []string

		_ = filepath.Walk(catDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			relPath, _ := filepath.Rel(f.wikiDir(), path)
			if relPath == cat+"/index.md" {
				return nil // skip category index files
			}

			// Read first non-empty, non-comment line as description.
			desc := extractPageDescription(path)
			pages = append(pages, fmt.Sprintf("- [%s](%s) — %s", filepath.Base(relPath), relPath, desc))
			return nil
		})

		if len(pages) > 0 {
			section := fmt.Sprintf("## %s\n\n%s", cat, strings.Join(pages, "\n"))
			sections = append(sections, section)
		}
	}

	content := "# Wiki Index\n\n"
	if len(sections) == 0 {
		content += "_No pages yet._\n"
	} else {
		content += strings.Join(sections, "\n\n") + "\n"
	}

	return os.WriteFile(f.indexPath(), []byte(content), 0644)
}

// GetMemoryStats returns system-level statistics.
func (f *FS) GetMemoryStats() (*MemoryStats, error) {
	f.mu.Lock()
	eventCount := len(f.events)
	compileCursor := f.cursor
	f.mu.Unlock()

	wikiCount := 0
	archiveCount := 0

	_ = filepath.Walk(f.wikiDir(), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		relPath, _ := filepath.Rel(f.wikiDir(), path)
		if relPath == "index.md" {
			return nil
		}
		if strings.HasPrefix(relPath, "archive/") {
			archiveCount++
		} else if !strings.HasPrefix(relPath, ".meta/") {
			wikiCount++
		}
		return nil
	})

	return &MemoryStats{
		EventCount:        eventCount,
		UncompiledCount:   eventCount - int(compileCursor),
		WikiPageCount:     wikiCount,
		ArchivedPageCount: archiveCount,
	}, nil
}

// SetCompileCursor updates the compile cursor (used by compile agent).
func (f *FS) SetCompileCursor(cursor uint64) {
	f.mu.Lock()
	f.cursor = cursor
	f.mu.Unlock()
}

// GetCompileCursor returns the current compile cursor.
func (f *FS) GetCompileCursor() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cursor
}

// --- Sidecar helpers ---

func (f *FS) sidecarPath(wikiPath string) string {
	// wiki/semantic/projects/db9.md → wiki/.meta/semantic/projects/db9.json
	jsonPath := strings.TrimSuffix(wikiPath, ".md") + ".json"
	return filepath.Join(f.metaDir(), jsonPath)
}

func (f *FS) readSidecar(wikiPath string) *Sidecar {
	path := f.sidecarPath(wikiPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return &Sidecar{}
	}
	var meta Sidecar
	if err := json.Unmarshal(data, &meta); err != nil {
		return &Sidecar{}
	}
	return &meta
}

func (f *FS) writeSidecar(wikiPath string, meta *Sidecar) error {
	path := f.sidecarPath(wikiPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (f *FS) deleteSidecar(wikiPath string) error {
	return os.Remove(f.sidecarPath(wikiPath))
}

// --- Helpers ---

func validateWikiPath(path string) error {
	if path == "" {
		return fmt.Errorf("empty wiki path")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("wiki path must be relative")
	}
	if strings.HasPrefix(path, ".meta/") {
		return fmt.Errorf("cannot directly access .meta/")
	}
	return nil
}

func inferMemoryType(path string) string {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	switch parts[0] {
	case "semantic", "episodic", "procedural", "prospective":
		return parts[0]
	}
	return ""
}

func extractPageDescription(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "<!--") || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 100 {
			return line[:100] + "..."
		}
		return line
	}
	return ""
}
