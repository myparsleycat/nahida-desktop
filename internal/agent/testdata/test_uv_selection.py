"""Run with Python's standard library; no Blender or live mod files required."""

import pathlib
import runpy
import struct
import tempfile
import unittest
import zlib


helper = runpy.run_path(str(pathlib.Path(__file__).resolve().parents[1] /
    "skills/texture-render-diagnosis/references/uv_selection.py"))


class UVSelectionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        positions = [(0, 0, 0), (1, 0, 0), (0, 1, 0), (2, 0, 0), (3, 0, 0), (2, 1, 0)]
        uvs = [(.1, .1), (.3, .1), (.1, .3), (.7, .7), (.9, .7), (.7, .9)]
        (self.root / "p.buf").write_bytes(b"".join(struct.pack("<fff", *v) for v in positions))
        (self.root / "u.buf").write_bytes(b"".join(struct.pack("<ff", *v) for v in uvs))
        (self.root / "i.buf").write_bytes(struct.pack("<6H", *range(6)))
        self.spec = dict(position_path=str(self.root / "p.buf"), position_stride=12, position_offset=0,
                         uv_path=str(self.root / "u.buf"), uv_stride=8, uv_offset=0,
                         index_path=str(self.root / "i.buf"), index_size=2, first_index=0,
                         index_count=6, base_vertex=0)

    def test_selection_excludes_other_island_and_never_overwrites(self):
        evidence = helper["inspect_uv_mesh"](self.spec)
        self.assertEqual([c["id"] for c in evidence["components"]], [0, 3])
        target = self.root / "selection.png"
        result = helper["write_uv_selection"](self.spec, [0], evidence, 32, 32, str(target), 1)
        self.assertGreater(result["selectedPixels"], 0)
        data = target.read_bytes()
        offset, encoded = 8, b""
        while offset < len(data):
            size = struct.unpack_from(">I", data, offset)[0]
            if data[offset + 4:offset + 8] == b"IDAT":
                encoded += data[offset + 8:offset + 8 + size]
            offset += size + 12
        rows = zlib.decompress(encoded)
        self.assertEqual(rows[4 * 33 + 1 + 4], 255)
        self.assertEqual(rows[24 * 33 + 1 + 24], 0)
        with self.assertRaises(FileExistsError):
            helper["write_uv_selection"](self.spec, [0], evidence, 32, 32, str(target), 1)
        self.assertEqual(target.read_bytes(), data)

    def test_changed_mesh_and_shared_uvs_fail_before_writing(self):
        evidence = helper["inspect_uv_mesh"](self.spec)
        path = self.root / "u.buf"
        path.write_bytes(path.read_bytes()[:24] * 2)
        target = self.root / "selection.png"
        with self.assertRaisesRegex(ValueError, "changed"):
            helper["write_uv_selection"](self.spec, [0], evidence, 32, 32, str(target))
        evidence = helper["inspect_uv_mesh"](self.spec)
        with self.assertRaisesRegex(ValueError, "overlap"):
            helper["write_uv_selection"](self.spec, [0], evidence, 32, 32, str(target))
        self.assertFalse(target.exists())

    def test_invalid_index_and_nonfinite_uv(self):
        (self.root / "i.buf").write_bytes(struct.pack("<6H", 0, 1, 99, 3, 4, 5))
        with self.assertRaisesRegex(ValueError, "exceeds"):
            helper["inspect_uv_mesh"](self.spec)
        (self.root / "i.buf").write_bytes(struct.pack("<6H", *range(6)))
        path = self.root / "u.buf"
        path.write_bytes(struct.pack("<f", float("nan")) + path.read_bytes()[4:])
        with self.assertRaisesRegex(ValueError, "Non-finite"):
            helper["inspect_uv_mesh"](self.spec)


if __name__ == "__main__":
    unittest.main()
