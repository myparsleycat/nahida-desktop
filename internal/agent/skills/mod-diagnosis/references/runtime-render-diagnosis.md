# Runtime render diagnosis

Use runtime evidence only after the narrow static workflow is insufficient. The goal is to answer one concrete question with the smallest useful capture, not to collect every shader or resource in the scene.

## Keep a hypothesis ledger

For each trial, retain a compact record in the conversation summary:

- **observation**: facts directly visible in files, captures, logs, or dumps;
- **hypothesis**: the proposed cause, explicitly marked as unconfirmed;
- **trial**: the exact reversible change or diagnostic action;
- **prediction**: what should visibly or structurally change if the hypothesis is correct;
- **reload**: whether `F10`, scene re-entry, or a full game-process restart is required;
- **rollback**: the files or settings to restore;
- **result**: `fixed`, `unchanged`, `worse`, or `inconclusive`; and
- **next**: the smallest test that distinguishes the remaining causes.

An `unchanged` result is evidence. First verify that the correct file was loaded and the edited command path ran. If it did, reject or narrow the hypothesis and revert the obsolete trial instead of accumulating speculative patches. A result is `inconclusive` when the target scene, camera, pose, overlay, or capture differed enough that the prediction was not actually tested.

Before applying a disruptive temporary diagnostic command such as `handling = skip`, identify the exact marker and removal patch and tell the user what the reload should show. Keep the temporary edit small enough to remove independently if the conversation or provider is interrupted. Remove it immediately after the observation, before starting the next trial.

## Choose the reload method

- Use `F10` first for ordinary INI command, condition, resource, texture, and buffer changes when the active importer reloads them.
- Re-enter the scene or reload the object when the defect happens only during initial object creation or pose setup.
- Restart the game process when enabling a feature that saves shaders only as they are first loaded, after changing the importer or its DLL, or when evidence shows that `F10` retained stale runtime state.
- Restart the XXMI application only when its own configuration or process state requires it; do not present it as a default prerequisite for restarting the game.

Use a registered window-input action only after resolving one exact target window. Use a registered capture action to compare the same scene before and after a trial. Successful input delivery is not successful verification.

## Distinguish geometry loss from render rejection

Prioritize mesh, buffer, or bone evidence when vertices move to implausible positions, the part explodes or rotates with the pose, or the draw range and output silhouette indicate malformed geometry.

Prioritize alpha, depth, or pass-specific state when the geometry remains spatially plausible but a stable UV-shaped region is cut away, when the missing silhouette already appears in a depth or alpha-writing pass, or when the missing area matches transparency in an original clothing texture.

Prioritize draw-range or overlap evidence when disappearance follows triangle boundaries, ends exactly at an index range, flickers with camera angle, or two surfaces alternately win depth testing.

Do not call one category confirmed from appearance alone. Use draw output, bound resources, buffer metadata, or a controlled trial.

## Compare like-for-like runtime draws

For the affected draw range, compare the available depth, shadow, color, outline, and reflection records. Preserve the identity of each observation:

- draw ID within that capture;
- draw command, count, first index, base vertex, and instance data;
- vertex, pixel, and compute shader hashes or identifiers;
- bound `ps-t*`, `vs-t*`, `vb*`, `ib`, render targets, and depth targets;
- active override, command list, conditions, and resource source; and
- whether alpha test, discard, or another clipping path consumes a bound texture.

Draw IDs are capture-local and are not stable identities. Compare stable shader/resource identities and draw parameters rather than assuming that equal draw IDs across captures refer to the same work.

If the disk INI contains a trial binding but the matching runtime record does not, first investigate whether a different copy of the INI was loaded, another override won, a condition was false, the target used another shader or draw, or the reload method was insufficient. Do not tune the replacement texture before establishing that it reached the target draw.

## Separate shader-hash and texture-slot migrations

After a game update, a replacement can fail because the shader identity changed, the consumed texture slot changed, or both. Test these separately:

1. Hunt the affected shader by a visible hide or skip result. Record that it is a pixel, vertex, or compute shader instead of keeping only a bare hash.
2. Update only the override that performs the replacement, then use one temporary execution probe when needed to confirm that the new hash reaches that command path. A matched override that still shows the original image is evidence against a hash-only repair.
3. Inspect the saved shader input declarations and uses, or an equivalent targeted runtime capture, to identify the slot that consumes the replacement's semantic. Do not infer it from slot number, resource name, or another game version.
4. For a large animated INI whose branches already select each frame into one old slot, preserve that selection chain. When evidence shows the same selected resource must also feed a new slot, add one `ps-t<new> = reference ps-t<old>` after the complete conditional rather than rewriting every frame branch. Keep the old binding when the shader still uses it.
5. Change one candidate slot at a time, reload, and revert an unchanged or harmful binding before trying another.

The slot numbers and hashes from one mod are not reusable rules. This workflow preserves the scalable pattern while requiring current shader evidence.

## Scope a render-state substitution

When runtime evidence confirms that one draw or pass inherits an incompatible texture or other resource, modify the narrowest command path. If later draws depend on the previous binding:

1. save the existing slot by reference;
2. bind the confirmed replacement;
3. execute only the affected draw;
4. restore the previous slot under the same condition; and
5. clear temporary resource references after restoration.

Keep conditional save and restore paths symmetric. Do not encode a shader identifier, component number, resource name, slot meaning, draw count, or first-index value from an example without evidence from the current mod and capture. `ps-t0` is not universally diffuse or alpha, and `ps-t1` is not universally a normal map.

## Bound diagnostic artifacts and cleanup

Before enabling a dump or shader-save option, identify its output directory, expected scope, whether a restart is required, and the possibility of a large disk cost. Prefer a targeted capture. Do not enable broad dumping merely because the exact target is not yet understood; narrow the scene and question first.

For a targeted shader save, locate the generated file in the configured output directory. If that directory is outside the exposed roots, ask for only the exact generated file to be copied into an exposed diagnostic location and then read it by exact filename; do not ask the user to paste the shader or a large INI into chat. Record the copied path so it can be removed or reported as a cleanup candidate after verification.

Track the pre-existing state and every temporary setting or artifact created for the trial. After verification:

- restore temporary importer and diagnostic settings;
- retain the final patch and a straightforward original backup;
- never delete a pre-existing cache, ShaderFixes file, configuration file, or unrelated dump;
- do not delete a generated artifact whose content changed after the diagnostic session created it; and
- report the count and kind of removed artifacts plus the files and runtime state deliberately retained.

If artifact provenance is unavailable, report the cleanup candidates instead of deleting them.
