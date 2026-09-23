# Resource hunting and frame dumps

Hunting and dumping are interactive 3DMigoto/XXMI workflows. Nahida can inspect resulting files and write or explain INI changes. When `hunting.*` actions are available, Nahida can inspect the live importer configuration and prepare a recoverable manual hunting session. The user always chooses when to advance a category and when to mark a found resource from the controls on the Agent conversation page.

## Choose the smallest useful capture

Start with a precise question: which visible mesh, texture, shader, or draw call must be identified? Make the target visible and, when practical, disable unrelated mods and shader fixes so the observation is reproducible.

Full frame dumps can consume many gigabytes and produce thousands of files. Prefer a targeted shader/resource dump when a relevant shader hash or index-buffer hash is already known. Do not enable broad dumping or alter the importer's base configuration automatically. Record the output location and pre-existing state before a diagnostic change so only artifacts created by that session can later be proposed for cleanup.

## User-controlled hunting workflow

Prefer this workflow whenever the high-level actions are listed:

1. Ask the user to make the target visible in the game.
2. Call `hunting.inspect` for the relevant importer. Do not guess whether the green hunting overlay is active; inspect the returned image. If the overlay cannot be classified, ask the user to turn it off and inspect again.
3. Call `hunting.begin` with the exact PID and observed `initiallyActive` state. This approval prepares hunting mode and exposes the configured category buttons on the conversation page.
4. Stop using hunting actions. Never cycle, scan, continue, accept, or cancel resources on the user's behalf. Those autonomous actions do not exist by design.
5. Tell the user which category is likely useful and let them press the corresponding IB, VB, VS, PS, CS, GS, DS, or HS button. Each press focuses the selected game and sends exactly one configured next-resource binding.
6. When the visible result isolates the target, the user presses **Found hash**. The page sends the configured mark binding, reads the clipboard hash, restores the pre-session hunting state, and displays the category together with the hash. The user can also press **Cancel** to restore state without marking anything.

Animated scenes are supported because the application does not infer candidates from screenshots. The visual decision belongs to the user, so background motion, login animations, particles, subtitles, and camera movement do not produce `hunting_scene_unstable` failures. Vertex-buffer hunting still covers only the current/default slot.

If inspect reports `HUNTING_DISABLED`, tell the user to enable **Enable Hunting** in the corresponding XXMI importer settings and relaunch the game. Do not edit `d3dx.ini` to bypass that setting. If the clean baseline still shows the hunting overlay, cancel and repeat inspection with the correct initial state.

## Manual controls

Bindings vary by importer configuration. The conversation-page controls use the bindings read from the active importer; do not treat example keys as universal. The user must prepare the meaningful game state and identify when the correct target is visible.

1. Enable the importer's development/hunting overlay using its documented launcher setting.
2. Display the target in a stable scene.
3. Use the category buttons on the Agent conversation page to cycle resources until hiding or skipping the selected item isolates the target.
4. Press **Found hash** to mark the selected resource. Record the resource category with the hash; a bare hash is ambiguous.
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
