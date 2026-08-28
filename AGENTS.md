# AGENTS.md

## Commands

- Dev: `task dev`
- Lint: `task lint` / `task lint:fix`
- Vuln: `task vuln`
- Do not run `golangci-lint` or `govulncheck` from PATH.

## Go tools

- Pin generators with `go get -tool` and run them via `go tool`.
- Do not add a `tools.go` pin file.
- Keep `golangci-lint` in `golangci-lint.mod`, not `go.mod`.
- Keep `govulncheck` in `govulncheck.mod`, not `go.mod`.
- Do not add unused tools.

### Updating lint and vulnerability tools

- Update the project-pinned `golangci-lint` with:

  ```text
  go get -tool -modfile="./golangci-lint.mod" github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
  ```

- Update the project-pinned `govulncheck` with:

  ```text
  go get -tool -modfile="./govulncheck.mod" golang.org/x/vuln/cmd/govulncheck@latest
  ```

## Go libraries

- Prefer the Go standard library when it provides an equally clear solution.
- Use `github.com/samber/lo` for common collection transformations when it reduces boilerplate and makes intent clearer.
- Prefer `lo` for operations such as `Map`, `Filter`, `FilterMap`, `GroupBy`, `KeyBy`, `UniqBy`, `Find`, `Chunk`, and similar collection helpers.
- Before adding a generic project-local helper, check the standard library and `lo` first.
- Do not force `lo` into complex control flow. Prefer a normal `for` loop for branching, early exits, multiple state updates, or performance-sensitive code.
- Avoid deeply nested `lo` pipelines and unnecessary intermediate allocations. Combine operations with helpers such as `FilterMap` when appropriate.
- Do not use `lo/mutable` or `lo/parallel` by default. Use them only when mutation or concurrency is intentional and justified.

## Go naming

Short, clear names. Prefer stdlib package names as a reference.

### Packages

- Lowercase letters only, one singular word: `time`, `http`.
- No camelCase (`computeServiceClient`) or snake_case (`priority_queue`).
- Avoid vague names: `base`, `util`, `common`, `lib`, `misc`.
- Match the directory name when practical.
- Exception: `package foo_test` in the same directory for black-box tests (unexported APIs stay hidden; useful to break import cycles).

### Files

- Snake_case: `addressed_types.go`, `addressed_types_test.go`.

### Directories

- Lowercase, short, and clear. Prefer one word that matches the package.
- Do not use kebab-case.

### Functions, types, structs

- CamelCase. Exported = initial capital (`Contents`); unexported = initial lower (`contents`).

### Receivers

- One or two letters, as short as possible (`Client` → `c` or `cl`).
- Use the same receiver name for a type everywhere.
- No adjectives: `httpClient` and `DBCreator` both use `c`.

### Variables and parameters

- Prefer short names; length should grow with scope.
- Reusing the receiver letter for a parameter of the same type is fine.
- Common abbreviations are OK (`Config` → `conf`, `String` → `str`). Do not invent opaque shortenings.
- Map/channel comma-ok uses `ok`: `id, ok := users[userID]`.

### Errors

- Sentinel / package-level error vars use an `Err` prefix: `var ErrInternal = errors.New(...)`.
- Local error handling uses `err`: `data, err := os.ReadFile(src)`.
- Keep `err` scoped with `:=` and `if err != nil`.

### Initialisms

- Keep well-known initialisms consistent in case: `URL` not `Url`, `HTTP` not `Http`, `ID` not `Id`.

## Project

- WIP port of Electron `nahida-desktop` to Go + Wails v3 beta.
- Source of truth for Electron behavior is that desktop tree, not a redesign.
- Windows only. Do not restore Linux, Android, iOS, or macOS build targets.

## Porting

- Keep the existing frontend UI, state management, and business logic unless a Wails/Go substitution requires a change.
- Remove Electron `main`, `preload`, `ipcMain`, and `ipcRenderer` dependencies.
- Replace IPC with Wails v3 Go services and generated bindings.
- Find Electron API uses (`BrowserWindow`, `dialog`, `menu`, `tray`, filesystem, `process`, `shell`, and similar) and replace them with Wails v3 or the Go standard library.
- Do not call Node.js APIs from the renderer.
- Do not do a wholesale refactor. Port in place.

## Order

1. Analyze project structure and Electron dependencies first.
2. Write an Electron API → Wails v3 / Go mapping.
3. Port in small sequential units.
4. Split the Go backend into feature services.
5. Call the backend from the frontend only through generated bindings.
6. Keep existing features and UX as close as possible.
7. After each change, fix build and type errors.

## Wails

- Implement against the current Wails v3 API. Do not use Wails v2 APIs.
- If documentation and the installed Wails v3 source/API disagree, follow the installed source/API.
- Wails is a fork of `wailsapp/wails`, not vendored here. Repo: `https://github.com/myparsleycat/wails`, branch `v3-nahida`, local checkout `E:\Dev\projects\nhd\wails`.
- Pin it in `go.mod` with `replace github.com/wailsapp/wails/v3 => github.com/myparsleycat/wails/v3 <tag>`. Do not copy the fork into `third_party`.

## Layout

- Wails v3 services are wired in `internal/app/runtime.go`.
- Most `internal/*` packages are still stubs. Do not invent layers.
- Frontend is still the Wails React template. Do not restyle it unless asked.
