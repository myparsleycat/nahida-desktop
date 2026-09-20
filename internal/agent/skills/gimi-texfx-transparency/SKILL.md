---
name: gimi-texfx-transparency
description: Diagnose and repair transparency gaps or opaque/transparent color seams only after confirming GIMI and an active TexFx transparency call on the affected draw; excludes other importers and GIMI paths without TexFx.
---

# GIMI TexFx transparency

## Applicability gate

Use this specialized workflow only when both facts are established from the active scope, INI, or runtime evidence:

1. The active importer is GIMI.
2. The affected draw invokes TexFx transparency, directly or through traced command lists, under the relevant active condition. Examples include `CommandList\TexFx\T`, `Transparency`, or the component path when its installed implementation performs the transparent draw.

Do not activate based only on appearance, a resource named Alpha, `ps-t69`, GIMI itself, the presence of installed TexFx files, a disabled/commented call, or a call on an unrelated part. An unknown importer or untraced call is insufficient. For other importers, GIMI without this active TexFx path, or uncertain applicability, return to `texture-render-diagnosis`; do not apply this case's channel rules or workaround. If this skill was loaded prematurely, stop its specialized workflow and establish the missing evidence through the common skill.

Load `texture-render-diagnosis` for shared DDS inspection, previews, UV selection, hash-verified trials, backups, and rollback. Its tools do not establish this skill's applicability by themselves.

## Trace the installed implementation

Locate the installed TexFx from exposed XXMI roots or the user's supplied location. GIMI libraries may be under the launcher's `Core/GIMI/Libraries`, separate from the mod folder. Do not hardcode a machine path or access outside authorized roots. Inspect the affected call, mask binding, discard rules, intermediate outputs, and overlay composition. Do not assume all TexFx versions implement the same algorithm.

Read `references/texfx-transparency.md` for the observed TexFx 1.0661 case and its evidentiary limits. Confirm that red is transparency in the installed path before treating its histogram as opacity evidence. Alpha can carry another effect. The missing-band mechanism additionally requires the relevant discard/overlay behavior; the 8-bit intermediate-alpha hypothesis must be captured or explicitly reported as conditional.

## Controlled trials for a matching path

Follow the shared skill's preview, authorization, backup, hash, and reload procedure. Change one cause at a time:

- For a confirmed sRGB effect mask and matching rejection mechanism, `reinterpret_linear` is a lossless-header trial. It may remove a gap while increasing transparency; it does not preserve sampled values.
- When that trial changes the intended curve, `linearize_mask` with `sourceColorSpace: "srgb"` can bake the original sampled RGB values into linear RGBA8. Start from verified original or only-retagged bytes, preserve alpha, and inspect effects carried by the other RGB channels. Values quantizing to zero then enter the opaque branch before intermediate storage. Do not repeat the conversion on baked bytes.
- If the gap is fixed but a seam remains at the opaque/transparent transition, inspect both render paths. Only when evidence implicates their color difference, consider a selected-region `minimum_red` trial with `minimumRed: 1`. This keeps near-opaque pixels in the same TexFx path as the transparent portion. It is a workaround that may flatten or change the whole part's lighting, affect outlines/depth, and introduce slight transparency; it does not restore the original opaque lighting.

For `minimum_red`, supply `selection: {rootId, relativePath}` for an opaque black/white PNG at the exact DDS dimensions plus `selectionSHA256`. Use the common skill's UV helper to select complete relevant surfaces, including their upper covering pieces, only after checking shared atlas usage. `wholeTexture: true` is acceptable only when every sampled region should use this path. Do not copy example component IDs or model-space height thresholds. Preserve higher red values and unrelated channels/regions.

Do not use path unification to hide an uninvestigated depth, backface, draw-range, or sorting defect. Do not modify shared TexFx globally as a shortcut for a single mod.

## Interpret the reload result

Compare continuity, transparency strength, and lighting separately. A fixed gap plus a remaining color seam is partial success. A seam removed by path unification supports a render-path color difference, but does not identify a specific missing lighting stage. Obtain a comparable game screenshot or targeted capture; file verification and CPU predictions are not runtime success. If the user requires original lighting, proceed to targeted render-state capture rather than tuning the mask indefinitely.
