# Desktop channel protocol, version 1

The channel connects Nahida Desktop to the forked 3DMigoto DLL running inside a
game. The DLL is the server and the desktop the only client. The forked DLL
repository keeps the same file as `docs/desktop-channel.md`, and both
repositories commit the vectors in `testdata/v1`, which each side's tests encode
and decode.

## Transport

- Pipe: `\\.\pipe\nahida-migoto-<pid>`, where `<pid>` is the game process ID in
  decimal.
- The DLL creates it after the first `Present` when `[Desktop] channel = 1`, with
  `PIPE_ACCESS_DUPLEX | FILE_FLAG_OVERLAPPED`,
  `PIPE_TYPE_BYTE | PIPE_READMODE_BYTE | PIPE_REJECT_REMOTE_CLIENTS`, and one
  instance.
- The pipe's security descriptor is `D:P(A;;GRGW;;;<user SID>)S:(ML;;NW;;;ME)`.
  This lets the medium-integrity desktop connect to a game that runs elevated.
- The client checks that `GetNamedPipeServerProcessId` equals `<pid>` before it
  sends anything. If it differs, the client closes the connection.
- A second client is refused until the first one disconnects. After a
  disconnect, the server listens again.
- Replies and pick results belong to the connection that requested or triggered
  them. Results from a disconnected client are not delivered to a new client.

## Frames

All integers are little-endian. A frame is:

| Offset | Size | Field | Meaning |
|---|---|---|---|
| 0 | 4 | `length` | Bytes after this field: 8 plus the payload size |
| 4 | 2 | `type` | Message type |
| 6 | 2 | `flags` | Reserved, must be 0 |
| 8 | 4 | `request_id` | See below |
| 12 | `length - 8` | payload | Message body |

- A frame is at most 4 MiB including the length field. A receiver closes the
  connection on a larger `length`, a `length` below 8, or nonzero flags.
- `request_id` rules:
  - A client request carries a nonzero ID.
  - The reply to it carries the same ID.
  - Messages the server sends on its own carry 0.
- Receivers skip frames whose `type` they do not know, so later revisions can
  add messages without a version change.
- A payload must be consumed exactly. Missing or trailing bytes make the frame
  malformed, and the receiver closes the connection.

## Payload encoding

| Notation | Encoding |
|---|---|
| `u8` `u16` `u32` `u64` `i32` | Little-endian integers |
| `f32` | IEEE 754 binary32 |
| `bool` | `u8`, 0 or 1; any other value is malformed |
| `str` | `u16` byte length, then that many bytes of UTF-8, no terminator; invalid UTF-8 is malformed |
| `T[u8]`, `T[u16]`, `T[u32]` | Element count of the given width, then the elements |

## Messages

### Session

| Type | Name | Direction | Payload |
|---|---|---|---|
| `0x0001` | Hello | client to server | `str client`, `u16 min_version`, `u16 max_version` |
| `0x0002` | HelloReply | server to client | `u16 version`, `str dll_version`, `u32 pid`, `str exe_path`, `str importer_folder`, `u8 render_api`, `u64 capabilities` |
| `0x0003` | Ping | either | `u64 nonce` |
| `0x0004` | Pong | either | `u64 nonce`, copied from the Ping |
| `0x00FF` | Error | either | `str code`, `str detail` |

- **Handshake.** Hello is the first frame on a connection. Before it, the server
  answers any other request with Error `NOT_READY` and sends nothing on its own.
  - If the version ranges overlap, the server answers HelloReply with the highest
    common version.
  - Otherwise it answers Error `UNSUPPORTED_VERSION`, with the detail
    `<min>-<max>` of its own range, and closes the connection.
- **`render_api`:** 1 D3D11, 2 D3D12, 3 Vulkan.
- **`capabilities` bits:**
  - bit 0 `VARS`: GetVars and SetVars.
  - bit 1 `PICK_D3D11`: picking in the D3D11 path.
  - Other bits are reserved.
- **Error** answers the request with the same `request_id`. Codes used in
  version 1:
  - `UNSUPPORTED_VERSION`
  - `MALFORMED`
  - `NOT_READY` (sent before Hello)
  - `UNSUPPORTED` (capability missing)
  - `BUSY`
  - `RESPONSE_TOO_LARGE` (the variable response would exceed 4 MiB)

### INI variables

| Type | Name | Direction | Payload |
|---|---|---|---|
| `0x0010` | GetVars | client to server | `str[u16] names` |
| `0x0011` | VarsReply | server to client | `Var[u16] values` |
| `0x0012` | SetVars | client to server | `Assign[u16] vars` |
| `0x0013` | VarsApplied | server to client | `u64 frame`, `Var[u16] values` |

- `Var` is `str name`, `bool exists`, `f32 value`.
- `Assign` is `str name`, `f32 value`.
- A name is a global INI variable spelled as in an INI file, with its namespace,
  for example `$\Mods\Foo\foo.ini\enabled`.
- The server reads GetVars values on the render thread at the next `Present`.
- SetVars is applied there too. VarsApplied then reports the frame and each
  variable's resulting value. A variable that does not exist is reported with
  `exists = 0` and is not created. A variable declared `locked` is not changed
  and is reported with `exists = 1` and its current value.
- If the request or reply queue is full, the server rejects the request with
  Error `BUSY` and the same `request_id` before processing it. A rejected SetVars
  changes no variables.
- A request whose response would exceed 4 MiB is rejected with Error
  `RESPONSE_TOO_LARGE` and the same `request_id`. A rejected SetVars changes no
  variables. The client can retry with fewer variables on the same connection.

### Events

| Type | Name | Direction | Payload |
|---|---|---|---|
| `0x0020` | Event | server to client | `u16 kind`, `u64 frame` |

`kind`:
- 1 `RELOADED`: the INI files were reloaded.
- 2 `SHUTDOWN`: the DLL is unloading.

Events, VarsReply and VarsApplied arrive in the order the server produced them
at `Present`. A `RELOADED` therefore precedes the replies to requests applied
after that reload. Pong and Error can arrive before earlier replies; a client
matches replies by `request_id`.

### Picking

| Type | Name | Direction | Payload |
|---|---|---|---|
| `0x0100` | PickArm | client to server | `u8 cursor`, `f32 x`, `f32 y` |
| `0x0101` | PickArmed | server to client | `u64 capture_id` |
| `0x0102` | PickResult | server to client, `request_id` 0 | see below |
| `0x0103` | PickFailed | server to client, `request_id` 0 | `u64 capture_id`, `str code`, `str detail` |

**PickArm.**
- `cursor` 0 uses the cursor position at the next `Present`.
- `cursor` 1 uses `x` and `y`, which are normalized client coordinates of the
  game window (0,0 top left, 1,1 bottom right).
- The server answers PickArmed, or Error `BUSY` while a pick is in progress.

**Key-triggered picks.** The in-game key `[Desktop] inspect_key` starts a pick
too, with a new `capture_id`, and its result goes to the connected client.

**Capture sequence.** The capture covers the frame after the next `Present`.
The result follows once the GPU readback completes.

On D3D11, the first swap chain to present drives the channel. Other swap
chains do not advance a capture. When that swap chain is released, an active
pick fails with `NO_ELIGIBLE_DRAW` and the next swap chain to present drives
the channel. Cursor coordinates use the driving swap chain's window.

**PickFailed codes:**
- `NOT_D3D11`
- `NO_ELIGIBLE_DRAW`
- `READBACK_TIMEOUT`
- `DEVICE_LOST`

#### PickResult

```
u64 capture_id
u64 frame
u8  trigger          1 desktop, 2 key
u32 window_width
u32 window_height
f32 cursor_x         normalized client coordinates used
f32 cursor_y
u32 flags            bit 0 HITS_OVERFLOW, bit 1 DEFERRED_SKIPPED
u32 hit_total        hits the pixel shader counted, including those past capacity
Hit[u16] hits
Record[u16] records  only records referenced by a hit, ascending id
```

**Hit**

```
u32 record           Record.id
u32 primitive        SV_PrimitiveID
f32 depth            SV_Position.z
u8  flags            bit 0 DEPTH_PASSED, bit 1 FRONT_FACE
```

Each picked draw is probed twice immediately before the original draw:
1. with depth and stencil testing off, which yields hits without `DEPTH_PASSED`;
2. with the draw's original depth and stencil comparisons, stencil reference,
   and early depth testing, which yields hits with `DEPTH_PASSED`.
Both probes disable depth and stencil writes and bind no render targets.
The probes retain the game's predication. Capture buffer initialization and
readback run independently of predication.
The second probe is omitted when depth testing is disabled or its comparison
is `NEVER` or `ALWAYS`.

The highest record id with a `DEPTH_PASSED` hit identifies the last submitted
draw whose probed geometry passed these tests at the cursor. Equal-depth draws
rejected by `LESS` or `GREATER` do not receive this flag. This is a visibility
candidate: the probe replaces the pixel shader, so it does not reproduce that
shader's discard, depth output, coverage changes or color writes.

Draws inside active sample-counting or pipeline statistics queries are skipped
to preserve the game's query results. Timestamp disjoint queries do not prevent
picking. Deferred command lists are skipped, and executing one during the capture
sets `DEFERRED_SKIPPED`, including lists recorded before the capture.
Draws whose VS, HS, DS or GS uses UAVs are skipped because replaying them can
change resource data or read different inputs after the UAV bindings change.
Draws whose resource bindings cannot be read from the shader bytecode are also
skipped. If no eligible draws remain, the server sends `NO_ELIGIBLE_DRAW`.

**Record**

```
u32 id               submission order within the capture
u8  origin           1 game draw, 2 command list draw
u32 draw_number      the game draw it belongs to (3DMigoto draw_number), 0 for none
u8  call             1 Draw, 2 DrawIndexed, 3 DrawInstanced, 4 DrawIndexedInstanced,
                     5 DrawAuto, 6 DrawInstancedIndirect, 7 DrawIndexedInstancedIndirect
u32 count            vertex or index count per instance
u32 instances
u32 start            start vertex or start index
i32 base_vertex
u32 start_instance
u8  topology         D3D11_PRIMITIVE_TOPOLOGY
u8  depth_func       D3D11_COMPARISON_FUNC of the game's state, 0 when depth testing is off
bool has_ib
  Buffer ib          present when has_ib
VertexBuffer[u8] vbs
Shader[5] shaders    VS, HS, DS, GS, PS in that order, always five
Texture[u16] srvs
Target[u8] rts
bool has_ds
  Surface ds         present when has_ds
Override[u16] overrides
  Origin origin      present when origin is 2
```

- `Buffer`: `u32 hash`, `u32 orig_hash`, `bool has_region`, `u32 region_hash`,
  `u32 offset`, `u32 format_or_stride`.
  - The IB carries its DXGI format there and a vertex buffer its stride.
  - `region_hash` is the hash 3DMigoto matches `[TextureOverride]` sections
    against when it hashes a region of the buffer. `has_region` is 0 when it
    hashes the whole resource.
- `VertexBuffer`: `u8 slot`, `Buffer`.
- `Shader`: `u64 hash`, `bool replaced`.
  - `hash` is 0 when the stage has no shader.
  - `replaced` is 1 when a ShaderFixes or ShaderRegex replacement ran in place of
    the game's shader. `hash` is always the game's original shader.
- `Texture`: `u8 stage` (0 VS, 1 HS, 2 DS, 3 GS, 4 PS), `u8 slot`, `u32 hash`,
  `u32 orig_hash`, `u8 dimension` (D3D11_SRV_DIMENSION), `u32 width`,
  `u32 height`, `u32 format`.
- `Target`: `u8 slot`, `Surface`.
- `Surface`: `u32 hash`, `u32 width`, `u32 height`, `u32 format`.
- `Override`: `u8 kind` (1 TextureOverride, 2 ShaderOverride), `str section`,
  `str ini_path`.
  - The sections that matched the game draw.
  - `ini_path` is the INI file's absolute path, or empty when the DLL cannot
    recover it.
- `Origin`: `str section`, `str ini_path`, `str line`. This is the command list
  section that issued the draw, its file, and the command as written, for example
  `drawindexed = 1234, 0, 0`.

**Which state is recorded.**
- Game state (buffers, shaders, textures, targets, overrides) is read when the
  game issued its draw, before any command list rebinds it.
- A command list draw reports the game draw it ran in. Its own bindings are not
  reported.
