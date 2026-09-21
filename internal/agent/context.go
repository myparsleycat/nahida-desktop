package agent

import (
	"encoding/json"

	"nahida.live/desktop/internal/db"
)

// contextUsageEventType names the durable event that anchors provider-reported usage. It is
// appended after each successful model response so a later replay can price the next request
// against the provider's own prompt size.
const contextUsageEventType = "turn/usage"

// tokenBreakdown is the heuristic composition of one request surface. The three parts are
// independent estimates, not provider-billed buckets.
type tokenBreakdown struct {
	System   int
	Tools    int
	Messages int
}

// contextUsagePayload is the durable body of a contextUsageEventType event. Provider and Model
// identify the route the provider usage belongs to; a route change invalidates the anchor.
type contextUsagePayload struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	SystemTokens  int    `json:"systemTokens"`
	ToolsTokens   int    `json:"toolsTokens"`
	MessageTokens int    `json:"messageTokens"`
}

// contextAnchor is the newest provider-reported prompt measurement together with the heuristic
// breakdown of that same prompt.
type contextAnchor struct {
	Provider      string
	Model         string
	InputTokens   int64
	SystemTokens  int
	ToolsTokens   int
	MessageTokens int
}

// estimateBreakdown prices a request surface with the fixed heuristic. The per-message and
// per-tool-call framing overheads match estimateTokens exactly, which it now sums.
func estimateBreakdown(system string, messages []Message, tools []ToolDefinition) tokenBreakdown {
	breakdown := tokenBreakdown{System: estimateTextTokens(system)}
	for _, message := range messages {
		breakdown.Messages += estimateTextTokens(message.Content) + estimateTextTokens(message.Reasoning) + 8
		for _, image := range message.Images {
			breakdown.Messages += estimateMessageImageTokens(image)
		}
		for _, call := range message.ToolCalls {
			breakdown.Messages += estimateTextTokens(call.Name) + estimateTextTokens(string(call.Arguments)) + 8
		}
	}
	if len(tools) > 0 {
		encoded, err := json.Marshal(tools)
		if err == nil {
			breakdown.Tools += estimateTextTokens(string(encoded))
		}
	}
	return breakdown
}

func (b tokenBreakdown) total() int {
	return b.System + b.Tools + b.Messages
}

// imageReferences keeps stored image metadata without reading payloads, so a token estimate can
// price an image from its decoded size alone.
func imageReferences(references []AgentImage) []MessageImage {
	images := make([]MessageImage, 0, len(references))
	for _, reference := range references {
		images = append(images, MessageImage{MIMEType: reference.MIMEType, Bytes: reference.Bytes})
	}
	return images
}

// estimateMessageImageTokens approximates what a provider charges for one image. A replay that must
// not read payloads from disk carries only the decoded size, so the base64 length is derived from
// it instead.
func estimateMessageImageTokens(image MessageImage) int {
	encoded := len(image.Data)
	if encoded == 0 {
		encoded = image.Bytes * 4 / 3
	}
	if encoded == 0 {
		return 0
	}
	return estimateImageTokens(encoded)
}

// parseContextAnchor returns the newest usable usage anchor, or nil when the session never recorded
// a provider-reported prompt size.
func parseContextAnchor(events []db.AgentEventRow) *contextAnchor {
	var anchor *contextAnchor
	for _, event := range events {
		if event.EventType != contextUsageEventType {
			continue
		}
		var payload contextUsagePayload
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
			continue
		}
		if payload.InputTokens <= 0 {
			continue
		}
		anchor = &contextAnchor{
			Provider:      payload.Provider,
			Model:         payload.Model,
			InputTokens:   payload.InputTokens,
			SystemTokens:  payload.SystemTokens,
			ToolsTokens:   payload.ToolsTokens,
			MessageTokens: payload.MessageTokens,
		}
	}
	return anchor
}

// buildContextUsage resolves the display occupancy of one session. The provider anchor supplies the
// exact scale of the last measured prompt, while the live surface is repriced heuristically so a
// compaction or an appended turn moves the projection immediately. A missing capacity, or an anchor
// recorded for another provider or model, falls back to the heuristic total.
func buildContextUsage(
	window int,
	provider, model, system string,
	messages []Message,
	tools []ToolDefinition,
	anchor *contextAnchor,
) (AgentContextUsage, bool) {
	if window <= 0 {
		return AgentContextUsage{}, false
	}
	live := estimateBreakdown(system, messages, tools)
	usage := AgentContextUsage{
		ContextWindow:   window,
		ProjectedTokens: int64(live.total()),
		SystemTokens:    live.System,
		ToolsTokens:     live.Tools,
		MessageTokens:   live.Messages,
	}
	if anchor == nil || anchor.Provider != provider || anchor.Model != model {
		return usage, true
	}

	// The anchor priced system and messages of an earlier prompt; the signed delta carries every
	// surface change since, including the negative delta a compaction produces. Tools stay at the
	// anchored value: their schema rarely changes mid-session and the read path does not rebuild the
	// MCP tool set the request used.
	delta := int64(live.System+live.Messages) - int64(anchor.SystemTokens+anchor.MessageTokens)
	projected := max(anchor.InputTokens+delta, 0)
	usage.PressureTokens = anchor.InputTokens
	usage.ToolsTokens = anchor.ToolsTokens
	usage.ProjectedTokens = projected
	return usage, true
}
