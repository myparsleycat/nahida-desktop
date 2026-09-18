package agent

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBlenderAddon speaks the add-on's socket protocol: it accepts one connection, reads a
// NUL-terminated JSON request and answers with a NUL-terminated JSON response.
type fakeBlenderAddon struct {
	listener net.Listener
	mu       sync.Mutex
	requests []blenderExecuteRequest
	// respond builds the reply for a request. Nil means "answer with a canned ok result".
	respond func(blenderExecuteRequest) map[string]any
}

func startFakeBlenderAddon(t *testing.T) *fakeBlenderAddon {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addon := &fakeBlenderAddon{listener: listener}
	t.Cleanup(func() { _ = listener.Close() })

	go addon.serve()
	return addon
}

func (a *fakeBlenderAddon) serve() {
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			return
		}
		go a.handle(conn)
	}
}

func (a *fakeBlenderAddon) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	buffer := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for indexNUL(buffer) < 0 {
		read, err := conn.Read(chunk)
		if err != nil {
			return
		}
		buffer = append(buffer, chunk[:read]...)
	}

	var request blenderExecuteRequest
	if err := json.Unmarshal(buffer[:indexNUL(buffer)], &request); err != nil {
		return
	}
	a.mu.Lock()
	a.requests = append(a.requests, request)
	respond := a.respond
	a.mu.Unlock()

	response := map[string]any{"status": "ok", "result": map[string]any{"ok": true}}
	if respond != nil {
		response = respond(request)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(payload, 0))
}

func (a *fakeBlenderAddon) endpoint() blenderEndpoint {
	address := a.listener.Addr().(*net.TCPAddr)
	return blenderEndpoint{Host: "127.0.0.1", Port: address.Port}
}

func (a *fakeBlenderAddon) recorded() []blenderExecuteRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]blenderExecuteRequest(nil), a.requests...)
}

func (a *fakeBlenderAddon) lastCode(t *testing.T) string {
	t.Helper()
	requests := a.recorded()
	if len(requests) == 0 {
		t.Fatal("add-on received no request")
	}
	return requests[len(requests)-1].Code
}

// pointBlenderAt makes the built-in transport talk to the fake add-on for the duration of one
// test. It uses a scoped override rather than the BLENDER_MCP_* environment so the tests can run
// in parallel.
func pointBlenderAt(t *testing.T, addon *fakeBlenderAddon) {
	t.Helper()
	endpoint := addon.endpoint()
	blenderEndpointOverride = &endpoint
	t.Cleanup(func() { blenderEndpointOverride = nil })
}

// callBuiltInTool drives one tool through a real MCP session over the in-memory transport, so the
// test covers registration, schema, dispatch and result rendering together.
func callBuiltInTool(t *testing.T, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()

	ctx := t.Context()
	session, err := newBlenderToolSession(ctx)
	if err != nil {
		t.Fatalf("open built-in Blender session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var text strings.Builder
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		t.Fatalf("result carried no text content: %#v", result.Content)
	}
	return text.String()
}

func TestBuiltInBlenderToolsMatchReferenceCatalog(t *testing.T) {
	t.Parallel()

	want := []string{
		"execute_blender_code",
		"get_objects_summary",
		"get_object_detail_summary",
		"get_blendfile_summary_datablocks",
		"get_blendfile_summary_path_info",
		"get_blendfile_summary_missing_files",
		"get_blendfile_summary_of_linked_libraries",
		"get_screenshot_of_window_as_json",
		"get_screenshot_of_window_as_image",
		"get_screenshot_of_area_as_image",
		"jump_to_tab_by_name",
		"jump_to_tab_by_space_type",
		"jump_to_view3d_object_by_name",
		"jump_to_view3d_object_data_by_name",
		"render_viewport_to_path",
		"render_thumbnail_to_path",
	}
	got := make([]string, 0, len(blenderTools()))
	for _, tool := range blenderTools() {
		got = append(got, tool.definition.Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("catalog mismatch\n got: %v\nwant: %v", got, want)
	}

	for _, tool := range blenderTools() {
		if tool.definition.Description == "" {
			t.Errorf("%s has no description", tool.definition.Name)
		}
		if tool.definition.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.definition.Name)
		}
		if tool.definition.Annotations == nil {
			t.Errorf("%s has no annotations", tool.definition.Name)
		}
	}
}

// TestBuiltInBlenderRenderToolsKeepReferenceAnnotations pins the annotations to the reference
// bridge, including the quirk that render_viewport_to_path writes a file yet is read-only.
func TestBuiltInBlenderRenderToolsKeepReferenceAnnotations(t *testing.T) {
	t.Parallel()

	readOnly := map[string]bool{
		"execute_blender_code":                      false,
		"get_objects_summary":                       true,
		"get_object_detail_summary":                 true,
		"get_blendfile_summary_datablocks":          true,
		"get_blendfile_summary_path_info":           true,
		"get_blendfile_summary_missing_files":       true,
		"get_blendfile_summary_of_linked_libraries": true,
		"get_screenshot_of_window_as_json":          true,
		"get_screenshot_of_window_as_image":         true,
		"get_screenshot_of_area_as_image":           true,
		"jump_to_tab_by_name":                       false,
		"jump_to_tab_by_space_type":                 false,
		"jump_to_view3d_object_by_name":             false,
		"jump_to_view3d_object_data_by_name":        false,
		"render_viewport_to_path":                   true,
		"render_thumbnail_to_path":                  false,
	}
	for _, tool := range blenderTools() {
		want, ok := readOnly[tool.definition.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tool.definition.Name)
		}
		if got := tool.definition.Annotations.ReadOnlyHint; got != want {
			t.Errorf("%s readOnlyHint = %v, want %v", tool.definition.Name, got, want)
		}
		if tool.definition.Annotations.DestructiveHint == nil {
			t.Errorf("%s has no destructiveHint", tool.definition.Name)
			continue
		}
		if got := *tool.definition.Annotations.DestructiveHint; got == want {
			t.Errorf("%s destructiveHint = %v, which contradicts readOnlyHint %v",
				tool.definition.Name, got, want)
		}
	}
}

func TestBuiltInBlenderToolListExposesEveryTool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	session, err := newBlenderToolSession(ctx)
	if err != nil {
		t.Fatalf("open built-in Blender session: %v", err)
	}
	defer func() { _ = session.Close() }()

	listed, err := session.client.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != len(blenderTools()) {
		t.Fatalf("listed %d tools, want %d", len(listed.Tools), len(blenderTools()))
	}
}

func TestBuiltInBlenderExecutesSentCodeAgainstAddon(t *testing.T) {
	addon := startFakeBlenderAddon(t)
	pointBlenderAt(t, addon)

	result := callBuiltInTool(t, "execute_blender_code", map[string]any{"code": "result = {'a': 1}"})
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, result))
	}

	requests := addon.recorded()
	if len(requests) != 1 {
		t.Fatalf("add-on saw %d requests, want 1", len(requests))
	}
	if requests[0].Type != "execute" {
		t.Errorf("request type = %q, want execute", requests[0].Type)
	}
	// The reference tool runs model-generated code verbatim and non-strictly, so non-serializable
	// values degrade to repr() instead of failing the call.
	if requests[0].Code != "result = {'a': 1}" {
		t.Errorf("code = %q, want the caller's code verbatim", requests[0].Code)
	}
	if requests[0].StrictJSON {
		t.Error("execute_blender_code must use strict_json=false")
	}
}

func TestBuiltInBlenderProgramsUseCallingConvention(t *testing.T) {
	cases := []struct {
		tool     string
		args     map[string]any
		wantMain string
	}{
		{"get_objects_summary", nil, "main(None)"},
		{"get_blendfile_summary_path_info", nil, "main(None)"},
		{"get_object_detail_summary", map[string]any{"name": "Cube"}, "main(Params(name='Cube'))"},
		{"jump_to_tab_by_name", map[string]any{"name": "Layout"}, "main(Params(name='Layout'))"},
		{
			"jump_to_view3d_object_by_name",
			map[string]any{"name": "Cube", "allow_edits": true},
			"main(Params(name='Cube', allow_edits=True))",
		},
		{
			"get_screenshot_of_window_as_image",
			map[string]any{"size_limit_in_bytes": 900000},
			"main(Params(size_limit_in_bytes=900000))",
		},
		{
			"get_screenshot_of_area_as_image",
			map[string]any{"area_ui_type": "VIEW_3D"},
			"main(Params(area_ui_type='VIEW_3D', size_limit_in_bytes=0))",
		},
		{
			"render_thumbnail_to_path",
			map[string]any{"output_path": `C:\tmp\thumb.png`},
			`main(Params(output_path='C:\\tmp\\thumb.png'))`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.tool, func(t *testing.T) {
			addon := startFakeBlenderAddon(t)
			pointBlenderAt(t, addon)

			callBuiltInTool(t, testCase.tool, testCase.args)
			code := addon.lastCode(t)
			if !strings.Contains(code, testCase.wantMain) {
				t.Fatalf("generated code does not contain %q:\n%s", testCase.wantMain, code)
			}
			// The calling convention is what arms the add-on's deferred response support.
			if !strings.Contains(code, "check_is_finished = _rv") {
				t.Errorf("generated code omits the deferred calling convention:\n%s", code)
			}
		})
	}
}

func TestBuiltInBlenderPortsReferenceToolCode(t *testing.T) {
	t.Parallel()

	// Spot-check that the embedded programs are the reference tool-code rather than stubs: each
	// declares the Result NamedTuple the calling convention converts with `._asdict()`.
	cases := map[string]string{
		"get_objects_summary":                       blenderToolCodeGetObjectsSummary,
		"get_object_detail_summary":                 blenderToolCodeGetObjectDetailSummary,
		"get_blendfile_summary_datablocks":          blenderToolCodeGetBlendfileSummaryDatablocks,
		"get_screenshot_of_area_as_image":           blenderToolCodeGetScreenshotOfAreaAsImage,
		"render_thumbnail_to_path":                  blenderToolCodeRenderThumbnailToPath,
		"jump_to_view3d_object_data_by_name":        blenderToolCodeJumpToView3dObjectDataByName,
		"get_blendfile_summary_missing_files":       blenderToolCodeGetBlendfileSummaryMissingFiles,
		"get_blendfile_summary_path_info":           blenderToolCodeGetBlendfileSummaryPathInfo,
		"get_screenshot_of_window_as_json":          blenderToolCodeGetScreenshotOfWindowAsJson,
		"get_screenshot_of_window_as_image":         blenderToolCodeGetScreenshotOfWindowAsImage,
		"get_blendfile_summary_of_linked_libraries": blenderToolCodeGetBlendfileSummaryOfLinkedLibraries,
		"jump_to_tab_by_name":                       blenderToolCodeJumpToTabByName,
		"jump_to_tab_by_space_type":                 blenderToolCodeJumpToTabBySpaceType,
		"jump_to_view3d_object_by_name":             blenderToolCodeJumpToView3dObjectByName,
		"render_viewport_to_path":                   blenderToolCodeRenderViewportToPath,
	}
	for tool, code := range cases {
		if !strings.Contains(code, "class Result(NamedTuple):") {
			t.Errorf("%s code has no Result named tuple", tool)
		}
		if !strings.Contains(code, "def main(") {
			t.Errorf("%s code has no main()", tool)
		}
		// Tool-code is loaded in text mode upstream, so the wire payload is LF-only.
		if strings.Contains(code, "\r") {
			t.Errorf("%s code contains carriage returns", tool)
		}
	}
}

func TestBuiltInBlenderSurfacesBlenderErrorsAsToolErrors(t *testing.T) {
	addon := startFakeBlenderAddon(t)
	addon.respond = func(blenderExecuteRequest) map[string]any {
		return map[string]any{
			"status":  "error",
			"message": "Traceback (most recent call last):\nRuntimeError: boom",
			"stderr":  "extra detail",
		}
	}
	pointBlenderAt(t, addon)

	result := callBuiltInTool(t, "get_objects_summary", nil)
	if !result.IsError {
		t.Fatal("a Blender exception must surface as a tool error")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "RuntimeError: boom") {
		t.Errorf("tool error lost the traceback: %s", text)
	}
	if !strings.Contains(text, "extra detail") {
		t.Errorf("tool error lost captured stderr: %s", text)
	}
}

func TestBuiltInBlenderReportsUnreachableInstanceAsError(t *testing.T) {
	// Reserve a port and close it so nothing is listening there.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	endpoint := blenderEndpoint{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	blenderEndpointOverride = &endpoint
	t.Cleanup(func() { blenderEndpointOverride = nil })

	ctx := t.Context()
	session, err := newBlenderToolSession(ctx)
	if err != nil {
		t.Fatalf("open built-in Blender session: %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.client.CallTool(ctx, &mcp.CallToolParams{Name: "get_objects_summary"})
	if err == nil {
		t.Fatal("an unreachable Blender instance must fail the call")
	}
	if !strings.Contains(err.Error(), "cannot connect to Blender") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

func TestBuiltInBlenderReadsImageResponses(t *testing.T) {
	// 1x1 transparent PNG header bytes; the transport forwards whatever Blender produced.
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	encoded := base64.StdEncoding.EncodeToString(png)

	addon := startFakeBlenderAddon(t)
	addon.respond = func(blenderExecuteRequest) map[string]any {
		return map[string]any{
			"status": "ok",
			"result": map[string]any{"status": "ok", "image_base64": encoded, "message": nil},
		}
	}
	pointBlenderAt(t, addon)

	result := callBuiltInTool(t, "get_screenshot_of_window_as_image", nil)
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, result))
	}
	if len(result.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1", len(result.Content))
	}
	image, ok := result.Content[0].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("content = %T, want *mcp.ImageContent", result.Content[0])
	}
	if image.MIMEType != "image/png" {
		t.Errorf("MIME type = %q, want image/png", image.MIMEType)
	}
	if string(image.Data) != encoded {
		t.Errorf("image data = %q, want base64 of the PNG bytes %q", image.Data, encoded)
	}
}

// TestBuiltInBlenderImageToolRejectsInnerError mirrors the reference bridge, which inspects the
// tool-code's own status and raises instead of returning image bytes.
func TestBuiltInBlenderImageToolRejectsInnerError(t *testing.T) {
	addon := startFakeBlenderAddon(t)
	addon.respond = func(blenderExecuteRequest) map[string]any {
		return map[string]any{
			"status": "ok",
			"result": map[string]any{
				"status":       "error",
				"image_base64": nil,
				"message":      "Screenshots are not available in background mode",
			},
		}
	}
	pointBlenderAt(t, addon)

	result := callBuiltInTool(t, "get_screenshot_of_window_as_image", nil)
	if !result.IsError {
		t.Fatal("an inner status error must become a tool error for image tools")
	}
	if text := resultText(t, result); !strings.Contains(text, "background mode") {
		t.Errorf("tool error lost the Blender message: %s", text)
	}
}

func TestBuiltInBlenderRejectsUnknownAreaType(t *testing.T) {
	addon := startFakeBlenderAddon(t)
	pointBlenderAt(t, addon)

	result := callBuiltInTool(t, "get_screenshot_of_area_as_image", map[string]any{"area_ui_type": "NOPE"})
	if !result.IsError {
		t.Fatal("an unsupported area type must be rejected before reaching Blender")
	}
	if len(addon.recorded()) != 0 {
		t.Error("an unsupported area type must not be sent to Blender")
	}
}

func TestBuiltInBlenderKeepsTimeoutAboveTypicalRender(t *testing.T) {
	t.Parallel()

	// The reference bridge waits 300s for a reply; renders are the only long-running tools.
	if blenderRequestTimeout != 300*time.Second {
		t.Fatalf("request timeout = %s, want the reference value of 300s", blenderRequestTimeout)
	}
}

func TestBlenderEndpointFromEnvFallsBackToAddonDefaults(t *testing.T) {
	t.Setenv("BLENDER_MCP_HOST", "")
	t.Setenv("BLENDER_MCP_PORT", "not-a-port")

	endpoint := blenderEndpointFromEnv()
	if endpoint.Host != blenderDefaultHost || endpoint.Port != blenderDefaultPort {
		t.Fatalf("endpoint = %+v, want %s:%d", endpoint, blenderDefaultHost, blenderDefaultPort)
	}
}

func TestQuotePythonStringMatchesReprForCommonValues(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Cube":       "'Cube'",
		`C:\tmp\a.b`: "'C:\\\\tmp\\\\a.b'",
		"it's":       "'it\\'s'",
	}
	for value, want := range cases {
		if got := quotePythonString(value); got != want {
			t.Errorf("quotePythonString(%q) = %s, want %s", value, got, want)
		}
	}
}

func TestBlenderErrorTextIncludesStderrOnlyOnce(t *testing.T) {
	t.Parallel()

	response := &blenderResponse{Status: "error", Message: "boom", Stderr: "boom"}
	if got := blenderErrorText(response); got != "boom" {
		t.Fatalf("blenderErrorText = %q, want the message without duplicated stderr", got)
	}

	response = &blenderResponse{Status: "error", Stderr: "detail"}
	if got := blenderErrorText(response); !strings.Contains(got, "detail") {
		t.Fatalf("blenderErrorText = %q, want stderr appended", got)
	}
}
