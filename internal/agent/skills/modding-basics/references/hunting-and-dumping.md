# Resource hunting and frame dumps

Hunting and dumping are interactive 3DMigoto/XXMI workflows. Nahida can inspect resulting files and write or explain INI changes. When registered desktop actions are available, Nahida may also capture the resolved game window, start a configured game through XXMI after approval, or send a focused shortcut to that window after approval. Nahida cannot understand or navigate an arbitrary in-game scene on the user's behalf, and it cannot operate third-party collection utilities unless a registered action or enabled MCP explicitly provides that capability.

## Choose the smallest useful capture

Start with a precise question: which visible mesh, texture, shader, or draw call must be identified? Make the target visible and, when practical, disable unrelated mods and shader fixes so the observation is reproducible.

Full frame dumps can consume many gigabytes and produce thousands of files. Prefer a targeted shader/resource dump when a relevant shader hash or index-buffer hash is already known. Do not enable broad dumping or alter the importer's base configuration automatically. Record the output location and pre-existing state before a diagnostic change so only artifacts created by that session can later be proposed for cleanup.

## Interactive hunting workflow

Bindings vary by importer configuration. Use the on-screen instructions or inspected local configuration rather than treating the example keys below as universal. The user must prepare the meaningful game state and identify when the correct target is visible. If registered capture and input actions exist, Nahida may perform the mechanical capture or key delivery after resolving one exact game window and obtaining any required approval.

1. Enable the importer's development/hunting overlay using its documented launcher setting.
2. Display the target in a stable scene.
3. Cycle the relevant category, such as vertex buffers, index buffers, pixel shaders, or textures, until hiding or skipping the selected item isolates the target.
4. Copy the selected hash using the key shown by that overlay. Record the resource category with the hash; a bare hash is ambiguous.
5. If an INI was added or changed to constrain analysis, press `F10` in game to reload it.
6. Trigger frame analysis, commonly with `F8`, only after the target is visible and capture scope is constrained.

Do not state exact number-pad bindings as universal. Ask the user to rely on the on-screen overlay because bindings and categories differ across importer versions.

Successful key delivery proves only that Windows accepted the input. Verify that the overlay, reload, or dump actually occurred by inspecting a subsequent screen capture or newly generated files. Keep the result inconclusive when the target scene changed or was not visible.

## Interpreting a dump

Common main-folder filenames encode a draw ID, binding slot, resource hash, vertex/pixel shader hashes, and extension. A deduplicated folder commonly uses a shorter `hash-format.ext` form. Use the actual filename rather than assuming every importer follows one template.

- Draw IDs describe order within that capture, not a stable identity across runs.
- Resource hashes and shader hashes identify different things; preserve which one was hunted.
- The format suffix on a dumped DDS, such as a linear or sRGB BC format, is evidence for how a replacement should be saved.
- Render-target or output files can help show what one draw produced and narrow a busy capture.
- If a targeted dump contains only logs, check that the INI was saved under the active Mods tree, reloaded, matched the currently visible target, and used the right shader/resource category.

Hashes can change between game versions. Keep the game/importer version with any captured result and do not present an old example hash as current.

## Optional external helpers

Tools such as GUI Collect can reduce a dump to selected targets and can generate include files. Mention them only as optional user-operated aids. Do not obtain them, access their sites, generate claims about their current interface, or edit base importer configuration on their behalf unless a registered Nahida action explicitly supports it.

## Provenance

Adapted from the locally supplied `leotorrez/modding` hunting and texture guides at revision `344a7cbfc1a03693205dfa8c01da312a68b8c841` (GPL-3.0). No external access is required.
