---
name: blender-xxmi-workflows
description: Inspect and modify Blender scenes for XXMI mesh work, including body or garment shaping, PMX/MMD conversion, part and weight transfer, component-safe integration, and export-preserving validation. Not for INI edits, hashes, hunting, texture-only render diagnosis, or runtime mod failures.
---

# Blender XXMI workflows

Use this skill only for work whose primary state lives in a Blender scene. `modding-basics` still owns general XXMI concepts, assets, dumps, and installation. `ini-editing` owns INI and toggle text. `mod-diagnosis` owns broken or conflicting mods and runtime evidence. `texture-render-diagnosis` owns DDS channels, shader bindings, transparency, and texture-only render defects. Those specialized skills take precedence at their boundaries; do not duplicate their workflows here.

An explicit request to create, fix, convert, fit, transfer, or export authorizes focused Blender edits through an enabled Blender MCP. A diagnosis, explanation, or inspection request remains read-only. The MCP does not authorize installing Blender, MMD Tools, XXMI Tools, or another add-on. When no capable Blender MCP is enabled, provide manual Blender steps and state what could not be verified.

## Select the scene workflow

Load [references/scene-contract.md](references/scene-contract.md) for every scene-changing task and for structural diagnosis where source, target, or exporter identity matters. Then load only the matching detailed reference:

- [references/body-garment.md](references/body-garment.md) for body proportions, garment fitting, silhouette changes, cloth/trim/rigid-part separation, and visual candidate review.
- [references/pmx-mmd-conversion.md](references/pmx-mmd-conversion.md) when PMX/PMD/MMD geometry, Japanese bones or groups, MMD morphs, physics, or an MMD Tools hierarchy supplies the source.
- [references/mesh-transfer-export.md](references/mesh-transfer-export.md) for non-PMX part transfer, weight transfer, skeleton/component organization, target-slot integration, and Blender-side export validation.

If a request spans several workflows, work in dependency order and validate a checkpoint before changing the primary objective. Do not load all references speculatively.

## Inspect before editing

Prefer the built-in Blender MCP's `run_xxmi_audit` tool when available:

- `scene_preflight` for object roles, transforms, topology counts, UVs, attributes, groups, shape keys, materials, and modifiers;
- `preservation_signature` for a before/after signature of one mesh;
- `pmx_source` for MMD hierarchy, morph, material, image, physics, and group-style evidence;
- `weight_integrity` for unassigned, non-normalized, excessive, invalid, unused, numeric, and Japanese-group evidence; and
- `material_uv` for material slots, image paths, UV bounds, color attributes, and custom-property keys.

These audits read the scene and do not change selection, mode, objects, or files. Pass explicit `object_names` when the active selection is not the intended scope. Treat an audit result as evidence, not permission to guess the profile or as proof that the visible result is correct.

Before a mutation, identify the authoritative source file, target object, donor/source object, reference object, final export object, actual axes, and the smallest reversible edit that tests the method. If source or destination is genuinely ambiguous and the choice would change user data, ask one concise question. Do not add a redundant confirmation when the user's explicit request and the inspected roles already make the edit unambiguous.

## Work and verification

- Preserve unrelated scene state and exporter-critical contracts. Prefer a separate checkpoint or work copy before topology changes, modifier application, full-object weight rewrites, final joins, or overwriting an existing export.
- Use native Blender operations or focused `bpy` code. Python is appropriate for deterministic selection, audits, signatures, batch transformations, and validation; it must not conceal an uncertain source/target decision.
- Prove a method with one small but visible and reversible edit before repeating it across the model. After two materially equivalent failures, restore the last useful checkpoint and change the root method instead of accumulating variants.
- Verify structure with a fresh audit or signature and verify appearance with ordinary viewport or rendered images at the user's judging scale. Overlays and metrics are diagnostic evidence, not substitutes for the visible result.
- Keep aesthetic alternatives to at most two materially different candidates unless the user asks for more. Use the same camera, pose, and lighting.
- A successful Blender operation is not an in-game validation. Hand runtime verification to `mod-diagnosis` when the scene and exported files are structurally correct but the game result remains wrong.
