# Mod Manager

The Mod Manager enables and disables mods for each registered game and organizes them by folder. [Previews](/features/mod-manager/previews), [presets](/features/mod-manager/presets), [mod tools](/features/mod-manager/mod-tools), and the [Model Viewer](/features/mod-manager/model-viewer) are also opened from this screen.

## Layout

The screen is divided into the sidebar on the left and the mod list on the right.

- **Sidebar**: Shows the folders directly under the mod folder, each with its enabled and total mod counts. The folder search box and the `More options` menu are at the top; the game and preset selectors are at the bottom.
- **Mod list**: Shows the mods in the folder selected in the sidebar. Downloading and enabled mods are listed first.

Drag the border between the sidebar and the mod list to resize the sidebar. While the border has focus, `←`/`→`, `Shift+←`/`Shift+→`, and `Home`/`End` also resize it.

### Mod List Header

| Item | Description |
|---|---|
| Search box | Searches the mods in the selected folder. See [Search](#search). |
| Sort | Selects `Name`, `Date`, or `Size`. The `Sort order` button next to it switches between ascending and descending. |
| `List view` / `Grid view` | Switches how the mod list is shown. |
| `Open folder` | Opens the selected folder in File Explorer. |
| `More options` | Shows `Enable all`, `Disable all`, `Merge mods (Beta)`, `Download`, and `Tools`. `Merge mods (Beta)` appears only for GIMI, SRMI, ZZMI, HIMI, WWMI, and EFMI games, and `Tools` only for games that support Wuwa Mod Fixer or ZZMI Mod Fixer. |

### List View and Grid View

- **List view**: Shows the `Name`, `Size`, and `Date` columns. Click a column header to sort by it; click it again to reverse the order.
- **Grid view**: Shows mods as cards. Card width and column count are set with `Mod grid layout` in [Mod Settings](/features/mod-manager/settings#layout).

The card color shows the mod's state: green is enabled, red is disabled, and yellow is enabled with a required dependency missing from the importer.

The card header has the `Nahida Agent`, `Mod Tools`, `Open in model viewer`, `Delete`, and `Open folder` buttons. Mods with Body Shape, Touch Profile, or Liquid emitter applied show the matching icon.

The right side of the card lists the mod's INI files and toggle sections. Change a toggle section's keys as described in [Change Keys](#keys). `Open INI file` opens the INI file, and `Collapse INI list` collapses the list.

## Games {#games}

### Add a Game

Click the `+` button next to the game selector (`Add Game`) to open the Add Game dialog.

- **Game Name**: The name shown in the game selector.
- **Importer**: Selects how the game loads mods. The default is `None`.
  - `None`: Select any folder as the `Mod folder path`. The game cannot be launched from Nahida Desktop.
  - An XXMI importer (`GIMI`, `SRMI`, `WWMI`, `ZZMI`, `HIMI`, `EFMI`): The importer's Mods folder is used as the mod folder. Importers that are not installed are marked `Not installed` and cannot be selected. Before [XXMI is set up](/others/set-up-xxmi), an `Open Importer settings` button is shown.
  - `NTE`: Select the `NTE install folder`. The game name is fixed to `NTE`. If you select a `Custom mod folder`, the game's Mods folder is linked to it. Nahida Desktop installs the files the game needs to load mods.

### Edit a Game

Open the game selector and click the pencil button next to the game (`Edit Game`). The dialog changes the importer and mod folder path, and `Display order` changes the game's position in the game selector. The game name cannot be changed.

`Delete Game` removes the game from the list. The game's presets are deleted too, and this cannot be undone.

### Launch a Game

Select a game in the game selector and click the `Launch game` button to launch it with mods.

- XXMI games use the launcher chosen on the [XXMI](/others/set-up-xxmi) page. If game settings that keep mods from applying, or uncompressed textures, are found before launch, a confirmation dialog is shown.
- For NTE, set the game's launcher path in the `Set launcher path` dialog the first time you launch it.
- Games whose importer is `None` cannot be launched.

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

## Sidebar

### Folder Menu

Right-click a folder in the sidebar to show these items.

| Item | Description |
|---|---|
| `Enable all` / `Disable all` | Enables or disables every mod in the folder. |
| `Enable Folder` / `Disable Folder` | Enables or disables the folder itself. |
| `Open in File Explorer` | Opens the folder in File Explorer. |
| `Create New Folder` | Creates a folder inside the folder. |
| `Delete Folder` | Moves the folder to the recycle bin. |
| `Expand subgroups` / `Keep subgroups expanded` | See [Subgroups](#subgroups). |
| `Set Character…` | Sets the character used by the folder's mods. See [Set Character](/features/mod-manager/mod-tools#set-character). |
| `Classify` | Assigns the folder to a group of a classification. See [Group By](#group-by). |
| `Tools` | Runs Wuwa Mod Fixer or ZZMI Mod Fixer on the whole folder. Shown only for supported games. |

### More Options Menu

The `More options` menu at the top of the sidebar has these items.

- `Create New Folder`: Creates a folder directly under the mod folder.
- `Sort folders`: Selects `Name`, `Mod count`, or `Active mod count`, and `Ascending` or `Descending`.
- `Group by`: See [Group By](#group-by).
- `Hide empty folders`: Hides folders that contain no mods.
- `Switch to Preview grid` / `Switch to Row list`: Shows the sidebar as a grid of square previews or as a row list.

`Sort folders` and `Hide empty folders` reset when the app restarts.

### Subgroups {#subgroups}

A folder in the sidebar can show its subfolders below it. Right-click the folder and select `Expand subgroups` to show them, or `Keep subgroups expanded` to keep them shown after the app restarts.

Clicking a folder whose subfolders are shown collapses or expands it; clicking any other folder selects it. While holding `Ctrl`, clicking a folder expands and selects it. While holding `Alt`, clicking a folder collapses or expands its parent.

A folder in the mod list can also be treated as a subgroup: right-click it and select `Mark as subgroup`. It then appears in the sidebar even while its parent is collapsed. `Unmark manual subgroup` in the sidebar folder's menu undoes it.

### Group By {#group-by}

`Group by` in the `More options` menu groups the sidebar's folders by classifications you define.

1. Select `Group by` → `Manage classifications…`, then click `New classification` to create a classification and its groups.
2. Right-click a folder in the sidebar and assign it to a group with `Classify`.
3. Select the classification in `Group by` to split the sidebar by group. Folders without a group appear under `Unclassified`.

Select `Group by` → `Folders` to show folders again.

## Add Mods

With a folder selected in the sidebar, add a mod in one of these ways:

- Download a mod from [GameBanana](/features/gamebanana). Selecting `Mod Manager` in the `Select Path` dialog opens `Confirm Download`, where you set the file name and download location.
- Drag and drop a mod folder or an archive (`.zip`, `.rar`, `.7z`) into the mod list. Archives are extracted automatically.
- Select `Download` in the `More options` menu above the mod list, and enter a direct download URL in the `Download from URL` dialog. Only http and https URLs are supported, and web page URLs cannot be used.

How an archive with a single top-level folder is extracted is set with `Archive extraction path` in [Mod Settings](/features/mod-manager/settings). With `Ask every time`, the `Choose extraction path` dialog is shown for each archive.

## Enable and Disable Mods

- Click a mod to enable or disable it.
- `Ctrl`+click a mod, or press `Ctrl+Enter` while it has focus, to disable every other mod in the folder and enable only that mod. If the mod is already enabled, it is disabled.
- When the mod search returns exactly one mod, `Enter` applies the same action to it and clears the search.

Enabling a mod whose required dependencies are missing from the importer shows the `Required Dependencies Are Missing` dialog. `Enable Anyway` enables it as is.

## Search {#search}

The folder search box is at the top of the sidebar, and the mod search box is above the mod list. `Ctrl+F` moves to the folder search; pressing it again while a folder is selected moves to the mod search.

- **Folder search**: Shows folders whose names contain the search text. If exactly one folder matches, it is selected automatically.
- **Mod search**: Searches the mods in the selected folder. Korean names also match by their initial consonants and partly typed syllables.

## Change Keys {#keys}

The card's INI list shows, for each toggle section, the key that switches to the next value and the key that switches to the previous one. Keys that are not set are shown as `Add key` or `Add previous key`.

Click a key to open the key dialog. Press the key combination to set, or enter it in `Manual edit` in the form `ctrl alt no_shift vk_up`.

NTE mods show a list of input keys instead of the INI list. Only one key without modifiers can be set, and a key the mod already uses cannot be set again.

## Mod Menu

Right-click a mod to show these items. Some items are hidden depending on the game or the mod.

| Section | Items |
|---|---|
| (top) | `View on GameBanana` |
| `Preview` | `Open in viewer`, `Paste`, `Delete preview` ([Previews](/features/mod-manager/previews)) |
| `Fix` | `Preset`, `Fix Tool`, Wuwa Mod Fixer, ZZMI Mod Fixer ([Mod Tools](/features/mod-manager/mod-tools#fix)) |
| `Mod` | `Open CMD`, `Open folder`, `Mark as subgroup`, `Rename` |
| `Tools` | `Nahida Agent`, `Model Viewer`, `Toggle presets`, and the other [mod tools](/features/mod-manager/mod-tools) |
| (bottom) | `Delete`: Moves the mod to the recycle bin. |
