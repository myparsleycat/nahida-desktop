"""Generate Go raw-string constants for the Blender MCP tool-code bodies.

Reads each `<tool>_toolcode.py` from the cloned reference tree, expands `@include_begin`
blocks exactly like `toolcode_load_from_filepath`, strips the SPDX header and module
docstring, and emits a Go file with one constant per tool.
"""

import ast
import json
import os
import sys

TOOLS = [
    "get_objects_summary",
    "get_object_detail_summary",
    "get_blendfile_summary_datablocks",
    "get_blendfile_summary_path_info",
    "get_blendfile_summary_missing_files",
    "get_blendfile_summary_of_linked_libraries",
    "get_screenshot_of_window_as_json",
    "get_screenshot_of_window_as_image",
    "get_screenshot_of_area_as_image",
    "jump_to_tab_by_name",
    "jump_to_tab_by_space_type",
    "jump_to_view3d_object_by_name",
    "jump_to_view3d_object_data_by_name",
    "render_viewport_to_path",
    "render_thumbnail_to_path",
]

INCLUDE_BEGIN = "# @include_begin: "
INCLUDE_END = "# @include_end"


def expand_includes(path):
    with open(path, encoding="utf-8") as handle:
        lines = handle.read().splitlines(True)
    directory = os.path.dirname(path)
    out = []
    skip = False
    for line in lines:
        if line.startswith(INCLUDE_BEGIN):
            name = line[len(INCLUDE_BEGIN):].rstrip()
            with open(os.path.join(directory, name), encoding="utf-8") as handle:
                out.append(handle.read())
            if out[-1] and not out[-1].endswith("\n"):
                out.append("\n")
            skip = True
        elif skip:
            if line.startswith(INCLUDE_END):
                skip = False
        else:
            out.append(line)
    return "".join(out)


def strip_header(source):
    """Drop the SPDX comment block and the module docstring, keeping the rest verbatim."""
    lines = source.splitlines(True)
    index = 0
    # Leading comment block.
    while index < len(lines) and (lines[index].startswith("#") or not lines[index].strip()):
        index += 1
    body = "".join(lines[index:])
    try:
        tree = ast.parse(body)
    except SyntaxError:
        return body
    if tree.body and isinstance(tree.body[0], ast.Expr) and isinstance(tree.body[0].value, ast.Constant):
        node = tree.body[0]
        if isinstance(node.value.value, str):
            remaining = body.splitlines(True)
            end = getattr(node, "end_lineno", 0)
            body = "".join(remaining[end:])
    return body


def go_const_name(tool):
    parts = tool.split("_")
    return "blenderToolCode" + "".join(part.capitalize() for part in parts)


def main():
    if len(sys.argv) != 2:
        print("usage: gen_blender_toolcode.py <reference tools dir>", file=sys.stderr)
        return 1
    tools_dir = sys.argv[1]

    chunks = [
        "// Code generated from the Blender MCP reference tool-code. DO NOT EDIT.",
        "//",
        "// Source: https://projects.blender.org/lab/blender_mcp (mcp/blmcp/tools/*_toolcode.py)",
        "// The bodies are embedded verbatim so the built-in Blender transport executes exactly the",
        "// same Blender-side Python the official bridge sends. Regenerate with",
        "// internal/agent/testdata/gen_blender_toolcode.py.",
        "",
        "package agent",
        "",
    ]
    for tool in TOOLS:
        path = os.path.join(tools_dir, tool + "_toolcode.py")
        body = strip_header(expand_includes(path)).strip("\n")
        # Interpreted string literals: the tool-code contains backticks (docstrings, repr hints),
        # so a raw Go string literal is not safe here.
        chunks.append("// %s is the Blender-side program for the %s tool." % (go_const_name(tool), tool))
        chunks.append("const %s = %s" % (go_const_name(tool), json.dumps(body)))
        chunks.append("")

    sys.stdout.write("\n".join(chunks))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
