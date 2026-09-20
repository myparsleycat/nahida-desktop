package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentactions "nahida.live/desktop/internal/agent/actions"
)

type toolExecutor struct {
	sandbox *Sandbox
	skills  *skillCatalog
	desktop *agentactions.Registry
	scope   AgentScope
	mcp     *mcpRuntime
}

type toolExecution struct {
	Output       any
	Images       []pendingImage
	ChangedFiles []string
	Approval     *agentactions.Proposal
}

func builtInToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_files",
			Description: "List files inside one authorized sandbox root.",
			InputSchema: objectSchema(map[string]any{
				"rootId": map[string]any{"type": "string"}, "relativePath": map[string]any{"type": "string"},
				"recursive": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer"},
			}, "rootId", "relativePath"),
		},
		{
			Name:        "read_file",
			Description: "Read a text range or inspect binary metadata inside the sandbox.",
			InputSchema: objectSchema(map[string]any{
				"rootId": map[string]any{"type": "string"}, "relativePath": map[string]any{"type": "string"},
				"startLine": map[string]any{"type": "integer"}, "endLine": map[string]any{"type": "integer"},
			}, "rootId", "relativePath"),
		},
		{
			Name:        "search_files",
			Description: "Search sandbox file names using a glob or name fragment.",
			InputSchema: objectSchema(map[string]any{
				"rootId": map[string]any{"type": "string"}, "pattern": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			}, "rootId", "pattern"),
		},
		{
			Name:        "search_text",
			Description: "Search text files using fixed text or a regular expression.",
			InputSchema: objectSchema(map[string]any{
				"rootId": map[string]any{"type": "string"}, "pattern": map[string]any{"type": "string"},
				"regex": map[string]any{"type": "boolean"},
			}, "rootId", "pattern"),
		},
		{
			Name:        "apply_patch",
			Description: "Atomically create, update, rewrite, or delete text files after preflight validation. For write, content is the complete replacement and expectedContent is the complete decoded text previously read; for update, hunks replace only the lines they name, in file order. A hunk may reuse the immediately preceding hunk's final old line as its context when the replacement keeps that line unchanged, but hunks must not otherwise overlap. Existing encoding, BOM, and newline style are preserved.",
			InputSchema: objectSchema(map[string]any{
				"rootId": map[string]any{
					"type": "string",
				},
				"operations": map[string]any{"type": "array", "items": objectSchema(map[string]any{
					"type": map[string]any{"type": "string", "enum": []string{"create", "write", "update", "delete"}},
					"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"},
					"expectedContent": map[string]any{"type": "string"},
					"hunks": map[string]any{"type": "array", "items": objectSchema(map[string]any{
						"context":  map[string]any{"type": "string"},
						"oldLines": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"newLines": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"eof":      map[string]any{"type": "boolean"},
					})},
				}, "type", "path")},
			}, "rootId", "operations"),
		},
		{
			Name:        "move_path",
			Description: "Move or rename a path within the same sandbox root.",
			InputSchema: objectSchema(map[string]any{
				"rootId": map[string]any{
					"type": "string",
				},
				"from": map[string]any{"type": "string"},
				"to":   map[string]any{"type": "string"},
			}, "rootId", "from", "to"),
		},
		{
			Name:        "load_skill",
			Description: "Load the full instructions or an explicit reference from a catalog skill.",
			InputSchema: objectSchema(map[string]any{
				"name": map[string]any{"type": "string"}, "reference": map[string]any{"type": "string"},
			}, "name"),
		},
		{
			Name:        "list_desktop_actions",
			Description: "Return schemas for explicitly allowed Nahida Desktop actions. Prefer an exact action ID or narrow query from the system prompt's action index; do not enumerate broadly when a candidate is known.",
			InputSchema: objectSchema(map[string]any{
				"domain": map[string]any{"type": "string"}, "query": map[string]any{"type": "string"},
			}),
		},
		{
			Name:        "run_desktop_action",
			Description: "Run one explicitly registered Nahida Desktop local action. Confirm-risk actions pause for user approval.",
			InputSchema: objectSchema(map[string]any{
				"actionId": map[string]any{"type": "string"}, "arguments": map[string]any{"type": "object"},
			}, "actionId", "arguments"),
		},
	}
}

// decodeToolArguments decodes strict tool arguments: unknown fields and trailing values are rejected.
func decodeToolArguments(raw json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode tool arguments: multiple values")
	}
	return nil
}

// objectSchema builds a strict object schema. Required stays an empty array rather than nil, because
// encoding/json renders a nil string slice as "required": null, which providers reject as invalid.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func (e *toolExecutor) Execute(ctx context.Context, call ToolCall) (toolExecution, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if strings.HasPrefix(call.Name, "mcp__") {
		if e.mcp == nil {
			return toolExecution{}, fmt.Errorf("MCP tool %q is unavailable", call.Name)
		}
		result, err := e.mcp.Call(ctx, call)
		output, images := splitImageContent(result)
		return toolExecution{Output: output, Images: images}, err
	}
	switch call.Name {
	case "list_files":
		var input struct {
			RootID, RelativePath string
			Recursive            bool
			Limit                int
		}
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.sandbox.ListFiles(ctx, input.RootID, input.RelativePath, input.Recursive, input.Limit)
		return toolExecution{Output: result}, err
	case "read_file":
		var input struct {
			RootID, RelativePath string
			StartLine, EndLine   int
		}
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.sandbox.ReadFile(input.RootID, input.RelativePath, input.StartLine, input.EndLine)
		return toolExecution{Output: result}, err
	case "search_files":
		var input struct {
			RootID, Pattern string
			Limit           int
		}
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.sandbox.SearchFiles(ctx, input.RootID, input.Pattern, input.Limit)
		return toolExecution{Output: result}, err
	case "search_text":
		var input struct {
			RootID, Pattern string
			Regex           bool
		}
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.sandbox.SearchText(ctx, input.RootID, input.Pattern, input.Regex)
		return toolExecution{Output: result}, err
	case "apply_patch":
		var input struct {
			RootID     string           `json:"rootId"`
			Operations []PatchOperation `json:"operations"`
		}
		if err := decodeToolArguments(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		prepared, targets, err := e.sandbox.PreparePatch(input.RootID, input.Operations)
		if err != nil {
			return toolExecution{}, err
		}
		for _, operation := range input.Operations {
			if patchNeedsApproval(operation.Type) {
				input.Operations = prepared
				canonical, _ := json.Marshal(input)
				return toolExecution{Approval: &agentactions.Proposal{
					ActionID:  "sandbox.apply_patch",
					Arguments: canonical,
					Summary:   "Modify or delete sandbox files.",
					Target:    input.RootID + ": " + strings.Join(targets, ", "),
					Impact:    "This patch rewrites, edits, or deletes existing local files.",
					Kind:      "sandbox",
				}}, nil
			}
		}
		result, err := e.sandbox.ApplyPatch(input.RootID, prepared)
		return toolExecution{Output: result, ChangedFiles: result.ChangedFiles}, err
	case "move_path":
		var input struct {
			RootID string `json:"rootId"`
			From   string `json:"from"`
			To     string `json:"to"`
		}
		if err := decodeToolArguments(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		source, err := e.sandbox.ResolveExisting(input.RootID, input.From)
		if err != nil {
			return toolExecution{}, err
		}
		root := e.sandbox.roots[input.RootID]
		targetRelative, err := validateRelativePath(input.To)
		if err != nil {
			return toolExecution{}, err
		}
		target := filepath.Join(root.view.Path, targetRelative)
		if !pathWithin(root.view.Path, target) {
			return toolExecution{}, errSandboxPath
		}
		if _, statErr := os.Lstat(target); statErr == nil {
			return toolExecution{}, fmt.Errorf("move target already exists: %s", input.To)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return toolExecution{}, statErr
		}
		canonical, _ := json.Marshal(input)
		return toolExecution{Approval: &agentactions.Proposal{
			ActionID: "sandbox.move_path", Arguments: canonical, Summary: "Move or rename a sandbox path.",
			Target: source + " → " + target, Impact: "This changes local file paths and may affect mod loading.",
			Kind: "sandbox",
		}}, nil
	case "load_skill":
		var input struct{ Name, Reference string }
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		content, err := e.skills.Load(input.Name, input.Reference)
		return toolExecution{Output: map[string]any{"content": content}}, err
	case "list_desktop_actions":
		var input struct{ Domain, Query string }
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		if e.desktop == nil {
			return toolExecution{}, errors.New("desktop actions are unavailable")
		}
		return toolExecution{Output: e.desktop.Definitions(e.scope.Type, input.Domain, input.Query)}, nil
	case "run_desktop_action":
		if e.desktop == nil {
			return toolExecution{}, errors.New("desktop actions are unavailable")
		}
		var input agentactions.Call
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		plan, err := e.desktop.Prepare(e.scope.Type, e.sandbox, input)
		if err != nil {
			return toolExecution{}, err
		}
		if plan.Proposal != nil {
			return toolExecution{Approval: plan.Proposal}, nil
		}
		output, err := plan.Run(ctx)
		persisted, images := splitActionImage(output)
		return toolExecution{Output: persisted, Images: images}, err
	case "list_mcp_resources":
		var input struct{ Server string }
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.mcp.ListResources(ctx, input.Server)
		return toolExecution{Output: result}, err
	case "read_mcp_resource":
		var input struct{ Server, URI string }
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.mcp.ReadResource(ctx, input.Server, input.URI)
		return toolExecution{Output: result}, err
	case "list_mcp_prompts":
		var input struct{ Server string }
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.mcp.ListPrompts(ctx, input.Server)
		return toolExecution{Output: result}, err
	case "get_mcp_prompt":
		var input struct {
			Server, Name string
			Arguments    map[string]string
		}
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return toolExecution{}, err
		}
		result, err := e.mcp.GetPrompt(ctx, input.Server, input.Name, input.Arguments)
		return toolExecution{Output: result}, err
	case "list_mod_actions":
		return toolExecution{Output: e.modActions()}, nil
	case "run_mod_action":
		return toolExecution{}, errors.New("run_mod_action is deprecated; use run_desktop_action")
	default:
		return toolExecution{}, fmt.Errorf("unregistered tool %q", call.Name)
	}
}

func (e *toolExecutor) ExecuteApproved(
	ctx context.Context,
	actionID, kind string,
	arguments json.RawMessage,
) (toolExecution, error) {
	switch kind {
	case "desktop":
		if e.desktop == nil {
			return toolExecution{}, errors.New("desktop actions are unavailable")
		}
		output, err := e.desktop.Execute(ctx, e.scope.Type, e.sandbox, actionID, arguments)
		persisted, images := splitActionImage(output)
		return toolExecution{Output: persisted, Images: images}, err
	case "sandbox":
		switch actionID {
		case "sandbox.apply_patch":
			var input struct {
				RootID     string
				Operations []PatchOperation
			}
			if err := json.Unmarshal(arguments, &input); err != nil {
				return toolExecution{}, err
			}
			result, err := e.sandbox.ApplyPatch(input.RootID, input.Operations)
			return toolExecution{Output: result, ChangedFiles: result.ChangedFiles}, err
		case "sandbox.move_path":
			var input struct{ RootID, From, To string }
			if err := json.Unmarshal(arguments, &input); err != nil {
				return toolExecution{}, err
			}
			err := e.sandbox.MovePath(input.RootID, input.From, input.To)
			return toolExecution{
				Output:       map[string]any{"moved": err == nil},
				ChangedFiles: []string{input.From, input.To},
			}, err
		}
	}
	return toolExecution{}, fmt.Errorf("unsupported approved action %q", actionID)
}

func (e *toolExecutor) modActions() []map[string]any {
	return []map[string]any{
		{"id": "inspect-fixes", "description": "Inspect the mod for applicable fixes"},
		{"id": "run-fix-script", "description": "Run an already-installed fix script; requires scriptId"},
		{"id": "run-fix-preset", "description": "Run an already-installed fix preset; requires presetId"},
		{"id": "resize-textures", "description": "Resize mod textures with existing Nahida settings"},
		{"id": "wuwa-fixer", "description": "Run an already-prepared Wuwa fixer"},
		{"id": "zzmi-fixer", "description": "Run an already-prepared ZZMI fixer; requires tool"},
	}
}
