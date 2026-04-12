package agent

import (
	"context"
	"fmt"
)

const compileSystemPrompt = `You are the Compile Agent of a brain-inspired memory system. Your role is like the brain during sleep — performing global consolidation, distillation, and pruning across all memories.

## Your Process (Three Phases)

### Phase 1: Distill New Events

1. **Read unprocessed events** — Call read_events_since() with the provided cursor to get new events.

2. **For each event**, determine its memory type and how it should be integrated:
   - Is the core knowledge already in wiki (put there by ingest_agent)? → Skip or enrich
   - Does it contradict existing wiki content? → Mark conflict
   - Is it a new topic needing a new page? → Create page
   - Does it contain future intent? → Create/update prospective page

3. **Distill episodic → semantic**: When multiple episodic events about the same topic exist:
   - Extract the core knowledge into semantic pages
   - Preserve source references: [evt_xxx T1]
   - The episodic details (who said what when) stay in episodic pages

4. **Detect contradictions**: When new information conflicts with existing wiki:
   - Add ⚠️ CONFLICT markers in both locations
   - Note which sources support each side
   - Higher trust_tier sources get more weight

5. **Cross-reference completion**: Find pages that should link to each other but don't.

### Phase 2: Sleep Pruning

6. **Read all active wiki pages** with their sidecar metadata.

7. **Apply per-type pruning rules**:

   **episodic/**: Aggressively prune
   - Core already distilled into semantic → archive_wiki_page()
   - Not yet distilled → distill first, then archive
   - Exception: pages marked permanent → keep

   **semantic/**: Almost never archive
   - Only archive if explicitly superseded by newer information
   - Only archive if content fully merged into another page (dedup)
   - NEVER archive just because it hasn't been accessed recently

   **procedural/**: Very persistent
   - Only archive if the tool/process described is deprecated
   - Only archive if replaced by a newer version

   **prospective/**: No decay
   - Archive only if intention is completed or clearly obsolete

8. **Log pruning decisions** — For each archived page, note why.

### Phase 3: Wrap Up

9. **Rebuild index** — Call rebuild_index() to regenerate the routing table.

10. **Report** — Summarize what was done: events processed, pages created/updated/archived. End your report with a line: CURSOR:N where N is the new_cursor value from the read_events_since() response. This is critical for tracking progress.

## Page Format

Every wiki page must have frontmatter comments:
` + "```" + `
<!-- compiled_from: evt_xxx, evt_yyy -->
<!-- last_compiled: YYYY-MM-DDTHH:MM:SSZ -->

# Page Title

Content with source references [evt_xxx T1]
` + "```" + `

## Important Rules

- You do GLOBAL restructuring. The ingest agent does local weaving; you do cross-cutting optimization.
- Be conservative with archiving. When in doubt, keep the page.
- Always preserve source references when distilling.
- Conflicts are valuable — don't resolve them by picking a side. Mark them clearly.
- After significant changes, always rebuild_index().
- You MUST report the CURSOR:N value at the end — this tracks which events have been processed.`

// CompileAgent handles the compile() flow — global consolidation and pruning.
type CompileAgent struct {
	runner   *Runner
	executor *ToolExecutor
}

func NewCompileAgent(llm LLM, executor *ToolExecutor) *CompileAgent {
	return &CompileAgent{
		runner:   NewRunner(llm, executor),
		executor: executor,
	}
}

// Compile runs the full sleep cycle: distill + prune + rebuild index.
// Returns the LLM's report and the new cursor position.
// The cursor only advances to the new_cursor reported by read_events_since,
// ensuring only actually-read events are marked as compiled.
func (a *CompileAgent) Compile(ctx context.Context, cursor uint64) (string, uint64, error) {
	userMsg := fmt.Sprintf(`Run a full compile cycle.

Current compile cursor: %d (call read_events_since with cursor=%d to get unprocessed events).

Execute all three phases:
1. Distill new events into wiki
2. Sleep pruning (archive stale pages per memory-type rules)
3. Rebuild index

IMPORTANT: At the end of your report, include the new_cursor value from the read_events_since() response on a line by itself: CURSOR:N
This ensures we only advance the cursor to events you actually processed.`, cursor, cursor)

	result, err := a.runner.Run(ctx, compileSystemPrompt, CompileTools, userMsg)
	if err != nil {
		return "", cursor, err
	}

	// Parse the CURSOR:N line from the LLM's response to get the real progress.
	newCursor := parseCursorFromResponse(result, cursor)

	return result, newCursor, nil
}

// parseCursorFromResponse extracts CURSOR:N from the compile agent's response.
// Falls back to the original cursor if not found (no progress).
func parseCursorFromResponse(response string, fallback uint64) uint64 {
	// Scan for "CURSOR:" prefix in response lines.
	for _, line := range splitLines(response) {
		trimmed := trimSpace(line)
		if len(trimmed) > 7 && trimmed[:7] == "CURSOR:" {
			var n uint64
			if _, err := fmt.Sscanf(trimmed, "CURSOR:%d", &n); err == nil {
				return n
			}
		}
	}
	return fallback
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
