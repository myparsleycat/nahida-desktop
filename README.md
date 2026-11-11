<div align="center">
  <img src="build/appicon.png" alt="Nahida Desktop" width="96" />

  <h1>Nahida Desktop</h1>

  <p>A Windows mod manager for games using 3DMigoto.</p>

  <p>
    <img alt="Version" src="https://img.shields.io/github/v/release/myparsleycat/nahida-desktop?style=flat-square" />
    <img alt="License" src="https://img.shields.io/badge/license-GNU%20GPLv3-green?style=flat-square" />
    <img alt="Platform" src="https://img.shields.io/badge/platform-Windows-lightgrey?style=flat-square" />
    <img alt="Downloads" src="https://img.shields.io/github/downloads/myparsleycat/nahida-desktop/total?style=flat-square&label=Downloads" />
  </p>

  <p>
    <a href="https://github.com/myparsleycat/nahida-desktop/releases/latest">Download</a> ·
    <a href="docs/guide/getting-started.md">Getting Started</a> ·
    <a href="docs/features/index.md">Features</a> ·
    <a href="https://deepwiki.com/myparsleycat/nahida-desktop">DeepWiki</a>
  </p>
</div>

---

## Overview

Nahida Desktop is a Windows mod manager for games supported by the XXMI importers (GIMI, SRMI, WWMI, ZZMI, HIMI, and EFMI). It also includes game launching, GameBanana browsing and downloads, mod editing and troubleshooting tools, and file transfers and backups through Nahida Drive.

## Features

### Mod Manager

- Enable, disable, rename, and organize mods by game, with support for nested subgroups
- **Exclusive toggle** — enable one mod and disable the others in the same group
- **Presets** — save and restore sets of enabled mods
- **Merge** — combine selected 3DMigoto mods into one pack
- **Find conflicts** — keep one mod enabled while disabling the others in batches to identify the mod it conflicts with
- Updates the mod list automatically when files in the mod folder change
- Add mods by dragging folders or archives into a group, or by downloading from a URL
- Image previews (JPEG, PNG, WebP, AVIF) and video previews (MP4, WebM)
- Set a preview by pasting an image from the clipboard or by saving a screenshot from the Model Viewer

### Game Launching

- Launch games using an existing XXMI Launcher installation or the built-in launcher
- The built-in launcher installs and updates importer packages and XXMI libraries, and can import user data from an existing XXMI Launcher installation
- Checks for mod-loading and performance issues before launching a game, including Genshin Impact's Dynamic Character Resolution, 3DMigoto logging, and large uncompressed DDS textures
- Configure ReShade for the built-in launcher

### Mod Tools

| Tool                    | Description                                                                                                                 |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Model Viewer            | View a mod's 3D model and save screenshots as mod previews                                                                  |
| Mod Bisect              | Find the mod that causes an in-game problem by disabling mods in batches                                                    |
| XXMI Menu Maker         | Generate an in-game menu for a mod's KeySwap toggles                                                                        |
| Toggle Viewer Generator | Generate toggle-viewer files from a mod's INI toggle sections                                                               |
| Toggle Persistence      | Write toggle values saved in `d3dx_user.ini` back into the mod INI so they survive disabling or reloading the mod           |
| Texture Resizer         | Resize and convert DDS textures in bulk, optionally keeping `.bak` backups of the originals                                 |
| 4001 Fixer              | Build and apply a GIMI `d3d11.dll` that fixes the 4001 error                                                                |
| Fix Tool Manager        | Register Python or executable fix scripts and run them on a mod                                                             |
| Wuwa Mod Fixer          | Install, update, and run [Wuwa Mod Fixer](https://github.com/Moonholder/Wuwa_Mod_Fixer) from the mod manager, with rollback |
| ZZMI Mod Fixer          | Native hash fixing and remapping for Zenless Zone Zero mods, with rollback                                                  |
| Vulkan Outline Fix      | Exclude EFMI mod parts that are drawn opaque in the Vulkan outline pass                                                     |
| Liquid Emitter          | Place liquid emitters on a mod's mesh and hook them into its INI                                                            |
| Body Shape (beta)       | Adjust a mod's mesh using painted regions or bone weights                                                                   |
| Touch Profile (beta)    | Convert a mod into a touch-reactive variant with configurable zones                                                         |

### GameBanana

- Browse and download GameBanana mods inside the app, directly into a game's mod groups
- Signing in is optional and only needed for account actions such as liking a mod

### Nahida Drive, Transfers, and Backup

- Upload files to and download files from Nahida Drive, and import shared folders into your drive
- Download files from nahida.live directly into mod groups
- Pause, resume, and retry uploads and downloads from the Transfers page, with configurable concurrency
- Back up registered games' mod folders and other folders to the server on a schedule, and restore them to an empty folder

Drive and Backup require a nahida.live account.

### Nahida Agent

- An in-app assistant that works with your mods using built-in app actions
- Supports OpenAI (API key or ChatGPT sign-in), Anthropic, and OpenAI-compatible endpoints
- Scripts written by the agent run only after approval unless auto-run is turned on
- Optional Blender integration through the Blender MCP add-on

The agent appears in the sidebar after a provider, endpoint, and model are configured in Settings.

### Network and Language

- Configure a proxy for external requests, with fallback to the Windows system proxy
- UI available in English, Japanese, Korean, and Chinese

## Download

Download the latest version from [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest):

- [`nahida-desktop-windows-amd64-installer.exe`](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe) — installer
- [`nahida-desktop-windows-amd64.exe`](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe) — portable executable

Nahida Desktop runs on 64-bit Windows only.

## Build From Source

Requirements:

- Go (the version in `go.mod`)
- [Task](https://taskfile.dev) v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` writes `bin/nahida-desktop.exe`.

Other common commands:

| Command     | Description                                                |
| ----------- | ---------------------------------------------------------- |
| `task run`  | Build and run the app                                      |
| `task test` | Run Go tests                                               |
| `task lint` | Run the Go linter                                          |
| `pnpm test` | Run release-script tests (requires Node.js 22 and pnpm 11) |

See [AGENTS.md](AGENTS.md) for project conventions.

## FAQ

### Can I use it without a nahida.live account?

Yes. The mod manager, game launching, mod tools, and GameBanana and nahida.live downloads work without signing in. Only Nahida Drive and Backup require an account.

### The installer is blocked

Windows Defender and some antivirus programs may flag the installer. Make sure you downloaded it from [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest). If you believe the detection is a false positive, submit the file to your antivirus vendor for review. The portable executable is another option.

## Credits

- [Wuwa Mod Fixer](https://github.com/Moonholder/Wuwa_Mod_Fixer) by [Moonholder](https://github.com/Moonholder) — used to fix Wuthering Waves mods
- [ZZZ-Mod-Fixer](https://github.com/Vonksdesu/ZZZ-Mod-Fixer) maintained by [VonksDesuuu](https://github.com/Vonksdesu), based on the [original tool](https://gamebanana.com/tools/21671) by [petrascyll](https://gamebanana.com/members/2644630) — hash-fixing and remapping logic used by the native ZZMI Mod Fixer, licensed under the MIT License; see [the bundled notice](internal/tools/zzmi/NOTICE.md)
- [XXMI-Menu-Maker](https://github.com/XingNian-www/XXMI-Menu-Maker) by [星念](https://github.com/XingNian-www) — strict KeySwap mapping, GUI generation, and resource-rendering behavior used by the native XXMI Menu Maker, licensed under the MIT License; see [the bundled notice](internal/tools/menumaker/NOTICE.md)

## License

Distributed under the [GNU GPLv3](LICENSE).

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fmyparsleycat%2Fnahida-desktop.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fmyparsleycat%2Fnahida-desktop?ref=badge_large)
