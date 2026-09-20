package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	modservice "nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/tools"
)

// pathReference is one sandbox-relative path argument in an action's input schema.
type pathReference struct {
	RootID       string `json:"rootId"`
	RelativePath string `json:"relativePath"`
}

// mergePlanNode mirrors the merge plan the mod service builds, with sandbox-relative leaf paths.
type mergePlanNode struct {
	Kind           string          `json:"kind"`
	RootID         string          `json:"rootId,omitempty"`
	RelativePath   string          `json:"relativePath,omitempty"`
	ID             string          `json:"id,omitempty"`
	Engine         string          `json:"engine,omitempty"`
	Name           string          `json:"name,omitempty"`
	ForwardKey     string          `json:"forwardKey,omitempty"`
	BackKey        string          `json:"backKey,omitempty"`
	IncludeVanilla bool            `json:"includeVanilla,omitempty"`
	Children       []mergePlanNode `json:"children,omitempty"`
}

// mergeRequest is the merge mod packs argument.
type mergeRequest struct {
	Group     pathReference `json:"group"`
	Placement string        `json:"placement"`
	PackName  string        `json:"packName"`
	Root      mergePlanNode `json:"root"`
}

func simpleAction(
	id, description, domain string,
	risk Risk,
	schema map[string]any,
	execute func(context.Context, actionContext, json.RawMessage) (any, error),
) action {
	return action{
		definition: Definition{ID: id, Description: description, Domain: domain, Risk: risk,
			Scopes: []string{"global", "mod"}, InputSchema: schema},
		execute: execute,
	}
}

func pathAction(
	id, description string,
	risk Risk,
	execute func(context.Context, string, json.RawMessage) (any, error),
) action {
	return action{
		definition: Definition{ID: id, Description: description, Domain: strings.SplitN(id, ".", 2)[0],
			Risk: risk, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"rootId": stringSchema(), "relativePath": stringSchema(), "importer": stringSchema(),
			}, "rootId", "relativePath")},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			return execute(ctx, path, raw)
		},
		describe: pathDescription(description),
	}
}

func pathIDAction(
	id, description, idField string,
	risk Risk,
	execute func(context.Context, string, string) (any, error),
) action {
	action := pathAction(
		id,
		description,
		risk,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			var value string
			if err := json.Unmarshal(input[idField], &value); err != nil || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%s is required", idField)
			}
			return execute(ctx, path, value)
		},
	)
	action.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), idField: stringSchema(),
	}, "rootId", "relativePath", idField)
	return action
}

func twoPathAction(
	id, description string,
	risk Risk,
	execute func(context.Context, string, string, json.RawMessage) (any, error),
) action {
	action := action{
		definition: Definition{ID: id, Description: description, Domain: strings.SplitN(id, ".", 2)[0],
			Risk: risk, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
				"targetRootId": stringSchema(), "targetRelativePath": stringSchema(),
			}, "sourceRootId", "sourceRelativePath", "targetRootId", "targetRelativePath")},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				SourceRootID       string `json:"sourceRootId"`
				SourceRelativePath string `json:"sourceRelativePath"`
				TargetRootID       string `json:"targetRootId"`
				TargetRelativePath string `json:"targetRelativePath"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			source, err := actionCtx.resolve(input.SourceRootID, input.SourceRelativePath)
			if err != nil {
				return nil, err
			}
			target, err := actionCtx.resolve(input.TargetRootID, input.TargetRelativePath)
			if err != nil {
				return nil, err
			}
			return execute(ctx, source, target, raw)
		},
		describe: func(actionCtx actionContext, raw json.RawMessage) (string, string, error) {
			var input struct {
				SourceRootID       string `json:"sourceRootId"`
				SourceRelativePath string `json:"sourceRelativePath"`
				TargetRootID       string `json:"targetRootId"`
				TargetRelativePath string `json:"targetRelativePath"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return "", "", err
			}
			source, err := actionCtx.resolve(input.SourceRootID, input.SourceRelativePath)
			if err != nil {
				return "", "", err
			}
			target, err := actionCtx.resolve(input.TargetRootID, input.TargetRelativePath)
			return description, source + " ??" + target, err
		},
	}
	return action
}

func idAction(
	id, description, idField string,
	risk Risk,
	execute func(context.Context, string) (any, error),
) action {
	return simpleAction(id, description, strings.SplitN(id, ".", 2)[0], risk,
		objectSchema(map[string]any{idField: stringSchema()}, idField),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			var value string
			if err := json.Unmarshal(input[idField], &value); err != nil || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%s is required", idField)
			}
			return execute(ctx, value)
		})
}

func sessionApplyAction(
	id, description string,
	execute func(context.Context, tools.TouchProfileApplyInput) (tools.TouchApplyResult, error),
) action {
	return simpleAction(id, description, "tools", RiskConfirm,
		objectSchema(map[string]any{"sessionId": stringSchema(), "force": booleanSchema()}, "sessionId"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.TouchProfileApplyInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return execute(ctx, input)
		})
}

func menuMakerSaveAction(
	id, description string,
	execute func(context.Context, string, string, json.RawMessage) (any, error),
) action {
	properties := map[string]any{
		"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
		"destinationRootId": stringSchema(), "destinationRelativePath": stringSchema(),
		"sourceText": stringSchema(), "slots": map[string]any{"type": "array"},
		"settings": map[string]any{"type": "object"}, "encoding": stringSchema(),
		"hasBOM": booleanSchema(), "newline": stringSchema(),
	}
	required := []string{
		"sourceRootId", "sourceRelativePath", "destinationRootId", "destinationRelativePath",
		"sourceText", "slots", "settings", "encoding", "hasBOM", "newline",
	}
	if id == "menumaker.save_zip" {
		properties["assets"] = map[string]any{"type": "array"}
		required = append(required, "assets")
	}
	return action{
		definition: Definition{ID: id, Description: description, Domain: "menumaker",
			Risk: RiskConfirm, Scopes: []string{"global", "mod"},
			InputSchema: objectSchema(properties, required...)},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var paths struct {
				SourceRootID            string `json:"sourceRootId"`
				SourceRelativePath      string `json:"sourceRelativePath"`
				DestinationRootID       string `json:"destinationRootId"`
				DestinationRelativePath string `json:"destinationRelativePath"`
			}
			if err := json.Unmarshal(raw, &paths); err != nil {
				return nil, err
			}
			source, err := actionCtx.resolve(paths.SourceRootID, paths.SourceRelativePath)
			if err != nil {
				return nil, err
			}
			destination, err := actionCtx.resolveTarget(paths.DestinationRootID,
				paths.DestinationRelativePath,
			)
			if err != nil {
				return nil, err
			}
			return execute(ctx, source, destination, raw)
		},
		describe: func(actionCtx actionContext, raw json.RawMessage) (string, string, error) {
			var paths struct {
				DestinationRootID       string `json:"destinationRootId"`
				DestinationRelativePath string `json:"destinationRelativePath"`
			}
			if err := json.Unmarshal(raw, &paths); err != nil {
				return "", "", err
			}
			destination, err := actionCtx.resolveTarget(paths.DestinationRootID,
				paths.DestinationRelativePath,
			)
			return description, destination, err
		},
	}
}

func resolveActionPaths(actionCtx actionContext, references []pathReference) ([]string, error) {
	paths := make([]string, len(references))
	for index, reference := range references {
		path, err := actionCtx.resolve(reference.RootID, reference.RelativePath)
		if err != nil {
			return nil, fmt.Errorf("resolve path %d: %w", index, err)
		}
		paths[index] = path
	}
	return paths, nil
}

func decodeAgentMergeRequest(actionCtx actionContext, raw json.RawMessage) (modservice.MergeModsRequest, error) {
	var input mergeRequest
	if err := decodeActionArguments(raw, &input); err != nil {
		return modservice.MergeModsRequest{}, err
	}
	groupPath, err := actionCtx.resolve(input.Group.RootID, input.Group.RelativePath)
	if err != nil {
		return modservice.MergeModsRequest{}, err
	}
	root, err := resolveAgentMergeNode(actionCtx, input.Root)
	if err != nil {
		return modservice.MergeModsRequest{}, err
	}
	return modservice.MergeModsRequest{
		GroupPath: groupPath,
		Placement: input.Placement,
		PackName:  input.PackName,
		Root:      root,
	}, nil
}

func resolveAgentMergeNode(actionCtx actionContext, input mergePlanNode) (modservice.MergePlanNode, error) {
	node := modservice.MergePlanNode{
		Kind: input.Kind, ID: input.ID, Engine: input.Engine, Name: input.Name,
		ForwardKey: input.ForwardKey, BackKey: input.BackKey, IncludeVanilla: input.IncludeVanilla,
		Children: make([]modservice.MergePlanNode, len(input.Children)),
	}
	if input.Kind == "leaf" {
		path, err := actionCtx.resolve(input.RootID, input.RelativePath)
		if err != nil {
			return modservice.MergePlanNode{}, err
		}
		node.Path = path
	}
	for index, child := range input.Children {
		resolved, err := resolveAgentMergeNode(actionCtx, child)
		if err != nil {
			return modservice.MergePlanNode{}, fmt.Errorf("resolve merge child %d: %w", index, err)
		}
		node.Children[index] = resolved
	}
	return node, nil
}

// windowTargetLabel describes a window selector for the approval prompt.
func windowTargetLabel(title, process string, pid uint32) string {
	parts := make([]string, 0, 3)
	if title = strings.TrimSpace(title); title != "" {
		parts = append(parts, fmt.Sprintf("title %q", title))
	}
	if process = strings.TrimSpace(process); process != "" {
		parts = append(parts, fmt.Sprintf("process %q", process))
	}
	if pid != 0 {
		parts = append(parts, fmt.Sprintf("pid %d", pid))
	}
	if len(parts) == 0 {
		return "no window selector"
	}
	return strings.Join(parts, ", ")
}

func pathDescription(summary string) func(actionContext, json.RawMessage) (string, string, error) {
	return func(actionCtx actionContext, raw json.RawMessage) (string, string, error) {
		var input struct {
			RootID       string `json:"rootId"`
			RelativePath string `json:"relativePath"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", "", err
		}
		path, err := actionCtx.resolve(input.RootID, input.RelativePath)
		return summary, path, err
	}
}

func decodeActionArguments(raw json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode action arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode action arguments: multiple values")
	}
	return nil
}

func actionOK(err error) (any, error) { return map[string]any{"completed": err == nil}, err }
