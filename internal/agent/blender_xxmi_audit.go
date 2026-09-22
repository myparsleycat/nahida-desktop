package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const xxmiAuditSourcePath = "skills/blender-xxmi-workflows/references/xxmi_audits.py"

func runXXMIAuditTool() blenderTool {
	return newBlenderTool(
		"run_xxmi_audit",
		"Run XXMI Scene Audit",
		"Run one deterministic, read-only XXMI audit inside Blender.\n\n"+
			"The audit does not change selection, mode, objects, data, add-ons, or files. "+
			"Use explicit object_names for mesh-scoped audits when the current selection is not the intended scope.",
		true,
		blenderObjectSchema(map[string]any{
			"audit": map[string]any{
				"type":        "string",
				"description": "Audit to run.",
				"enum": []string{
					"scene_preflight",
					"preservation_signature",
					"pmx_source",
					"weight_integrity",
					"material_uv",
				},
			},
			"object_names": map[string]any{
				"type":        "array",
				"description": "Exact Blender object names for mesh-scoped audits.",
				"items":       map[string]any{"type": "string"},
				"maxItems":    64,
			},
			"max_influences": map[string]any{
				"type":        "integer",
				"description": "Allowed positive-weight influences per vertex for weight_integrity. Default 4.",
				"minimum":     1,
				"maximum":     32,
			},
		}, "audit"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input xxmiAuditInput
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			expression, err := xxmiAuditExpression(input)
			if err != nil {
				return errorResult(err.Error()), nil
			}
			source, err := fs.ReadFile(builtInSkills, xxmiAuditSourcePath)
			if err != nil {
				return nil, fmt.Errorf("read built-in XXMI audit source: %w", err)
			}

			code := string(source) + "\n\nresult = " + expression + "\n"
			return callBlender(ctx, code, true)
		},
	)
}

type xxmiAuditInput struct {
	Audit         string   `json:"audit"`
	ObjectNames   []string `json:"object_names"`
	MaxInfluences *int     `json:"max_influences"`
}

func xxmiAuditExpression(input xxmiAuditInput) (string, error) {
	if len(input.ObjectNames) > 64 {
		return "", errors.New("object_names exceeds the maximum of 64")
	}
	for _, name := range input.ObjectNames {
		if strings.TrimSpace(name) == "" {
			return "", errors.New("object_names must not contain an empty name")
		}
	}
	if input.MaxInfluences != nil && (*input.MaxInfluences < 1 || *input.MaxInfluences > 32) {
		return "", errors.New("max_influences must be between 1 and 32")
	}

	objects := pythonOptionalStringList(input.ObjectNames)
	switch input.Audit {
	case "scene_preflight":
		if len(input.ObjectNames) > 0 || input.MaxInfluences != nil {
			return "", errors.New("scene_preflight does not accept mesh-scoped options")
		}
		return "scene_preflight()", nil
	case "preservation_signature":
		if len(input.ObjectNames) > 1 {
			return "", errors.New("preservation_signature accepts at most one object name")
		}
		if input.MaxInfluences != nil {
			return "", errors.New("max_influences is only valid for weight_integrity")
		}
		object := "None"
		if len(input.ObjectNames) == 1 {
			object = quotePythonString(input.ObjectNames[0])
		}
		return "preservation_signature(" + object + ")", nil
	case "pmx_source":
		if len(input.ObjectNames) > 0 || input.MaxInfluences != nil {
			return "", errors.New("pmx_source does not accept mesh-scoped options")
		}
		return "pmx_source_audit()", nil
	case "weight_integrity":
		maximum := 4
		if input.MaxInfluences != nil {
			maximum = *input.MaxInfluences
		}
		return fmt.Sprintf("weight_integrity_audit(%s, %d)", objects, maximum), nil
	case "material_uv":
		if input.MaxInfluences != nil {
			return "", errors.New("max_influences is only valid for weight_integrity")
		}
		return "material_uv_audit(" + objects + ")", nil
	default:
		return "", fmt.Errorf("unsupported XXMI audit %q", input.Audit)
	}
}

func pythonOptionalStringList(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quotePythonString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
