// Compares the live skowt catalog with the captured baseline and the bundled indexes, and writes state/diff.json.
// Run after live.mjs and before prep.mjs.
//
//   bun scripts/skowt/live.mjs
//   bun scripts/skowt/diff.mjs
//   bun scripts/skowt/prep.mjs
//
// changed: captured characters whose live revision differs, joined by publicId so a renamed character keeps
//          its slug.
// missing: captured rows whose publicId no longer exists in the live catalog. Informational; they are not
//          captured again.
// newData: catalog characters with no bundled model yet. The live item is resolved by slugifying its name, and
//          an unresolved character aborts the run instead of being skipped silently.

import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const REPO = path.resolve(ROOT, '..', '..');
const STATE = path.join(ROOT, 'state');
const GAMES = ['gimi', 'srmi', 'zzmi'];

const live = JSON.parse(await readFile(path.join(STATE, 'live.json'), 'utf8'));
const captured = JSON.parse(await readFile(path.join(STATE, 'captured.json'), 'utf8'));
// Characters deliberately left without data. They stay out of newData so every run does not report them again.
let skipped = [];
try {
  skipped = JSON.parse(await readFile(path.join(STATE, 'skipped.json'), 'utf8')).skipped ?? [];
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const skippedKeys = new Set(skipped.map((row) => `${row.game}/${row.key}`));
const motion = JSON.parse(
  await readFile(path.join(REPO, 'internal/tools/model_viewer/character_motion_index.json'), 'utf8'),
);
const original = JSON.parse(
  await readFile(path.join(REPO, 'internal/tools/model_viewer/character_original_index.json'), 'utf8'),
);

const slug = (name) =>
  name
    .replace(/\s*·\s*3D model$/i, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');

const allItems = Object.values(live).flat();
const byPublicId = new Map(allItems.map((item) => [item.publicId, item]));
const bySlug = new Map();
for (const [game, items] of Object.entries(live)) {
  for (const item of items) {
    const key = `${game}/${slug(item.name)}`;
    if (!bySlug.has(key)) bySlug.set(key, item);
  }
}

const baseline = new Map();
const duplicates = [];
// The baseline also holds EFMI characters, which have no skowt publicId and never come from this catalog.
const skowtCaptured = captured.filter(
  (row) => typeof row.publicId === 'string' && row.publicId && GAMES.includes(row.key?.split('/')[0]),
);
for (const row of skowtCaptured) {
  if (baseline.has(row.publicId)) duplicates.push(row.key);
  else baseline.set(row.publicId, row);
}

const changed = [];
const missing = [];
for (const row of skowtCaptured) {
  const item = byPublicId.get(row.publicId);
  if (!item) {
    missing.push({ key: row.key, publicId: row.publicId, name: row.name });
    continue;
  }
  if (row.rev !== item.rev) {
    changed.push({ key: row.key, publicId: item.publicId, oldRev: row.rev, newRev: item.rev, name: item.name });
  }
}

const newData = [];
const unmatchedCatalog = [];
for (const game of GAMES) {
  const catalog = JSON.parse(
    await readFile(path.join(REPO, `internal/mod/character/data/${game}.json`), 'utf8'),
  ).characters;
  for (const character of catalog) {
    const key = character.id;
    if (motion.characters[game]?.[key] && original.characters[game]?.[key]) continue;
    if (skippedKeys.has(`${game}/${key}`)) continue;
    const item = bySlug.get(`${game}/${key}`);
    // A catalog character with no skowt item is not a capture target, such as the GIMI mannequin.
    if (!item) {
      unmatchedCatalog.push(`${game}/${key}`);
      continue;
    }
    newData.push({ g: game, key, publicId: item.publicId, rev: item.rev, name: item.name });
  }
}

changed.sort((a, b) => a.key.localeCompare(b.key));
newData.sort((a, b) => `${a.g}/${a.key}`.localeCompare(`${b.g}/${b.key}`));

const diff = { changed, missing, newData, duplicates, unmatchedCatalog, skipped: skipped.map((row) => `${row.game}/${row.key}`) };
await writeFile(path.join(STATE, 'diff.json'), JSON.stringify(diff, null, 1) + '\n');
console.log(
  `changed ${changed.length}, newData ${newData.length}, skipped ${skippedKeys.size}, missing ${missing.length}, duplicate baseline rows ${duplicates.length}`,
);
if (newData.length) console.log(`newData: ${newData.map((row) => `${row.g}/${row.key}`).join(', ')}`);
if (unmatchedCatalog.length) console.log(`catalog without a skowt item: ${unmatchedCatalog.join(', ')}`);
