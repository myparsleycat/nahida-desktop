# AGENTS.md

- ALWAYS USE PARALLEL TOOLS WHEN APPLICABLE, except for project commands; see "Running commands sequentially".
- Prefer automation: execute requested actions without confirmation unless blocked by missing information, safety, or irreversibility.

## Project

- This is a Windows-only desktop application built with Go and the native UI framework Fairy (`github.com/myparsleycat/fairy`).
- Keep the existing business logic unless the requested change requires otherwise.
- Do not restore Linux, Android, iOS, or macOS build targets.
- Make small, reviewable changes and keep the build and types valid after each unit.

## Go Skills

- Use the `golang-*` skills from `https://github.com/samber/cc-skills-golang` for Go work: style, naming, error handling, testing, concurrency, performance, and library choice.
- `golang-how-to` is the entry point; it routes to the relevant skills and loads several together when a task spans multiple concerns.
- This file takes precedence on conflict: project commands, pinned tools, UI framework wiring, and cross-layer contracts override general skill guidance.

## Layout

- `main.go` calls `app.Run`. `internal/app/app.go` takes the single-instance lock, boots the backend, and runs the `fairy` message loop with the tray and the main window (`internal/app/window.go`, `internal/app/tray.go`).
- The main window shows `ui.Shell` (`internal/ui`): title bar, navigation rail, router, toasts, confirmation dialogs and theme.
- Backend wiring lives in `internal/app/runtime.go`, which takes UI framework capabilities through `uiHost` (`internal/app/host.go`).
- Backend features live in focused packages under `internal/` such as `auth`, `drive`, `gamebanana`, `mod`, `platform`, `setting`, `tools`, `transfer`, and `xxmi`.
- Keep new behavior in the package that owns the feature. Do not create speculative layers or vague `util`, `common`, or `base` packages.
- Self-update (release check, SHA-256 verification, executable swap helper) lives in `internal/selfupdate`; `infra.Updater` owns the update state.

## UI framework

- The UI is built with Fairy, `github.com/myparsleycat/fairy` (Direct3D 11, DirectWrite, flexbox layout, component kit in `fairy/kit`, native dialogs in `fairy/dialog`, tray icons in `fairy/tray`). It is a published module pinned in `go.mod`; update it with `go get github.com/myparsleycat/fairy@<version>`. Read its `README.md` and `docs/README.md` before UI work.
- Windows, trays and menus are created on the UI thread (inside `fairy.Main`, a handler, or `fairy.Do`). From other goroutines use `fairy.Go`, `fairy.Do`, or `Window.Post`.
- Only `internal/app` and `internal/ui` import `fairy`. Feature packages receive UI capabilities as injected functions, as with `uiHost`; do not import `fairy` from them. Screens call service methods directly.
- Backend packages emit events through the `EventEmit` function injected in their options, in the form `domain:kebab-name`. They reach the UI through `ui.Events`, which drops events without a handler and runs handlers on the UI thread; keep the names and payloads stable.
- UI text comes from `internal/ui/locales/*.json`, in i18next format. Add new keys to all four locales. The UI font (IBM Plex Sans KR) is embedded from `internal/ui/fonts`.
- `ui.Route` values are the former React frontend's URL paths, because the start page setting and `fn:navi` events store them. Keep them stable.
- `ui.Deps.Ready` (the runtime's `startup.wait`) blocks until startup maintenance finishes. Screens run mod service calls and setting writes after it (`Shell.background`, `runSettingWrites`); new screens that call services during startup must do the same.
- The single-instance ID `com.nahida.desktop` in `app.go` names the mutex the installer checks (`scripts/nsis-uninstaller.test.mjs`); keep it a string literal and change both together.

## Required Gateways

Each cross-cutting policy below has one owning package. Go through it instead of calling the standard library or a third-party package directly, and take the shared instance wired in `internal/app/runtime.go` through the feature's options rather than constructing a second one. When a gateway lacks something, extend the gateway; do not bypass it.

### Network

- Outbound HTTP goes through the injected `infra.Client`: `Fetch`, `Stream`, or `HTTPClient()` when the caller must own the response body or retry policy. It carries the user's proxy configuration, system-proxy failover, the product User-Agent, and backend status tracking. Do not use `http.DefaultClient`, `http.Get`, or a fresh `http.Client{}` in feature code.
- GitHub REST API calls (`api.github.com`) go only through `github.Client` in `internal/github` (`Releases`, `AllReleases`, `LatestRelease`, `ResolveTagCommit`, `Tree`, `GetBytes`). It applies the process-wide core-rate gate and the persistent release-metadata cache owned by `infra.GitHubRateCoordinator`.
  - Use the one client built in `runtime.go`. `github.New` without `Options.Rate` creates a private coordinator that does not share the budget, which is only acceptable in tests.
  - Prefer `CachedReleases` or `ReleaseTags` for update checks and other repeated or background lookups.
  - Handle `github.ErrRateLimited` (`*github.RateLimitError`, `ResetAt`) as an expected state, not as a failure to retry immediately.
  - Download release assets, tag archives, and raw repository files with `DownloadFile` or `FetchFile`, and build URLs with `ReleaseFileURL` and `TagArchiveURL`. Validate repository input with `Repo.Validate`.
- Download files to disk with the shared `infra.Download` (`File`), which applies the transfer bandwidth limiter, retries, and a `.ntmp` temporary file. Do not copy a response body to the destination by hand.
- A pinned third-party artifact declares its expected SHA-256 next to its URL and verifies it before use.
- Downloads and uploads the user should see, pause, or cancel are registered with `internal/transfer` instead of running as untracked goroutines.

### Filesystem

- Wrap bulk or parallel file work (hashing, copying, compressing, scanning, walking many files) in `diskio.Acquire(ctx, files...)` or `diskio.AcquireDir(ctx, dirs...)` and release the slot when done. It bounds concurrency on rotational disks for the whole process and passes straight through on solid-state and network volumes.
  - Acquire around a leaf file operation only. Code holding a slot must never acquire again, directly or through a callee; nested acquisition can deadlock.
  - Do not add a per-feature worker cap as a substitute. Unrelated features must queue behind the same disk.
- Replace a file by writing a temporary file in the same directory and calling `platform.ReplaceAtomic`. Do not `os.Rename` over an existing file or truncate it in place.
- Compare paths with `platform.SamePathFold` and `platform.SameOrChildPath`, not `==`, `strings.EqualFold`, or `strings.HasPrefix`. Both are lexical. When the physical location matters, such as a write below a junction, resolve with `platform.FinalPath`; `filepath.EvalSymlinks` leaves junctions unresolved.
- Validate and sanitize user-supplied file names with `platform.FS` (`IsValidWindowsFilename`, `SanitizeWindowsFilename`, `GetUniqueName`) and `platform.IsUnaddressableName`.
- Application-owned files live under `~/.nahida-desktop` and are addressed through `appdata.Store` (`Resolve`, `EnsureDir`, and the directory constants in `internal/appdata`). Do not derive those paths from `os.UserHomeDir` in feature code.
- Extract archives of user or unknown origin with `infra.Archive`, which detects the container by content and rejects unsafe entry paths. Code that reads a zip directly must validate entry paths itself.
- Watch files and directories with `internal/watcher` (debounced, settled `ReadDirectoryChangesW`). Do not add another watching library.
- Sort names shown to the user with `platform.NewLocaleLess`, not byte order.

### Persistence

- All access to the application database goes through `internal/db`; `db.Open` fixes the single connection and its pragmas.
- Declare schema changes in `TableSpecs` in `internal/db/schema.go` and let `Reconcile` apply them. Rename a table or column by adding the old name to `Aliases`. Do not write ad-hoc `ALTER TABLE` statements.
- A one-shot data migration runs from `Reconcile` and is gated by its own `SchemaKey*` entry in the schema state so it never runs twice.
- Adding a setting touches both of these: the `Key*` constant and its `allDefinitions` row in `internal/setting/keys.go`, with storage key `{scope}_{snake_case}`, which a test enforces; and its spec in `buildSpecs` in `specs.go`.
- Settings side effects on other services go through `setting.Hooks`. The setting package does not import feature packages.

## Commands

- Windows build: `task build`
- Windows package: `task package APP_VERSION=<version>`
- Go tests: `task test` (runs `go test ./...`)
- Go commands run outside `task` need `-tags nodynamic`; `internal/xxmi` fails to compile without it so `github.com/gen2brain/avif` never loads `libavif.dll` at startup. The Taskfile sets it through `GOFLAGS`.
- Go lint: `task lint`
- Go lint with fixes: `task lint:fix`
- Go formatting: `task fmt -- <files-or-directories...>` (gofmt, goimports, golines; 120 columns)
- Go formatting preview/check: `task fmt:check -- <paths...>` (defaults to the whole repository; writes nothing)
- VS Code Go formatter setup: `task fmt:setup` (repeat after updating the pinned lint module)
- Vulnerability scan: `task vuln`
- Release-script tests from the repository root: `pnpm test`

Do not run `golangci-lint` or `govulncheck` from `PATH`; use the project tasks so the pinned tool versions are used.

### Running commands sequentially

- Run `task` and `pnpm` commands strictly one at a time: one command per tool call, and wait for it to finish before starting the next. Never issue them as parallel tool calls or as background jobs alongside another command.
- Concurrent runs race on shared state: the shell working directory, the `.task` checksum and tool cache, `node_modules`, and build outputs.
- Parallel tool calls remain appropriate for read-only work such as reading files and searching.

### Command time limit

- No `task` command takes more than two minutes locally, including `task build`. A run that passes two minutes is hung, not slow.
- Run every `task` command under a hard two-minute limit that kills the whole process tree. A tool-level timeout is not enough: it can move the command to the background, where it keeps holding the shared state above.
- Use this PowerShell form from the repository root, replacing `build` with the task and its arguments:

  ```powershell
  $p = Start-Process task -ArgumentList 'build' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
  ```

- After a timeout, confirm that no `task`, `go`, or `node` process from the run is left, then retry once. Investigate the cause instead of retrying again if it hangs a second time.
- Leave processes that belong to another checkout or worktree alone.

### Go formatting

- Format changed Go files with `task fmt -- <files...>` and check them with `task fmt:check -- <files...>`.
- Use the formatter settings in `.golangci.yml`. `golines` targets 120 columns with four-column tabs; it does not guarantee wrapping every string, comment, or expression.
- `task fmt` requires explicit paths to avoid accidental repository-wide rewrites. Preview larger scopes with `task fmt:check` first.
- Wrapping is enabled explicitly by the formatting tasks and VS Code. `task lint` and `task lint:fix` retain the existing gofmt/goimports checks while legacy files are migrated incrementally; `lint:fix` is not a substitute for `fmt`.
- In VS Code, open the repository root, install the recommended Go extension, and run `task fmt:setup` once. Repeat setup after changing `golangci-lint.mod` or `golangci-lint.sum`.
- Go saves use the project-pinned `.task/bin/golangci-lint.exe` through the Go extension, passing the current buffer to `fmt --stdin`. The same gofmt/goimports/golines pipeline handles standard formatting, imports, and wrapping. Go's separate organize-imports save action is disabled to avoid competing import rules.

## Code Generation

- `task build` generates the Windows icon, manifest and version resources with the pinned `go-winres` from `build/windows/*.winres.json`. Release versions come from `scripts/write-release-metadata.mjs`.

## Verification

- Verification should be proportional to the change: run the commands for the layers that changed, but keep a successful production build as the final compile and resource check for changes that cross packages.
- For Go-only changes, run focused package tests first (`go test <pkgs>`), then `task test` and `task lint` when the change is ready.
- For changes to application wiring, the UI layer, build configuration, or the installer, run `task build` after the relevant tests and linters. For release scripts, run `pnpm test` from the repository root.
- Run `task vuln` when dependencies, networking, archive handling, process execution, or other security-sensitive code changes.

### Adding and changing tests

- Do not add tests by default for every change. Add or extend tests only for a concrete behavior or failure mode that existing coverage and required checks do not adequately verify. Prioritize consequential risks such as data loss, security, rollback, cancellation, concurrency, and UI/backend contracts. More coverage or a new function alone is not sufficient justification; presentation-only changes, mechanical renames, and trivial forwarding usually need no new tests.
- For bug fixes, a regression test is not mandatory. Skip it when an existing test already reproduces the bug, or when the fixed code and the process around it are self-evident enough that a test would only restate the change. Add or extend one when the bug could plausibly recur unnoticed, such as an edge case, a subtle interaction, or a failure that existing checks did not catch. When you do add one, verify that the relevant assertion fails with the original faulty behavior and passes with the fix; compilation errors or unrelated setup failures do not count. If deterministic automated reproduction is not feasible, explain the limitation and the alternative verification performed.
- Prefer the smallest case in an existing test that covers the risk. Avoid equivalent assertions across test layers, and keep fixtures and fakes proportional to the risk. Do not introduce production abstractions solely to support a low-value test.
- Assert observable behavior and stable contracts rather than mirroring implementation details. Source or generated-text checks are allowed for explicit repository contracts whose runtime verification is unavailable or impractical, such as generated binding surfaces, dependency pins, or startup ordering; do not use them merely to assert that an implementation fragment exists.
- Follow the Verification rules regardless of whether new tests are added. Preserve meaningful existing coverage and follow GitHub Actions test compatibility when adding or changing tests.
- When tests are added or extended, briefly state the concrete risk they cover in the final response.

### GitHub Actions test compatibility

- When writing or changing tests, inspect the relevant `.github/workflows` and `.github/actions` validation steps and account for the Windows runner environment, not just the local workstation.
- Treat the CI runner as a clean machine: do not assume games, launchers, optional executables, GPUs or vendor drivers, registry keys, user settings, credentials, or caches exist. Identify every host dependency reached before the assertion, including earlier validation and preparation stages.
- Unit and service-flow tests must inject per-instance fakes or isolated fixtures for host dependencies, including Windows registry access, driver APIs, installed-program discovery, process lists, and external services. `t.TempDir()` and temporary profile environment variables do not isolate `HKEY_CURRENT_USER`, machine-wide state, or driver settings.
- Never read or modify the developer's real game settings, registry records, driver profiles, or installed applications to make a test pass. Test native OS access separately with disposable resources owned by the test; use an isolated subprocess for process-wide registry overrides or other global state.
- Cover the relevant absent, unavailable, enabled, and failing dependency states explicitly. A test that expects a later-stage result must provide deterministic earlier-stage dependencies; do not bypass product checks, weaken assertions, seed real user settings, or skip a test because optional software or hardware is missing.
- Do not assume `t.TempDir()`, `TEMP`, `TMP`, the checkout, or the user profile uses a particular drive, username, spelling, or long path name. GitHub Actions may provide Windows 8.3 aliases such as `RUNNER~1`; the same physical path can have different textual representations.
- For filesystem behavior, use isolated temporary fixtures and cover relevant path aliases, spaces, case differences, and supported junctions. Normalize paths consistently before lexical comparisons, and use physical file identity when testing whether aliases refer to the same file.
- Reproduce environment-dependent failures with a regression test that exercises the CI condition locally. Use Windows APIs to obtain real short path aliases instead of hard-coding runner paths; skip only when the filesystem lacks the required capability, with an explicit reason.
- Keep tests independent of workstation state and execution order. Avoid process-wide environment changes in parallel tests; use per-test inputs, or an isolated subprocess when environment changes are necessary. Use synchronization for concurrent behavior instead of relying on the runner matching local timing.
- Validate with the project commands used by CI. A local pass under default environment settings alone does not verify a fix for a runner-specific failure.
- For a host-dependent CI regression, verify both the reported clean-runner condition and deterministic fixture behavior locally. Keep concurrent tests isolated with per-instance dependencies; do not replace package-global functions or registry roots in parallel tests.

## Error Logging

- For user actions, log the original backend error before returning it when the UI will show only a generic fallback message.
- Include enough structured context to diagnose the failure without reproduction: service/action name, user-facing entity name, relevant domain identifiers, current operation or stage, input and resolved paths, external URLs or executable paths when relevant, and rollback or cleanup state.
- For multi-step operations, track and log the current stage and any registered rollback or cleanup state.
- Preserve established sentinel messages or domain error codes when callers depend on them. This includes `CODE:detail` prefixed errors such as `XXMI_RUNTIME_CORRUPTED:` and `infra.ContractError` text.
- Never log secrets, session cookies, authorization headers, tokens, or unredacted sensitive user data. Pass URLs through `infra.SanitizeLogURL` before logging them.
- Log through the injected `infra.Log`. `log/slog` and `fmt.Print*` are not routed to `desktop.log`.
- Attach context with `infra.AnnotateError` in inner layers and emit with `infra.ReportError` at the layer that owns the operation. A reported error is marked, so the UI and transfer boundaries do not log it again; do not log the same failure at several levels.
- Cancellation by the user or by shutdown is not a failure. Check `infra.IsCancellationError` instead of logging it.
- Use `infra.WithCause` when a domain contract hides the underlying cause from the caller but the log still needs it.
- Use `infra.DiagnosticThrottle` for a failure that repeats in a loop and `infra.DiagnosticBatch` for many failures of one operation.

## Go Tools

- Pin generators with `go get -tool` and run them via `go tool`.
- Do not add a `tools.go` pin file.
- Keep `golangci-lint` in `golangci-lint.mod`, not `go.mod`.
- Keep `govulncheck` in `govulncheck.mod`, not `go.mod`.
- Tests use the standard `testing` package. `testify` is not a dependency; do not add it.
- A `//nolint` directive must name the specific linter and give an explanation; `nolintlint` rejects anything else.

### Updating lint and vulnerability tools

- Update the project-pinned `golangci-lint` with:

  ```text
  go get -tool -modfile="./golangci-lint.mod" github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
  ```

- Update the project-pinned `govulncheck` with:

  ```text
  go get -tool -modfile="./govulncheck.mod" golang.org/x/vuln/cmd/govulncheck@latest
  ```

## Comments

- These rules apply to Go code, including method documentation and inline comments.
- Do not add comments to self-explanatory methods or code. Avoid restating a method name, signature, assignment, or obvious control flow in prose.
- Add comments only when they explain non-obvious intent, constraints, surprising behavior, or a contract that the code alone does not convey. Preserve required tool directives and documentation required by project tooling.

## Blank Lines

Treat a blank line as a paragraph break: one blank line ends one topic. These rules apply to Go code.

- Always separate top-level declarations, such as functions, types, and const blocks, with a single blank line.
- Inside a function, insert a blank line only when the topic changes: validation, preparation, transformation, output.
- Keep tightly coupled lines together, such as an error check with its immediate handling or an assignment with its next use.
- Put a blank line above a comment that introduces a new step.
- Never write two consecutive blank lines or blank lines at the start or end of a block. Both gofmt and oxfmt collapse multiples to one, and both strip blank lines at the start or end of a block.
- Formatters never add meaningful blank lines, so paragraphing is a human decision. oxfmt preserves single blank lines, so paragraphs survive formatting.
- Prefer blank-line paragraphs inside one function over extracting single-use helpers.

```go
func (s *service) sync(ctx context.Context, id string) error {
	mod, err := s.repo.get(ctx, id)
	if err != nil {
		return fmt.Errorf("load mod %s: %w", id, err)
	}

	items, err := s.fetch(ctx, mod)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", mod.Name, err)
	}
	cleaned := lo.Filter(items, func(f file, _ int) bool {
		return f.valid
	})

	if len(cleaned) == 0 {
		return nil
	}
	return s.store.save(ctx, mod, cleaned)
}
```

Three paragraphs: load, fetch and filter, save.

## Git Revert

When reverting multiple commits, revert them one at a time from newest to oldest to reduce conflicts. Use `--no-commit` for all but the final revert, then create one commit.

## Commit

Commit messages must follow Conventional Commits:

```text
<type>[optional scope]: <description>
```

- Do not use a body or footer.
- Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, and `revert`.
- Add a scope when it clarifies the affected area.
- Keep the description brief, concrete, and imperative.

Examples:

```text
feat(auth): add OAuth login
fix(mod): preserve rollback error
docs: update build instructions
refactor(store): simplify user state
test(transfer): cover cancellation cleanup
chore: update dependencies
```
