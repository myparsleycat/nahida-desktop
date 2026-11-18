# Ported export code

The Blender live link previews meshes as the mod's Blender exporter would write them. The numeric part of
each exporter is ported to Go so a Blender edit does not wait for the Python export. Blender-side work
(evaluating modifiers, triangulating, `calc_tangents`, object selection and transforms) stays in the add-on
and reaches Go as `blenderExportMesh`.

Update the commit and the tables below whenever the ported code is moved to a newer upstream version, and
regenerate the fixtures under `testdata/blender_live/` with the same version.

## XXMI-Tools (GIMI, SRMI, ZZMI)

- Repository: <https://github.com/leotorrez/XXMITools>
- Version: v1.8.2, commit `45be554`
- Fixtures: `testdata/blender_live/xxmi/make_fixture.py` builds scenes in Blender, records what the add-on
  sends (`capture.json`) and runs the real `ModExporter` to write the reference output (`export/`).

| Python (`XXMITools/`) | Go |
| --- | --- |
| `migoto/data/data_model.py` `DataModelXXMI.from_obj` | `blender_export_xxmi.go` `buildXXMILayout` |
| `migoto/data/data_extractor.py` `make_proxy_layout` | `xxmiSemantic.proxy`, `xxmiBlenderFormat` |
| `migoto/data/data_extractor.py` `get_data`, `get_loop_data`, `get_vertex_data` | `exportXXMIObject` |
| `migoto/data/data_extractor.py` `fetch_data`, `sanitize_blender_data` | `blender_export_common.go` `fetchExportColumn`, `exportSanitize` |
| `migoto/data/byte_buffer.py` `NumpyBuffer.remove_duplicates` | `xxmiUniqueLoops` |
| `migoto/data/byte_buffer.py` `AbstractSemantic.get_name`, `__eq__` | `exportSemanticName`, `xxmiSemantic.same` |
| `migoto/data/dxgi_format.py` type encoders | `exportEncode`, `exportCast`, `exportFloat16Value` |
| `migoto/data/data_model.py` `converter_flip_vector`, `converter_mirror_vector` | `exportNegate` |
| `migoto/data/data_model.py` `converter_flip_texcoord_v` | `exportFlipV` |
| `migoto/data/data_model.py` `converter_resize_second_dim` | `exportResize` |
| `migoto/data/data_model.py` `converter_normalize_weights` | `xxmiNormalizeWeights` |
| `migoto/data/data_model.py` `converter_flip_bitangent_sign` | `exportNegate` (Zenless format converter) |
| `migoto/exporter.py` `ModExporter.generate_buffers` | `exportXXMIComponent` |
| `migoto/exporter.py` `ModExporter.optimize_outlines` | `optimizeXXMIOutlines` |

Behavior kept on purpose because the game sees it:

- The index buffer numbers loop rows by their bytes, while the vertex rows come from `numpy.unique`, which
  compares by value. Rows that differ only in the sign of a zero can make the two disagree.
- Weights are normalized only for the semantic spelled `BLENDWEIGHTS`; the `is` comparison in
  `from_obj` skips the `BLENDWEIGHT` alias.
- UNORM and SNORM values wrap instead of clamping, as `astype` does.

Differences:

- No INI or mod folder is written. `blender_scene.go` decodes the exported buffers in memory with the mod
  loader's decoders (`extractModelViewerGeometry`, the blend decoder) into one viewer mesh per object.
- Without a `hash.json`, each object is its own component, and an object without `3DMigoto:VBLayout` gets
  the GIMI layout from `defaultXXMILayout`. An object without a `TEXCOORD.xy` layer uses its first UV layer.
- Textures come from the images the object's materials use, not from a dump.
