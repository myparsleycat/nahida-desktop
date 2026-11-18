"""Checks the add-on's XXMI planner and extractor against the XXMI-Tools parity fixtures.

Run with a Blender that has XXMI-Tools installed as the add-on module `XXMITools`:

    blender -b --factory-startup --python internal/blenderlink/addon/tests/test_extract_xxmi.py

It rebuilds the gimi, srmi and zzmi scenes of internal/tools/model_viewer/testdata/blender_live/xxmi/
make_fixture.py, plans and extracts them with the add-on, sends them through the transport to an in-memory
server, and requires the decoded Scene, settings and attributes to equal capture.json (and edited.json for the
edited gimi scene) exactly. A last case plans the gimi scene without a dump folder or game, with material
textures on one object, and again after one object loses every face.
"""

import json
import os
import sys
import tempfile
import traceback
from pathlib import Path

import addon_utils
import bmesh
import bpy
import numpy

HERE = Path(__file__).resolve().parent
ADDON_DIR = HERE.parent
REPO = HERE.parents[3]
FIXTURES = REPO / "internal" / "tools" / "model_viewer" / "testdata" / "blender_live" / "xxmi"

sys.path.insert(0, str(ADDON_DIR))
from nahida_live_link import extract, plan_xxmi, protocol, textures, transport  # noqa: E402


def load_fixture_builder():
    source = (FIXTURES / "make_fixture.py").read_text(encoding="utf-8")
    source = source[: source.rindex("\nmain()")]
    namespace = {"__name__": "make_fixture"}
    exec(compile(source, str(FIXTURES / "make_fixture.py"), "exec"), namespace)
    return namespace


class FakeServer:
    """A pipe that decodes the client's frames and answers each request with Ack APPLIED."""

    def __init__(self):
        self.buffer = bytearray()
        self.replies = bytearray()
        self.scene = None
        self.objects = {}
        self.begin = None
        self.chunks = {}
        self.store = {}
        self.texture = None
        self.textures = {}

    def write(self, data):
        self.buffer += bytes(data)
        while len(self.buffer) >= 4:
            length = int.from_bytes(self.buffer[:4], "little")
            if len(self.buffer) < length + 4:
                break
            frame, self.buffer = bytes(self.buffer[: length + 4]), self.buffer[length + 4 :]
            self.handle(frame)
        return len(data)

    def read(self, n):
        part, self.replies = bytes(self.replies[:n]), self.replies[n:]
        return part

    def handle(self, frame):
        msg_type, request_id, payload = protocol.read_frame(Chunks(frame).read_exact)
        message = protocol.decode(msg_type, payload)
        if msg_type == protocol.SCENE:
            self.scene = message
            self.ack(request_id)
        elif msg_type == protocol.MESH_BEGIN:
            assert message["scene_rev"] == self.scene["scene_rev"]
            self.begin, self.chunks = message, {}
        elif msg_type == protocol.MESH_CHUNK:
            data = self.chunks.setdefault(message["attr_id"], bytearray())
            assert message["offset"] == len(data)
            data += message["data"]
        elif msg_type == protocol.MESH_COMMIT:
            begin, attrs = self.begin, {}
            for header in begin["attrs"]:
                if header["state"] == protocol.ATTR_INLINE:
                    attrs[header["id"]] = bytes(self.chunks.get(header["id"], b""))
                else:
                    attrs[header["id"]] = self.store[header["hash"]]
                assert len(attrs[header["id"]]) == header["byte_len"]
                assert transport.content_hash(attrs[header["id"]]) == header["hash"]
                self.store[header["hash"]] = attrs[header["id"]]
            self.objects[begin["object_key"]] = dict(begin, attrs=attrs)
            self.ack(request_id)
        elif msg_type == protocol.TEXTURE_BEGIN:
            assert message["scene_rev"] == self.scene["scene_rev"]
            self.texture = dict(message, data=bytearray())
        elif msg_type == protocol.TEXTURE_CHUNK:
            assert message["offset"] == len(self.texture["data"])
            self.texture["data"] += message["data"]
        elif msg_type == protocol.TEXTURE_COMMIT:
            assert len(self.texture["data"]) == self.texture["byte_len"]
            self.textures[self.texture["image"]] = self.texture
            self.ack(request_id)
        else:
            raise AssertionError("unexpected message %#x" % msg_type)

    def ack(self, request_id):
        self.replies += protocol.encode_message(protocol.ACK, request_id, {
            "status": protocol.ACK_APPLIED, "flags": 0, "needed": [], "message": "",
        })


class Chunks:
    def __init__(self, data):
        self.data, self.pos = data, 0

    def read_exact(self, n):
        part = self.data[self.pos : self.pos + n]
        self.pos += n
        return part


def configure_xxmi(scene, game, dump, normalize, outline):
    xxmi = scene.xxmi
    if game is not None:
        xxmi.game = game.name
    xxmi.dump_path = str(dump)
    xxmi.destination_path = ""
    xxmi.ignore_hidden = False
    xxmi.only_selected = False
    xxmi.apply_modifiers_and_shapekeys = True
    xxmi.normalize_weights = normalize
    xxmi.outline_optimization = outline["enabled"]
    xxmi.outline_rounding_precision = outline["precision"]
    xxmi.outline_gate_divergence = outline["gateDivergence"]
    xxmi.outline_custom_normals = outline["customNormals"]


def stream(link, server, plan, previous, force_weights):
    """Extracts every planned object and sends the scene and meshes like handlers.Link.tick."""
    depsgraph = bpy.context.evaluated_depsgraph_get()
    extracted = {}
    for entry in plan.entries:
        extracted[entry.key] = extract.extract(
            depsgraph, entry, plan.apply_modifiers, previous.get(entry.key), force_weights
        )
        assert not extracted[entry.key].errors, extracted[entry.key].errors
    layers = {key: {"uv": ex.uv_names, "color": ex.color_names} for key, ex in extracted.items()}
    scene_rev = (server.scene or {"scene_rev": 0})["scene_rev"] + 1
    link._send_scene(plan_xxmi.scene_message(plan, scene_rev, layers))
    for revision, (key, ex) in enumerate(extracted.items(), 1):
        link._send_mesh(transport.Mesh(
            key, revision, scene_rev, ex.vertex_count, ex.loop_count, ex.vertex_groups, ex.attrs
        ))
    return extracted


def floats(data):
    return numpy.frombuffer(data, dtype=numpy.float32)


def expect_equal(name, got, want, dtype):
    want = numpy.asarray(want, dtype=numpy.float64)
    got = numpy.asarray(got)
    if dtype == numpy.float32:
        exact = want.astype(numpy.float32)
        assert numpy.array_equal(exact.astype(numpy.float64), want), name + ": capture value is not a float32"
        want = exact
    else:
        want = want.astype(dtype)
    if got.shape != want.shape or not numpy.array_equal(got.view(numpy.uint8), want.view(numpy.uint8)):
        raise AssertionError("%s: %s values differ from the capture (%s vs %s)" % (name, got.dtype, got.shape,
                                                                                  want.shape))


def check(server, capture):
    scene = server.scene
    settings = json.loads(scene["settings"].decode("utf-8"))
    assert scene["exporter"] == "xxmi"
    assert scene["exporter_version"] == ".".join(str(v) for v in capture["xxmiVersion"])
    assert scene["game"] == plan_xxmi.GAMES[capture["game"]]
    assert scene["errors"] == [], scene["errors"]
    assert settings["game"] == capture["game"]
    assert len(settings["components"]) == len(capture["components"]) == len(scene["components"])

    count = 0
    for component, setting, sent in zip(capture["components"], settings["components"], scene["components"]):
        want = {key: value for key, value in component.items() if key != "parts"}
        assert setting == want, (setting, want)
        assert sent["name"] == component["fullName"]
        assert [part["name"] for part in sent["parts"]] == [part["fullName"] for part in component["parts"]]
        for part, sent_part in zip(component["parts"], sent["parts"]):
            assert [o["name"] for o in sent_part["objects"]] == [o["name"] for o in part["objects"]]
            for want_obj, sent_obj in zip(part["objects"], sent_part["objects"]):
                key = sent_obj["object_key"]
                check_object(want_obj, server.objects[key], settings["objects"][key])
                assert settings["objects"][key]["textures"] == [], key
                count += 1
    assert len(server.objects) == count
    return count


def check_object(want, got, info, geometry=True):
    """geometry=False skips positions, normals and tangents, which depend on the object space."""
    name = want["name"]
    attrs = got["attrs"]
    assert info["polygons"] == want["polygons"], name
    assert got["vertex_groups"] == want["groupNames"], name
    loops = len(want["loopVertices"])
    assert got["loop_count"] == loops and got["vertex_count"] == len(want["positions"]) // 3, name

    if geometry:
        expect_equal(name + " positions", floats(attrs[protocol.ATTR_VERT_CO]), want["positions"], numpy.float32)
    expect_equal(name + " loops", numpy.frombuffer(attrs[protocol.ATTR_LOOP_VERT], dtype=numpy.uint32),
                 want["loopVertices"], numpy.uint32)
    if loops == 0:
        # capture.json lists vertex.groups of the vertices of a mesh without faces; the add-on sends such an
        # object with no vertices because XXMI-Tools exports nothing from it.
        assert set(attrs) == {protocol.ATTR_VERT_CO, protocol.ATTR_LOOP_VERT}, name
        assert info["uv"] == list(want["uvs"]) and info["color"] == list(want["colors"]), name
        return
    for attr_id, key in (
        (protocol.ATTR_LOOP_NORMAL, "loopNormals"),
        (protocol.ATTR_LOOP_TANGENT, "loopTangents"),
        (protocol.ATTR_LOOP_BTSIGN, "loopBitangentSigns"),
    ):
        if not geometry and key != "loopBitangentSigns":
            continue
        expect_equal("%s %s" % (name, key), floats(attrs[attr_id]), want[key], numpy.float32)
    assert info["uv"] == list(want["uvs"]), (name, info["uv"])
    assert info["color"] == list(want["colors"]), (name, info["color"])
    for k, layer in enumerate(info["uv"]):
        expect_equal("%s uv %s" % (name, layer), floats(attrs[protocol.attr_uv(k)]), want["uvs"][layer],
                     numpy.float32)
    for k, layer in enumerate(info["color"]):
        expect_equal("%s color %s" % (name, layer), floats(attrs[protocol.attr_color(k)]), want["colors"][layer],
                     numpy.float32)
    expect_equal(name + " group counts", numpy.frombuffer(attrs[protocol.ATTR_VG_COUNT], dtype=numpy.uint8),
                 want["groupCounts"], numpy.uint8)
    expect_equal(name + " group indices", numpy.frombuffer(attrs[protocol.ATTR_VG_INDEX], dtype=numpy.uint16),
                 want["groupIndices"], numpy.uint16)
    expect_equal(name + " group weights", floats(attrs[protocol.ATTR_VG_WEIGHT]), want["groupWeights"],
                 numpy.float32)


def new_link(server):
    link = transport.Transport("test", "test")
    link._pipe = server
    return link


def run_case(fixture, case, scratch):
    game, parts, normalize, outline, dump = fixture["build_case"](case, scratch)
    addon_utils.enable("XXMITools", default_set=True)
    scene = bpy.context.scene
    configure_xxmi(scene, game, dump, normalize, outline)

    plan = plan_xxmi.build(scene)
    assert plan.message["errors"] == [], plan.message["errors"]
    assert plan.message["source_folder"] == str(dump)
    assert plan.message["output_folder"] == str(dump.parent / (dump.name + "Mod"))
    server = FakeServer()
    link = new_link(server)
    extracted = stream(link, server, plan, {}, True)
    capture = json.loads((FIXTURES / case / "capture.json").read_text(encoding="utf-8"))
    count = check(server, capture)
    print("%s: %d objects match capture.json" % (case, count))

    # A positions-only resend reuses the weights it does not read; the transport sends them as UNCHANGED.
    again = stream(link, server, plan, extracted, False)
    assert all(not ex.weights_read for ex in again.values() if ex.loop_count), "unchanged topology re-read weights"
    check(server, capture)

    if case == "gimi":
        fixture["edit_scene"]()
        plan = plan_xxmi.build(scene)
        edited = stream(link, server, plan, again, False)
        main = next(entry.key for entry in plan.entries if entry.obj.name == "FixBodyA")
        assert edited[main].weights_read, "a topology change must re-read weights"
        count = check(server, json.loads((FIXTURES / case / "edited.json").read_text(encoding="utf-8")))
        print("%s edited: %d objects match edited.json" % (case, count))


def add_textures(obj, scratch):
    """Gives obj a material with an external diffuse image, a packed normal map whose file is gone, and an
    unlinked light map whose viewer slot is set. Returns the expected textures entries and the packed image."""
    def image_file(name):
        path = scratch / (name + ".png")
        generated = bpy.data.images.new(name + "_source", 4, 4)
        generated.pixels = [len(name) / 32.0] * 64
        generated.save_render(str(path))
        bpy.data.images.remove(generated)
        return bpy.data.images.load(str(path)), path

    diffuse, diffuse_path = image_file("FixBodyADiffuse")
    normal, normal_path = image_file("FixBodyANormalMap")
    normal.pack()
    os.remove(normal_path)
    light, light_path = image_file("FixBodyALightMap")
    setattr(light, textures.ROLE_PROPERTY, "light_map")

    material = bpy.data.materials.new("FixBodyA")
    if bpy.app.version < (5, 0, 0):
        material.use_nodes = True
    nodes, links = material.node_tree.nodes, material.node_tree.links
    bsdf = next(node for node in nodes if node.type == "BSDF_PRINCIPLED")
    for image in (diffuse, normal, light):
        node = nodes.new("ShaderNodeTexImage")
        node.image = image
        if image == diffuse:
            links.new(node.outputs["Color"], bsdf.inputs["Base Color"])
        elif image == normal:
            normal_map = nodes.new("ShaderNodeNormalMap")
            links.new(node.outputs["Color"], normal_map.inputs["Color"])
            links.new(normal_map.outputs["Normal"], bsdf.inputs["Normal"])
    obj.data.materials.append(material)

    def file_entry(image, path, role):
        stat = os.stat(path)
        return {"name": image.name, "role": role, "source": "file", "path": os.path.normpath(str(path)),
                "version": "%d:%d" % (stat.st_mtime_ns, stat.st_size)}

    return [
        file_entry(diffuse, diffuse_path, "diffuse"),
        {"name": normal.name, "role": "normal_map", "source": "pixels", "path": "", "version": ""},
        file_entry(light, light_path, "light_map"),
    ], normal


def run_fallback(fixture, scratch):
    game, parts, normalize, outline, dump = fixture["build_case"]("gimi", scratch)
    addon_utils.enable("XXMITools", default_set=True)
    scene = bpy.context.scene
    configure_xxmi(scene, None, "", normalize, outline)
    assert scene.xxmi.game == ""
    textures.register()
    want_textures, packed = add_textures(bpy.data.objects["FixBodyA"], scratch)

    plan = plan_xxmi.build(scene)
    assert plan.message["errors"] == [], plan.message["errors"]
    assert plan.message["game"] == "" and plan.message["source_folder"] == "" and plan.message["output_folder"] == ""
    server = FakeServer()
    stream(new_link(server), server, plan, {}, True)
    scene_msg = server.scene
    settings = json.loads(scene_msg["settings"].decode("utf-8"))
    assert settings["game"] == ""

    # LooseA2 has no faces, so only three objects remain, each its own component and part.
    names = ["ExtraA1", "FixBodyA", "FixBodyB"]
    assert [c["name"] for c in scene_msg["components"]] == names
    capture = json.loads((FIXTURES / "gimi" / "capture.json").read_text(encoding="utf-8"))
    want_objects = {o["name"]: o for part in capture["components"][0]["parts"] for o in part["objects"]}
    captured = {key: value for key, value in capture["components"][0].items() if key != "parts"}
    for name, component, setting in zip(names, scene_msg["components"], settings["components"]):
        assert component["parts"] == [{"name": name, "objects": component["parts"][0]["objects"]}]
        (sent,) = component["parts"][0]["objects"]
        assert sent["name"] == name and sent["collection"] == ""
        info = settings["objects"][sent["object_key"]]
        if name == "ExtraA1":
            want = dict(captured, fullName=name, layout=[], ibFormat="", flipV={})
            check_object(want_objects[name], server.objects[sent["object_key"]], info, geometry=False)
        else:
            want = dict(captured, fullName=name)
            check_object(want_objects[name], server.objects[sent["object_key"]], info)
        assert setting == want, (setting, want)
        assert info["textures"] == (want_textures if name == "FixBodyA" else []), info["textures"]

    # The packed normal map goes as pixels, as the bytes Blender holds with the top row first. Each row gets its
    # own value so that the order shows.
    packed.pixels = [row / 4 for row in range(4) for _ in range(16)]
    stored = numpy.round(numpy.array(packed.pixels[:]) * 255).astype(numpy.uint8).reshape(4, 16)
    link = new_link(server)
    link._scene_rev = scene_msg["scene_rev"]
    link._send_texture(transport.Texture(packed.name, *textures.read_pixels(packed)))
    sent = server.textures[packed.name]
    assert (sent["width"], sent["height"], sent["format"], sent["version"]) == (4, 4, protocol.PIXEL_RGBA8, 1), sent
    assert bytes(sent["data"]) == stored[::-1].tobytes(), bytes(sent["data"])
    assert len(set(stored[:, 0])) == 4, stored
    # Equal pixels are not sent again.
    link._send_texture(transport.Texture(packed.name, *textures.read_pixels(packed)))
    assert server.textures[packed.name] is sent
    # The desktop forgets the pixels once a scene no longer lists the image, so the same pixels go again after it.
    bare = json.loads(scene_msg["settings"].decode("utf-8"))
    for info in bare["objects"].values():
        info["textures"] = []
    link._send_scene(dict(scene_msg, settings=json.dumps(bare).encode("utf-8")))
    link._send_scene(scene_msg)
    link._send_texture(transport.Texture(packed.name, *textures.read_pixels(packed)))
    assert server.textures[packed.name]["version"] == 2, server.textures[packed.name]

    # An object that loses every face stays planned while the previous plan listed it, and only then.
    extra = bpy.data.objects["ExtraA1"]
    bm = bmesh.new()
    bm.from_mesh(extra.data)
    bmesh.ops.delete(bm, geom=bm.faces[:], context="FACES_ONLY")
    bm.to_mesh(extra.data)
    bm.free()
    assert len(extra.data.polygons) == 0
    listed = frozenset(entry.key for entry in plan.entries)
    kept = plan_xxmi.build(scene, listed)
    assert [c["name"] for c in kept.message["components"]] == names, kept.message["components"]
    assert kept.message["errors"] == [], kept.message["errors"]
    fresh = plan_xxmi.build(scene)
    assert [c["name"] for c in fresh.message["components"]] == ["FixBodyA", "FixBodyB"], fresh.message["components"]
    print("fallback: %d objects, textures match, faceless objects kept" % len(names))


def main():
    fixture = load_fixture_builder()
    with tempfile.TemporaryDirectory() as scratch:
        for case in ("gimi", "srmi", "zzmi"):
            run_case(fixture, case, Path(scratch))
        run_fallback(fixture, Path(scratch))
    print("OK")


try:
    main()
except Exception:
    traceback.print_exc()
    sys.stdout.flush()
    os._exit(1)
sys.stdout.flush()
os._exit(0)
