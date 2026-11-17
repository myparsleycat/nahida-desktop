# Mod Manager

The Mod Manager enables and disables mods, organizes them into groups, and manages previews and presets for each registered game.

The page is divided into the sidebar on the left and the mod list on the right. The game and preset selectors are at the bottom of the sidebar.

## Add a Game

Click the `+` button next to the game selector to open the Add Game dialog.

- **Game Name**: The name shown in the game selector.
- **Importer**: Selects how the game loads mods.
  - An XXMI importer (`GIMI`, `SRMI`, `WWMI`, `ZZMI`, `HIMI`, `EFMI`): The importer's Mods folder is used as the mod folder. Importers are listed after [XXMI is set up](/others/set-up-xxmi).
  - `NTE`: Select the Neverness to Everness install folder. You can also choose a custom mod folder, which the game's Mods folder is linked to. Nahida Desktop installs the files the game needs to load mods.
  - `None`: Select any folder as the mod folder. The game cannot be launched from Nahida Desktop.

To change a game later, open the game selector and click the pencil button next to the game. The Edit Game dialog also changes the game's display order and deletes the game.

## Folder Structure

The following is the recommended mod folder structure.

```txt
Character
├─ Aino
│  ├─ AinoMod1
│  └─ AinoMod2
└─ Amber
   ├─ AmberMod1
   └─ AmberMod2
Enemy
└─ EnemyMod1
NPC
└─ Katheryne
   └─ KatheryneMod1
```

::: info
The sidebar shows the folders directly under the mod folder. Place mods in category folders such as `Character`, `Enemy`, or `NPC` rather than directly in the mod folder.

- Recommended: `Mods\Character\SomeCharacterMod`
- Not recommended: `Mods\SomeCharacterMod`
:::

### Subgroups

A folder in the sidebar can show its subfolders below it. Right-click the folder in the sidebar and select `Expand subgroups` to show them, or `Persist subgroups` to keep them shown. Clicking a folder whose subfolders are shown or persisted collapses or expands it; clicking any other folder selects it.

While holding `Ctrl`, clicking a folder expands and selects it. While holding `Alt`, clicking a folder collapses or expands its parent.

A folder in the mod list can also be treated as a subgroup: right-click it and select `Mark as subgroup`. `Unmark manual subgroup` in the sidebar folder's menu undoes it.

### Group By

`Group by` in the sidebar's `More options` menu groups the game's folders by classifications you define instead of by folder. Select `Manage classifications…` to create classifications, then right-click a folder in the sidebar and assign it with `Classify`.

## Add Mods

With a folder selected in the sidebar, add a mod in one of these ways:

- Download a mod from [GameBanana](/features/gamebanana).
- Drag and drop a mod folder or archive into the mod list.
- Click the `...` button above the mod list, select `Download`, and enter the URL of the file to download.

## Search

Press `Ctrl+F` to search the current game's mods. Korean names also match by their initial consonants.

## Previews

The Mod Manager uses an image or video file named `preview` inside a mod folder, case-insensitively, as the mod's preview. If there is none, the first supported media file in the folder is used.

::: info
If `Search Preview in Subfolders` is enabled in Settings → Mod, subfolders are searched as well.
:::

If `Show model previews for mods without previews` is enabled in Settings → Mod, mods without a preview show a model image of their current toggle state in the grid.

### Set or Replace a Preview

- For a folder in the sidebar, drag and drop an image or video file onto it.
- For a mod, copy an image or video file, then right-click the mod and select `Paste` under `Preview`. A mod without a preview also shows a `Paste` button in the preview area.
- In the [Model Viewer](/features/mod-manager/model-viewer), select `Misc` → `Capture and set preview`.

`Delete preview` in the same menu removes the current preview.

## Presets

A preset saves and restores which mods are enabled. Click the `+` button next to the preset selector to save the current state, and select a preset to apply or delete it.

## Toggle Presets {#toggle-presets}

A toggle preset saves the values of one mod's toggles. Right-click a mod and select `Toggle presets` to save the current values or apply a saved preset. Presets can be stored in the mod folder (`nhd.json`) or in the app database.

Reading values from the running game requires the built-in launcher and the myparsleycat XXMI libraries. Otherwise, values are read from the mod's INI files and `d3dx_user.ini`.

## Launch a Game

Select a game in the game selector and click the play button to launch it with mods. XXMI games use the launcher chosen on the [XXMI](/others/set-up-xxmi) page. For Neverness to Everness, set the game's launcher path the first time you launch it.
