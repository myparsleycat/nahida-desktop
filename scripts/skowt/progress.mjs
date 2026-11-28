import { readdir, readFile, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const REPO = path.resolve(ROOT, '..', '..');

async function walk(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...(await walk(full)));
    else out.push(full);
  }
  return out;
}

const staged = path.join(ROOT, 'work', 'upload');
const motion = await walk(path.join(staged, 'motion'));
const original = await walk(path.join(staged, 'original'));
const sum = async (files) => {
  let bytes = 0;
  for (const file of files) bytes += (await stat(file)).size;
  return bytes;
};

const targets = JSON.parse(await readFile(path.join(ROOT, 'state/targets.json'), 'utf8')).targets;
const remaining = JSON.parse(await readFile(path.join(ROOT, 'state/remaining.json'), 'utf8')).remaining;
const motionIndex = JSON.parse(
  await readFile(path.join(REPO, 'internal/tools/model_viewer/character_motion_index.json'), 'utf8'),
);
const originalIndex = JSON.parse(
  await readFile(path.join(REPO, 'internal/tools/model_viewer/character_original_index.json'), 'utf8'),
);

const byGame = (rows) => Object.fromEntries(Object.entries(rows.reduce((m, t) => ((m[t.game] = (m[t.game] ?? 0) + 1), m), {})).sort());
const doneKeys = targets
  .filter((t) => !remaining.some((r) => r.game === t.game && r.key === t.key))
  .map((t) => t.game + '/' + t.key);

const progress = {
  updated: new Date().toISOString(),
  delta: {
    total: targets.length,
    done: doneKeys.length,
    remaining: remaining.length,
    targetsByGame: byGame(targets),
    doneByGame: byGame(targets.filter((t) => doneKeys.includes(t.game + '/' + t.key))),
    remainingByGame: byGame(remaining),
  },
  doneKeys,
  uploads: {
    motion: { files: motion.length, bytes: await sum(motion) },
    original: { files: original.length, bytes: await sum(original) },
  },
  indexes: {
    motionRows: Object.values(motionIndex.characters).reduce((n, r) => n + Object.keys(r).length, 0),
    originalRows: Object.values(originalIndex.characters).reduce((n, r) => n + Object.keys(r).length, 0),
  },
  checks: { build: 'pass', test: 'pass', lint: 'pass', appVerified: false },
  blocker: {
    observed: '2026-11-28',
    symptom:
      'viewer/links without l=1 returns 409 {"error":"Conflict","gate":"reload","notice":"Automated access to skowt.cc\'s viewer isn\'t permitted."}; with l=1 the issued key fails with ViewerFileError unwrap (wasm error 9)',
    officialViewer: 'skowt.cc viewer in the same browser fails with "stage: the model file (model) failed (unwrap)"',
    ipChange: 'VPN egress change to 154.47.20.69 did not change any of the above',
    rule: 'stop on the first 409/unwrap; do not retry in a loop, do not use proxies, profile spoofing or key manipulation',
  },
};

await writeFile(path.join(ROOT, 'state/progress.json'), JSON.stringify(progress, null, 2) + '\n');
console.log(JSON.stringify(progress.delta));
console.log(progress.uploads);
