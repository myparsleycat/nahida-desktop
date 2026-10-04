package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Models that are not trained on the patch envelope edit through JSON operations, so that schema
// must lead with `update` and offer oldString/newString.
func TestApplyPatchSchemaOffersUpdateOperations(t *testing.T) {
	t.Parallel()
	definitions := builtInToolDefinitions(false)
	index := slices.IndexFunc(definitions, func(definition ToolDefinition) bool {
		return definition.Name == "apply_patch"
	})
	if index < 0 {
		t.Fatal("apply_patch is missing from the built-in tool definitions")
	}

	properties, _ := definitions[index].InputSchema["properties"].(map[string]any)
	operations, _ := properties["operations"].(map[string]any)
	items, _ := operations["items"].(map[string]any)
	operationProperties, _ := items["properties"].(map[string]any)
	operationTypes, _ := operationProperties["type"].(map[string]any)["enum"].([]string)
	if len(operationTypes) == 0 || operationTypes[0] != "update" {
		t.Fatalf("apply_patch operation enum = %v, want update first", operationTypes)
	}
	for _, field := range []string{"oldString", "newString", "replaceAll"} {
		if _, ok := operationProperties[field]; !ok {
			t.Fatalf("apply_patch operation schema is missing %q: %#v", field, operationProperties)
		}
	}
}

func TestApplyPatchSchemaFollowsModel(t *testing.T) {
	t.Parallel()
	for model, wantPatchText := range map[string]bool{
		"gpt-6-luna":        true,
		"openai/gpt-5.5":    true,
		"gpt-4.1":           false,
		"gpt-oss-120b":      false,
		"claude-sonnet-5-5": false,
		"":                  false,
	} {
		if got := patchTextModel(model); got != wantPatchText {
			t.Errorf("patchTextModel(%q) = %v, want %v", model, got, wantPatchText)
		}
	}

	definitions := builtInToolDefinitions(true)
	index := slices.IndexFunc(definitions, func(definition ToolDefinition) bool {
		return definition.Name == "apply_patch"
	})
	if index < 0 {
		t.Fatal("apply_patch is missing from the built-in tool definitions")
	}
	properties, _ := definitions[index].InputSchema["properties"].(map[string]any)
	if _, ok := properties["patchText"]; !ok || len(properties) != 2 {
		t.Fatalf("patch text schema = %#v", properties)
	}
}

// newPatchExecutor returns an executor over one writable root that holds mod.ini.
func newPatchExecutor(t *testing.T, content string) (*toolExecutor, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sandbox.Close() })
	return &toolExecutor{sandbox: sandbox}, path
}

// A patch envelope that only edits and creates files applies every chunk without an approval.
func TestExecuteAppliesPatchTextWithoutApproval(t *testing.T) {
	t.Parallel()
	executor, path := newPatchExecutor(t, "[A]\r\n\tvalue=old\r\n[B]\r\n\tvalue=old\r\n")

	arguments, err := json.Marshal(map[string]string{
		"rootId": "root",
		"patchText": "*** Begin Patch\n*** Update File: mod.ini\n@@ [A]\n-\tvalue=old\n+\tvalue=new\n" +
			"@@ [B]\n-\tvalue=old\n+\tvalue=newer\n*** Add File: notes.txt\n+created\n*** End Patch",
	})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-1", Name: "apply_patch", Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval != nil || len(execution.ChangedFiles) != 2 {
		t.Fatalf("execution = %#v", execution)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\n\tvalue=new\r\n[B]\r\n\tvalue=newer\r\n" {
		t.Fatalf("file after patch = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "notes.txt")); string(got) != "created\n" {
		t.Fatalf("created file = %q", got)
	}
}

// Every way of editing an existing file applies without an approval.
func TestExecuteAppliesUpdatesWithoutApproval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, operations, want string
	}{
		{
			name: "hunks",
			operations: `{"type":"update","path":"mod.ini","hunks":[` +
				`{"context":"[A]","oldLines":["value=old"],"newLines":["value=new"]}]}`,
			want: "[A]\r\nvalue=new\r\n[B]\r\nother=old\r\n",
		},
		{
			name:       "old string",
			operations: `{"type":"update","path":"mod.ini","oldString":"[A]\nvalue=old","newString":"[A]\nvalue=new"}`,
			want:       "[A]\r\nvalue=new\r\n[B]\r\nother=old\r\n",
		},
		{
			name: "repeated path",
			operations: `{"type":"update","path":"mod.ini","oldString":"value=old","newString":"value=new"},` +
				`{"type":"update","path":"mod.ini","oldString":"other=old","newString":"other=new"}`,
			want: "[A]\r\nvalue=new\r\n[B]\r\nother=new\r\n",
		},
		{
			name:       "write",
			operations: `{"type":"write","path":"mod.ini","content":"[C]\nvalue=1\n"}`,
			want:       "[C]\r\nvalue=1\r\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			executor, path := newPatchExecutor(t, "[A]\r\nvalue=old\r\n[B]\r\nother=old\r\n")

			execution, err := executor.Execute(context.Background(), ToolCall{
				ID: "call-1", Name: "apply_patch",
				Arguments: json.RawMessage(`{"rootId":"root","operations":[` + test.operations + `]}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			if execution.Approval != nil || len(execution.ChangedFiles) != 1 || execution.ChangedFiles[0] != "mod.ini" {
				t.Fatalf("execution = %#v", execution)
			}
			if got, _ := os.ReadFile(path); string(got) != test.want {
				t.Fatalf("file after patch = %q", got)
			}
		})
	}
}

// A patch that deletes a file must pause as a whole for one approval that seals the observed
// content, and only the approved arguments may touch the files.
func TestExecuteApprovesPatchDeletion(t *testing.T) {
	t.Parallel()
	executor, path := newPatchExecutor(t, "[A]\r\nvalue=old\r\n")
	obsolete := filepath.Join(filepath.Dir(path), "obsolete.ini")
	if err := os.WriteFile(obsolete, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	arguments, err := json.Marshal(map[string]string{
		"rootId": "root",
		"patchText": "*** Begin Patch\n*** Update File: mod.ini\n@@ [A]\n-value=old\n+value=new\n" +
			"*** Delete File: obsolete.ini\n*** End Patch",
	})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-1", Name: "apply_patch", Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval == nil || execution.Approval.ActionID != "sandbox.apply_patch" ||
		execution.Approval.Target != "root: mod.ini, obsolete.ini" {
		t.Fatalf("execution = %#v", execution)
	}
	if strings.Contains(string(execution.Approval.Arguments), "patchText") ||
		strings.Count(string(execution.Approval.Arguments), `"expectedContent"`) != 2 {
		t.Fatalf("approval arguments = %s", execution.Approval.Arguments)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=old\r\n" {
		t.Fatalf("file changed before approval: %q", got)
	}
	if _, err := os.Stat(obsolete); err != nil {
		t.Fatalf("file deleted before approval: %v", err)
	}

	approved, err := executor.ExecuteApproved(
		context.Background(),
		"sandbox.apply_patch",
		"sandbox",
		execution.Approval.Arguments,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(approved.ChangedFiles) != 2 {
		t.Fatalf("changed files = %#v", approved.ChangedFiles)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=new\r\n" {
		t.Fatalf("file after approval = %q", got)
	}
	if _, err := os.Stat(obsolete); !os.IsNotExist(err) {
		t.Fatalf("deleted file still present: %v", err)
	}
}

// A move inside a writable root runs without an approval and never replaces an existing path.
func TestExecuteMovesPathWithoutApproval(t *testing.T) {
	t.Parallel()
	executor, path := newPatchExecutor(t, "[A]\r\nvalue=old\r\n")
	root := filepath.Dir(path)

	execution, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-1", Name: "move_path",
		Arguments: json.RawMessage(`{"rootId":"root","from":"mod.ini","to":"nested/renamed.ini"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval != nil || len(execution.ChangedFiles) != 2 {
		t.Fatalf("execution = %#v", execution)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "nested", "renamed.ini")); string(got) != "[A]\r\nvalue=old\r\n" {
		t.Fatalf("moved file = %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}

	if err := os.WriteFile(path, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-2", Name: "move_path",
		Arguments: json.RawMessage(`{"rootId":"root","from":"mod.ini","to":"nested/renamed.ini"}`),
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("move onto an existing path err = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "other\n" {
		t.Fatalf("source after a rejected move = %q", got)
	}
}

// A read-only root can be listed, read, and searched, but no tool may change anything inside it,
// not even through a writable root that contains it or after an approval.
func TestExecuteKeepsReadOnlyRootUnchanged(t *testing.T) {
	t.Parallel()
	importer := t.TempDir()
	core := filepath.Join(importer, "Core")
	if err := os.MkdirAll(core, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core, "main.ini"), []byte("[Constants]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{
		{ID: "importer", Name: "Importer", Path: importer},
		{ID: "core", Name: "Core", Path: core, ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	executor := &toolExecutor{sandbox: sandbox}

	read, err := executor.Execute(context.Background(), ToolCall{
		ID: "read", Name: "read_file", Arguments: json.RawMessage(`{"rootId":"core","relativePath":"main.ini"}`),
	})
	if err != nil || read.Output.(ReadFileResult).Text != "[Constants]\n" {
		t.Fatalf("read = %#v, %v", read, err)
	}
	found, err := executor.Execute(context.Background(), ToolCall{
		ID: "search", Name: "search_text", Arguments: json.RawMessage(`{"rootId":"core","pattern":"Constants"}`),
	})
	if err != nil || len(found.Output.([]SearchMatch)) != 1 {
		t.Fatalf("search = %#v, %v", found, err)
	}

	calls := map[string]ToolCall{
		"create": {Name: "apply_patch", Arguments: json.RawMessage(
			`{"rootId":"core","operations":[{"type":"create","path":"new.ini","content":"x"}]}`,
		)},
		"update": {Name: "apply_patch", Arguments: json.RawMessage(
			`{"rootId":"core","operations":[{"type":"update","path":"main.ini","oldString":"Constants","newString":"X"}]}`,
		)},
		"delete": {Name: "apply_patch", Arguments: json.RawMessage(
			`{"rootId":"core","operations":[{"type":"delete","path":"main.ini"}]}`,
		)},
		"update through parent": {Name: "apply_patch", Arguments: json.RawMessage(
			`{"rootId":"importer","operations":[{"type":"write","path":"Core/main.ini","content":"x"}]}`,
		)},
		"move": {Name: "move_path", Arguments: json.RawMessage(
			`{"rootId":"core","from":"main.ini","to":"renamed.ini"}`,
		)},
		"move through parent": {Name: "move_path", Arguments: json.RawMessage(
			`{"rootId":"importer","from":"Core","to":"Moved"}`,
		)},
	}
	for name, call := range calls {
		execution, err := executor.Execute(context.Background(), call)
		if !errors.Is(err, errSandboxReadOnly) || execution.Approval != nil {
			t.Fatalf("%s: execution = %#v, err = %v", name, execution, err)
		}
	}
	if _, err := executor.ExecuteApproved(context.Background(), "sandbox.apply_patch", "sandbox", json.RawMessage(
		`{"RootID":"core","Operations":[{"type":"delete","path":"main.ini"}]}`,
	)); !errors.Is(err, errSandboxReadOnly) {
		t.Fatalf("approved delete err = %v", err)
	}
	if _, err := executor.ExecuteApproved(context.Background(), "sandbox.move_path", "sandbox", json.RawMessage(
		`{"RootID":"importer","From":"Core","To":"Moved"}`,
	)); !errors.Is(err, errSandboxReadOnly) {
		t.Fatalf("approved move err = %v", err)
	}

	if _, err := sandbox.ResolveExisting("core", "main.ini"); err != nil {
		t.Fatalf("read resolver err = %v", err)
	}
	writable := sandbox.Writable()
	if _, err := writable.ResolveExisting("core", "main.ini"); !errors.Is(err, errSandboxReadOnly) {
		t.Fatalf("writable existing err = %v", err)
	}
	if _, err := writable.ResolveExisting("importer", "."); !errors.Is(err, errSandboxReadOnly) {
		t.Fatalf("writable parent err = %v", err)
	}
	if _, err := writable.ResolveTarget("importer", "Core/output.ini"); !errors.Is(err, errSandboxReadOnly) {
		t.Fatalf("writable target err = %v", err)
	}
	if _, err := writable.ResolveTarget("importer", "Mods/output.ini"); err != nil {
		t.Fatalf("writable sibling err = %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(core, "main.ini")); string(got) != "[Constants]\n" {
		t.Fatalf("read-only file changed: %q", got)
	}
}

// A junction inside a writable root that points into a read-only root must not make a file that is
// yet to be created look writable: the target is judged by where it would really be created.
func TestReadOnlyRootHoldsThroughJunction(t *testing.T) {
	t.Parallel()
	importer := t.TempDir()
	core := filepath.Join(importer, "Core")
	mods := filepath.Join(importer, "Mods")
	for _, dir := range []string{core, mods} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mods, "mod.ini"), []byte("[Constants]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(importer, "Alias")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", alias, core).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	sandbox, err := NewSandbox([]SandboxRoot{
		{ID: "importer", Name: "Importer", Path: importer},
		{ID: "core", Name: "Core", Path: core, ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	writable := sandbox.Writable()
	for _, relativePath := range []string{"Alias/new.ini", "Alias/Sub/new.ini", "Alias"} {
		if _, err := writable.ResolveTarget("importer", relativePath); !errors.Is(err, errSandboxReadOnly) {
			t.Fatalf("writable target %s err = %v", relativePath, err)
		}
	}
	guard := writable.(interface{ CheckWritable(string) error })
	if err := guard.CheckWritable(filepath.Join(alias, "Sub")); !errors.Is(err, errSandboxReadOnly) {
		t.Fatalf("stored path through junction err = %v", err)
	}
	if err := guard.CheckWritable(mods); err != nil {
		t.Fatalf("stored writable path err = %v", err)
	}

	executor := &toolExecutor{sandbox: sandbox}
	calls := map[string]ToolCall{
		"create": {Name: "apply_patch", Arguments: json.RawMessage(
			`{"rootId":"importer","operations":[{"type":"create","path":"Alias/new.ini","content":"x"}]}`,
		)},
		"move into": {Name: "move_path", Arguments: json.RawMessage(
			`{"rootId":"importer","from":"Mods/mod.ini","to":"Alias/mod.ini"}`,
		)},
	}
	for name, call := range calls {
		if _, err := executor.Execute(context.Background(), call); !errors.Is(err, errSandboxReadOnly) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if entries, err := os.ReadDir(core); err != nil || len(entries) != 0 {
		t.Fatalf("read-only root changed: %v, %v", entries, err)
	}
}

// Providers reject a function name longer than 64 characters for the whole request, so every
// built-in definition has to fit that limit too.
func TestToolDefinitionsFitProviderNameLimit(t *testing.T) {
	t.Parallel()
	for _, definition := range slices.Concat(
		builtInToolDefinitions(false), builtInToolDefinitions(true), mcpBuiltInToolDefinitions(),
	) {
		if len(definition.Name) > mcpToolNameLimit {
			t.Errorf("tool %q is %d characters", definition.Name, len(definition.Name))
		}
	}
}

// Providers reject a tool schema whose "required" is null, so every definition sent with a model
// request must carry an array.
func TestToolDefinitionsSendArrayRequired(t *testing.T) {
	t.Parallel()
	definitions := slices.Concat(
		builtInToolDefinitions(false), builtInToolDefinitions(true), mcpBuiltInToolDefinitions(),
	)
	for _, definition := range definitions {
		encoded, err := json.Marshal(definition.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", definition.Name, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("decode %s schema: %v", definition.Name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("tool %s schema type = %v, want object", definition.Name, schema["type"])
		}
		names, ok := schema["required"].([]any)
		if !ok {
			t.Errorf("tool %s schema needs an array required: %s", definition.Name, encoded)
			continue
		}
		for _, name := range names {
			if _, ok := name.(string); !ok {
				t.Errorf("tool %s required entry %#v is not a string", definition.Name, name)
			}
		}
	}
}
