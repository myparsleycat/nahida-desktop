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
