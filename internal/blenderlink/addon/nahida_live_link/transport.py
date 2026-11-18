# SPDX-License-Identifier: GPL-3.0-or-later
"""Pipe client of the Blender live link.

A worker thread owns the pipe. Blender's main thread hands it scenes, meshes and image pixels with
send_scene, send_mesh and send_texture, which only queue: the newest scene, the newest mesh of each
object and the newest pixels of each image replace older ones that have not been sent yet. Scenes go
first, then errors, meshes and textures. The worker converts pixels to RGBA8, skips pixels equal to the
last ones the desktop kept for the image, and stamps each texture with the newest scene revision the
desktop kept. It talks to Nahida Desktop in strict request and reply order, because reads and writes on
a synchronous pipe handle are serialized, and pings every 500 ms when idle so that the server's flags
reach it.

on_event(kind, value) runs on the worker thread; the add-on must hand the value to the main thread
(for example through bpy.app.timers) before touching bpy. Kinds:

- "state": one of the STATE_* strings.
- "viewer_attached": bool.
- "resync": None; send the scene and every object again.
- "ack": (object_key, status, message) for each mesh; object_key is "" for the scene.
- "texture_ack": (image, status, message) for each texture.
"""

import hashlib
import json
import os
import threading

from . import pixels, protocol

DISCOVERY_PATH = os.path.join(os.path.expanduser("~"), ".nahida-desktop", "blender-link.json")
PING_INTERVAL = 0.5
BACKOFF = (0.5, 1.0, 2.0, 4.0, 5.0)

STATE_STOPPED = "stopped"
STATE_CONNECTING = "connecting"
STATE_CONNECTED = "connected"
STATE_INCOMPATIBLE = "incompatible"
STATE_REJECTED = "rejected"

_KEPT = (protocol.ACK_APPLIED, protocol.ACK_QUEUED, protocol.ACK_UNMATCHED)


def content_hash(data):
    return hashlib.blake2b(data, digest_size=protocol.HASH_SIZE).digest()


class Mesh:
    """One evaluated object. attrs maps attribute IDs to bytes-like little-endian data; an object
    without an attribute leaves it out."""

    __slots__ = ("object_key", "revision", "scene_rev", "vertex_count", "loop_count", "vertex_groups", "attrs")

    def __init__(self, object_key, revision, scene_rev, vertex_count, loop_count, vertex_groups, attrs):
        self.object_key = object_key
        self.revision = revision
        self.scene_rev = scene_rev
        self.vertex_count = vertex_count
        self.loop_count = loop_count
        self.vertex_groups = list(vertex_groups)
        self.attrs = attrs


class Texture:
    """The pixels of one image as textures.read_pixels returns them; values belongs to the transport."""

    __slots__ = ("image", "width", "height", "channels", "values", "encode_srgb")

    def __init__(self, image, width, height, channels, values, encode_srgb):
        self.image = image
        self.width = width
        self.height = height
        self.channels = channels
        self.values = values
        self.encode_srgb = encode_srgb


class LinkClosed(Exception):
    """The desktop closed the pipe or answered out of order."""


class Transport:
    def __init__(self, addon_version, blender_version, on_event=None, discovery_path=DISCOVERY_PATH):
        self._addon_version = addon_version
        self._blender_version = blender_version
        self._on_event = on_event or (lambda kind, value: None)
        self._discovery_path = discovery_path

        self._cond = threading.Condition()
        self._stop = False
        self._thread = None
        self._scene = None
        self._scene_pending = False
        self._meshes = {}
        self._textures = {}
        self._texture_busy = None
        self._errors = []

        self._pipe = None
        self._request_id = 0
        self._known = {}
        self._scene_keys = set()
        self._scene_rev = 0
        self._sent_pixels = {}
        self._texture_versions = {}
        self._state = STATE_STOPPED
        self._viewer_attached = False

    @property
    def state(self):
        return self._state

    @property
    def viewer_attached(self):
        return self._viewer_attached

    def start(self):
        with self._cond:
            if self._thread is not None:
                return
            self._stop = False
            self._thread = threading.Thread(target=self._run, name="nahida-live-link", daemon=True)
            self._thread.start()

    def stop(self, timeout=2.0):
        """Ask the worker to finish. It may stay in a pipe read until the desktop answers; the thread
        is a daemon, so it does not keep Blender from exiting."""
        with self._cond:
            self._stop = True
            thread, self._thread = self._thread, None
            self._cond.notify_all()
        if thread is not None:
            thread.join(timeout)

    def send_scene(self, scene):
        with self._cond:
            self._scene, self._scene_pending = scene, True
            self._cond.notify_all()

    def send_mesh(self, mesh):
        with self._cond:
            self._meshes[mesh.object_key] = mesh
            self._cond.notify_all()

    def send_texture(self, texture):
        with self._cond:
            self._textures[texture.image] = texture
            self._cond.notify_all()

    def texture_pending(self, image):
        """Reports whether pixels of the image are queued or still being converted or sent."""
        with self._cond:
            return image in self._textures or self._texture_busy == image

    def send_client_error(self, object_key, code, message):
        with self._cond:
            self._errors.append({"object_key": object_key, "code": code, "message": message})
            self._cond.notify_all()

    def _run(self):
        attempt = 0
        while not self._stopping():
            if self._state not in (STATE_INCOMPATIBLE, STATE_REJECTED):
                self._set_state(STATE_CONNECTING)
            try:
                self._connect()
                attempt = 0
                self._serve()
            except _HelloRefused as refused:
                self._set_state(refused.state)
            except (OSError, ValueError, EOFError, LinkClosed, protocol.ProtocolError):
                pass
            finally:
                self._close()
            with self._cond:
                if not self._stop:
                    self._cond.wait(BACKOFF[min(attempt, len(BACKOFF) - 1)])
            attempt += 1
        self._set_state(STATE_STOPPED)

    def _stopping(self):
        with self._cond:
            return self._stop

    def _connect(self):
        with open(self._discovery_path, encoding="utf-8") as f:
            discovery = json.load(f)
        if discovery.get("proto") != protocol.PROTOCOL_VERSION:
            raise _HelloRefused(STATE_INCOMPATIBLE)
        self._pipe = open(discovery["pipe"], "r+b", buffering=0)
        ack = self._request(protocol.HELLO, protocol.HELLO_ACK, {
            "protocol_version": protocol.PROTOCOL_VERSION,
            "addon_version": self._addon_version,
            "blender_version": self._blender_version,
            "pid": os.getpid(),
            "token": discovery["token"],
        })
        if ack["status"] == protocol.HELLO_VERSION_MISMATCH:
            raise _HelloRefused(STATE_INCOMPATIBLE)
        if ack["status"] != protocol.HELLO_OK:
            raise _HelloRefused(STATE_REJECTED)

        self._known, self._scene_keys, self._scene_rev = {}, set(), 0
        self._sent_pixels, self._texture_versions = {}, {}
        with self._cond:
            self._scene_pending = self._scene is not None
        self._set_state(STATE_CONNECTED)
        self._on_event("resync", None)

    def _close(self):
        pipe, self._pipe = self._pipe, None
        if pipe is not None:
            try:
                pipe.close()
            except OSError:
                pass
        if self._viewer_attached:
            self._viewer_attached = False
            self._on_event("viewer_attached", False)

    def _serve(self):
        while True:
            with self._cond:
                if not self._has_work():
                    self._cond.wait(PING_INTERVAL)
                if self._stop:
                    return
                scene = self._scene if self._scene_pending else None
                self._scene_pending = False
                error = self._errors.pop(0) if scene is None and self._errors else None
                mesh = texture = None
                if scene is None and error is None and self._meshes:
                    key = next(iter(self._meshes))
                    mesh = self._meshes.pop(key)
                elif scene is None and error is None and self._textures:
                    key = next(iter(self._textures))
                    texture = self._textures.pop(key)
                    self._texture_busy = texture.image

            if scene is not None:
                self._send_scene(scene)
            elif error is not None:
                self._flags(self._request(protocol.CLIENT_ERROR, protocol.ACK, error)["flags"])
            elif mesh is not None:
                self._send_mesh(mesh)
            elif texture is not None:
                try:
                    self._send_texture(texture)
                finally:
                    with self._cond:
                        self._texture_busy = None
            else:
                self._flags(self._request(protocol.PING, protocol.PONG, {})["flags"])

    def _has_work(self):
        return (
            self._stop or self._scene_pending or bool(self._errors) or bool(self._meshes) or bool(self._textures)
        )

    def _send_scene(self, scene):
        ack = self._request(protocol.SCENE, protocol.ACK, scene)
        if ack["status"] in _KEPT:
            self._scene_keys = {
                obj["object_key"]
                for component in scene["components"]
                for part in component["parts"]
                for obj in part["objects"]
            }
            self._known = {key: hashes for key, hashes in self._known.items() if key in self._scene_keys}
            self._scene_rev = scene["scene_rev"]
            # The desktop forgets the pixels of images a scene it keeps no longer describes as "pixels".
            streamed = _pixel_images(scene.get("settings"))
            self._sent_pixels = {name: digest for name, digest in self._sent_pixels.items() if name in streamed}
        self._on_event("ack", ("", ack["status"], ack["message"]))
        self._flags(ack["flags"])

    def _send_mesh(self, mesh, force_inline=()):
        known = {digest for hashes in self._known.values() for digest in hashes.values()}
        headers, inline, hashes = [], [], {}
        for attr_id in sorted(mesh.attrs):
            data = memoryview(mesh.attrs[attr_id]).cast("B")
            digest = content_hash(data)
            hashes[attr_id] = digest
            state = protocol.ATTR_INLINE
            if digest in known and attr_id not in force_inline:
                state = protocol.ATTR_UNCHANGED
            else:
                inline.append((attr_id, data))
            headers.append({"id": attr_id, "state": state, "byte_len": len(data), "hash": digest})

        request_id = self._next_request_id()
        self._write(protocol.encode_message(protocol.MESH_BEGIN, request_id, {
            "object_key": mesh.object_key,
            "revision": mesh.revision,
            "scene_rev": mesh.scene_rev,
            "vertex_count": mesh.vertex_count,
            "loop_count": mesh.loop_count,
            "vertex_groups": mesh.vertex_groups,
            "attrs": headers,
        }))
        for attr_id, data in inline:
            for offset in range(0, len(data), protocol.MAX_CHUNK_DATA):
                self._write(protocol.encode_message(protocol.MESH_CHUNK, request_id, {
                    "attr_id": attr_id,
                    "offset": offset,
                    "data": data[offset:offset + protocol.MAX_CHUNK_DATA],
                }))
        self._write(protocol.encode_message(protocol.MESH_COMMIT, request_id, {}))
        ack = self._reply(request_id, protocol.ACK)

        if ack["status"] == protocol.ACK_NEED_FULL and not force_inline:
            self._flags(ack["flags"])
            self._send_mesh(mesh, force_inline=set(ack["needed"]))
            return
        if ack["status"] in _KEPT:
            self._known[mesh.object_key] = hashes
        self._on_event("ack", (mesh.object_key, ack["status"], ack["message"]))
        self._flags(ack["flags"])

    def _send_texture(self, texture):
        data = pixels.to_rgba8(texture.values, texture.width, texture.height, texture.channels, texture.encode_srgb)
        digest = content_hash(data)
        if self._sent_pixels.get(texture.image) == digest:
            return
        version = self._texture_versions.get(texture.image, 0) + 1
        self._texture_versions[texture.image] = version

        request_id = self._next_request_id()
        self._write(protocol.encode_message(protocol.TEXTURE_BEGIN, request_id, {
            "image": texture.image,
            "version": version,
            "scene_rev": self._scene_rev,
            "width": texture.width,
            "height": texture.height,
            "format": protocol.PIXEL_RGBA8,
            "byte_len": len(data),
        }))
        view = memoryview(data)
        for offset in range(0, len(data), protocol.MAX_CHUNK_DATA):
            self._write(protocol.encode_message(protocol.TEXTURE_CHUNK, request_id, {
                "offset": offset,
                "data": view[offset:offset + protocol.MAX_CHUNK_DATA],
            }))
        self._write(protocol.encode_message(protocol.TEXTURE_COMMIT, request_id, {}))
        ack = self._reply(request_id, protocol.ACK)

        # The desktop keeps no pixels of an image it answers as unmatched.
        if ack["status"] in (protocol.ACK_APPLIED, protocol.ACK_QUEUED):
            self._sent_pixels[texture.image] = digest
        self._on_event("texture_ack", (texture.image, ack["status"], ack["message"]))
        self._flags(ack["flags"])

    def _flags(self, flags):
        attached = bool(flags & protocol.FLAG_VIEWER_ATTACHED)
        if attached != self._viewer_attached:
            self._viewer_attached = attached
            self._on_event("viewer_attached", attached)
        if flags & protocol.FLAG_RESYNC_ALL:
            self._sent_pixels = {}
            with self._cond:
                self._scene_pending = self._scene is not None
            self._on_event("resync", None)

    def _request(self, msg_type, reply_type, message):
        request_id = self._next_request_id()
        self._write(protocol.encode_message(msg_type, request_id, message))
        return self._reply(request_id, reply_type)

    def _next_request_id(self):
        self._request_id = self._request_id % 0xFFFFFFFF + 1
        return self._request_id

    def _reply(self, request_id, reply_type):
        msg_type, reply_id, payload = protocol.read_frame(self._read_exact)
        if reply_id != request_id or msg_type != reply_type:
            raise LinkClosed("reply %#04x to %d, want %#04x to %d" % (msg_type, reply_id, reply_type, request_id))
        return protocol.decode(msg_type, payload)

    def _write(self, data):
        view = memoryview(data)
        while view:
            written = self._pipe.write(view)
            if not written:
                raise LinkClosed("pipe write returned %r" % written)
            view = view[written:]

    def _read_exact(self, n):
        parts, remaining = [], n
        while remaining:
            part = self._pipe.read(remaining)
            if not part:
                raise LinkClosed("pipe closed")
            parts.append(part)
            remaining -= len(part)
        return b"".join(parts)

    def _set_state(self, state):
        if state != self._state:
            self._state = state
            self._on_event("state", state)


def _pixel_images(settings):
    """Returns the names of the images a Scene settings block describes as "pixels"."""
    if not settings:
        return set()
    objects = json.loads(bytes(settings).decode("utf-8")).get("objects", {})
    return {
        texture["name"]
        for obj in objects.values()
        for texture in obj.get("textures", ())
        if texture.get("source") == "pixels"
    }


class _HelloRefused(Exception):
    def __init__(self, state):
        super().__init__(state)
        self.state = state
