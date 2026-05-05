# Getting Started

## Download From GitHub Releases

On the [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) page, click `Nahida-Desktop-Setup-<version>.exe` to download the latest installer.

## Build From Source

Before building, make sure the following tools are installed:

- pnpm v10
- Rust toolchain, including `cargo` and `rustc`

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
pnpm install && pnpm run build:native
pnpm run build:win
```
