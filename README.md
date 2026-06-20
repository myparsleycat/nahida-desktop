<div align="center">
  <img src="resources/nahida.png" alt="Nahida Desktop" width="96" />

  <h1>Nahida Desktop</h1>

  <p>Desktop client for <a href="https://nahida.live">nahida.live</a></p>

  <p>
    <img alt="Version" src="https://img.shields.io/github/v/release/myparsleycat/nahida-desktop?style=flat-square" />
    <img alt="License" src="https://img.shields.io/badge/license-%20%20GNU%20GPLv3%20-green?style=flat-square" />
    <img alt="Platform" src="https://img.shields.io/badge/platform-Windows-lightgrey?style=flat-square" />
    <img alt="Electron" src="https://img.shields.io/badge/electron-40-47848F?style=flat-square&logo=electron" />
  </p>
</div>

---

## Overview

Nahida Desktop is a desktop application that provides a unified interface for managing 3dmigoto mods, transferring files with Nahida Drive, and accessing various other features

## Features

### Mod Manager

- Enable, disable, and toggle mods per game with a single click
- **Exclusive toggle** — automatically disable conflicting mods in the same group
- **Presets** — save and restore sets of enabled mods instantly
- Watches the mod folder in real time and reflects changes immediately
- Drag-and-drop archive extraction and folder copy directly into mod groups
- Clipboard image paste for mod preview thumbnails
- Save scripts for mod fixes and run them from each mod file with one click
- Support nested folder structures through subgroups

### XXMI Launcher Integration

- Automatically detects your XXMI installation by scanning all drives
- Launch games directly from Nahida Desktop via XXMI Launcher
- Built-in d3d11.dll build functionality

### ☁️ Cloud Drive & File Transfer

- Upload and download files to/from your Nahida Drive
- Multi-threaded uploads with configurable concurrency (up to 16 threads)gurable)
- Download directly from GameBanana and nahida.live into character folders registered in the mod manager through Nahida Desktop

## Download

Latest release can be downloaded either from [Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) page.
Visit the latest release page above to download and install the app.

## FAQ

### Can I use it without a nahida.live account?

Yes. You can use the mod manager and tools, as well as download from the Nahida Live website through Nahida Desktop, without logging in.

### Installation does not proceed

Windows Defender and some antivirus programs may occasionally block the installer. Try adding the installer as an exception or temporarily disabling real-time protection, then try again.

## License

Distributed under the [GNU GPLv3](LICENSE).
