# Mesh transfer, weights, components, and export

Use this workflow for non-PMX part transfers, target-side weight work, component organization, static props, and Blender-side export validation.

## Define transfer direction

Before geometry or weight transfer, identify:

- donor/source geometry;
- destination/export object;
- deformation source and destination;
- skeleton/component mode;
- protected target seams and attributes; and
- whether topology and vertex order may change.

Fit geometry before final weight transfer. A transfer from the wrong direction can look plausible in rest pose while collapsing in animation. Prefer a compatible nearby target mesh or known-good body as the weight source, choose a mapping method suited to the surfaces, and restrict transfer to intended groups and regions.

After transfer, audit unassigned vertices, sums, excessive influences, locked groups, unused groups, and numeric-group requirements. Test representative poses and high-bend joints. Do not treat normalization alone as proof that the bone mapping is correct.

## Preserve target identity

For same-character transfers, retain the target's group, component, attribute, and slot conventions. For cross-character transfers, do not copy donor component numbers or custom properties. Rebuild those fields from the current target profile.

When integrating geometry into an exporter-recognized slot, keep the known-good target object active for a join only when current exporter evidence supports that method. Verify the surviving object name, mesh name, custom properties, material slots, UV layers, color attributes, original index/normal/tangent data, vertex groups, modifiers, and component collection immediately afterward.

Rigid or static props still need the profile's required component, group/weight, UV, color, and naming contract. Use one rigid target group only when the target exporter and animation contract call for it.

## Components and visibility

Component numbers are target data, not semantic labels. Preserve expected slot objects even when they contain no final faces if the exporter enumerates objects rather than geometry. Human-readable suffixes are acceptable only when the installed exporter accepts them and the component identity remains unambiguous.

Use an explicit export whitelist. Hide/exclude behavior varies by exporter; inspect the installed operator, panel state, current project, or a known-good export rather than assuming viewport visibility controls output.

## Export boundary

Before invoking the exporter:

1. compare preservation signatures for every affected final mesh;
2. run weight and material/UV audits on the explicit target objects;
3. verify skeleton mode, component membership, slot policy, group names, UVs, attributes, shape keys, transforms, and modifier state;
4. exclude source, reference, backup, cage, physics, and diagnostic objects;
5. confirm destination paths and avoid overwriting the untouched source; and
6. inspect generated files and exporter logs after the call.

Do not edit INI or toggle syntax under this workflow; load `ini-editing` for those files. Do not explain a clean export as a confirmed game fix; load `mod-diagnosis` for reload, conflict, or runtime render evidence.
