package actions_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/agent/actions"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/tools/texture"
)

func TestDDSActionsScopePreviewApprovalAndStaleApply(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	raw := make([]byte, 152)
	copy(raw, "DDS ")
	copy(raw[84:], "DX10")
	copy(raw[148:], []byte{6, 128, 64, 200})
	for offset, value := range map[int]uint32{4: 124, 8: 0x100f, 12: 1, 16: 1, 20: 4, 28: 1, 76: 32, 80: 4, 108: 0x1000, 128: 29, 132: 3, 140: 1} {
		binary.LittleEndian.PutUint32(raw[offset:offset+4], value)
	}
	path := filepath.Join(root, "mask.dds")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := agent.NewSandbox([]agent.SandboxRoot{{ID: "mod", Name: "Mod", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	registry := actions.NewRegistry(actions.Dependencies{Tools: tools.New()})
	prepare := func(id string, args map[string]any) (actions.Plan, error) {
		encoded, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		return registry.Prepare("mod", sandbox, actions.Call{ActionID: id, Arguments: encoded})
	}
	plan, err := prepare("tools.inspect_dds", map[string]any{"rootId": "mod", "relativePath": "mask.dds"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := plan.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inspection := value.(texture.DDSInspection)
	args := map[string]any{
		"rootId":         "mod",
		"relativePath":   "mask.dds",
		"expectedSHA256": inspection.SHA256,
		"operation":      "reinterpret_linear",
		"apply":          false,
	}
	plan, err = prepare("tools.repair_dds", args)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Risk != actions.RiskRead || plan.Proposal != nil {
		t.Fatal("dry run needs approval")
	}
	if _, err := plan.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("dry run wrote files")
	}
	args["apply"] = true
	plan, err = prepare("tools.repair_dds", args)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Proposal == nil || plan.Risk != actions.RiskConfirm {
		t.Fatal("mutation bypassed approval")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("prepare mutated texture")
	}
	// Simulate an external edit while the exact proposal awaits approval.
	raw[148] = 8
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Run(context.Background()); err == nil {
		t.Fatal("approved stale hash overwrote external edit")
	}
	plan, err = prepare(
		"tools.preview_dds",
		map[string]any{"rootId": "mod", "relativePath": "mask.dds", "channel": "r"},
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err = plan.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if image, ok := value.(actions.CapturedImage); !ok || len(image.PNG) == 0 {
		t.Fatalf("preview is not model-visible: %T", value)
	}
	for _, id := range []string{"tools.inspect_dds", "tools.preview_dds"} {
		x := map[string]any{"rootId": "mod", "relativePath": "../outside.dds"}
		if id == "tools.preview_dds" {
			x["channel"] = "r"
		}
		plan, err := prepare(id, x)
		if err == nil {
			_, err = plan.Run(context.Background())
		}
		if err == nil {
			t.Fatalf("%s escaped sandbox", id)
		}
	}
	args["apply"] = false
	args["operation"] = "minimum_red"
	args["minimumRed"] = 1
	args["selection"] = map[string]any{"rootId": "mod", "relativePath": "../outside.png"}
	plan, err = prepare("tools.repair_dds", args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Run(context.Background()); err == nil {
		t.Fatal("selection escaped sandbox")
	}
}
