---
name: rabbitfx-dependency-remover
description: Remove active RabbitFX texture-binding dependencies from XXMI/3DMigoto mod INIs by reproducing their direct shader-slot bindings and state lifetime. Use for requests to make a mod work without RabbitFX; requires evidence for the affected channel-to-slot mapping.
---

# RabbitFX dependency removal

Convert RabbitFX texture binding into local bindings that preserve the texture state expected by each affected draw. Removing a name or external call alone does not reproduce its behavior.

An explicit request to remove the dependency authorizes focused reversible INI edits inside the exposed roots. Diagnosis, explanation, or review stays read-only. Load `ini-editing` for text-patch mechanics; this skill owns the RabbitFX conversion rules. Use `mod-diagnosis` for runtime failures and `texture-render-diagnosis` for DDS or render defects beyond dependency removal.

## Use the available Nahida tools

Use `search_files`, `search_text`, and bounded `read_file` ranges to inspect the selected mod folder in an authorized sandbox root. Use one `apply_patch` call carrying every edit to an INI, and anchor each edit with its section header when a snippet repeats. Preserve encoding, BOM, line endings, comments, section order, and unrelated content. Do not replace an existing INI wholesale after an edit fails.

Retain the exact original text of every changed region and any added sections in the session so the patch can be reversed. Do not claim a separate backup or working copy was created unless a tool actually created it.

## Inventory active dependencies

Find every INI in the selected mod, including sibling INIs that may prove slot meaning. For case-insensitive `search_text`, set `regex: true` and use patterns such as `(?i)rabbitfx`; also inspect `SetTextures`, direct `ps-t*` bindings, and temporary resource save/restore patterns. Search results are bounded and text search skips files larger than 1 MiB. Cross-check the INI inventory with `list_files` in narrower directories and bounded `read_file` ranges when search coverage is incomplete. A truncated read cannot expose a large file's unread tail by changing line numbers. If available tools cannot cover the full active INI scope, report the missing coverage and do not declare complete dependency removal.

For every active RabbitFX group, record its file, section, conditions, assigned channels, exact referenced resources, external command, covered draw calls, and next texture-state change. Trace local command-list callers when a group lives outside its draw section. A mention only in comments or documentation does not warrant rendering edits. Assignments without an identifiable consumer require tracing before removal. Commands other than a proven texture setter, non-texture values, shader effects, and unsupported channels require inspecting their implementation rather than applying this recipe.

## Prove the slot mapping

Read [references/rabbitfx-removal-playbook.md](references/rabbitfx-removal-playbook.md) when mapping evidence, state lifetime, or control flow needs further analysis.

Prefer equivalent direct bindings in a sibling INI, then consistent bindings or save/bind/restore paths in the same INI or package. An installed RabbitFX implementation is useful only when it is the actual affected consumer and is accessible through an exposed root; do not assume a machine path, installed version, or permission outside those roots. Filename suffixes alone are weak evidence.

One observed WWMI package mapped Normal/Lightmap/Material/Diffuse to `ps-t0`/`ps-t1`/`ps-t2`/`ps-t3`. This is an example, not a universal XXMI mapping, and must not be copied to GIMI or another importer without matching evidence. If the mapping or setter behavior is ambiguous, report the specific missing file or behavior before editing; do not guess.

## Replace each texture-state transition

Replace each semantic assignment and setter call with direct bindings to the same resources in the same execution location, using only proven slots. Preserve resource-reference semantics, conditional branches, draw order, index ranges, draw counts, hashes, mesh resources, and toggles. Several groups in one override can intentionally select different textures for different draws; retain every transition. A channel omitted from a later group may inherit an earlier value or receive a setter default; establish that behavior before substituting independent bindings.

Read [references/example-conversion.md](references/example-conversion.md) for a two-group conversion after local evidence proves its mapping. Choose helper names unique in the actual INI namespace; example identifiers and slots are placeholders.

Direct slot assignments change graphics state. Reuse equivalent existing save/restore helpers when their lifetimes are safe. Otherwise save the slots before the first replacement binding and restore them after the last covered draw, before unrelated rendering or leaving the affected override. Protect exactly the slots that change. If the installed setter exposes a different save/restore lifetime, preserve that lifetime instead of forcing the example's section-wide pattern. Clear temporary references after restoration.

Every path that saves must restore, including conditional paths. Never save in one override and restore in another. Nested regions must use distinct temporary resources or avoid nesting; reusing one set would overwrite the saved state. If control flow prevents establishing a safe lifetime, stop before an uncertain conversion and explain the missing evidence.

## Validate and report

After patching, re-read changed regions and compare them with the recorded inventory:

- No active RabbitFX dependency remains in the selected mod. Ignore pure comments and comment text when classifying active commands; report unrelated active RabbitFX functionality as unresolved rather than deleting it.
- Each replaced group binds the same textures at the same conditional draw boundary. Draw commands and their arguments, hashes, ranges, mesh bindings, and toggle logic are preserved.
- New local resource and command-list targets resolve in the correct namespace. Check section-name collisions and any referenced texture filenames. Understand intentional external targets rather than treating same-file absence as proof of failure.
- Save/restore pairs cover every affected execution path, protect the changed slots, and restore before unrelated rendering. Matching invocation counts alone do not prove control-flow safety.
- The diff contains only the intended conversion and necessary state helpers. Generated checksum comments are not evidence of validity after editing.

Stop after sufficient static checks; do not broaden into unrelated textures or fixers. Static checks cannot establish visual equivalence. If runtime verification is requested or a trial fails, use `mod-diagnosis` and available targeted reload/capture actions. For ordinary INI changes use the established `F10` reload workflow and compare affected parts and later unrelated draws under the same pose and lighting. Claim runtime success only for behavior actually observed.

Report the modified files and group count, slot mapping with concrete evidence, save/restore strategy, static checks, remaining dependencies, runtime verification status, and recovery information.
