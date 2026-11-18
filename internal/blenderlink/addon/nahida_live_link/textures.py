# SPDX-License-Identifier: GPL-3.0-or-later
"""Lists the image files an object's materials use, so the desktop can load them by path.

Packed images are written once to %TEMP%\\nahida-live-link under the BLAKE2b-128 of their bytes. Generated
images and images without a file or packed data are skipped.
"""

import hashlib
import os
import tempfile

import bpy

PACKED_DIR = os.path.join(tempfile.gettempdir(), "nahida-live-link")

_packed_paths = {}


def object_textures(obj):
    """Returns [{"name", "path", "role"}], one entry per distinct image of an Image Texture node in the
    object's material slots, in slot and node order. role is "diffuse" for an image linked to a Principled
    BSDF Base Color, "normal_map" for one linked to a Normal Map node, else ""."""
    found = {}
    visited = set()
    for slot in obj.material_slots:
        material = slot.material
        if material is not None and _uses_nodes(material) and material.node_tree is not None:
            _scan(material.node_tree, found, visited)
    textures = []
    for image, role in found.values():
        path = _image_path(image)
        if path:
            textures.append({"name": image.name, "path": path, "role": role})
    return textures


def _uses_nodes(material):
    # Blender 5.0 deprecated Material.use_nodes: every material uses its node tree.
    return bpy.app.version >= (5, 0, 0) or material.use_nodes


def _scan(tree, found, visited):
    if tree.as_pointer() in visited:
        return
    visited.add(tree.as_pointer())
    for node in tree.nodes:
        if node.type == "TEX_IMAGE" and node.image is not None:
            role = _role(node)
            image, previous = found.get(node.image.name, (node.image, ""))
            found[node.image.name] = (image, previous or role)
        elif node.type == "GROUP" and node.node_tree is not None:
            _scan(node.node_tree, found, visited)


def _role(node):
    for output in node.outputs:
        for link in output.links:
            target = link.to_node
            if target.type == "BSDF_PRINCIPLED" and link.to_socket.identifier == "Base Color":
                return "diffuse"
            if target.type == "NORMAL_MAP":
                return "normal_map"
    return ""


def _image_path(image):
    if image.source != "FILE":
        return ""
    packed = image.packed_file
    if packed is not None:
        return _write_packed(image, packed)
    if not image.filepath:
        return ""
    path = os.path.normpath(bpy.path.abspath(image.filepath, library=image.library))
    return path if os.path.isfile(path) else ""


def _write_packed(image, packed):
    key = (image.name, image.filepath, packed.size)
    path = _packed_paths.get(key)
    if path is not None and os.path.isfile(path):
        return path
    data = packed.data
    extension = os.path.splitext(image.filepath)[1] or ".png"
    path = os.path.join(PACKED_DIR, hashlib.blake2b(data, digest_size=16).hexdigest() + extension.lower())
    if not os.path.isfile(path):
        os.makedirs(PACKED_DIR, exist_ok=True)
        temporary = "%s.%d.tmp" % (path, os.getpid())
        with open(temporary, "wb") as f:
            f.write(data)
        os.replace(temporary, path)
    _packed_paths[key] = path
    return path
