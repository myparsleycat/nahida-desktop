// Records the captured model file of every target in state/captured.json, so the next diff only reports what
// changed after this run. Run it after the capture has been split and converted.
//
//   bun scripts/warfarin/mark-captured.mjs            targets from state/targets.json
//   bun scripts/warfarin/mark-captured.mjs --all      seed the baseline from the whole studio catalog

import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const STATE = path.join(ROOT, 'state');

const read = (file, fallback) => {
  try {
    return JSON.parse(readFileSync(file, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT' && fallback !== undefined) return fallback;
    throw error;
  }
};

const live = read(path.join(STATE, 'live.json'));
const captured = read(path.join(STATE, 'captured.json'), { characters: {} });
captured.characters ??= {};

const all = process.argv.includes('--all');
const keys = all
  ? Object.keys(live.characters)
  : read(path.join(STATE, 'targets.json'), { targets: [] }).targets.map((target) => target.key);

let added = 0;
let updated = 0;
for (const key of keys) {
  const row = live.characters[key];
  if (!row) continue;
  const known = captured.characters[key];
  if (!known) added++;
  else if (known.model !== row.model) updated++;
  captured.characters[key] = { name: row.name, model: row.model };
}

captured.version = live.version;
captured.updated = new Date().toISOString();
const rows = Object.fromEntries(Object.entries(captured.characters).sort(([a], [b]) => a.localeCompare(b)));
writeFileSync(
  path.join(STATE, 'captured.json'),
  JSON.stringify({ version: captured.version, updated: captured.updated, characters: rows }, null, 1) + '\n',
);
console.log(`baseline rows ${Object.keys(rows).length}, added ${added}, updated ${updated}`);
