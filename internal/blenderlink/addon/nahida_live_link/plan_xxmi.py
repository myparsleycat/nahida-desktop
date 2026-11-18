# SPDX-License-Identifier: GPL-3.0-or-later
"""Selects the objects XXMI-Tools would export and the settings it would export them with.

With a readable hash.json the rules follow ModExporter.__post_init__, obj_from_col and DataModelXXMI.from_obj
in XXMI-Tools 1.8.2 (migoto/exporter.py, migoto/data/data_model.py). XXMI-Tools aborts the whole export on its
first error; the planner records the error, drops the part or component it belongs to and keeps the rest
streaming. Without a dump, every mesh object with faces becomes a component of its own.
"""

import json
import os
from pathlib import Path

import addon_utils
import bpy

from . import textures

EXPORTER = "xxmi"
GAMES = {
    "GenshinImpact": "GIMI",
    "HonkaiStarRail": "SRMI",
    "ZenlessZoneZero": "ZZMI",
}


class Entry:
    """One exported object: obj extracted into main's object space."""

    __slots__ = ("key", "obj", "main", "collection")

    def __init__(self, key, obj, main, collection):
        self.key = key
        self.obj = obj
        self.main = main
        self.collection = collection


class Plan:
    """What the add-on sends in Scene. message is the Scene dict without scene_rev; entries are in XXMI export
    order; apply_modifiers is the exporter's "Apply modifiers and shapekeys" option."""

    __slots__ = ("message", "entries", "apply_modifiers", "game", "components", "polygons", "textures")

    def __init__(self):
        self.message = None
        self.entries = []
        self.apply_modifiers = True
        self.game = ""
        self.components = []
        self.polygons = {}
        self.textures = {}


class _DefaultOptions:
    """XXMI-Tools' export option defaults, used when XXMI-Tools is not enabled."""

    game = ""
    dump_path = ""
    destination_path = ""
    ignore_hidden = True
    only_selected = False
    apply_modifiers_and_shapekeys = True
    normalize_weights = True
    outline_optimization = True
    outline_rounding_precision = 8
    outline_gate_divergence = 10.0
    outline_custom_normals = False


def xxmi_version():
    for module in addon_utils.modules(refresh=False):
        info = getattr(module, "bl_info", {})
        if info.get("name") == "XXMI_Tools":
            return ".".join(str(part) for part in info.get("version", ()))
    return ""


def object_key(obj):
    uid = getattr(obj, "session_uid", None)
    return "uid:%d" % uid if uid is not None else "name:" + obj.name


_hash_cache = {}


def _load_hashes(path):
    """Returns the components of hash.json, cached by modification time."""
    stat = os.stat(path)
    cached = _hash_cache.get(path)
    if cached is not None and cached[0] == (stat.st_mtime_ns, stat.st_size):
        return cached[1]
    with open(path, encoding="utf-8") as f:
        components = json.load(f)
    _hash_cache[path] = ((stat.st_mtime_ns, stat.st_size), components)
    return components


def _layout(main):
    layout = main.get("3DMigoto:VBLayout")
    if layout is None:
        return None
    entries = []
    for entry in layout:
        entry = entry.to_dict() if hasattr(entry, "to_dict") else dict(entry)
        entries.append({
            "SemanticName": entry["SemanticName"],
            "SemanticIndex": int(entry["SemanticIndex"]),
            "Format": entry["Format"],
        })
    return entries


def _flip_v(main):
    flip_v = {}
    for layer in main.data.uv_layers:
        prop = main.get("3DMigoto:" + layer.name)
        if prop is not None:
            flip_v[layer.name] = bool(prop["flip_v"])
    return flip_v


def _component_settings(fullname, has_blend, has_texcoord, main, xxmi):
    return {
        "fullName": fullname,
        "hasBlend": has_blend,
        "hasTexcoord": has_texcoord,
        "layout": _layout(main) or [],
        "ibFormat": main.get("3DMigoto:IBFormat", ""),
        "flipWinding": bool(main.get("3DMigoto:FlipWinding", False)),
        "flipNormal": bool(main.get("3DMigoto:FlipNormal", False)),
        "flipTangent": bool(main.get("3DMigoto:FlipTangent", False)),
        "flipBitangentSign": bool(main.get("3DMigoto:Tangent", False)),
        "mirror": bool(main.get("3DMigoto:FlipMesh", False)),
        "flipV": _flip_v(main),
        "normalizeWeights": bool(xxmi.normalize_weights),
        "outlines": {
            "enabled": bool(xxmi.outline_optimization),
            "precision": int(xxmi.outline_rounding_precision),
            "gateDivergence": float(xxmi.outline_gate_divergence),
            "customNormals": bool(xxmi.outline_custom_normals),
        },
    }


def _empty_message(game="", source="", output=""):
    return {
        "exporter": EXPORTER,
        "exporter_version": xxmi_version(),
        "game": game,
        "source_folder": source,
        "output_folder": output,
        "settings": b"",
        "components": [],
        "errors": [],
    }


def build(scene):
    """Plans the export of scene. Never raises for user data problems; they end up in message["errors"]."""
    plan = Plan()
    xxmi = getattr(scene, "xxmi", None)
    notes, errors = [], []
    if xxmi is None:
        xxmi = _DefaultOptions()
        notes.append("XXMI-Tools is not enabled.")

    dump_path = Path(xxmi.dump_path)
    if xxmi.dump_path == "":
        notes.append("Dump path not set")
    elif not dump_path.is_dir() or dump_path.suffix != "":
        dump_path = dump_path.parent
    mod_name = dump_path.stem
    if xxmi.destination_path:
        destination = xxmi.destination_path
    elif xxmi.dump_path:
        destination = str(dump_path.parent / (mod_name + "Mod"))
    else:
        destination = ""
    plan.game = xxmi.game if xxmi.game in GAMES else ""
    plan.message = _empty_message(GAMES.get(plan.game, ""), str(dump_path) if xxmi.dump_path else "", destination)
    plan.apply_modifiers = bool(xxmi.apply_modifiers_and_shapekeys)
    if xxmi.game == "":
        notes.append("Please select a valid game before continuing.")
    elif plan.game == "":
        errors.append("The live link does not support %s mods." % xxmi.game)

    hashes = None
    if xxmi.dump_path:
        try:
            hashes = _load_hashes(str(dump_path / "hash.json"))
        except (OSError, ValueError) as exc:
            errors.append("Cannot read %s: %s" % (dump_path / "hash.json", exc))

    candidates = list(scene.objects)
    if xxmi.ignore_hidden:
        candidates = [obj for obj in candidates if obj.visible_get()]
    if xxmi.only_selected:
        candidates = [obj for obj in candidates if obj.select_get()]

    if hashes is not None:
        _plan_dump(plan, hashes, mod_name, candidates, xxmi, errors)
    else:
        _plan_objects(plan, candidates, xxmi)
        if not plan.entries:
            errors = notes + errors + ["No visible mesh object with faces."]

    plan.polygons = {entry.key: len(entry.obj.data.polygons) for entry in plan.entries}
    plan.textures = {entry.key: textures.object_textures(entry.obj) for entry in plan.entries}
    plan.message["errors"] = errors
    return plan


def _plan_objects(plan, candidates, xxmi):
    """One component and one part per mesh object with faces, named after the object."""
    objs = [obj for obj in candidates if obj.type == "MESH" and len(obj.data.polygons) > 0]
    for obj in sorted(objs, key=lambda obj: obj.name):
        key = object_key(obj)
        plan.entries.append(Entry(key, obj, obj, ""))
        plan.components.append(_component_settings(obj.name, True, True, obj, xxmi))
        plan.message["components"].append({
            "name": obj.name,
            "parts": [{"name": obj.name, "objects": [{"object_key": key, "name": obj.name, "collection": ""}]}],
        })


def _plan_dump(plan, hashes, mod_name, candidates, xxmi, errors):
    seen = {}
    for hash_component in hashes:
        component_fullname = mod_name + hash_component["component_name"]
        if hash_component.get("draw_vb", "") == "":
            continue
        if not any(obj.name.startswith(component_fullname) for obj in candidates):
            continue

        parts, entries, first_main = [], [], None
        for index, obj_class in enumerate(hash_component["object_classifications"]):
            part_fullname = component_fullname + obj_class
            matching = [obj for obj in candidates if obj.name.startswith(part_fullname)]
            if not matching:
                errors.append("Cannot find object %s in the scene." % part_fullname)
                continue
            if len(matching) > 1:
                errors.append("Found multiple objects with the name %s." % part_fullname)
                continue
            main = matching[0]
            if index == 0:
                first_main = main
            collections = [c for c in bpy.data.collections if c.name.lower().startswith(part_fullname.lower())]
            if len(collections) > 1:
                errors.append(
                    "Found multiple collections with the name %s. Ensure only one collection exists with that "
                    "name." % part_fullname
                )
                continue

            members = [(main, "")]
            if collections:
                _walk(collections[0], main, xxmi, members)
            part_objects = []
            for obj, collection in members:
                key = object_key(obj)
                seen[key] = seen.get(key, 0) + 1
                if seen[key] > 1:
                    key = "%s#%d" % (key, seen[key])
                entries.append(Entry(key, obj, main, collection))
                part_objects.append({"object_key": key, "name": obj.name, "collection": collection})
            parts.append({"name": part_fullname, "objects": part_objects})

        error = _component_error(component_fullname, first_main)
        if error:
            errors.append(error)
            continue
        plan.components.append(_component_settings(
            component_fullname,
            hash_component.get("blend_vb", "") != "",
            hash_component.get("texcoord_vb", "") != "",
            first_main,
            xxmi,
        ))
        plan.message["components"].append({"name": component_fullname, "parts": parts})
        plan.entries.extend(entries)


def _component_error(fullname, main):
    if main is None:
        return "Component %s has no object for its first part." % fullname
    if main.get("3DMigoto:VBLayout") is None:
        return (
            "Object(%s) is missing custom properties required for export! Reimport the mesh from dump folder."
            % main.name
        )
    if main.get("3DMigoto:IBFormat") is None:
        return "Export doesn't support meshes without index buffer"
    return ""


def _walk(collection, main, xxmi, members):
    objs = [obj for obj in collection.objects if obj.type == "MESH" and obj != main]
    if xxmi.ignore_hidden:
        objs = [obj for obj in objs if obj.visible_get()]
    if xxmi.only_selected:
        objs = [obj for obj in objs if obj.select_get()]
    for obj in sorted(objs, key=lambda obj: obj.name):
        members.append((obj, collection.name))
    for child in collection.children:
        _walk(child, main, xxmi, members)


def encode_settings(plan, layers):
    """Returns the Scene settings block: the component settings plus, per object key, the polygon count of the
    object's own mesh, the names of the UV and color layers that extraction sends as layer k, and the image
    files of its materials."""
    objects = {}
    for key, polygons in plan.polygons.items():
        names = layers.get(key, {})
        objects[key] = {
            "polygons": polygons,
            "uv": list(names.get("uv", ())),
            "color": list(names.get("color", ())),
            "textures": plan.textures.get(key, []),
        }
    document = {"game": plan.game, "components": plan.components, "objects": objects}
    return json.dumps(document, separators=(",", ":")).encode("utf-8")


def scene_message(plan, scene_rev, layers):
    message = dict(plan.message)
    message["scene_rev"] = scene_rev
    message["settings"] = encode_settings(plan, layers)
    return message
