package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderStreamMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		protocol      string
		stream        string
		wantText      string
		wantReasoning string
		wantTool      string
	}{
		{protocol: "openai-compatible", wantText: "hello", wantReasoning: "inspect files", wantTool: "read_file",
			stream: strings.Join([]string{
				`data: {"choices":[{"delta":{"reasoning_content":"inspect "}}]}`,
				`data: {"choices":[{"delta":{"reasoning_content":"files"}}]}`,
				`data: {"choices":[{"delta":{"content":"hello","tool_calls":[{"index":0,"id":"call-1","function":{"name":"read_file","arguments":"{\"rootId\":\"r\"}"}}]}}]}`,
				`data: {"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`,
				"data: [DONE]",
				"",
			}, "\n\n")},
		{protocol: "openai-responses", wantText: "hello", wantTool: "read_file", stream: strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"item-1","call_id":"call-1","name":"read_file","arguments":""}}`,
			`data: {"type":"response.function_call_arguments.delta","item_id":"item-1","delta":"{\"rootId\":\"r\"}"}`,
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":3}}}`,
			"data: [DONE]",
			"",
		}, "\n\n")},
		{protocol: "anthropic", wantText: "hello", wantTool: "read_file", stream: strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":2}}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call-1","name":"read_file"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"rootId\":\"r\"}"}}`,
			`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
			"data: [DONE]",
			"",
		}, "\n\n")},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(response, test.stream)
			}))
			defer server.Close()
			adapter := newModelAdapter(
				modelAdapterConfig{
					settings: AgentSettingsView{Protocol: test.protocol, Endpoint: server.URL, Model: "test"},
				},
				server.Client(),
			)
			result, err := adapter.Complete(context.Background(), ModelRequest{
				System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 10,
			}, func(string, string) {})
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != test.wantText || len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != test.wantTool {
				t.Fatalf("result = %#v", result)
			}
			if result.Reasoning != test.wantReasoning {
				t.Fatalf("reasoning = %q, want %q", result.Reasoning, test.wantReasoning)
			}
			if result.InputTokens != 2 || result.OutputTokens != 3 {
				t.Fatalf("usage = %d/%d", result.InputTokens, result.OutputTokens)
			}
		})
	}
}

func TestProviderPreservesToolCallOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		protocol string
		stream   string
	}{
		{
			name:     "openai compatible",
			protocol: "openai-compatible",
			stream: strings.Join([]string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":2,"id":"c3","function":{"name":"third","arguments":"{}"}},{"index":0,"id":"c1","function":{"name":"first","arguments":"{}"}},{"index":1,"id":"c2","function":{"name":"second","arguments":"{}"}}]}}]}`,
				"data: [DONE]",
				"",
			}, "\n\n"),
		},
		{
			name:     "openai responses",
			protocol: "openai-responses",
			stream: strings.Join([]string{
				`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"i3","call_id":"c3","name":"third","arguments":"{}"}}`,
				`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"first","arguments":"{}"}}`,
				`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"i2","call_id":"c2","name":"second","arguments":"{}"}}`,
				"data: [DONE]",
				"",
			}, "\n\n"),
		},
		{
			name:     "anthropic",
			protocol: "anthropic",
			stream: strings.Join([]string{
				`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"c3","name":"third"}}`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"first"}}`,
				`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c2","name":"second"}}`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
				`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
				`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
				"data: [DONE]",
				"",
			}, "\n\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(response, test.stream)
			}))
			defer server.Close()

			adapter := newModelAdapter(
				modelAdapterConfig{
					settings: AgentSettingsView{Protocol: test.protocol, Endpoint: server.URL, Model: "test"},
				},
				server.Client(),
			)
			result, err := adapter.Complete(context.Background(), ModelRequest{}, func(string, string) {})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ToolCalls) != 3 || result.ToolCalls[0].Name != "first" ||
				result.ToolCalls[1].Name != "second" || result.ToolCalls[2].Name != "third" {
				t.Fatalf("tool calls = %#v", result.ToolCalls)
			}
		})
	}
}

func TestChatCompletionsPreservesAssistantReasoning(t *testing.T) {
	t.Parallel()
	type wireMessage struct {
		Role             string `json:"role"`
		ReasoningContent string `json:"reasoning_content"`
		ToolCalls        []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
	}
	type wireRequest struct {
		Messages []wireMessage `json:"messages"`
	}
	received := make(chan wireRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body wireRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		received <- body
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	adapter := newModelAdapter(
		modelAdapterConfig{
			settings: AgentSettingsView{Protocol: "openai-compatible", Endpoint: server.URL, Model: "test"},
		},
		server.Client(),
	)
	_, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test",
		Messages: []Message{
			{
				Role:      "assistant",
				Reasoning: "inspect files",
				ToolCalls: []ToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{}`)}},
			},
			{Role: "tool", ToolCallID: "call-1", Content: `{"ok":true}`},
		},
		MaxOutputTokens: 10,
	}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}

	body := <-received
	if len(body.Messages) != 3 {
		t.Fatalf("messages = %#v", body.Messages)
	}
	if body.Messages[1].ReasoningContent != "inspect files" {
		t.Fatalf("assistant reasoning_content = %q", body.Messages[1].ReasoningContent)
	}
	if len(body.Messages[1].ToolCalls) != 1 || body.Messages[1].ToolCalls[0].ID != "call-1" {
		t.Fatalf("assistant tool_calls = %#v", body.Messages[1].ToolCalls)
	}
	if body.Messages[2].ReasoningContent != "" {
		t.Fatalf("tool reasoning_content = %q", body.Messages[2].ReasoningContent)
	}
}

// TestProviderSendsReasoningEffort pins where each protocol carries the effort: the OpenAI
// protocols take it next to the model, and Anthropic takes it inside its output configuration.
func TestProviderSendsReasoningEffort(t *testing.T) {
	t.Parallel()
	request := ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}},
		MaxOutputTokens: 10, Reasoning: "high",
	}
	tests := []struct {
		protocol string
		read     func(body map[string]any) any
	}{
		{protocol: protocolOpenAICompatible, read: func(body map[string]any) any {
			return body["reasoning_effort"]
		}},
		{protocol: protocolOpenAIResponses, read: func(body map[string]any) any {
			section, _ := body["reasoning"].(map[string]any)
			return section["effort"]
		}},
		{protocol: protocolAnthropic, read: func(body map[string]any) any {
			section, _ := body["output_config"].(map[string]any)
			return section["effort"]
		}},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			t.Parallel()
			if effort := test.read(captureProviderRequest(t, test.protocol, request)); effort != "high" {
				t.Fatalf("effort = %#v, want high", effort)
			}
		})
	}
}

// TestProviderOmitsAutomaticReasoningEffort keeps "auto" off the wire so every endpoint applies
// its own default.
func TestProviderOmitsAutomaticReasoningEffort(t *testing.T) {
	t.Parallel()
	request := ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}},
		MaxOutputTokens: 10, Reasoning: "auto",
	}
	for _, protocol := range []string{protocolOpenAICompatible, protocolOpenAIResponses, protocolAnthropic} {
		body := captureProviderRequest(t, protocol, request)
		for _, field := range []string{"reasoning", "reasoning_effort", "output_config"} {
			if _, present := body[field]; present {
				t.Fatalf("%s sent %s for the automatic effort: %#v", protocol, field, body[field])
			}
		}
	}
}

func TestProviderCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter := newModelAdapter(
		modelAdapterConfig{
			settings: AgentSettingsView{Protocol: "openai-compatible", Endpoint: server.URL, Model: "test"},
		},
		server.Client(),
	)
	_, err := adapter.Complete(
		ctx,
		ModelRequest{Messages: []Message{{Role: "user", Content: "test"}}},
		func(string, string) {},
	)
	if err == nil {
		t.Fatal("Complete unexpectedly ignored cancellation")
	}
}

func TestProviderSendsConfiguredHeaders(t *testing.T) {
	t.Parallel()
	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		received <- request.Header.Clone()
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()
	adapter := newModelAdapter(
		modelAdapterConfig{
			settings:   AgentSettingsView{Protocol: "openai-compatible", Endpoint: server.URL, Model: "test"},
			credential: providerCredential{Type: credentialKindAPI, Key: "api-key"},
			headers:    map[string]string{"X-Title": "Nahida Desktop", "Authorization": "Bearer custom"},
		},
		server.Client(),
	)
	if _, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 10,
	}, func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	headers := <-received
	if headers.Get("X-Title") != "Nahida Desktop" {
		t.Fatalf("custom header = %q", headers.Get("X-Title"))
	}
	if headers.Get("Authorization") != "Bearer custom" {
		t.Fatalf("authorization = %q", headers.Get("Authorization"))
	}
	if headers.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", headers.Get("Content-Type"))
	}
}
