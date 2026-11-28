import { writeFile } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);

const games = {
  gimi: '0198390a-9b36-7687-83c4-e133684e4d39',
  srmi: '0198390d-e63b-77e0-959c-d5449cb290db',
  zzmi: '01983920-ee89-756a-a41a-0aa350739a0c',
};
const CATEGORY = '01a11f8c-66e5-7710-aeb2-164174049b24';
const BASE = 'https://den.skowt.cc/trpc/';

async function query(game, sortBy, cursor) {
  const input = { games: [game], categories: [CATEGORY], limit: 100, sortBy };
  if (cursor) input.cursor = cursor;
  const r = await fetch(`${BASE}asset.query?input=${encodeURIComponent(JSON.stringify(input))}`, {
    headers: { 'user-agent': 'Mozilla/5.0' },
  });
  if (!r.ok) throw new Error(`${sortBy}: ${r.status}`);
  return (await r.json()).result.data;
}

const all = {};
for (const [key, id] of Object.entries(games)) {
  const byPublic = new Map();
  for (const sortBy of ['name', 'date', 'views']) {
    let cursor;
    for (let page = 0; page < 20; page++) {
      const data = await query(id, sortBy, cursor);
      for (const item of data.items) byPublic.set(item.publicId, item);
      cursor = data.nextCursor;
      if (!cursor) break;
    }
  }
  all[key] = [...byPublic.values()].map((item) => ({
    id: item.id,
    publicId: item.publicId,
    name: item.name,
    rev: item.metadata?.model?.rev ?? null,
    character: item.metadata?.model?.character ?? null,
    createdAt: item.createdAt,
  }));
  console.log(key, all[key].length);
}

await writeFile(path.join(ROOT, 'state', 'live.json'), JSON.stringify(all, null, 1));

