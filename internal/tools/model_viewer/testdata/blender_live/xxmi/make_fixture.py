"""Builds the XXMI-Tools parity fixtures for the Blender live link.

Run with a Blender that has XXMI-Tools installed as the add-on module `XXMITools`:

    blender -b --python make_fixture.py -- <output dir> <scratch dir>

For each case it builds a synthetic scene, records what the live link add-on sends for every exported
object (capture.json), and runs XXMI-Tools' ModExporter to write the reference mod (export/). The gimi
case also exports an edited copy of the scene (edited.json and edited/) for the live rebuild test.
"""

import json
import math
import shutil
import sys
from pathlib import Path

import bmesh
import bpy
import mathutils

import XXMITools
from XXMITools.migoto.datastructures import GameEnum
from XXMITools.migoto.exporter import ModExporter

MOD = "Fix"
COMPONENT = "Body"


class Reporter:
    def report(self, kind, message):
        print("ModExporter", kind, message)


def layout_entry(name, index, fmt, slot, offset):
    return {
        "SemanticName": name,
        "SemanticIndex": index,
        "Format": fmt,
        "InputSlot": slot,
        "AlignedByteOffset": offset,
        "InputSlotClass": "per-vertex",
        "InstanceDataStepRate": 0,
    }


def vb_layout(weight_name, texcoord1_format):
    return [
        layout_entry("POSITION", 0, "R32G32B32_FLOAT", 0, 0),
        layout_entry("NORMAL", 0, "R32G32B32_FLOAT", 0, 12),
        layout_entry("TANGENT", 0, "R32G32B32A32_FLOAT", 0, 24),
        layout_entry(weight_name, 0, "R32G32B32A32_FLOAT", 1, 0),
        layout_entry("BLENDINDICES", 0, "R32G32B32A32_SINT", 1, 16),
        layout_entry("COLOR", 0, "R8G8B8A8_UNORM", 2, 0),
        layout_entry("TEXCOORD", 0, "R32G32_FLOAT", 2, 4),
        layout_entry("TEXCOORD", 1, texcoord1_format, 2, 12),
    ]


def reset_scene():
    bpy.ops.wm.read_factory_settings(use_empty=True)


def grid_object(name, size, offset, quads=True):
    mesh = bpy.data.meshes.new(name)
    bm = bmesh.new()
    verts = []
    for y in range(size + 1):
        row = []
        for x in range(size + 1):
            fx, fy = x / size - 0.5, y / size - 0.5
            z = 0.25 * math.cos(fx * 3.0) * math.cos(fy * 3.0)
            row.append(bm.verts.new((fx + offset[0], fy + offset[1], z + offset[2])))
        verts.append(row)
    for y in range(size):
        for x in range(size):
            a, b, c, d = verts[y][x], verts[y][x + 1], verts[y + 1][x + 1], verts[y + 1][x]
            if quads:
                bm.faces.new((a, b, c, d))
            else:
                bm.faces.new((a, b, c))
                bm.faces.new((a, c, d))
    bm.to_mesh(mesh)
    bm.free()
    obj = bpy.data.objects.new(name, mesh)
    return obj


def decorate(obj, seed, weighted=True):
    mesh = obj.data
    mesh.polygons.foreach_set("use_smooth", [True] * len(mesh.polygons))
    for edge in mesh.edges:
        if edge.index % 7 == seed % 7:
            edge.use_edge_sharp = True
    for name in ("TEXCOORD.xy", "TEXCOORD1.xy"):
        layer = mesh.uv_layers.new(name=name)
        for loop in mesh.loops:
            co = mesh.vertices[loop.vertex_index].co
            u, v = co.x + 0.5, co.y + 0.5
            if name == "TEXCOORD1.xy":
                u, v = v * 0.5, u * 0.25
            if loop.index % 11 == 0:
                u = -0.0
            layer.data[loop.index].uv = (u, v)
    color = mesh.color_attributes.new("COLOR", "FLOAT_COLOR", "CORNER")
    for loop in mesh.loops:
        k = (loop.index * 7 + seed) % 13
        color.data[loop.index].color = (k / 12.0, 1.2 if k == 3 else 0.5, -0.1 if k == 5 else 0.25, 1.0)
    if not weighted:
        return
    groups = [obj.vertex_groups.new(name=str(index)) for index in range(6)]
    groups.append(obj.vertex_groups.new(name="MASK_hidden"))
    for vertex in mesh.vertices:
        i = vertex.index
        count = 1 + (i + seed) % 6
        for k in range(count):
            group = groups[(i + k * 2 + seed) % len(groups)]
            weight = 0.5 if k in (1, 2) else 1.0 / (k + 1)
            group.add([i], weight, "REPLACE")


def link(obj, collection=None):
    (collection or bpy.context.scene.collection).objects.link(obj)


def set_properties(obj, layout, flip_winding=False, flip_mesh=False, flip_v1=True):
    obj["3DMigoto:VBLayout"] = layout
    obj["3DMigoto:IBFormat"] = "DXGI_FORMAT_R32_UINT"
    obj["3DMigoto:FlipWinding"] = flip_winding
    obj["3DMigoto:FlipNormal"] = False
    obj["3DMigoto:FlipTangent"] = False
    obj["3DMigoto:FlipMesh"] = flip_mesh
    obj["3DMigoto:TEXCOORD.xy"] = {"flip_v": True}
    obj["3DMigoto:TEXCOORD1.xy"] = {"flip_v": flip_v1}


def write_dump(dump, parts):
    dump.mkdir(parents=True, exist_ok=True)
    hashes = [
        {
            "component_name": COMPONENT,
            "root_vs": "",
            "draw_vb": "a0a0a0a0",
            "position_vb": "b1b1b1b1",
            "blend_vb": "c2c2c2c2",
            "texcoord_vb": "d3d3d3d3",
            "ib": "e4e4e4e4",
            "object_indexes": list(range(len(parts))),
            "object_classifications": parts,
            "texture_hashes": [[] for _ in parts],
        }
    ]
    (dump / "hash.json").write_text(json.dumps(hashes, indent=2))
    first = 0
    for part in parts:
        (dump / f"{MOD}{COMPONENT}{part}-ib=e4e4e4e4.txt").write_text(f"index count: 600\nfirst index: {first}\n")
        first += 600


def floats(values):
    return [float(value) for value in values]


def capture_mesh(main, obj, apply_modifiers):
    depsgraph = bpy.context.evaluated_depsgraph_get()
    mesh = obj.evaluated_get(depsgraph).to_mesh() if apply_modifiers else obj.to_mesh()
    if main != obj:
        mesh.transform(obj.matrix_world)
        mesh.transform(main.matrix_world.inverted())
    bm = bmesh.new()
    bm.from_mesh(mesh)
    bmesh.ops.triangulate(bm, faces=bm.faces)
    bm.to_mesh(mesh)
    bm.free()
    loops = len(mesh.loops)
    data = {
        "name": obj.name,
        "polygons": len(obj.data.polygons),
        "positions": [],
        "loopVertices": [],
        "loopNormals": [],
        "loopTangents": [],
        "loopBitangentSigns": [],
        "uvs": {},
        "colors": {},
        "groupCounts": [],
        "groupIndices": [],
        "groupWeights": [],
        "groupNames": [group.name for group in obj.vertex_groups],
    }
    if loops:
        mesh.calc_tangents(uvmap="TEXCOORD.xy")
        coords = [0.0] * (len(mesh.vertices) * 3)
        mesh.vertices.foreach_get("undeformed_co", coords)
        data["positions"] = floats(coords)
        vertex_ids = [0] * loops
        mesh.loops.foreach_get("vertex_index", vertex_ids)
        data["loopVertices"] = vertex_ids
        for key, attr, width in (
            ("loopNormals", "normal", 3),
            ("loopTangents", "tangent", 3),
            ("loopBitangentSigns", "bitangent_sign", 1),
        ):
            values = [0.0] * (loops * width)
            mesh.loops.foreach_get(attr, values)
            data[key] = floats(values)
        for layer in mesh.uv_layers:
            values = [0.0] * (loops * 2)
            layer.data.foreach_get("uv", values)
            data["uvs"][layer.name] = floats(values)
        for attribute in mesh.color_attributes:
            if attribute.domain != "CORNER":
                continue
            values = [0.0] * (loops * 4)
            attribute.data.foreach_get("color", values)
            data["colors"][attribute.name] = floats(values)
    for vertex in mesh.vertices:
        data["groupCounts"].append(len(vertex.groups))
        for element in vertex.groups:
            data["groupIndices"].append(element.group)
            data["groupWeights"].append(float(element.weight))
    obj.to_mesh_clear()
    return data


def capture(game, parts, normalize, outline):
    scene_objects = list(bpy.context.scene.objects)
    first_main = None
    capture_parts = []
    for part in parts:
        fullname = MOD + COMPONENT + part
        main = next(obj for obj in scene_objects if obj.name.startswith(fullname))
        first_main = first_main or main
        objects = [capture_mesh(main, main, True)]
        collection = next(
            (c for c in bpy.data.collections if c.name.lower().startswith(fullname.lower())), None
        )

        def walk(col):
            members = sorted(
                (obj for obj in col.objects if obj.type == "MESH" and obj != main), key=lambda obj: obj.name
            )
            for obj in members:
                objects.append(capture_mesh(main, obj, True))
            for child in col.children:
                walk(child)

        if collection is not None:
            walk(collection)
        capture_parts.append({"fullName": fullname, "objects": objects})
    main = first_main
    flip_v = {}
    for layer in main.data.uv_layers:
        prop = main.get("3DMigoto:" + layer.name)
        if prop is not None:
            flip_v[layer.name] = bool(prop["flip_v"])
    layout = [
        {"SemanticName": e["SemanticName"], "SemanticIndex": e["SemanticIndex"], "Format": e["Format"]}
        for e in (entry.to_dict() for entry in main["3DMigoto:VBLayout"])
    ]
    return {
        "xxmiVersion": list(XXMITools.bl_info["version"]),
        "game": game.name,
        "components": [
            {
                "fullName": MOD + COMPONENT,
                "hasBlend": True,
                "hasTexcoord": True,
                "layout": layout,
                "ibFormat": main["3DMigoto:IBFormat"],
                "flipWinding": bool(main.get("3DMigoto:FlipWinding", False)),
                "flipNormal": bool(main.get("3DMigoto:FlipNormal", False)),
                "flipTangent": bool(main.get("3DMigoto:FlipTangent", False)),
                "flipBitangentSign": bool(main.get("3DMigoto:Tangent", False)),
                "mirror": bool(main.get("3DMigoto:FlipMesh", False)),
                "flipV": flip_v,
                "normalizeWeights": normalize,
                "outlines": outline,
                "parts": capture_parts,
            }
        ],
    }


def export(game, dump, destination, normalize, outline):
    if destination.exists():
        shutil.rmtree(destination)
    exporter = ModExporter(
        context=bpy.context,
        operator=Reporter(),
        dump_path=dump,
        destination=destination,
        credit="",
        game=game,
        ignore_hidden=False,
        apply_modifiers=True,
        only_selected=False,
        copy_textures=False,
        normalize_weights=normalize,
        outline_optimization=outline["enabled"],
        no_ramps=False,
        ignore_duplicate_textures=False,
        write_buffers=True,
        write_ini=True,
        template=None,
        outline_rounding_precision=outline["precision"],
        outline_gate_divergence=outline["gateDivergence"],
        outline_custom_normals=outline["customNormals"],
    )
    exporter.export()


def build_case(case, scratch):
    reset_scene()
    parts = ["A", "B"]
    outline = {"enabled": True, "precision": 8, "gateDivergence": 10.0, "customNormals": False}
    if case == "gimi":
        game, weight_name, texcoord1, normalize = GameEnum.GenshinImpact, "BLENDWEIGHT", "R32G32_FLOAT", True
        flip_winding, flip_mesh, flip_v1 = False, False, True
    elif case == "srmi":
        game, weight_name, texcoord1, normalize = GameEnum.HonkaiStarRail, "BLENDWEIGHTS", "R32G32_FLOAT", True
        flip_winding, flip_mesh, flip_v1 = True, True, True
    else:
        game, weight_name, texcoord1, normalize = GameEnum.ZenlessZoneZero, "BLENDWEIGHTS", "R16G16_FLOAT", True
        flip_winding, flip_mesh, flip_v1 = False, False, False
    layout = vb_layout(weight_name, texcoord1)

    main_a = grid_object(f"{MOD}{COMPONENT}A", 8, (0.0, 0.0, 0.0))
    main_a.matrix_world = mathutils.Matrix.Translation((0.1, -0.2, 0.3)) @ mathutils.Matrix.Rotation(0.4, 4, "Z")
    decorate(main_a, 1)
    set_properties(main_a, layout, flip_winding, flip_mesh, flip_v1)
    link(main_a)

    collection = bpy.data.collections.new(f"{MOD}{COMPONENT}A_parts")
    bpy.context.scene.collection.children.link(collection)
    extra = grid_object("ExtraA1", 4, (1.2, 0.0, 0.0), quads=False)
    extra.matrix_world = mathutils.Matrix.Translation((0.0, 0.5, 0.0)) @ mathutils.Matrix.Scale(1.5, 4)
    decorate(extra, 2)
    link(extra, collection)
    nested = bpy.data.collections.new("Nested")
    collection.children.link(nested)
    loose = bpy.data.meshes.new("LooseA2")
    loose.from_pydata([(0, 0, 0), (1, 0, 0)], [], [])
    link(bpy.data.objects.new("LooseA2", loose), nested)

    main_b = grid_object(f"{MOD}{COMPONENT}B", 6, (0.0, 1.5, 0.0))
    decorate(main_b, 3, weighted=case != "zzmi")
    set_properties(main_b, layout, flip_winding, flip_mesh, flip_v1)
    link(main_b)

    dump = scratch / case / MOD
    write_dump(dump, parts)
    return game, parts, normalize, outline, dump


def edit_scene():
    main = bpy.data.objects[f"{MOD}{COMPONENT}A"]
    for vertex in main.data.vertices:
        if vertex.index % 5 == 0:
            vertex.co.z += 0.05
    bm = bmesh.new()
    bm.from_mesh(main.data)
    bmesh.ops.subdivide_edges(bm, edges=bm.edges[:6], cuts=1, use_grid_fill=True)
    bm.to_mesh(main.data)
    bm.free()
    group = main.vertex_groups["2"]
    for vertex in main.data.vertices:
        if vertex.index % 4 == 0:
            group.add([vertex.index], 0.75, "REPLACE")


def main():
    args = sys.argv[sys.argv.index("--") + 1 :]
    out, scratch = Path(args[0]).resolve(), Path(args[1]).resolve()
    for case in ("gimi", "srmi", "zzmi"):
        game, parts, normalize, outline, dump = build_case(case, scratch)
        target = out / case
        target.mkdir(parents=True, exist_ok=True)
        data = capture(game, parts, normalize, outline)
        (target / "capture.json").write_text(json.dumps(data))
        export(game, dump, target / "export", normalize, outline)
        if case == "gimi":
            edit_scene()
            data = capture(game, parts, normalize, outline)
            (target / "edited.json").write_text(json.dumps(data))
            export(game, dump, target / "edited", normalize, outline)


main()
