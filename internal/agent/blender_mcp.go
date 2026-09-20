package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Nahida Agent hosts an MCP server speaking directly to the Blender add-on's socket bridge over an
// in-memory transport. This replaces the separate `blender-mcp` Python bridge the official setup
// requires, so enabling the built-in Blender transport is enough to expose Blender tools.

const blenderServerImplementation = "nahida-blender"

// blenderTool is one exposed Blender tool: its MCP definition plus the handler that produces the
// Blender-side Python and interprets the add-on's reply.
type blenderTool struct {
	definition mcp.Tool
	handle     func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error)
}

// blenderToolSession owns the paired client/server sessions of the in-memory MCP connection.
type blenderToolSession struct {
	client *mcp.ClientSession
	server *mcp.ServerSession
}

// Close tears the pair down: closing the client ends the session, which lets the server side
// observe termination instead of leaking the in-memory pipe.
func (s *blenderToolSession) Close() error {
	return s.client.Close()
}

// newBlenderToolSession builds the in-memory MCP connection. The server must connect before the
// client because the client drives the MCP initialization handshake.
func newBlenderToolSession(ctx context.Context) (*blenderToolSession, error) {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := mcp.NewServer(&mcp.Implementation{
		Name:    blenderServerImplementation,
		Title:   "Blender (built-in)",
		Version: "1",
	}, nil)
	bindBlenderTools(server)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect built-in Blender MCP server: %w", err)
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "nahida-agent",
		Title:   "Nahida Agent",
		Version: "1",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		return nil, fmt.Errorf("connect built-in Blender MCP client: %w", err)
	}
	return &blenderToolSession{client: clientSession, server: serverSession}, nil
}

// bindBlenderTools registers every built-in Blender tool on the in-memory server.
func bindBlenderTools(server *mcp.Server) {
	for _, tool := range blenderTools() {
		handle := tool.handle
		server.AddTool(&tool.definition, func(
			ctx context.Context,
			request *mcp.CallToolRequest,
		) (*mcp.CallToolResult, error) {
			return handle(ctx, request.Params.Arguments)
		})
	}
}

// blenderTools returns the catalog in a stable order so tool listings are deterministic.
func blenderTools() []blenderTool {
	tools := []blenderTool{
		executeBlenderCodeTool(),
		getObjectsSummaryTool(),
		getObjectDetailSummaryTool(),
		getBlendfileSummaryDatablocksTool(),
		getBlendfileSummaryPathInfoTool(),
		getBlendfileSummaryMissingFilesTool(),
		getBlendfileSummaryOfLinkedLibrariesTool(),
		getScreenshotOfWindowAsJSONTool(),
		getScreenshotOfWindowAsImageTool(),
		getScreenshotOfAreaAsImageTool(),
		jumpToTabByNameTool(),
		jumpToTabBySpaceTypeTool(),
		jumpToView3DObjectByNameTool(),
		jumpToView3DObjectDataByNameTool(),
		renderViewportToPathTool(),
		renderThumbnailToPathTool(),
	}
	return tools
}

func newBlenderTool(name, title, description string, readOnly bool, schema map[string]any,
	handle func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error),
) blenderTool {
	destructive := !readOnly
	return blenderTool{
		definition: mcp.Tool{
			Name:        name,
			Title:       title,
			Description: description,
			InputSchema: schema,
			Annotations: &mcp.ToolAnnotations{
				Title:           title,
				ReadOnlyHint:    readOnly,
				DestructiveHint: &destructive,
			},
		},
		handle: handle,
	}
}

// decodeBlenderArguments unmarshals tool arguments, treating absent arguments as an empty object.
func decodeBlenderArguments(arguments json.RawMessage, target any) error {
	if len(arguments) == 0 {
		return nil
	}
	if err := json.Unmarshal(arguments, target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

// textResult renders a tool's payload as MCP text content.
func textResult(value any) *mcp.CallToolResult {
	if value == nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "No result."}}}
	}
	if text, ok := value.(string); ok {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprint(value)}}}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}
}

// errorResult reports a Blender-side failure as a tool error, which keeps the model in the loop
// instead of surfacing a transport failure to the agent runtime.
func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}

// imageResult returns a PNG payload as MCP image content so the model can look at the viewport.
// ImageContent carries base64-encoded data, while the add-on hands over raw file bytes.
func imageResult(data []byte) *mcp.CallToolResult {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(encoded, data)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{
		Data:     encoded,
		MIMEType: "image/png",
	}}}
}

// callBlender sends Python to the connected Blender instance and turns the reply into a tool
// result. A Blender-side exception becomes a tool error; only an unreachable Blender or a
// malformed reply becomes a Go error.
func callBlender(ctx context.Context, code string, strictJSON bool) (*mcp.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := executeBlenderCode(blenderEndpointFromEnv(), code, strictJSON)
	if err != nil {
		return nil, err
	}
	if response.Status != "ok" {
		return errorResult(blenderErrorText(response)), nil
	}
	return appendBlenderStdout(textResult(blenderResultText(response)), response.Stdout), nil
}

// appendBlenderStdout forwards output the Blender-side code printed, which is often where the
// interesting detail of a tool call ends up.
func appendBlenderStdout(result *mcp.CallToolResult, stdout string) *mcp.CallToolResult {
	if strings.TrimSpace(stdout) == "" {
		return result
	}
	result.Content = append(result.Content, &mcp.TextContent{Text: "stdout:\n" + stdout})
	return result
}

// objectSchema builds a JSON Schema object, mirroring the helper the agent tool definitions use.
func blenderObjectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func blenderStringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func blenderBoolProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func blenderIntProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

// blenderImageResult decodes the base64 PNG the screenshot tools return and forwards it as MCP
// image content, so the model receives the picture itself rather than a wall of base64.
func blenderImageResult(response *blenderResponse) (*mcp.CallToolResult, error) {
	if response.Status != "ok" {
		return errorResult(blenderErrorText(response)), nil
	}
	var payload struct {
		Status      string `json:"status"`
		ImageBase64 string `json:"image_base64"`
		Message     string `json:"message"`
	}
	if err := json.Unmarshal(response.Result, &payload); err != nil {
		return errorResult(fmt.Sprintf("unexpected screenshot response: %v", err)), nil
	}
	// The tool-code reports its own failures inside the result envelope, matching the reference
	// bridge which raises there instead of returning image bytes.
	if payload.Status != "ok" {
		message := payload.Message
		if message == "" {
			message = "Blender did not return a screenshot."
		}
		return errorResult(message), nil
	}
	data, err := base64.StdEncoding.DecodeString(payload.ImageBase64)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid screenshot payload: %v", err)), nil
	}
	return imageResult(data), nil
}

// callBlenderForImage runs a screenshot program and returns the captured PNG as image content.
func callBlenderForImage(ctx context.Context, code string) (*mcp.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := executeBlenderCode(blenderEndpointFromEnv(), code, true)
	if err != nil {
		return nil, err
	}
	return blenderImageResult(response)
}
