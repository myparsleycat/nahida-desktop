# Imported shader-override reference

> Operational boundary: this is local reference material, not authorization to browse websites, download or install tools, execute scripts or binaries, or start a launcher or game. Blender may be inspected and operated only through a capable enabled Blender MCP and within the user's requested scope; if it is unavailable, explain the Blender steps manually. External links and all other named tools are informational only. Verify claims from available local files or registered tools.
>
> Version boundary: examples, hashes, key bindings, tool interfaces, and game-specific fixes may be stale or importer-specific. Treat them as diagnostic context rather than current facts.
>
> Provenance: adapted from the locally supplied leotorrez/modding repository at revision 344a7cbfc1a03693205dfa8c01da312a68b8c841 under GPL-3.0. Image-only lines and VitePress presentation markup were removed; substantive text and code examples were retained.

## Imported source: `docs/shader-override.md`

<!-- -------------------------------------------------------------------------------------------------
 On the fly regular expression shader patching engine
------------------------------------------------------------------------------------------------------

 These sections define regular expressions to match against shaders and apply
 certain classes of fixes on the fly. Only assembly shaders are supported by
 this method for reliability and performance reasons.

 Every pattern must have a main section prefixed with ShaderRegex:

[ShaderRegex1]
 shader_model is required and must be set to the types of shaders that this
 pattern will be applied against. Multiple shader models can be specified to
 match the pattern against multiple types. There are some differences in
 instructions between shader model 4 and 5 (such as in resource load
 instructions), so in some cases you may need separate patterns for each.
shader_model = ps_4_0 ps_5_0

 temps is used to give names to temporary registers that you will use in the
 pattern. 3DMigoto will identify free register numbers and automatically
 adjust dcl_temps as required.
temps = stereo tmp1

 This main section also acts as a command list so that you can define actions
 that will be applied on every matching shader, just as you would on any other
 command list enabled section, such as ShaderOverride, Present, etc.

 The next section of interest is the regular expression pattern. If this
 section is omitted than every shader of with matching shader_model will be
 matched (and have the command lists and InsertDeclarations processed). The
 first part of the section name must match the main ShaderRegex section you
 defined above, and it ends with ".Pattern". The regular expression grammar
 that we support is PCRE2, which is largely compatible with the powerful Perl
 and Python grammars. You can find the syntax reference here, but generally
 speaking any regular expression tutorial will give you a good primer:

   http://www.pcre.org/current/doc/html/pcre2syntax.html

 Note that since this is parsed in an ini file that blank lines and ini
 comments are ignored, and preceding and trailing whitespace from each line
 will be stripped, so if you need to match an indented line you will need to
 explicitly match the whitespace at the start of the line with \s*
 You should also use \n to match the newline character at the end of each
 line. This should not be confused with extended mode activated by the (?x)
 switch, which will ignore *all* whitespace to allow complex patterns to be
 broken up for clarity.

 Multiline matching is enabled by default, as is case insensitivity (due to
 differences in the capitalisation produced by different versions of the
 disassembler), but PCRE2 provides switches for most of these options if you
 need something else.

 This is an example of how you might match a matrix multiply in a shader, and
 uses Python style named capture groups to pull out the registers and swizzles
 of the X and Z coordinates, and another named capture group to verify that
 the register used in the div instruction matches the one used in the multiply:

[ShaderRegex1.Pattern]
mul r\d+\.xyzw, r\d+\.yyyy, cb0\[28\]\.xyzw\n
mad r\d+\.xyzw, (?P<pos_x>r\d+)\.(?P<swizzle_x>[xyzw])[xyzw]{3}, cb0\[27\]\.xyzw, r\d+\.xyzw\n
mad r\d+\.xyzw, (?P<pos_z>r\d+)\.(?P<swizzle_z>[xyzw])[xyzw]{3}, cb0\[29\]\.xyzw, r\d+\.xyzw\n
add (?P<result>r\d+)\.xyzw, r\d+\.xyzw, cb0\[30\]\.xyzw\n
div r\d+\.[xyzw]{2}, (?P=result)\.[xyzw]{4}, r\d+\.wwww\n

 The next section specifies how to modify the matched pattern. Again the name
 must begin with the same name as the main section, and end in
 ".Pattern.Replace" (there is a reason the replacement is associated with the
 pattern, but that's coming soon). You can (and I highly encourage that you
 do) use named capture groups in the above pattern and substitute them in
 here. Temporary registers that you defined in the main ShaderRegex section
 are also available here with the same syntax as named capture groups. Use
 ${0} to indicate where the matched pattern goes, allowing you to insert code
 before and/or after it, or use additonal capture groups to insert code in the
 middle. Extended substitution is enabled in PCRE2, which among other things
 makes \n insert a newline.

[ShaderRegex1.Pattern.Replace]
\n
// UE4 shadow correction:\n
ld_indexable(texture2d)(float,float,float,float) ${stereo}.xyzw, l(0, 0, 0, 0), t125.xyzw\n
add ${tmp1}.x, ${pos_z}.${swizzle_z}, -${stereo}.y\n
mad ${pos_x}.${swizzle_x}, -${tmp1}.x, ${stereo}.x, ${pos_x}.${swizzle_x}\n
\n
${0}

 The final section allows you to insert new declarations into the shader, and
 3DMigoto will check that this declaration has not already been inserted
 first. Typically this is used to get access to StereoParams in t125:

[ShaderRegex1.InsertDeclarations]
dcl_resource_texture2d (float,float,float,float) t125

------------------------------------------------------------------------------------------------------
 texture / render target manipulations
------------------------------------------------------------------------------------------------------

 NOTE: If you are trying to match a texture the same size as the resolution (or
 a /2, x2, x4 or x8 multiple), you should confirm that the same hash is used
 on different resolutions, and adjust get_resolution_from if necessary.

 NOTE: If you find a texture hash seems to change inconsistently, try enabling
 track_texture_updates in the [Rendering] section.

[TextureOverride1]
Hash=c3e55ebd
 NVidia stores surface creation mode heuristics in the game profile. setting
 this option overrides the creation mode for a given texture / buffer.
 0 = NVAPI_STEREO_SURFACECREATEMODE_AUTO - use driver registry profile settings.
 1 = NVAPI_STEREO_SURFACECREATEMODE_FORCESTEREO - create stereo surface.
 2 = NVAPI_STEREO_SURFACECREATEMODE_FORCEMONO - create mono surface.
StereoMode=2

[TextureOverride2]
Hash = e27b9d07
 Prevent the game reading from this texture - will give the game a blank
 buffer instead. Used to prevent CryEngine games falsely culling objects. Use
 the frame analysis log and look for MapType:1 to identify possible hashes.
deny_cpu_read=1
 Expand the region copied to this texture with CopySubresourceRegion (similar
 issue to rasterizer_disable_scissor). Used to solve issues with transparent
 refraction effects (like glass) in CryEngine games.
expand_region_copy=1

[TextureOverrideUAVNotRT]
 Example of fuzzy matching based on attributes instead of hash. Provides an
 alternative to driver heuristics that we have more precise control over.
match_type = Texture2D
match_width = height * 16 / 9
match_height = !res_height
match_msaa = >1
match_bind_flags = +unordered_access -render_target
match_priority = -1
StereoMode = 2

------------------------------------------------------------------------------------------------------
 Example of settings override by mouse button configuration
 Mapping of from game provided hard coded convergence values to custom values
 Those are values for L.A. Noir
 Example of settings override by mouse button configuration
------------------------------------------------------------------------------------------------------
[ConvergenceMap]

Map1=from 3e99999a to 0.3
Map2=from 3f800000 to 1.0
Map3=from 3f666666 to 0.9
-------------------------------------------------------------------------------------------------
On the fly regular expression shader patching engine
------------------------------------------------------------------------------------------------------

These sections define regular expressions to match against shaders and apply
certain classes of fixes on the fly. Only assembly shaders are supported by
this method for reliability and performance reasons.

Every pattern must have a main section prefixed with ShaderRegex:

[ShaderRegex1]
shader_model is required and must be set to the types of shaders that this
pattern will be applied against. Multiple shader models can be specified to
match the pattern against multiple types. There are some differences in
instructions between shader model 4 and 5 (such as in resource load
instructions), so in some cases you may need separate patterns for each.
shader_model = ps_4_0 ps_5_0

temps is used to give names to temporary registers that you will use in the
pattern. 3DMigoto will identify free register numbers and automatically
adjust dcl_temps as required.
temps = stereo tmp1

This main section also acts as a command list so that you can define actions
that will be applied on every matching shader, just as you would on any other
command list enabled section, such as ShaderOverride, Present, etc.

The next section of interest is the regular expression pattern. If this
section is omitted than every shader of with matching shader_model will be
matched (and have the command lists and InsertDeclarations processed). The
first part of the section name must match the main ShaderRegex section you
defined above, and it ends with ".Pattern". The regular expression grammar
that we support is PCRE2, which is largely compatible with the powerful Perl
and Python grammars. You can find the syntax reference here, but generally
speaking any regular expression tutorial will give you a good primer:

  http://www.pcre.org/current/doc/html/pcre2syntax.html

Note that since this is parsed in an ini file that blank lines and ini
comments are ignored, and preceding and trailing whitespace from each line
will be stripped, so if you need to match an indented line you will need to
explicitly match the whitespace at the start of the line with \s*
You should also use \n to match the newline character at the end of each
line. This should not be confused with extended mode activated by the (?x)
switch, which will ignore *all* whitespace to allow complex patterns to be
broken up for clarity.

Multiline matching is enabled by default, as is case insensitivity (due to
differences in the capitalisation produced by different versions of the
disassembler), but PCRE2 provides switches for most of these options if you
need something else.

This is an example of how you might match a matrix multiply in a shader, and
uses Python style named capture groups to pull out the registers and swizzles
of the X and Z coordinates, and another named capture group to verify that
the register used in the div instruction matches the one used in the multiply:

[ShaderRegex1.Pattern]
mul r\d+\.xyzw, r\d+\.yyyy, cb0\[28\]\.xyzw\n
mad r\d+\.xyzw, (?P<pos_x>r\d+)\.(?P<swizzle_x>[xyzw])[xyzw]{3}, cb0\[27\]\.xyzw, r\d+\.xyzw\n
mad r\d+\.xyzw, (?P<pos_z>r\d+)\.(?P<swizzle_z>[xyzw])[xyzw]{3}, cb0\[29\]\.xyzw, r\d+\.xyzw\n
add (?P<result>r\d+)\.xyzw, r\d+\.xyzw, cb0\[30\]\.xyzw\n
div r\d+\.[xyzw]{2}, (?P=result)\.[xyzw]{4}, r\d+\.wwww\n

The next section specifies how to modify the matched pattern. Again the name
must begin with the same name as the main section, and end in
".Pattern.Replace" (there is a reason the replacement is associated with the
pattern, but that's coming soon). You can (and I highly encourage that you
do) use named capture groups in the above pattern and substitute them in
here. Temporary registers that you defined in the main ShaderRegex section
are also available here with the same syntax as named capture groups. Use
${0} to indicate where the matched pattern goes, allowing you to insert code
before and/or after it, or use additonal capture groups to insert code in the
middle. Extended substitution is enabled in PCRE2, which among other things
makes \n insert a newline.

[ShaderRegex1.Pattern.Replace]
\n
// UE4 shadow correction:\n
ld_indexable(texture2d)(float,float,float,float) ${stereo}.xyzw, l(0, 0, 0, 0), t125.xyzw\n
add ${tmp1}.x, ${pos_z}.${swizzle_z}, -${stereo}.y\n
mad ${pos_x}.${swizzle_x}, -${tmp1}.x, ${stereo}.x, ${pos_x}.${swizzle_x}\n
\n
${0}

The final section allows you to insert new declarations into the shader, and
3DMigoto will check that this declaration has not already been inserted
first. Typically this is used to get access to StereoParams in t125:

[ShaderRegex1.InsertDeclarations]
dcl_resource_texture2d (float,float,float,float) t125

------------------------------------------------------------------------------------------------------
texture / render target manipulations
------------------------------------------------------------------------------------------------------

NOTE: If you are trying to match a texture the same size as the resolution (or
a /2, x2, x4 or x8 multiple), you should confirm that the same hash is used
on different resolutions, and adjust get_resolution_from if necessary.

NOTE: If you find a texture hash seems to change inconsistently, try enabling
track_texture_updates in the [Rendering] section.

[TextureOverride1]
Hash=c3e55ebd
NVidia stores surface creation mode heuristics in the game profile. setting
this option overrides the creation mode for a given texture / buffer.
0 = NVAPI_STEREO_SURFACECREATEMODE_AUTO - use driver registry profile settings.
1 = NVAPI_STEREO_SURFACECREATEMODE_FORCESTEREO - create stereo surface.
2 = NVAPI_STEREO_SURFACECREATEMODE_FORCEMONO - create mono surface.
StereoMode=2

[TextureOverride2]
Hash = e27b9d07
Prevent the game reading from this texture - will give the game a blank
buffer instead. Used to prevent CryEngine games falsely culling objects. Use
the frame analysis log and look for MapType:1 to identify possible hashes.
deny_cpu_read=1
Expand the region copied to this texture with CopySubresourceRegion (similar
issue to rasterizer_disable_scissor). Used to solve issues with transparent
refraction effects (like glass) in CryEngine games.
expand_region_copy=1

[TextureOverrideUAVNotRT]
Example of fuzzy matching based on attributes instead of hash. Provides an
alternative to driver heuristics that we have more precise control over.
match_type = Texture2D
match_width = height * 16 / 9
match_height = !res_height
match_msaa = >1
match_bind_flags = +unordered_access -render_target
match_priority = -1
StereoMode = 2

------------------------------------------------------------------------------------------------------
Example of settings override by mouse button configuration
Mapping of from game provided hard coded convergence values to custom values
Those are values for L.A. Noir
Example of settings override by mouse button configuration
------------------------------------------------------------------------------------------------------
[ConvergenceMap]

Map1=from 3e99999a to 0.3
Map2=from 3f800000 to 1.0
Map3=from 3f666666 to 0.9

------------------------------------------------------------------------------------------------------
Commands to run from the Present call at the start/end of each frame

Useful to clear custom resources or ini params at the start of each frame, or
to run a custom shader to do whatever you can dream up. The post keyword will
make an action run at the start of a frame instead of the end - as general
guideline you want overlays drawn at the end of a frame and resources cleared
at the start of a new frame.
------------------------------------------------------------------------------------------------------
[Present]
Example: Clear an ini param at the start of each frame:
post x = 0
Example: Undefine a custom resource until something is copied into it:
post ResourceDepthBuffer = null
Example: Clear a custom resource with black/zero at the start of each frame
(beware that driver bugs may mean only one eye is cleared in some cases):
post clear = ResourceFoo -->
