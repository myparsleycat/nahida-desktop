# Nahida Desktop

Windows desktop client for [nahida.live](https://nahida.live), implemented in Go and Wails v3.

## Development

The project targets Windows amd64. The Wails CLI is pinned as a Go tool, so a separate global Wails installation is not required.

```text
task dev
task lint
go test ./...
pnpm --dir frontend test
```

Frontend dependencies live in `frontend/package.json`. The private package at the repository root contains release automation only.

## Release channels

- `v3` is the `beta` semantic-release channel and produces versions such as `v3.0.0-beta.1`.
- `main` is the stable channel and produces versions such as `v3.0.0`.
- Release workflows are manual. They create a Git tag and a draft GitHub Release; a maintainer publishes the draft after testing its assets.
- Beta builds check published beta and stable releases. Stable builds ignore prereleases.

Run the **Release desktop** workflow with `dry_run` enabled first. Once the computed version is confirmed, run it again with `dry_run` disabled. Production packaging requires `APP_VERSION` and can also be exercised locally:

```text
task package APP_VERSION=3.0.0-beta.1 ARCH=amd64
```

Every draft contains exactly these update assets:

- `nahida-desktop-windows-amd64.exe`
- `nahida-desktop-windows-amd64-installer.exe`
- `SHA256SUMS`

The unsigned NSIS installer is per-user and installs under `%LOCALAPPDATA%\Programs`. Electron v2 installations are not automatically migrated to v3; v3 must initially be installed separately.
