# Warfarin (EFMI) character asset update checklist

Follow this checklist in order. If a result differs from what a step specifies,
follow only the failure instructions for that step. Run the commands from the
repository root at `E:\Dev\projects\nhd\desktop`.

This checklist follows the same structure as `scripts/skowt/`. **All fetches
run inside the Warfarin site page.** `assets.warfarin.wiki` returns 403 for
requests made outside the page.

---

## 0. Current status

- The EFMI catalog matches all 36 live `chr_*` entries. The motion and original
  indexes each have 36 rows.
- The baseline was recorded in `state/captured.json` as of 2026-11-28.
- Fetching and converting `arclight` again with this tool produced byte-for-byte
  matches with the existing outputs (motion `fbd05e6cd37d`, original
  `ee3e776d0c9e`).
- EFMI skips the toon merge because the viewer uses its own shading.

---

## 1. Source layout

```text
https://assets.warfarin.wiki/studio/<version>/data/catalog-v8.json
  models[<chr_key>] = { name, model: { glb: "<hash>.bin" }, clips: "<hash>.json", ... }
  → Character entries are the 36 items in models whose keys begin with chr_.

<version>/data/<hash>.json        Clip list [{ file, name, source, duration, samples, loop, bytes }]
<version>/models/<hash>.bin       Model GLB (first 16 characters of the SHA-256 hash)
<version>/clips/<hash>.bin        Clip GLB (one animation)
<version>/textures/<hash>.bin     External image buffer referenced by the model
```

The model GLB's `buffers[1..]` refer to `../textures/<hash>`. The actual
filename is `<hash>.bin`.

---

## 2. Preflight checks

```powershell
bun --version
node --version
npx --no-install wrangler whoami
Get-NetTCPConnection -LocalPort 8789 -State Listen -ErrorAction SilentlyContinue
```

Bun and Node should print their versions. `whoami` should show an account
with access to `nhd-public`. No process should be listening on port 8789.

If a check fails, stop the process using the port or authenticate with
`npx wrangler login`, as applicable, then check again. If the issue persists,
stop the run.

---

## 3. Receiver

Start the receiver in a separate terminal and leave it running.

```powershell
bun scripts/warfarin/server.mjs
```

The server should print `warfarin receiver: http://127.0.0.1:8789/`.

---

## 4. Install the page helper

1. Open `https://studio.warfarin.wiki/` in the built-in browser.
2. Run the following code with `browser.evaluate`.

```js
await eval(await (await fetch('http://127.0.0.1:8789/script')).text())
```

The helper should return
`{ version: "https://assets.warfarin.wiki/studio/<32hex>/", characters: 36, targets: <n>, skipped: 0, first: "<slug>" }`.

The receiver saves the catalog to `state/live.json` via `POST /catalog`. If
`eval` is blocked, paste the full contents of `scripts/warfarin/capture-page.js`.
Reloading the page removes the helper. Repeat this step after a reload.

---

## 5. Identify targets

Run this command after step 4 creates `state/live.json`.

```powershell
bun scripts/warfarin/diff.mjs
```

The command should print the counts and target list.

```text
changed <n>, added <m>, skipped <k>, missing <d>
targets: ...
```

- `changed` counts characters whose live model filename (content hash) differs
  from the baseline.
- `added` counts `chr_*` entries absent from the baseline.
- `skipped` counts the exclusions listed in `state/skipped.json`.
- `missing` counts baseline entries that have disappeared from the studio. This
  count is informational.

If `changed` and `added` are both zero, stop here.

If the command prints `needs an efmi catalog row ...`, the character has no
entry in `internal/mod/character/data/efmi.json` and will not appear in the app.
Update the catalog before continuing.

---

## 6. Access check

Use this check to determine whether to proceed with capture.

```js
await window.__war.loadTargets()   // Reload the target list from step 5.
await window.__war.testOne()
```

The check should return
`{ key: "chr_...", slug: "...", bytes: <positive>, ms: <positive> }` without
throwing an exception.

If it fails, record the exact error string, **stop immediately, and report the
failure**. Do not proceed to step 7.

---

## 7. Capture

```js
await window.__war.runBatch(5)
window.__war.report()
```

- Allow about 10–15 seconds per character (a 4 MB model, 15 clips, and 28
  textures), with a two-second interval.
- Repeat until `report().idx === report().total`.
- For network errors, retry once with `await window.__war.retryErrors()`.
- The helper automatically skips files already in `work/incoming`.

The report should show
`{ idx: <total>, total: <total>, done: <total>, errors: [], running: false }`.

---

## 8. Split the captures

```powershell
bun scripts/warfarin/split.mjs
```

The script should print `efmi-<slug>.bin.gz motion,original` for each input file
and create `work/batches/{motion,original}/efmi-<slug>.bin.gz` and
`work/raw-original/efmi/<slug>/...`.

---

## 9. Convert the assets

```powershell
node scripts/import-character-motion.mjs scripts/warfarin/work/batches/motion scripts/warfarin/work/out-motion
node scripts/import-character-original.mjs scripts/warfarin/work/batches/original scripts/warfarin/work/out-original
```

Check the output from each importer.

- Motion: `efmi/<slug>: N clips, ... -> ... MB (rest ..., unmatched 0, shape keys 0)`
- Original: `efmi/<slug>: N vertices, ... ; T textures ...`

If `unmatched` is nonzero, stop and report the failure.

---

## 10. Merge indexes and upload

Pass the Warfarin working directory to the shared skowt scripts.

```powershell
bun scripts/skowt/merge-index.mjs scripts/warfarin/work
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group motion -Work scripts/warfarin/work/upload -Throttle 8
pwsh -NoProfile -File scripts/skowt/upload.ps1 -Group original -Work scripts/warfarin/work/upload -Throttle 10
```

The merge should print `motion: N files, <rows> characters`,
`original: N files, ...`, and `textures: T`. Each upload should print
`uploaded X/X files, ... MB`.

Use the same R2 destinations as skowt (`nhd-public/desktop/character-motion` and
`.../character-original`).

Run the cleanup only after recapturing existing characters.

```powershell
bun scripts/skowt/orphans.mjs scripts/warfarin/work/upload
pwsh -NoProfile -File scripts/skowt/delete-orphans.ps1 -Manifest scripts/warfarin/work/r2-orphans.json
```

---

## 11. Verify R2 objects

```powershell
npx --no-install wrangler r2 object get nhd-public/desktop/character-motion/index.json --file scripts/warfarin/work/verify-motion.json --remote
(Get-FileHash scripts/warfarin/work/verify-motion.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_motion_index.json).Hash
npx --no-install wrangler r2 object get nhd-public/desktop/character-original/index.json --file scripts/warfarin/work/verify-original.json --remote
(Get-FileHash scripts/warfarin/work/verify-original.json).Hash -eq (Get-FileHash internal/tools/model_viewer/character_original_index.json).Hash
```

Both comparisons should return `True`. Verify a sample model and texture
using the procedure in section 13 of `scripts/skowt/README.md`.

---

## 12. Update the baseline

```powershell
bun scripts/warfarin/mark-captured.mjs
```

The command should print `baseline rows <n>, added <a>, updated <u>`.
Adding `--all` rebuilds the baseline from the entire live catalog. This option
is for initial setup, which was completed on 2026-11-28.

---

## 13. Report

```text
- Capture: <n>/<target>
- Conversion: motion <n>, original <n>, textures <n>
- Upload: motion <n> files <mb>, original <n> files <mb>
- R2 verification: index <true/false>
- build/test/lint: <pass/fail>
- On failure: target slug, request URL, status, error message
```

Run `task build`, `task test`, and `task lint` separately, with a two-minute
time limit for each command.

---

## 14. Data format

### 14.1 Upload records

`split.mjs` splits the gzip-compressed record stream
(`[u32 nameLen][u32 dataLen][name][data]`) without changing the records.

```text
motion/efmi/<slug>/model.glb        lite model (see section 14.3)
motion/efmi/<slug>/clips/<name>.glb clip GLB
motion/efmi/<slug>/meta.json        { source, key, name, clips[], materials[] }
original/efmi/<slug>/model.glb      full model GLB
original/efmi/<slug>/textures/<hash> external image buffer (without the .bin extension)
original/efmi/<slug>/meta.json      { source, key, name }
```

Each `meta.clips[]` entry has `{ name, source, duration, samples, loop }`.
`name` must match a key in `EFMI_CLIPS` in `import-character-motion.mjs`.

### 14.2 Clip selection and order

Use the keys of `EFMI_CLIPS` as the allowlist.

```text
idle_loop run_loop sprint_loop walk_loop battle_loop battle_normal_skill battle_skill_ult
gacha_ani gacha_ani_loop interact_sit_loop relax_loop relax_sp_01 relax_sp_02
ui_overview_loop ui_relax_loop uiteam_idle_01
```

The site names clips `A_actor_<actor>_<name>`. Strip the prefix to get
`<name>`. The list in `capture-page.js` uses the same order as the 2026-11-28
batch. Changing the order changes the resulting file hashes when the same
character is recaptured.

### 14.3 Lite model

`import-character-motion.mjs` reads `primitives[].extras.material` and
`extras.indices` from each mesh. The motion `model.glb` must use the lite
format with the following data.

- Keep only `name, translation, rotation, scale, matrix, children` on nodes.
- Keep only meshes on skinned nodes (`mesh != null && skin != null`), with
  attributes `POSITION, JOINTS_0, WEIGHTS_0, JOINTS_1, WEIGHTS_1`.
- Set `extras = { material: <material name>, indices: <index count> }` on each
  primitive.
- Keep `joints` and `inverseBindMatrices` on skins, plus `skeleton` if present.
- Copy only the accessors and buffer views used by the data above.

The full model lacks the material and index metadata that the importer reads,
so passing it directly changes the conversion result.

---

## 15. File layout

```text
state/
  live.json       Studio catalog snapshot (step 4, ignored by Git)
  captured.json   Capture baseline: chr_key -> { name, model } (tracked in Git)
  diff.json       changed / added / missing / targets (ignored by Git)
  targets.json    Targets for the current run (ignored by Git)
  remaining.json  Remaining targets served by the receiver at /targets (ignored by Git)
scripts/
  server.mjs          Receiver at 127.0.0.1:8789
  capture-page.js     Page capture helper that also sends the catalog
  split.mjs           Splits the record stream
  diff.mjs            Compares the live catalog with the baseline to identify targets
  mark-captured.mjs   Updates the baseline
  README.md
```

Conversion, index merging, and uploads use the shared scripts
`scripts/import-character-{motion,original}.mjs`,
`scripts/skowt/merge-index.mjs`, `scripts/skowt/upload.ps1`, and
`scripts/skowt/orphans.mjs`.
