package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nahida.live/desktop/internal/appdata"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00}

func TestDecodeImageInputsValidatesAttachments(t *testing.T) {
	t.Parallel()
	valid := AgentImageInput{
		Name: "shot.png", MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(testPNG),
	}
	tests := []struct {
		name    string
		inputs  []AgentImageInput
		wantErr string
	}{
		{name: "valid", inputs: []AgentImageInput{valid}},
		{name: "mime type is normalized", inputs: []AgentImageInput{{
			Name: "shot.png", MIMEType: " IMAGE/PNG ", Data: valid.Data,
		}}},
		{name: "unsupported type", inputs: []AgentImageInput{{
			MIMEType: "image/tiff", Data: valid.Data,
		}}, wantErr: "unsupported image type"},
		{name: "invalid base64", inputs: []AgentImageInput{{
			MIMEType: "image/png", Data: "not-base64!",
		}}, wantErr: "decode image"},
		{name: "empty payload", inputs: []AgentImageInput{{MIMEType: "image/png"}}, wantErr: "is empty"},
		{name: "too many images", inputs: []AgentImageInput{valid, valid, valid, valid, valid},
			wantErr: "at most 4 images"},
		{name: "oversized payload", inputs: []AgentImageInput{{
			Name: "huge.png", MIMEType: "image/png", Data: strings.Repeat("A", maxImageBytes/3*4+8),
		}}, wantErr: "exceeds the 5 MB limit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			images, err := decodeImageInputs(test.inputs)
			if test.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(images) != len(test.inputs) || images[0].MIMEType != "image/png" {
					t.Fatalf("images = %#v", images)
				}
				if string(images[0].Data) != string(testPNG) {
					t.Fatalf("decoded data = %q", images[0].Data)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestSplitImageContentExtractsImages(t *testing.T) {
	t.Parallel()
	encoded := base64.StdEncoding.EncodeToString(testPNG)
	result := &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: "summary"},
		&mcp.ImageContent{Data: []byte(encoded), MIMEType: "image/png"},
		&mcp.ImageContent{Data: []byte(encoded), MIMEType: "image/tiff"},
	}}

	trimmed, images := splitImageContent(result)
	if trimmed == result {
		t.Fatal("a result with images must be copied, not reused")
	}
	if len(images) != 1 || images[0].MIMEType != "image/png" || string(images[0].Data) != string(testPNG) {
		t.Fatalf("images = %#v", images)
	}
	if len(trimmed.Content) != 3 {
		t.Fatalf("trimmed content = %#v", trimmed.Content)
	}
	placeholder, ok := trimmed.Content[1].(*mcp.TextContent)
	if !ok || placeholder.Text != fmt.Sprintf("[image: image/png, %d bytes]", len(testPNG)) {
		t.Fatalf("placeholder = %#v", trimmed.Content[1])
	}
	if _, kept := trimmed.Content[2].(*mcp.ImageContent); !kept {
		t.Fatal("an unsupported image type must stay in the tool result")
	}
	if _, kept := result.Content[1].(*mcp.ImageContent); !kept {
		t.Fatal("the original result was modified")
	}

	if trimmed, images := splitImageContent(nil); trimmed != nil || images != nil {
		t.Fatalf("splitImageContent(nil) = %#v, %#v", trimmed, images)
	}
}

func TestStoreAndLoadMessageImagesRoundTrip(t *testing.T) {
	t.Parallel()
	service := New(Options{})
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(store); err != nil {
		t.Fatal(err)
	}

	stored, err := service.storeImages("session-1", []pendingImage{{
		Name: "shot.png", MIMEType: "image/png", Data: testPNG,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored = %#v", stored)
	}
	if !filepath.IsAbs(stored[0].Src) ||
		!strings.HasPrefix(stored[0].Path, filepath.Join("agent", "images", "session-1")) {
		t.Fatalf("stored image = %#v", stored[0])
	}
	if stored[0].Bytes != len(testPNG) || stored[0].MIMEType != "image/png" || stored[0].Name != "shot.png" {
		t.Fatalf("stored metadata = %#v", stored[0])
	}
	if _, err := os.Stat(stored[0].Src); err != nil {
		t.Fatalf("stored file is missing: %v", err)
	}

	parts := service.loadMessageImages(stored)
	if len(parts) != 1 || parts[0].MIMEType != "image/png" {
		t.Fatalf("parts = %#v", parts)
	}
	if parts[0].Data != base64.StdEncoding.EncodeToString(testPNG) {
		t.Fatalf("part data = %q", parts[0].Data)
	}

	missing := service.loadMessageImages([]AgentImage{{
		Path: filepath.Join("agent", "images", "session-1", "gone.png"), MIMEType: "image/png",
	}})
	if len(missing) != 0 {
		t.Fatalf("missing images = %#v", missing)
	}
	if parts := (&Service{}).loadMessageImages(stored); parts != nil {
		t.Fatalf("images without a store = %#v", parts)
	}
}

func TestMessageTitlePrefersTextThenAttachmentName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		text   string
		images []AgentImage
		want   string
	}{
		{name: "text wins", text: " hello ", images: []AgentImage{{Name: "shot.png"}}, want: "hello"},
		{name: "image name", text: "  ", images: []AgentImage{{Name: "shot.png"}}, want: "shot.png"},
		{name: "unnamed image", images: []AgentImage{{MIMEType: "image/png"}}, want: "Image"},
		{name: "no content", want: "Image"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := messageTitle(test.text, test.images); got != test.want {
				t.Fatalf("messageTitle = %q, want %q", got, test.want)
			}
		})
	}

	if got := messageTitle(strings.Repeat("a", 60), nil); len([]rune(got)) != 49 {
		t.Fatalf("long titles = %q", got)
	}
}

func TestExecutorExtractsToolResultImages(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	server.AddTool(
		&mcp.Tool{Name: "screenshot", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return imageResult(testPNG), nil
		},
	)
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	executor := &toolExecutor{mcp: &mcpRuntime{tools: map[string]mcpRuntimeTool{
		"mcp__test__screenshot": {session: session, name: "screenshot"},
	}}}
	result, err := executor.Execute(ctx, ToolCall{
		ID: "call-1", Name: "mcp__test__screenshot", Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Images) != 1 || string(result.Images[0].Data) != string(testPNG) {
		t.Fatalf("images = %#v", result.Images)
	}
	encoded, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), base64.StdEncoding.EncodeToString(testPNG)) {
		t.Fatalf("tool output kept the base64 payload: %s", encoded)
	}
	if !strings.Contains(string(encoded), "[image: image/png") {
		t.Fatalf("tool output is missing the image placeholder: %s", encoded)
	}
}

func TestSummarizeMessagesDropsImagePayloads(t *testing.T) {
	t.Parallel()
	messages := summarizeMessages([]Message{
		{Role: "user", Content: "look at this"},
		{
			Role:    "user",
			Content: "text",
			Images:  []MessageImage{{MIMEType: "image/png", Data: strings.Repeat("A", 400)}},
		},
	})
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if len(messages[1].Images) != 0 {
		t.Fatalf("image payloads survived compaction: %#v", messages[1].Images)
	}
	if !strings.Contains(messages[1].Content, "[image/png image, 300 bytes]") {
		t.Fatalf("content = %q", messages[1].Content)
	}
}
