package storage

// Sidecar holds per-page telemetry metadata, stored separately from wiki content.
type Sidecar struct {
	CreatedAt    string   `json:"created_at"`
	LastAccessed string   `json:"last_accessed"`
	AccessDates  []string `json:"access_dates"`
	SourceEvents []string `json:"source_events"`
	TrustTierMax int      `json:"trust_tier_max"`
	MemoryType   string   `json:"memory_type"`
	ArchivedAt   string   `json:"archived_at,omitempty"`
	ArchiveReason string  `json:"archive_reason,omitempty"`
}

// WikiPage is the combined result of reading a wiki page: content + sidecar.
type WikiPage struct {
	Path    string   `json:"path"`
	Content string   `json:"content"`
	Meta    *Sidecar `json:"meta,omitempty"`
}

// SearchResult represents a single match from search_wiki.
type SearchResult struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// MemoryStats holds system-level statistics.
type MemoryStats struct {
	EventCount       int `json:"event_count"`
	UncompiledCount  int `json:"uncompiled_count"`
	WikiPageCount    int `json:"wiki_page_count"`
	ArchivedPageCount int `json:"archived_page_count"`
}

// EventsPage is the result of reading events since a cursor.
type EventsPage struct {
	Events    []Event `json:"events"`
	NewCursor uint64  `json:"new_cursor"`
}
