# SPDX-License-Identifier: GPL-3.0-or-later
"""Codec of the Blender live link wire format.

internal/blenderlink/PROTOCOL.md in the Nahida Desktop repository is the contract. Messages are
plain dicts whose keys match that document; byte fields are bytes and hashes are 16-byte bytes.
This module imports nothing from Blender so its tests run in a plain interpreter.
"""

import struct

PROTOCOL_VERSION = 1
MAX_FRAME_SIZE = 4 << 20
MAX_CHUNK_DATA = 1 << 20
HEADER_SIZE = 12
HASH_SIZE = 16

HELLO = 0x01
SCENE = 0x02
MESH_BEGIN = 0x10
MESH_CHUNK = 0x11
MESH_COMMIT = 0x12
TEXTURE_BEGIN = 0x13
TEXTURE_CHUNK = 0x14
TEXTURE_COMMIT = 0x15
PING = 0x20
CLIENT_ERROR = 0x30
HELLO_ACK = 0x81
ACK = 0x82
PONG = 0xA0

HELLO_OK = 0
HELLO_VERSION_MISMATCH = 1
HELLO_BAD_TOKEN = 2

ACK_APPLIED = 0
ACK_QUEUED = 1
ACK_UNMATCHED = 2
ACK_NEED_FULL = 3
ACK_REJECTED = 4

FLAG_RESYNC_ALL = 1 << 0
FLAG_VIEWER_ATTACHED = 1 << 1

PIXEL_RGBA8 = 1

ATTR_ABSENT = 0
ATTR_INLINE = 1
ATTR_UNCHANGED = 2

ATTR_VERT_CO = 0x0001
ATTR_LOOP_VERT = 0x0002
ATTR_LOOP_NORMAL = 0x0003
ATTR_LOOP_TANGENT = 0x0004
ATTR_LOOP_BTSIGN = 0x0005
ATTR_VG_COUNT = 0x0300
ATTR_VG_INDEX = 0x0301
ATTR_VG_WEIGHT = 0x0302
MAX_LAYERS = 0x100


def attr_uv(layer):
    if not 0 <= layer < MAX_LAYERS:
        raise ValueError("UV layer %d out of range" % layer)
    return 0x0100 + layer


def attr_color(layer):
    if not 0 <= layer < MAX_LAYERS:
        raise ValueError("color layer %d out of range" % layer)
    return 0x0200 + layer


class ProtocolError(Exception):
    """A frame or payload breaks the wire format."""


_HEADER = struct.Struct("<IHHI")


def encode_frame(msg_type, request_id, payload):
    if len(payload) > MAX_FRAME_SIZE - HEADER_SIZE:
        raise ProtocolError("payload of %d bytes" % len(payload))
    return _HEADER.pack(HEADER_SIZE - 4 + len(payload), msg_type, 0, request_id) + payload


def encode_message(msg_type, request_id, message):
    return encode_frame(msg_type, request_id, encode(msg_type, message))


def read_frame(read_exact):
    """Read one frame with read_exact(n), which returns exactly n bytes or raises.

    Returns (type, request_id, payload).
    """
    length, msg_type, flags, request_id = _HEADER.unpack(read_exact(HEADER_SIZE))
    if length < HEADER_SIZE - 4 or length > MAX_FRAME_SIZE - 4:
        raise ProtocolError("frame length %d" % length)
    if flags != 0:
        raise ProtocolError("frame flags %#x" % flags)
    payload = read_exact(length - (HEADER_SIZE - 4)) if length > HEADER_SIZE - 4 else b""
    return msg_type, request_id, payload


class _Writer:
    def __init__(self):
        self.parts = []

    def u8(self, value):
        self.parts.append(struct.pack("<B", value))

    def u16(self, value):
        self.parts.append(struct.pack("<H", value))

    def u32(self, value):
        self.parts.append(struct.pack("<I", value))

    def str(self, value):
        data = value.encode("utf-8")
        if len(data) > 0xFFFF:
            raise ProtocolError("string of %d bytes" % len(data))
        self.u16(len(data))
        self.parts.append(data)

    def bytes32(self, value):
        value = bytes(value)
        if len(value) > 0xFFFFFFFF:
            raise ProtocolError("%d bytes" % len(value))
        self.u32(len(value))
        self.parts.append(value)

    def raw(self, value, size):
        value = bytes(value)
        if len(value) != size:
            raise ProtocolError("%d bytes, want %d" % (len(value), size))
        self.parts.append(value)

    def count16(self, items):
        if len(items) > 0xFFFF:
            raise ProtocolError("%d elements" % len(items))
        self.u16(len(items))
        return items

    def strs(self, values):
        for value in self.count16(values):
            self.str(value)

    def payload(self):
        return b"".join(self.parts)


class _Reader:
    def __init__(self, payload):
        self.buf = memoryview(payload)
        self.pos = 0

    def take(self, n):
        if self.pos + n > len(self.buf):
            raise ProtocolError("payload ends early")
        value = self.buf[self.pos:self.pos + n]
        self.pos += n
        return value

    def u8(self):
        return self.take(1)[0]

    def u16(self):
        return struct.unpack("<H", self.take(2))[0]

    def u32(self):
        return struct.unpack("<I", self.take(4))[0]

    def str(self):
        data = self.take(self.u16())
        try:
            return str(data, "utf-8")
        except UnicodeDecodeError:
            raise ProtocolError("invalid UTF-8") from None

    def bytes32(self):
        return bytes(self.take(self.u32()))

    def count16(self, min_element_size):
        n = self.u16()
        if n * min_element_size > len(self.buf) - self.pos:
            raise ProtocolError("%d elements in %d bytes" % (n, len(self.buf) - self.pos))
        return n

    def strs(self):
        return [self.str() for _ in range(self.count16(2))]

    def finish(self):
        if self.pos != len(self.buf):
            raise ProtocolError("%d trailing bytes" % (len(self.buf) - self.pos))


def _enc_hello(w, m):
    w.u16(m["protocol_version"])
    w.str(m["addon_version"])
    w.str(m["blender_version"])
    w.u32(m["pid"])
    w.str(m["token"])


def _dec_hello(r):
    return {
        "protocol_version": r.u16(), "addon_version": r.str(), "blender_version": r.str(),
        "pid": r.u32(), "token": r.str(),
    }


def _enc_hello_ack(w, m):
    w.u8(m["status"])
    w.u16(m["protocol_version"])


def _dec_hello_ack(r):
    return {"status": r.u8(), "protocol_version": r.u16()}


def _enc_scene(w, m):
    w.u32(m["scene_rev"])
    w.str(m["exporter"])
    w.str(m["exporter_version"])
    w.str(m["game"])
    w.str(m["source_folder"])
    w.str(m["output_folder"])
    w.bytes32(m["settings"])
    for component in w.count16(m["components"]):
        w.str(component["name"])
        for part in w.count16(component["parts"]):
            w.str(part["name"])
            for obj in w.count16(part["objects"]):
                w.str(obj["object_key"])
                w.str(obj["name"])
                w.str(obj["collection"])
    w.strs(m["errors"])


def _dec_scene(r):
    scene = {
        "scene_rev": r.u32(), "exporter": r.str(), "exporter_version": r.str(), "game": r.str(),
        "source_folder": r.str(), "output_folder": r.str(), "settings": r.bytes32(),
    }
    components = []
    for _ in range(r.count16(4)):
        component = {"name": r.str(), "parts": []}
        for _ in range(r.count16(4)):
            part = {"name": r.str(), "objects": []}
            for _ in range(r.count16(6)):
                part["objects"].append({"object_key": r.str(), "name": r.str(), "collection": r.str()})
            component["parts"].append(part)
        components.append(component)
    scene["components"] = components
    scene["errors"] = r.strs()
    return scene


def _enc_mesh_begin(w, m):
    w.str(m["object_key"])
    w.u32(m["revision"])
    w.u32(m["scene_rev"])
    w.u32(m["vertex_count"])
    w.u32(m["loop_count"])
    w.strs(m["vertex_groups"])
    for attr in w.count16(m["attrs"]):
        w.u16(attr["id"])
        w.u8(attr["state"])
        w.u32(attr["byte_len"])
        w.raw(attr["hash"], HASH_SIZE)


def _dec_mesh_begin(r):
    begin = {
        "object_key": r.str(), "revision": r.u32(), "scene_rev": r.u32(), "vertex_count": r.u32(),
        "loop_count": r.u32(), "vertex_groups": r.strs(),
    }
    begin["attrs"] = [
        {"id": r.u16(), "state": r.u8(), "byte_len": r.u32(), "hash": bytes(r.take(HASH_SIZE))}
        for _ in range(r.count16(7 + HASH_SIZE))
    ]
    return begin


def _enc_mesh_chunk(w, m):
    w.u16(m["attr_id"])
    w.u32(m["offset"])
    w.bytes32(m["data"])


def _dec_mesh_chunk(r):
    return {"attr_id": r.u16(), "offset": r.u32(), "data": r.bytes32()}


def _enc_texture_begin(w, m):
    w.str(m["image"])
    w.u32(m["version"])
    w.u32(m["scene_rev"])
    w.u32(m["width"])
    w.u32(m["height"])
    w.u8(m["format"])
    w.u32(m["byte_len"])


def _dec_texture_begin(r):
    return {
        "image": r.str(), "version": r.u32(), "scene_rev": r.u32(), "width": r.u32(), "height": r.u32(),
        "format": r.u8(), "byte_len": r.u32(),
    }


def _enc_texture_chunk(w, m):
    w.u32(m["offset"])
    w.bytes32(m["data"])


def _dec_texture_chunk(r):
    return {"offset": r.u32(), "data": r.bytes32()}


def _enc_empty(w, m):
    pass


def _dec_empty(r):
    return {}


def _enc_ack(w, m):
    w.u8(m["status"])
    w.u16(m["flags"])
    for attr_id in w.count16(m["needed"]):
        w.u16(attr_id)
    w.str(m["message"])


def _dec_ack(r):
    status, flags = r.u8(), r.u16()
    needed = [r.u16() for _ in range(r.count16(2))]
    return {"status": status, "flags": flags, "needed": needed, "message": r.str()}


def _enc_pong(w, m):
    w.u16(m["flags"])


def _dec_pong(r):
    return {"flags": r.u16()}


def _enc_client_error(w, m):
    w.str(m["object_key"])
    w.str(m["code"])
    w.str(m["message"])


def _dec_client_error(r):
    return {"object_key": r.str(), "code": r.str(), "message": r.str()}


_CODECS = {
    HELLO: (_enc_hello, _dec_hello),
    SCENE: (_enc_scene, _dec_scene),
    MESH_BEGIN: (_enc_mesh_begin, _dec_mesh_begin),
    MESH_CHUNK: (_enc_mesh_chunk, _dec_mesh_chunk),
    MESH_COMMIT: (_enc_empty, _dec_empty),
    TEXTURE_BEGIN: (_enc_texture_begin, _dec_texture_begin),
    TEXTURE_CHUNK: (_enc_texture_chunk, _dec_texture_chunk),
    TEXTURE_COMMIT: (_enc_empty, _dec_empty),
    PING: (_enc_empty, _dec_empty),
    CLIENT_ERROR: (_enc_client_error, _dec_client_error),
    HELLO_ACK: (_enc_hello_ack, _dec_hello_ack),
    ACK: (_enc_ack, _dec_ack),
    PONG: (_enc_pong, _dec_pong),
}


def encode(msg_type, message):
    try:
        encoder = _CODECS[msg_type][0]
    except KeyError:
        raise ProtocolError("unknown message type %#04x" % msg_type) from None
    w = _Writer()
    try:
        encoder(w, message)
    except struct.error as exc:
        raise ProtocolError(str(exc)) from None
    return w.payload()


def decode(msg_type, payload):
    try:
        decoder = _CODECS[msg_type][1]
    except KeyError:
        raise ProtocolError("unknown message type %#04x" % msg_type) from None
    r = _Reader(payload)
    message = decoder(r)
    r.finish()
    return message
