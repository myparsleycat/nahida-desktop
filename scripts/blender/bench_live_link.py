"""Development benchmark for the Blender live link. Not shipped with the app.

Measures how long the Blender side of the live link takes to pull mesh data out of
Blender (to_mesh, triangulation, tangents, foreach_get, hashing, vertex-group weights)
and how fast Python can write to a Windows named pipe.

Usage (from the repository root):

    # Run inside every Blender found on this machine; writes one JSON file per version.
    "<blender python.exe>" scripts/blender/bench_live_link.py run --out-dir <dir>

    # Run inside one Blender directly.
    blender.exe -b --factory-startup --python-exit-code 1 \
        --python scripts/blender/bench_live_link.py -- --out result.json

    # Render the JSON files as Markdown tables.
    "<python.exe>" scripts/blender/bench_live_link.py report <dir>/*.json

The `sink` mode is the named-pipe server the benchmark starts for itself.
"""

import argparse
import ctypes
import glob
import hashlib
import json
import math
import os
import platform
import statistics
import struct
import subprocess
import sys
import time

PIPE_PREFIX = "\\\\.\\pipe\\"
DEFAULT_SIZES = [10000, 25000, 50000, 150000]
MIB = 1 << 20


# ---------------------------------------------------------------------------
# Host side: Blender discovery, launching, pipe sink, report rendering.
# ---------------------------------------------------------------------------


def find_blenders():
    candidates = []
    for root in (os.environ.get("ProgramFiles", r"C:\Program Files"), os.environ.get("ProgramFiles(x86)", "")):
        if root:
            candidates += glob.glob(os.path.join(root, "Blender Foundation", "*", "blender.exe"))

    for lib in _steam_libraries():
        candidates.append(os.path.join(lib, "steamapps", "common", "Blender", "blender.exe"))

    try:
        import winreg

        try:
            with winreg.OpenKey(winreg.HKEY_CLASSES_ROOT, r"blendfile\shell\open\command") as k:
                cmd = winreg.QueryValue(k, None)
            exe = cmd.split('"')[1] if cmd.startswith('"') else cmd.split()[0]
            candidates.append(os.path.join(os.path.dirname(exe), "blender.exe"))
        except OSError:
            pass
        for hive, path in (
            (winreg.HKEY_LOCAL_MACHINE, r"SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall"),
            (winreg.HKEY_LOCAL_MACHINE, r"SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall"),
            (winreg.HKEY_CURRENT_USER, r"SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall"),
        ):
            try:
                root = winreg.OpenKey(hive, path)
            except OSError:
                continue
            for i in range(winreg.QueryInfoKey(root)[0]):
                try:
                    with winreg.OpenKey(root, winreg.EnumKey(root, i)) as sub:
                        name = winreg.QueryValueEx(sub, "DisplayName")[0]
                        if "blender" not in str(name).lower():
                            continue
                        loc = winreg.QueryValueEx(sub, "InstallLocation")[0]
                        candidates.append(os.path.join(loc, "blender.exe"))
                except OSError:
                    continue
    except ImportError:
        pass

    seen, found = set(), []
    for c in candidates:
        key = os.path.normcase(os.path.abspath(c))
        if key not in seen and os.path.isfile(c):
            seen.add(key)
            found.append(c)
    return found


def _steam_libraries():
    vdf = os.path.join(os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)"), "Steam", "steamapps",
                       "libraryfolders.vdf")
    libs = []
    try:
        with open(vdf, encoding="utf-8") as f:
            for line in f:
                parts = line.strip().split('"')
                if len(parts) >= 4 and parts[1] == "path":
                    libs.append(parts[3].replace("\\\\", "\\"))
    except OSError:
        pass
    return libs


def host_run(args):
    exes = args.blender or find_blenders()
    print("Blender executables:", *exes, sep="\n  ")
    if not exes:
        print("No Blender found; nothing measured.")
        return 1
    os.makedirs(args.out_dir, exist_ok=True)
    failed = 0
    for exe in exes:
        tag = os.path.basename(os.path.dirname(exe)).replace(" ", "_")
        out = os.path.join(args.out_dir, f"bench_{tag}.json")
        cmd = [exe, "-b", "--factory-startup", "--python-exit-code", "1", "--python", os.path.abspath(__file__), "--",
               "--out", out, "--reps", str(args.reps), "--sizes", *map(str, args.sizes)]
        print(">", " ".join(cmd), flush=True)
        r = subprocess.run(cmd, timeout=args.timeout)
        if r.returncode != 0:
            print(f"  failed with exit code {r.returncode}")
            failed += 1
    return 1 if failed else 0


def host_sink(args):
    """Named-pipe server: reads length-prefixed messages and acknowledges each one."""
    k32 = ctypes.WinDLL("kernel32", use_last_error=True)
    HANDLE = ctypes.c_void_p
    k32.CreateNamedPipeW.restype = HANDLE
    k32.CreateNamedPipeW.argtypes = [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_uint32,
                                     ctypes.c_uint32, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_void_p]
    k32.ConnectNamedPipe.argtypes = [HANDLE, ctypes.c_void_p]
    k32.ReadFile.argtypes = [HANDLE, ctypes.c_void_p, ctypes.c_uint32, ctypes.POINTER(ctypes.c_uint32),
                             ctypes.c_void_p]
    k32.WriteFile.argtypes = [HANDLE, ctypes.c_void_p, ctypes.c_uint32, ctypes.POINTER(ctypes.c_uint32),
                              ctypes.c_void_p]
    k32.CloseHandle.argtypes = [HANDLE]

    PIPE_ACCESS_DUPLEX, PIPE_TYPE_BYTE_WAIT, ERROR_PIPE_CONNECTED = 3, 0, 535
    h = k32.CreateNamedPipeW(PIPE_PREFIX + args.name, PIPE_ACCESS_DUPLEX, PIPE_TYPE_BYTE_WAIT, 1, args.buffer,
                             args.buffer, 0, None)
    if h in (None, ctypes.c_void_p(-1).value):
        raise OSError(ctypes.get_last_error(), "CreateNamedPipeW")
    if not k32.ConnectNamedPipe(h, None) and ctypes.get_last_error() != ERROR_PIPE_CONNECTED:
        raise OSError(ctypes.get_last_error(), "ConnectNamedPipe")

    buf = ctypes.create_string_buffer(4 * MIB)
    got = ctypes.c_uint32()

    def read_exact(n, keep):
        out = bytearray() if keep else None
        while n:
            if not k32.ReadFile(h, buf, min(n, len(buf)), ctypes.byref(got), None) or got.value == 0:
                return None
            if keep:
                out += buf.raw[: got.value]
            n -= got.value
        return out if keep else b""

    try:
        while True:
            head = read_exact(8, True)
            if head is None:
                break
            (n,) = struct.unpack("<Q", head)
            if n == 0 or read_exact(n, False) is None:
                break
            ack = struct.pack("<Q", n)
            k32.WriteFile(h, ack, 8, ctypes.byref(got), None)
    finally:
        k32.CloseHandle(h)
    return 0


def host_report(args):
    for path in args.files:
        with open(path, encoding="utf-8") as f:
            r = json.load(f)
        print(render_markdown(r))
    return 0


def render_markdown(r):
    lines = [f"### Blender {r['blender']} (Python {r['python']}, numpy {r['numpy']})", ""]
    sizes = r["sizes"]
    head = "| metric (median ms) | " + " | ".join(f"{s['verts']:,} v / {s['tris']:,} tri" for s in sizes) + " |"
    lines += [head, "|---|" + "---:|" * len(sizes)]
    names = []
    for s in sizes:
        for k in s["samples"]:
            if k not in names:
                names.append(k)
    for k in names:
        cells = []
        for s in sizes:
            v = s["samples"].get(k)
            cells.append("—" if v is None else f"{v['median']:.2f}")
        lines.append(f"| {k} | " + " | ".join(cells) + " |")
    lines.append("")
    for s in sizes:
        lines.append(f"Checks at {s['verts']:,} vertices: `{json.dumps(s['checks'], sort_keys=True)}`")
    if r.get("pipe"):
        lines += ["", "Named pipe: " + ", ".join(f"{p['server_buffer'] // 1024} KiB server buffer "
                                                 f"{p['mb_per_s']:.0f} MB/s" for p in r["pipe"])]
    lines += ["", f"API: `{json.dumps(r['api'], sort_keys=True)}`", ""]
    return "\n".join(lines)


def host_main(argv):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    r = sub.add_parser("run", help="run the benchmark in every Blender found")
    r.add_argument("--blender", nargs="*", help="blender.exe paths; default: auto-detect")
    r.add_argument("--out-dir", required=True)
    r.add_argument("--sizes", type=int, nargs="+", default=DEFAULT_SIZES)
    r.add_argument("--reps", type=int, default=5)
    r.add_argument("--timeout", type=int, default=3600)
    sub.add_parser("find", help="list Blender executables")
    s = sub.add_parser("sink", help="named-pipe sink (started by the benchmark)")
    s.add_argument("name")
    s.add_argument("--buffer", type=int, default=MIB)
    rep = sub.add_parser("report", help="render result JSON as Markdown")
    rep.add_argument("files", nargs="+")
    a = p.parse_args(argv)
    if a.cmd == "find":
        print(*find_blenders(), sep="\n")
        return 0
    return {"run": host_run, "sink": host_sink, "report": host_report}[a.cmd](a)


# ---------------------------------------------------------------------------
# Blender side.
# ---------------------------------------------------------------------------


class Samples:
    def __init__(self):
        self.data = {}

    def add(self, name, ms):
        self.data.setdefault(name, []).append(ms)

    def time(self, name, fn):
        t = time.perf_counter_ns()
        r = fn()
        self.add(name, (time.perf_counter_ns() - t) / 1e6)
        return r

    def summary(self):
        return {k: {"median": statistics.median(v), "min": min(v), "max": max(v), "n": len(v)}
                for k, v in self.data.items()}


def blender_main(argv):
    import bpy
    import numpy as np

    p = argparse.ArgumentParser(prog="bench_live_link.py (inside Blender)")
    p.add_argument("--out", required=True)
    p.add_argument("--sizes", type=int, nargs="+", default=DEFAULT_SIZES)
    p.add_argument("--reps", type=int, default=5)
    p.add_argument("--groups", type=int, default=60)
    p.add_argument("--pipe-mib", type=int, default=512)
    p.add_argument("--skip-pipe", action="store_true")
    p.add_argument("--skip-per-group", type=int, default=0,
                   help="skip vg.weight() extraction above this vertex count (0 = never skip)")
    a = p.parse_args(argv)

    result = {
        "blender": bpy.app.version_string,
        "version": list(bpy.app.version),
        "python": sys.version.split()[0],
        "numpy": np.__version__,
        "cpu": platform.processor(),
        "cpu_count": os.cpu_count(),
        "api": probe_api(bpy),
        "pipe": None,
        "sizes": [],
    }

    link = None
    if not a.skip_pipe:
        result["pipe"] = []
        for buf in (64 * 1024, MIB):
            pl = PipeLink(bpy, buf)
            try:
                result["pipe"].append(pl.throughput(a.pipe_mib))
            finally:
                pl.close()
        link = PipeLink(bpy, MIB)

    try:
        for n in a.sizes:
            print(f"[bench] Blender {bpy.app.version_string}: {n} vertices", flush=True)
            result["sizes"].append(bench_size(bpy, np, n, a, link))
            with open(a.out, "w", encoding="utf-8") as f:
                json.dump(result, f, indent=1)
    finally:
        if link:
            link.close()

    with open(a.out, "w", encoding="utf-8") as f:
        json.dump(result, f, indent=1)
    print(render_markdown(result))


def probe_api(bpy):
    mesh_props = bpy.types.Mesh.bl_rna.properties.keys()
    funcs = bpy.types.Mesh.bl_rna.functions.keys()
    vert_props = bpy.types.MeshVertex.bl_rna.properties.keys()
    return {
        "mesh.vertex_colors": "vertex_colors" in mesh_props,
        "mesh.color_attributes": "color_attributes" in mesh_props,
        "mesh.corner_normals": "corner_normals" in mesh_props,
        "mesh.calc_normals_split": "calc_normals_split" in funcs,
        "mesh.calc_tangents": "calc_tangents" in funcs,
        "vertex.undeformed_co": "undeformed_co" in vert_props,
        "sys.executable": sys.executable,
    }


class PipeLink:
    def __init__(self, bpy, server_buffer):
        self.server_buffer = server_buffer
        self.name = f"nhd-bench-{os.getpid()}-{server_buffer}"
        self.proc = subprocess.Popen([python_exe(bpy), "-I", os.path.abspath(__file__), "sink", self.name,
                                      "--buffer", str(server_buffer)])
        deadline = time.monotonic() + 10
        while True:
            try:
                self.f = open(PIPE_PREFIX + self.name, "r+b", buffering=0)
                break
            except (FileNotFoundError, OSError):
                if time.monotonic() > deadline or self.proc.poll() is not None:
                    raise
                time.sleep(0.01)

    def write_all(self, mv):
        mv = memoryview(mv).cast("B")
        while len(mv):
            n = self.f.write(mv)
            mv = mv[n:]

    def send(self, *parts):
        total = sum(memoryview(p).nbytes for p in parts)
        self.write_all(struct.pack("<Q", total))
        for p in parts:
            self.write_all(p)
        return total

    def ack(self, expect):
        data = b""
        while len(data) < 8:
            chunk = self.f.read(8 - len(data))
            if not chunk:
                raise EOFError("pipe closed")
            data += chunk
        (got,) = struct.unpack("<Q", data)
        if got != expect:
            raise RuntimeError(f"ack {got} != {expect}")

    def throughput(self, total_mib):
        chunk = memoryview(bytearray(os.urandom(MIB)))
        runs = []
        for _ in range(3):
            t = time.perf_counter()
            self.write_all(struct.pack("<Q", total_mib * MIB))
            for _ in range(total_mib):
                self.write_all(chunk)
            self.ack(total_mib * MIB)
            runs.append(time.perf_counter() - t)
        best = statistics.median(runs)
        return {"server_buffer": self.server_buffer, "chunk": MIB, "total_mib": total_mib,
                "seconds_median": best, "mb_per_s": total_mib * MIB / 1e6 / best}

    def close(self):
        try:
            self.write_all(struct.pack("<Q", 0))
            self.f.close()
        except OSError:
            pass
        try:
            self.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.proc.kill()


def python_exe(bpy):
    exe = sys.executable
    if os.path.basename(exe).lower().startswith("python"):
        return exe
    major, minor = bpy.app.version[:2]
    return os.path.join(os.path.dirname(bpy.app.binary_path), f"{major}.{minor}", "python", "bin", "python.exe")


def build_scene(bpy, np, n_target, n_groups, seed=1):
    import bmesh
    from mathutils import Quaternion

    bpy.ops.wm.read_factory_settings(use_empty=True)
    scene = bpy.context.scene
    rng = np.random.default_rng(seed)

    side = max(2, round(math.sqrt(n_target)))
    xs = np.linspace(-1.0, 1.0, side, dtype=np.float32)
    gx, gy = np.meshgrid(xs, xs)
    co = np.stack([gx.ravel(), gy.ravel(), 0.05 * np.sin(gx * 7).ravel() * np.cos(gy * 5).ravel()], 1)
    co = co.astype(np.float32)
    idx = np.arange(side * side, dtype=np.int32).reshape(side, side)
    a, b, c, d = idx[:-1, :-1].ravel(), idx[:-1, 1:].ravel(), idx[1:, 1:].ravel(), idx[1:, :-1].ravel()
    tris = np.empty((a.size * 2, 3), np.int32)
    tris[0::2] = np.stack([a, b, c], 1)
    tris[1::2] = np.stack([a, c, d], 1)

    me = bpy.data.meshes.new("Bench")
    me.from_pydata(co.tolist(), [], tris.tolist())
    if hasattr(me, "shade_smooth"):
        me.shade_smooth()
    else:
        me.polygons.foreach_set("use_smooth", [True] * len(me.polygons))

    corner_vert = tris.ravel()
    uv0 = co[corner_vert, :2] * 0.5 + 0.5
    for name, uv in (("TEXCOORD.xy", uv0), ("TEXCOORD1.xy", uv0 * 0.5 + 0.25)):
        me.uv_layers.new(name=name).data.foreach_set("uv", uv.astype(np.float32).ravel())
    rgba = np.concatenate([uv0, np.full((corner_vert.size, 2), 0.5, np.float32)], 1).astype(np.float32)
    me.color_attributes.new(name="COLOR", type="FLOAT_COLOR", domain="CORNER").data.foreach_set("color", rgba.ravel())

    obj = bpy.data.objects.new("Bench", me)
    scene.collection.objects.link(obj)
    for g in range(n_groups):
        obj.vertex_groups.new(name=str(g))

    # Insertion order per vertex is random so "native order" differs from group-index order.
    nv = co.shape[0]
    k = rng.integers(4, 7, nv)
    order = np.argsort(rng.random((nv, n_groups)), axis=1)[:, :6]
    w = rng.random((nv, 6)) + 0.05
    w[np.arange(6)[None, :] >= k[:, None]] = 0.0
    w /= w.sum(1, keepdims=True)
    bm = bmesh.new()
    bm.from_mesh(me)
    dl = bm.verts.layers.deform.verify()
    order_l, w_l, k_l = order.tolist(), w.tolist(), k.tolist()
    for i, v in enumerate(bm.verts):
        dv = v[dl]
        oi, wi = order_l[i], w_l[i]
        for j in range(k_l[i]):
            dv[oi[j]] = wi[j]
    bm.to_mesh(me)
    bm.free()

    obj.shape_key_add(name="Basis", from_mix=False)
    key = obj.shape_key_add(name="Lift", from_mix=False)
    lifted = co.copy()
    lifted[:, 2] += 0.1
    key.data.foreach_set("co", lifted.ravel())
    key.value = 0.5

    arm_data = bpy.data.armatures.new("BenchArm")
    arm = bpy.data.objects.new("BenchArm", arm_data)
    scene.collection.objects.link(arm)
    bpy.context.view_layer.objects.active = arm
    bpy.ops.object.mode_set(mode="EDIT")
    for g in range(n_groups):
        x, y = -1 + 2 * (g % 8) / 7, -1 + 2 * (g // 8) / 7
        eb = arm_data.edit_bones.new(str(g))
        eb.head, eb.tail = (x, y, 0.0), (x, y, 0.2)
    bpy.ops.object.mode_set(mode="OBJECT")
    for g in range(0, n_groups, 5):
        arm.pose.bones[str(g)].rotation_quaternion = Quaternion((1, 0, 0), 0.3)
    mod = obj.modifiers.new("Armature", "ARMATURE")
    mod.object = arm

    return obj, arm, co, lifted


def read_corner_normals(me, buf):
    if hasattr(me, "corner_normals"):
        me.corner_normals.foreach_get("vector", buf)
    else:
        me.calc_normals_split()
        me.loops.foreach_get("normal", buf)


def weights_rna_loop(np, me):
    from array import array

    counts, groups, weights = array("B"), array("H"), array("f")
    for v in me.vertices:
        gs = v.groups
        counts.append(len(gs))
        for g in gs:
            groups.append(g.group)
            weights.append(g.weight)
    return (np.frombuffer(counts, np.uint8), np.frombuffer(groups, np.uint16), np.frombuffer(weights, np.float32))


def weights_rna_comprehension(np, me):
    rows = [[(g.group, g.weight) for g in v.groups] for v in me.vertices]
    counts = np.fromiter(map(len, rows), np.uint8, len(rows))
    flat = [p for r in rows for p in r]
    groups = np.fromiter((p[0] for p in flat), np.uint16, len(flat))
    weights = np.fromiter((p[1] for p in flat), np.float32, len(flat))
    return counts, groups, weights


def weights_bmesh(np, bm):
    from array import array

    dl = bm.verts.layers.deform.verify()
    counts, groups, weights = array("B"), array("H"), array("f")
    for v in bm.verts:
        items = v[dl].items()
        counts.append(len(items))
        for g, w in items:
            groups.append(g)
            weights.append(w)
    return (np.frombuffer(counts, np.uint8), np.frombuffer(groups, np.uint16), np.frombuffer(weights, np.float32))


def weights_per_group(np, obj, nv):
    triples = []
    for gi, vg in enumerate(obj.vertex_groups):
        w = vg.weight
        for i in range(nv):
            try:
                triples.append((i, gi, w(i)))
            except RuntimeError:
                pass
    triples.sort(key=lambda t: t[0])  # stable: group-index order within a vertex
    counts = np.bincount(np.fromiter((t[0] for t in triples), np.int64, len(triples)), minlength=nv)
    return (counts.astype(np.uint8), np.fromiter((t[1] for t in triples), np.uint16, len(triples)),
            np.fromiter((t[2] for t in triples), np.float32, len(triples)))


def sorted_by_group(np, sparse):
    counts, groups, weights = sparse
    vert = np.repeat(np.arange(counts.size), counts)
    o = np.lexsort((groups, vert))
    return counts, groups[o], weights[o]


def same_sparse(np, x, y):
    return all(a.shape == b.shape and np.array_equal(a, b) for a, b in zip(x, y))


def build_weight_gn(bpy, n_groups):
    ng = bpy.data.node_groups.new("BenchVG", "GeometryNodeTree")
    if hasattr(ng, "interface"):
        ng.interface.new_socket(name="Geometry", in_out="INPUT", socket_type="NodeSocketGeometry")
        ng.interface.new_socket(name="Geometry", in_out="OUTPUT", socket_type="NodeSocketGeometry")
    else:
        ng.inputs.new("NodeSocketGeometry", "Geometry")
        ng.outputs.new("NodeSocketGeometry", "Geometry")
    gin, gout = ng.nodes.new("NodeGroupInput"), ng.nodes.new("NodeGroupOutput")

    def pick(sockets, name, typ):
        return next(s for s in sockets if s.name == name and s.type == typ and getattr(s, "enabled", True))

    prev = gin.outputs[0]
    for g in range(n_groups):
        na = ng.nodes.new("GeometryNodeInputNamedAttribute")
        na.data_type = "FLOAT"
        na.inputs["Name"].default_value = str(g)
        st = ng.nodes.new("GeometryNodeStoreNamedAttribute")
        st.data_type, st.domain = "FLOAT", "POINT"
        st.inputs["Name"].default_value = f"bench_w{g}"
        ng.links.new(prev, st.inputs["Geometry"])
        ng.links.new(pick(na.outputs, "Attribute", "VALUE"), pick(st.inputs, "Value", "VALUE"))
        prev = st.outputs["Geometry"]
    ng.links.new(prev, gout.inputs[0])
    return ng


def weights_from_dense(np, me, n_groups, dense):
    for g in range(n_groups):
        me.attributes[f"bench_w{g}"].data.foreach_get("value", dense[g])
    w = dense.T
    mask = w != 0
    counts = mask.sum(1).astype(np.uint8)
    vi, gi = np.nonzero(mask)
    return counts, gi.astype(np.uint16), w[vi, gi]


def bench_size(bpy, np, n, a, link):
    import bmesh
    from mathutils import Quaternion

    obj, arm, basis, lifted = build_scene(bpy, np, n, a.groups)
    dg = bpy.context.evaluated_depsgraph_get()
    dg.update()
    obj_eval = obj.evaluated_get(dg)
    S = Samples()
    checks = {}
    reps = a.reps
    blake = hashlib.blake2b
    pose_bone = arm.pose.bones["1"]
    step = [0]

    def pose_change():
        step[0] += 1
        pose_bone.rotation_quaternion = Quaternion((0, 1, 0), 0.05 * step[0])

    for _ in range(reps):
        pose_change()
        S.time("depsgraph update after pose change (Blender, not add-on)", dg.update)

    # Per-array costs on a fresh to_mesh() result each repetition.
    for _ in range(reps):
        me = S.time("to_mesh()", obj_eval.to_mesh)
        nv, nl = len(me.vertices), len(me.loops)
        co = np.empty(nv * 3, np.float32)
        und = np.empty(nv * 3, np.float32)
        vidx = np.empty(nl, np.int32)
        nrm = np.empty(nl * 3, np.float32)
        uv = np.empty(nl * 2, np.float32)
        col = np.empty(nl * 4, np.float32)
        S.time("fg vertex co", lambda: me.vertices.foreach_get("co", co))
        S.time("fg vertex undeformed_co", lambda: me.vertices.foreach_get("undeformed_co", und))
        S.time("fg loop vertex_index", lambda: me.loops.foreach_get("vertex_index", vidx))
        S.time("fg polygon vertices (= corner verts when all tris)",
               lambda: me.polygons.foreach_get("vertices", vidx))
        corner_vert = me.attributes.get(".corner_vert")
        if corner_vert is not None:
            S.time("fg attribute .corner_vert", lambda: corner_vert.data.foreach_get("value", vidx))
        S.time("fg corner normals (first access)", lambda: read_corner_normals(me, nrm))
        S.time("fg corner normals (cached)", lambda: read_corner_normals(me, nrm))
        S.time("fg uv TEXCOORD.xy (attribute vector)",
               lambda: me.attributes["TEXCOORD.xy"].data.foreach_get("vector", uv))
        S.time("fg uv TEXCOORD.xy (uv_layers uv)", lambda: me.uv_layers["TEXCOORD.xy"].data.foreach_get("uv", uv))
        S.time("fg color COLOR", lambda: me.attributes["COLOR"].data.foreach_get("color", col))
        for label, arr in (("co", co), ("loop vertex_index", vidx), ("corner normals", nrm), ("uv", uv),
                           ("color", col)):
            S.time(f"blake2b {label}", lambda arr=arr: blake(arr, digest_size=16).digest())

        before = (co.copy(), vidx.copy(), uv.copy())
        loop_start = np.empty(len(me.polygons), np.int32)
        me.polygons.foreach_get("loop_start", loop_start)

        bm = bmesh.new()
        S.time("bmesh from_mesh", lambda: bm.from_mesh(me))
        S.time("bmesh triangulate (all tris)", lambda: bmesh.ops.triangulate(bm, faces=bm.faces[:]))
        S.time("bmesh to_mesh (into to_mesh result)", lambda: bm.to_mesh(me))
        bm.free()
        after_co = np.empty(len(me.vertices) * 3, np.float32)
        after_vidx = np.empty(len(me.loops), np.int32)
        after_ls = np.empty(len(me.polygons), np.int32)
        after_uv = np.empty(len(me.loops) * 2, np.float32)
        me.vertices.foreach_get("co", after_co)
        me.loops.foreach_get("vertex_index", after_vidx)
        me.polygons.foreach_get("loop_start", after_ls)
        me.attributes["TEXCOORD.xy"].data.foreach_get("vector", after_uv)
        checks["bmesh round trip preserves order (all-tri)"] = bool(
            np.array_equal(before[0], after_co) and np.array_equal(before[1], after_vidx)
            and np.array_equal(loop_start, after_ls) and np.array_equal(before[2], after_uv))

        S.time("calc_tangents(uvmap=TEXCOORD.xy)", lambda: me.calc_tangents(uvmap="TEXCOORD.xy"))
        tan = np.empty(nl * 3, np.float32)
        sgn = np.empty(nl, np.float32)
        S.time("fg loop tangent", lambda: me.loops.foreach_get("tangent", tan))
        S.time("fg loop bitangent_sign", lambda: me.loops.foreach_get("bitangent_sign", sgn))
        S.time("to_mesh_clear()", obj_eval.to_mesh_clear)

    # ORCO: does to_mesh() carry undeformed coordinates?
    for label, kwargs in (("default", {}), ("preserve_all_data_layers", {"preserve_all_data_layers": True,
                                                                         "depsgraph": dg})):
        me = obj_eval.to_mesh(**kwargs)
        co = np.empty(len(me.vertices) * 3, np.float32)
        und = np.empty_like(co)
        me.vertices.foreach_get("co", co)
        me.vertices.foreach_get("undeformed_co", und)
        checks[f"orco {label}"] = {
            "undeformed==co": bool(np.allclose(und, co)),
            "undeformed==basis": bool(np.allclose(und, basis.ravel())),
            "undeformed==shape-key mix": bool(np.allclose(und, ((basis + lifted) * 0.5).ravel())),
            "max|undeformed-co|": float(np.abs(und - co).max()),
            "evaluated co differs from basis": not bool(np.allclose(co, basis.ravel())),
        }
        obj_eval.to_mesh_clear()

    # Vertex-group weights.
    nv = len(obj.data.vertices)
    ref = None
    for _ in range(reps):
        me = obj_eval.to_mesh()
        ref = S.time("weights (1a) RNA loop vertex.groups", lambda: weights_rna_loop(np, me))
        r1b = S.time("weights (1b) RNA comprehension vertex.groups", lambda: weights_rna_comprehension(np, me))
        bm = bmesh.new()
        S.time("weights (2) bmesh from_mesh", lambda: bm.from_mesh(me))
        r2 = S.time("weights (2) bmesh deform items", lambda: weights_bmesh(np, bm))
        bm.free()
        obj_eval.to_mesh_clear()
    checks["weights 1b == 1a"] = same_sparse(np, r1b, ref)
    checks["vertex.groups order == bmesh deform items order"] = same_sparse(np, r2, ref)
    checks["vertex.groups order is group-index order"] = same_sparse(np, sorted_by_group(np, ref), ref)
    checks["influences"] = int(ref[0].sum())

    if not a.skip_per_group or nv <= a.skip_per_group:
        r3 = S.time("weights (3) vg.weight(i) per group", lambda: weights_per_group(np, obj, nv))
        checks["weights 3 == 1a sorted by group"] = same_sparse(np, r3, sorted_by_group(np, ref))

    try:
        ng = build_weight_gn(bpy, a.groups)
        gm = obj.modifiers.new("BenchVG", "NODES")
        gm.node_group = ng
        S.time("weights (4) GN modifier first evaluation", dg.update)
        for _ in range(reps):
            pose_change()
            S.time("depsgraph update after pose change with GN weight modifier", dg.update)
        dense = np.empty((a.groups, nv), np.float32)
        for _ in range(reps):
            me = S.time("weights (4) to_mesh() with GN attributes", obj_eval.to_mesh)
            r4 = S.time("weights (4) GN dense foreach_get + numpy sparse",
                        lambda: weights_from_dense(np, me, a.groups, dense))
            obj_eval.to_mesh_clear()
        checks["weights 4 == 1a sorted by group"] = same_sparse(np, r4, sorted_by_group(np, ref))
        obj.modifiers.remove(gm)
        dg.update()
    except Exception as e:  # noqa: BLE001 - optional path; record why it failed
        checks["weights (4) GN error"] = f"{type(e).__name__}: {e}"

    me = obj_eval.to_mesh()
    checks["evaluated mesh exposes vertex groups as attributes"] = me.attributes.get("0") is not None
    obj_eval.to_mesh_clear()

    # Positions-only update as the add-on handler would run it after Blender's own evaluation.
    for _ in range(reps):
        pose_change()
        dg.update()
        t0 = time.perf_counter_ns()
        me = obj_eval.to_mesh()
        nv, nl = len(me.vertices), len(me.loops)
        co = np.empty(nv * 3, np.float32)
        vidx = np.empty(nl, np.int32)
        nrm = np.empty(nl * 3, np.float32)
        corner_vert = me.attributes.get(".corner_vert")
        if corner_vert is not None:
            corner_vert.data.foreach_get("value", vidx)
        else:
            me.loops.foreach_get("vertex_index", vidx)
        topo = blake(vidx, digest_size=16).digest()
        me.vertices.foreach_get("co", co)
        read_corner_normals(me, nrm)
        blake(co, digest_size=16).digest()
        t1 = time.perf_counter_ns()
        sent = link.send(topo, co, nrm) if link else 0
        t2 = time.perf_counter_ns()
        obj_eval.to_mesh_clear()
        t3 = time.perf_counter_ns()
        S.add("POSITIONS-ONLY update: extract+hash", (t1 - t0) / 1e6)
        S.add("POSITIONS-ONLY update: pipe write", (t2 - t1) / 1e6)
        S.add("POSITIONS-ONLY update: total", (t3 - t0) / 1e6)
        if link:
            link.ack(sent)
        checks["positions-only payload bytes"] = sent

    for _ in range(reps):
        pose_change()
        dg.update()
        t0 = time.perf_counter_ns()
        me = obj_eval.to_mesh()
        nv, nl = len(me.vertices), len(me.loops)
        co = np.empty(nv * 3, np.float32)
        nrm = np.empty(nl * 3, np.float32)
        tan = np.empty(nl * 3, np.float32)
        sgn = np.empty(nl, np.float32)
        me.vertices.foreach_get("co", co)
        read_corner_normals(me, nrm)
        me.calc_tangents(uvmap="TEXCOORD.xy")
        me.loops.foreach_get("tangent", tan)
        me.loops.foreach_get("bitangent_sign", sgn)
        sent = link.send(co, nrm, tan, sgn) if link else 0
        obj_eval.to_mesh_clear()
        S.add("POSITIONS+TANGENTS update: total", (time.perf_counter_ns() - t0) / 1e6)
        if link:
            link.ack(sent)

    return {
        "verts": nv,
        "loops": nl,
        "tris": nl // 3,
        "groups": a.groups,
        "samples": S.summary(),
        "checks": checks,
    }


def main():
    try:
        import bpy  # noqa: F401
    except ImportError:
        return host_main(sys.argv[1:])
    argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    blender_main(argv)
    return 0


if __name__ == "__main__":
    code = main()
    if code:
        sys.exit(code)
