// Lists the staged uploads that the embedded indexes no longer reference, then writes work/r2-orphans.json for
// delete-orphans.ps1. Run it after a merge that replaced existing characters, before work/upload is deleted.
//
//   bun scripts/skowt/orphans.mjs
//   pwsh -NoProfile -File scripts/skowt/delete-orphans.ps1

import { readdirSync, readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { gunzipSync } from 'node:zlib';

const ROOT = path.resolve(import.meta.dir);
const REPO = path.resolve(ROOT, '..', '..');
const STAGE = path.join(ROOT, 'work', 'upload');

const glb = (buf) => {
  const len = buf.readUInt32LE(12);
  return JSON.parse(buf.toString('utf8', 20, 20 + len));
};

const walk = (dir, base = dir, out = []) => {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, base, out);
    else out.push(path.relative(base, full).replaceAll('\\', '/'));
  }
  return out;
};

const referenced = (index) => {
  const files = new Set();
  for (const rows of Object.values(index.characters)) {
    for (const row of Object.values(rows)) if (row.file) files.add(row.file);
  }
  return files;
};

const textureRefs = (file) => {
  const names = new Set();
  const visit = (value) => {
    if (value == null || typeof value !== 'object') return;
    if (typeof value.file === 'string' && value.file.startsWith('textures/')) names.add(value.file.slice(9));
    for (const child of Object.values(value)) visit(child);
  };
  for (const material of glb(gunzipSync(readFileSync(file))).materials ?? []) visit(material.extras?.textures);
  return names;
};

const load = (name) =>
  JSON.parse(readFileSync(path.join(REPO, `internal/tools/model_viewer/character_${name}_index.json`), 'utf8'));

const report = { motion: [], original: [], textures: [] };

for (const [name, index] of [
  ['motion', load('motion')],
  ['original', load('original')],
]) {
  const dir = path.join(STAGE, name);
  let files = [];
  try {
    files = walk(dir).filter((file) => file.endsWith('.glb.gz'));
  } catch {
    console.error(`no staged ${name} files at ${dir}; run merge-index first`);
    process.exit(1);
  }
  const refs = referenced(index);
  for (const file of files) {
    if (!refs.has(file)) report[name].push(`desktop/character-${name}/${file}`);
  }
}

const models = walk(path.join(STAGE, 'original')).filter((file) => file.endsWith('.glb.gz'));
const used = new Set();
for (const model of models) for (const name of textureRefs(path.join(STAGE, 'original', model))) used.add(name);

let textures = [];
try {
  textures = readdirSync(path.join(STAGE, 'original', 'textures'));
} catch {}
for (const name of textures) {
  if (!used.has(name)) report.textures.push(`desktop/character-original/textures/${name}`);
}

mkdirSync(path.join(ROOT, 'work'), { recursive: true });
writeFileSync(path.join(ROOT, 'work', 'r2-orphans.json'), JSON.stringify(report, null, 1) + '\n');
console.log(
  `unreferenced: motion ${report.motion.length}, original ${report.original.length}, textures ${report.textures.length}`,
);
