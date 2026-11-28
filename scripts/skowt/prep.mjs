import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const REPO = path.resolve(ROOT, '..', '..');
const STATE = path.join(ROOT, 'state');

const BASE = 'https://den.skowt.cc/trpc/';
const HEAD = { 'user-agent': 'Mozilla/5.0' };

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function trpc(proc, input) {
  for (let attempt = 0; attempt < 8; attempt++) {
    const r = await fetch(`${BASE}${proc}?input=${encodeURIComponent(JSON.stringify(input))}`, { headers: HEAD });
    if (r.status === 429) {
      await sleep(6000 * (attempt + 1));
      continue;
    }
    if (!r.ok) throw new Error(`${proc}: ${r.status}`);
    return (await r.json()).result.data;
  }
  throw new Error(`${proc}: rate limited`);
}

const live = JSON.parse(await readFile(path.join(STATE, 'live.json'), 'utf8'));
const captured = JSON.parse(await readFile(path.join(STATE, 'captured.json'), 'utf8'));
const diff = JSON.parse(await readFile(path.join(STATE, 'diff.json'), 'utf8'));

const catalog = {};
for (const game of ['gimi', 'srmi', 'zzmi']) {
  catalog[game] = JSON.parse(await readFile(path.join(REPO, `internal/mod/character/data/${game}.json`), 'utf8')).characters;
}

const targets = [];

for (const change of diff.changed) {
  const [game, key] = change.key.split('/');
  const item = live[game].find((x) => x.publicId === change.publicId);
  if (!item) throw new Error(`${change.key}: no live item`);
  targets.push({ game, key, id: item.id, publicId: item.publicId, name: item.name, oldRev: change.oldRev, newRev: item.rev });
}

for (const added of diff.newData) {
  const item = live[added.g].find((x) => x.publicId === added.publicId);
  if (!item) throw new Error(`${added.g}/${added.key}: no live item`);
  targets.push({
    game: added.g,
    key: added.key,
    id: item.id,
    publicId: item.publicId,
    name: item.name,
    oldRev: null,
    newRev: item.rev,
  });
}

for (const target of targets) {
  const data = await trpc('asset.getById', { id: target.id });
  const model = data.metadata?.model ?? {};
  target.meta = {
    source: 'skowt.cc',
    name: target.name,
    publicId: target.publicId,
    rev: model.rev ?? target.newRev,
    heightM: model.heightM ?? null,
    bones: model.bones ?? null,
    animations: (model.animations ?? []).map((animation) => {
      const copy = { ...animation };
      delete copy.hold;
      return copy;
    }),
    materials: (model.materials ?? []).map((m) => (typeof m === 'string' ? m : m.name)),
    roles: model.viewer?.roles ?? null,
  };
  if (!target.meta.materials.length || !target.meta.roles) {
    // metadata.model strips materials; take them from the decoded model during capture instead.
    delete target.meta.materials;
  }
}

targets.sort((a, b) => a.game.localeCompare(b.game) || a.key.localeCompare(b.key));
const body = JSON.stringify({ targets }, null, 1);
await writeFile(path.join(STATE, 'targets.json'), body);
// The receiver serves remaining.json, so the capture helper can start right after this step.
await writeFile(path.join(STATE, 'remaining.json'), body);
console.log(targets.length, Object.fromEntries(Object.entries(targets.reduce((m, t) => ((m[t.game] = (m[t.game] ?? 0) + 1), m), {}))));
