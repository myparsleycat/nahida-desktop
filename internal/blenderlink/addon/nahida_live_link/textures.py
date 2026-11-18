# SPDX-License-Identifier: GPL-3.0-or-later
"""Lists the images an object's materials use and reads their pixels.

A clean external PNG, JPEG, WebP or DDS file is described by path, and the desktop reads the file. Any other
image that has pixels (edited and unsaved, packed, generated, or in a format the desktop does not decode) is
described as "pixels", and the add-on sends what Blender holds for it in TextureBegin transfers.
"""

import os

import bpy
import numpy

ROLE_PROPERTY = "nahida_role"
ROLE_AUTO = "AUTO"
ROLE_ITEMS = (
    (ROLE_AUTO, "Auto", "Use the material wiring or the image name"),
    ("diffuse", "Diffuse", ""),
    ("normal_map", "Normal map", ""),
    ("light_map", "Light map", ""),
    ("material_map", "Material map", ""),
)
FILE_EXTENSIONS = {".png", ".jpg", ".jpeg", ".webp", ".dds"}


def object_textures(obj):
    """Returns [{"name", "role", "source", "path", "version"}], one entry per distinct image of an Image Texture
    node in the object's material slots, in slot and node order. role is the image's Viewer slot when set, else
    "diffuse" for an image linked to a Principled BSDF Base Color, "normal_map" for one linked to a Normal Map
    node, else "". source is "file" with the file's path and "mtime_ns:size" as version, or "pixels" with both
    empty. Images without pixels are left out."""
    found = {}
    visited = set()
    for slot in obj.material_slots:
        material = slot.material
        if material is not None and _uses_nodes(material) and material.node_tree is not None:
            _scan(material.node_tree, found, visited)
    textures = []
    for image, wired in found.values():
        entry = describe(image, wired)
        if entry is not None:
            textures.append(entry)
    return textures


def describe(image, wired_role):
    if image.source not in ("FILE", "GENERATED"):
        return None
    entry = {"name": image.name, "role": image_role(image, wired_role), "source": "pixels", "path": "",
             "version": ""}
    if image.source == "GENERATED" or image.packed_file is not None or image.is_dirty:
        return entry
    path = _file_path(image)
    if not path:
        return None
    if os.path.splitext(path)[1].lower() not in FILE_EXTENSIONS:
        return entry
    try:
        stat = os.stat(path)
    except OSError:
        return None
    entry.update(source="file", path=path, version="%d:%d" % (stat.st_mtime_ns, stat.st_size))
    return entry


def image_role(image, wired_role):
    role = getattr(image, ROLE_PROPERTY, ROLE_AUTO)
    return wired_role if role == ROLE_AUTO else role


def read_pixels(image):
    """Returns (width, height, channels, values, encode_srgb) with Image.pixels in a float32 array, or None for
    an image without pixels. encode_srgb is set for a float buffer that holds color, which Blender keeps in
    scene linear."""
    width, height = image.size
    if width <= 0 or height <= 0:
        return None
    count = len(image.pixels)
    channels = count // (width * height)
    if channels <= 0 or channels > 4 or channels * width * height != count:
        return None
    values = numpy.empty(count, dtype=numpy.float32)
    image.pixels.foreach_get(values)
    encode_srgb = image.is_float and not image.colorspace_settings.is_data
    return width, height, channels, values, encode_srgb


def signature(image):
    """Changes when what read_pixels returns may have changed without a depsgraph update."""
    packed = image.packed_file
    return (
        tuple(image.size), image.is_dirty, image.source, image.is_float, image.colorspace_settings.name,
        packed.as_pointer() if packed is not None else 0, image.filepath,
    )


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


def _file_path(image):
    if not image.filepath:
        return ""
    path = os.path.normpath(bpy.path.abspath(image.filepath, library=image.library))
    return path if os.path.isfile(path) else ""


def _role_changed(image, context):
    from . import handlers

    handlers.link.plan_dirty = True


def register():
    setattr(bpy.types.Image, ROLE_PROPERTY, bpy.props.EnumProperty(
        name="Viewer slot",
        description="Texture slot of the Nahida Desktop viewer that shows this image",
        items=ROLE_ITEMS,
        default=ROLE_AUTO,
        update=_role_changed,
    ))


def unregister():
    if hasattr(bpy.types.Image, ROLE_PROPERTY):
        delattr(bpy.types.Image, ROLE_PROPERTY)
