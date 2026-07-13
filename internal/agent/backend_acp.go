package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiffang/engram9/internal/storage"
)

const (
	DefaultACPTurnTimeout  = 10 * time.Minute
	DefaultACPMaxDiffBytes = 1 << 20 // 1 MB
)

// ACPBackendConfig holds configuration for the ACP backend.
type ACPBackendConfig struct {
	Provider       string        // "claude", "codex"
	AcpmuxCommand  string        // path to acpmux binary (default: "acpmux")
	TurnTimeout    time.Duration // per-turn timeout (default: 10m)
	MaxDiffBytes   int64         // max bytes changed per turn (default: 1MB)
	AdditionalDirs string        // rejected if set (Phase 1)
}

// ACPBackend runs wiki agents via acpmux ACP protocol.
type ACPBackend struct {
	cfg       ACPBackendConfig
	dataDir   string
	validator *WikiValidator
}

// NewACPBackend creates an ACPBackend. It validates the config at construction time.
func NewACPBackend(dataDir string, cfg ACPBackendConfig) (*ACPBackend, error) {
	if cfg.Provider == "" {
		return nil, fmt.Errorf("ACP_PROVIDER is required")
	}
	if cfg.AcpmuxCommand == "" {
		cfg.AcpmuxCommand = "acpmux"
	}
	if cfg.TurnTimeout <= 0 {
		cfg.TurnTimeout = DefaultACPTurnTimeout
	}
	if cfg.MaxDiffBytes <= 0 {
		cfg.MaxDiffBytes = DefaultACPMaxDiffBytes
	}
	if cfg.AdditionalDirs != "" {
		return nil, fmt.Errorf("ACP_ADDITIONAL_DIRS is disabled in Phase 1; remove it from config")
	}

	// Verify acpmux binary exists.
	if _, err := exec.LookPath(cfg.AcpmuxCommand); err != nil {
		return nil, fmt.Errorf("acpmux binary not found: %w", err)
	}

	maxDiff := cfg.MaxDiffBytes
	if maxDiff <= 0 {
		maxDiff = DefaultACPMaxDiffBytes
	}

	return &ACPBackend{
		cfg:       cfg,
		dataDir:   dataDir,
		validator: NewWikiValidator(maxDiff),
	}, nil
}

func (b *ACPBackend) RunIngest(ctx context.Context, eventID string, text string, ctxInfo map[string]string) (IngestResult, error) {
	prompt := fmt.Sprintf(`You are the Ingest Agent. Event %s has been recorded with this content:

%s`, eventID, text)
	if len(ctxInfo) > 0 {
		ctxJSON, _ := json.Marshal(ctxInfo)
		prompt += fmt.Sprintf("\n\nContext: %s", string(ctxJSON))
	}
	prompt += "\n\n" + integrateSystemPrompt

	summary, err := b.runACPTurn(ctx, prompt)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Summary: summary}, nil
}

func (b *ACPBackend) RunCompile(ctx context.Context, cursor uint64) (CompileResult, error) {
	prompt := fmt.Sprintf(`You are the Compile Agent. Run a full compile cycle.

Current compile cursor: %d (call read_events_since with cursor=%d to get unprocessed events).

Execute all three phases:
1. Distill new events into wiki
2. Sleep pruning (archive stale pages per memory-type rules)
3. Rebuild index

Report what you did when finished.`, cursor, cursor)
	prompt += "\n\n" + compileSystemPrompt

	summary, err := b.runACPTurn(ctx, prompt)
	if err != nil {
		return CompileResult{}, err
	}
	// In ACP mode, cursor tracking from tool results is not available.
	// The caller must detect progress through other means (e.g. checking wiki state).
	return CompileResult{Summary: summary, NewCursor: 0}, nil
}

func (b *ACPBackend) RunQuery(_ context.Context, _ string, _ map[string]string, _ []storage.Event) (QueryResult, error) {
	return QueryResult{}, ErrNotImplemented
}

func (b *ACPBackend) Close() error {
	return nil
}

// runACPTurn executes a single ACP agent turn:
// 1. Copy data dir to staging
// 2. Spawn acpmux with MCP config pointing to staging
// 3. Send initialize + session/new + session/prompt
// 4. Wait for completion
// 5. Validate staging wiki
// 6. Merge staging -> production
func (b *ACPBackend) runACPTurn(ctx context.Context, prompt string) (string, error) {
	// Apply turn timeout.
	ctx, cancel := context.WithTimeout(ctx, b.cfg.TurnTimeout)
	defer cancel()

	// 1. Create staging directory and copy data.
	stagingDir, err := os.MkdirTemp("", "engram9-acp-staging-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	if err := copyDir(b.dataDir, stagingDir); err != nil {
		return "", fmt.Errorf("copy to staging: %w", err)
	}

	// 2. Spawn acpmux process.
	args := []string{
		"--provider", b.cfg.Provider,
		"--provider-arg", "--tools",
		"--provider-arg", "",
		"--provider-arg", "--strict-mcp-config",
	}

	cmd := exec.CommandContext(ctx, b.cfg.AcpmuxCommand, args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start acpmux: %w", err)
	}

	// Ensure process is killed on exit.
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)

	// 3. Send initialize.
	initReq := acpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params: mustMarshal(map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "engram9", "version": "0.1.0"},
		}),
	}
	if err := sendACPRequest(stdin, initReq); err != nil {
		return "", fmt.Errorf("send initialize: %w", err)
	}
	if _, err := readACPResponse(scanner); err != nil {
		return "", fmt.Errorf("initialize response: %w", err)
	}

	// Send initialized notification.
	initNotif := acpRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := sendACPRequest(stdin, initNotif); err != nil {
		return "", fmt.Errorf("send initialized notification: %w", err)
	}

	// 4. Send session/new with MCP config.
	mcpConfig := map[string]any{
		"command": "engram9-mcp",
		"args":    []string{"-data", stagingDir, "-mode", "agent"},
	}
	sessionReq := acpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "session/new",
		Params: mustMarshal(map[string]any{
			"mcpServers": []map[string]any{
				{"name": "engram9", "transport": mcpConfig},
			},
		}),
	}
	if err := sendACPRequest(stdin, sessionReq); err != nil {
		return "", fmt.Errorf("send session/new: %w", err)
	}
	sessionResp, err := readACPResponse(scanner)
	if err != nil {
		return "", fmt.Errorf("session/new response: %w", err)
	}

	// Extract session ID.
	var sessionResult struct {
		SessionID string `json:"sessionId"`
	}
	if sessionResp.Result != nil {
		_ = json.Unmarshal(sessionResp.Result, &sessionResult)
	}

	// 5. Send session/prompt.
	promptReq := acpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "session/prompt",
		Params: mustMarshal(map[string]any{
			"sessionId": sessionResult.SessionID,
			"prompt":    prompt,
		}),
	}
	if err := sendACPRequest(stdin, promptReq); err != nil {
		return "", fmt.Errorf("send session/prompt: %w", err)
	}

	// 6. Stream events until completion.
	var summary string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var resp acpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			log.Printf("[acp] invalid response: %s", line)
			continue
		}

		// Check for errors.
		if resp.Error != nil {
			return "", fmt.Errorf("ACP error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		// If this is a response to our prompt request (id=3), we're done.
		if string(resp.ID) == "3" {
			if resp.Result != nil {
				var promptResult struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(resp.Result, &promptResult)
				summary = promptResult.Text
			}
			break
		}

		// Otherwise it's a notification/update — log and continue.
		if resp.Method != "" {
			log.Printf("[acp] notification: %s", resp.Method)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read acp output: %w", err)
	}

	// 7. Validate staging wiki.
	violations, err := b.validator.Validate(b.dataDir, stagingDir)
	if err != nil {
		return "", fmt.Errorf("validate staging: %w", err)
	}
	if len(violations) > 0 {
		var msgs []string
		for _, v := range violations {
			msgs = append(msgs, v.String())
		}
		return "", fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
	}

	// 8. Merge staging wiki -> production.
	if err := mergeWiki(stagingDir, b.dataDir); err != nil {
		return "", fmt.Errorf("merge staging: %w", err)
	}

	return summary, nil
}

// --- ACP JSON-RPC types ---

type acpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type acpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpError       `json:"error,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type acpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func sendACPRequest(w io.Writer, req acpRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

func readACPResponse(scanner *bufio.Scanner) (*acpResponse, error) {
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var resp acpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		return &resp, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// copyDir copies src directory to dst recursively.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// mergeWiki copies staging wiki/ directory to production wiki/ directory.
func mergeWiki(stagingDir, prodDir string) error {
	stagingWiki := filepath.Join(stagingDir, "wiki")
	prodWiki := filepath.Join(prodDir, "wiki")

	// Check if staging wiki exists.
	if _, err := os.Stat(stagingWiki); os.IsNotExist(err) {
		return nil // nothing to merge
	}

	return filepath.Walk(stagingWiki, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(stagingWiki, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(prodWiki, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

