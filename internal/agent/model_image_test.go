package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// imageRequest captures the provider request body so each protocol can be checked for the image
// content its wire format requires.
func imageRequest(t *testing.T, protocol string) map[string]any {
	t.Helper()
	image := MessageImage{MIMEType: "image/png", Data: "QUJD"}
	return captureProviderRequest(t, protocol, ModelRequest{
		System: "test",
		Messages: []Message{
			{Role: "user", Content: "look", Images: []MessageImage{image}},
			{Role: "tool", ToolCallID: "call-1", Content: `{"ok":true}`, Images: []MessageImage{image}},
		},
		MaxOutputTokens: 10,
	})
}

// captureProviderRequest sends one request and returns the body the provider received.
func captureProviderRequest(t *testing.T, protocol string, request ModelRequest) map[string]any {
	t.Helper()
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, raw *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(raw.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		received <- body
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	adapter := newModelAdapter(
		modelAdapterConfig{settings: AgentSettingsView{Protocol: protocol, Endpoint: server.URL, Model: "test"}},
		server.Client(),
	)
	if _, err := adapter.Complete(context.Background(), request, func(string, string) {}); err != nil {
		t.Fatal(err)
	}

	select {
	case body := <-received:
		return body
	case <-time.After(5 * time.Second):
		t.Fatal("provider request was not received")
		return nil
	}
}

func TestChatCompletionsSendsImages(t *testing.T) {
	t.Parallel()
	body := imageRequest(t, "openai-compatible")
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("messages = %#v", body["messages"])
	}

	parts, ok := messages[1].(map[string]any)["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("user content = %#v", messages[1])
	}
	if parts[0].(map[string]any)["text"] != "look" {
		t.Fatalf("user text part = %#v", parts[0])
	}
	url := parts[1].(map[string]any)["image_url"].(map[string]any)["url"]
	if parts[1].(map[string]any)["type"] != "image_url" || url != "data:image/png;base64,QUJD" {
		t.Fatalf("user image part = %#v", parts[1])
	}

	// The tool role stays textual, so the image follows as its own user turn.
	tool := messages[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call-1" {
		t.Fatalf("tool message = %#v", tool)
	}
	if _, isText := tool["content"].(string); !isText {
		t.Fatalf("tool content = %#v", tool["content"])
	}
	followUp, ok := messages[3].(map[string]any)
	if !ok || followUp["role"] != "user" {
		t.Fatalf("follow-up message = %#v", messages[3])
	}
	followParts := followUp["content"].([]any)
	if len(followParts) != 2 || followParts[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("follow-up content = %#v", followParts)
	}
}

// TestChatCompletionsDefersToolImagesUntilToolRunEnds reproduces a provider-rejected request: every
// tool_call of one assistant message must be answered by tool messages before the image user turn.
func TestChatCompletionsDefersToolImagesUntilToolRunEnds(t *testing.T) {
	t.Parallel()
	image := MessageImage{MIMEType: "image/png", Data: "QUJD"}
	body := captureProviderRequest(t, "openai-compatible", ModelRequest{
		System: "test",
		Messages: []Message{
			{Role: "user", Content: "check the hairstyle"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "call-1", Name: "screenshot", Arguments: json.RawMessage(`{}`)},
				{ID: "call-2", Name: "detail", Arguments: json.RawMessage(`{}`)},
			}},
			{Role: "tool", ToolCallID: "call-1", Content: `{"ok":true}`, Images: []MessageImage{image}},
			{Role: "tool", ToolCallID: "call-2", Content: `{"ok":true}`},
		},
		MaxOutputTokens: 10,
	})

	messages, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %#v", body["messages"])
	}

	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.(map[string]any)["role"].(string))
	}
	want := []string{"system", "user", "assistant", "tool", "tool", "user"}
	if !slices.Equal(roles, want) {
		t.Fatalf("roles = %#v", roles)
	}
	if messages[3].(map[string]any)["tool_call_id"] != "call-1" ||
		messages[4].(map[string]any)["tool_call_id"] != "call-2" {
		t.Fatalf("tool messages = %#v", messages[3:5])
	}

	followParts, ok := messages[5].(map[string]any)["content"].([]any)
	if !ok || len(followParts) != 2 {
		t.Fatalf("tool image content = %#v", messages[5])
	}
	if followParts[0].(map[string]any)["text"] != "Images returned by the preceding tool call:" ||
		followParts[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("tool image parts = %#v", followParts)
	}
}

func TestResponsesSendsImages(t *testing.T) {
	t.Parallel()
	body := imageRequest(t, "openai-responses")
	input, ok := body["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("input = %#v", body["input"])
	}

	parts, ok := input[0].(map[string]any)["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("user content = %#v", input[0])
	}
	if parts[0].(map[string]any)["type"] != "input_text" || parts[1].(map[string]any)["type"] != "input_image" {
		t.Fatalf("user content parts = %#v", parts)
	}
	if parts[1].(map[string]any)["image_url"] != "data:image/png;base64,QUJD" {
		t.Fatalf("user image part = %#v", parts[1])
	}

	output, ok := input[1].(map[string]any)["output"].([]any)
	if !ok || len(output) != 2 {
		t.Fatalf("function call output = %#v", input[1])
	}
	if output[1].(map[string]any)["type"] != "input_image" {
		t.Fatalf("tool image part = %#v", output[1])
	}
}

func TestAnthropicSendsImages(t *testing.T) {
	t.Parallel()
	body := imageRequest(t, "anthropic")
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", body["messages"])
	}

	blocks, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("user content = %#v", messages[0])
	}
	image, ok := blocks[1].(map[string]any)
	if !ok || image["type"] != "image" {
		t.Fatalf("user image block = %#v", blocks[1])
	}
	source := image["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "QUJD" {
		t.Fatalf("image source = %#v", source)
	}

	toolBlocks := messages[1].(map[string]any)["content"].([]any)
	toolResult, ok := toolBlocks[0].(map[string]any)
	if !ok || toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call-1" {
		t.Fatalf("tool result = %#v", toolBlocks[0])
	}
	resultContent, ok := toolResult["content"].([]any)
	if !ok || len(resultContent) != 2 || resultContent[1].(map[string]any)["type"] != "image" {
		t.Fatalf("tool result content = %#v", toolResult["content"])
	}
}

// TestAnthropicGroupsToolResults covers parallel tool calls: every tool_result of one assistant
// tool_use turn belongs to a single user message.
func TestAnthropicGroupsToolResults(t *testing.T) {
	t.Parallel()
	image := MessageImage{MIMEType: "image/png", Data: "QUJD"}
	body := captureProviderRequest(t, "anthropic", ModelRequest{
		System: "test",
		Messages: []Message{
			{Role: "user", Content: "check the hairstyle"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "call-1", Name: "screenshot", Arguments: json.RawMessage(`{}`)},
				{ID: "call-2", Name: "detail", Arguments: json.RawMessage(`{}`)},
			}},
			{Role: "tool", ToolCallID: "call-1", Content: `{"ok":true}`, Images: []MessageImage{image}},
			{Role: "tool", ToolCallID: "call-2", Content: `{"ok":true}`},
		},
		MaxOutputTokens: 10,
	})

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages = %#v", body["messages"])
	}
	results, ok := messages[2].(map[string]any)["content"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("tool results = %#v", messages[2])
	}
	if messages[2].(map[string]any)["role"] != "user" ||
		results[0].(map[string]any)["tool_use_id"] != "call-1" ||
		results[1].(map[string]any)["tool_use_id"] != "call-2" {
		t.Fatalf("grouped tool results = %#v", results)
	}

	blocks, ok := results[0].(map[string]any)["content"].([]any)
	if !ok || len(blocks) != 2 || blocks[1].(map[string]any)["type"] != "image" {
		t.Fatalf("tool image block = %#v", results[0])
	}
}
