package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestTestMCPServerReportsConnectFailureInView(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	server, err := service.UpsertMCPServer(ctx, MCPServerInput{
		Name: "missing", Transport: "stdio", Executable: "nahida-missing-mcp-executable", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := service.TestMCPServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("connect failures must reach the renderer through the view: %v", err)
	}
	if view.Status != "error" || !strings.Contains(view.Error, "nahida-missing-mcp-executable") {
		t.Fatalf("view = %#v", view)
	}
	if view.ToolCount != 0 {
		t.Fatalf("tool count = %d, want 0", view.ToolCount)
	}
}

// Providers answer a function name over 64 characters with a 400 for the whole turn, so an
// over-long public name is shortened rather than sent as-is.
func TestPublicMCPToolNameShortensOverlongNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		server    string
		tool      string
		unchanged bool
	}{
		{name: "readable names are kept", server: "test", tool: "screenshot", unchanged: true},
		{
			name:      "a name at the limit is kept",
			server:    "srv",
			tool:      strings.Repeat("a", mcpToolNameLimit-len("mcp__srv__")),
			unchanged: true,
		},
		{name: "an over-long tool name is shortened", server: "srv", tool: strings.Repeat("b", 90)},
		{name: "an over-long server name is shortened", server: strings.Repeat("s", 70), tool: "screenshot"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			original := "mcp__" + mcpName(testCase.server) + "__" + mcpName(testCase.tool)
			publicName := publicMCPToolName(testCase.server, testCase.tool)
			if len(publicName) > mcpToolNameLimit {
				t.Fatalf("public name %q is %d characters", publicName, len(publicName))
			}
			if unchanged := publicName == original; unchanged != testCase.unchanged {
				t.Fatalf("public name = %q, want it unchanged from %q: %v", publicName, original, testCase.unchanged)
			}
			// The model keeps seeing the readable part of the name; only the tail becomes a hash.
			hash := publicName[strings.LastIndex(publicName, "_")+1:]
			if !testCase.unchanged {
				if len(hash) != 8 || !strings.HasPrefix(original, strings.TrimSuffix(publicName, "_"+hash)) {
					t.Fatalf("public name %q does not keep a prefix of %q with a hash suffix", publicName, original)
				}
			}
			if strings.ContainsFunc(publicName, func(character rune) bool {
				return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789_", character)
			}) {
				t.Errorf("public name %q has a character providers reject", publicName)
			}
			if again := publicMCPToolName(testCase.server, testCase.tool); again != publicName {
				t.Errorf("public name changed between calls: %q then %q", publicName, again)
			}
		})
	}

	// Two names that share their readable prefix have to stay distinct after shortening.
	first := publicMCPToolName("srv", strings.Repeat("c", 80))
	second := publicMCPToolName("srv", strings.Repeat("c", 80)+"d")
	if first == second {
		t.Errorf("both over-long names shortened to %q", first)
	}
}

// The built-in Blender server is seeded with a display name that inflates every public tool name,
// and one of its tools only fits the provider limit through the shortening rule. Registration has
// to keep every tool while staying inside the limit.
func TestRegisteredBlenderToolNamesFitProviderLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	rows, err := client.AgentMCPServers.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	for _, row := range rows {
		if row.Transport != db.BlenderMCPTransport {
			continue
		}
		if _, err := service.UpsertMCPServer(ctx, MCPServerInput{
			ID: row.ID, Name: row.Name, Transport: row.Transport,
			Endpoint: "localhost:9876", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		enabled = true
	}
	if !enabled {
		t.Fatal("the built-in Blender MCP server was not seeded")
	}

	runtime, definitions := service.openMCPRuntime(ctx, nil)
	defer func() { _ = runtime.Close() }()

	if want := len(blenderTools()) + len(mcpBuiltInToolDefinitions()); len(definitions) != want {
		t.Fatalf("registered %d definitions, want %d", len(definitions), want)
	}
	seen := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		if len(definition.Name) > mcpToolNameLimit {
			t.Errorf("tool %q is %d characters", definition.Name, len(definition.Name))
		}
		if seen[definition.Name] {
			t.Errorf("tool name %q is registered twice", definition.Name)
		}
		seen[definition.Name] = true
	}

	// The shortened name has to resolve to the tool Blender itself exposes.
	linked := false
	for publicName, tool := range runtime.tools {
		if tool.name != "get_blendfile_summary_of_linked_libraries" {
			continue
		}
		linked = true
		if len(publicName) > mcpToolNameLimit {
			t.Errorf("the linked-libraries tool is exposed as %q, %d characters", publicName, len(publicName))
		}
	}
	if !linked {
		t.Error("the linked-libraries Blender tool was not registered")
	}
}
