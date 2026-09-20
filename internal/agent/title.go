package agent

import (
	"context"
	_ "embed"
	"errors"
	"regexp"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	// defaultSessionTitle is the placeholder a session carries until its first turn names it.
	defaultSessionTitle = "New conversation"

	sessionTitleTimeout         = 30 * time.Second
	sessionTitleMaxOutputTokens = 512
	sessionTitleInputRunes      = 4000
	sessionTitleRunes           = 48
)

//go:embed prompts/session-title.md
var sessionTitlePrompt string

var (
	// sessionTitleThinkBlock matches the reasoning blocks models embed in a text response.
	sessionTitleThinkBlock = regexp.MustCompile(`(?is)<think(?:ing)?>.*?</think(?:ing)?>`)
	// sessionTitleMarkup matches one leading Markdown heading or bullet.
	sessionTitleMarkup = regexp.MustCompile(`^(?:#{1,6}|[-*+•])\s+`)
)

// startSessionTitleGeneration replaces the placeholder title of a fresh session with a summary of
// the user's request. The turn never waits for the summary: the session keeps the first-message
// title until the request settles, and keeps it for good when the request fails.
func (s *Service) startSessionTitleGeneration(adapter ModelAdapter, sessionID, runID, fallback, request string) {
	if s.closing.Load() {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Bound to the service context rather than the turn: a summary may settle after its turn
		// finished, but never after the application stops.
		ctx, cancel := context.WithTimeout(s.runCtx, sessionTitleTimeout)
		defer cancel()
		s.summarizeSessionTitle(ctx, adapter, sessionID, runID, fallback, request)
	}()
}

// summarizeSessionTitle asks the configured model for a title that summarizes the request and
// stores it, replacing the first-message title the session was opened with.
func (s *Service) summarizeSessionTitle(
	ctx context.Context,
	adapter ModelAdapter,
	sessionID, runID, fallback, request string,
) {
	title, err := s.generateSessionTitle(ctx, adapter, request)
	if err != nil {
		s.reportTitleFailure(err, sessionID, runID, "generate")
		return
	}
	client, err := s.dbClient()
	if err != nil {
		s.reportTitleFailure(err, sessionID, runID, "persist")
		return
	}
	renamed, err := client.AgentSessions.RenameIfUnchanged(
		ctx, sessionID, fallback, title, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		s.reportTitleFailure(err, sessionID, runID, "persist")
		return
	}
	if !renamed {
		// The user renamed the session while the summary was in flight; that title wins.
		return
	}
	s.emit(sessionID, runID, 0, "session-title", map[string]any{"title": title})
}

// generateSessionTitle asks the configured model for a short title for one user request. Reasoning
// stays at low effort; an endpoint that rejects reasoning_effort is retried without it once, since
// the summary is worth more than the effort hint.
func (s *Service) generateSessionTitle(ctx context.Context, adapter ModelAdapter, request string) (string, error) {
	input := "Create a session title for this user request:\n" +
		leadingRunes(strings.TrimSpace(request), sessionTitleInputRunes)

	var lastErr error
	for _, reasoning := range []string{"low", "auto"} {
		response, err := adapter.Complete(ctx, ModelRequest{
			System:          sessionTitlePrompt,
			Messages:        []Message{{Role: "user", Content: input}},
			MaxOutputTokens: sessionTitleMaxOutputTokens,
			Reasoning:       reasoning,
		}, func(string, string) {})
		if err != nil {
			lastErr = err
		} else {
			title := normalizeSessionTitle(response.Text)
			if title != "" {
				return title, nil
			}
			lastErr = errors.New("session title response carried no usable text")
		}
		if ctx.Err() != nil {
			break
		}
	}
	return "", lastErr
}

// normalizeSessionTitle reduces a model reply to one plain title line: models wrap the answer in
// reasoning blocks, quotes, or Markdown, while the session list shows a single short line.
func normalizeSessionTitle(raw string) string {
	cleaned := sessionTitleThinkBlock.ReplaceAllString(raw, "")
	if index := strings.Index(strings.ToLower(cleaned), "<think"); index >= 0 {
		// An unterminated reasoning block swallows the rest of the reply.
		cleaned = cleaned[:index]
	}
	for _, line := range strings.Split(cleaned, "\n") {
		title := sessionTitleMarkup.ReplaceAllString(strings.TrimSpace(line), "")
		title = strings.TrimSpace(strings.Trim(title, "*_`\"'“”‘’"))
		if head, rest, found := strings.Cut(title, ":"); found && strings.EqualFold(strings.TrimSpace(head), "title") {
			title = strings.TrimSpace(rest)
		}
		title = strings.Join(strings.Fields(title), " ")
		if title == "" {
			continue
		}
		return truncateRunes(title, sessionTitleRunes)
	}
	return ""
}

func (s *Service) reportTitleFailure(err error, sessionID, runID, stage string) {
	_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "agent-session", Stage: "generate-title-" + stage,
		Fields: map[string]any{"sessionId": sessionID, "runId": runID},
	})
}
