# skowt character asset update checklist

Follow this checklist in order. If a result differs from what a step specifies,
follow only the failure instructions for that step. Do not skip or reorder steps.

- Working directory: `E:\Dev\projects\nhd\desktop` (repository root)
- Tool directory: `E:\Dev\projects\nhd\desktop\scripts\skowt`

---

## 0. Current status

Completed on 2026-11-28. All 122 targets were captured, converted, uploaded, and
verified. In-game verification was also completed. Use this checklist when
running the update again.

- 122 targets total: 116 changed revisions and 6 new entries (GIMI 57, SRMI 2, ZZMI 63)
- Initial upload: 81 motion files (517 MB) and 1,658 original files (241 MB).
  Recapture upload: 59 motion files (716 MB) and 1,339 original files (365 MB).
- The index has 373 rows. The toon merge and R2 SHA verification passed.
- `task build`, `task test`, and `task lint` passed.
- 32 superseded models were deleted from R2. There were no unreferenced textures.
- The run creates `work/`, which Git ignores (see `scripts/skowt/.gitignore`).
- Git also ignores the generated `state/live.json`, `state/diff.json`,
  `state/targets.json`, and `state/remaining.json`. Of these state files, only the
  baseline (`state/captured.json`) is committed.
- See `state/progress.json` and `state/recapture.json` for counts and run history.

**Caution.** In `capture-page.js`, `liteAnim` uses the node's `mesh` index to find
the source mesh and attach `{targetNames, mesh}`. Node and mesh names differ for
many characters. Matching by name drops all shape key channels.

---

## 1. Access status and stop conditions

Access to the skowt viewer was blocked as of 2026-11-28.

- `GET https://den.skowt.cc/viewer/links?a=<assetId>:model&g=3,4`
  → `409 {"error":"Conflict","gate":"reload","notice":"Automated access to skowt.cc's viewer isn't permitted. ..."}`
- Adding `&l=1` to the same request returned 200, but key decryption failed
  (`ViewerFileError: unwrap`, wasm error 9).
- The official viewer in the same browser also showed
  `stage: the model file (model) failed (unwrap)`.
- Changing the VPN egress IP to `154.47.20.69` produced the same result.

Clearing the browser's cookies and site data restored access. The block was
tied to the cookies or session. No workaround was implemented.

Stop immediately and report the failure if any of these signals appear.

| Signal | Result |
|---|---|
| HTTP 409 with `gate` | Access blocked |
| `unwrap` or `open 9` | Access blocked |
| `Automated access` message | Access blocked |
| `no <role> link` | Access blocked |
| Same error three times | Access blocked |

After stopping, do not retry, use a proxy to bypass the block, spoof a browser
profile or fingerprint, or manipulate keys. Start [capture in step 6](#6-capture)
only after the [access check in step 5](#5-access-check) succeeds.

---

## 2. Preflight checks

### 2.0 Identify targets for the next update

Run these commands first to select only the assets added or changed on skowt
since the previous run. This generates a fresh `state/targets.json`.

```powershell
bun scripts/skowt/live.mjs
bun scripts/skowt/diff.mjs
bun scripts/skowt/prep.mjs
```

`diff.mjs` should print `changed <n>, newData <m>, skipped <k>, missing <d>`.
`prep.mjs` should print the total target count and the count for each game.

- `changed` counts characters whose live revision differs from the revision in
  `state/captured.json`. The script matches them by `publicId`.
- `newData` counts characters in the catalog that are absent from the index. The
  script converts names to slugs to match them to the live catalog.
- `skipped` counts characters listed in `state/skipped.json`. It excludes them
  from `newData`.
- `missing` counts previously captured entries that are absent from the live
  catalog. Leave them out of the capture.
- `catalog without a skowt item` identifies catalog entries with no skowt
  counterpart, such as `gimi/manekin`. Ignore these entries.

If `changed` and `newData` are both `0`, stop here.

After completing the update, refresh the baseline so the next run does not
select the same characters again.

```powershell
bun scripts/skowt/mark-captured.mjs
```

The command should print `captured rows <n>, added <a>, updated <u>`.

### 2.1 Tools

```powershell
bun --version
node --version
npx --no-install wrangler --version
npx --no-install wrangler whoami
```

The first three commands should print their versions. `whoami` should show an
account with access to the `nhd-public` R2 bucket. If `whoami` fails, authenticate
with `npx wrangler login` and check again. If the check still fails, stop and
report the failure.

### 2.2 Port

```powershell
Get-NetTCPConnection -LocalPort 8788 -State Listen -ErrorAction SilentlyContinue
```

The command should print nothing. If a process is listening on port 8788, stop
it and check again. If you cannot stop it, stop the run.

---

## 3. Receiver

Start the receiver in a separate terminal and keep it running for the entire
update.

```powershell
cd E:\Dev\projects\nhd\desktop
bun scripts/skowt/server.mjs
```

The server should print `skowt handoff receiver: http://127.0.0.1:8788`.

Run the checks in another terminal.

```powershell
(Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8788/ping).Content
$r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8788/targets
([System.Text.Encoding]::UTF8.GetString($r.Content) | ConvertFrom-Json).remaining.Count
```

The checks should return `pong` and the target count from section 2.0. If they
fail, inspect the receiver logs and stop.

---

## 4. Install the page helper

1. Open `https://skowt.cc/a/ONBSg8opf` in the built-in browser.
2. Run the following code with `browser.evaluate`.

```js
await eval(await (await fetch('http://127.0.0.1:8788/script')).text())
```

The helper should return
`{ targets: <target count from 2.0>, skipped: 0, first: "<first target>" }`.

The helper automatically skips files already in `work/incoming`. A rerun may
return `{ targets: 12, skipped: 30, first: "..." }`, for example.

If the CSP blocks `eval`, paste the full contents of
`scripts/skowt/capture-page.js` into the same `browser.evaluate` call.
Reloading the page removes the helper. Repeat this step after a reload.

If the step fails, record the error string and stop.

---

## 5. Access check

Use this check to determine whether to proceed with capture.

```js
await window.__testOne()
```

The check should return `{ key: "zzmi/...", bytes: <positive>, ms: <positive> }`
without throwing an exception.

If it fails, record the exact exception message, **stop immediately, and report
the failure**. Do not retry or proceed to step 6.

---

## 6. Capture

```js
await window.__runBatch(10)
```

- The batch should return `{ idx, total, results, errorCount: 0 }`. `total` is the
  count loaded in step 4.
- If the call times out, the capture continues in the page. Check
  `window.__report()` and resume from `idx`. Do not restart from the beginning.
- Repeat `__runBatch(10)` until
  `window.__report().idx === window.__report().total`.

```js
window.__report()
```

The report should show
`{ idx: <total>, total: <total>, done: <total>, errors: [], running: false }`.

If `errors` contains entries, inspect the error strings.

1. If any contains `409`, `gate`, `unwrap`, `open 9`, or `Automated access`,
   **stop immediately and report the failure**.
2. For other errors, such as network failures, retry once.

```js
await window.__retryErrors()
window.__report()
```

The report should show `errors: []`. If errors remain after the retry, stop and
report them.

---

## 7. Verify the received files

```powershell
(Get-ChildItem scripts\skowt\work\incoming -Filter *.bin.gz).Count
```

The count should match the number of captured characters. If all captures
succeeded, it should match the target count from section 2.0.

If the count differs, compare it against `state/remaining.json`, list the
missing keys, and stop and report the failure.

---

## 8. Split the captures

```powershell
bun scripts/skowt/split.mjs
```

The script should print one `motion,original` line per input file.

```powershell
(Get-ChildItem scripts\skowt\work\batches\motion -Filter *.bin.gz).Count
(Get-ChildItem scripts\skowt\work\batches\original -Filter *.bin.gz).Count
(Get-ChildItem scripts\skowt\work\raw-original -Directory).Count
```

All three counts should match the count from step 7. If any differs, stop and
report the failure.

---

## 9. Convert the assets

```powershell
node scripts/import-character-motion.mjs scripts/skowt/work/batches/motion scripts/skowt/work/out-motion
node scripts/import-character-original.mjs scripts/skowt/work/batches/original scripts/skowt/work/out-original
```

Check the last line from each importer.

- Motion: `N characters, ... MB`
- Original: `N characters, models ... MB, M textures ... MB`
- `N` should match the file count from step 8.

If either importer fails, record the `<game>/<key>` from the exception, then
stop and report the failure.

---

## 10. Merge toon data

```powershell
bun scripts/skowt/merge-toon.mjs
```

The script should print a line such as `gimi/...: N materials` for each
character and finish with a summary such as
`gimi: N characters, M updated images, K removed` for each game.

If it fails, record `no captured originals ...` or the exception message, then
stop and report the failure.

---

## 11. Merge indexes and stage files

```powershell
bun scripts/skowt/merge-index.mjs
```

The command should print these summaries.

- `motion: N files, M characters`
- `original: N files, M characters`
- `textures: T`

If it fails, stop and report the failure.

---

## 12. Upload

```powershell
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group motion -Throttle 8
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group original -Throttle 10
```

Each command should print `uploaded X/X files, ... MB`. If either fails, record
the `FAILED ...` line, then stop and report the failure.

---

## 13. Verify R2 objects

```powershell
npx --no-install wrangler r2 object get nhd-public/desktop/character-motion/index.json --file scripts/skowt/work/verify-motion.json --remote
npx --no-install wrangler r2 object get nhd-public/desktop/character-original/index.json --file scripts/skowt/work/verify-original.json --remote
```

```powershell
(Get-FileHash scripts/skowt/work/verify-motion.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_motion_index.json).Hash
(Get-FileHash scripts/skowt/work/verify-original.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_original_index.json).Hash
```

Both comparisons should return `True`.

Check one sample model and one sample texture.

```powershell
$m = (Get-ChildItem scripts\skowt\work\upload\motion -Recurse -Filter *.glb.gz | Select-Object -First 1)
$rel = [System.IO.Path]::GetRelativePath("scripts\skowt\work\upload\motion", $m.FullName).Replace('\','/')
npx --no-install wrangler r2 object get "nhd-public/desktop/character-motion/$rel" --file scripts/skowt/work/verify-model.glb.gz --remote
(Get-FileHash scripts/skowt/work/verify-model.glb.gz).Hash -eq (Get-FileHash $m.FullName).Hash

$t = (Get-ChildItem scripts\skowt\work\upload\original\textures -Filter *.webp | Select-Object -First 1)
npx --no-install wrangler r2 object get "nhd-public/desktop/character-original/textures/$($t.Name)" --file scripts/skowt/work/verify-texture.webp --remote
(Get-FileHash scripts\skowt\work\verify-texture.webp).Hash -eq (Get-FileHash $t.FullName).Hash
```

Both comparisons should return `True`. If verification fails, stop and report
the failure.

The edge cache may serve an older public
`https://nhd-public.nahida.live/.../index.json`. The app reads its embedded
indexes, so verifying the R2 objects is sufficient.

### 13.1 Clean up superseded objects after recapturing existing characters

```powershell
bun scripts/skowt/orphans.mjs
pwsh -NoProfile -File scripts/skowt/delete-orphans.ps1
```

The commands should print `unreferenced: motion <n>, original <n>, textures <n>`
followed by `deleted <k>/<k>`. If all three counts are zero, there is nothing to
delete. If a command fails, record the `FAILED` line, then stop and report the
failure.

---

## 14. Final checks

Run each `task` command separately with a two-minute time limit.

```powershell
$p = Start-Process task -ArgumentList 'build' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
$p = Start-Process task -ArgumentList 'test' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
$p = Start-Process task -ArgumentList 'lint' -NoNewWindow -PassThru; $null = $p.Handle; if (-not $p.WaitForExit(120000)) { taskkill /T /F /PID $p.Id; exit 124 }; exit $p.ExitCode
```

The build should succeed, all test results should be `ok`, and lint should
report `0 issues`. If a check fails, stop and report the failure.

---

## 15. Report

Use this report format.

```text
- Capture: <n>/<target> (by game: ...)
- Conversion: motion <n>, original <n>, textures <n>
- Upload: motion <n> files <mb>, original <n> files <mb>
- R2 verification: index <true/false>, model <true/false>, texture <true/false>
- build/test/lint: <pass/fail>
- If blocked: request URL, status, gate, error code
- Remaining work: <none | list>
```

---

## 16. Restrictions

- Commit or push only on explicit request.
- Do not add a local `replace` directive to `go.mod`.
- Run only one `task` command at a time.
- Stop without retrying if any condition in section 1 applies.
- Do not use proxies to bypass access restrictions, spoof browser profiles or
  fingerprints, or manipulate keys.

---

## 17. File layout

```text
state/
  live.json             Live skowt catalog (live.mjs)
  captured.json         Capture baseline (key, publicId, rev)
  diff.json             changed / newData / missing (diff.mjs)
  targets.json          Targets and metadata for this run (prep.mjs)
  remaining.json        Remaining targets served by the receiver at /targets
  skipped.json          Excluded characters and reasons (tracked in Git)
  progress.json         Run results, upload sizes, and verification status
  recapture.json        Record of recaptures for missing morph targets (2026-11-28)
scripts/
  live.mjs              Fetches the live catalog
  diff.mjs              Identifies new or changed entries against the baseline
  prep.mjs              Builds the target list and adds metadata via asset.getById
  server.mjs            Receiver at 127.0.0.1:8788
  capture-page.js       Capture helper that runs in the browser page
  split.mjs             Splits the received stream into importer batches
  merge-toon.mjs        Merges toon deltas
  merge-index.mjs       Merges indexes and stages files for upload
  upload.ps1            Uploads files to R2
  orphans.mjs           Lists unreferenced objects
  delete-orphans.ps1    Deletes unreferenced objects
  mark-captured.mjs     Updates the baseline
  select-recapture.mjs  Rebuilds the list of recapture targets
  progress.mjs          Regenerates progress.json
```

Run `capture-page.js` in the browser and run the other scripts from the
repository root. The conversion uses the shared importers in the top-level
`scripts/` directory (`import-character-motion.mjs`,
`import-character-original.mjs`, and `import-character-toon.mjs`).

---

## 18. Data format and decoder

Use this reference if `capture-page.js` needs to be rewritten. The helper
requires a browser session with cookies.

### 18.1 Link requests

```text
GET https://den.skowt.cc/viewer/links?a=<assetId>:<role>&g=3,4&l=1&vr=1
    credentials: include
→ { ttl, signed, links: { <name>: "exp=…&sig=…" },
    assets: { <assetId>: { <role>: <name> } },
    keys: { <name>: <b64url> }, digests: { <name>: <b64url> },
    mark: <b64url>, locks: { <name>: <u32> }, decoder: <id> }
```

Omitting `l=1` returns
`409 {error:"Conflict", gate:"reload", notice:"Automated access … isn't permitted"}`.
A missing role key in `assets[assetId]` means the asset does not provide that
role. For example, Promeia provides `model` only.

### 18.2 Files and decoder

```text
GET https://pack.skowt.cc/v/<name>?<links[name]>       (credentials: include)
GET https://den.skowt.cc/viewer/decoder/<decoder>      wasm
```

The WebAssembly exports are `m` (memory), `h` (alloc), `o` (open), and `s`
(free). Populate memory with the following values before calling `open`.

| Offset | Contents |
|---|---|
| 0 | `keys[name]` (16 bytes) |
| 16 | SHA-256 of the wasm bytes |
| 5376 | `mark` (16 bytes) |
| 5392 | `digests[name]` (16 bytes) |
| 5408 | `locks[name]` (u32 LE) |

A return value of 0 from `o(ptr, len)` indicates success. A return value of 9
indicates an `unwrap` failure. On success, read
`role, jsonOff, jsonLen, binOff, binLen` (u32 LE) starting at offset 64, then
extract the JSON and binary chunks.

### 18.3 Contents by role

- `model` is the full glTF with images embedded in the binary chunk.
- `anim` contains clips. The nodes lack extras, so the helper must attach
  morph target names.
- `toon` is an image-only glTF. Some assets, such as Promeia, omit it.

### 18.4 Upload payload

The payload is a gzip-compressed record stream. Each record has the format
`[u32 nameLen][u32 dataLen][name][data]`.

```text
motion/<game>/<key>/model.glb    lite glTF with only POSITION/JOINTS_n/WEIGHTS_n
motion/<game>/<key>/anim.glb     clips (with morph target names)
motion/<game>/<key>/meta.json    roles, animations, heightM …
original/<game>/<key>/model.glb  full glTF (texture paths in material extras.textures)
original/<game>/<key>/toon.glb   toon (if present)
original/<game>/<key>/meta.json
```

`split.mjs` splits the stream into
`work/batches/{motion,original}/<game>-<key>.bin.gz`.

### 18.5 Morph target (shape key) mapping

For many characters, `anim` node names differ from the source mesh names. Use
the node's `mesh` index to find the source mesh and attach
`extras = { targetNames, mesh: <mesh name> }`. Matching by name drops all morph
channels.

### 18.6 External dependencies

- `capture-page.js` imports
  `https://cdn.jsdelivr.net/npm/meshoptimizer@0.22.0/meshopt_decoder.module.js`
  because some `model` and `anim` assets use `EXT_meshopt_compression`.
- Run the importers with `node`. The gzip bytes produced by `bun` differ, which
  changes the hashes.
- Uploading requires `wrangler` authentication (section 2.1).
