// Merges the delta index rows into the embedded indexes and stages the files to upload.
//
//   bun scripts/skowt/merge-index.mjs [work-dir]
//
// work-dir defaults to scripts/skowt/work; scripts/warfarin passes its own.

import { copyFile, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const HANDOFF = path.resolve(import.meta.dir);
const REPO = path.resolve(HANDOFF, '..', '..');
const ROOT = process.argv[2] ? path.resolve(process.argv[2]) : path.join(HANDOFF, 'work');
const STAGE = path.join(ROOT, 'upload');

async function loadIndex(file) {
  return JSON.parse(await readFile(file, 'utf8'));
}

function merge(base, delta) {
  const out = { format: 1, characters: structuredClone(base.characters) };
  for (const [game, rows] of Object.entries(delta.characters)) {
    out.characters[game] ??= {};
    Object.assign(out.characters[game], rows);
  }
  const sorted = {};
  for (const game of Object.keys(out.characters).sort()) {
    sorted[game] = Object.fromEntries(
      Object.entries(out.characters[game]).sort(([a], [b]) => a.localeCompare(b)),
    );
  }
  return { format: 1, characters: sorted };
}

async function stageFiles(outDir, targetDir) {
  let count = 0;
  for (const game of await readdir(outDir, { withFileTypes: true })) {
    if (!game.isDirectory()) continue;
    const src = path.join(outDir, game.name);
    const dst = path.join(targetDir, game.name);
    await mkdir(dst, { recursive: true });
    for (const file of await readdir(src)) {
      if (!file.endsWith('.glb.gz')) continue;
      await copyFile(path.join(src, file), path.join(dst, file));
      count++;
    }
  }
  return count;
}

for (const [name, embedded, out] of [
  ['motion', path.join(REPO, 'internal/tools/model_viewer/character_motion_index.json'), 'out-motion'],
  ['original', path.join(REPO, 'internal/tools/model_viewer/character_original_index.json'), 'out-original'],
]) {
  const base = await loadIndex(embedded);
  const delta = await loadIndex(path.join(ROOT, out, 'index.json'));
  const merged = merge(base, delta);
  const text = `${JSON.stringify(merged, null, 2)}\n`;
  await mkdir(path.join(STAGE, name), { recursive: true });
  await writeFile(path.join(STAGE, name, 'index.json'), text);
  await writeFile(embedded, text);
  const count = await stageFiles(path.join(ROOT, out), path.join(STAGE, name));
  console.log(`${name}: ${count} files, ${Object.values(merged.characters).reduce((n, r) => n + Object.keys(r).length, 0)} characters`);
}

let textures = [];
try {
  textures = await readdir(path.join(ROOT, 'out-original', 'textures'));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
await mkdir(path.join(STAGE, 'original', 'textures'), { recursive: true });
for (const file of textures) {
  await copyFile(path.join(ROOT, 'out-original', 'textures', file), path.join(STAGE, 'original', 'textures', file));
}
console.log(`textures: ${textures.length}`);
