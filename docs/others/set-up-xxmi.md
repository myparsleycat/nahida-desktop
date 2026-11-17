# Set Up XXMI

The XXMI page in the navigation rail chooses how games start and manages the XXMI importers. Set it up before adding XXMI games to the Mod Manager or using the tools that need XXMI.

## Choose a Game Launcher

Select `Game launcher` at the top of the XXMI page:

- **Built-in**: Nahida Desktop installs and updates the importer packages and XXMI libraries itself and starts the games.
- **XXMI Launcher**: Nahida Desktop uses an installed XXMI Launcher.

Settings for both are kept when you switch.

## Built-in Launcher

1. Set the `Importer root folder`. Each importer is installed in its own subfolder here.
2. If an XXMI Launcher installation is found while no importer is configured yet, you can import its settings. Packages and XXMI libraries are installed fresh, and you choose whether `Mods` and `d3dx_user.ini` stay in the XXMI Launcher folder or move to the new importer folders.
3. Select an importer in the sidebar, set its game folder on the `General` tab, and install its package on the `Packages` tab.

Each importer page has these tabs:

- **General**: Game folder, mod folder, importer icon, how the game starts (directly, Steam, Epic Games, or a custom command), and DLL injection options.
- **Packages**: Importer package and XXMI library versions. The library provider can be SpectrumQT or the myparsleycat fork.
- **Game settings** (GIMI, SRMI, HIMI, and WWMI only): FPS unlock, HDR, texture detail, and other per-game options. Genshin Impact uses GI FPS Unlocker.
- **Advanced**: 3DMigoto options such as hunting mode, unsafe mode, log level, mod key bindings, and custom commands.
- **d3dx.ini**: Edits `d3dx.ini` settings.
- **Tools**: Creates the Quick Start desktop shortcut, repairs the runtime or package, optimizes mod INI files, and turns on logging mode to collect logs and crash dumps for a problem report.
- **Runtimes** (importers that have runtimes only): Global mods that tools install into the game's Mods folder, with automatic updates on launch.

The `ReShade` page installs ReShade and its official effect packages and sets a shared preset folder.

`Manage` shows pending updates, global options, pre-launch checks, cache cleanup, and resets the built-in launcher.

### DLL Injector

`DLL injector` on an importer's `General` tab selects how the runtime DLL is loaded:

- `Existing launcher`: The default. The runtime is loaded by its own hook or loader.
- `Native`: The runtime and extra DLLs are injected directly into 64-bit games instead of through hooks.

If `Injection mode` is `Bypass`, only the extra DLLs are injected.

`Native` does not need the XXMI loader (`3dmloader.dll`) or the legacy 3DMigoto loader, so a legacy runtime can inject extra DLLs without the XXMI libraries. It works only with 64-bit games, and waits for the game process and each DLL up to the `Game start timeout (seconds)`.

## XXMI Launcher

1. Enter the XXMI Launcher folder in `XXMI Path` and click `Save`. If you did not change the installation directory, it is usually:

   ```txt
   %AppData%\XXMI Launcher
   ```

2. If you do not know where it is installed, click `Auto Scan`.

::: warning
`Auto Scan` searches all drives for XXMI Launcher. If several installations exist, it may select a different one, so check the detected path.
:::

After the path is saved, the page shows the enabled importers and lets you change the XXMI DLL and importer package versions.

## Pre-launch Checks

Both launchers can check for problems before a game starts: Genshin Impact's Dynamic Character Resolution, NVIDIA Smooth Motion, 3DMigoto logging, and uncompressed textures.

## Features That Need XXMI

- Launching XXMI games from the Mod Manager (Neverness to Everness games do not need XXMI)
- [4001 Fixer](/features/mod-tools/dll-builder)
- [Persist Toggles](/features/mod-tools/persist-toggles)
- Toggle Viewer Generator
