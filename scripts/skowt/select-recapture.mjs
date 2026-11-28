// Rebuilds state/remaining.json for a recapture: the outstanding targets plus the characters whose shape keys
// the previous capture dropped because mesh and node names differ.
//
//   bun scripts/skowt/select-recapture.mjs

import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const STATE = path.join(ROOT, 'state');

const LOST_MORPHS = [
  'srmi/aeon-aha',
  'zzmi/anby',
  'zzmi/anton',
  'zzmi/ben',
  'zzmi/billy',
  'zzmi/burnice',
  'zzmi/caesar',
  'zzmi/cissia',
  'zzmi/claret',
  'zzmi/corin',
  'zzmi/dialyn',
  'zzmi/ellen',
  'zzmi/evelyn',
  'zzmi/grace',
  'zzmi/harumasa',
  'zzmi/hugo',
];

const { targets } = JSON.parse(await readFile(path.join(STATE, 'targets.json'), 'utf8'));
const { remaining } = JSON.parse(await readFile(path.join(STATE, 'remaining.json'), 'utf8'));

const wanted = new Set([...remaining.map((t) => `${t.game}/${t.key}`), ...LOST_MORPHS]);
const selected = targets.filter((t) => wanted.has(`${t.game}/${t.key}`));
const missing = [...wanted].filter((k) => !selected.some((t) => `${t.game}/${t.key}` === k));

await writeFile(path.join(STATE, 'remaining.json'), JSON.stringify({ remaining: selected }, null, 1) + '\n');
console.log(`selected ${selected.length} of ${wanted.size}`);
if (missing.length) console.log(`missing from targets.json: ${missing.join(', ')}`);
