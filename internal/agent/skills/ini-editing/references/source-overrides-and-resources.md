# Imported override and resource reference

> Operational boundary: this is local reference material, not authorization to browse websites, download or install tools, execute scripts or binaries, or start a launcher or game. Blender may be inspected and operated only through a capable enabled Blender MCP and within the user's requested scope; if it is unavailable, explain the Blender steps manually. External links and all other named tools are informational only. Verify claims from available local files or registered tools.
>
> Version boundary: examples, hashes, key bindings, tool interfaces, and game-specific fixes may be stale or importer-specific. Treat them as diagnostic context rather than current facts.
>
> Provenance: adapted from the locally supplied leotorrez/modding repository at revision 344a7cbfc1a03693205dfa8c01da312a68b8c841 under GPL-3.0. Image-only lines and VitePress presentation markup were removed; substantive text and code examples were retained.

## Imported source: `docs/override.md`

# Override

There are two types of overrides: `TextureOverride` and `ShaderOverride`. When there is a corresponding hashed object on the screen, it triggers the operation of the respective override section. This is the core functionality of GIMI and the starting point for all mods.

```ini
[*Override*]
[TextureOverrideLumineBody]
[ShaderOverridGroundHealthBar]
```

# Common properties
The most used ones for most mods, while not necesary to read for most mod makers is good to learn about them.
## hash

It tells GIMI which object to pay attention to and triggers the corresponding action when found.
```ini
[TextureOverrideLumineBody]
hash = afd36b46
[ShaderOverrideOutlines]
hash = afd36b46afd36b46
```

## handling

It specifies the rendering operation for the designated object, usually using `skip` to bypass rendering.
```ini
[TextureOverrideLumineDress]
handling = skip
```

## drawindexed

Tell GIMI to perform our own rendering instead of using the game's rendering. It is usually used in conjunction with [handling](#handling).
```ini
[TextureOverrideLumineBody]
drawindexed = auto
```

## draw

Assigns x vertex in memory space to be drawn at y index. DirectX Documentation: https://learn.microsoft.com/en-us/windows/win32/api/d3d11/nf-d3d11-id3d11devicecontext-draw
```ini
[TextureOverrideLumineBlend]
draw = x, y
```
## vbx
Vertex buffer. It usually points directly to another [Resource](#resource) section.
```ini
[TextureOverrideLumineBody]
vb0 = ResourceLuminePosition
```
In other games vb1, vb2, etc are used. For genshin's case it's always vb0.

## ib
Index buffer. It usually points directly to a [Resource](#resource) section that contains the IB. There can only be one index buffer per object.
```ini
[TextureOverrideLumineBody]
ib = ResourceLumineBodyIB
```

## ps-tx
Texture resource layer. There are several different types, generally, t0 represents the texture map, t1 represents the light map, t2 represents the metal map, and t3 represents the shadow map. In 3.0 characters this convetion is broken. For more info and fix check: 
```ini
[TextureOverrideLumineDress]
ps-t0 = ResourceLumineDressDiffuse
ps-t1 = ResourceLumineDressLightMap
ps-t2 = ResourceLumineDressMetalMap
ps-t3 = ResourceLumineDressShadowRamp
...

```

## allow_duplicate_hash
At ShaderOverride.
Controls whether to allow overriding the same hash or not.

Possible values are:
 - true: Overrides when duplicates exist.
 - false: Does not override when duplicates exist.
 - overrule: Forces the override, which seems to be a plugin-level override.

```ini
[ShaderOverrideLumineQEffect]
hash = 030dbce199e10697
allow_duplicate_hash = overrule
```

## match_priority
At TextureOverride.
Declares the order priority for overrides. The higher the value, the higher the priority.
It is not commonly used in GIMI, except in cases where it is used to eliminate hash conflicts. In those cases, setting it to 0 is sufficient.
```ini
[TextureOverrideLumineGlasses]
match_priority = 0
```

# Advanced properties
These are often not applied in most mods but they can be very powerful.

## filter_index

Declares a check value that allows checking elsewhere. It is likely to occupy ps-t0, so it is uncertain whether it is a good approach.
<!-- Actually it's still not very clear (for me), from someone, it seems that it can disable a specified filter. -->
```ini
[TextureOverrideLumineGlasses]
filter_index = 34
```
## analyse_options

can also be specified in `[ShaderOverride*]` sections (or other
command lists) to set up triggers to change the options mid-way through a
frame analysis, either for a single draw call (default), or permanently (by
adding the 'persist' keyword).

Alternatively, `"dump"` can be specified in a `[ShaderOverride*]` section (or
any other command list) to dump specific resources with per-resource options
```
"dump = dump_tex dds share_dupes mono ps-t0"
```
 dump resources at a
specific point in time (e.g. `"pre dump = o0"`) or dump a custom resource that
frame analysis cannot otherwise see  
```
"dump = ResourceDepthBuffer"
```
 Use additional `"dump"` commands to dump multiple resources.

## match_first_index

Specifies the starting position of the buffer. Sometimes, a hash may contain more than one material, so it is necessary to specify the correct resource to load.
```ini
[TextureOverrideLumineBody]
match_first_index = 25600
```

## match_type
At TextureOverride.
Used instead of hash. Called when any component of the selected type is rendered.
```ini
[TextureOverrideTexture2D]
match_type = Texture2D
```

## match_width
At TextureOverride.
Checks the width of the texture.
```ini
[TextureOverrideWidth1024]
match_width = 1024
```

## match_height
At TextureOverride.
Checks the height of the texture.
```ini
[TextureOverrideHeight1024]
match_height = 1024
```

## match_msaa
At TextureOverride.
Filter by MSAA (Not used in anime game).
```ini
[TextureOverrideMsaa]
match_msaa = 1
```

## match_msaa_quality
At TextureOverride.
```ini
[TextureOverrideMsaaQuality]
match_msaa_quality = 1
```

## match_usage
At TextureOverride.

This setting doesn't make much sense and defaults to DEFAULT.

More details here:
https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_usage
```ini
[TextureOverrideUsage]
match_usage = IMMUTABLE
```

## match_bind_flags
At TextureOverride.
Another filter.
You can use + or - before the flag to change the filtering.
If there is no + or - then the filter is simply not used.
```ini
[TextureOverrideAllBindFlags]
match_bind_flags = +VERTEX_BUFFER -INDEX_BUFFER CONSTANT_BUFFER SHADER_RESOURCE STREAM_OUTPUT RENDER_TARGET DEPTH_STENCIL UNORDERED_ACCESS DECODER VIDEO_ENCODER
```

## match_cpu_access_flags
At TextureOverride.
Another filter.
You can use + or - before the flag to change the filtering.
If there is no + or - then the filter is simply not used.
```ini
[TextureOverrideAllCPUAccessFlags]
match_cpu_access_flags = +READ -WRITE
```

## match_misc_flags
At TextureOverride.
Another filter.
You can use + or - before the flag to change the filtering.
If there is no + or - then the filter is simply not used.
```ini
[TextureOverrideAllMiscFlags]
match_misc_flags = GENERATE_MIPS SHARED TEXTURECUBE DRAWINDIRECT_ARGS BUFFER_ALLOW_RAW_VIEWS BUFFER_STRUCTURED RESOURCE_CLAMP SHARED_KEYEDMUTEX GDI_COMPATIBLE SHARED_NTHANDLE RESTRICTED_CONTENT RESTRICT_SHARED_RESOURCE RESTRICT_SHARED_RESOURCE_DRIVER GUARDED TILE_POOL TILED
```

## match_byte_width
At TextureOverride.
Match byte width.
```ini
[TextureOverrideByteWidth]
match_byte_width = res_width * res_height
```

## match_stride
At TextureOverride.
Something to do with buffers.
```ini
[TextureOverrideStride]
match_stride = 40
```

## match_mips
At TextureOverride.
```ini
[TextureOverrideMips]
match_mips = 1
```

## match_format
At TextureOverride.
Filter by format. Useful for modifying something that doesn't have a constant hash.
List of DX formats:
https://learn.microsoft.com/en-us/windows/win32/api/dxgiformat/ne-dxgiformat-dxgi_format
```ini
[TextureOverrideFormat]
match_format = R32G32B32A32_FLOAT
```

## match_depth
At TextureOverride.
```ini
[TextureOverrideDepth]
match_depth = 1
```

## match_array
At TextureOverride.
```ini
[TextureOverrideArray]
match_array = 12
```

## Imported source: `docs/resource.md`

# Resource

Not sure if it is one of the reserved words in GIMI, but since sections with resources are usually prefixed with `Resource` let's consider it a reserved word. This makes it easier to identify and avoids confusion with other sections that have specific purposes. These sections are typically used to store various resource locations.
```ini
[Resource*]
...

[ResourceLumineDress]
...

[ResourceMyRGBWeapon]
...
```

## type (Resource)

This is a properties under [Resource](#resource), not under [Key](#key-section).
It declares the type of the resource, which is generally used for buffer types.
```ini
[ResourceLumineDressPosition]
type = Buffer
``` 

## filename

Only appears under [Resource](#resource). It uses a relative path to point to the storage location of the resource. It is unclear whether absolute paths are supported, but using absolute paths in this context of redistributable data wouldn't make much sense.
```ini
[ResourceLumineBodyDiffuse]
filename = .\LumineParts\LumineBodyDiffuse.dds
```

## format

Used for IB (Index Buffer) resources, specifies the size of a single index value. Full list: https://learn.microsoft.com/en-us/windows/win32/api/dxgiformat/ne-dxgiformat-dxgi_format
```ini
[ResourceLumineBodyIB]
format = DXGI_FORMAT_R32_UINT
```
<!-- TODO: Lacks full list of available formats and their proper use  -->
## stride
Used for VB (Vertex Buffer) resources, specifies the byte size of a single vertex's total data.
```ini
[ResourceLumineDress]
stride = 20
```

## data
Used for character strings, as of the time of writing this guide it is unknown if it can be modified on the fly or if it is read-only. It is also unknown if it can be used for other types of data.
```ini
[ResourceLumineDress]
data = "Just a string."
```

---
