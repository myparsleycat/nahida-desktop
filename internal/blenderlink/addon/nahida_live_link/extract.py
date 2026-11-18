# SPDX-License-Identifier: GPL-3.0-or-later
"""Reads one object the way XXMI-Tools exports it, as live link attributes.

ModExporter.process_mesh and DataExtractor.get_loop_data / get_vertex_data in XXMI-Tools 1.8.2 are the
reference: the evaluated mesh (or the plain mesh without "Apply modifiers"), collection members transformed
into the part's main object space, triangulated, tangents from the TEXCOORD.xy UV map. The add-on sends the
raw Blender values; the desktop applies XXMI-Tools' masking, sorting, flips and format conversion.

Nothing here writes to user objects: every change is made on the temporary mesh from Object.to_mesh().
"""

import bmesh
import numpy

from . import protocol

TANGENT_UV = "TEXCOORD.xy"


class Extracted:
    """One extracted object. attrs maps attribute IDs to bytes; uv_names and color_names name UV layer k and
    color layer k; errors lists (code, message) pairs for ClientError; weights_read reports whether the weight
    attributes were read now rather than copied from the previous extraction."""

    __slots__ = ("vertex_count", "loop_count", "vertex_groups", "attrs", "uv_names", "color_names", "errors",
                 "weights_read")

    def __init__(self):
        self.vertex_count = 0
        self.loop_count = 0
        self.vertex_groups = []
        self.attrs = {}
        self.uv_names = []
        self.color_names = []
        self.errors = []
        self.weights_read = False


WEIGHT_ATTRS = (protocol.ATTR_VG_COUNT, protocol.ATTR_VG_INDEX, protocol.ATTR_VG_WEIGHT)


def extract(depsgraph, entry, apply_modifiers, previous=None, force_weights=True):
    """Extracts entry (a plan_xxmi.Entry). previous is the last Extracted of the same key; its weight bytes are
    reused unless force_weights is set or the topology or the vertex group names changed."""
    obj, main = entry.obj, entry.main
    source = obj.evaluated_get(depsgraph) if apply_modifiers and not sculpts_original(obj) else obj
    mesh = source.to_mesh()
    try:
        return _read(mesh, obj, main, previous, force_weights)
    finally:
        source.to_mesh_clear()


def sculpts_original(obj):
    """Whether Sculpt mode edits obj's own mesh without re-evaluating it. Without viewport modifiers Blender
    writes strokes into the original positions and draws from them, so the evaluated mesh keeps the positions
    from before the stroke until the object leaves Sculpt mode."""
    return obj.mode == "SCULPT" and not any(modifier.show_viewport for modifier in obj.modifiers)


def original_positions(obj):
    mesh = obj.data
    values = numpy.empty(len(mesh.vertices) * 3, dtype=numpy.float32)
    attribute = mesh.attributes.get("position")
    if attribute is not None and attribute.domain == "POINT" and attribute.data_type == "FLOAT_VECTOR":
        attribute.data.foreach_get("vector", values)
    else:
        mesh.vertices.foreach_get("co", values)
    return values.tobytes()


def _read(mesh, obj, main, previous, force_weights):
    out = Extracted()
    out.vertex_groups = [group.name for group in obj.vertex_groups]
    if main != obj:
        mesh.transform(obj.matrix_world)
        mesh.transform(main.matrix_world.inverted())

    bm = None
    if any(polygon_size != 3 for polygon_size in _polygon_sizes(mesh)):
        bm = bmesh.new()
        bm.from_mesh(mesh)
        bmesh.ops.triangulate(bm, faces=bm.faces)
        bm.to_mesh(mesh)
    try:
        loops = len(mesh.loops)
        if loops == 0:
            out.attrs[protocol.ATTR_VERT_CO] = b""
            out.attrs[protocol.ATTR_LOOP_VERT] = b""
            return out

        vertices = len(mesh.vertices)
        out.vertex_count, out.loop_count = vertices, loops
        loop_vert = _corner_verts(mesh, loops)
        out.attrs[protocol.ATTR_LOOP_VERT] = loop_vert

        # The desktop treats the first UV map as TEXCOORD.xy when the mesh has none of that name, so tangents
        # come from the same map.
        has_tangents = False
        tangent_uv = TANGENT_UV if TANGENT_UV in mesh.uv_layers else next(iter(mesh.uv_layers.keys()), "")
        if not tangent_uv:
            out.errors.append(("MISSING_UV_LAYER", "%s: the mesh has no UV map to compute tangents from" % obj.name))
        else:
            try:
                mesh.calc_tangents(uvmap=tangent_uv)
                has_tangents = True
            except RuntimeError as exc:
                out.errors.append(("MISSING_UV_LAYER", "%s: cannot compute tangents from UV map %s: %s" % (
                    obj.name, tangent_uv, exc)))

        coords = numpy.empty(vertices * 3, dtype=numpy.float32)
        mesh.vertices.foreach_get("undeformed_co", coords)
        out.attrs[protocol.ATTR_VERT_CO] = coords.tobytes()
        out.attrs[protocol.ATTR_LOOP_NORMAL] = _corner_normals(mesh, loops, has_tangents)
        if has_tangents:
            tangents = numpy.empty(loops * 3, dtype=numpy.float32)
            mesh.loops.foreach_get("tangent", tangents)
            signs = numpy.empty(loops, dtype=numpy.float32)
            mesh.loops.foreach_get("bitangent_sign", signs)
            out.attrs[protocol.ATTR_LOOP_TANGENT] = tangents.tobytes()
            out.attrs[protocol.ATTR_LOOP_BTSIGN] = signs.tobytes()

        for k, layer in enumerate(mesh.uv_layers):
            if k >= protocol.MAX_LAYERS:
                break
            out.attrs[protocol.attr_uv(k)] = _uv(mesh, layer, loops)
            out.uv_names.append(layer.name)
        k = 0
        for attribute in mesh.color_attributes:
            if attribute.domain not in ("CORNER", "POINT") or k >= protocol.MAX_LAYERS:
                continue
            out.attrs[protocol.attr_color(k)] = _color(mesh, attribute, loops, loop_vert)
            out.color_names.append(attribute.name)
            k += 1

        if (
            force_weights
            or previous is None
            or protocol.ATTR_VG_COUNT not in previous.attrs
            or previous.vertex_count != vertices
            or previous.vertex_groups != out.vertex_groups
            or previous.attrs.get(protocol.ATTR_LOOP_VERT) != loop_vert
        ):
            if bm is None:
                bm = bmesh.new()
                bm.from_mesh(mesh)
            _weights(bm, vertices, len(out.vertex_groups), out)
            out.weights_read = True
        else:
            for attr_id in WEIGHT_ATTRS:
                out.attrs[attr_id] = previous.attrs[attr_id]
        return out
    finally:
        if bm is not None:
            bm.free()


def _polygon_sizes(mesh):
    sizes = numpy.empty(len(mesh.polygons), dtype=numpy.int32)
    mesh.polygons.foreach_get("loop_total", sizes)
    return sizes


def _corner_verts(mesh, loops):
    values = numpy.empty(loops, dtype=numpy.int32)
    attribute = mesh.attributes.get(".corner_vert")
    if attribute is not None and attribute.domain == "CORNER" and attribute.data_type == "INT":
        attribute.data.foreach_get("value", values)
    else:
        mesh.loops.foreach_get("vertex_index", values)
    return values.view(numpy.uint32).tobytes()


def _corner_normals(mesh, loops, has_tangents):
    values = numpy.empty(loops * 3, dtype=numpy.float32)
    normals = getattr(mesh, "corner_normals", None)
    if normals is not None:
        normals.foreach_get("vector", values)
    else:
        if not has_tangents:
            mesh.calc_normals_split()
        mesh.loops.foreach_get("normal", values)
    return values.tobytes()


def _uv(mesh, layer, loops):
    values = numpy.empty(loops * 2, dtype=numpy.float32)
    attribute = mesh.attributes.get(layer.name)
    if attribute is not None and attribute.domain == "CORNER" and attribute.data_type == "FLOAT2":
        attribute.data.foreach_get("vector", values)
    else:
        layer.data.foreach_get("uv", values)
    return values.tobytes()


def _color(mesh, attribute, loops, loop_vert):
    """Byte colors are read as stored (sRGB), which is what XXMI-Tools gets from mesh.vertex_colors; point
    colors are spread to corners."""
    count = loops if attribute.domain == "CORNER" else len(mesh.vertices)
    values = numpy.empty(count * 4, dtype=numpy.float32)
    if attribute.data_type == "BYTE_COLOR":
        legacy = getattr(mesh, "vertex_colors", None)
        if attribute.domain == "CORNER" and legacy is not None and attribute.name in legacy:
            legacy[attribute.name].data.foreach_get("color", values)
        else:
            attribute.data.foreach_get("color_srgb", values)
    else:
        attribute.data.foreach_get("color", values)
    if attribute.domain == "POINT":
        corners = numpy.frombuffer(loop_vert, dtype=numpy.uint32)
        values = values.reshape(-1, 4)[corners].reshape(-1)
    return values.tobytes()


def _weights(bm, vertices, group_count, out):
    counts = numpy.zeros(vertices, dtype=numpy.uint8)
    indices, weights = [], []
    layer = bm.verts.layers.deform.active
    if layer is not None:
        for i, vertex in enumerate(bm.verts):
            items = vertex[layer].items()
            if len(items) > 255:
                raise ValueError("vertex %d is in %d vertex groups; the live link allows 255" % (i, len(items)))
            counts[i] = len(items)
            for group, weight in items:
                indices.append(group)
                weights.append(weight)
    index_array = numpy.array(indices, dtype=numpy.uint16)
    if indices and int(index_array.max()) >= group_count:
        out.errors.append(("VERTEX_GROUP_RANGE", "a vertex refers to a vertex group the object does not have"))
        keep = numpy.repeat(numpy.arange(vertices), counts)
        valid = index_array < group_count
        counts = numpy.bincount(keep[valid], minlength=vertices).astype(numpy.uint8)
        index_array = index_array[valid]
        weights = numpy.array(weights, dtype=numpy.float32)[valid]
    out.attrs[protocol.ATTR_VG_COUNT] = counts.tobytes()
    out.attrs[protocol.ATTR_VG_INDEX] = index_array.tobytes()
    out.attrs[protocol.ATTR_VG_WEIGHT] = numpy.asarray(weights, dtype=numpy.float32).tobytes()
