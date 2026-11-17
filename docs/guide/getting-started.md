# Getting Started

## Download From GitHub Releases

The latest version is distributed as the following two Windows amd64 executables:

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe): Installer version
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe): Portable version that runs without installation

See the [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) page for release notes and other assets.

`SHA256SUMS` on the release page lists the checksums of both files. Nahida Desktop runs on 64-bit Windows only. GameBanana sign-in uses the Microsoft Edge WebView2 Runtime; the installer installs it, and the portable version requires it to be installed already.

## Build From Source

Building requires the following tools:

- Go (the version specified in `go.mod`)
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` creates the Windows executable at `bin/nahida-desktop.exe`.
