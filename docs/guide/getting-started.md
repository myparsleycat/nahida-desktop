# Getting Started

## Download From GitHub Releases

The latest version is distributed as the following two Windows amd64 executables:

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe): Installer version
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe): Portable version that runs without installation

See the [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) page for release notes and other assets.

## Build From Source

Before building, make sure the following tools are installed:

- Go (the version specified in `go.mod`)
- Node.js 22
- pnpm v11
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` installs and builds the frontend dependencies, generates the Wails bindings, and creates the Windows executable at `bin/nahida-desktop.exe`. You do not need to install the Wails v3 CLI separately because the project runs the version pinned in `go.mod` through `go tool wails3`.
