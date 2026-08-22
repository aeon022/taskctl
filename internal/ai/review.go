// Package ai asks the configured AI provider (Anthropic, OpenAI, Gemini, or
// a local Ollama model — see missionctl-core/ai) to review a set of tasks
// and suggest follow-ups.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/models"
	coreai "github.com/aeon022/missionctl-core/ai"
)

// FollowUp is one AI-suggested follow-up task.
type FollowUp struct {
	Title   string `json:"title"`
	DueDate string `json:"due_date"` // YYYY-MM-DD
	Reason  string `json:"reason"`
}

func reviewSystemPrompt(today string) string {
	return fmt.Sprintf(
		"You are a helpful assistant reviewing someone's weekly task list. Today's date is %s.\n\n"+
			"Look at the completed and pending tasks for this week and suggest 0 to 5 follow-up tasks — "+
			"things that naturally come next (e.g. a follow-up email after a call, a check-in a few days "+
			"after a deadline, a recurring chore that's due again). Only suggest something genuinely useful; "+
			"if nothing comes to mind, return an empty list rather than inventing filler.\n\n"+
			"Each suggestion needs a concrete due date in YYYY-MM-DD format, computed relative to today's "+
			"real date above (never a relative phrase like \"tomorrow\").\n\n"+
			"Return ONLY a JSON array of objects with keys \"title\", \"due_date\" (YYYY-MM-DD), and \"reason\" "+
			"(one sentence explaining what task prompted the suggestion). No markdown, no explanation, no "+
			"other text.",
		today,
	)
}

func buildReviewPrompt(tasks []models.Task) string {
	var lines []string
	for _, t := range tasks {
		due := "no due date"
		if t.DueDate != nil {
			due = t.DueDate.Format("2006-01-02")
		}
		status := "pending"
		if t.Done() {
			status = "completed"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %q (list: %s, due: %s)", status, t.Title, t.List, due))
	}
	return "This week's tasks:\n" + strings.Join(lines, "\n")
}

// ReviewWeek sends this week's tasks to the configured AI provider and
// returns its suggested follow-ups. now is passed in (rather than read
// internally) so the prompt's "today" always matches whatever range the
// caller used to select tasks.
func ReviewWeek(ctx context.Context, tasks []models.Task, now time.Time) ([]FollowUp, error) {
	info, err := coreai.Detect("TASKCTL")
	if err != nil {
		return nil, err
	}

	text, err := coreai.CallJSON(ctx, info, reviewSystemPrompt(now.Format("2006-01-02")), buildReviewPrompt(tasks))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("blank response from %s", info.Display)
	}
	return parseFollowUps(text)
}

// parseFollowUps extracts the follow-up list from the raw AI response text.
// Split out from ReviewWeek so it's testable without a live AI call.
func parseFollowUps(text string) ([]FollowUp, error) {
	// Strip any surrounding markdown code fence the model might add.
	text = strings.TrimSpace(text)
	if start := strings.Index(text, "["); start >= 0 {
		if end := strings.LastIndex(text, "]"); end > start {
			text = text[start : end+1]
		}
	}

	var suggestions []FollowUp
	if err := json.Unmarshal([]byte(text), &suggestions); err != nil {
		raw := text
		if len(raw) > 300 {
			raw = raw[:300] + "…"
		}
		return nil, fmt.Errorf("parse AI response: %w (raw: %s)", err, raw)
	}

	var result []FollowUp
	for _, s := range suggestions {
		if strings.TrimSpace(s.Title) != "" {
			result = append(result, s)
		}
	}
	return result, nil
}
