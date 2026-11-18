# Blender live link protocol, version 1

The link streams evaluated Blender meshes from the `nahida_live_link` add-on to
Nahida Desktop's model viewer. The desktop is the server and each Blender
instance a client. Go (`internal/blenderlink`) and Python
(`addon/nahida_live_link/protocol.py`) both encode and decode the vectors in
`testdata/v1`.

## Transport

- Pipe: `\\.\pipe\nahida-blender-link-<random>`, where `<random>` is 22
  base64url characters from 16 random bytes, chosen each time the desktop starts
  the server. It is independent of the token.
- Pipe flags: byte mode, `PIPE_REJECT_REMOTE_CLIENTS`, unlimited instances. The
  first instance is created with the `FILE_CREATE` disposition, so the server
  fails instead of sharing a name another process already created (the same
  effect as `FILE_FLAG_FIRST_PIPE_INSTANCE`). go-winio v0.6.4 sets both in
  `makeServerPipeHandle`; it creates further instances with `FILE_OPEN`.
- Security descriptor: `D:P(A;;GRGW;;;<user SID>)S:(ML;;NW;;;ME)`, where the
  SID is the desktop process user. Only that user can connect, and
  low-integrity processes cannot write.
- Discovery: the desktop writes `~/.nahida-desktop/blender-link.json` after the
  pipe exists and removes it on shutdown:

  ```json
  {"pipe": "\\\\.\\pipe\\nahida-blender-link-...", "token": "...", "pid": 1234, "proto": 1}
  ```

  `token` is 43 base64url characters from 32 random bytes. `pid` is the desktop
  process ID. A client that cannot connect re-reads the file before each retry.
- At most four connections are served at once. Further connections are closed
  without a reply.
- A connection that does not complete Hello within 5 seconds, or sends nothing
  for 15 seconds afterwards, is closed.

## Frames

The framing is the same as the desktop channel to the 3DMigoto DLL
(`internal/gamelink/PROTOCOL.md`). All integers are little-endian.

| Offset | Size | Field | Meaning |
|---|---|---|---|
| 0 | 4 | `length` | Bytes after this field: 8 plus the payload size |
| 4 | 2 | `type` | Message type |
| 6 | 2 | `flags` | Reserved, must be 0 |
| 8 | 4 | `request_id` | Nonzero; a reply carries the ID of its request |
| 12 | `length - 8` | payload | Message body |

- A frame is at most 4 MiB including the length field. The server closes the
  connection on a larger `length`, a `length` below 8, nonzero flags, or a
  payload that is not consumed exactly.

## Payload encoding

| Notation | Encoding |
|---|---|
| `u8` `u16` `u32` | Little-endian integers |
| `f32` | IEEE 754 binary32 |
| `str` | `u16` byte length, then UTF-8 bytes, no terminator; invalid UTF-8 is malformed |
| `bytes[u32]` | `u32` byte length, then the bytes |
| `T[u16]` | `u16` element count, then the elements |
| `hash` | 16 bytes, BLAKE2b with a 16-byte digest and no key |

## Conversation

The client sends one request and waits for its reply before it sends the next,
because a synchronous pipe handle in Python serializes reads and writes. The
server never sends a frame on its own. Server notifications ride on Ack and
Pong as flags, and an idle client sends Ping every 500 ms to receive them.

A mesh transfer is the one exception to strict alternation: MeshBegin and its
MeshChunk frames have no reply and are written back to back, and the
MeshCommit that ends them is answered with Ack. All frames of one transfer carry
the same `request_id`. Between MeshBegin and MeshCommit only MeshChunk is
allowed; anything else closes the connection.

| Type | Name | Direction | Reply |
|---|---|---|---|
| `0x01` | Hello | client to server | HelloAck |
| `0x02` | Scene | client to server | Ack |
| `0x10` | MeshBegin | client to server | none |
| `0x11` | MeshChunk | client to server | none |
| `0x12` | MeshCommit | client to server | Ack |
| `0x20` | Ping | client to server | Pong |
| `0x30` | ClientError | client to server | Ack |
| `0x81` | HelloAck | server to client | |
| `0x82` | Ack | server to client | |
| `0xA0` | Pong | server to client | |

The server answers a request type it does not know with Ack `REJECTED`.

### Hello and HelloAck

```
Hello
u16 protocol_version
str addon_version
str blender_version
u32 pid
str token

HelloAck
u8  status            0 OK, 1 VERSION_MISMATCH, 2 BAD_TOKEN
u16 protocol_version  the server's version
```

- Hello is the first frame. The server closes a connection whose first frame
  is anything else, without a reply.
- The layout of `protocol_version` in Hello and of the whole HelloAck never
  changes. The server reads `protocol_version` first, and if it differs from its
  own version answers `VERSION_MISMATCH` without parsing the rest.
- The server answers `BAD_TOKEN` when the token differs from the discovery file
  or `pid` differs from the pipe's client process ID.
- After any status other than `OK` the server closes the connection.

### Scene

```
u32 scene_rev
str exporter          "xxmi", "wwmi" or "efmi"
str exporter_version  version of that exporter add-on
str game              importer key, for example "GIMI"
str source_folder     the exporter's dump or source folder
str output_folder     the exporter's mod output folder
bytes[u32] settings   exporter-specific block, opaque to the transport
Component[u16] components
str[u16] errors       the errors the exporter itself would report

Component: str name, Part[u16] parts
Part:      str name, Object[u16] objects
Object:    str object_key, str name, str collection
```

- `object_key` identifies a Blender object for the life of the connection. It
  must be nonempty and unique within the scene.
- The client sends Scene before the first mesh and whenever anything in it
  changes, with a larger `scene_rev`. Objects missing from the newest Scene are
  forgotten, together with their attribute bytes.

### Mesh transfer

```
MeshBegin
str object_key
u32 revision
u32 scene_rev         must equal the last Scene's
u32 vertex_count
u32 loop_count        triangle corners, a multiple of 3
str[u16] vertex_groups
Attr[u16] attrs

Attr
u16  id
u8   state            0 ABSENT, 1 INLINE, 2 UNCHANGED
u32  byte_len         0 when ABSENT
hash hash             zero when ABSENT

MeshChunk
u16 attr_id
u32 offset
bytes[u32] data       at most 1 MiB

MeshCommit            empty payload
```

- `INLINE` bytes follow in MeshChunk frames. The chunks of one attribute arrive
  in order: each `offset` equals the bytes of that attribute received so far.
  Chunks of different attributes may interleave.
- `UNCHANGED` refers to bytes the server already holds under `hash`. The
  server keeps, for each object in the current scene, the bytes of every
  attribute of its last committed revision. An attribute missing from `attrs` is
  absent.
- The server does not recompute hashes. The hash is a content key computed by
  the client, and the server checks only that `byte_len` matches.
- Each object's committed revision includes every attribute. The server
  delivers the whole object to the viewer, not a delta.

### Attributes

| ID | Name | Element | Count |
|---|---|---|---|
| `0x0001` | `VERT_CO` | `f32` x3 | vertex_count, required |
| `0x0002` | `LOOP_VERT` | `u32` | loop_count, required, each below vertex_count |
| `0x0003` | `LOOP_NORMAL` | `f32` x3 | loop_count |
| `0x0004` | `LOOP_TANGENT` | `f32` x3 | loop_count |
| `0x0005` | `LOOP_BTSIGN` | `f32` | loop_count |
| `0x0100 + k` | UV layer k | `f32` x2 | loop_count, k < 256 |
| `0x0200 + k` | color layer k | `f32` x4 | loop_count, k < 256; the add-on converts point colors to corners |
| `0x0300` | `VG_COUNT` | `u8` | vertex_count |
| `0x0301` | `VG_INDEX` | `u16` | sum of `VG_COUNT`, each below the number of vertex groups |
| `0x0302` | `VG_WEIGHT` | `f32` | sum of `VG_COUNT` |

- Weights are sparse: vertex `v` owns the next `VG_COUNT[v]` entries of
  `VG_INDEX` and `VG_WEIGHT`. The three weight attributes are present together
  or not at all.
- Unknown or repeated IDs reject the transfer.

### Ack

```
u8  status            0 APPLIED, 1 QUEUED, 2 UNMATCHED, 3 NEED_FULL, 4 REJECTED
u16 flags             bit 0 RESYNC_ALL, bit 1 VIEWER_ATTACHED
u16[u16] needed       attribute IDs, with NEED_FULL
str message
```

- `APPLIED`: the viewer shows the update.
- `QUEUED`: accepted and kept, but no viewer shows it yet.
- `UNMATCHED`: accepted and kept, but the object has no draw in the exported
  mod. It needs one export.
- `NEED_FULL`: an `UNCHANGED` hash in MeshBegin is unknown to the server.
  `needed` lists those attributes. The server discards the transfer; the
  client sends the object again with them inline.
- `REJECTED`: the request broke a rule above or a limit below, or the viewer
  failed to apply it. `message` says why. The server keeps the previous
  revision.
- `RESYNC_ALL`: the client sends Scene and every object again, for example
  because a viewer attached.
- `VIEWER_ATTACHED`: a viewer shows this client's scene.

Applied, queued and unmatched objects keep their attribute bytes on the server,
so later transfers can refer to them as `UNCHANGED`.

### Ping and Pong

```
Ping   empty payload
Pong   u16 flags      same bits as Ack
```

### ClientError

```
str object_key        empty for scene-wide errors
str code              for example "MISSING_UV_LAYER"
str message
```

The server shows the error to the user and answers Ack `APPLIED`.

## Limits

- An `INLINE` attribute's `byte_len` must equal its element size times its
  count. For `VG_INDEX` and `VG_WEIGHT` the count is checked when the transfer
  commits.
- The bytes a connection holds, committed attributes plus the `INLINE` bytes of
  the transfer in progress, are at most 512 MiB. MeshBegin over the limit
  rejects the transfer.

## XXMI scenes

The add-on (`addon/nahida_live_link`, `plan_xxmi.py`, `extract.py` and `textures.py`) describes a GIMI,
SRMI or ZZMI mod the way XXMI-Tools 1.8.2 exports it (`ModExporter`, `DataModelXXMI.from_obj`,
`DataExtractor` in its `migoto` package), with the options of the scene's XXMI-Tools export panel
(`scene.xxmi`). Neither a dump folder nor an export is required. Without XXMI-Tools the add-on uses
XXMI-Tools' default options: ignore hidden objects, apply modifiers, normalize weights, outline optimization
with precision 8, gate divergence 10 and no custom normals.

### Scene fields

| Field | Value |
|---|---|
| `exporter` | `"xxmi"` |
| `exporter_version` | XXMI-Tools `bl_info["version"]` joined with dots, for example `"1.8.2"`; empty when XXMI-Tools is not installed |
| `game` | `"GIMI"`, `"SRMI"` or `"ZZMI"` for the export game `GenshinImpact`, `HonkaiStarRail`, `ZenlessZoneZero`; empty when no supported game is selected |
| `source_folder` | the dump folder: `dump_path`, or its parent when `dump_path` names a file; empty without a dump path |
| `output_folder` | `destination_path`, or `<parent of the dump folder>/<mod name>Mod` when it is empty; empty without either |
| `errors` | the messages XXMI-Tools would stop the export with |

The add-on groups objects from the dump when `dump_path` is set and its `hash.json` is readable, whatever
the selected game. Otherwise it groups them per object.

#### Grouping from the dump

- The mod name is the dump folder's name. Each `hash.json` component that has a `draw_vb` and an object
  whose name starts with its full name becomes a Component named `<mod name><component_name>`. Each of its
  object classifications becomes a Part named `<component name><classification>`.
- A part's objects are, in export order: the one object whose name starts with the part name (`collection`
  is empty), then the mesh objects of the collection whose name starts with the part name (case
  insensitive) sorted by name, then those of its child collections, depth first. `collection` is the name of
  the collection the object was taken from. "Ignore hidden objects" and "Only export selected" filter the
  candidates as in XXMI-Tools.
- XXMI-Tools aborts the whole export on its first error. The add-on lists every error in `errors` and drops
  only the part (missing or ambiguous object or collection) or component (no first-part object, missing
  `3DMigoto:VBLayout` or `3DMigoto:IBFormat`) it belongs to.
- An object that is exported more than once gets `#2`, `#3`, ... appended to its later keys, and each copy is
  sent as its own object.

#### Grouping per object

- Every mesh object of the scene with at least one face, filtered by "Ignore hidden objects" and "Only export
  selected", becomes its own Component with one Part, both named after the object, holding only that object
  with an empty `collection`. Components are sorted by object name. Each object is extracted in its own
  object space.
- "Dump path not set" and "Please select a valid game before continuing." are not errors here. They are
  listed, with "No visible mesh object with faces.", only when no object qualifies. An unreadable
  `hash.json` and an unsupported game stay errors.

#### Both groupings

- `object_key` is `uid:<session_uid>` of the Blender object.
- `settings` is always present.

### Settings block

`settings` is UTF-8 JSON:

```json
{
  "game": "GenshinImpact",
  "components": [
    {
      "fullName": "FixBody",
      "hasBlend": true,
      "hasTexcoord": true,
      "layout": [{"SemanticName": "POSITION", "SemanticIndex": 0, "Format": "R32G32B32_FLOAT"}],
      "ibFormat": "DXGI_FORMAT_R32_UINT",
      "flipWinding": false,
      "flipNormal": false,
      "flipTangent": false,
      "flipBitangentSign": false,
      "mirror": false,
      "flipV": {"TEXCOORD.xy": true, "TEXCOORD1.xy": true},
      "normalizeWeights": true,
      "outlines": {"enabled": true, "precision": 8, "gateDivergence": 10.0, "customNormals": false}
    }
  ],
  "objects": {
    "uid:258": {
      "polygons": 64,
      "uv": ["TEXCOORD.xy", "TEXCOORD1.xy"],
      "color": ["COLOR"],
      "textures": [
        {"name": "FixBodyADiffuse.png", "path": "C:\\mods\\Fix\\FixBodyADiffuse.png", "role": "diffuse"},
        {"name": "FixBodyANormalMap.png", "path": "C:\\Users\\me\\AppData\\Local\\Temp\\nahida-live-link\\0f3c...e1.png", "role": "normal_map"},
        {"name": "FixBodyALightMap.png", "path": "C:\\mods\\Fix\\FixBodyALightMap.png", "role": ""}
      ]
    }
  }
}
```

- `game` is the `scene.xxmi.game` value when it is `GenshinImpact`, `HonkaiStarRail` or `ZenlessZoneZero`,
  else empty.
- `components` follows the Scene's components in order. Every value except the outline options and
  `normalizeWeights` comes from the main object of the component's first part (the object itself when
  grouping per object), as XXMI-Tools reads it:
  - `hasBlend`, `hasTexcoord`: whether `hash.json` names a `blend_vb` and a `texcoord_vb`; always true when
    grouping per object.
  - `layout`: `3DMigoto:VBLayout`, keeping `SemanticName`, `SemanticIndex` and `Format`. Grouping per
    object, it is empty when the object has no such property.
  - `ibFormat`: `3DMigoto:IBFormat`, or empty when grouping per object without it.
  - `flipWinding`, `flipNormal`, `flipTangent`, `mirror`: `3DMigoto:FlipWinding`, `3DMigoto:FlipNormal`,
    `3DMigoto:FlipTangent`, `3DMigoto:FlipMesh`, false when absent.
  - `flipBitangentSign`: `3DMigoto:Tangent`, which XXMI-Tools reads as its bitangent sign flip.
  - `flipV`: `flip_v` of `3DMigoto:<layer name>` for each UV layer of the object's own mesh that has it.
  - `normalizeWeights` and `outlines` (`enabled`, `precision`, `gateDivergence`, `customNormals`): the
    export options "Normalize weights to format", "Outline Optimization", "Outline decimal rounding
    precision", "Outline gate divergence" and "Use custom normals for outlines".
- `objects` has an entry for every `object_key` in the Scene:
  - `polygons`: the face count of the object's own mesh data. XXMI-Tools exports nothing from an object
    without faces.
  - `uv`, `color`: the names of the layers sent as attributes `0x0100 + k` and `0x0200 + k`, by `k`. Both are
    empty until the object's first mesh has been extracted.
  - `textures`: one entry per distinct image of an Image Texture node in the object's material slots,
    including node groups, in slot and node order:
    - `name`: the image datablock name.
    - `path`: an absolute, normalized path of an existing file. An external image is its file. A packed
      image is written once to `%TEMP%\nahida-live-link\<BLAKE2b-128 of its bytes, hex><extension>`, the
      extension of its file path in lowercase or `.png`. Generated images, image sequences, movies, tiled
      images and images whose file is missing are left out.
    - `role`: `"diffuse"` when a node of the image is linked to a Principled BSDF Base Color, `"normal_map"`
      when one is linked to a Normal Map node, else `""`.
- The add-on sends a new Scene whenever any of this changes, including layer names and textures. It
  replans after an object, collection, material, image or node tree update and at least once a second, which
  catches image files that appear or disappear. A packed image is rewritten only when its name, file path or
  packed size changes.

### Meshes

- The mesh is the evaluated object (the object without modifiers and shape keys when "Apply modifiers and
  shapekeys" is off), transformed into the part's main object space for collection members, and
  triangulated like XXMI-Tools' `mesh_triangulate`. Faces that are already triangles keep their order.
- In Sculpt mode an object without viewport modifiers is read from its own mesh instead of the evaluated
  one: Blender writes strokes into the original positions and does not re-evaluate the object until it leaves
  Sculpt mode.
- `VERT_CO` is `undeformed_co`. `LOOP_NORMAL` holds the corner normals.
- `LOOP_TANGENT` and `LOOP_BTSIGN` come from `calc_tangents(uvmap="TEXCOORD.xy")`, or from the first UV map
  when the mesh has no `TEXCOORD.xy`. When the mesh has no UV map they are absent and the add-on sends
  ClientError `MISSING_UV_LAYER`.
- UV layer `k` is the `k`-th entry of the mesh's UV maps. Color layer `k` is the `k`-th corner or point
  attribute of the mesh's color attributes. Byte colors are sent as stored (sRGB), the values XXMI-Tools reads
  through `mesh.vertex_colors`; point colors are copied to each corner of their vertex.
- `vertex_groups` are the names of the object's vertex groups. The weight attributes list each vertex's
  `vertex.groups` in Blender's order with unmodified weights. The desktop applies XXMI-Tools' rules itself:
  zero weight for groups whose name starts with `MASK`, sorting by weight, truncation and normalization.
- An object without triangles is sent with `vertex_count` and `loop_count` 0 and only `VERT_CO` and
  `LOOP_VERT`, both empty.
- The add-on reads weights on the first send, after a reconnect or `RESYNC_ALL`, while the object is in
  Weight Paint mode, when its topology or vertex group names change, and 2 seconds after an update that
  skipped them. Other updates repeat the previous weight bytes, which go out as `UNCHANGED`.
