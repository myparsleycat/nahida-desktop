# SPDX-License-Identifier: GPL-3.0-or-later
"""Change tracking: depsgraph updates mark objects dirty, and a timer extracts and sends them.

depsgraph_update_post only records which objects changed. The timer replans the scene at most every
REPLAN_INTERVAL after a change and at least every REPLAN_IDLE, which also catches texture files appearing on
disk. It extracts the latest state of each dirty object and hands it to the transport, which keeps only the
newest mesh per object. Weights are read when an object is in Weight Paint mode, when its topology or vertex
group names change, on (re)connection or RESYNC_ALL, and once an object has been idle for WEIGHT_RESYNC after
an update that skipped them. Sculpt strokes on an object without viewport modifiers change neither the evaluated
mesh nor its geometry flag, so each tick compares such an object's original positions instead.

The pixels of images the scene describes as "pixels" are read while a viewer is attached: after a depsgraph
update of the image or a change of its textures.signature, on (re)connection or RESYNC_ALL, and, for the images
being painted on, during an image paint stroke and once after it ends (before Blender 4.2, which cannot detect
strokes, every LEGACY_PAINT_INTERVAL in Texture Paint mode). Edited images are also read after an undo or redo,
and one of them every DIRTY_RECHECK, since other edits may give no signal. Reads of one image are at least
TEXTURE_INTERVAL apart, at least TEXTURE_COST_FACTOR times the duration of its last read, and wait until the
transport has sent its previous pixels, so painting a large image keeps Blender responsive. An image the desktop
answers as unmatched fills no viewer slot and is not read again until the next scene. A "file" image that gets
unsaved edits marks the plan dirty, so painting on a saved image streams after REPLAN_INTERVAL.
"""

import collections
import time

import bpy

from . import extract, plan_xxmi, protocol, textures, transport

TICK = 0.033
REPLAN_INTERVAL = 0.25
REPLAN_IDLE = 1.0
WEIGHT_RESYNC = 2.0
TEXTURE_INTERVAL = 0.2
TEXTURE_COST_FACTOR = 4.0
LEGACY_PAINT_INTERVAL = 1.0
DIRTY_RECHECK = 2.0
PAINT_OPERATORS = {"PAINT_OT_image_paint"}

STATUS_NAMES = {0: "applied", 1: "queued", 2: "unmatched", 3: "need full", 4: "rejected"}


class ObjectState:
    __slots__ = ("extracted", "revision", "last_extract", "weights_stale", "status", "message", "errors",
                 "sculpt_positions")

    def __init__(self):
        self.extracted = None
        self.revision = 0
        self.last_extract = 0.0
        self.weights_stale = False
        self.status = ""
        self.message = ""
        self.errors = set()
        self.sculpt_positions = None


class ImageState:
    __slots__ = ("signature", "pending", "unused", "last_read", "interval", "status", "message")

    def __init__(self):
        self.signature = None
        self.pending = True
        self.unused = False
        self.last_read = 0.0
        self.interval = TEXTURE_INTERVAL
        self.status = ""
        self.message = ""


class TextureSync:
    """Decides when to read the pixels of the images the plan describes as "pixels"."""

    def __init__(self):
        self.images = {}
        self.stroking = False
        self.targets = set()
        self.last_recheck = 0.0

    def mark(self, name):
        state = self.images.get(name)
        if state is not None:
            state.pending = True

    def mark_all(self):
        for state in self.images.values():
            state.pending = True

    def mark_dirty(self):
        for name, state in self.images.items():
            image = bpy.data.images.get(name)
            if image is not None and image.is_dirty:
                state.pending = True

    def acknowledged(self, name, status, message):
        state = self.images.get(name)
        if state is not None:
            state.status, state.message = STATUS_NAMES.get(status, str(status)), message
            state.unused = status == protocol.ACK_UNMATCHED

    def scene_sent(self):
        """The desktop chooses the images it shows by the newest scene, so unused images are sent again."""
        for state in self.images.values():
            if state.unused:
                state.unused, state.pending = False, True

    def tick(self, now, plan, attached, link_transport):
        """Reads and sends the images that are due. Returns True when an image the plan describes as "file" has
        unsaved edits, so that the plan describes it as "pixels" again."""
        names, files = set(), set()
        for entries in plan.textures.values():
            for texture in entries:
                (names if texture["source"] == "pixels" else files).add(texture["name"])
        self.images = {name: state for name, state in self.images.items() if name in names}
        painting = _painting()
        legacy = painting is None
        stroking = bool(painting)
        ended, self.stroking = self.stroking and not stroking, stroking
        if stroking or (legacy and bpy.context.mode == "PAINT_TEXTURE"):
            self.targets = _paint_targets()
        elif legacy:
            self.targets = set()

        images = []
        for name in names:
            state = self.images.setdefault(name, ImageState())
            image = bpy.data.images.get(name)
            if image is None:
                continue
            current = textures.signature(image)
            if current != state.signature:
                state.signature, state.pending = current, True
            if image.is_dirty and name in self.targets:
                if stroking or ended:
                    state.pending = True
                elif legacy and now - state.last_read >= LEGACY_PAINT_INTERVAL:
                    state.pending = True
            images.append((name, image, state))

        # Edits other than paint strokes may give no signal, so one edited image is read again now and then.
        if attached and now - self.last_recheck >= DIRTY_RECHECK:
            stale = [
                (state.last_read, name, state)
                for name, image, state in images
                if image.is_dirty and not state.pending and not state.unused
                and now - state.last_read >= DIRTY_RECHECK
            ]
            if stale:
                min(stale, key=lambda item: item[:2])[2].pending = True
                self.last_recheck = now

        for name, image, state in images:
            # A read while the previous pixels wait for the worker would only replace them.
            if (
                not attached
                or state.unused
                or not state.pending
                or now - state.last_read < state.interval
                or link_transport.texture_pending(name)
            ):
                continue
            started = time.perf_counter()
            read = textures.read_pixels(image)
            cost = time.perf_counter() - started
            state.pending, state.last_read = False, now
            state.interval = max(TEXTURE_INTERVAL, TEXTURE_COST_FACTOR * cost)
            if read is not None:
                link_transport.send_texture(transport.Texture(name, *read))
        return any(image is not None and image.is_dirty for image in map(bpy.data.images.get, files))


def _painting():
    """Reports whether an image paint stroke is running, or None before Blender 4.2, which cannot list modal
    operators."""
    for window in bpy.context.window_manager.windows:
        operators = getattr(window, "modal_operators", None)
        if operators is None:
            return None
        if any(operator.bl_idname in PAINT_OPERATORS for operator in operators):
            return True
    return False


def _paint_targets():
    """Returns the names of the images image paint draws on: the 3D viewport's canvas and the image of each
    Image Editor in Paint mode."""
    names = set()
    context = bpy.context
    paint = context.scene.tool_settings.image_paint
    if paint.mode == "IMAGE":
        if paint.canvas is not None:
            names.add(paint.canvas.name)
    else:
        obj = context.active_object
        material = obj.active_material if obj is not None else None
        if material is not None:
            images, slot = material.texture_paint_images, material.paint_active_slot
            if 0 <= slot < len(images) and images[slot] is not None:
                names.add(images[slot].name)
    for window in context.window_manager.windows:
        for area in window.screen.areas:
            if area.type != "IMAGE_EDITOR":
                continue
            space = area.spaces.active
            if getattr(space, "mode", "") == "PAINT" and space.image is not None:
                names.add(space.image.name)
    return names


class _Events(collections.deque):
    """Transport events, appended on the worker thread and drained by the timer."""

    def append_event(self, kind, value):
        self.append((kind, value))


class Link:
    def __init__(self):
        self.transport = None
        self.events = _Events()
        self.scene_rev = 0
        self.state = transport.STATE_STOPPED
        self.viewer_attached = False
        self.scene_status = ""
        self.scene_message = ""
        self.reset()

    def reset(self):
        self.plan = None
        self.plan_dirty = True
        self.last_plan = 0.0
        self.objects = {}
        self.dirty = set()
        self.moved = set()
        self.force_all = True
        self.sent_scene = None
        self.textures = TextureSync()

    def start(self, addon_version):
        if self.transport is not None:
            return
        blender_version = ".".join(str(part) for part in bpy.app.version)
        self.transport = transport.Transport(addon_version, blender_version, self.events.append_event)
        self.transport.start()

    def stop(self):
        link_transport, self.transport = self.transport, None
        if link_transport is not None:
            link_transport.stop()
        self.state = transport.STATE_STOPPED
        self.viewer_attached = False

    def drain_events(self):
        while self.events:
            kind, value = self.events.popleft()
            if kind == "state":
                self.state = value
            elif kind == "viewer_attached":
                self.viewer_attached = value
            elif kind == "resync":
                self.force_all = True
                self.sent_scene = None
                self.textures.mark_all()
            elif kind == "texture_ack":
                self.textures.acknowledged(*value)
            elif kind == "ack":
                key, status, message = value
                if key == "":
                    self.scene_status, self.scene_message = STATUS_NAMES.get(status, str(status)), message
                elif key in self.objects:
                    state = self.objects[key]
                    state.status, state.message = STATUS_NAMES.get(status, str(status)), message

    def tick(self):
        self.drain_events()
        if self.transport is None or self.state != transport.STATE_CONNECTED:
            return
        now = time.monotonic()
        since = now - self.last_plan
        if (
            self.plan is None
            or self.force_all
            or since >= REPLAN_IDLE
            or (self.plan_dirty and since >= REPLAN_INTERVAL)
        ):
            listed = frozenset(entry.key for entry in self.plan.entries) if self.plan is not None else frozenset()
            self.plan = plan_xxmi.build(bpy.context.scene, listed)
            self.plan_dirty, self.last_plan = False, now
            keys = {entry.key for entry in self.plan.entries}
            self.objects = {key: state for key, state in self.objects.items() if key in keys}

        depsgraph = bpy.context.evaluated_depsgraph_get()
        force_all, self.force_all = self.force_all, False
        dirty, self.dirty = self.dirty, set()
        moved, self.moved = self.moved, set()
        changed = []
        for entry in self.plan.entries:
            state = self.objects.setdefault(entry.key, ObjectState())
            updated = (
                force_all
                or state.extracted is None
                or plan_xxmi.object_key(entry.obj) in dirty
                or (entry.main != entry.obj and plan_xxmi.object_key(entry.main) in moved)
            )
            # Sculpt strokes reach neither depsgraph_update_post nor the evaluated mesh; compare the positions.
            if extract.sculpts_original(entry.obj):
                positions = extract.original_positions(entry.obj)
                updated = updated or positions != state.sculpt_positions
                state.sculpt_positions = positions
            else:
                state.sculpt_positions = None
            idle_resync = state.weights_stale and now - state.last_extract >= WEIGHT_RESYNC
            if not updated and not idle_resync:
                continue
            force_weights = force_all or idle_resync or entry.obj.mode == "WEIGHT_PAINT"
            try:
                extracted = extract.extract(
                    depsgraph, entry, self.plan.apply_modifiers, state.extracted, force_weights
                )
            except Exception as exc:  # noqa: BLE001 - one broken object must not stop the others
                self._client_error(entry.key, state, "EXTRACT_FAILED", "%s: %s" % (entry.obj.name, exc))
                continue
            for code, message in extracted.errors:
                self._client_error(entry.key, state, code, message)
            previous = state.extracted
            state.extracted, state.last_extract = extracted, now
            state.weights_stale = not extracted.weights_read
            if force_all or not _same(previous, extracted):
                changed.append(entry.key)

        layers = {
            key: {"uv": state.extracted.uv_names, "color": state.extracted.color_names}
            for key, state in self.objects.items()
            if state.extracted is not None
        }
        scene = plan_xxmi.scene_message(self.plan, 0, layers)
        if scene != self.sent_scene:
            self.sent_scene = scene
            self.scene_rev += 1
            self.transport.send_scene(dict(scene, scene_rev=self.scene_rev))
            self.textures.scene_sent()
            changed = [entry.key for entry in self.plan.entries if self.objects[entry.key].extracted is not None]
        for key in changed:
            state = self.objects[key]
            state.revision += 1
            ex = state.extracted
            self.transport.send_mesh(transport.Mesh(
                key, state.revision, self.scene_rev, ex.vertex_count, ex.loop_count, ex.vertex_groups, ex.attrs
            ))
        if self.textures.tick(now, self.plan, self.viewer_attached, self.transport):
            self.plan_dirty = True

    def _client_error(self, key, state, code, message):
        if (code, message) in state.errors:
            return
        state.errors.add((code, message))
        state.status, state.message = "error", message
        self.transport.send_client_error(key, code, message)

    def mark(self, depsgraph):
        for update in depsgraph.updates:
            datablock = update.id
            if isinstance(datablock, bpy.types.Object):
                key = plan_xxmi.object_key(datablock.original)
                self.dirty.add(key)
                if update.is_updated_transform:
                    self.moved.add(key)
                self.plan_dirty = True
            elif isinstance(datablock, bpy.types.Image):
                self.textures.mark(datablock.name)
                self.plan_dirty = True
            elif isinstance(datablock, (bpy.types.Collection, bpy.types.Scene, bpy.types.Material,
                                        bpy.types.NodeTree)):
                self.plan_dirty = True

    def mark_all(self):
        self.plan_dirty = True
        if self.plan is not None:
            for entry in self.plan.entries:
                self.dirty.add(plan_xxmi.object_key(entry.obj))


def _same(a, b):
    if a is None:
        return False
    return (
        a.vertex_count == b.vertex_count
        and a.loop_count == b.loop_count
        and a.vertex_groups == b.vertex_groups
        and a.attrs == b.attrs
    )


link = Link()


def _tick():
    try:
        link.tick()
    except Exception as exc:  # noqa: BLE001 - keep the timer alive and show the failure in the panel
        link.scene_status, link.scene_message = "error", str(exc)
    _redraw()
    return TICK


_last_redraw = [None]


def _redraw():
    snapshot = (link.state, link.viewer_attached, link.scene_status, link.scene_message,
                tuple((key, s.status, s.message) for key, s in link.objects.items()),
                tuple((name, s.status, s.message) for name, s in link.textures.images.items()))
    if snapshot == _last_redraw[0]:
        return
    _last_redraw[0] = snapshot
    for window in getattr(bpy.context.window_manager, "windows", ()):
        for area in window.screen.areas:
            if area.type == "VIEW_3D":
                area.tag_redraw()


@bpy.app.handlers.persistent
def _on_depsgraph(scene, depsgraph):
    link.mark(depsgraph)


@bpy.app.handlers.persistent
def _on_frame(scene, depsgraph=None):
    link.mark_all()


@bpy.app.handlers.persistent
def _on_undo(*args):
    link.mark_all()
    link.textures.mark_dirty()


@bpy.app.handlers.persistent
def _on_load(*args):
    link.reset()


_HANDLERS = (
    (bpy.app.handlers.depsgraph_update_post, _on_depsgraph),
    (bpy.app.handlers.frame_change_post, _on_frame),
    (bpy.app.handlers.undo_post, _on_undo),
    (bpy.app.handlers.redo_post, _on_undo),
    (bpy.app.handlers.load_post, _on_load),
)


def register(addon_version):
    for handlers, handler in _HANDLERS:
        if handler not in handlers:
            handlers.append(handler)
    if not bpy.app.timers.is_registered(_tick):
        bpy.app.timers.register(_tick, first_interval=TICK, persistent=True)
    # The desktop app enables the add-on from a background Blender, which must not connect.
    if not bpy.app.background:
        link.start(addon_version)


def unregister():
    link.stop()
    if bpy.app.timers.is_registered(_tick):
        bpy.app.timers.unregister(_tick)
    for handlers, handler in _HANDLERS:
        if handler in handlers:
            handlers.remove(handler)
