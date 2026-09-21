package agent

import (
	"encoding/json"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestEstimateBreakdownSumMatchesEstimateTokens(t *testing.T) {
	t.Parallel()
	system := "You are a helpful modding assistant."
	messages := []Message{
		{Role: "user", Content: "inspect this mod", Images: []MessageImage{{MIMEType: "image/png", Data: "QUJD"}}},
		{Role: "assistant", Content: "checking", Reasoning: "look at the ini", ToolCalls: []ToolCall{{
			ID: "call", Name: "read_file", Arguments: json.RawMessage(`{"path":"mod.ini"}`),
		}}},
	}
	tools := []ToolDefinition{{Name: "read_file", Description: "read", InputSchema: map[string]any{"type": "object"}}}

	breakdown := estimateBreakdown(system, messages, tools)
	if breakdown.total() != estimateTokens(system, messages, tools) {
		t.Fatalf("breakdown = %#v, estimateTokens = %d", breakdown, estimateTokens(system, messages, tools))
	}
}

func TestBuildContextUsageFallsBackWithoutAnchor(t *testing.T) {
	t.Parallel()
	system := "system"
	messages := []Message{{Role: "user", Content: "hello world"}}
	tools := []ToolDefinition{{Name: "read_file", Description: "read"}}

	want := estimateBreakdown(system, messages, tools)
	usage, ok := buildContextUsage(128_000, "openai", "gpt", system, messages, tools, nil)
	if !ok {
		t.Fatal("usage is unavailable with a known window")
	}
	if usage.PressureTokens != 0 {
		t.Fatalf("pressureTokens = %d, want 0 without an anchor", usage.PressureTokens)
	}
	if usage.ProjectedTokens != int64(want.total()) {
		t.Fatalf("projectedTokens = %d, want %d", usage.ProjectedTokens, want.total())
	}
	if usage.ToolsTokens != want.Tools || usage.SystemTokens != want.System || usage.MessageTokens != want.Messages {
		t.Fatalf("breakdown = %#v", usage)
	}
}

func TestBuildContextUsageAnchorsProviderPromptSize(t *testing.T) {
	t.Parallel()
	system := "hello"
	messages := []Message{{Role: "user", Content: "world"}}
	anchor := &contextAnchor{
		Provider: "openai", Model: "gpt", InputTokens: 1_000,
		SystemTokens: 10, ToolsTokens: 500, MessageTokens: 400,
	}

	usage, ok := buildContextUsage(128_000, "openai", "gpt", system, messages, nil, anchor)
	if !ok {
		t.Fatal("usage is unavailable with a known window")
	}
	if usage.PressureTokens != 1_000 {
		t.Fatalf("pressureTokens = %d, want 1000", usage.PressureTokens)
	}
	if usage.SystemTokens != 2 || usage.MessageTokens != 10 {
		t.Fatalf("breakdown = %#v", usage)
	}
	if usage.ToolsTokens != 500 {
		t.Fatalf("toolsTokens = %d, want the anchored 500", usage.ToolsTokens)
	}
	// The live surface shrank by 398 tokens against the anchored prompt, so the projection follows.
	if usage.ProjectedTokens != 602 {
		t.Fatalf("projectedTokens = %d, want 602", usage.ProjectedTokens)
	}
}

func TestBuildContextUsageRejectsForeignRoute(t *testing.T) {
	t.Parallel()
	system := "hello"
	messages := []Message{{Role: "user", Content: "world"}}
	anchor := &contextAnchor{Provider: "openai", Model: "other", InputTokens: 1_000, MessageTokens: 400}

	usage, ok := buildContextUsage(128_000, "openai", "gpt", system, messages, nil, anchor)
	if !ok {
		t.Fatal("usage is unavailable with a known window")
	}
	if usage.PressureTokens != 0 {
		t.Fatalf("pressureTokens = %d, want 0 for a foreign route", usage.PressureTokens)
	}
	if usage.ProjectedTokens != int64(estimateBreakdown(system, messages, nil).total()) {
		t.Fatalf("projectedTokens = %d, want the heuristic total", usage.ProjectedTokens)
	}
}

func TestBuildContextUsageClampsShrinkingProjection(t *testing.T) {
	t.Parallel()
	messages := []Message{{Role: "user", Content: "small"}}
	anchor := &contextAnchor{Provider: "openai", Model: "gpt", InputTokens: 100, MessageTokens: 10_000}

	usage, ok := buildContextUsage(128_000, "openai", "gpt", "", messages, nil, anchor)
	if !ok {
		t.Fatal("usage is unavailable with a known window")
	}
	if usage.ProjectedTokens != 0 {
		t.Fatalf("projectedTokens = %d, want a clamped 0", usage.ProjectedTokens)
	}
	if usage.PressureTokens != 100 {
		t.Fatalf("pressureTokens = %d, want 100", usage.PressureTokens)
	}
}

func TestBuildContextUsageRequiresCapacity(t *testing.T) {
	t.Parallel()
	if _, ok := buildContextUsage(0, "openai", "gpt", "system", nil, nil, nil); ok {
		t.Fatal("usage is available without a context window")
	}
}

func TestEstimateMessageImageTokensUsesStoredSize(t *testing.T) {
	t.Parallel()
	stored := estimateMessageImageTokens(MessageImage{MIMEType: "image/png", Bytes: 4_096})
	if want := estimateImageTokens(4_096 * 4 / 3); stored != want {
		t.Fatalf("stored-size tokens = %d, want %d", stored, want)
	}
	if empty := estimateMessageImageTokens(MessageImage{MIMEType: "image/png"}); empty != 0 {
		t.Fatalf("empty image tokens = %d, want 0", empty)
	}
}

func TestParseContextAnchorKeepsNewestUsableEvent(t *testing.T) {
	t.Parallel()
	events := []db.AgentEventRow{
		{EventType: contextUsageEventType, Payload: `{"provider":"openai","model":"gpt","inputTokens":10}`},
		{EventType: contextUsageEventType, Payload: `{"provider":"openai","model":"gpt","inputTokens":0}`},
		{
			EventType: contextUsageEventType,
			Payload:   `{"provider":"openai","model":"gpt","inputTokens":250,"messageTokens":7}`,
		},
		{EventType: "turn/end", Payload: `{}`},
	}

	anchor := parseContextAnchor(events)
	if anchor == nil {
		t.Fatal("anchor is nil")
	}
	if anchor.InputTokens != 250 || anchor.MessageTokens != 7 {
		t.Fatalf("anchor = %#v", anchor)
	}
}

func TestImageReferencesKeepStoredSizeWithoutPayload(t *testing.T) {
	t.Parallel()
	references := imageReferences([]AgentImage{{MIMEType: "image/png", Bytes: 1_024, Path: "agent/images/a.png"}})
	if len(references) != 1 {
		t.Fatalf("references = %#v", references)
	}
	if references[0].Bytes != 1_024 || references[0].Data != "" {
		t.Fatalf("reference = %#v", references[0])
	}
}
