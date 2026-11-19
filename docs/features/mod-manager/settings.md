# Mod Settings

Settings → Mod sets how the Mod Manager behaves.

## Mod Management

| Setting | Default | Description |
|---|---|---|
| `Archive extraction path` | `Remove top-level folder` | How archives with a single top-level folder are extracted. |
| `Delete archive after extraction` | On | Deletes the original archive after extracting it to the mod folder. |
| `Move folder instead of copy` | On | Moves a folder dropped into the mod folder instead of copying it. |
| `Automatically inspect mods for required fixes` | On | Inspects downloaded or added mods and notifies you if a fix is recommended. |
| `Copy ShaderFixes on enable` | On | Copies the mod's ShaderFixes files into the importer's ShaderFixes folder when the mod is enabled. |
| `Disabled mod prefix style` | `DISABLED (space)` | The prefix added to the folder name when a mod is disabled. Both formats are recognized when enabling. |
| `Search for previews in subfolders` | Off | If a sidebar folder has no preview, searches the mod folders inside it. |
| `Toggle preset storage` | `Not selected` | Where [toggle presets](/features/mod-manager/presets#toggle-presets) are stored. Changing it moves the existing presets. |
| `Automatically select download target folder` | Off | Selects a mod folder whose name is similar to the download. Choose the download sources it applies to (`GameBanana`, `Nahida Live`, `Hui`, `Drive`); the default is `GameBanana`. |

The options of `Archive extraction path` are:

- `Remove top-level folder`: Removes the top-level folder and extracts its contents. Example: `mod.zip/a/b/mod.ini` → `b/mod.ini`
- `Keep archive paths`: Keeps the paths inside the archive. Example: `mod.zip/a/b/mod.ini` → `a/b/mod.ini`
- `Ask every time`: Choose in the `Choose extraction path` dialog each time a mod is added.

## Layout {#layout}

| Setting | Default | Description |
|---|---|---|
| `Sidebar layout` | `Row list` | Shows the sidebar as a `Row list` or a square `Preview grid`. |
| `Show model previews for mods without previews` | On | In grid view, shows a model image of the current toggle state for mods without an image or video. |
| `Mod grid layout` | `Responsive` | How grid view decides card widths and column counts. |

The options of `Mod grid layout` are:

- `Responsive`: Calculates how many columns fit using `Responsive base card width` (240–640, default 400).
- `Fixed card width`: Keeps the card width fixed (240–640, default 360) and only changes the column count.
- `Fixed column count`: Uses this many columns when possible (1–8, default 4).

## Mod Compression

Compresses mod files to save disk space. Turn it on and off with the `Enable mod compression` switch, which also shows the progress.

- `Compression method`: The default is `XPRESS4K`.
  - `Zstd`: Compresses only large files in disabled mods. The files are restored when the mod is enabled.
  - `XPRESS4K`: Uses Windows file compression on all mod files. Mods stay usable while compressed.
- `Compression size threshold`: The minimum file size for `Zstd` compression. The range is 1–64 MiB, and the default is 1 MiB.
