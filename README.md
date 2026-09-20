<div align="center">
  <img src="build/appicon.png" alt="Nahida Desktop" width="96" />

  <h1>Nahida Desktop</h1>

  <p>
    <img alt="Version" src="https://img.shields.io/github/v/release/myparsleycat/nahida-desktop?style=flat-square" />
    <img alt="License" src="https://img.shields.io/badge/license-%20%20GNU%20GPLv3%20-green?style=flat-square" />
    <img alt="Platform" src="https://img.shields.io/badge/platform-Windows-lightgrey?style=flat-square" />
    <img alt="Electron" src="https://img.shields.io/badge/wails-3-DF0000?style=flat-square&logo=wails" />
    <img alt="Downloads" src="https://img.shields.io/github/downloads/myparsleycat/nahida-desktop/total?style=flat-square&label=Downloads" />
  </p>

  <p>
    <a href="https://deepwiki.com/myparsleycat/nahida-desktop"><img src="https://img.shields.io/badge/DeepWiki-myparsleycat%2Fnahida--desktop-blue.svg?logo=data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACwAAAAyCAYAAAAnWDnqAAAAAXNSR0IArs4c6QAAA05JREFUaEPtmUtyEzEQhtWTQyQLHNak2AB7ZnyXZMEjXMGeK/AIi+QuHrMnbChYY7MIh8g01fJoopFb0uhhEqqcbWTp06/uv1saEDv4O3n3dV60RfP947Mm9/SQc0ICFQgzfc4CYZoTPAswgSJCCUJUnAAoRHOAUOcATwbmVLWdGoH//PB8mnKqScAhsD0kYP3j/Yt5LPQe2KvcXmGvRHcDnpxfL2zOYJ1mFwrryWTz0advv1Ut4CJgf5uhDuDj5eUcAUoahrdY/56ebRWeraTjMt/00Sh3UDtjgHtQNHwcRGOC98BJEAEymycmYcWwOprTgcB6VZ5JK5TAJ+fXGLBm3FDAmn6oPPjR4rKCAoJCal2eAiQp2x0vxTPB3ALO2CRkwmDy5WohzBDwSEFKRwPbknEggCPB/imwrycgxX2NzoMCHhPkDwqYMr9tRcP5qNrMZHkVnOjRMWwLCcr8ohBVb1OMjxLwGCvjTikrsBOiA6fNyCrm8V1rP93iVPpwaE+gO0SsWmPiXB+jikdf6SizrT5qKasx5j8ABbHpFTx+vFXp9EnYQmLx02h1QTTrl6eDqxLnGjporxl3NL3agEvXdT0WmEost648sQOYAeJS9Q7bfUVoMGnjo4AZdUMQku50McDcMWcBPvr0SzbTAFDfvJqwLzgxwATnCgnp4wDl6Aa+Ax283gghmj+vj7feE2KBBRMW3FzOpLOADl0Isb5587h/U4gGvkt5v60Z1VLG8BhYjbzRwyQZemwAd6cCR5/XFWLYZRIMpX39AR0tjaGGiGzLVyhse5C9RKC6ai42ppWPKiBagOvaYk8lO7DajerabOZP46Lby5wKjw1HCRx7p9sVMOWGzb/vA1hwiWc6jm3MvQDTogQkiqIhJV0nBQBTU+3okKCFDy9WwferkHjtxib7t3xIUQtHxnIwtx4mpg26/HfwVNVDb4oI9RHmx5WGelRVlrtiw43zboCLaxv46AZeB3IlTkwouebTr1y2NjSpHz68WNFjHvupy3q8TFn3Hos2IAk4Ju5dCo8B3wP7VPr/FGaKiG+T+v+TQqIrOqMTL1VdWV1DdmcbO8KXBz6esmYWYKPwDL5b5FA1a0hwapHiom0r/cKaoqr+27/XcrS5UwSMbQAAAABJRU5ErkJggg==" alt="DeepWiki"></a>
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
- **Merge** — combine selected 3DMigoto mods into one pack
- Watches the mod folder in real time and reflects changes immediately
- Drag-and-drop archive extraction and folder copy directly into mod groups
- Clipboard image paste for mod preview thumbnails
- Save scripts for mod fixes and run them from each mod file with one click
- Support nested folder structures through subgroups

### XXMI Launcher Integration

- Automatically detects your XXMI installation by scanning all drives
- Launch games directly from Nahida Desktop via XXMI Launcher
- Built-in d3d11.dll build functionality

### Cloud Drive & File Transfer

- Upload and download files to/from your Nahida Drive
- Multi-threaded uploads with configurable concurrency (up to 16 threads)
- Download directly from GameBanana and nahida.live into character folders registered in the mod manager through Nahida Desktop

### Wuwa Mod Fixer

- One-click install, update, and mod fixing with Wuwa Mod Fixer directly from within the mod manager.

## Download

Latest release can be downloaded either from [Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) page.
Visit the latest release page above to download and install the app.

## FAQ

### Can I use it without a nahida.live account?

Yes. You can use the mod manager and tools, as well as download from the Nahida Live website through Nahida Desktop, without logging in.

### Installation does not proceed

Windows Defender and some antivirus programs may occasionally block the installer. Try adding the installer as an exception or temporarily disabling real-time protection, then try again.

## Credits

- [Wuwa Mod Fixer](https://github.com/Moonholder/Wuwa_Mod_Fixer) by [Moonholder](https://github.com/Moonholder) — used to fix Wuthering Waves mods
- [ZZZ-Mod-Fixer](https://github.com/Vonksdesu/ZZZ-Mod-Fixer) maintained by [VonksDesuuu](https://github.com/Vonksdesu), based on the [original tool](https://gamebanana.com/tools/21671) by [petrascyll](https://gamebanana.com/members/2644630) — hash-fixing and remapping logic used by the native ZZMI Mod Fixer, licensed under the MIT License
- [XXMI-Menu-Maker](https://github.com/XingNian-www/XXMI-Menu-Maker) by [星念](https://github.com/XingNian-www) — strict KeySwap mapping, GUI generation, and resource-rendering behavior used by the native XXMI Menu Maker, licensed under the MIT License; see [the bundled notice](internal/tools/menumaker/NOTICE.md)

## License

Distributed under the [GNU GPLv3](LICENSE).

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fmyparsleycat%2Fnahida-desktop.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fmyparsleycat%2Fnahida-desktop?ref=badge_large)
