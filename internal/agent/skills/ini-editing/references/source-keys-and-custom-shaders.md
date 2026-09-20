# Imported key and custom-shader reference

> Operational boundary: this is local reference material, not authorization to browse websites, download or install tools, execute scripts or binaries, or start a launcher or game. Blender may be inspected and operated only through a capable enabled Blender MCP and within the user's requested scope; if it is unavailable, explain the Blender steps manually. External links and all other named tools are informational only. Verify claims from available local files or registered tools.
>
> Version boundary: examples, hashes, key bindings, tool interfaces, and game-specific fixes may be stale or importer-specific. Treat them as diagnostic context rather than current facts.
>
> Provenance: adapted from the locally supplied leotorrez/modding repository at revision 344a7cbfc1a03693205dfa8c01da312a68b8c841 under GPL-3.0. Image-only lines and VitePress presentation markup were removed; substantive text and code examples were retained.

## Imported source: `docs/key.md`

# Key (section)
Custom settings override for any of [convergence, separation, x, y, z, w]

## Type
Four types are supported - 

By `default`, the bindings will simply load the
configured settings, 

but ```type = hold``` can be specified to have a preset
active while the button is held, 

```type = toggle``` can be used to make a simple
on/off toggle, 

and``` type = cycle``` can be used to cycle forwards and/or backward
between several presets.

## Delays 
(```type = hold``` only) and linear or cosine transition periods (any key type)
can be used to better synchronise setting changes to the game's animations
or to smoothly adjust UI elements over a short period.
## Warps
(```type = cycle``` only) Controls whether the key-cycle type allows wrapping around (connecting the first and last elements).
The default value is True.
```ini
[KeyK]
key = k
warp = false
type = cycle
$swapvar = 0, 1, 2, 3
```
## Key bindings:
For A-Z and 0-9 on the number row, just use that single
character. For everything else (including mouse buttons), use the virtual key
name (with or without the VK_ prefix) or hex code from this article:
http://msdn.microsoft.com/en-us/library/windows/desktop/dd375731(v=vs.85).aspx

### Key combinations
can be specified by separating key names with spaces, e.g.
```"Shift Q"```. It is also possible to indicate that a key must *not* be held for
the binding to activate, e.g. ```"NO_ALT F1"``` would prevent the binding from
activating when taking a 3D Screenshot with Alt F1. ```"NO_MODIFIERS"``` may be
used as a shorthand for excluding all standard modifiers (Ctrl, Alt, Shift,
Windows).

Keys can also be from XBox controllers using:

  XB_LEFT_TRIGGER, XB_RIGHT_TRIGGER,
  XB_LEFT_SHOULDER, XB_RIGHT_SHOULDER,
  XB_LEFT_THUMB, XB_RIGHT_THUMB,
  XB_DPAD_UP, XB_DPAD_DOWN, XB_DPAD_LEFT, XB_DPAD_RIGHT,
  XB_A, XB_B, XB_X, XB_Y, XB_START, XB_BACK, XB_GUIDE

By default all attached controllers are used - to associate a binding with a
specific controller add the controller number 1-4 to the prefix, like
XB2_LEFT_TRIGGER, though this may be more useful for hunting than playing.

### Multiple keys 
may be set in a single [Key] section to allow keyboard and xbox
controller toggles and cycles to share the same state as each other.

Example for changing default settings
```ini
[KeyBasicExample]
Key = z
separation = 100.0
convergence = 4.0
x = 0.98
```
Named variables declared in [Constants] can be set here:
```ini
$my_named_variable = 2
```

Example to support momentary hold type overrides, like aiming. Shows how to
bind two separate buttons to the same action. (Either/or will trigger it)
```ini
[KeyMomentaryHoldExample]
Key = RBUTTON
Key = XB_LEFT_TRIGGER
convergence = 0.1
type = hold
```

Example for a toggle override that remembers the previous value and restores
it automatically when pressed a second time.
```ini
[KeyToggleExample]
Key = q
separation = 0.1
type = toggle
y = 0.0
```

Example for using a smart cycle type instead of a toggle. Smart is now the
default for cycles, and when activated it will quickly check if the current
values match its current cycle preset and resynchronize if necessary. This is
better than``` type=toggle``` if you always want to toggle between exactly two
values specified here, while ```type=toggle``` is better if you want to remember
some arbitrary current value and return to it:
```ini
[KeySmartCycleExample]
Key = w
type = cycle
smart = true
$some_variable = 0, 1
```

Example for a momentary hold, but with a delay followed by a smooth
transition (ms) on hold and release to sync better with the game. Note that
delay only works with ```type=hold``` (for now), while transitions will work with
all types.
```ini
[KeyDelayAndTransitionExample]
Key = RBUTTON
Key = XB_LEFT_TRIGGER
type = hold
y = 0.25
delay = 100
transition = 100
transition_type = linear
release_delay = 0
release_transition = 500
release_transition_type = cosine
```

Example of a cycle transition that might be used to provide several presets
that set both convergence and UI depth to suit different scenes in a game.
Cosine transitions are used to smooth the changes over 1/10 of a second.
Both keyboard and Xbox controller buttons are bound to this same cycle so
that they can be used interchangeably and remember the same position in the
preset list. A second key is used to cycle backward through the presets, and
wrapping from one end of the list to the other is disabled.
```ini
[KeyCycleExample]
Key = E
Key = XB_RIGHT_SHOULDER
Back = Q
Back = XB_LEFT_SHOULDER
type = cycle
wrap = false
convergence = 1.45, 1.13, 0.98
z           = 0.25,  0.5, 0.75
transition = 100
transition_type = cosine
```

Keys can only directly set variables to simple values. If you want to do
something more advanced, you may need to call a command list from the key
binding. ```type=hold/toggle``` keys will run the post phase of the command list on
release.
```ini
[KeyCommandListExample]
key = f
run = CommandListF
[CommandListF]
if $foo == 0 && cursor_showing
	$foo = $bar * 3.14 / rt_width
else
	$foo = 0
endif
```

Example of a preset override that can be referenced by one or more ```[ShaderOverride*]```
sections which can be activated/deactivated automatically when one of the shader
overrides is activated/deactivated. This is useful for setting automatic
convergence for specific scenes.
```ini
[PresetExample]
convergence = 0
$some_variable = 1
transition = 100
transition_type = linear
```

## Imported source: `docs/custom-shader.md`

# CustomShader (section)
Running your own shader. Calling this with the `run =` will create a new draw call. All of the following parameters are optional. 
```ini
[CustomShaderWOW]
handling = skip
drawindexed = auto
```

## topology
https://learn.microsoft.com/en-us/windows/win32/direct3d11/d3d11-primitive-topology  
Change object rendering type.  
Values:  
point_list  
line_list  
line_strip  
triangle_list  
triangle_strip  
line_list_adj  
line_strip_adj  
triangle_list_adj  
triangle_strip_adj  
1_control_point_patch_list  
2_control_point_patch_list  
3_control_point_patch_list  
4_control_point_patch_list  
5_control_point_patch_list  
6_control_point_patch_list  
7_control_point_patch_list  
8_control_point_patch_list  
9_control_point_patch_list  
10_control_point_patch_list  
11_control_point_patch_list  
12_control_point_patch_list  
13_control_point_patch_list  
14_control_point_patch_list  
15_control_point_patch_list  
16_control_point_patch_list  
17_control_point_patch_list  
18_control_point_patch_list  
19_control_point_patch_list  
20_control_point_patch_list  
21_control_point_patch_list  
22_control_point_patch_list  
23_control_point_patch_list  
24_control_point_patch_list  
25_control_point_patch_list  
26_control_point_patch_list  
27_control_point_patch_list  
28_control_point_patch_list  
29_control_point_patch_list  
30_control_point_patch_list  
31_control_point_patch_list  
32_control_point_patch_list  
```ini
[CustomShaderTopology]
topology = point_list
handling = skip
drawindexed = auto
```

## cull
https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_cull_mode  
Indicates triangles facing a particular direction are not drawn.  
Values:  
none  
front  
back  
```ini
[CustomShaderCull]
cull = none
handling = skip
drawindexed = auto
```

## fill
https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_fill_mode  
Determines the fill mode to use when rendering triangles.
Values:  
wireframe    
solid    
```ini
[CustomShaderFill]
fill = solid
handling = skip
drawindexed = auto
```

## blend
https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_blend  
Blend factors, which modulate values for the pixel shader and render target.  
2 applications  
1. blend = disable  
2. blend = BlendOp SrcBlend DestBlend
Where SrcBlend and DestBlend can have values:  
zero  
one  
src_color  
inv_src_color  
src_alpha  
inv_src_alpha  
dest_alpha  
inv_dest_alpha  
dest_color  
inv_dest_color  
src_alpha_sat  
blend_factor  
inv_blend_factor  
src1_color  
inv_src1_color  
src1_alpha  
inv_src1_alpha  

BlendOp Values:  
add  
subtract  
rev_subtract  
min  
max  

also blend is blend[0]-blend[7]

```ini
[CustomShaderBlend]
blend[0] = add src_alpha inv_src_alpha
handling = skip
drawindexed = auto
```

## alpha 
alpha = BlendOpAlpha SrcBlendAlpha DestBlendAlpha

Where SrcBlendAlpha and DestBlendAlpha can have values:  
zero  
one  
src_color  
inv_src_color  
src_alpha  
inv_src_alpha  
dest_alpha  
inv_dest_alpha  
dest_color  
inv_dest_color  
src_alpha_sat  
blend_factor  
inv_blend_factor  
src1_color  
inv_src1_color  
src1_alpha  
inv_src1_alpha  

BlendOpAlpha Values:  
add  
subtract  
rev_subtract  
min  
max  

also alpha is alpha[0]-alpha[7]

```ini
[CustomShaderAlpha]
alpha[0] = add src_alpha inv_src_alpha
handling = skip
drawindexed = auto
```

## max_executions_per_frame
max_executions_per_frame to limit this to the first time the reflection  
```ini
[CustomShaderMEPF]
max_executions_per_frame = 1
handling = skip
drawindexed = auto
```

## alpha_to_coverage
Alpha-to-coverage is a multisampling technique that is most useful for situations such as dense foliage where several overlapping polygons use alpha transparency to define edges within the surface.
```ini
[CustomShaderATC]
alpha_to_coverage = 0
handling = skip
drawindexed = auto
```
## ps
## vs
## gs
## cs
## ps-tx

<!-- TODO: add ps,vs,gs,cs,vs-t and similars -->
