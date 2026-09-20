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

// Targeted edits go through the `update` operation, so the schema the model sees must offer it
// together with the hunk fields it needs.
func TestApplyPatchSchemaOffersUpdateHunks(t *testing.T) {
	t.Parallel()
	definitions := builtInToolDefinitions()
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
	if !slices.Contains(operationTypes, "update") {
		t.Fatalf("apply_patch operation enum = %v, want it to contain update", operationTypes)
	}

	hunks, _ := operationProperties["hunks"].(map[string]any)
	hunkItems, _ := hunks["items"].(map[string]any)
	hunkProperties, _ := hunkItems["properties"].(map[string]any)
	for _, field := range []string{"context", "oldLines", "newLines", "eof"} {
		if _, ok := hunkProperties[field]; !ok {
			t.Fatalf("apply_patch hunk schema is missing %q: %#v", field, hunkProperties)
		}
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

// Providers reject a function name longer than 64 characters for the whole request, so every
// built-in definition has to fit that limit too.
func TestToolDefinitionsFitProviderNameLimit(t *testing.T) {
	t.Parallel()
	for _, definition := range slices.Concat(builtInToolDefinitions(), mcpBuiltInToolDefinitions()) {
		if len(definition.Name) > mcpToolNameLimit {
			t.Errorf("tool %q is %d characters", definition.Name, len(definition.Name))
		}
	}
}

// Providers reject a tool schema whose "required" is null, so every definition sent with a model
// request must carry an array.
func TestToolDefinitionsSendArrayRequired(t *testing.T) {
	t.Parallel()
	definitions := slices.Concat(builtInToolDefinitions(), mcpBuiltInToolDefinitions())
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
