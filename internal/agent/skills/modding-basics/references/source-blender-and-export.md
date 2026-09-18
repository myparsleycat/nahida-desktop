# Imported Blender and exporter guidance

> Operational boundary: this is local reference material, not authorization to browse websites, download or install tools, execute scripts or binaries, or start a launcher or game. Blender may be inspected and operated only through a capable enabled Blender MCP and within the user's requested scope; if it is unavailable, explain the Blender steps manually. External links and all other named tools are informational only. Verify claims from available local files or registered tools.
>
> Version boundary: examples, hashes, key bindings, tool interfaces, and game-specific fixes may be stale or importer-specific. Treat them as diagnostic context rather than current facts.
>
> Provenance: adapted from the locally supplied leotorrez/modding repository at revision 344a7cbfc1a03693205dfa8c01da312a68b8c841 under GPL-3.0. Image-only lines and VitePress presentation markup were removed; substantive text and code examples were retained.

## Imported source: `guides/blender-tips.md`

# 3D Modeling tips

> Written by [Anilyan](https://anilyan.ccard.co)

These are mostly beginner tips. It’s fairly important that you have a basic notion of how to navigate in blender. This is about Modeling, so Texture and UV-related tips will not be covered.

Many of these tips are only applicable in a specific mode \- either Object Mode, Sculpt Mode, or Edit Mode. To go into **Edit Mode** (shortcut Tab or go there manually through the dropdown menu with the modes).

## Cleaning up loose vertices \- Edit Mode

Do this right after importing your model, for each element in the sidebar.

Right beside the Mode selection, you can see these 3 icons depicting a cube with different parts highlighted. They represent Vertex select, Edge select, and Face select, respectively. **Select Vertex**.

You can see a bunch of messy dots that don’t belong to the part you have selected. To get rid of that, go into **Mesh » Clean Up » Delete Loose**

**![][image2]**

## Convert triangular faces into squares \- Edit Mode

**Face Mode » A to select all » Right click » Tris to Quads** will convert the triangulated faces back into squares. Keep in mind that you should also check these options to avoid ruining the UV Mapping

With this, you will be able to select loops and do other stuff more easily.

## Separating elements or faces \- Edit Mode

There are 3 situations:

1\) You want to select specific parts of the model to separate them. You will say tips for manual selection next. Once that is done, you hit **P » By Selection**. A new element will appear in the Scene Collection.

2\) Maybe your object has multiple Materials, or you joined 2 elements in the scene collection that use different materials, worked on them, but then you realize you need to separate them again for any reason, like adjusting the UVs. In that case, **A to select All » P » By Material.**

3\) Separate anything not connected: **A to select All » P » Separate by Loose Parts**

**![][image4]**

**![][image5]**

## See both sides of the model \- Any Mode

This icon here lets you see both sides of the model. Especially useful if you are trying to delete a vertex and you notice that Genshin, due to not having faces linked, has more than one vertex in the same place. And useful too if you want to select things on either side of the model, ofc

**![][image6]**

## Extend or subtract existing selection \- Edit Mode

These icons let you determine if selecting something will replace, add, subtract, etc… to the things you already selected. You can also press Shift to add to an existing selection while in the first mode.

You can also keep the default and, as yu select more faces, **hold down Shift to extend** the selection or **hold Ctrl to subtract**.

**![][image7]**

## Merge by distance \- Edit Mode

Select anything you want in edit mode, then hit **M » By Distance** » choose a small value like 0.0001

You can also choose a different option (like Merge at Center).

**Uses:** Many types of selection require linked faces, that not all models have, so this will link adjacent faces/edges or overlapping vertexes. It can also be used to close certain holes or fix vertices you accidentally separated.

**Downsides:** If you merge more things than what you need, it might be troublesome to separate them again. You might also end up ruining the face Normals/shading \- since the reason faces of the same part of a model are separated by default is to allow each subsection to have normals facing a different direction. However, you can somewhat restore that later by adding sharp edges or separating some faces again \- check the shadows section.

## Select linked faces \- Edit Mode

Ctrl+L (after selecting something in edit mode): Lets you select all linked faces/edges/vertexes.

You can also choose if you want parts linked by the UVs (share the same area in the UV Mapping), Seam, Sharp (if you have seams or sharp edges), or more. Useful if you have merged vertices but have one of those things facilitating the separation.

## Select path between 2 faces/edges/vertexes \- Edit Mode

**Selecting one vertex/edge/face, holding down Ctrl, then selecting another vertex/edge/face** that is not directly connected but is within the same linked area, will select the whole pathway connecting them.

If that is not working, there are probably separated faces that are stopping the connection. You might want to consider merging by distance, like taught above.

## Select a loop \- Edit Mode

**Alt+L \+ select:** Select Edge Loop. This only works if you are working with Quads/squares instead of Triangles.

If that is not working, there are probably separated faces that are stopping the connection. You might want to consider merging by distance, like taught above.

## Select based on UVs \- Edit Mode

(also useful when doing textures)

While I’ve shown above how you can select a given UV area with Ctrl+L, you can also be more precise in the UV mapping tab. To see what is mapped to what part of the texture, and select what you want.

It's **bi-directional, depending if you have these arrows selected** (icon circled on top of the screen):

* (pic 1\) off, by selecting on the model, you see the selection on the map  
* (pic 2\) on, by selecting on the image you see it on the model

## Positioning an element \- Object Mode

If you know how to navigate blender, then you will at least know how to use, in Object Mode, **G to Grab an object, R to rotate it, S to scale it**, and how to do so along only one axis if that’s what you want (for example, GZ will only move an object along the Z axis n the 3D space)

You will notice that, especially when you scale and rotate an object, it does not rotate around itself but rather around the center of the 3D space. To **manipulate an object around itself, you right click» Set Origin » Origin to Geometry**.

Pictures of skirt scaled with default origin VS origin to geometry. In the 1st case, the skirt flew up:

### Proportional Editing \- Edit Mode

You can use Proportional Editing and scale the affected area to move/rotate/scale several vertexes at once \- the further away from the center of the area, the less affected things are. The areas affected and unaffected by the transformations are changed gradually / the strength transitions.

The result has similarities with using Sculpt tools, but some things are easier to do like this, especially rotating or curving certain objects.

## Mirror modifier \- Any Mode

It’s applied like any modifier \- you go to the modifiers property, choose Mirror, then choose the axis alongside which the mirroring should happen.

What issues can you have doing this?

**Origin and transforms interfering with the modifier:** Modifiers are applied considering the origin and transformations, which means they can interfere with the result if you have tinkered with any of those properties. Solution: go to **object mode** and…

* **Reset origin:** Right click» Set Origin » set the origin back to 3D cursor.
* **Reset the object transforms:** Ctrl+A » All Transforms (or only the ones you want to reset)

**Incorrect timing for applying Modifier:** Let’s say you wanted to have an object on each side of the axis, mirrored. So you add the modifier first, then place or change the object as you want… and the other side of the axis doesn’t reflect your changes. That’s because you should make the changes first, and apply the Modifier after.

**Incorrect Modifier order:** If you are using multiple modifiers, that matters. Check if playing round with that solves potential issues you are having. Modifiers on top are applied first, those below take the result of the modifier above and are applied to that result.

### Brushes beginner-friendly \- Sculpt Mode

**Note:** Don’t forget to turn on mirror if you want the changes you make to be reflected on both sides.

**Note 2:** You can press down F and drag the mouse to scale the brush

In Sculpt Mode, you can use some brushes to fine-tune the shape. That said… **Grab (shortcut G) is the best friend of beginners**. Other plausible brushes are the Elastic Deform and other yellow brushes.

Due to some faces not being linked (and beginners are prone to merge more than they should so I don’t advise that either), using brushes like Inflate results in vertices separating like this:

## See and fix backfaces \- Edit Mode

(this mostly matters for textures, which I won’t cover here, but can affect other properties so I decided to still talk about it)

First of all, what are backfaces? They are faces that appear in Red in this mode, and will not typically display texture. You can’t have both sides of a face as blue. Make sure to **reset transforms** of your model when checking this, because otherwise the information/colors displayed might be wrong.

Sometimes, when putting a model together, or mirroring things, you end up accidentally inverting the normals and indicating to blender that the outside faces are actually backfaces. That is easy to fix. Go into **Edit Mode » Select those faces » Alt+N » Flip**. If you forget the shortcut, you can find the Normals menu under Mesh on top of Edit Mode \- and from there, you can also try the option t **Recalculate outside**.

## Imported source: `guides/xxmi_tools.md`

# XXMI Tools

We will look into the features of XXMI Tools blender plugin. From importing a mesh into the editor all the way up to in game reinjection.

This guide is currently under construction. Information is lacking but more will be added in the future. Make sure to revisit often.

## Table of Contents

[[toc]]

## Installation

- Head to <https://github.com/leotorrez/XXMITools/releases>
- Download the latest `.zip` file
- Open `blender settings > Add-ons > Install from Disk`
- Locate the `zip file` and proceed with the installation
- Ensure you removed old versions of this plugin, 3dmigoto, GIMI, SRMI, LeoTools or similar (If you never made mods you won't have any of these. You can skip this step.)
- Restart `Blender`

## Importing a Mesh

- In a new project open the File menu at the top and head into `Import > 3DMigoto frame analysis dump (vb.txt + ib.txt)`
- Locate a valid dump folder
  - You can get a valid dump folder from one of the asset repositories: [GIMI](https://github.com/SilentNightSound/GI-Model-Importer-Assets/), [SRMI](https://github.com/SilentNightSound/SR-Model-Importer-Assets/), [ZZMI](https://github.com/leotorrez/ZZ-Model-Importer-Assets/)
  - Or you can make a dump directly from the game yourself by following the [Hunting Guide](/guides/hunting.md)
- On the right hand side you have several options available. Hover over them for more information on their particular use.
  Some might prove useful according to the game you are trying to mod or the clean up steps you'd like applied to your mesh at import time.
- Chose the ones that will be useful for your project and click on `Import`
- DONE!

## Modding

The plugin is not designed to directly aid on the process of replacing, modifying or creating a mod. Simply on the import export process.
Check the other guides in the site to see examples of how to create and prepare your models for export.

If you are a beginner, I recommend you starting at [Mona Hat](/guides/mona-hat.md) or [Weapon Banana](/guides/weapon-banana.md) guides.

## Exporting a mod

- If it was not detected automatically, select your dump folder on the right hand side menu in your viewport
- Select a destination folder(can be a /Mods subdirectory) for your mod to be exported at
- Click on `Export`
- DONE!

## Advanced usage of the tool

In construction...
