# Mod Tools

Mod tools work on one mod or on the selected mods. Most are opened from the `Tools` section of the mod menu or from the `Mod Tools` button on a card in grid view. For NTE, the `Tools` section shows only `Nahida Agent`.

## Tool List

| Tool | Games | Description |
|---|---|---|
| `Nahida Agent` | All | Opens Nahida Agent for the selected mod. |
| [`Model Viewer`](/features/mod-manager/model-viewer) | All except NTE | Shows the mod's 3D model and toggle states. On a card, use the `Open in model viewer` button. |
| `Texture Resizer` | All | Resizes the mod's DDS textures and saves them to the original files. |
| `XXMI Menu Maker` | All | Generates an in-game menu from the mod's toggles. Opens on the Tools screen. |
| `Body Shape (Beta)` | All | Adjusts the mod's mesh using painted regions or bone weights. |
| `Touch Profile (Beta)` | All | Converts the mod into a touch-reactive variant with configurable zones. |
| `Vulkan Outline Fix` | EFMI | See [Vulkan Outline Fix](#vulkan-outline-fix). |
| `Liquid emitter (Beta)` | EFMI, GIMI, SRMI, WWMI, ZZMI | Places liquid emitters on the mod's mesh and hooks them into its INI. |
| `Find Conflict Mods` | All | See [Find Conflict Mods](#find-conflict). |
| [`Toggle presets`](/features/mod-manager/presets#toggle-presets) | All except NTE | Shown in the mod menu only. |

## Merge Mods (Beta) {#merge}

Combines several mods into one mod whose variants are switched with keys in game. Available for GIMI, SRMI, ZZMI, HIMI, WWMI, and EFMI games.

1. Select `Merge mods (Beta)` in the `More options` menu above the mod list.
2. Click two or more mods to select them, then click `Next`. `Cancel merge` or `Esc` leaves merge mode.
3. In the `Merge mods` dialog, set the following and click `Merge`.
   - `Name`: The name of the merged mod.
   - `Next key` / `Previous key`: The keys that switch between the mods in game.
   - `Save to`: `Current folder` or `New folder`.
   - `Merge type`: `Classic` or `Namespace`. If a selected mod cannot use classic merge, only `Namespace` can be selected.
   - `Include vanilla slot`: Adds the unmodded state to the variants. Available only with `Namespace`.
   - Mod list: The up and down buttons change the switching order. `Group with next` groups a mod with the next one into a nested group, and `Ungroup` dissolves the group.

## Find Conflict Mods {#find-conflict}

Keeps the selected mod enabled and disables the other mods half at a time to find the mod that conflicts with it. If the mod is disabled, enable it first.

## Fix {#fix}

Run fix tools from the `Fix` section of the mod menu or from the `Mod Tools` menu on a card. Output appears in a log dialog; tools that need input take it at the bottom of the log dialog.

- `Preset`, `Fix Tool`: Run the presets and fix scripts registered in Fix Tool Manager on the Tools screen. Python fix tools require Python to be installed.
- **Wuwa Mod Fixer**: Shown for WWMI games and games whose importer is `None`. It is installed the first time you run it, and a newer version is offered when available. Choose options and run it on the `Fix Mod` tab; restore backups on the `Rollback` tab.
- **ZZMI Mod Fixer**: Shown for ZZMI games. It has the `Hash Fix`, `Jane Remap`, `Dialyn Remap`, and `Rollback` tabs.

Wuwa Mod Fixer and ZZMI Mod Fixer can also be run on a whole folder from `Tools` in the sidebar folder menu or in the `More options` menu above the mod list.

When `Automatically inspect mods for required fixes` is enabled in [Mod Settings](/features/mod-manager/settings), a notification is shown when a downloaded or added mod needs a fix. `Open Fixer` in the notification opens the matching fixer.

## Vulkan Outline Fix {#vulkan-outline-fix}

For Arknights: Endfield (EFMI) mods only. On Vulkan, some parts of a mod are drawn again in the outline pass and look opaque. This tool lists those parts by INI file; select the parts to keep out of the outline pass and apply.

- `Revert`: Restores the original file.
- If a file was edited by hand after applying, it shows `Edited by hand`. To apply or revert again, select `Use current file as original`.

After applying, press `F10` in game to reload mods.

## Set Character {#set-character}

When a sidebar folder has a character set, the mods in it and its subfolders use that character's toon style, motions, and original data in the [Model Viewer](/features/mod-manager/model-viewer). See [Character Data](/features/mod-manager/character-data).

1. Right-click a folder in the sidebar and select `Set Character…`.
2. Select the `Game` and `Character`, then click `Save`.

To remove the character, click `Clear` in the same dialog.

## Other Mod Menu Items

- `Rename`: Renames the mod folder.
- `Open CMD`: Opens a command prompt in the mod folder.
- `View on GameBanana`: Opens the GameBanana page of a mod downloaded from GameBanana.
