// Compares the studio catalog snapshot with the captured baseline and writes the capture list.
//
//   bun scripts/warfarin/diff.mjs
//
// changed: a captured character whose model file name (the site names it by content hash) differs.
// added:   a live chr_* that is not in the baseline yet.
// missing: a baseline character that the studio no longer lists. Informational.
//
// state/live.json comes from capture-page.js, which posts it to the receiver because
// assets.warfarin.wiki answers 403 outside its own page.

import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const REPO = path.resolve(ROOT, '..', '..');
const STATE = path.join(ROOT, 'state');

const read = (file, fallback) => {
  try {
    return JSON.parse(readFileSync(file, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT' && fallback !== undefined) return fallback;
    throw error;
  }
};

const slugify = (name) =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');

let live;
try {
  live = read(path.join(STATE, 'live.json'));
} catch {
  console.error('state/live.json is missing; run the capture-page helper first (README 4)');
  process.exit(1);
}
const captured = read(path.join(STATE, 'captured.json'), { characters: {} });
const skipped = read(path.join(STATE, 'skipped.json'), { skipped: [] }).skipped ?? [];
const catalog = read(path.join(REPO, 'internal/mod/character/data/efmi.json')).characters;

const slugByKey = new Map(catalog.map((character) => [character.key, character.id]));
const skippedKeys = new Set(skipped.map((row) => row.key));

const changed = [];
const added = [];
for (const [key, row] of Object.entries(live.characters)) {
  const base = captured.characters[key];
  if (!base) {
    if (!skippedKeys.has(key)) added.push(key);
    continue;
  }
  if (base.model !== row.model) changed.push(key);
}

const missing = Object.keys(captured.characters).filter((key) => !(key in live.characters));

const slugs = (key) => slugByKey.get(key) ?? slugify(live.characters[key].name);
const targets = [...changed, ...added]
  .sort()
  .map((key) => ({
    key,
    slug: slugs(key),
    name: live.characters[key].name,
    model: live.characters[key].model,
  }));

const withoutCatalog = targets.filter((target) => !slugByKey.has(target.key));
const siteKeys = new Set(Object.keys(live.characters));
const catalogOnly = catalog.filter((character) => !siteKeys.has(character.key)).map((character) => character.key);

const body = JSON.stringify({ targets }, null, 1);
const diff = {
  version: live.version,
  changed: changed.sort(),
  added: added.sort(),
  missing,
  targets,
  withoutCatalog: withoutCatalog.map((target) => target.key),
  catalogOnly,
};

writeFileSync(path.join(STATE, 'diff.json'), JSON.stringify(diff, null, 1) + '\n');
writeFileSync(path.join(STATE, 'targets.json'), body + '\n');
// The receiver serves remaining.json, so the capture helper can start right after this step.
writeFileSync(path.join(STATE, 'remaining.json'), body + '\n');

console.log(`changed ${changed.length}, added ${added.length}, skipped ${skipped.length}, missing ${missing.length}`);
if (targets.length) {
  console.log(`targets: ${targets.map((target) => `${target.key} (${target.slug})`).join(', ')}`);
}
if (withoutCatalog.length) {
  console.log(`needs an efmi catalog row before it appears in the app: ${withoutCatalog.join(', ')}`);
}
if (catalogOnly.length) {
  console.log(`in the efmi catalog but not on the studio: ${catalogOnly.join(', ')}`);
}
