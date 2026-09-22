# Scene contract and preservation

Use this reference to establish the current Blender scene's source, target, preservation, and exporter contract. Never import component numbers, group maps, color attributes, UV names, rotation recipes, or DDS formats from a different game, character, guide, or older source file merely because the names look similar.

## Establish authority and roles

The user's latest explicitly designated file is authoritative. When it replaces an earlier file, discard object-role, modifier, shape-key, component, and group-map assumptions and repeat the relevant preflight.

Record enough evidence to distinguish:

- authoritative `.blend` or imported source;
- target/export mesh;
- donor/source mesh;
- visual or deformation reference;
- armature and parent chain;
- known-good target object that defines the exporter contract; and
- current importer/exporter and their installed operators or properties.

Infer left/right, front, and up from the actual model and importer state. Do not assume that Blender Y is forward or that every exporter uses the same rotation correction.

## Build the target profile

Derive, as applicable:

- game/project and importer/exporter identity;
- merged, per-component, or static skeleton mode;
- component and object-slot policy;
- vertex-group naming and maximum bone influences;
- required point, edge, face, and corner attributes;
- color-attribute names, domains, and data types;
- UV-layer names, order, and use;
- original normal, tangent, blend-index, blend-weight, and original-index fields;
- shape-key policy;
- custom-property whitelist;
- export visibility and naming policy;
- axis/rotation convention; and
- whether topology or vertex-order changes are allowed.

Use the current scene, a known-good current target, working mod files, exporter panels/logs, and exported output as evidence. Game name alone is not a profile. If a required field remains unknown, preserve generic Blender data and stop before inventing profile-sensitive values or claiming export compatibility.

## Preserve by default

Unless current target evidence explicitly allows a change, preserve:

- vertex order and topology;
- edge, loop, polygon, and corner order;
- UV layers and per-loop UV data;
- material slots and polygon material indices;
- color and custom attributes, including original normal/tangent/index data;
- vertex-group names, indices, and weights outside the requested scope;
- shape-key names, order, and unaffected coordinates;
- object transforms, parents, armatures, and modifier order;
- component identity and exporter-recognized slot objects; and
- intentional seams, gaps, overlaps, and inner/outer layers.

Before a destructive boundary, prefer a separate work copy or checkpoint and capture `preservation_signature` for each affected export mesh. Never overwrite the user's untouched source automatically. Applying modifiers, joining final objects, deleting source geometry, changing topology, rewriting all weights, or replacing an existing export deserves an explicit rollback point; if the request did not clearly authorize that boundary, explain it and ask before crossing it.

## Validate in two dimensions

Structural checks should compare topology/counts, attributes, UVs, materials, weights, shape keys, modifier stack, transforms, and component identity. Geometry checks should cover the task-relevant risks: zero-area or flipped faces, clipping, gaps, unintended intersections, rigid-part deformation, layer-order errors, unweighted vertices, and excessive influences.

Visual checks should include the affected area and an ordinary whole-character or whole-prop view at the scale where the user judges the mod. For rigged work, use representative poses. A numerical change or clean audit is incomplete when the requested appearance is not visible or is visibly wrong.

Before export, explicitly whitelist final objects and exclude references, backups, cages, physics helpers, and diagnostics. Verify skeleton mode, components, attributes, UVs, shape-key state, accepted naming, unapplied modifiers, exporter visibility, generated files, and logs. A dry export is useful when the installed exporter supports it, but it does not prove the in-game render path.
