# XXMI and 3DMigoto mod fundamentals

Use this reference to explain the files already available to Nahida. Product names below describe the ecosystem; they are not tools Nahida can launch or install.

## Runtime model

XXMI game importers are based on 3DMigoto. A mod reacts to game rendering resources, commonly by matching hashes and replacing or redrawing resources. The important consequence is that a mod can break after a game or importer update even when its files have not changed.

A typical content mod contains:

- one or more `.ini` files that match render resources and describe replacements;
- vertex or blend buffers such as `.buf` files;
- index buffers, often `.ib` files; and
- textures, commonly `.dds` files.

Do not assume every package has every file type. Texture-only mods may have no custom mesh, and shared libraries may intentionally provide INIs or shader resources used by other mods.

The usual interactive cycle is: place the extracted mod under the active importer's Mods directory, make the relevant object visible in game, and press `F10` to reload configuration. Nahida may explain this cycle but cannot press in-game keys, start the game, open a launcher, or confirm the result.

## Package and dependency checks

Before diagnosing content, inspect the archive or folder shape:

1. Find the INI entry points and determine whether an extra wrapper directory was introduced during extraction.
2. Resolve every referenced filename relative to the INI and confirm that the target exists.
3. Note includes, global command lists, shader libraries, or other declared dependencies.
4. Look for README or author instructions already included with the mod.
5. Flag unexpected `.exe`, `.dll`, `.bat`, `.cmd`, `.ps1`, or `.py` files. Do not run them. Some legitimate tools use executable code, but executable code is not required merely to load ordinary model and texture content.

If the author requires an external launcher, add-on, fix script, TexFX, GUI Collect, or another library, explain the prerequisite only. Do not download it, visit its site, or claim it is installed unless an available tool or inspected file proves that. Blender itself may be inspected and operated through an enabled Blender MCP, but that does not authorize downloading Blender, installing add-ons, or inventing unsupported MCP operations.

## Mesh and buffer concepts

- A vertex buffer contains per-vertex data. Its `stride` is the number of bytes per vertex for that specific layout.
- An index buffer selects vertices to form triangles. Its format determines the index width.
- Position, blend/weight, and texture-coordinate buffers serve different roles and are not interchangeable merely because their sizes look similar.
- When a workflow requires a vertex count and the exact position-buffer layout is known, `file size / stride` should be an integer. A remainder is evidence that the assumed file, stride, or layout is wrong.
- Character deformation depends on vertex-group membership, numbering/order, and weights. Missing groups, gaps introduced during export, or weights transferred from the wrong source commonly cause collapsed or severely distorted meshes.
- Imported and replacement models may use different coordinate spaces. Wrong orientation can persist even when two objects appear aligned in an editor; transforms and the importer's expected export orientation matter.

Do not rewrite buffer metadata from a different mod without proving that both mods use the same game object and layout.

## Texture concepts

- Preserve the original texture dimensions, channel meaning, compression/format, and linear-versus-sRGB variant unless there is evidence the target accepts a change.
- DDS filenames in a dump often expose both the resource hash and format. Those are stronger evidence than guessing from appearance.
- `TEXCOORD.xy` commonly maps the primary texture. Additional UV layers can carry game-specific outline, projection, or inside-surface data; do not discard them just because the primary UV looks correct.
- Reversed normals, a damaged or mismatched texture-coordinate buffer, a wrong resource path, or an incorrectly named UV layer can all look like a texture failure.
- Alpha channels may encode opacity, emission, masks, or unrelated packed data depending on the game and texture slot. A bright/glowing result can come from a missing or incorrect alpha channel, but inspect a comparable original before changing it.
- Texture slots are game- and material-specific. Names such as Diffuse, LightMap, NormalMap, or MaterialMap are useful clues, not proof of a slot's meaning.

## Hashes and compatibility

Hashes identify rendered resources for an importer at a point in time. Game updates can change them, and one hash may cover several materials distinguished by draw range or first index. Therefore:

- never transplant a hash solely because a section name is similar;
- compare the game, importer, object/part, resource type, draw parameters, and nearby resource bindings;
- treat a known update map or a compatible built-in fixer as stronger evidence than another arbitrary mod; and
- report the observed old and candidate hashes before changing them.

## Provenance

This reference is an adapted summary of the locally supplied `leotorrez/modding` documentation at revision `344a7cbfc1a03693205dfa8c01da312a68b8c841` (GPL-3.0), especially its getting-started, modding, texture, INI, and troubleshooting guides. The upstream project name is included for attribution; using this reference does not require network access.
