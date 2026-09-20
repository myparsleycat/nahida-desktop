"""Read-only mesh evidence and exclusive-create PNG selection for Blender code tools.

Load this reference with load_skill and execute its definitions in Blender.
inspect_uv_mesh(spec) -> hashes and component bounds.
write_uv_selection(spec, component_ids, evidence, width, height, output, padding=3)
    -> output path, SHA256, selected pixel count (rejects shared UV coverage).

spec: position_path, position_stride, position_offset, uv_path, uv_stride,
uv_offset, index_path, index_size (2/4), first_index, index_count, base_vertex.
Positions: 3 little-endian float32; UVs: 2 little-endian float32 in DDS top-left
orientation. Supply actual draw arguments/layouts; no guessed default layouts.
All paths must be in the agent's authorized roots. No files are overwritten.
"""

import hashlib
import math
import os
import struct
import zlib


def _read_buffer(path):
    with open(path, "rb") as stream:
        if os.fstat(stream.fileno()).st_size > 128 * 1024 * 1024:
            raise ValueError("Buffer exceeds 128 MiB")
        data = stream.read(128 * 1024 * 1024 + 1)
    if len(data) > 128 * 1024 * 1024:
        raise ValueError("Buffer grew beyond limit")
    return data


def _mesh(spec):
    buffers = {name: _read_buffer(spec[name + "_path"]) for name in ("position", "uv", "index")}
    hashes = {name: hashlib.sha256(data).hexdigest() for name, data in buffers.items()}
    values = {}
    for name, size in (("position", 3), ("uv", 2)):
        stride, offset = spec[name + "_stride"], spec[name + "_offset"]
        data = buffers[name]
        if stride <= 0 or offset < 0 or offset + size * 4 > stride or len(data) % stride:
            raise ValueError("Invalid " + name + " layout")
        if len(data) // stride > 250000:
            raise ValueError("Too many vertices")
        values[name] = [struct.unpack_from("<" + "f" * size, data, i + offset)
                        for i in range(0, len(data), stride)]
    size, first, count = spec["index_size"], spec["first_index"], spec["index_count"]
    if size not in (2, 4) or first < 0 or count <= 0 or count % 3 or count > 750000:
        raise ValueError("Invalid triangle draw range")
    if len(buffers["index"]) % size or (first + count) * size > len(buffers["index"]):
        raise ValueError("Index range exceeds buffer")
    indices = [item[0] + spec["base_vertex"] for item in struct.iter_unpack(
        "<H" if size == 2 else "<I", buffers["index"][first * size:(first + count) * size])]
    used = set(indices)
    for index in used:
        if index < 0 or index >= min(len(values["position"]), len(values["uv"])):
            raise ValueError("Vertex index exceeds buffer")
        if not all(math.isfinite(v) for v in values["position"][index] + values["uv"][index]):
            raise ValueError("Non-finite vertex attribute")
        if not all(0 <= v <= 1 for v in values["uv"][index]):
            raise ValueError("UV wrapping requires separate analysis")
    parent = {index: index for index in used}

    def root(index):
        while parent[index] != index:
            parent[index] = parent[parent[index]]
            index = parent[index]
        return index

    triangles = [indices[i:i + 3] for i in range(0, count, 3)]
    for a, b, c in triangles:
        roots = [root(a), root(b), root(c)]
        for key in roots:
            parent[key] = min(roots)
    groups = {}
    for index in sorted(used):
        groups.setdefault(root(index), []).append(index)
    return values, triangles, {index: root(index) for index in used}, groups, hashes


def inspect_uv_mesh(spec):
    values, triangles, roots, groups, hashes = _mesh(spec)
    components = []
    for key, ids in sorted(groups.items()):
        component = {"id": key, "vertices": len(ids)}
        for name in ("position", "uv"):
            axes = list(zip(*(values[name][index] for index in ids)))
            component[name + "Min"] = [min(axis) for axis in axes]
            component[name + "Max"] = [max(axis) for axis in axes]
        components.append(component)
    return {"hashes": hashes, "components": components, "triangles": len(triangles), "spec": dict(spec)}


def write_uv_selection(spec, component_ids, evidence, width, height, output, padding=3):
    if not (1 <= width <= 4096 and 1 <= height <= 4096 and 0 <= padding <= 4):
        raise ValueError("Invalid mask dimensions/padding")
    values, triangles, roots, groups, hashes = _mesh(spec)
    if hashes != evidence["hashes"] or spec != evidence["spec"]:
        raise ValueError("Mesh or draw specification changed since inspection")
    selected = set(component_ids)
    if not selected or not selected.issubset(groups):
        raise ValueError("Choose inspected component IDs")
    masks = [bytearray(width * height), bytearray(width * height)]
    bounds = [[width, height, -1, -1], [width, height, -1, -1]]
    work = 0
    for triangle in triangles:
        points = [(values["uv"][i][0] * width, values["uv"][i][1] * height) for i in triangle]
        (ax, ay), (bx, by), (cx, cy) = points
        area = (bx - ax) * (cy - ay) - (by - ay) * (cx - ax)
        if abs(area) < 1e-10:
            raise ValueError("Degenerate UV triangle; inspect before selecting")
        left = max(0, math.floor(min(p[0] for p in points)))
        right = min(width - 1, math.floor(max(p[0] for p in points)))
        top = max(0, math.floor(min(p[1] for p in points)))
        bottom = min(height - 1, math.floor(max(p[1] for p in points)))
        work += (right - left + 1) * (bottom - top + 1)
        if work > 50000000:
            raise ValueError("UV raster workload exceeds limit")
        mask_index = 0 if roots[triangle[0]] in selected else 1
        mask = masks[mask_index]
        bound = bounds[mask_index]
        for y in range(top, bottom + 1):
            for x in range(left, right + 1):
                u = ((bx - x - .5) * (cy - y - .5) - (by - y - .5) * (cx - x - .5)) / area
                v = ((cx - x - .5) * (ay - y - .5) - (cy - y - .5) * (ax - x - .5)) / area
                if min(u, v, 1 - u - v) >= -1e-7:
                    mask[y * width + x] = 255
                    if x < bound[0]:
                        bound[0] = x
                    if y < bound[1]:
                        bound[1] = y
                    if x > bound[2]:
                        bound[2] = x
                    if y > bound[3]:
                        bound[3] = y
    # Pad both selections for bilinear sampling; fail rather than paint shared texels.
    regions = []
    for bound in bounds:
        if bound[2] < 0:
            regions.append(None)
            continue
        x0 = max(0, bound[0] - padding)
        y0 = max(0, bound[1] - padding)
        x1 = min(width - 1, bound[2] + padding)
        y1 = min(height - 1, bound[3] + padding)
        regions.append((x0, y0, x1, y1))
        work += (x1 - x0 + 1) * (y1 - y0 + 1) * padding * 4
        if work > 50000000:
            raise ValueError("UV raster workload exceeds limit")
    for _ in range(padding):
        for i, mask in enumerate(masks):
            region = regions[i]
            if region is None:
                continue
            x0, y0, x1, y1 = region
            expanded = bytearray(mask)
            for y in range(y0, y1 + 1):
                base = y * width
                for x in range(x0, x1 + 1):
                    if mask[base + x]:
                        for yy in range(max(0, y - 1), min(height, y + 2)):
                            expanded[yy * width + max(0, x - 1):yy * width + min(width, x + 2)] = b'\xff' * (min(width, x + 2) - max(0, x - 1))
            masks[i] = expanded
    if any(a and b for a, b in zip(*masks)):
        raise ValueError("Selected and excluded UVs overlap; texture-only isolation is unsafe")
    if not any(masks[0]):
        raise ValueError("Selection covers no texel centers")

    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data))

    rows = b"".join(b"\x00" + masks[0][y * width:(y + 1) * width] for y in range(height))
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 0, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")
    with open(output, "xb") as stream:
        stream.write(png)
    return {"path": output, "sha256": hashlib.sha256(png).hexdigest(),
            "selectedPixels": sum(v != 0 for v in masks[0]), "meshHashes": hashes}
