---
name: mod-diagnosis
description: Diagnose or modify a broken, conflicting, outdated, or visually incorrect XXMI/3DMigoto mod using available fixers, INI evidence, isolation, and compatible comparisons.
---

# Mod diagnosis and modification

Use this skill whenever the user asks to modify a mod or identify the cause of a mod problem. Match the workflow to the request:

- An explicit request to fix, modify, update, or repair a mod authorizes focused file edits inside the exposed scope. When an exact fast path below matches inspected local evidence, apply the smallest reversible edit immediately, verify it by re-reading the changed region, and stop.
- A request only to diagnose, explain, or review does not authorize file edits. Report the evidence and proposed patch instead.
- Do not turn a plausible cause into a confirmed one without file or user-observed evidence.

The exact rules in this entrypoint take precedence over summarized references and imported source snapshots. Imported references provide background, not additional prerequisites. Do not load a large reference, discover broad tool catalogs, or inspect unrelated files after the available evidence is sufficient for an exact rule.

## Direct-fix fast path

Inspect only the affected INI section and enough referenced definitions to test a known rule. Check for an already active equivalent command so the edit will not create a duplicate. If the rule matches, patch, re-read the changed section, briefly state what changed and how to reload or visually verify it, then stop. Use the general workflow only when no exact rule matches, the patch cannot be applied safely, or the user reports that the verified patch did not solve the symptom.

### GIMI green texture caused by a missing no-normal fix

For a GIMI mod, use this exact fast path when all of the following are locally visible:

- the user reports a green or incorrectly green-tinted part and asks for it to be fixed;
- the affected active non-face `TextureOverride` binds `ps-t0` to its Diffuse resource and `ps-t1` to its LightMap resource;
- the path has no active normal-map binding; and
- the path has no active `CommandList\global\ORFix\NNFix` call after those texture assignments.

Insert this line after the `ps-t*` assignments, normally immediately after `ps-t1`, and before any draw command or the next section:

```ini
run = CommandList\global\ORFix\NNFix
```

`Head` and `Body` sections are eligible when their bindings match; do not automatically patch a `Face` section or face resource. If the same confirmed no-normal path instead has an active `CommandList\global\ORFix\ORFix` call, replace that call with `CommandList\global\ORFix\NNFix`. Never add both or duplicate an active call.

Do not require separate inspection of the GIMI library, launcher files, a sibling mod, HLSL, model-viewer metadata, or XXMI configuration to use this canonical global NNFix call in a GIMI-scoped mod. Do not run `tools.inspect_fixes`, enumerate all desktop actions, or load the imported troubleshooting snapshot before applying this exact fast path. The global command does not need to be defined inside the selected mod folder.

For example, if both `TextureOverrideChongyunHead` and `TextureOverrideChongyunBody` have the matching `ps-t0 = ...Diffuse` and `ps-t1 = ...LightMap` layout, add the NNFix call to both sections. Re-read both sections and stop; do not continue investigating merely because the command implementation lives outside the mod folder.

## 1. Establish the symptom and scope

For toggle-dependent transparency gaps, DDS effect masks, abrupt transparency gradients, or opaque/transparent color seams, load `texture-render-diagnosis`. It provides importer-independent channel inspection and reversible DDS trials; do not route these symptoms automatically to a hash fixer or ORFix. Load `gimi-texfx-transparency` only after confirming both GIMI and an active TexFx transparency call on the affected draw. Other importers, GIMI without TexFx on that path, and uncertain cases remain on the common workflow.

- Identify the game/importer, affected object or character part, expected result, visible symptom, when it started, and whether it affects one mod or the whole setup.
- Read the active scope and query available XXMI or desktop state before asking the user for a path, importer, version, or running process that Nahida can already determine locally. Ask only when the available values conflict or remain ambiguous.
- Inspect the authorized sandbox for INIs, resources, bundled instructions, dependencies, disabled files, and logs. Do not claim to inspect the game, launcher, sibling mods, or directories outside the exposed roots.
- Treat a failed directory listing, recursive walk, or basename search as a failure of that operation, not proof that the target is inaccessible or absent. Before asking the user to upload or paste content, list parent directories one level at a time, search a narrower ancestor, or directly read a known filename. Report which operation failed and which alternate access path succeeded.
- Preserve originals and prefer a focused, reversible patch.
- Separate direct observations from hypotheses. Before a trial patch, state the evidence, the predicted visible result, the required reload method, and the rollback. Do not describe a plausible cause as confirmed until the file or runtime evidence verifies it.

Read [references/troubleshooting.md](references/troubleshooting.md) and use the symptom matrix to prioritize checks.

Read [references/source-troubleshooting-guide.md](references/source-troubleshooting-guide.md) only when both this entrypoint and the summarized matrix lack a relevant symptom or concrete example. Do not load it for an ORFix/NNFix case covered above. This imported source retains older prescriptions and external references; never treat its hashes, download suggestions, or tool interfaces as current without local evidence, and never access them directly.

## 2. Check compatible fixers when the symptom suggests an update

- Use a compatible built-in fixer dry run when the evidence suggests stale hashes, a game/importer update, or a known structural migration. Do not use it as a prerequisite for a deterministic local INI edit.
- Call the known read-only `tools.inspect_fixes` action directly. Use `list_desktop_actions` only if the needed action is unknown, and filter discovery by the relevant domain or query instead of listing the full catalog.
- Do not run a mutating fixer merely to diagnose the mod.
- If the dry run reports a required fix, explain the result and affected files. Run the reported fixer only when it is appropriate to the user's requested change.
- If no compatible fixer is available, continue with inspection. Never search for, download, or execute an external fix script.

## 3. Inspect INIs and resource integrity

Load `ini-editing`, then inspect every active path relevant to the symptom:

- missing or misspelled files, includes, resources, and command lists;
- unusual or version-stale hashes, draw ranges, formats, or strides;
- duplicate overrides and hashes, including conflicts across visible INIs;
- disabled entries, malformed values, unmatched conditions, and inline comments;
- texture-slot, buffer, and draw bindings; and
- any error/warning text the user supplied.

Do not delete cache, ShaderFixes, or configuration files. If an external cleanup or in-game reload is a useful test, describe it as a reversible manual experiment and state what each outcome would mean.

## 4. Escalate from static files to runtime evidence

A syntactically correct patch is not proof that the edited path executed. If a focused trial patch produces no visible change after the appropriate reload, do not stack more speculative edits on top of it. Confirm that the patch is present on disk, record the unchanged result, revert the rejected trial when it is no longer useful, and determine whether the edited override, branch, command list, shader, and draw actually ran.

Read [references/runtime-render-diagnosis.md](references/runtime-render-diagnosis.md) when a visual defect remains after a verified static edit, when the disk state differs from a frame-analysis record, or when the symptom may differ between depth, shadow, color, outline, or reflection passes. Load `modding-basics` and its summarized hunting reference when frame analysis or resource hunting is required.

Use registered desktop actions when they are available and directly advance the diagnosis:

- inspect the configured XXMI state before asking the user to retype it;
- capture the target game window before and after a trial so the same visible symptom can be compared;
- after one-time approval, send a focused reload or analysis shortcut to the resolved game window; and
- after one-time approval, start a configured game through XXMI when a full game restart is required.

Key delivery proves only that the input was sent, not that the expected scene, overlay, dump, or reload occurred. Verify the resulting screen or generated files. Ask the user to perform only the in-game navigation, pose, camera setup, or other semantic interaction that no registered action can safely perform. Never assume an exact hunting shortcut is universal; inspect the active configuration or use the overlay instructions supplied by that importer.

## 5. Isolate conflicts when the scope permits

When many mods may be responsible, recommend a binary or "halves" isolation test: disable half, reproduce, and repeat with the failing half. After Mods are ruled out, ShaderFixes can be isolated separately. Nahida should perform this only through registered reversible actions and only when the user's request authorizes those changes; otherwise give manual steps.

Change one variable per test and keep a record of the enabled set. A successful reload without reproducing the original scene is not conclusive.

## 6. Compare a compatible mod

- Look for other mods of the same character/object in the authorized sandbox. They are often one or two ancestor directory levels above the target mod.
- Compare like-for-like INI sections, resource types, slots, draw parameters, buffer layouts, and hashes.
- If the sandbox does not expose the needed ancestor or sibling folders, state that limitation.
- A difference is evidence, not proof. Never replace a hash, stride, format, or draw count solely because another mod differs.

## 7. Report or patch

State the observed evidence, rejected hypotheses, most likely cause, remaining uncertainty, and smallest next test. For an authorized edit, explain the exact render path or reference being changed and preserve a straightforward rollback. Re-read every changed region. Once a sufficient fix has been applied and verified in the files, stop unrelated tool use and perform or request only the necessary runtime check.

Use `F10` first for ordinary INI/resource changes when the importer supports reloading them. Require a full game-process restart when the diagnostic feature or shader source is initialized only on first load, when an importer or DLL changed, or when runtime evidence shows that reload did not refresh the relevant state. Do not require restarting the XXMI application when restarting only the game process is sufficient.

After the user or a captured screen confirms the fix, restore every temporary diagnostic setting. Clean up only artifacts that the current diagnostic session recorded as its own. Preserve the working mod, the final patch, a straightforward original backup, pre-existing ShaderFixes and caches, and any generated file that changed after creation. Report what was restored, removed, and retained.

Do not call the repair complete while a known hunting, shader-save, warning-display, skip, or other diagnostic setting remains unintentionally enabled. Restore it through an available action; when only the user can do so, name the exact pending setting and verify or explicitly report that cleanup remains manual. Treat a shader or dump copied into the active mod solely for this diagnosis as a cleanup candidate, not part of the repair.

## GIMI outline and reflection diagnosis

First distinguish the symptom:

- Wrong reflection or outline coloring on a part with a confirmed normal map can indicate that the appropriate ORFix call is missing from that render path.
- Green skin, a multicolored face, or a newly corrupted part can indicate that ORFix was applied to an incompatible path, especially a face or a part without a normal map.

Inspect every active `TextureOverride` and invoked `CommandList` for the affected part. A line beginning with `;` is inactive.

- Never add or remove a call based only on the symptom name, the presence of `ps-t2`, or a resource label.
- Confirm texture-slot meaning from resource definitions and a compatible part. Faces and no-normal-map paths require special caution.
- For the conventional layout `ps-t0 = NormalMap`, `ps-t1 = Diffuse`, and `ps-t2 = LightMap`, the normal-mapped path may use:

  ```ini
  run = CommandList\global\ORFix\ORFix
  ```

- For a confirmed no-normal-map GIMI path with `ps-t0 = Diffuse` and `ps-t1 = LightMap`, use:

  ```ini
  run = CommandList\global\ORFix\NNFix
  ```

  The direct-fix rule above is sufficient evidence for this canonical call; do not add a separate library- or sibling-mod prerequisite. Do not substitute it automatically on faces.

- Put an applicable call after its `ps-t*` assignments and before `drawindexed`. If branches select textures, place one shared call after `endif` only when all branches use the same layout; otherwise handle each branch before its draw.
- Never add both calls to one render path or duplicate an active call.
- When a bad call is strongly implicated, prefer commenting it out for a reversible test over deleting it. Do not obtain or run an external auto-apply script.
