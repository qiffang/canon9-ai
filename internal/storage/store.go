package storage

// Store defines the tool functions that agents use to interact with memory.
// This is the stable abstraction layer — implementations can be swapped without
// changing agent code.
type Store interface {
	// --- Raw event operations ---

	// AppendEvent writes a new event to the append-only log.
	AppendEvent(ev Event) (string, error)

	// ReadEventsSince returns events after the given cursor position.
	ReadEventsSince(cursor uint64) (*EventsPage, error)

	// --- Wiki read operations ---

	// ReadWikiIndex returns the contents of wiki/index.md.
	ReadWikiIndex() (string, error)

	// ReadWikiPage reads a wiki page and its sidecar metadata.
	// Automatically updates sidecar last_accessed and access_dates.
	ReadWikiPage(path string) (*WikiPage, error)

	// SearchWiki performs a text search across all wiki pages (including archive).
	SearchWiki(query string) ([]SearchResult, error)

	// --- Wiki write operations ---

	// WriteWikiPage creates or updates a wiki page.
	// Automatically creates sidecar if it doesn't exist.
	WriteWikiPage(path string, content string) error

	// WriteWikiPageWithMeta creates or updates a wiki page with source tracking.
	// sourceEvents are appended to sidecar (deduplicated).
	// trustTier updates TrustTierMax if more trusted (lower number).
	WriteWikiPageWithMeta(path string, content string, sourceEvents []string, trustTier int) error

	// ArchiveWikiPage moves a page from active wiki to archive/.
	ArchiveWikiPage(path string, reason string) error

	// RebuildIndex scans all active wiki pages and regenerates index.md.
	RebuildIndex() error

	// --- Compile cursor ---

	// SetCompileCursor persists the compile progress cursor.
	SetCompileCursor(cursor uint64) error

	// GetCompileCursor reads the persisted compile cursor.
	GetCompileCursor() uint64

	// --- Stats ---

	// GetMemoryStats returns system-level statistics.
	GetMemoryStats() (*MemoryStats, error)
}
