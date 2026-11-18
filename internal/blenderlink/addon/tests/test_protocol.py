"""Encodes and decodes internal/blenderlink/testdata/v1 with the add-on codec.

Run with: python -I internal/blenderlink/addon/tests/test_protocol.py
"""

import base64
import glob
import importlib.util
import json
import os
import re
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PACKAGE_DIR = os.path.join(HERE, os.pardir, "nahida_live_link")
LINK_DIR = os.path.join(HERE, os.pardir, os.pardir)
VECTOR_DIR = os.path.join(LINK_DIR, "testdata", "v1")


def load_protocol():
    # Loaded by path: importing the package would run its __init__, which needs bpy.
    spec = importlib.util.spec_from_file_location("nahida_protocol", os.path.join(PACKAGE_DIR, "protocol.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


protocol = load_protocol()

TYPES = {
    "Hello": protocol.HELLO,
    "HelloAck": protocol.HELLO_ACK,
    "Scene": protocol.SCENE,
    "MeshBegin": protocol.MESH_BEGIN,
    "MeshChunk": protocol.MESH_CHUNK,
    "MeshCommit": protocol.MESH_COMMIT,
    "Ack": protocol.ACK,
    "Ping": protocol.PING,
    "Pong": protocol.PONG,
    "ClientError": protocol.CLIENT_ERROR,
}


def from_json(message):
    """Converts the Go JSON form (base64 bytes, hex hashes) to the codec's dict form."""
    message = dict(message)
    for key in ("settings", "data"):
        if key in message:
            message[key] = base64.b64decode(message[key])
    if "attrs" in message:
        message["attrs"] = [dict(attr, hash=bytes.fromhex(attr["hash"])) for attr in message["attrs"]]
    return message


class Reader:
    def __init__(self, data):
        self.data = data
        self.pos = 0

    def read_exact(self, n):
        if self.pos + n > len(self.data):
            raise EOFError
        value = self.data[self.pos:self.pos + n]
        self.pos += n
        return value


class ProtocolVectorTest(unittest.TestCase):
    def test_vectors(self):
        descriptions = sorted(glob.glob(os.path.join(VECTOR_DIR, "*.json")))
        self.assertTrue(descriptions, "no vectors in %s" % VECTOR_DIR)
        for description in descriptions:
            name = os.path.splitext(os.path.basename(description))[0]
            with self.subTest(name):
                with open(description, encoding="utf-8") as f:
                    vector = json.load(f)
                with open(os.path.splitext(description)[0] + ".bin", "rb") as f:
                    golden = f.read()
                msg_type = TYPES[vector["type"]]
                want = from_json(vector["message"])

                self.assertEqual(protocol.encode_message(msg_type, vector["request_id"], want), golden)

                reader = Reader(golden)
                read_type, request_id, payload = protocol.read_frame(reader.read_exact)
                self.assertEqual(reader.pos, len(golden))
                self.assertEqual((read_type, request_id), (msg_type, vector["request_id"]))
                self.assertEqual(protocol.decode(read_type, payload), want)

    def test_protocol_version_matches_go(self):
        with open(os.path.join(LINK_DIR, "frame.go"), encoding="utf-8") as f:
            match = re.search(r"^\s*ProtocolVersion\s*=\s*(\d+)\s*$", f.read(), re.MULTILINE)
        self.assertIsNotNone(match)
        self.assertEqual(int(match.group(1)), protocol.PROTOCOL_VERSION)

    def test_rejects_malformed_payloads(self):
        cases = {
            "trailing bytes": (protocol.PING, b"\x00"),
            "short payload": (protocol.PONG, b"\x01"),
            "invalid UTF-8": (protocol.CLIENT_ERROR, b"\x01\x00\xff\x00\x00\x00\x00"),
            "count past payload": (protocol.ACK, b"\x00\x00\x00\xff\xff\x00\x00"),
            "bytes past payload": (protocol.MESH_CHUNK, b"\x01\x00\x00\x00\x00\x00\x09\x00\x00\x00\x01"),
        }
        for name, (msg_type, payload) in cases.items():
            with self.subTest(name):
                with self.assertRaises(protocol.ProtocolError):
                    protocol.decode(msg_type, payload)

    def test_rejects_invalid_headers(self):
        header = protocol.encode_frame(protocol.PING, 1, b"")
        for name, data in {
            "length below header": b"\x07" + header[1:],
            "reserved flags": header[:6] + b"\x01\x00" + header[8:],
        }.items():
            with self.subTest(name):
                with self.assertRaises(protocol.ProtocolError):
                    protocol.read_frame(Reader(data).read_exact)


if __name__ == "__main__":
    unittest.main()
