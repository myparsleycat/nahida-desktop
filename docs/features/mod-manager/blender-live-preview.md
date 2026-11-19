# Blender Live Preview

Blender Live Preview shows the XXMI mod being edited in Blender in the [Model Viewer](/features/mod-manager/model-viewer) as you edit it, without exporting. It supports Genshin Impact (GIMI), Honkai: Star Rail (SRMI), and Zenless Zone Zero (ZZMI) mods.

Open it by selecting `Blender Live Preview (Beta)` on the Tools screen.

::: info
Requires Blender 3.6 or later and the Nahida Live Link add-on.
:::

## Install the Add-on

The `Add-on (bundled X)` section lists the Blender 3.6 or later installations found on the PC and the add-on state of each. Click `Refresh` if the list is out of date.

- `Install`, `Update`, `Reinstall`: Installs the add-on version bundled with the app and enables it in Blender. A successful install turns Blender Live Preview on.
- `Remove`: Removes the add-on.

The add-on cannot be installed in these cases:

- That version of Blender is running: Close Blender and try again.
- Another copy of the add-on is installed elsewhere: Remove it in Blender first.
- The add-on folder is linked to another folder: Update it in the linked folder.

If the Blender executable is not found, only the add-on files are installed. Enable Nahida Live Link in Edit > Preferences > Add-ons in Blender.

## Connect

1. Turn on Blender Live Preview with the switch at the top of the screen.
2. When the Nahida Live Link add-on is enabled in Blender, Blender appears in the `Connected Blender instances` list.
3. For the connected Blender, set the following and click `Open in model viewer`.
   - `Game`: The mod's game. It is set automatically when the scene names its game.
   - `Character for game motions`: The character used for [character data](/features/mod-manager/character-data). It is preselected from the object names.

If the connected add-on is older than the version bundled with the app, a warning and an update button are shown.

## Live Updates

While the Model Viewer is open, meshes and textures edited in Blender are shown in the viewer immediately. When the structure changes, for example when an object is added, the scene is reloaded.
