# Body and garment shaping

Use this workflow when the requested outcome is a visible body-proportion or garment-silhouette change. It does not own final INI work, runtime diagnosis, or texture-channel repair.

## Classify before deforming

Separate the affected model conceptually or with temporary non-destructive masks:

- **flex cloth:** fabric that should follow chest, waist, hips, buttocks, or thighs;
- **soft trim:** hems, ribbons, fur, or borders that should follow cloth with controlled offset;
- **semi-rigid:** belts, straps, buckles, armor-backed cloth, and accessories that need limited follow-through;
- **rigid:** metal, hard ornaments, weapons, and mechanically fixed parts that should move as islands or transforms rather than squash; and
- **locked:** face, hands, feet, neck seams, exporter anchors, and unrelated regions that must not move.

Primary classifications should be complete and non-overlapping. Transition masks may overlap only to blend deformation deliberately. Hidden or occluded geometry still matters when it provides a body envelope or clips during animation.

## Shape from landmarks and silhouette

Record stable landmarks such as neck base, shoulder line, chest apex/depth, ribcage, waist minimum, pelvis width, hip maximum, buttock depth, crotch, and upper-thigh envelope. Use a reference body as an envelope and proportion guide, not as geometry to copy blindly.

Work in a visible order appropriate to the request: torso/chest depth, waist, pelvis/hip/buttock contour, upper thighs, soft trim, then semi-rigid and rigid accessories. Preserve garment thickness, clearance, layer order, and design intent. A clothed silhouette should not be forced to reproduce nude anatomy when the garment construction would not show it.

Prefer the simplest controllable method:

1. direct edit or proportional editing for localized contour work;
2. a named shape key for reversible candidate storage;
3. lattice or mesh-deform only when a broader cage improves control;
4. sculpting only when topology, seams, and exporter attributes remain protected; and
5. targeted scripted transforms for deterministic repeatable selections.

Do not begin with remesh, voxel remesh, Dyntopo, decimation, automatic retopology, full-object triangulation changes, or a destructive Boolean unless the current exporter contract explicitly permits topology replacement.

## Prove and compare

Start with one meaningful region and a clearly visible but reversible change. Check front, back, left, right, three-quarter, and affected-area views. Confirm that disconnected layers, opposite-side accessories, seams, and rigid details did not follow unintentionally.

Store alternatives as separate shape keys or checkpoints with one purpose each. Generate no more than two materially distinct aesthetic candidates by default. Compare with the same camera, pose, lighting, and normal whole-character scale. Let the user choose when taste, rather than a structural requirement, decides the winner.

If the metrics change but the silhouette does not, enlarge or reclassify the affected region rather than repeating tiny edits. If rigid details deform, restore the checkpoint and transform them separately. If clipping improves in one pose but fails in others, test representative animation extremes before accepting the shape.
