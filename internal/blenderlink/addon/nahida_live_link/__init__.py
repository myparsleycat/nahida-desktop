# SPDX-License-Identifier: GPL-3.0-or-later
"""Streams the meshes of a mod being edited to the Nahida Desktop model viewer.

Blender 4.2 and later read blender_manifest.toml and ignore bl_info.
"""

bl_info = {
    "name": "Nahida Live Link",
    "author": "Nahida Desktop",
    "version": (0, 1, 0),
    "blender": (3, 6, 0),
    "location": "View3D > Sidebar > Nahida",
    "description": "Shows XXMI-Tools exports of the open mod live in the Nahida Desktop model viewer",
    "category": "Import-Export",
}

ADDON_VERSION = ".".join(str(part) for part in bl_info["version"])

from . import handlers, ui  # noqa: E402


def register():
    ui.register()
    handlers.register(ADDON_VERSION)


def unregister():
    handlers.unregister()
    ui.unregister()
