"""Read-only Blender audits used by Nahida Agent's run_xxmi_audit tool.

The functions in this module read Blender data-blocks and return JSON-serializable dictionaries.
They do not change selection, mode, objects, data, add-ons, or files.
"""

from __future__ import annotations

import hashlib
import math
import os
import re
import struct
from collections import Counter, defaultdict
from typing import Any, Iterable

import bpy


JAPANESE_RE = re.compile(r"[\u3040-\u30ff\u3400-\u9fff]")
NUMERIC_GROUP_RE = re.compile(r"^-?\d+(?:\.\d+)?$")
PHYSICS_WORDS = ("rigid", "joint", "physics", "剛体", "ジョイント", "物理")
EXTERNAL_IMAGE_SOURCES = {"FILE", "SEQUENCE", "MOVIE", "TILED"}


def _vec(values: Any) -> list[float]:
    return [round(float(value), 8) for value in values]


def _json_value(value: Any) -> Any:
    if value is None or isinstance(value, (str, int, bool)):
        return value
    if isinstance(value, float):
        return value if math.isfinite(value) else str(value)
    if hasattr(value, "name"):
        return getattr(value, "name")
    try:
        return [_json_value(item) for item in value]
    except TypeError:
        return repr(value)


def _custom_properties(owner: Any, values: bool = False) -> Any:
    result: dict[str, Any] = {}
    try:
        for key in owner.keys():
            if key == "_RNA_UI":
                continue
            result[str(key)] = _json_value(owner[key]) if values else None
    except Exception:  # Blender RNA access can fail for stale data-blocks.
        return {} if values else []
    return result if values else sorted(result)


def _mesh_targets(object_names: list[str] | None) -> list[bpy.types.Object]:
    if object_names:
        targets = []
        for name in object_names:
            obj = bpy.data.objects.get(name)
            if obj is None:
                raise ValueError(f"Object {name!r} was not found")
            if obj.type != "MESH":
                raise TypeError(f"Object {name!r} must be MESH, got {obj.type}")
            targets.append(obj)
        return targets

    selected = [obj for obj in bpy.context.selected_objects if obj.type == "MESH"]
    if selected:
        return selected
    active = bpy.context.view_layer.objects.active
    if active is not None and active.type == "MESH":
        return [active]
    return [obj for obj in bpy.context.scene.objects if obj.type == "MESH"]


def _modifier_info(modifier: bpy.types.Modifier) -> dict[str, Any]:
    result: dict[str, Any] = {
        "name": modifier.name,
        "type": modifier.type,
        "show_viewport": bool(modifier.show_viewport),
        "show_render": bool(modifier.show_render),
    }
    for attribute in ("object", "target", "vertex_group"):
        try:
            value = getattr(modifier, attribute)
        except Exception:
            continue
        result[attribute] = _json_value(value)
    return result


def _image_info(image: bpy.types.Image) -> dict[str, Any]:
    packed = bool(getattr(image, "packed_file", None)) or bool(
        getattr(image, "packed_files", [])
    )
    requires_external_file = image.source in EXTERNAL_IMAGE_SOURCES and not packed
    absolute = ""
    if image.filepath:
        try:
            absolute = bpy.path.abspath(image.filepath, library=image.library)
        except Exception:
            absolute = image.filepath
    return {
        "name": image.name,
        "source": image.source,
        "filepath": absolute,
        "requires_external_file": requires_external_file,
        "exists_on_disk": packed or bool(absolute and os.path.isfile(absolute)),
        "packed": packed,
        "size": [int(value) for value in image.size],
        "colorspace": getattr(getattr(image, "colorspace_settings", None), "name", None),
        "alpha_mode": getattr(image, "alpha_mode", None),
        "file_format": getattr(image, "file_format", None),
    }


def _material_info(material: bpy.types.Material | None) -> dict[str, Any] | None:
    if material is None:
        return None
    image_nodes = []
    node_types: Counter[str] = Counter()
    if material.use_nodes and material.node_tree:
        for node in material.node_tree.nodes:
            node_types[node.type] += 1
            image = getattr(node, "image", None)
            if image is not None:
                image_nodes.append(
                    {
                        "node": node.name,
                        "label": node.label,
                        "node_type": node.type,
                        "image": _image_info(image),
                    }
                )
    return {
        "name": material.name,
        "use_nodes": bool(material.use_nodes),
        "diffuse_color": _vec(material.diffuse_color),
        "surface_render_method": getattr(material, "surface_render_method", None),
        "blend_method": getattr(material, "blend_method", None),
        "node_type_counts": dict(node_types),
        "image_nodes": image_nodes,
        "custom_property_keys": _custom_properties(material),
    }


def _shape_key_info(obj: bpy.types.Object) -> list[dict[str, Any]]:
    mesh = obj.data
    if not getattr(mesh, "shape_keys", None):
        return []
    return [
        {
            "name": block.name,
            "value": round(float(block.value), 8),
            "mute": bool(block.mute),
            "slider_min": round(float(block.slider_min), 8),
            "slider_max": round(float(block.slider_max), 8),
            "vertex_group": block.vertex_group,
        }
        for block in mesh.shape_keys.key_blocks
    ]


def _group_style(names: list[str]) -> dict[str, Any]:
    numeric = [name for name in names if NUMERIC_GROUP_RE.fullmatch(name)]
    japanese = [name for name in names if JAPANESE_RE.search(name)]
    duplicate_suffix = [name for name in names if re.fullmatch(r".+\.\d{3}", name)]
    return {
        "total": len(names),
        "numeric_count": len(numeric),
        "japanese_count": len(japanese),
        "duplicate_suffix_count": len(duplicate_suffix),
        "numeric_examples": numeric[:30],
        "japanese_examples": japanese[:30],
        "duplicate_suffix_examples": duplicate_suffix[:30],
    }


def _parent_chain(obj: bpy.types.Object) -> list[str]:
    result = []
    seen: set[int] = set()
    parent = obj.parent
    while parent is not None and parent.as_pointer() not in seen:
        seen.add(parent.as_pointer())
        result.append(parent.name)
        parent = parent.parent
    return result


def _attribute_summary(mesh: bpy.types.Mesh) -> list[dict[str, Any]]:
    return [
        {
            "name": attribute.name,
            "domain": attribute.domain,
            "data_type": attribute.data_type,
            "length": len(attribute.data),
        }
        for attribute in mesh.attributes
    ]


def _scene_mesh_info(obj: bpy.types.Object) -> dict[str, Any]:
    mesh = obj.data
    return {
        "object_name": obj.name,
        "mesh_name": mesh.name,
        "visible_viewport": bool(obj.visible_get()),
        "hide_viewport": bool(obj.hide_viewport),
        "hide_render": bool(obj.hide_render),
        "counts": {
            "vertices": len(mesh.vertices),
            "edges": len(mesh.edges),
            "loops": len(mesh.loops),
            "polygons": len(mesh.polygons),
        },
        "location": _vec(obj.location),
        "rotation_euler": _vec(obj.rotation_euler),
        "scale": _vec(obj.scale),
        "dimensions": _vec(obj.dimensions),
        "bound_box_world": [
            _vec(obj.matrix_world @ __import__("mathutils").Vector(corner))
            for corner in obj.bound_box
        ],
        "uv_layers": [layer.name for layer in mesh.uv_layers],
        "materials": [
            slot.material.name if slot.material else None for slot in obj.material_slots
        ],
        "attributes": _attribute_summary(mesh),
        "vertex_groups": [group.name for group in obj.vertex_groups],
        "shape_keys": [item["name"] for item in _shape_key_info(obj)],
        "modifiers": [_modifier_info(modifier) for modifier in obj.modifiers],
        "custom_property_keys": _custom_properties(obj),
        "parent": obj.parent.name if obj.parent else None,
        "parent_type": obj.parent_type,
        "collections": sorted(collection.name for collection in obj.users_collection),
    }


def scene_preflight() -> dict[str, Any]:
    scene = bpy.context.scene
    objects = []
    for obj in sorted(scene.objects, key=lambda item: item.name.lower()):
        item: dict[str, Any] = {
            "name": obj.name,
            "type": obj.type,
            "parent": obj.parent.name if obj.parent else None,
        }
        if obj.type == "MESH":
            item["mesh"] = _scene_mesh_info(obj)
        elif obj.type == "ARMATURE":
            item["bones"] = len(obj.data.bones)
            item["pose_position"] = obj.data.pose_position
        objects.append(item)

    active = bpy.context.view_layer.objects.active
    return {
        "audit": "SCENE_PREFLIGHT_READ_ONLY",
        "read_only": True,
        "blend_file": bpy.data.filepath,
        "is_dirty": bool(bpy.data.is_dirty),
        "blender_version": bpy.app.version_string,
        "scene": scene.name,
        "active_object": active.name if active else None,
        "mode": bpy.context.mode,
        "selected_objects": [obj.name for obj in bpy.context.selected_objects],
        "objects": objects,
    }


def _mmd_markers(obj: bpy.types.Object) -> dict[str, Any]:
    markers: dict[str, Any] = {}
    try:
        marker_type = str(getattr(obj, "mmd_type", "")).upper()
    except Exception:
        marker_type = ""
    if marker_type and marker_type != "NONE":
        markers["mmd_type"] = marker_type

    detail_attribute = {
        "ROOT": "mmd_root",
        "RIGID_BODY": "mmd_rigid",
        "JOINT": "mmd_joint",
    }.get(marker_type)
    if detail_attribute:
        try:
            markers[detail_attribute] = _json_value(getattr(obj, detail_attribute))
        except Exception:
            pass
    properties = _custom_properties(obj, values=True)
    mmd_properties = {
        key: value
        for key, value in properties.items()
        if "mmd" in key.lower() or key.lower() in {"name_j", "name_e"}
    }
    if mmd_properties:
        markers["custom_properties"] = mmd_properties
    return markers


def _physics_class(obj: bpy.types.Object) -> str | None:
    try:
        marker_type = str(getattr(obj, "mmd_type", "")).upper()
    except Exception:
        marker_type = ""
    lowered = obj.name.lower()
    if marker_type in {"RIGID_BODY", "RIGID"} or any(
        word in lowered for word in ("rigid", "剛体")
    ):
        return "RIGID_BODY_CANDIDATE"
    if marker_type == "JOINT" or any(
        word in lowered for word in ("joint", "ジョイント")
    ):
        return "JOINT_CANDIDATE"
    if any(word in lowered for word in PHYSICS_WORDS):
        return "PHYSICS_CANDIDATE"
    return None


def _armature_info(obj: bpy.types.Object) -> dict[str, Any]:
    bones = [bone.name for bone in obj.data.bones]
    return {
        "object": obj.name,
        "data": obj.data.name,
        "parent": obj.parent.name if obj.parent else None,
        "parent_chain": _parent_chain(obj),
        "bone_count": len(bones),
        "bone_name_style": _group_style(bones),
        "pose_position": obj.data.pose_position,
        "matrix_world": [_vec(row) for row in obj.matrix_world],
        "markers": _mmd_markers(obj),
        "custom_properties": _custom_properties(obj, values=True),
    }


def _pmx_mesh_info(obj: bpy.types.Object) -> dict[str, Any]:
    mesh = obj.data
    groups = [group.name for group in obj.vertex_groups]
    return {
        "object": obj.name,
        "mesh": mesh.name,
        "parent": obj.parent.name if obj.parent else None,
        "parent_chain": _parent_chain(obj),
        "counts": {
            "vertices": len(mesh.vertices),
            "edges": len(mesh.edges),
            "loops": len(mesh.loops),
            "polygons": len(mesh.polygons),
        },
        "visible": bool(obj.visible_get()),
        "hide_viewport": bool(obj.hide_viewport),
        "hide_render": bool(obj.hide_render),
        "matrix_world": [_vec(row) for row in obj.matrix_world],
        "dimensions": _vec(obj.dimensions),
        "modifiers": [_modifier_info(modifier) for modifier in obj.modifiers],
        "shape_keys": _shape_key_info(obj),
        "uv_layers": [layer.name for layer in mesh.uv_layers],
        "attributes": _attribute_summary(mesh),
        "vertex_group_style": _group_style(groups),
        "materials": [_material_info(slot.material) for slot in obj.material_slots],
        "markers": _mmd_markers(obj),
        "custom_properties": _custom_properties(obj, values=True),
    }


def pmx_source_audit() -> dict[str, Any]:
    scene = bpy.context.scene
    armatures = [obj for obj in scene.objects if obj.type == "ARMATURE"]
    meshes = [obj for obj in scene.objects if obj.type == "MESH"]
    roots = []
    physics = []
    for obj in scene.objects:
        markers = _mmd_markers(obj)
        lowered = obj.name.lower()
        marker_type = str(markers.get("mmd_type", "")).upper()
        is_named_root = obj.type == "EMPTY" and ("mmd" in lowered or "pmx" in lowered)
        if marker_type == "ROOT" or is_named_root:
            roots.append(
                {
                    "object": obj.name,
                    "type": obj.type,
                    "parent": obj.parent.name if obj.parent else None,
                    "markers": markers,
                }
            )
        physics_class = _physics_class(obj)
        if physics_class:
            physics.append(
                {
                    "object": obj.name,
                    "type": obj.type,
                    "class": physics_class,
                    "parent": obj.parent.name if obj.parent else None,
                    "markers": markers,
                }
            )

    images = [_image_info(image) for image in bpy.data.images]
    warnings = []
    if not roots:
        warnings.append("No MMD/PMX root marker was detected; the source may be generic or already cleaned.")
    if not armatures:
        warnings.append("No armature was detected.")
    if not physics:
        warnings.append("No rigid-body or joint candidate was detected.")

    active = bpy.context.view_layer.objects.active
    return {
        "audit": "PMX_MMD_SOURCE_READ_ONLY",
        "read_only": True,
        "blend_file": bpy.data.filepath,
        "blender_version": bpy.app.version_string,
        "scene": scene.name,
        "active_object": active.name if active else None,
        "selected_objects": [obj.name for obj in bpy.context.selected_objects],
        "mmd_root_candidates": roots,
        "armatures": [_armature_info(obj) for obj in armatures],
        "meshes": [_pmx_mesh_info(obj) for obj in meshes],
        "physics_candidates": physics,
        "images": images,
        "warnings": warnings,
    }


def _weight_report(obj: bpy.types.Object, max_influences: int) -> dict[str, Any]:
    mesh = obj.data
    group_names = {group.index: group.name for group in obj.vertex_groups}
    usage: Counter[int] = Counter()
    zero_vertices: list[int] = []
    non_unit_vertices = []
    excessive_vertices = []
    invalid_weights = []
    minimum = math.inf
    maximum = -math.inf
    total = 0.0

    for vertex in mesh.vertices:
        assignments = []
        for assignment in vertex.groups:
            weight = float(assignment.weight)
            assignments.append((assignment.group, weight))
            usage[assignment.group] += 1
            if not math.isfinite(weight) or weight < 0.0 or weight > 1.000001:
                invalid_weights.append(
                    {
                        "vertex": vertex.index,
                        "group_index": assignment.group,
                        "group": group_names.get(assignment.group),
                        "weight": weight,
                    }
                )
        positive = [(index, weight) for index, weight in assignments if weight > 1e-8]
        weight_sum = sum(weight for _, weight in positive)
        minimum = min(minimum, weight_sum)
        maximum = max(maximum, weight_sum)
        total += weight_sum

        details = [
            {
                "group_index": index,
                "group": group_names.get(index),
                "weight": weight,
            }
            for index, weight in sorted(positive, key=lambda item: -item[1])
        ]
        if not positive:
            zero_vertices.append(vertex.index)
        elif abs(weight_sum - 1.0) > 1e-4:
            non_unit_vertices.append(
                {"vertex": vertex.index, "sum": weight_sum, "influences": details}
            )
        if len(positive) > max_influences:
            excessive_vertices.append(
                {
                    "vertex": vertex.index,
                    "influence_count": len(positive),
                    "influences": details,
                }
            )

    duplicate_bases: dict[str, list[str]] = defaultdict(list)
    for group in obj.vertex_groups:
        match = re.fullmatch(r"(.+)\.(\d{3})", group.name)
        if match:
            duplicate_bases[match.group(1)].append(group.name)
    unused = [
        {"index": group.index, "name": group.name, "locked": bool(group.lock_weight)}
        for group in obj.vertex_groups
        if usage[group.index] == 0
    ]

    count = len(mesh.vertices)
    if count == 0:
        minimum = 0.0
        maximum = 0.0
    return {
        "object": obj.name,
        "mesh": mesh.name,
        "vertices": count,
        "vertex_groups": len(obj.vertex_groups),
        "group_style": _group_style([group.name for group in obj.vertex_groups]),
        "locked_groups": [group.name for group in obj.vertex_groups if group.lock_weight],
        "duplicate_suffix_candidates": dict(duplicate_bases),
        "weight_sum": {
            "minimum": minimum,
            "maximum": maximum,
            "mean": total / count if count else 0.0,
        },
        "unassigned_vertex_count": len(zero_vertices),
        "unassigned_vertices_sample": zero_vertices[:200],
        "non_normalized_vertex_count": len(non_unit_vertices),
        "non_normalized_sample": non_unit_vertices[:100],
        "over_influence_vertex_count": len(excessive_vertices),
        "over_influence_limit": max_influences,
        "over_influence_sample": excessive_vertices[:100],
        "invalid_weight_count": len(invalid_weights),
        "invalid_weight_sample": invalid_weights[:100],
        "unused_group_count": len(unused),
        "unused_groups": unused,
    }


def weight_integrity_audit(
    object_names: list[str] | None = None,
    max_influences: int = 4,
) -> dict[str, Any]:
    return {
        "audit": "WEIGHT_INTEGRITY_READ_ONLY",
        "read_only": True,
        "blend_file": bpy.data.filepath,
        "blender_version": bpy.app.version_string,
        "objects": [
            _weight_report(obj, max_influences) for obj in _mesh_targets(object_names)
        ],
        "notes": [
            "The target profile, not the default, decides the final influence limit.",
            "Unassigned static or intentionally hidden vertices require semantic review.",
        ],
    }


def _material_uv_report(obj: bpy.types.Object) -> dict[str, Any]:
    mesh = obj.data
    material_usage = Counter(polygon.material_index for polygon in mesh.polygons)
    slots = [
        {
            "slot_index": index,
            "polygon_count": material_usage[index],
            "material": _material_info(slot.material),
        }
        for index, slot in enumerate(obj.material_slots)
    ]
    uv_layers = []
    for layer in mesh.uv_layers:
        if len(layer.data):
            u_values = [float(item.uv.x) for item in layer.data]
            v_values = [float(item.uv.y) for item in layer.data]
            bounds = {
                "u_min": min(u_values),
                "u_max": max(u_values),
                "v_min": min(v_values),
                "v_max": max(v_values),
            }
        else:
            bounds = None
        uv_layers.append(
            {
                "name": layer.name,
                "active": bool(layer.active),
                "active_render": bool(layer.active_render),
                "loop_count": len(layer.data),
                "bounds": bounds,
            }
        )

    color_attributes = []
    for index, attribute in enumerate(mesh.color_attributes):
        color_attributes.append(
            {
                "index": index,
                "name": attribute.name,
                "domain": attribute.domain,
                "data_type": attribute.data_type,
                "length": len(attribute.data),
                "active": getattr(mesh.color_attributes, "active_color_index", -1) == index,
                "render": getattr(mesh.color_attributes, "render_color_index", -1) == index,
            }
        )
    return {
        "object": obj.name,
        "mesh": mesh.name,
        "counts": {
            "vertices": len(mesh.vertices),
            "loops": len(mesh.loops),
            "polygons": len(mesh.polygons),
        },
        "uv_layers": uv_layers,
        "color_attributes": color_attributes,
        "all_attributes": _attribute_summary(mesh),
        "material_slots": slots,
        "object_custom_property_keys": _custom_properties(obj),
        "mesh_custom_property_keys": _custom_properties(mesh),
    }


def material_uv_audit(object_names: list[str] | None = None) -> dict[str, Any]:
    images = [_image_info(image) for image in bpy.data.images]
    return {
        "audit": "MATERIAL_UV_READ_ONLY",
        "read_only": True,
        "blend_file": bpy.data.filepath,
        "blender_version": bpy.app.version_string,
        "objects": [_material_uv_report(obj) for obj in _mesh_targets(object_names)],
        "global_images": images,
        "missing_image_count": sum(
            1
            for image in images
            if image["requires_external_file"] and not image["exists_on_disk"]
        ),
        "notes": [
            "UV bounds outside 0..1 can be intentional for tiling or atlas workflows.",
            "Compare attribute names, domains, and types with a known-good current target.",
        ],
    }


def _hash_chunks(chunks: Iterable[bytes]) -> str:
    digest = hashlib.sha256()
    for chunk in chunks:
        digest.update(chunk)
    return digest.hexdigest()


def _pack_int(value: int) -> bytes:
    return struct.pack("<q", int(value))


def _pack_float(value: float) -> bytes:
    return struct.pack("<d", float(value))


def _topology_hash(mesh: bpy.types.Mesh) -> str:
    chunks = [
        _pack_int(len(mesh.vertices)),
        _pack_int(len(mesh.edges)),
        _pack_int(len(mesh.loops)),
        _pack_int(len(mesh.polygons)),
    ]
    for edge in mesh.edges:
        chunks.extend((_pack_int(edge.vertices[0]), _pack_int(edge.vertices[1])))
    for loop in mesh.loops:
        chunks.extend((_pack_int(loop.vertex_index), _pack_int(loop.edge_index)))
    for polygon in mesh.polygons:
        chunks.extend(
            (
                _pack_int(polygon.loop_start),
                _pack_int(polygon.loop_total),
                _pack_int(polygon.material_index),
                _pack_int(1 if polygon.use_smooth else 0),
            )
        )
    return _hash_chunks(chunks)


def _attribute_item_bytes(item: Any) -> bytes:
    for field in ("value", "vector", "color", "uv", "byte_color"):
        if not hasattr(item, field):
            continue
        value = getattr(item, field)
        try:
            return b"".join(_pack_float(component) for component in value)
        except TypeError:
            if isinstance(value, bool):
                return _pack_int(int(value))
            if isinstance(value, int):
                return _pack_int(value)
            if isinstance(value, float):
                return _pack_float(value)
            return str(value).encode("utf-8")
    return repr(item).encode("utf-8")


def _attribute_signatures(mesh: bpy.types.Mesh) -> list[dict[str, Any]]:
    result = []
    for attribute in mesh.attributes:
        digest = hashlib.sha256()
        for item in attribute.data:
            digest.update(_attribute_item_bytes(item))
        result.append(
            {
                "name": attribute.name,
                "domain": attribute.domain,
                "data_type": attribute.data_type,
                "length": len(attribute.data),
                "hash": digest.hexdigest(),
            }
        )
    return result


def _shape_key_signature(mesh: bpy.types.Mesh) -> dict[str, Any]:
    if not mesh.shape_keys:
        return {"names": [], "hash": None}
    chunks = []
    names = []
    for block in mesh.shape_keys.key_blocks:
        names.append(block.name)
        chunks.append(block.name.encode("utf-8") + b"\0")
        for point in block.data:
            chunks.extend(_pack_float(value) for value in point.co)
    return {"names": names, "hash": _hash_chunks(chunks)}


def preservation_signature(object_name: str | None = None) -> dict[str, Any]:
    obj = bpy.data.objects.get(object_name) if object_name else bpy.context.view_layer.objects.active
    if obj is None:
        raise ValueError("No target object was provided and Blender has no active object")
    if obj.type != "MESH":
        raise TypeError(f"Object {obj.name!r} must be MESH, got {obj.type}")
    mesh = obj.data
    groups = {group.index: group.name for group in obj.vertex_groups}

    material_chunks = []
    for slot in obj.material_slots:
        name = slot.material.name if slot.material else ""
        material_chunks.append(name.encode("utf-8") + b"\0")
    material_chunks.extend(_pack_int(polygon.material_index) for polygon in mesh.polygons)

    weight_chunks = []
    for index in sorted(groups):
        weight_chunks.extend((_pack_int(index), groups[index].encode("utf-8") + b"\0"))
    for vertex in mesh.vertices:
        weight_chunks.append(_pack_int(vertex.index))
        for assignment in sorted(vertex.groups, key=lambda item: item.group):
            weight_chunks.extend(
                (_pack_int(assignment.group), _pack_float(assignment.weight))
            )

    return {
        "audit": "PRESERVATION_SIGNATURE_READ_ONLY",
        "read_only": True,
        "blend_file": bpy.data.filepath,
        "blender_version": bpy.app.version_string,
        "object": obj.name,
        "mesh": mesh.name,
        "counts": {
            "vertices": len(mesh.vertices),
            "edges": len(mesh.edges),
            "loops": len(mesh.loops),
            "polygons": len(mesh.polygons),
        },
        "topology_hash": _topology_hash(mesh),
        "coordinate_hash": _hash_chunks(
            _pack_float(value) for vertex in mesh.vertices for value in vertex.co
        ),
        "uv_hashes": {
            layer.name: _hash_chunks(
                _pack_float(value) for item in layer.data for value in item.uv
            )
            for layer in mesh.uv_layers
        },
        "material_hash": _hash_chunks(material_chunks),
        "weight_hash": _hash_chunks(weight_chunks),
        "attributes": _attribute_signatures(mesh),
        "shape_keys": _shape_key_signature(mesh),
        "modifier_stack": [_modifier_info(modifier) for modifier in obj.modifiers],
        "transform": {
            "location": list(obj.location),
            "rotation_euler": list(obj.rotation_euler),
            "scale": list(obj.scale),
        },
    }
