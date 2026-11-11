package ui

import (
	"encoding/json"
	"testing"

	"nahida.live/desktop/internal/agent"
)

func TestAcceptAgentEventDropsStaleEventsPerRun(t *testing.T) {
	latest := map[string]int64{}
	steps := []struct {
		run  string
		seq  int64
		want bool
	}{
		{"a", 2, true}, {"a", 2, false}, {"a", 1, false}, {"a", 3, true}, {"b", 1, true},
	}
	for _, step := range steps {
		got := acceptAgentEvent(latest, agent.AgentStreamEvent{RunID: step.run, Sequence: step.seq})
		if got != step.want {
			t.Fatalf("accept(%s, %d) = %v, want %v", step.run, step.seq, got, step.want)
		}
	}
}

func TestUpdateAgentLiveEntriesKeepsRoundOrder(t *testing.T) {
	event := func(seq int64, kind string, payload any) agent.AgentStreamEvent {
		return agent.AgentStreamEvent{SessionID: "session", RunID: "run", Sequence: seq, Type: kind, Payload: payload}
	}
	var entries []agentEntry
	for _, e := range []agent.AgentStreamEvent{
		event(1, "assistant-delta", map[string]any{"delta": "조사 "}),
		event(2, "assistant-delta", map[string]any{"delta": "시작"}),
		event(3, "tool-start", agent.ToolCall{ID: "tool-1", Name: "read_file"}),
		event(4, "tool-end", map[string]any{
			"toolName": "read_file", "toolCallId": "tool-1", "result": json.RawMessage(`{"ok":true}`),
		}),
		event(5, "reasoning-delta", map[string]any{"delta": "결과 확인"}),
		event(6, "assistant-delta", map[string]any{"delta": "완료"}),
	} {
		entries = updateAgentLiveEntries(entries, e)
	}

	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}
	if e := entries[0]; e.Text != "조사 시작" || e.streaming {
		t.Errorf("first message = %q streaming=%v, want settled text", e.Text, e.streaming)
	}
	if e := entries[1]; e.Type != "tool/end" || e.ToolCallID != "tool-1" || e.Sequence != 3 ||
		string(e.Result) != `{"ok":true}` {
		t.Errorf("tool entry = %+v, want tool-1 ended in place of its start", e)
	}
	if e := entries[2]; e.Reasoning != "결과 확인" || e.reasoningStreaming || e.Text != "" {
		t.Errorf("reasoning entry = %+v, want settled reasoning only", e)
	}
	if e := entries[3]; e.Text != "완료" || !e.streaming {
		t.Errorf("last message = %q streaming=%v, want streaming text", e.Text, e.streaming)
	}
}

func TestAgentPendingApprovalBlocksOnlyOpenApprovals(t *testing.T) {
	if agentPendingApproval([]agent.AgentApproval{{Status: "completed"}, {Status: "failed"}}) {
		t.Error("finished approvals block the composer")
	}
	if !agentPendingApproval([]agent.AgentApproval{{Status: "failed"}, {Status: "executing"}}) {
		t.Error("an executing approval does not block the composer")
	}
}
