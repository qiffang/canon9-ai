package repo

const FactsVersion = "repo-facts/v1"

type ScanOptions struct {
	RepoPath string
	Scope    string
	Since    string
}

type Bundle struct {
	Manifest Manifest  `json:"manifest"`
	Facts    []Fact    `json:"facts"`
	Snippets []Snippet `json:"snippets"`
}

type Manifest struct {
	Version    string            `json:"version"`
	Repo       string            `json:"repo"`
	Scope      string            `json:"scope"`
	BaseSHA    string            `json:"base_sha,omitempty"`
	HeadSHA    string            `json:"head_sha"`
	FileHash   map[string]string `json:"file_hashes"`
	FactIDs    []string          `json:"fact_ids"`
	SnippetIDs []string          `json:"snippet_ids"`
	Files      []FileManifest    `json:"files"`
	Changed    []string          `json:"changed,omitempty"`
	Deleted    []string          `json:"deleted,omitempty"`
}

type FileManifest struct {
	Path       string   `json:"path"`
	Status     string   `json:"status"`
	Hash       string   `json:"hash,omitempty"`
	FactIDs    []string `json:"fact_ids"`
	SnippetIDs []string `json:"snippet_ids"`
	Symbols    []string `json:"symbols"`
	Anchors    []string `json:"source_anchors"`
}

type Fact struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Status     string   `json:"status"`
	Repo       string   `json:"repo"`
	CommitSHA  string   `json:"commit_sha"`
	Path       string   `json:"path"`
	Line       int      `json:"line,omitempty"`
	EndLine    int      `json:"end_line,omitempty"`
	Symbol     string   `json:"symbol,omitempty"`
	Anchor     string   `json:"source_anchor"`
	Package    string   `json:"package,omitempty"`
	Name       string   `json:"name,omitempty"`
	Receiver   string   `json:"receiver,omitempty"`
	Signature  string   `json:"signature,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	Target     string   `json:"target,omitempty"`
	Imports    []string `json:"imports,omitempty"`
	Implements []string `json:"implements,omitempty"`
	Exported   bool     `json:"exported"`
	TestTarget string   `json:"test_target,omitempty"`
	FileHash   string   `json:"file_hash,omitempty"`
}

type Snippet struct {
	ID        string `json:"id"`
	FactID    string `json:"fact_id"`
	Kind      string `json:"kind"`
	Repo      string `json:"repo"`
	CommitSHA string `json:"commit_sha"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Symbol    string `json:"symbol"`
	Anchor    string `json:"source_anchor"`
	Language  string `json:"language"`
	FileHash  string `json:"file_hash"`
	Content   string `json:"content"`
}

