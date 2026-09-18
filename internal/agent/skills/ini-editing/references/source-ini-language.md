# Imported 3DMigoto INI language reference

> Operational boundary: this is local reference material, not authorization to browse websites, download or install tools, execute scripts or binaries, or start a launcher or game. Blender may be inspected and operated only through a capable enabled Blender MCP and within the user's requested scope; if it is unavailable, explain the Blender steps manually. External links and all other named tools are informational only. Verify claims from available local files or registered tools.
>
> Version boundary: examples, hashes, key bindings, tool interfaces, and game-specific fixes may be stale or importer-specific. Treat them as diagnostic context rather than current facts.
>
> Provenance: adapted from the locally supplied leotorrez/modding repository at revision 344a7cbfc1a03693205dfa8c01da312a68b8c841 under GPL-3.0. Image-only lines and VitePress presentation markup were removed; substantive text and code examples were retained.

## Imported source: `docs/index.md`

# 3dmigoto INI File Documentation

## Preface

This is a XXMI-based ini file documentation. This is the first version, so only the most basic syntax is covered. Some programming language knowledge may be required. But don't worry, we will update it in the future, the ultimate goal is to make `ini` easy to understand.

This wiki was originally written in chinese; Later it has been translated and revised by a non-english native and then reworked by a non-english person, so some translation errors may have occured.

## Notice

This documentation was written specifically for GIMI. Some syntax may not apply to 3DMigoto or it's forks. Most syntax won't apply in standard `.ini` files.

It is recommended to turn on file extensions in your explorer view and use the [development version of GIMI](https://github.com/SilentNightSound/GI-Model-Importer/releases) (the same version as the playable GIMI plus overlay errors) because it is useful for troubleshooting when developing `.ini` files.

---

## .ini Structure introduction

Since `ini` is not the focus of this article, and the relevant `ini` syntax can be found on the [Internet](https://en.wikipedia.org/wiki/INI_file), we will only explain how to read `ini` in short.
The following is an example, which is from a very common mod syntax.

```ini
;Constants -------------------------------
[Constants]
global value = 1
...

;Overrides -------------------------------
[TextureOverrideExampleA]
hash = abcd1234
match_first_index = 0
ib = ResourceExampleAIB
ps-t0 = ResourceExampleADiffuse
...
;Resources -------------------------------
[ResourceExampleAIB]
type = Buffer
format = DXGI_FORMAT_R32_UNIT
filename = IB.ib

[ResourceExampleADiffuse]
filename = ExampleADiffuse.dds
...

```

This can simply be divided into three parts: sections, properties, and comments.
Sections and properties are case-insensitive, but in GIMI, sections are written in uppercase camel case.

## Section

```ini
[TextureOverrideExampleA]
[ShaderOverrideExampleB]
[ResourceExampleC]
[CommandListExampleD]
...
```

The part enclosed in [] is the beginning of a **section** as well as it's identifier. A section represents a code block, and its scope includes the current line, up to the line before the next section or the end of the file. All other types need to be within a section, with the exception of [namespace](/docs/namespace.md). Lastly, it's worth mentioning that INI files are case-insensitive, but in GIMI section names follow the PascalCase convention.

## Properties

```ini
...
exampleConfiguration = 1
run = CommandListExampleA
$exampleVariable = 1000
...
```

Properties are sub-items of a section, they are often used to assign values or to excecute functions within the scope of that section. They can be Parameters or Variables, more information about them can be found in their respective sections.

## Comments

Also referred to as annotations, comments start with `;` and continue until the end of the line. Here's one thing to note: in INI files, comments can only occupy a separate line. In other words, placing a semicolon after a property or section is not allowed.

```ini
; Commenting a whole line separately is allowed
[TextureOverrideA] ; Commenting after a section or property is not allowed
hash = abcd1234 ; Note that commenting in the wrong place will result in correct syntax highlighting in some software but will cause compilation issues regardless
```

That's the basic introduction to INI files. As long as you know how to distinguish between sections and properties, and how to write comments, you should be good.

## Reserved words

> There are some words that shouldn't be used as variables because they could overwrite some system-defined values.

[time]

[if, endif, else if, else]

[run]

[x123, y123, z123, w123] (These numbers can vary, they are just examples.)

There is a lot more reserved words, but they are not listed here because they are not commonly used in mods files.
<!-- TODO: add more detail about how 3dm properties can be modified from within mods files. which are likley to be mistakenly used as variables. -->
---

First timers should take a look into syntax to comprehend better how to write `.ini` files and their capabilities. More experienced users will have a better time reading specific sections of this wiki, please use the navigation tree on the side to find the section you are looking for.

## Imported source: `docs/namespace.md`

# Namespace
A namespace is useful to access and modify the variables and CommandLists of a specific mod from another mod, without having to worry about the folder structure. This functionality allows for mods to interact without having to force the user to follow a specific folder structure. By default a namespace will be thier folder address.

It's use is rather advanced but very powerful.
## Definition

```ini
 namespace = example\address\to\your\namespace 
 ```
It must go on the first line of your ini file. It's recommended but not obligatory to make your namespaces unique. Either by including the creator name, character and/or mod name in it. Having a unique namespace will prevent conflicts with other mods.

```ini
 namespace = LeoTorreZ\Mona\FontaineDressMona 
 ```
## Usage
Variables, CommandLists, Resources and such are all called in a similar syntax:

```ini
[type]\[namespace]\[variable name]
```

```ini
; main.ini
$\namespace\variable = 0
run = CommandList\namespace\name
this = Resource\namespace\Texture

; namespace.ini
[Constants]
$\namespace\variable = 1
...

[CommandListname]
...

[ResourceTexture]
filename = .\example.dds
...
```
## Example
You can have a main.ini that contains your mod definitions and you call a different mod to track if your character is swimming. The namespace of tracking.ini is what you use to access those values.

```ini
;/Mods/main.ini
...
[Present]
$swapvar = $\global\tracking\isSwimming
...
```

```ini
namespace = global\tracking
;/Mods/BufferValues/tracking.ini

[Constants]
global $isSwimming = 0
...

[Present]
$isSwimming = 1
;in this section you'd develop your logic to properly track if the character is swimming or not. In this example we just set it to 1 for simplicity.
...
```

## Imported source: `docs/command-list.md`

# CommandList

CommandLists are akin to functions in your averge program language. You can call them from within overrides, present and other commandlists. When called they can create a new draw call. In order too verify if this is the case, the best is to make a framedump analysis and check the render targets. If you see a new render target, then you know that a new draw call was created.

## Definition:
```ini
[CommandList*]
...

[CommandListToggleLogic]
...

[CommandListFixReflection]
...
```

## Usage:
```ini
[Present]
run = CommandListExample
post run = CommandListExample2

...

[CommandListNesting]
run = CommandListExample3

...
```
Since the `CommandList` section is all about advanced operations, there are no fixed properties. The only elements that might be repeated are various [Variables](#variable) and [Conditions](#condition).

---

## Imported source: `docs/modifiers.md`

# Modifiers
 Modifiers are keywords that can be used to modify the behavior of a property. They are placed at the beginning of a property and are separated from the property name by a space. The following modifiers are available:

---

## post

Specifies that the corresponding parameter is computed at the ***beginning of a frame***, such as setting the start time of a frame.
```ini
post $triggerDate = time
```

## pre

Specifies that the corresponding parameter is computed at the ***end of a frame***, such as calculating the number of times [Present](#present) has been executed.
```ini
pre $auxTime = $auxTime + 1
```

## ref

`ref` or `reference` are used as pointers to a resource. It's up to the programmer to take advantage of this powerful tool. Advanced discussion of this topic can be found at: https://github.com/bo3b/3Dmigoto/wiki/Resource-Copying
```ini
pre ResourceHelp = ref ResourceHelpFull
pre ResourceHelp = reference ResourceHelpFull
```
## copy

It copies the resource into the new one. Very helpful to keep a copy of a resource before it gets modified by another shader or draw call.
```ini
pre ResourceHelp = copy ResourceHelpFull
```

## run

Declares the section to be executed.
Commonly used to refer to a [CommandList](#commandlist) for further computations.
```ini
[KeyChangeColor]
run = CommandListLumineChangeDressColor
```

## Imported source: `docs/constants.md`

## Constants
Declare named global variables here to use them from other command lists,
[Key] bindings and [Preset]s. Named variables are namespaced so that any
included ini files can use their own without worrying about name clashes:

```global $my_named_variable = 0.0```

Mark a variable as persist[ent] to automatically save it to the
d3dx_user.ini on exit or F10 (config_reload). 
Use Ctrl+Alt+F10
(wipe_user_config) to discard persistent values:

```global persist $some_persistent_variable = 1```

Set the initial value of "IniParams" variables, which are accessible from
within shaders, but they are not namespaced and too many can become unwieldy:
```x = 0.8
y = 1.0
z = 1.2
w = 2.0
y1 = 3
```

For more information about variables, please refer to the [Variables section](#variable).

---

### global

The necessary modifier when declaring a **global** variable. [Variable rules can be found here](#variable).
Also, note that global variables are only declared within the [Constants](#constants) section.

```ini
[Constants]
global $a_global_var = 1
```

### local

The necessary modifier when declaring a **local** variable. [Variable rules can be found here](#variable).
Local variables can be declared anywhere needed for calculations. However, it is uncertain how GIMI handles the recycling mechanism for local variables. At least for now, local variables are not seen frequently.

```ini
[AnySection]
local $i = 0
```

### persist

The necessary modifier when declaring a **persistent** variable. [Variable rules can be found here](#variable).
This modifier is only used for global variables. Once declared, the variable will persist(throughout gameplay sessions) and be stored in `d3dx_user.ini`. It will only be reset when you use `Ctrl + Alt + F10`.
```ini
[Constants]
global persist $a_persist_var = 1
```

## Imported source: `docs/properties.md`

# Variable

In GIMI, variables are identified by starting with the `$` symbol.
And if there is no `$` symbol in some positions that should be variables, that is a parameter.

```ini
$last_time = time
; $last_time is our defined variable, while time is a reserved word in GIMI.
```

# Parameters

These are 3Dmigoto configuration values and don't start with $ before their name. Most of them can be found in `d3dx.ini` in the base installation directory. Some of them need to be defined within specific sections. For the use of GIMI modding we don't often use these parameters, but they are still useful for some special cases.

## time

`time` is a reserved word in GIMI. It saves a float with the time in seconds since the game launched. It can be used to measure time delays since the last occurence of an action (more creative uses of the parameter are possible as well).

For example we can track the last time the user pressed the key E

```ini
[KeyDetection]
key = e
type = hold
run = CommandListKey

[CommandListKey]
$last_time = time

...

if time - $last_time > 10
    ; do something only after 10 seconds have passed since last E press
endif

...
```

## iniParams
Another particular case is `iniParams`, an array of values that can be defined in an ini file and later on called from within a shader. Useful to comunicate the two. These values are shared across diferent ini files, so their use must be as concise as possible to avoid conflicts with other mods.

It's definition is as follows:

```ini
x123 = 0.8
y123 = 1.0
z123 = 1.2
w123 = 2.0

x1 = 3

...
```

and it's later usage in the shader is as follows:

```hlsl
// 3Dmigoto declarations
#define cmp -
Texture1D<float4> IniParams : register(t120);
Texture2D<float4> StereoParams : register(t125);
#define OFFSET IniParams[123].x
#define SCALE IniParams[123].y
#define CONVERGENCE IniParams[123].z
#define SEPARATION IniParams[123].w
#define Y1 IniParams[1].x

... 

r1.x = dot(r1.xyz, r2.xyz);
r1.y = dot(v0.xyz, r2.xyz);
r1.y=r1.y + OFFSET;

...
```

## Imported source: `docs/operators.md`

# Arimethical Operators

This is just a list of operators allowed in GIMI and does not include any usage tutorials.

| Operators | Name           | Note                            |
| --------- | -------------- | ------------------------------- |
| +         | Addition       |                                 |
| -         | Subtraction    |                                 |
| *         | Multiplication |                                 |
| /         | Division       |                                 |
| //        | Division Floor |                                 |
| %         | Modulus        |                                 |
| =         | Assignment     |                                 |
| ==        | Equal          |                                 |
| !=        | Not equal      |                                 |
| !==       | Not equal      | Similar to `!=` but more strict |

# Logical Operators

This is just a list of operators allowed in GIMI and does not include any usage tutorials.

| Operators | Name           | Note                      |
| --------- | -------------- | ------------------------- |
| &&        | AND            |                           |
| \|\|      | OR             |                           |
| ( )       | Parenthesis    |                           |

# Condition

GIMI has control structures as reserved words, including `if`, `else if`, `else`, and `endif`.
The condition block starts with `if` and ends with `endif`. Nesting is supported.
If you are new to programming, it is recommended to familiarize yourself with condition control syntax in other programming languages. This explanation will not delve into it extensively. 

```ini
if time == $lest_date + 10.0
    run = CommandListA
else if time == $lest_date + 15.0
    run = CommandListB
else
    run = CommandListC
endif
```

It's worth mentioning other control structures such as for, while, and switch are not supported in GIMI at the time of writing this document.

## Imported source: `docs/present.md`

# Present

This section is executed at the beginning of each frame and is intended for code that needs to be continuously executed. It is commonly used for real-time calculations, such as switching appearances based on key inputs or various interactive effects.
```ini
[Present]
post $active = 0
```
Similar to [CommandList](#commandlist), it is an area for various operations. so it does not have fixed properties. However, it is generally not directly linked to [Resource](#resource) sections. Instead, it is typically linked to a CommandList, which then calls the Resource.
