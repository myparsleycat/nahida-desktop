# PMX and MMD source conversion

Use this workflow when PMX/PMD/MMD geometry or its imported Blender hierarchy supplies the model, clothing, hair, face, or another part. A PMX model used only as a visual reference belongs to the body/garment or ordinary mesh workflow instead.

## Keep two independent contracts

Never confuse PMX source semantics with the target game's exporter contract.

Build a PMX source profile from evidence:

- source format and file;
- MMD Tools availability/version when import is required;
- MMD root, armature, meshes, rigid bodies, and joints;
- morphs/shape keys and their visible effects;
- materials, images, toon textures, and sphere textures;
- UV/color data;
- Japanese, numeric, and duplicate-suffix vertex groups;
- physics bones, current pose, hierarchy transforms, scale, and axes.

Build the target profile independently using [scene-contract.md](scene-contract.md). PMX bone names, physics, SDEF/QDEF behavior, toon/sphere shading, and morph organization do not define target component numbers, numeric groups, color attributes, tangents, slots, or exporter rotations.

If raw `.pmx` or `.pmd` import is requested and no compatible MMD Tools operator is installed, stop at that prerequisite and give manual setup guidance; do not install it or invent an importer. An already imported `.blend` can still be audited and converted when its data is present.

## Preserve and clean the source

Keep an untouched source and work on a copy. Before deleting anything, inspect hidden objects, morph-controlled variants, materials with alpha, rigid bodies, joints, IK helpers, and shape keys. Apparently missing clothing or body variants may be controlled by morph values rather than absent geometry.

Choose which morphs must be retained, baked, or discarded. Remove source-only physics, display helpers, or alternate variants only from the work copy and only after their relationship to geometry and weights is known. Do not detach the hierarchy or apply armature state while the required source transforms and shape keys are unresolved.

## Align pose and semantics

Align the source and target by stable anatomical landmarks and record scale/axis assumptions. Pose the source armature toward the target rest state before transferring final weights. When a static target mesh is required, checkpoint first, resolve shape-key policy, apply the intended visual state in a controlled order, and verify that meshes, armature, and root did not receive the same transform twice.

Separate by material as an initial semantic aid, not as a final component policy. Rejoin pieces by target meaning—body, face, hair, clothing layer, rigid accessory, or exporter slot—while preserving material membership for later UV and texture reconstruction. Do not merge objects that require different component, visibility, shape-key, or exporter contracts.

## Map groups and weights explicitly

Create an evidence-backed mapping table from each PMX group/bone role to target groups. Mapping can be one-to-one, many-to-one, split by region, or intentionally unmapped. Spelling similarity alone is insufficient, especially for twist, helper, skirt, ribbon, hair, physics, and facial groups.

For each transfer, state the source and destination before running it. Use a known-good target body or neighboring target mesh as the source of target-game deformation when appropriate; do not transfer PMX physics behavior blindly. After transfer, audit unassigned vertices, normalization, influence count, invalid values, unused groups, duplicate suffixes, and remaining non-target groups. Inspect deformation in representative poses, not only rest pose.

## Rebuild material, UV, and target identity

Treat PMX toon and sphere textures as source-shader semantics. Reconstruct the target material channels from a known-good current target. When building an atlas, preserve a reversible source-to-atlas map, padding, UV orientation, and any intentionally tiled coordinates. Check every draw that shares the atlas.

Prefer retaining a known-good target export-slot object and integrating converted geometry into that identity when the exporter depends on its name, custom properties, attributes, or component membership. When joining, make the intended target object active and verify afterward that target properties survived. Some exporters require empty slot objects to remain; remove faces rather than deleting the slot when current evidence shows that policy.

Before handoff, run scene, signature, weight, and material/UV audits on the final target objects. Verify no PMX-only physics groups or helpers remain on export geometry, target seams and neck/body boundaries are valid, required attributes match the known-good target, and backups/reference objects are excluded. Exporter success still requires inspection of generated files/logs and, when requested, a separate runtime validation workflow.
