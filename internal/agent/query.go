package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiffang/engram9/internal/storage"
)

const querySystemPrompt = `You are the Query Agent of a brain-inspired memory system. Your role is to recall information — like the brain reconstructing memories from fragments in the current context, not just reading back stored text.

## Your Process (Recall = Reconstruct, not Read)

1. **Read the wiki index** — Call read_wiki_index() to understand the knowledge structure.

2. **Check prospective memories** — Read prospective/index.md (if it exists) to check if any future intentions match the current query context. If a trigger condition matches, include the reminder in your answer.

3. **Read relevant pages** — Based on the question, read the most relevant wiki pages. Use search_wiki() if the index doesn't clearly point to the right pages.

4. **Reconstruct the answer** — Synthesize information from multiple pages + the current context to construct a comprehensive answer. Do NOT just copy-paste wiki content. Reconstruct it like a brain would:
   - Combine information from multiple sources
   - Apply current context to shape the answer
   - Note confidence levels based on trust tiers
   - Cite sources: [evt_xxx]

5. **Opportunistic wiki maintenance** — Only if you discover issues during recall:
   - **Fix errors**: If wiki content is factually wrong, correct it via write_wiki_page()
   - **Fix links**: If cross-references are missing or broken, add them
   - **Archive new knowledge**: If your synthesis produced genuinely new insights (comparisons, connections, patterns) not already in the wiki, write them as new pages

   Do NOT write to wiki on every query. Only when you find actual errors, missing links, or produce novel synthesis.

## Answer Format

Your answer should:
- Directly address the question
- Cite source events: [evt_xxx]
- Note trust levels if relevant (T1=user stated, T2=inferred, T3=second-hand)
- Mention if information might be outdated or contradictory
- Include any triggered prospective reminders

## Important Rules

- Recall is RECONSTRUCTION, not retrieval. Frame answers in the context of the current question.
- Be honest about gaps. If the wiki doesn't have information, say so.
- Don't fabricate information. If you don't know, say you don't know.
- Only modify wiki when you find genuine issues — don't write wiki content on every query.`

// QueryAgent handles the recall() flow — reconstructive memory retrieval.
type QueryAgent struct {
	runner *Runner
}

func NewQueryAgent(llm LLM, executor *ToolExecutor) *QueryAgent {
	return NewQueryAgentWithOptions(llm, executor, RunnerOptions{})
}

func NewQueryAgentWithOptions(llm LLM, executor *ToolExecutor, opts RunnerOptions) *QueryAgent {
	return &QueryAgent{runner: NewRunnerWithOptions(llm, executor, opts)}
}

// Recall answers a question by reconstructing knowledge from the memory system.
// recentEvents are injected into the user message so the LLM can use them
// even if wiki integration hasn't completed yet.
func (a *QueryAgent) Recall(ctx context.Context, question string, ctxInfo map[string]string, recentEvents []storage.Event) (string, error) {
	userMsg := fmt.Sprintf("Recall/answer this question:\n\n%s", question)
	if len(ctxInfo) > 0 {
		ctxJSON, _ := json.Marshal(ctxInfo)
		userMsg += fmt.Sprintf("\n\nCurrent context: %s", string(ctxJSON))
	}
	if len(recentEvents) > 0 {
		userMsg += "\n\n---\nRecent events (may not be in wiki yet):\n"
		for _, ev := range recentEvents {
			var parts []string
			parts = append(parts, fmt.Sprintf("[%s] %s", ev.ID, ev.Content))
			if ev.ActiveProject != "" {
				parts = append(parts, fmt.Sprintf("  project=%s", ev.ActiveProject))
			}
			if ev.ActiveTask != "" {
				parts = append(parts, fmt.Sprintf("  task=%s", ev.ActiveTask))
			}
			userMsg += strings.Join(parts, "") + "\n"
		}
	}

	return a.runner.Run(ctx, querySystemPrompt, QueryTools, userMsg)
}
