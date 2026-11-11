package ui

import (
	"encoding/json"
	"math"
	"time"

	"nahida.live/desktop/internal/agent"
)

// agentEntry is a chat entry as the agent screen shows it. Live entries are built from the stream of the run
// in progress and are replaced by the stored snapshot when the run ends.
type agentEntry struct {
	agent.AgentChatEntry
	streaming          bool
	reasoningStreaming bool
}

// acceptAgentEvent drops duplicate, stale and out-of-order events of a run.
func acceptAgentEvent(latest map[string]int64, event agent.AgentStreamEvent) bool {
	if event.Sequence <= latest[event.RunID] {
		return false
	}
	latest[event.RunID] = event.Sequence
	return true
}

// updateAgentLiveEntries applies one assistant, reasoning or tool event to the live entries of a run.
func updateAgentLiveEntries(entries []agentEntry, event agent.AgentStreamEvent) []agentEntry {
	switch event.Type {
	case "assistant-delta", "reasoning-delta":
		return appendAgentDelta(entries, event)
	case "tool-start":
		name, id := agentPayloadString(event.Payload, "toolName"), agentPayloadString(event.Payload, "toolCallId")
		if call, ok := event.Payload.(agent.ToolCall); ok {
			name, id = call.Name, call.ID
		}
		if name == "" {
			name = agentPayloadString(event.Payload, "name")
		}
		if id == "" {
			id = agentPayloadString(event.Payload, "id")
		}
		return append(settleAgentAssistant(entries), agentEntry{AgentChatEntry: agent.AgentChatEntry{
			Sequence: event.Sequence, TurnID: event.RunID, Type: "tool/start", ToolName: name, ToolCallID: id,
			CreatedAt: agentNow(),
		}})
	case "tool-end":
	default:
		return entries
	}

	payload, _ := event.Payload.(map[string]any)
	done := agentEntry{AgentChatEntry: agent.AgentChatEntry{
		Sequence:   event.Sequence,
		TurnID:     event.RunID,
		Type:       "tool/end",
		ToolName:   agentPayloadString(payload, "toolName"),
		ToolCallID: agentPayloadString(payload, "toolCallId"),
		Error:      agentPayloadString(payload, "error"),
		CreatedAt:  agentNow(),
	}}
	if result, ok := payload["result"]; ok && result != nil {
		done.Result = agentRawJSON(result)
	}
	done.ChangedFiles, _ = payload["changedFiles"].([]string)
	done.Images, _ = payload["images"].([]agent.AgentImage)

	if done.ToolCallID != "" {
		for i := range entries {
			if entries[i].ToolCallID == done.ToolCallID {
				done.Sequence = entries[i].Sequence
				next := append([]agentEntry(nil), entries...)
				next[i] = done
				return next
			}
		}
	}
	return append(entries, done)
}

func appendAgentDelta(entries []agentEntry, event agent.AgentStreamEvent) []agentEntry {
	delta := agentPayloadString(event.Payload, "delta")
	text := event.Type == "assistant-delta"
	var last *agentEntry
	if n := len(entries); n > 0 {
		last = &entries[n-1]
	}
	continuing := last != nil && last.Role == "assistant" && last.TurnID == event.RunID &&
		((text && last.streaming) || (!text && last.reasoningStreaming))

	var next agentEntry
	if continuing {
		next = *last
	} else {
		next = agentEntry{AgentChatEntry: agent.AgentChatEntry{
			Sequence: event.Sequence, TurnID: event.RunID, Type: "message/assistant", Role: "assistant",
			CreatedAt: agentNow(),
		}}
	}
	if text {
		next.Text += delta
		next.streaming, next.reasoningStreaming = true, false
	} else {
		next.Reasoning += delta
		next.reasoningStreaming = true
	}

	if continuing {
		out := append([]agentEntry(nil), entries[:len(entries)-1]...)
		return append(out, next)
	}
	return append(settleAgentAssistant(entries), next)
}

// settleAgentAssistant ends the streaming of the last assistant entry, as a new entry starts after it.
func settleAgentAssistant(entries []agentEntry) []agentEntry {
	n := len(entries)
	if n == 0 || entries[n-1].Role != "assistant" {
		return entries
	}
	out := append([]agentEntry(nil), entries...)
	out[n-1].streaming, out[n-1].reasoningStreaming = false, false
	return out
}

func agentApprovalEvent(eventType string) bool {
	return eventType == "approval-requested" || eventType == "approval-updated"
}

// agentPendingApproval reports whether an approval still blocks the composer.
func agentPendingApproval(approvals []agent.AgentApproval) bool {
	for _, approval := range approvals {
		if approval.Status == "pending" || approval.Status == "executing" {
			return true
		}
	}
	return false
}

// agentContextOccupancy is the composer meter reading. It uses the projection rather than the provider-reported
// sample so a compaction shows immediately. ok is false until the context window is known.
func agentContextOccupancy(usage *agent.AgentContextUsage) (percent int, used int64, ok bool) {
	if usage == nil || usage.ContextWindow <= 0 {
		return 0, 0, false
	}
	used = usage.ProjectedTokens
	percent = int(math.Round(float64(used) / float64(usage.ContextWindow) * 100))
	return min(percent, 100), used, true
}

func agentPayloadString(payload any, key string) string {
	values, _ := payload.(map[string]any)
	text, _ := values[key].(string)
	return text
}

func agentRawJSON(value any) json.RawMessage {
	if raw, ok := value.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func agentNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
