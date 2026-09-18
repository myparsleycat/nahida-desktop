package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/platform"
)

// codexEndpoint serves ChatGPT Pro/Plus requests; the plan does not expose api.openai.com.
const (
	codexEndpoint    = "https://chatgpt.com/backend-api/codex"
	codexOriginator  = "nahida-desktop"
	anthropicVersion = "2023-06-01"
)

type httpModelAdapter struct {
	settings      AgentSettingsView
	credential    providerCredential
	headers       map[string]string
	sessionID     string
	oauthEndpoint string
	client        *http.Client
	refresh       func(ctx context.Context, current providerCredential) (providerCredential, error)
	mu            sync.Mutex
}

// modelAdapterConfig wires one adapter to the active provider, its stored credential, and the
// headers resolved for this turn.
type modelAdapterConfig struct {
	settings   AgentSettingsView
	credential providerCredential
	headers    map[string]string
	sessionID  string
	// oauthEndpoint overrides where an account login sends its requests; tests point it at a
	// local server.
	oauthEndpoint string
	refresh       func(ctx context.Context, current providerCredential) (providerCredential, error)
}

func newModelAdapter(config modelAdapterConfig, client *http.Client) ModelAdapter {
	return &httpModelAdapter{
		settings:      config.settings,
		credential:    config.credential,
		headers:       config.headers,
		sessionID:     config.sessionID,
		oauthEndpoint: config.oauthEndpoint,
		client:        client,
		refresh:       config.refresh,
	}
}

// providerRoute is how one provider request is addressed: the base URL, the wire protocol, and
// whether the request goes to the ChatGPT backend, which needs its own request shape.
type providerRoute struct {
	endpoint string
	protocol string
	codex    bool
}

func (a *httpModelAdapter) route(credential providerCredential) providerRoute {
	route := providerRoute{endpoint: a.settings.Endpoint, protocol: a.settings.Protocol}
	if a.settings.Provider == providerOpenAI && credential.Type == credentialKindOAuth {
		route.endpoint = a.oauthEndpoint
		if route.endpoint == "" {
			route.endpoint = codexEndpoint
		}
		route.protocol = protocolOpenAIResponses
		route.codex = true
	}
	return route
}

// requestHeaders merges the provider headers with configured headers and adds the Codex headers a
// ChatGPT login needs.
func (a *httpModelAdapter) requestHeaders(credential providerCredential) map[string]string {
	headers := make(map[string]string, len(a.headers)+3)
	for name, value := range a.headers {
		headers[name] = value
	}
	if a.settings.Provider == providerOpenAI && credential.Type == credentialKindOAuth {
		headers["originator"] = codexOriginator
		if a.sessionID != "" {
			headers["session-id"] = a.sessionID
		}
		if credential.AccountID != "" {
			headers["ChatGPT-Account-Id"] = credential.AccountID
		}
	}
	return headers
}

func (a *httpModelAdapter) currentCredential() providerCredential {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.credential
}

// refreshCredential renews an expiring OAuth token and stores the result on the adapter.
func (a *httpModelAdapter) refreshCredential(ctx context.Context) (providerCredential, error) {
	if a.refresh == nil {
		return providerCredential{}, errors.New("provider credentials cannot be refreshed")
	}
	refreshed, err := a.refresh(ctx, a.currentCredential())
	if err != nil {
		return providerCredential{}, err
	}
	a.mu.Lock()
	a.credential = refreshed
	a.mu.Unlock()
	return refreshed, nil
}

func (a *httpModelAdapter) Complete(
	ctx context.Context,
	request ModelRequest,
	emit func(kind, delta string),
) (ModelResponse, error) {
	// The route decides the protocol: a ChatGPT login always speaks Responses, whatever the
	// stored settings say.
	switch a.route(a.currentCredential()).protocol {
	case protocolOpenAIResponses:
		return a.responses(ctx, request, emit)
	case protocolOpenAICompatible:
		return a.chatCompletions(ctx, request, emit)
	case protocolAnthropic:
		return a.anthropic(ctx, request, emit)
	default:
		return ModelResponse{}, fmt.Errorf("unsupported protocol %q", a.settings.Protocol)
	}
}

func (a *httpModelAdapter) chatCompletions(
	ctx context.Context,
	request ModelRequest,
	emit func(kind, delta string),
) (ModelResponse, error) {
	messages := []map[string]any{{"role": "system", "content": request.System}}

	// Chat completions keeps the tool role textual, so tool images ride on a user message. The
	// message is deferred until the tool run ends: an assistant message with tool_calls must be
	// followed by tool messages for every call before any other role appears.
	var toolImages []MessageImage
	toolCallsWithImages := 0
	flushToolImages := func() {
		if len(toolImages) == 0 {
			return
		}
		messages = append(messages, toolImageMessage(toolImages, toolCallsWithImages))
		toolImages = nil
		toolCallsWithImages = 0
	}

	for _, message := range request.Messages {
		if message.Role != "tool" {
			flushToolImages()
		}

		item := map[string]any{"role": message.Role, "content": message.Content}
		if len(message.Images) > 0 && message.Role != "tool" {
			item["content"] = openAIContentParts(message.Content, message.Images)
		}
		if message.Role == "assistant" && message.Reasoning != "" {
			item["reasoning_content"] = message.Reasoning
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{
					"id": call.ID, "type": "function",
					"function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)},
				})
			}
			item["tool_calls"] = calls
		}
		messages = append(messages, item)
		if message.Role == "tool" && len(message.Images) > 0 {
			toolImages = append(toolImages, message.Images...)
			toolCallsWithImages++
		}
	}
	// A tool round ends the message list, so the trailing images need an explicit flush.
	flushToolImages()

	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
			"name": tool.Name, "description": tool.Description, "parameters": tool.InputSchema,
		}})
	}
	body := map[string]any{
		"model": a.settings.Model, "messages": messages, "tools": tools, "stream": true,
		"stream_options": map[string]any{"include_usage": true}, "max_tokens": request.MaxOutputTokens,
	}
	if request.Reasoning != "" && request.Reasoning != "auto" {
		body["reasoning_effort"] = request.Reasoning
	}
	var response ModelResponse
	type partialCall struct{ id, name, arguments string }
	partial := make([]*partialCall, 0)
	err := a.stream(ctx, "/chat/completions", body, func(event string, data []byte) error {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return err
		}
		response.InputTokens = chunk.Usage.PromptTokens
		response.OutputTokens = chunk.Usage.CompletionTokens
		for _, choice := range chunk.Choices {
			if choice.Delta.ReasoningContent != "" {
				response.Reasoning += choice.Delta.ReasoningContent
				emit("reasoning-delta", choice.Delta.ReasoningContent)
			}
			if choice.Delta.Content != "" {
				response.Text += choice.Delta.Content
				emit("assistant-delta", choice.Delta.Content)
			}
			for _, delta := range choice.Delta.ToolCalls {
				for len(partial) <= delta.Index {
					partial = append(partial, nil)
				}
				call := partial[delta.Index]
				if call == nil {
					call = &partialCall{}
					partial[delta.Index] = call
				}
				call.id += delta.ID
				call.name += delta.Function.Name
				call.arguments += delta.Function.Arguments
			}
		}
		return nil
	})
	for _, call := range partial {
		if call != nil {
			response.ToolCalls = append(
				response.ToolCalls,
				ToolCall{ID: call.id, Name: call.name, Arguments: json.RawMessage(call.arguments)},
			)
		}
	}
	return response, err
}

func (a *httpModelAdapter) responses(
	ctx context.Context,
	request ModelRequest,
	emit func(kind, delta string),
) (ModelResponse, error) {
	input := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		switch {
		case message.Role == "tool":
			input = append(
				input,
				map[string]any{
					"type":    "function_call_output",
					"call_id": message.ToolCallID,
					"output":  responsesContent("input_text", message.Content, message.Images),
				},
			)
		case len(message.ToolCalls) > 0:
			if message.Content != "" {
				input = append(input, map[string]any{"role": "assistant", "content": message.Content})
			}
			for _, call := range message.ToolCalls {
				input = append(
					input,
					map[string]any{
						"type":      "function_call",
						"call_id":   call.ID,
						"name":      call.Name,
						"arguments": string(call.Arguments),
					},
				)
			}
		default:
			input = append(
				input,
				map[string]any{
					"role":    message.Role,
					"content": responsesContent("input_text", message.Content, message.Images),
				},
			)
		}
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description,
			"parameters": tool.InputSchema, "strict": false})
	}
	body := map[string]any{"model": a.settings.Model, "instructions": request.System, "input": input, "tools": tools,
		"stream": true, "max_output_tokens": request.MaxOutputTokens}
	if request.Reasoning != "" && request.Reasoning != "auto" {
		body["reasoning"] = map[string]any{"effort": request.Reasoning}
	}
	var response ModelResponse
	type partialCall struct {
		id, name, arguments string
		index, arrival      int
	}
	partial := map[string]*partialCall{}
	order := make([]string, 0)
	err := a.stream(ctx, "/responses", body, func(event string, data []byte) error {
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		typeName, _ := value["type"].(string)
		delta, _ := value["delta"].(string)
		switch typeName {
		case "response.output_text.delta":
			response.Text += delta
			emit("assistant-delta", delta)
		case "response.reasoning_summary_text.delta":
			response.Reasoning += delta
			emit("reasoning-delta", delta)
		case "response.output_item.added", "response.output_item.done":
			item, _ := value["item"].(map[string]any)
			if item["type"] == "function_call" {
				itemID, _ := item["id"].(string)
				callID, _ := item["call_id"].(string)
				call := partial[itemID]
				if call == nil {
					index := len(order)
					if _, present := value["output_index"]; present {
						index = int(numberToInt64(value["output_index"]))
					}
					call = &partialCall{id: callID, index: index, arrival: len(order)}
					partial[itemID] = call
					order = append(order, itemID)
				}
				if name, ok := item["name"].(string); ok {
					call.name = name
				}
				if arguments, ok := item["arguments"].(string); ok && arguments != "" {
					call.arguments = arguments
				}
			}
		case "response.function_call_arguments.delta":
			id, _ := value["item_id"].(string)
			if call := partial[id]; call != nil {
				call.arguments += delta
			}
		case "response.completed":
			completed, _ := value["response"].(map[string]any)
			usage, _ := completed["usage"].(map[string]any)
			response.InputTokens = numberToInt64(usage["input_tokens"])
			response.OutputTokens = numberToInt64(usage["output_tokens"])
		case "response.failed":
			return fmt.Errorf("provider response failed: %s", string(data))
		}
		return nil
	})
	sort.SliceStable(order, func(left, right int) bool {
		leftCall, rightCall := partial[order[left]], partial[order[right]]
		if leftCall.index == rightCall.index {
			return leftCall.arrival < rightCall.arrival
		}
		return leftCall.index < rightCall.index
	})
	for _, itemID := range order {
		call := partial[itemID]
		response.ToolCalls = append(
			response.ToolCalls,
			ToolCall{ID: call.id, Name: call.name, Arguments: json.RawMessage(call.arguments)},
		)
	}
	return response, err
}

func (a *httpModelAdapter) anthropic(
	ctx context.Context,
	request ModelRequest,
	emit func(kind, delta string),
) (ModelResponse, error) {
	messages := make([]map[string]any, 0, len(request.Messages))
	for index := 0; index < len(request.Messages); index++ {
		message := request.Messages[index]

		// Anthropic wants one user message carrying the tool_result blocks for every tool_use of
		// the preceding assistant message, and rejects results split across several messages.
		if message.Role == "tool" {
			results := make([]map[string]any, 0, 1)
			for ; index < len(request.Messages) && request.Messages[index].Role == "tool"; index++ {
				tool := request.Messages[index]
				results = append(results, map[string]any{
					"type": "tool_result", "tool_use_id": tool.ToolCallID,
					"content": anthropicContent(tool.Content, tool.Images),
				})
			}
			index--
			messages = append(messages, map[string]any{"role": "user", "content": results})
			continue
		}

		content := anthropicContent(message.Content, message.Images)
		for _, call := range message.ToolCalls {
			var input any
			if err := json.Unmarshal(call.Arguments, &input); err != nil {
				return ModelResponse{}, err
			}
			content = append(
				content,
				map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input},
			)
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": content})
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(
			tools,
			map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.InputSchema},
		)
	}
	body := map[string]any{"model": a.settings.Model, "system": request.System, "messages": messages, "tools": tools,
		"stream": true, "max_tokens": request.MaxOutputTokens}
	// Anthropic carries the effort inside its output configuration; the other protocols put it
	// next to the model.
	if request.Reasoning != "" && request.Reasoning != "auto" {
		body["output_config"] = map[string]any{"effort": request.Reasoning}
	}
	var response ModelResponse
	type partialCall struct{ id, name, arguments string }
	partial := make([]*partialCall, 0)
	err := a.stream(ctx, "/messages", body, func(event string, data []byte) error {
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		typeName, _ := value["type"].(string)
		switch typeName {
		case "content_block_start":
			index := int(numberToInt64(value["index"]))
			for len(partial) <= index {
				partial = append(partial, nil)
			}
			block, _ := value["content_block"].(map[string]any)
			if block["type"] == "tool_use" {
				partial[index] = &partialCall{id: stringValue(block["id"]), name: stringValue(block["name"])}
			}
		case "content_block_delta":
			index := int(numberToInt64(value["index"]))
			delta, _ := value["delta"].(map[string]any)
			switch delta["type"] {
			case "text_delta":
				text := stringValue(delta["text"])
				response.Text += text
				emit("assistant-delta", text)
			case "thinking_delta":
				thinking := stringValue(delta["thinking"])
				response.Reasoning += thinking
				emit("reasoning-delta", thinking)
			case "input_json_delta":
				if index >= 0 && index < len(partial) && partial[index] != nil {
					call := partial[index]
					call.arguments += stringValue(delta["partial_json"])
				}
			}
		case "message_delta":
			usage, _ := value["usage"].(map[string]any)
			response.OutputTokens = numberToInt64(usage["output_tokens"])
		case "message_start":
			message, _ := value["message"].(map[string]any)
			usage, _ := message["usage"].(map[string]any)
			response.InputTokens = numberToInt64(usage["input_tokens"])
		case "error":
			return fmt.Errorf("provider response failed: %s", string(data))
		}
		return nil
	})
	for _, call := range partial {
		if call != nil {
			response.ToolCalls = append(
				response.ToolCalls,
				ToolCall{ID: call.id, Name: call.name, Arguments: json.RawMessage(call.arguments)},
			)
		}
	}
	return response, err
}

// providerAuthError marks a response the provider rejected because the credential is invalid or
// expired, which is the only failure an OAuth refresh can retry safely.
type providerAuthError struct {
	status int
	body   string
}

func (e *providerAuthError) Error() string {
	return fmt.Sprintf("provider authentication failed (%d): %s", e.status, e.body)
}

func (a *httpModelAdapter) stream(
	ctx context.Context,
	path string,
	body any,
	handle func(event string, data []byte) error,
) error {
	err := a.attempt(ctx, path, body, handle)
	if err == nil {
		return nil
	}
	var authErr *providerAuthError
	if !errors.As(err, &authErr) {
		return err
	}
	// Only an account login can be renewed; a rejected API key is final.
	if current := a.currentCredential(); current.Type != credentialKindOAuth || current.Refresh == "" {
		return err
	}
	if refreshed, refreshErr := a.refreshCredential(ctx); refreshErr == nil {
		if refreshed.Type != credentialKindOAuth {
			return err
		}
		return a.attempt(ctx, path, body, handle)
	}
	return err
}

func (a *httpModelAdapter) attempt(
	ctx context.Context,
	path string,
	body any,
	handle func(event string, data []byte) error,
) error {
	credential := a.currentCredential()
	if credential.needsRefresh(time.Now()) {
		refreshed, err := a.refreshCredential(ctx)
		if err != nil {
			if credential.Access == "" {
				return err
			}
		} else {
			credential = refreshed
		}
	}
	route := a.route(credential)

	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if route.codex {
		encoded = codexRequestBody(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, route.endpoint+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("User-Agent", platform.UserAgent())
	if route.protocol == protocolAnthropic {
		request.Header.Set("anthropic-version", anthropicVersion)
	}
	if key := credential.apiKey(); key != "" {
		if route.protocol == protocolAnthropic {
			request.Header.Set("x-api-key", key)
		} else {
			request.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if credential.Type == credentialKindOAuth && credential.Access != "" {
		request.Header.Set("Authorization", "Bearer "+credential.Access)
	}
	// Configured headers are applied last so they can override protocol defaults.
	for name, value := range a.requestHeaders(credential) {
		request.Header.Set(name, value)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		body := strings.TrimSpace(string(data))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return &providerAuthError{status: response.StatusCode, body: body}
		}
		return fmt.Errorf("provider request failed (%d): %s", response.StatusCode, body)
	}
	return parseSSE(response.Body, handle)
}

// codexRequestBody adapts a Responses request for the ChatGPT backend: the Codex endpoint rejects
// an output-token limit, which it replaces with its own default, and expects stateless requests.
func codexRequestBody(encoded []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return encoded
	}
	delete(fields, "max_output_tokens")
	fields["store"] = json.RawMessage("false")
	adapted, err := json.Marshal(fields)
	if err != nil {
		return encoded
	}
	return adapted
}

func parseSSE(body io.Reader, handle func(event string, data []byte) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := bytes.TrimSpace([]byte(strings.TrimPrefix(line, "data:")))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		if err := handle(event, data); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// imageDataURL renders one image as the data URL both OpenAI protocols accept.
func imageDataURL(image MessageImage) string {
	return "data:" + image.MIMEType + ";base64," + image.Data
}

// openAIContentParts renders a text message that carries images as a content part list.
func openAIContentParts(text string, images []MessageImage) []map[string]any {
	parts := make([]map[string]any, 0, len(images)+1)
	if text != "" {
		parts = append(parts, map[string]any{"type": "text", "text": text})
	}
	for _, image := range images {
		parts = append(parts, map[string]any{
			"type": "image_url", "image_url": map[string]any{"url": imageDataURL(image)},
		})
	}
	return parts
}

// toolImageMessage carries images that arrived on a text-only tool role. Chat completions only
// accepts images on user content, so the tool images follow the tool messages as their own turn.
func toolImageMessage(images []MessageImage, toolCalls int) map[string]any {
	label := "Images returned by the preceding tool call:"
	if toolCalls > 1 {
		label = "Images returned by the preceding tool calls:"
	}

	parts := make([]map[string]any, 0, len(images)+1)
	parts = append(parts, map[string]any{"type": "text", "text": label})
	return map[string]any{"role": "user", "content": append(parts, openAIContentParts("", images)...)}
}

// responsesContent renders a message body as plain text, or as content items when it carries
// images. Function call outputs use the same item shape, so both share this helper.
func responsesContent(textType, text string, images []MessageImage) any {
	if len(images) == 0 {
		return text
	}
	parts := make([]map[string]any, 0, len(images)+1)
	if text != "" {
		parts = append(parts, map[string]any{"type": textType, "text": text})
	}
	for _, image := range images {
		parts = append(parts, map[string]any{"type": "input_image", "image_url": imageDataURL(image)})
	}
	return parts
}

// anthropicContent renders text plus images as Anthropic content blocks.
func anthropicContent(text string, images []MessageImage) []map[string]any {
	content := make([]map[string]any, 0, len(images)+1)
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, image := range images {
		content = append(content, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type": "base64", "media_type": image.MIMEType, "data": image.Data,
			},
		})
	}
	return content
}

func numberToInt64(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case json.Number:
		result, _ := number.Int64()
		return result
	default:
		return 0
	}
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "context_length") || strings.Contains(message, "context window") ||
		strings.Contains(message, "too many tokens")
}
