package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handlers for the built-in Blender MCP tools.
//
// Each handler sends the mirrored Blender-side program to the add-on and converts the reply into
// MCP content. The programs live in blender_toolcode.go (generated from the reference tool-code)
// and are completed here with the add-on calling convention. Parameter values are rendered as a
// Python constructor call, exactly like the reference bridge does with `repr(Params(...))`.

// blenderProgram appends the add-on calling convention to a tool-code body. The parameters are
// already rendered as Python source by the caller.
func blenderProgram(body, params string) string {
	return body + fmt.Sprintf(`
_rv = main(%s)
if callable(_rv):
    check_is_finished = _rv
    result = {}
else:
    result = _rv._asdict()
`, params)
}

// quotePythonString renders a Go string as a single-quoted Python string literal, matching the
// reference bridge's `repr()` output for the parameter values these tools accept.
func quotePythonString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "''"
	}
	// json.Marshal always produces a double-quoted literal; convert to the single-quoted form
	// Python's repr() uses, unescaping the double quotes it no longer needs.
	inner := string(encoded[1 : len(encoded)-1])
	inner = replaceAllLiteral(inner, `\"`, `"`)
	inner = replaceAllLiteral(inner, `'`, `\'`)
	return "'" + inner + "'"
}

func replaceAllLiteral(value, old, replacement string) string {
	if old == "" {
		return value
	}
	out := make([]byte, 0, len(value))
	for index := 0; index < len(value); {
		if index+len(old) <= len(value) && value[index:index+len(old)] == old {
			out = append(out, replacement...)
			index += len(old)
			continue
		}
		out = append(out, value[index])
		index++
	}
	return string(out)
}

func boolPythonLiteral(value bool) string {
	if value {
		return "True"
	}
	return "False"
}

func executeBlenderCodeTool() blenderTool {
	return newBlenderTool(
		"execute_blender_code",
		"Execute Python Code",
		"Execute Python code in the connected Blender instance.\n\n"+
			"The code runs in Blender's Python environment with full access to `bpy`.\n"+
			"To return data, assign a JSON-serialisable dict to a variable named `result`.\n"+
			"Deferred completion via `check_is_finished` is only supported by the\n"+
			"interactive addon server, and is rejected in background mode.\n\n"+
			"Import the modules you use (`import bpy`); nothing is pre-imported.",
		false,
		blenderObjectSchema(map[string]any{
			"code": blenderStringProperty("Python code to execute inside Blender. " +
				"Assign a JSON-serialisable dict to a variable named `result` to return data."),
		}, "code"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				Code string `json:"code"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			// Non-strict: model-generated code may return Blender objects, which the add-on
			// renders with `repr` instead of failing the whole call.
			return callBlender(ctx, input.Code, false)
		},
	)
}

func getObjectsSummaryTool() blenderTool {
	return newBlenderTool(
		"get_objects_summary",
		"Get Objects Summary",
		"Return the scene's collection hierarchy and their objects.\n\n"+
			"Each collection lists its objects (name, type, parent, data name,\n"+
			"selection, visibility) and nested child collections.",
		true,
		blenderObjectSchema(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return callBlender(ctx, blenderProgram(blenderToolCodeGetObjectsSummary, "None"), true)
		},
	)
}

func getObjectDetailSummaryTool() blenderTool {
	return newBlenderTool(
		"get_object_detail_summary",
		"Get Object Detail Summary",
		"Return a structured summary of the object identified by *name*.\n\n"+
			"Includes type, transforms, parent, children, modifiers, constraints,\n"+
			"materials, visibility, data-block name, and collections.",
		true,
		blenderObjectSchema(map[string]any{
			"name": blenderStringProperty("Name of the object to describe."),
		}, "name"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				Name string `json:"name"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(name=" + quotePythonString(input.Name) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeGetObjectDetailSummary, params), true)
		},
	)
}

func getBlendfileSummaryDatablocksTool() blenderTool {
	return newBlenderTool(
		"get_blendfile_summary_datablocks",
		"Get Blend-File Data-blocks Summary",
		"Return a summary of the blend file: data-block counts, active workspace, and render engine.",
		true,
		blenderObjectSchema(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return callBlender(ctx, blenderProgram(blenderToolCodeGetBlendfileSummaryDatablocks, "None"), true)
		},
	)
}

func getBlendfileSummaryPathInfoTool() blenderTool {
	return newBlenderTool(
		"get_blendfile_summary_path_info",
		"Get Blend-File Path Info Summary",
		"Simple/fast access to the blend file's path, save status, age, and backups.",
		true,
		blenderObjectSchema(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return callBlender(ctx, blenderProgram(blenderToolCodeGetBlendfileSummaryPathInfo, "None"), true)
		},
	)
}

func getBlendfileSummaryMissingFilesTool() blenderTool {
	return newBlenderTool(
		"get_blendfile_summary_missing_files",
		"Get Blend-File Missing Files Summary",
		"Report external file references that are missing from disk\n"+
			"(images, libraries, fonts, sounds, movie clips, caches, sequences).",
		true,
		blenderObjectSchema(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return callBlender(ctx, blenderProgram(blenderToolCodeGetBlendfileSummaryMissingFiles, "None"), true)
		},
	)
}

func getBlendfileSummaryOfLinkedLibrariesTool() blenderTool {
	return newBlenderTool(
		"get_blendfile_summary_of_linked_libraries",
		"Get Blend-File Linked Library Summary",
		"Return a tree of directly and indirectly linked library files.",
		true,
		blenderObjectSchema(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return callBlender(ctx, blenderProgram(blenderToolCodeGetBlendfileSummaryOfLinkedLibraries, "None"), true)
		},
	)
}

func getScreenshotOfWindowAsJSONTool() blenderTool {
	return newBlenderTool(
		"get_screenshot_of_window_as_json",
		"Get Window Layout as JSON",
		"Return a JSON description of the Blender window layout, areas, active\n"+
			"object, and selection.",
		true,
		blenderObjectSchema(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return callBlender(ctx, blenderProgram(blenderToolCodeGetScreenshotOfWindowAsJson, "None"), true)
		},
	)
}

func getScreenshotOfWindowAsImageTool() blenderTool {
	return newBlenderTool(
		"get_screenshot_of_window_as_image",
		"Get Window Screenshot",
		"Take a screenshot of the entire Blender window and return it as a PNG image.\n\n"+
			"*size_limit_in_bytes* caps the image size in bytes.\n"+
			"Zero (the default) uses the MCP message size limit.",
		true,
		blenderObjectSchema(map[string]any{
			"size_limit_in_bytes": blenderIntProperty(
				"Maximum encoded image size in bytes. Zero uses the MCP message size limit."),
		}),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				SizeLimitInBytes *int `json:"size_limit_in_bytes"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(size_limit_in_bytes=" + intPythonLiteral(input.SizeLimitInBytes) + ")"
			return callBlenderForImage(ctx, blenderProgram(blenderToolCodeGetScreenshotOfWindowAsImage, params))
		},
	)
}

func getScreenshotOfAreaAsImageTool() blenderTool {
	return newBlenderTool(
		"get_screenshot_of_area_as_image",
		"Get Area Screenshot",
		"Take a screenshot of a single Blender area and return it as a PNG image.\n\n"+
			"*area_ui_type* matches the area's `ui_type`.\n\n"+
			"*size_limit_in_bytes* caps the image size in bytes.\n"+
			"Zero (the default) uses the MCP message size limit.",
		true,
		blenderObjectSchema(map[string]any{
			"area_ui_type": map[string]any{
				"type":        "string",
				"description": "UI type of the area to capture.",
				"enum":        blenderAreaUITypes(),
			},
			"size_limit_in_bytes": blenderIntProperty(
				"Maximum encoded image size in bytes. Zero uses the MCP message size limit."),
		}, "area_ui_type"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				AreaUIType       string `json:"area_ui_type"`
				SizeLimitInBytes *int   `json:"size_limit_in_bytes"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			if !validBlenderAreaUIType(input.AreaUIType) {
				return errorResult(fmt.Sprintf(
					"unsupported area_ui_type %q", input.AreaUIType)), nil
			}
			params := "Params(area_ui_type=" + quotePythonString(input.AreaUIType) +
				", size_limit_in_bytes=" + intPythonLiteral(input.SizeLimitInBytes) + ")"
			return callBlenderForImage(ctx, blenderProgram(blenderToolCodeGetScreenshotOfAreaAsImage, params))
		},
	)
}

// blenderAreaUITypes mirrors the reference tool's Literal list, which excludes the internal
// "EMPTY" area type users can never see. "UV" is an alias of IMAGE_EDITOR and is kept because the
// reference tool accepts it.
func blenderAreaUITypes() []string {
	return []string{
		"VIEW_3D",
		"IMAGE_EDITOR",
		"UV",
		"ShaderNodeTree",
		"CompositorNodeTree",
		"GeometryNodeTree",
		"TextureNodeTree",
		"SEQUENCE_EDITOR",
		"CLIP_EDITOR",
		"DOPESHEET_EDITOR",
		"GRAPH_EDITOR",
		"NLA_EDITOR",
		"TEXT_EDITOR",
		"CONSOLE",
		"INFO",
		"TOPBAR",
		"STATUSBAR",
		"OUTLINER",
		"PROPERTIES",
		"FILE_BROWSER",
		"SPREADSHEET",
		"PREFERENCES",
	}
}

func validBlenderAreaUIType(value string) bool {
	for _, candidate := range blenderAreaUITypes() {
		if candidate == value {
			return true
		}
	}
	return false
}

// intPythonLiteral renders an optional integer. The reference bridge always emits the parameter,
// because the tool-code default of zero means "use the MCP message size limit".
func intPythonLiteral(value *int) string {
	if value == nil {
		return "0"
	}
	return fmt.Sprintf("%d", *value)
}

func jumpToTabByNameTool() blenderTool {
	return newBlenderTool(
		"jump_to_tab_by_name",
		"Switch to Workspace",
		"Switch the active workspace tab to *name*.",
		false,
		blenderObjectSchema(map[string]any{
			"name": blenderStringProperty("Name of the workspace tab to activate."),
		}, "name"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				Name string `json:"name"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(name=" + quotePythonString(input.Name) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeJumpToTabByName, params), true)
		},
	)
}

func jumpToTabBySpaceTypeTool() blenderTool {
	return newBlenderTool(
		"jump_to_tab_by_space_type",
		"Switch to Matching Workspace",
		"Switch to a workspace whose main area matches *space_type*.\n\n"+
			"If *allow_edits* is True and no matching workspace exists, a new one\n"+
			"is created by duplicating the current workspace.",
		false,
		blenderObjectSchema(map[string]any{
			"space_type": blenderStringProperty("Space type to look for, e.g. VIEW_3D or NODE_EDITOR."),
			"allow_edits": blenderBoolProperty(
				"Create a workspace by duplicating the current one when no match exists.",
			),
		}, "space_type"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				SpaceType  string `json:"space_type"`
				AllowEdits *bool  `json:"allow_edits"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(space_type=" + quotePythonString(input.SpaceType) + ", allow_edits=" +
				boolPythonLiteral(input.AllowEdits != nil && *input.AllowEdits) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeJumpToTabBySpaceType, params), true)
		},
	)
}

func jumpToView3DObjectByNameTool() blenderTool {
	return newBlenderTool(
		"jump_to_view3d_object_by_name",
		"Focus on Object",
		"Move the 3D viewport to focus on an object by *name*.\n\n"+
			"If *allow_edits* is True the object may be un-hidden and its\n"+
			"collections enabled to make it visible.",
		false,
		blenderObjectSchema(map[string]any{
			"name":        blenderStringProperty("Name of the object to focus on."),
			"allow_edits": blenderBoolProperty("Un-hide the object and enable its collections if needed."),
		}, "name"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				Name       string `json:"name"`
				AllowEdits *bool  `json:"allow_edits"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(name=" + quotePythonString(input.Name) + ", allow_edits=" +
				boolPythonLiteral(input.AllowEdits != nil && *input.AllowEdits) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeJumpToView3dObjectByName, params), true)
		},
	)
}

func jumpToView3DObjectDataByNameTool() blenderTool {
	return newBlenderTool(
		"jump_to_view3d_object_data_by_name",
		"Focus on Object Data",
		"Move the 3D viewport to the object whose data block matches *name*.\n\n"+
			"If *allow_edits* is True the object may be un-hidden and its\n"+
			"collections enabled to make it visible.",
		false,
		blenderObjectSchema(map[string]any{
			"name":        blenderStringProperty("Name of the data block to locate."),
			"allow_edits": blenderBoolProperty("Un-hide the object and enable its collections if needed."),
		}, "name"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				Name       string `json:"name"`
				AllowEdits *bool  `json:"allow_edits"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(name=" + quotePythonString(input.Name) + ", allow_edits=" +
				boolPythonLiteral(input.AllowEdits != nil && *input.AllowEdits) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeJumpToView3dObjectDataByName, params), true)
		},
	)
}

func renderViewportToPathTool() blenderTool {
	// The reference tool annotates this as read-only even though it writes a render to disk, and
	// the path it reports is resolved inside Blender's scratch directory. Both quirks are kept so
	// behaviour matches the bridge this transport replaces.
	return newBlenderTool(
		"render_viewport_to_path",
		"Render Viewport to Path",
		"Render the current scene to *output_path* using current render settings.",
		true,
		blenderObjectSchema(map[string]any{
			"output_path": blenderStringProperty("Path of the image file to write."),
		}, "output_path"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				OutputPath string `json:"output_path"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(output_path=" + quotePythonString(input.OutputPath) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeRenderViewportToPath, params), true)
		},
	)
}

func renderThumbnailToPathTool() blenderTool {
	return newBlenderTool(
		"render_thumbnail_to_path",
		"Render Thumbnail to Path",
		"Render a small, low-quality thumbnail to *output_path* (temporarily\n"+
			"overrides settings).",
		false,
		blenderObjectSchema(map[string]any{
			"output_path": blenderStringProperty("Path of the thumbnail image file to write."),
		}, "output_path"),
		func(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
			var input struct {
				OutputPath string `json:"output_path"`
			}
			if err := decodeBlenderArguments(arguments, &input); err != nil {
				return errorResult(err.Error()), nil
			}
			params := "Params(output_path=" + quotePythonString(input.OutputPath) + ")"
			return callBlender(ctx, blenderProgram(blenderToolCodeRenderThumbnailToPath, params), true)
		},
	)
}
