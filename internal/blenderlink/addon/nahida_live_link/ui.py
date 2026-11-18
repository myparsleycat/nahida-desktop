# SPDX-License-Identifier: GPL-3.0-or-later
"""N panel of the live link: connection state, scene and per-object status, resync and connect buttons, and the
viewer slot of each image of the active object's materials."""

import bpy

from . import handlers, plan_xxmi, textures, transport

STATE_LABELS = {
    transport.STATE_STOPPED: ("Stopped", "CANCEL"),
    transport.STATE_CONNECTING: ("Waiting for Nahida Desktop", "TIME"),
    transport.STATE_CONNECTED: ("Connected", "LINKED"),
    transport.STATE_INCOMPATIBLE: ("Incompatible Nahida Desktop version", "ERROR"),
    transport.STATE_REJECTED: ("Rejected by Nahida Desktop", "ERROR"),
}


class NAHIDA_OT_live_link_resync(bpy.types.Operator):
    bl_idname = "nahida_live_link.resync"
    bl_label = "Resync"
    bl_description = "Send the scene and every object again, including vertex weights"

    def execute(self, context):
        handlers.link.force_all = True
        handlers.link.sent_scene = None
        return {"FINISHED"}


class NAHIDA_OT_live_link_connect(bpy.types.Operator):
    bl_idname = "nahida_live_link.connect"
    bl_label = "Connect"
    bl_description = "Connect to Nahida Desktop, or disconnect"

    connect: bpy.props.BoolProperty(default=True)

    def execute(self, context):
        if self.connect:
            handlers.link.start(_addon_version())
        else:
            handlers.link.stop()
        return {"FINISHED"}


class NAHIDA_PT_live_link(bpy.types.Panel):
    bl_idname = "NAHIDA_PT_live_link"
    bl_label = "Nahida Live Link"
    bl_space_type = "VIEW_3D"
    bl_region_type = "UI"
    bl_category = "Nahida"

    def draw(self, context):
        link = handlers.link
        layout = self.layout
        label, icon = STATE_LABELS.get(link.state, (link.state, "QUESTION"))
        layout.label(text=label, icon=icon)
        if link.state == transport.STATE_CONNECTED:
            layout.label(text="Viewer attached" if link.viewer_attached else "No viewer attached",
                         icon="HIDE_OFF" if link.viewer_attached else "HIDE_ON")

        row = layout.row(align=True)
        if link.transport is None:
            row.operator(NAHIDA_OT_live_link_connect.bl_idname, text="Connect", icon="LINKED").connect = True
        else:
            row.operator(NAHIDA_OT_live_link_connect.bl_idname, text="Disconnect", icon="UNLINKED").connect = False
            row.operator(NAHIDA_OT_live_link_resync.bl_idname, icon="FILE_REFRESH")

        plan = link.plan
        if plan is None:
            return
        if link.scene_status:
            layout.label(text="Scene: %s %s" % (link.scene_status, link.scene_message))
        for error in plan.message["errors"]:
            layout.label(text=error, icon="ERROR")
        box = layout.box()
        for component in plan.message["components"]:
            box.label(text=component["name"], icon="OUTLINER_COLLECTION")
            for part in component["parts"]:
                for obj in part["objects"]:
                    state = link.objects.get(obj["object_key"])
                    status = state.status if state else ""
                    icon = "ERROR" if status in ("error", "rejected") else "MESH_DATA"
                    row = box.row()
                    row.label(text=obj["name"], icon=icon)
                    row.label(text=status)
                    if state and state.message:
                        box.label(text=state.message)
        _draw_viewer_slots(layout, context, link, plan)


def _draw_viewer_slots(layout, context, link, plan):
    """Lists the images of the active object's materials with their viewer slot."""
    obj = context.active_object
    if obj is None:
        return
    # Keys, not plan objects: those may be freed by an undo until the next replan.
    entries = plan.textures.get(plan_xxmi.object_key(obj))
    if not entries:
        return
    box = layout.box()
    box.label(text="Viewer slots", icon="TEXTURE")
    for entry in entries:
        image = bpy.data.images.get(entry["name"])
        if image is None:
            continue
        row = box.row()
        row.label(text=image.name, icon="IMAGE_DATA")
        row.prop(image, textures.ROLE_PROPERTY, text="")
        state = link.textures.images.get(image.name)
        if state is not None and state.status == "rejected":
            box.label(text=state.message, icon="ERROR")
        elif state is not None and state.status == "unmatched":
            box.label(text=state.message, icon="INFO")


def _addon_version():
    from . import ADDON_VERSION

    return ADDON_VERSION


CLASSES = (NAHIDA_OT_live_link_resync, NAHIDA_OT_live_link_connect, NAHIDA_PT_live_link)


def register():
    for cls in CLASSES:
        bpy.utils.register_class(cls)


def unregister():
    for cls in reversed(CLASSES):
        bpy.utils.unregister_class(cls)
