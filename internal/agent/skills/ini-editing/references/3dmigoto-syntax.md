# XXMI and 3DMigoto INI reference

This syntax extends far beyond ordinary INI files. Some commands are importer- or game-specific, so nearby working sections and the active mod's conventions outrank generic examples.

## Structure and comments

- `[SectionName]` begins a section whose scope continues until the next section or end of file.
- Section and property names are generally case-insensitive, but preserve the author's casing and naming style.
- `;` starts a whole-line comment. Do not append comments after a section or command; some parsers highlight that form but fail to compile it.
- A section name must be unique within its effective namespace unless the importer explicitly documents otherwise.
- `namespace = Name` changes how named variables and qualified references resolve. Do not strip qualifiers such as `CommandList\global\...`.

## Common section roles

### Overrides

`TextureOverride*` and `ShaderOverride*` sections run when their matching rendered resource appears.

- `hash` identifies the resource or shader to match.
- `handling = skip` suppresses the original handling/draw and normally requires an intentional replacement path.
- `draw` and `drawindexed` issue replacement drawing; validate their counts/ranges against the matching buffers.
- `vb0`, `vb1`, and similar properties bind vertex buffers; `ib` binds an index buffer; `ps-t0`, `ps-t1`, and similar properties bind pixel-shader resources.
- `match_first_index` narrows a shared hash to a draw range/material.
- `match_priority` and `allow_duplicate_hash` affect conflict resolution. Do not add them merely to silence a warning without understanding which override should win.

Slot meanings vary. A resource named `Diffuse`, `LightMap`, `NormalMap`, or `Shadow` is evidence supplied by the author, not proof of what a slot means at runtime.

### Resources

`Resource*` sections name files or runtime resources referenced elsewhere.

- `filename` is normally a path relative to the INI. Resolve it from the file's directory and preserve Windows path conventions used by the mod.
- `type` identifies a buffer/resource kind when required.
- `format` describes element representation, especially index buffers and textures.
- `stride` is the byte size of one vertex/element for the declared layout.

If a vertex count is derived from a buffer, verify the exact source file and layout first, then require `file size % stride == 0`. Do not infer stride from a similarly named mod.

### Command lists and includes

`CommandList*` sections act like callable instruction blocks. `run = CommandListName` invokes one, and nested command lists are common. Trace every `run` reached by the affected override, including conditional branches. `pre`, `post`, `ref`/`reference`, and `copy` modify timing or resource assignment semantics; preserve them unless the requested change specifically depends on them.

Includes and globally qualified command lists may resolve outside the selected file or mod folder. If the sandbox does not contain the target, report that the dependency could not be verified.

### Keys, presets, and custom shaders

`Key*` sections can set variables or run a command list. Common interaction types are a one-shot/default assignment, `hold`, `toggle`, and `cycle`; cycle values may advance forward or backward and may wrap. A section can contain repeated `key` entries so keyboard and controller bindings share state. Preserve repeated entries and check other visible INIs for an accidental binding collision before adding a shortcut. Do not assume the source guide's example keys are free in the user's setup.

`Preset*` sections group values that shader overrides can activate. A key can directly assign simple values, while nontrivial calculations belong in an invoked command list. Hold/toggle release behavior and transition timing are importer semantics, so retain existing `post`, delay, and transition commands when editing nearby logic.

Running a `CustomShader*` section can create another draw call. Its render state can include topology, face culling, solid/wireframe fill, color/alpha blending, alpha-to-coverage, shader stages, and execution limits. When a custom effect is invisible, duplicated, inside-out, opaque, or drawn too many times, inspect those states as well as the shader code and bound resources. Do not replace a blend expression, cull mode, topology, or shader-stage binding with a generic recipe without comparing the target draw.

## Variables and conditions

- User variables normally begin with `$`.
- Global variables are commonly declared in `[Constants]`; `persist` values survive reloads through user configuration.
- `if`, `else if`, `else`, and `endif` form nested conditions.
- GIMI-style INIs do not generally support ordinary `for`, `while`, or `switch` constructs.
- Names such as `time`, `run`, condition keywords, and ini-parameter names can be reserved. Preserve an established variable scheme instead of inventing a near-system name.
- Ini-parameter slots used to pass values into shaders are shared rather than namespaced. Reusing an occupied slot can make otherwise unrelated mods interfere with each other.

When inserting a command into conditional code, decide whether it applies once after a shared `endif` or separately inside each branch. Never leave one draw path unfixed by accident.

## Validation checklist

After a focused edit:

1. Confirm every changed section header is closed and every conditional has a matching `endif`.
2. Resolve changed `filename`, `run`, `Resource`, and include references.
3. Check for duplicate active section names and unintended duplicate hashes.
4. Check that a skipped original draw has a deliberate replacement.
5. Check buffer format/stride and draw/count assumptions against the actual files.
6. Preserve disabled lines as comments when reversibility matters; a commented command is not active.
7. Tell the user that an in-game `F10` reload and visual check are manual verification steps Nahida cannot perform.

## Provenance

Adapted from the locally supplied `leotorrez/modding` INI documentation at revision `344a7cbfc1a03693205dfa8c01da312a68b8c841` (GPL-3.0), including its structure, override, resource, command-list, modifier, namespace, property, operator, and constant references. No external access is required.
