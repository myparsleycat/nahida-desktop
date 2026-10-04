package agent

import (
	"context"
	"encoding/json"
	"os"
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

// A patch envelope must pause for one approval that seals the observed content, and the approved
// operations must apply every chunk.
func TestExecuteApprovesPatchText(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	original := "[A]\r\n\tvalue=old\r\n[B]\r\n\tvalue=old\r\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	executor := &toolExecutor{sandbox: sandbox}

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
	if execution.Approval == nil || execution.Approval.Target != "root: mod.ini, notes.txt" {
		t.Fatalf("execution = %#v", execution)
	}
	if strings.Contains(string(execution.Approval.Arguments), "patchText") ||
		strings.Count(string(execution.Approval.Arguments), `"expectedContent"`) != 1 {
		t.Fatalf("approval arguments = %s", execution.Approval.Arguments)
	}
	if got, _ := os.ReadFile(path); string(got) != original {
		t.Fatalf("file changed before approval: %q", got)
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
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\n\tvalue=new\r\n[B]\r\n\tvalue=newer\r\n" {
		t.Fatalf("file after approval = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "notes.txt")); string(got) != "created\n" {
		t.Fatalf("created file = %q", got)
	}
}

// An `update` call must pause for approval with the observed file content sealed into the review
// payload, and only the approved arguments may touch the file.
func TestExecuteApprovesUpdateHunks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("[A]\r\nvalue=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	executor := &toolExecutor{sandbox: sandbox}

	execution, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-1", Name: "apply_patch", Arguments: json.RawMessage(`{"rootId":"root","operations":[
{"type":"update","path":"mod.ini","hunks":[{"context":"[A]","oldLines":["value=old"],"newLines":["value=new"]}]}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval == nil || execution.Approval.ActionID != "sandbox.apply_patch" {
		t.Fatalf("execution = %#v", execution)
	}
	if !strings.Contains(string(execution.Approval.Arguments), `"expectedContent":"[A]\r\nvalue=old\r\n"`) {
		t.Fatalf("approval arguments are not sealed: %s", execution.Approval.Arguments)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=old\r\n" {
		t.Fatalf("file changed before approval: %q", got)
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
	if len(approved.ChangedFiles) != 1 || approved.ChangedFiles[0] != "mod.ini" {
		t.Fatalf("changed files = %#v", approved.ChangedFiles)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=new\r\n" {
		t.Fatalf("file after approval = %q", got)
	}
}

// Several updates to one file must reach the user as a single approval and apply together.
func TestExecuteApprovesRepeatedUpdatesToOnePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("[A]\r\nvalue=old\r\n[B]\r\nother=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	executor := &toolExecutor{sandbox: sandbox}

	execution, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-1", Name: "apply_patch", Arguments: json.RawMessage(`{"rootId":"root","operations":[
{"type":"update","path":"mod.ini","oldString":"value=old","newString":"value=new"},
{"type":"update","path":"mod.ini","oldString":"other=old","newString":"other=new"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval == nil || execution.Approval.Target != "root: mod.ini" {
		t.Fatalf("execution = %#v", execution)
	}
	if strings.Count(string(execution.Approval.Arguments), `"expectedContent"`) != 1 {
		t.Fatalf("approval arguments seal the file more than once: %s", execution.Approval.Arguments)
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
	if len(approved.ChangedFiles) != 1 || approved.ChangedFiles[0] != "mod.ini" {
		t.Fatalf("changed files = %#v", approved.ChangedFiles)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=new\r\n[B]\r\nother=new\r\n" {
		t.Fatalf("file after approval = %q", got)
	}
}

func TestExecuteApprovesUpdateOldString(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("[A]\r\nvalue=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	executor := &toolExecutor{sandbox: sandbox}

	execution, err := executor.Execute(context.Background(), ToolCall{
		ID: "call-1", Name: "apply_patch", Arguments: json.RawMessage(
			`{"rootId":"root","operations":[{"type":"update","path":"mod.ini","oldString":"[A]\nvalue=old","newString":"[A]\nvalue=new"}]}`,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval == nil || execution.Approval.ActionID != "sandbox.apply_patch" {
		t.Fatalf("execution = %#v", execution)
	}
	if !strings.Contains(string(execution.Approval.Arguments), `"oldString":"[A]\nvalue=old"`) {
		t.Fatalf("approval arguments dropped oldString: %s", execution.Approval.Arguments)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=old\r\n" {
		t.Fatalf("file changed before approval: %q", got)
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
	if len(approved.ChangedFiles) != 1 || approved.ChangedFiles[0] != "mod.ini" {
		t.Fatalf("changed files = %#v", approved.ChangedFiles)
	}
	if got, _ := os.ReadFile(path); string(got) != "[A]\r\nvalue=new\r\n" {
		t.Fatalf("file after approval = %q", got)
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
