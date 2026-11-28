// Marks every target in state/targets.json as captured at its new revision, so the next diff only reports
// what changed after this run.
//
//   bun scripts/skowt/mark-captured.mjs

import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const STATE = path.join(ROOT, 'state');

const captured = JSON.parse(await readFile(path.join(STATE, 'captured.json'), 'utf8'));
const { targets } = JSON.parse(await readFile(path.join(STATE, 'targets.json'), 'utf8'));

const byKey = new Map(captured.map((row) => [row.key, row]));
let added = 0;
let updated = 0;
for (const target of targets) {
  const key = `${target.game}/${target.key}`;
  const rev = target.newRev ?? target.meta?.rev;
  const row = byKey.get(key);
  if (!row) {
    byKey.set(key, { key, publicId: target.publicId, rev, name: target.name });
    added++;
    continue;
  }
  if (row.rev !== rev || row.publicId !== target.publicId) {
    row.rev = rev;
    row.publicId = target.publicId;
    updated++;
  }
}

const rows = [...byKey.values()].sort((a, b) => a.key.localeCompare(b.key));
await writeFile(path.join(STATE, 'captured.json'), JSON.stringify(rows, null, 1) + '\n');
console.log(`captured rows ${rows.length}, added ${added}, updated ${updated}`);
