// Applies a delta of skowt characters to the bundled toon data. Reads the raw original models
// (model.glb + toon.glb) captured under work/raw-original/<game>/<key>, takes the same material values
// and ramp textures scripts/import-character-toon.mjs keeps, and replaces only those characters in
// internal/ui/assets/character_toon.
//
//   bun scripts/skowt/merge-toon.mjs

import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';

const ROOT = path.resolve(import.meta.dir);
const REPO = path.resolve(ROOT, '..', '..');
const RAW = process.argv[2] ?? path.join(ROOT, 'work', 'raw-original');
const OUTPUT = path.join(REPO, 'internal/ui/assets/character_toon');

try {
  await readdir(RAW);
} catch {
  console.error(`no captured originals at ${RAW}; run capture + split first`);
  process.exit(1);
}

const GAMES = {
  gimi: {
    role: /_Mat_([A-Za-z]+\d*)$/,
    floats:
      /^_(LightArea|UseMaterial[2-5]|Shininess[2-5]?|SpecMulti[2-5]?|OutLineIntensity[2-5]?|MetalMaterial|MTMapBrightness|MTShininess|MTSpecularScale|MTSpecularAttenInShadow)$/,
    colors:
      /^_(OutlineColor[2-5]?|SpecularColor[2-5]?|MTMapDarkColor|MTMapLightColor|MTShadowMultiColor|MTSpecularColor)$/,
    textures: /^_PackedShadowRampTex$/,
  },
  srmi: {
    role: /_Mat_([A-Za-z0-9_]+)$|_(Body|Face|Hair)_Color/,
    floats: /^_(UseMaterialValuesLUT|SpecularIntensity\d|SpecularRoughness\d|SpecularShininess\d)$/,
    colors: /^_(OutlineColor\d?|RimColor\d|SpecularColor\d?)$/,
    textures: /^_(DiffuseRampMultiTex|MaterialValuesPackLUT)$/,
  },
  zzmi: {
    role: /^MAT_[A-Za-z0-9]+_([A-Za-z0-9_]+)$/,
    floats: /^_(SpecIntensity|ToonSpecular\d?|Metallic|Glossiness)$/,
    colors: /^_(ShadowColor\d?|ShallowColor\d?|SpecularColor\d?|OutlineColor\d?)$/,
    textures: /^$/,
  },
};

function parseGLB(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const jsonLength = view.getUint32(12, true);
  const json = JSON.parse(new TextDecoder().decode(bytes.subarray(20, 20 + jsonLength)));
  const binStart = 20 + jsonLength;
  const binLength = view.getUint32(binStart, true);
  return { json, bin: bytes.subarray(binStart + 8, binStart + 8 + binLength) };
}

function pick(record, pattern) {
  const kept = {};
  for (const [key, value] of Object.entries(record ?? {})) {
    if (pattern.test(key)) kept[key] = value;
  }
  return kept;
}

function asWebp(glb, name) {
  const image = glb.json.images.find((x) => x.name === name);
  if (!image) return null;
  if (image.mimeType !== 'image/webp') throw new Error(`${name}: ${image.mimeType}`);
  const view = glb.json.bufferViews[image.bufferView];
  const bytes = glb.bin.subarray(view.byteOffset ?? 0, (view.byteOffset ?? 0) + view.byteLength);
  return { mime: image.mimeType, data: Buffer.from(bytes) };
}

async function existing(game) {
  const file = path.join(OUTPUT, `${game}.json`);
  const document = JSON.parse(await readFile(file, 'utf8'));
  return { file, document, images: new Set() };
}

for (const game of Object.keys(GAMES)) {
  const spec = GAMES[game];
  const dir = path.join(RAW, game);
  let ids;
  try {
    ids = await readdir(dir);
  } catch {
    continue;
  }
  const { file, document, images } = await existing(game);
  for (const id of ids.sort()) {
    const model = parseGLB(await readFile(path.join(dir, id, 'model.glb')));
    const toonPath = path.join(dir, id, 'toon.glb');
    let toon = null;
    try {
      toon = parseGLB(await readFile(toonPath));
    } catch {
      // No toon file: the model's own images still cover every referenced texture.
    }
    const materials = [];
    for (const material of model.json.materials ?? []) {
      const values = material.extras?.game;
      const match = spec.role.exec(material.name);
      const role = (match?.[1] ?? match?.[2])?.toLowerCase();
      if (!values || !role) continue;
      const textures = pick(values.textures, spec.textures);
      for (const name of Object.values(textures)) {
        const image = (toon && asWebp(toon, name)) || asWebp(model, name);
        if (!image) throw new Error(`${game}/${id}: texture ${name} has no image`);
        const target = path.join(OUTPUT, game, `${name}.webp`);
        await mkdir(path.dirname(target), { recursive: true });
        await writeFile(target, image.data);
        images.add(name);
      }
      materials.push({ role, floats: pick(values.floats, spec.floats), colors: pick(values.colors, spec.colors), textures });
    }
    if (materials.length === 0) {
      console.warn(`${game}/${id}: no game material data`);
      continue;
    }
    document.characters[id] = { materials };
    console.log(`${game}/${id}: ${materials.length} materials`);
  }
  document.characters = Object.fromEntries(Object.entries(document.characters).sort(([a], [b]) => a.localeCompare(b)));
  await writeFile(file, `${JSON.stringify(document)}\n`);
  // Drop images the merged document no longer references, keeping the other games' files intact.
  const referenced = new Set();
  for (const entry of Object.values(document.characters)) {
    for (const material of entry.materials) {
      for (const name of Object.values(material.textures ?? {})) referenced.add(name);
    }
  }
  let removed = 0;
  let files = [];
  try {
    files = await readdir(path.join(OUTPUT, game));
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
  for (const name of files) {
    if (!name.endsWith('.webp')) continue;
    if (!referenced.has(name.slice(0, -'.webp'.length))) {
      await rm(path.join(OUTPUT, game, name));
      removed++;
    }
  }
  console.log(`${game}: ${Object.keys(document.characters).length} characters, ${images.size} updated images, ${removed} removed`);
}
