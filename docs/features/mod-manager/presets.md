# Presets

The Mod Manager has two kinds of presets.

- **Presets**: Save which mods are enabled for a game.
- **Toggle presets**: Save the toggle values of one mod.

## Presets

The preset selector is next to the game selector at the bottom of the sidebar.

- **Save**: Click the `+` button next to the preset selector (`Create New Preset`) and enter a name and an optional `Description (optional)`. The mods that are currently enabled are saved.
- **Apply or delete**: Select a preset in the preset selector to show its name and description in a dialog, then select `Apply` or `Delete`.

If some mods cannot be found or changed while a preset is applied, the count is shown in a notification.

Folders whose names differ only by the `DISABLED` prefix are treated as the same mod. If such folders exist when you save a preset, the `Duplicate mod entries found` dialog is shown. `Rename disabled duplicates and continue` renames the disabled folders and then saves the preset.

Presets created in older versions are marked `Legacy` and cannot be applied. Delete and recreate them.

## Toggle Presets {#toggle-presets}

A toggle preset saves the toggle values of one mod under a name and applies them again later.

### Open Toggle Presets

- Right-click a mod and select `Toggle presets`.
- In grid view, click the toggle preset button at the top of the card's INI list. It lists the saved presets and `Manage presets…`.
- In the [Model Viewer](/features/mod-manager/model-viewer), select `Save as preset` or `Apply preset` in the `Toggle` menu.

Toggle presets are available only for mods with INI files, and not for NTE mods.

### Storage

The first time you use toggle presets, choose the `Toggle preset storage`.

- `Mod folder (nhd.json)`: Stored in `nhd.json` in the mod folder. Presets stay with the folder when it is renamed or moved, and are shared along with it.
- `App database`: Stored in the app database. Mod folders are not changed, but presets cannot be found after the mod folder is renamed or moved.

Change the storage with `Storage` in the toggle presets dialog or with `Toggle preset storage` in [Mod Settings](/features/mod-manager/settings). Changing it moves the existing presets to the new location.

### Save and Apply

In the toggle presets dialog, enter a preset name and select `Save current values`. Apply, rename, or delete saved presets from the list.

- **Where values are read from**: When the game is running and connected to the app, values are read from the running game. Otherwise, they are read from the mod's INI files and `d3dx_user.ini`.
- **Applying**: Writes the preset's values to the mod's INI files. When the game is running and connected to the app, the values are also applied to the running game.

::: warning
Saving and applying toggle presets requires [XXMI](/others/set-up-xxmi) to launch the game with the built-in launcher and the myparsleycat XXMI libraries. Otherwise, `Requires the built-in launcher and the myparsleycat libraries` is shown and saving and applying are disabled.
:::
