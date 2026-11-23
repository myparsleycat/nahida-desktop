<div align="center">
  <img src="build/appicon.png" alt="Nahida Desktop" width="96" />

  <h1>Nahida Desktop</h1>

  <p>A Windows mod manager for 3DMigoto (XXMI) games and Neverness to Everness.</p>

  <p>
    <img alt="Version" src="https://img.shields.io/github/v/release/myparsleycat/nahida-desktop?style=flat-square" />
    <img alt="License" src="https://img.shields.io/badge/license-GNU%20GPLv3-green?style=flat-square" />
    <img alt="Platform" src="https://img.shields.io/badge/platform-Windows-lightgrey?style=flat-square" />
    <img alt="Downloads" src="https://img.shields.io/github/downloads/myparsleycat/nahida-desktop/total?style=flat-square&label=Downloads" />
  </p>

  <p>
    <a href="https://github.com/myparsleycat/nahida-desktop/releases/latest">Download</a> ·
    <a href="docs/guide/getting-started.md">Getting Started</a> ·
    <a href="#features">Features</a> ·
    <a href="#faq">FAQ</a> ·
    <a href="https://deepwiki.com/myparsleycat/nahida-desktop">DeepWiki</a>
  </p>
</div>

<!--
<p align="center">
  <img src="docs/public/readme/main.png" alt="Nahida Desktop mod manager" width="960" />
</p>
-->

---

## Overview

Nahida Desktop manages mods, launches games with the XXMI importers, and includes tools for editing and fixing mods. It also browses and downloads GameBanana mods, and syncs files and backups with Nahida Drive.

| Game                  | Mod loader                                   |
| --------------------- | -------------------------------------------- |
| Genshin Impact        | GIMI                                         |
| Honkai: Star Rail     | SRMI                                         |
| Wuthering Waves       | WWMI                                         |
| Zenless Zone Zero     | ZZMI                                         |
| Honkai Impact 3rd     | HIMI                                         |
| Arknights: Endfield   | EFMI                                         |
| Neverness to Everness | Unreal Engine pak mods (`Content/Paks/Mods`) |

## Features

### Mod Manager

- Enable, disable, rename, and organize mods by game, with nested subgroups
- **Group by** — group a game's folders by your own classifications
- **Search** — find mods with Ctrl+F, including Korean initial-consonant matching
- **Exclusive toggle** — enable one mod and disable the others in the same group
- **Presets** — save and restore sets of enabled mods, and pick one before launching a game
- **Toggle presets** — save and apply a mod's toggle values, stored in the mod folder (`nhd.json`) or in the app database
- **Toggle keys** — change the keys a mod's toggles use, with a key recorder
- **NTE input keys** — change the input keys compiled into Neverness to Everness mods
- **Merge** — combine selected 3DMigoto mods into one pack
- **Find conflicts** — disable mods in batches to find the one that conflicts with a chosen mod
- **Mod compression** — save disk space with Zstd for disabled mods, or Windows XPRESS4K compression that keeps mods usable
- Detects mods that need a fix and offers to open the matching fixer
- Updates the mod list when files in the mod folder change
- Add mods by dragging folders or archives into a group, or by downloading from a URL
- Image and video previews; mods without a preview can show a model render of their current toggle state
- Set a preview by pasting an image from the clipboard or by saving a screenshot from the Model Viewer

### Mod Tools

Tools page:

| Tool                    | Description                                                                                                       |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------- |
| 4001 Fixer              | Build and apply a GIMI `d3d11.dll` that fixes the 4001 error                                                      |
| Persist Toggles         | Write toggle values saved in `d3dx_user.ini` back into the mod INI so they survive disabling or reloading the mod |
| XXMI Menu Maker         | Generate an in-game menu for a mod's KeySwap toggles                                                              |
| Toggle Viewer Generator | Generate toggle-viewer files from a mod's INI toggle sections                                                     |
| Fix Tool Manager        | Register Python or executable fix scripts and run them on a mod                                                   |
| Mod Bisect              | Find the mod that causes an in-game problem by disabling mods in batches                                          |
| Body Shape (beta)       | Adjust a mod's mesh using painted regions or bone weights                                                         |
| Touch Profile (beta)    | Convert a mod into a touch-reactive variant with configurable zones                                               |

Mod menu:

| Tool                  | Description                                                                                      |
| --------------------- | ------------------------------------------------------------------------------------------------ |
| Model Viewer          | View a mod's 3D model; see [Model Viewer](#model-viewer)                                         |
| Texture Resizer       | Resize, convert, and upscale DDS textures in bulk (Real-ESRGAN, Real-CUGAN), with `.bak` backups |
| Vulkan Outline Fix    | Exclude EFMI mod parts that are drawn opaque in the Vulkan outline pass                          |
| Liquid Emitter (beta) | Place liquid emitters on a mod's mesh and hook them into its INI (all importers except HIMI)     |

XXMI Menu Maker, Body Shape, and Touch Profile can also be opened from the mod menu.

Character sidebar:

| Tool           | Description                                                                                                                 |
| -------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Wuwa Mod Fixer | Install, update, and run [Wuwa Mod Fixer](https://github.com/Moonholder/Wuwa_Mod_Fixer) from the mod manager, with rollback |
| ZZMI Mod Fixer | Native hash fixing and remapping for Zenless Zone Zero mods, with rollback                                                  |

### Model Viewer

- Game-specific toon shading for GIMI, SRMI, WWMI, ZZMI, and EFMI mods
- Animation playback
- Preview a mod's toggles and save the values to the INI or as a toggle preset
- Save screenshots as mod previews
- Apply toggle values to the running game, and preview hovered values in game

### XXMI

- Launch games through an existing XXMI Launcher installation or the built-in launcher
- The built-in launcher installs and updates importer packages and XXMI libraries (SpectrumQT or the myparsleycat fork), and can import user data from XXMI Launcher
- Start games directly or through Steam or Epic Games
- Pre-launch checks for Genshin Impact's Dynamic Character Resolution, NVIDIA Smooth Motion, 3DMigoto logging, and uncompressed textures
- **Game settings** — FPS unlock, HDR, texture detail, and other per-game options, plus GI FPS Unlocker
- **Runtimes** — global mods that tools install into the game's Mods folder, with automatic updates on launch
- **ReShade** — install ReShade and official effect packages, and share a preset folder
- **Logging mode** — collect XXMI logs and crash dumps to attach to a problem report
- Create desktop shortcuts, repair packages, and clean unused caches

Features that act on the running game, such as applying Model Viewer toggles and reading toggle presets from the game, require the built-in launcher and the myparsleycat XXMI libraries.

### GameBanana

- Browse and download GameBanana mods inside the app, directly into a game's mod groups
- Signing in is optional and only needed for account actions such as liking a mod

### Nahida Drive, Transfers, and Backup

- Upload files to and download files from Nahida Drive, with folder search, copy and move, trash, and video previews
- Import folders from Shared Drive links into your drive
- Download files from nahida.live directly into mod groups
- Files the server already has are not uploaded again
- Pause, resume, and retry transfers, with configurable concurrency and a download bandwidth limit
- Back up registered games' mod folders and other folders on a schedule or after changes, and restore them to an empty folder

Drive, Shared Drive, and Backup require a nahida.live account.

### Nahida Agent

- An in-app assistant that works with your mods using built-in app actions
- Providers: OpenAI (API key or ChatGPT sign-in), Anthropic, OpenRouter, Vercel AI Gateway, OpenCode Zen, OpenCode Go, and custom endpoints (OpenAI Responses, OpenAI-compatible, or Anthropic protocol)
- MCP servers, skills, and image attachments
- Scripts written by the agent run only after approval unless auto-run is turned on
- Optional Blender integration through the Blender MCP add-on

The agent appears in the navigation rail after a provider and model are configured in Settings.

### App

- Automatic updates, with an option to include pre-releases
- Run at sign-in and keep running in the system tray
- Proxy for external requests, with fallback to the Windows system proxy
- Report a problem with logs and files attached from Settings
- UI in English, Japanese, Korean, and Chinese

## Download

Download the latest version from [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest):

- [`nahida-desktop-windows-amd64-installer.exe`](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe) — installer
- [`nahida-desktop-windows-amd64.exe`](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe) — portable executable
- `SHA256SUMS` — checksums of both files

Nahida Desktop runs on 64-bit Windows only. GameBanana sign-in uses the Microsoft Edge WebView2 Runtime; the installer installs it, and the portable executable requires it to be installed already.

## FAQ

### Can I use it without a nahida.live account?

Yes. The mod manager, game launching, mod tools, the agent, and GameBanana and nahida.live downloads work without signing in. Drive, Shared Drive, and Backup require an account, as do sharing agent conversations and attaching a mod folder to a problem report.

### The installer or a component is blocked by antivirus

Windows Defender and some antivirus programs may flag the installer. Download it only from [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest), and report false positives to your antivirus vendor.

If antivirus blocks the elevated helper, allow `nahida-elevated-helper` or exclude `%USERPROFILE%\.nahida-desktop\elevated`. If it quarantines XXMI library files, restore them from the antivirus protection history. With the built-in launcher, you can also click `Repair runtime` on the importer's Tools tab on the XXMI page.

### What do the mod tools need?

- The 4001 Fixer builds `d3d11.dll` with Visual Studio or Visual Studio Build Tools (C++ build tools).
- Python fix scripts in the Fix Tool Manager need Python installed.

## Development

Requirements:

- Go (the version in `go.mod`)
- [Task](https://taskfile.dev) v3
- [Rustup](https://rustup.rs/) and Visual Studio C++ Build Tools with the Windows SDK for the amd64 build

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` writes `bin/nahida-desktop.exe`.

| Command                              | Description                                                |
| ------------------------------------ | ---------------------------------------------------------- |
| `task run`                           | Build and run the app                                      |
| `task dev`                           | Run the app and rebuild and restart it when sources change |
| `task test`                          | Run Go tests                                               |
| `task lint`                          | Run the Go linter                                          |
| `task fmt -- <paths...>`             | Format Go files                                            |
| `task vuln`                          | Scan for known vulnerabilities                             |
| `task package APP_VERSION=<version>` | Build the installer (requires Node.js 22 and NSIS 3.11)    |
| `pnpm test`                          | Run release-script tests (requires Node.js 22 and pnpm 11) |

Go commands run outside `task` need `-tags nodynamic`. See [AGENTS.md](AGENTS.md) for project conventions.

## Credits

- [Wuwa Mod Fixer](https://github.com/Moonholder/Wuwa_Mod_Fixer) by [Moonholder](https://github.com/Moonholder) — used to fix Wuthering Waves mods
- [ZZZ-Mod-Fixer](https://github.com/Vonksdesu/ZZZ-Mod-Fixer) maintained by [VonksDesuuu](https://github.com/Vonksdesu), based on the [original tool](https://gamebanana.com/tools/21671) by [petrascyll](https://gamebanana.com/members/2644630) — hash-fixing and remapping logic used by the native ZZMI Mod Fixer, licensed under the MIT License; see [the bundled notice](internal/tools/zzmi/NOTICE.md)
- [XXMI-Menu-Maker](https://github.com/XingNian-www/XXMI-Menu-Maker) by [星念](https://github.com/XingNian-www) — strict KeySwap mapping, GUI generation, and resource-rendering behavior used by the native XXMI Menu Maker, licensed under the MIT License; see [the bundled notice](internal/tools/menumaker/NOTICE.md)
- [HoyoToon](https://github.com/Hoyotoon/HoyoToon) (GPL-3.0), [UnityGenshinToonShader](https://github.com/kaze-mio/UnityGenshinToonShader) by kaze-mio (MIT), [StarRailNPRShader](https://github.com/stalomeow/StarRailNPRShader) (GPL-3.0), and Warfarin Studio's reconstruction of the Arknights: Endfield character shaders — references for the Model Viewer's toon shading
- [three.js](https://github.com/mrdoob/three.js) (MIT) — the Model Viewer's standard material, room environment lighting, and DFG lookup table are ported from r186
- Rayvich (RZMenu) — touch detection shaders used by Touch Profile
- [IBM Plex Sans KR](https://github.com/IBM/plex) and [Inter](https://github.com/rsms/inter) — UI fonts, licensed under the SIL Open Font License 1.1

The app downloads these projects on demand; they are not bundled:

- XXMI packages and libraries by [SpectrumQT](https://github.com/SpectrumQT/XXMI-Libs-Package), [SilentNightSound](https://github.com/SilentNightSound/GIMI-Package), and [leotorrez](https://github.com/leotorrez/ZZMI-Package), and [GI-FPS-Unlocker-Package](https://github.com/SpectrumQT/GI-FPS-Unlocker-Package)
- [ReShade](https://github.com/crosire/reshade) and [reshade-shaders](https://github.com/crosire/reshade-shaders) by crosire
- [Real-ESRGAN](https://github.com/xinntao/Real-ESRGAN) by xinntao and [realcugan-ncnn-vulkan](https://github.com/nihui/realcugan-ncnn-vulkan) by nihui
- [UniversalSigBypasser](https://github.com/rm-NoobInCoding/UniversalSigBypasser) by rm-NoobInCoding and [Ultimate ASI Loader](https://github.com/ThirteenAG/Ultimate-ASI-Loader) by ThirteenAG
- [Git for Windows](https://github.com/git-for-windows/git)
- The [models.dev](https://models.dev) model catalog

## License

Distributed under the [GNU GPLv3](LICENSE).

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fmyparsleycat%2Fnahida-desktop.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fmyparsleycat%2Fnahida-desktop?ref=badge_large)
