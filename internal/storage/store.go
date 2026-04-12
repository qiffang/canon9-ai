package storage

// Store defines the 9 tool functions that agents use to interact with memory.
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

	// SearchWiki performs a text search across all active wiki pages.
	SearchWiki(query string) ([]SearchResult, error)

	// --- Wiki write operations ---

	// WriteWikiPage creates or updates a wiki page.
	// Automatically creates sidecar if it doesn't exist.
	WriteWikiPage(path string, content string) error

	// ArchiveWikiPage moves a page from active wiki to archive/.
	ArchiveWikiPage(path string, reason string) error

	// RebuildIndex scans all active wiki pages and regenerates index.md.
	RebuildIndex() error

	// --- Stats ---

	// GetMemoryStats returns system-level statistics.
	GetMemoryStats() (*MemoryStats, error)
}
