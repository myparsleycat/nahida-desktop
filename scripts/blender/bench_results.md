# Blender live link benchmark results

Measured with `scripts/blender/bench_live_link.py` (development only, not shipped) on 2026-11-18.

- Machine: AMD Ryzen 7 9700X (8 cores / 16 threads), 64 GB RAM, Windows 11.
- Blender versions found: **5.2.2 LTS only** (`C:\Program Files\Blender Foundation\Blender 5.2\blender.exe`).
  Searched Program Files, all Steam libraries (`C:`, `D:\SteamLibrary`, `F:\SteamLibrary`), the uninstall registry
  keys and the `.blend` file association. The association still points to
  `C:\Program Files\Blender Foundation\Blender 3.6\blender-launcher.exe`, which no longer exists.
  **3.6, 4.2, 4.5 and 5.0 were not measured.** The script has fallbacks for them (`calc_normals_split` before 4.1,
  `ng.inputs` before 4.0) that have not been executed.
- Synthetic mesh: triangulated grid, smooth shading, UV maps `TEXCOORD.xy` / `TEXCOORD1.xy`, corner `FLOAT_COLOR`
  attribute `COLOR`, 60 vertex groups `"0".."59"` with 4–6 influences per vertex inserted in random order, a
  shape key (value 0.5) and an Armature modifier with 60 bones. Median of 7 repetitions; the per-group `vg.weight()`
  method ran 1 repetition.

Reproduce:

```powershell
& 'C:\Program Files\Blender Foundation\Blender 5.2\5.2\python\bin\python.exe' -I scripts/blender/bench_live_link.py run --out-dir .tmp/bench --reps 7
```

## Blender 5.2.2 LTS (Python 3.13.13, numpy 2.3.4)

Times in milliseconds (median). "fg" = `foreach_get` into a preallocated numpy array.

| metric | 10,000 v / 19,602 tri | 24,964 v / 49,298 tri | 50,176 v / 99,458 tri | 149,769 v / 297,992 tri |
|---|---:|---:|---:|---:|
| depsgraph update after pose change (Blender's own cost) | 0.21 | 0.23 | 0.33 | 0.85 |
| `to_mesh()` | 0.01 | 0.02 | 0.03 | 0.02 |
| `to_mesh_clear()` | 0.18 | 0.54 | 1.04 | 2.92 |
| fg vertex `co` | 0.26 | 0.65 | 1.23 | 2.81 |
| fg vertex `undeformed_co` | 0.27 | 0.59 | 0.81 | 2.54 |
| fg `loops` `vertex_index` | 2.35 | 5.50 | 10.81 | 31.66 |
| fg `polygons` `vertices` | 2.08 | 5.41 | 11.14 | 33.76 |
| fg attribute `.corner_vert` `value` | 1.16 | 2.96 | 6.03 | 18.90 |
| fg `corner_normals` `vector` (first access) | 0.02 | 0.10 | 0.20 | 0.66 |
| fg `corner_normals` `vector` (cached) | 0.01 | 0.08 | 0.15 | 0.50 |
| fg UV via `attributes["TEXCOORD.xy"]` `vector` | 0.05 | 0.11 | 0.24 | 0.72 |
| fg UV via `uv_layers["TEXCOORD.xy"]` `uv` | 1.30 | 2.68 | 5.19 | 14.39 |
| fg color `attributes["COLOR"]` `color` | 0.07 | 0.43 | 0.84 | 2.27 |
| blake2b-128 co | 0.12 | 0.29 | 0.57 | 1.33 |
| blake2b-128 loop vertex_index | 0.22 | 0.55 | 0.91 | 2.47 |
| blake2b-128 corner normals | 0.65 | 1.29 | 2.53 | 7.67 |
| blake2b-128 UV | 0.33 | 0.83 | 1.66 | 4.97 |
| blake2b-128 color | 0.64 | 1.67 | 3.30 | 10.23 |
| bmesh `from_mesh` | 2.56 | 8.03 | 15.65 | 63.01 |
| bmesh `triangulate` (already all tris) | 2.62 | 6.62 | 14.80 | 58.21 |
| bmesh `to_mesh` (into the `to_mesh()` result) | 0.59 | 1.87 | 4.51 | 15.96 |
| `calc_tangents(uvmap="TEXCOORD.xy")` | 1.70 | 4.68 | 9.56 | 28.17 |
| fg `loops` `tangent` | 2.33 | 5.96 | 12.86 | 35.76 |
| fg `loops` `bitangent_sign` | 2.05 | 5.28 | 11.25 | 31.80 |
| **weights (1a)** RNA loop over `vertex.groups` | 17.31 | 44.70 | 89.43 | 270.07 |
| **weights (1b)** RNA nested comprehension | 19.36 | 70.23 | 136.34 | 506.60 |
| **weights (2)** bmesh `from_mesh` (shareable with triangulation) | 2.49 | 8.05 | 17.42 | 69.31 |
| **weights (2)** bmesh deform layer `items()` | 5.43 | 14.13 | 28.02 | 87.03 |
| **weights (3)** `vg.weight(i)` per group with try/except | 387.49 | 990.70 | 2165.13 | 5991.70 |
| **weights (4)** GN modifier first evaluation | 4.04 | 4.31 | 6.13 | 10.81 |
| **weights (4)** depsgraph update after pose change with GN modifier | 1.96 | 2.79 | 4.05 | 7.75 |
| **weights (4)** 60 dense fg + numpy to sparse | 2.40 | 5.21 | 14.68 | 38.01 |
| **positions-only update: extract + hash** | 2.41 | 5.56 | 14.06 | 27.45 |
| **positions-only update: pipe write** | 0.12 | 0.80 | 1.05 | 3.49 |
| **positions-only update: total** | **2.53** | **6.30** | **15.17** | **30.98** |
| positions + tangents update: total | 6.78 | 16.05 | 35.76 | 95.39 |

The positions-only update is what an add-on handler does after Blender has evaluated the depsgraph:
`to_mesh()`, `.corner_vert` fg + blake2b (topology check), `co` fg, `corner_normals` fg, blake2b of `co`, one
length-prefixed pipe message (topology hash, co, corner normals; 2.07 MB at 49k triangles), `to_mesh_clear()`.

Named pipe, Blender's Python writing 1 MiB chunks through `open(r'\\.\pipe\…', 'r+b', buffering=0)` to a ctypes
`ReadFile` sink, 512 MiB per run: **9,680 MB/s** with a 64 KiB server buffer, **10,125 MB/s** with 1 MiB.

All four weight methods produced identical data (after sorting by group index for methods 3 and 4) at every size.

## Open questions

| question | answer on 5.2.2 |
|---|---|
| Does a `to_mesh()` result carry ORCO? | **No.** `undeformed_co` equals the deformed `co` exactly (max diff 0.0) both with defaults and with `preserve_all_data_layers=True, depsgraph=dg`, although the armature and shape key moved every vertex. It matches neither the basis nor the shape-key mix. Rest positions must be read from the original `obj.data` (or the shape-key basis), not from `undeformed_co`. |
| Does a bmesh round trip of an all-triangle mesh preserve order? | **Yes.** Vertex `co`, loop `vertex_index`, polygon `loop_start` and UVs are identical after `from_mesh` → `triangulate` → `to_mesh`. |
| Is `vertex.groups` order identical to the bmesh deform-layer `items()` order? | **Yes**, element for element, with a random (non-sorted) insertion order. `vg.weight()` and the GN path can only give group-index order. |
| Does `mesh.vertex_colors` still exist in 5.x? | **Yes**, `Mesh.vertex_colors` is still an RNA property in 5.2.2 (existence only; its contents were not checked). `color_attributes` and `corner_normals` exist; `calc_normals_split` does not. |
| Are vertex groups exposed in `mesh.attributes`? | **No.** `attributes.get("0")` is `None` on the evaluated mesh. |

Other findings:

- `to_mesh()` itself is effectively free (implicit sharing); the cost appears in reading data and in `to_mesh_clear()`.
- Corner-domain reads that go through `MeshLoop` (`vertex_index`, `tangent`, `bitangent_sign`) and `uv_layers` are
  5–20× slower than reads through `attributes`. Read UVs via `attributes[name].data.foreach_get("vector")` and
  corner vertices via the `.corner_vert` attribute when present (it exists in 5.2.2; not verified in older versions).
- Hashing corner arrays costs more than reading them. Hash only what decides whether to resend.
- The pipe is not a bottleneck: a 2 MB frame takes about 1 ms.

## Recommendation for weight extraction

Keep weights out of the positions-only update entirely. At 49k triangles that update measures **6.3 ms** on 5.2.2,
well under the 30 ms budget; even 99k triangles is 15 ms. Any weight method added per frame would cost 5–45 ms more
at the same size.

Extract weights only on a full sync (first send, topology hash change, or while the object is in Weight Paint mode /
after an edit-mode exit), with **method (2), the bmesh deform layer**:

- It is the only fast method that keeps native `vertex.groups` order (verified identical) and zero-weight memberships.
- The full-sync path already builds a bmesh for triangulation, so `from_mesh` is shared and weights add only the
  `items()` loop: 14 ms at 49k triangles, 28 ms at 99k.
- Method (1a) RNA is about 3× slower (45 ms at 49k triangles); (1b) comprehension is slower still.
- Method (3) `vg.weight(i)` is about 1 s at 25k vertices and reads the original mesh, not the evaluated one. Rejected.
- Method (4) GN has the cheapest extraction (5 ms at 49k triangles) but adds 2–7 ms to every depsgraph update for as
  long as the modifier exists, modifies the user's modifier stack, loses native order and drops zero-weight
  memberships. Use it only if the Go side sorts influences by weight anyway and a temporary modifier is acceptable.

Estimated full sync at 49k triangles on 5.2.2: from_mesh 8 + triangulate 6.6 + to_mesh 1.9 + weights 14 +
tangents 16 + other reads and hashes ~8 ≈ 55 ms. Tangent reads are the next biggest cost; computing tangents in Go
would remove about 16 ms.

Caveats: one Blender version, one machine, a synthetic grid with one Armature modifier. Real character meshes with
more modifiers change Blender's evaluation cost, not the add-on costs above.
