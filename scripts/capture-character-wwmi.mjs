// Downloads the Wuthering Waves character data that scripts/convert-character-wwmi.mjs turns into a raw batch for the
// character motion and original model importers. The source is encore.moe, which serves the game's skeletal meshes
// and animation sequences as UEFormat files (.uemodel, .ueanim), material instances as JSON and textures as PNG.
//
// For each character of internal/mod/character/data/wwmi.json it takes the default outfit: the model and its parts,
// the clips CLIPS selects, every material of the models and every texture those materials name. Files are stored
// below <source-dir>/files by their path on the server, so textures shared between characters are fetched once, and
// a file already on disk is not fetched again. <source-dir>/characters/<id>.json lists what belongs to a character.
//
// Usage: node scripts/capture-character-wwmi.mjs <source-dir> [id ...]

import { mkdir, readFile, rename, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { setTimeout as sleep } from 'node:timers/promises';

import { uemodelMaterialPaths } from './character-ueformat.mjs';

const API = 'https://api-v2.encore.moe/api/en/character';
const DATA = 'https://s3-storage-api.encore.moe/resource/Data/';
const USER_AGENT = 'nahida-desktop-import';
const PAUSE_MS = 150;
const ROVERS = { 'rover-male': 1501, 'rover-female': 1502 };

// CLIPS lists the clips taken per character with the group and label the viewer lists them under; the first URL
// path a rule's pattern matches wins. once marks a clip that plays to its end instead of looping.
const CLIPS = [
  { pattern: /\/CommonAnim\/Stand1\.ueanim$/, group: 'idle', label: 'Idle' },
  { pattern: /\/CommonAnim\/Stand2\.ueanim$/, group: 'idle', label: 'Stand' },
  { pattern: /\/CommonAnim\/Stand1_Action01\.ueanim$/, group: 'idle', label: 'Idle action 1', once: true },
  { pattern: /\/CommonAnim\/Stand1_Action02\.ueanim$/, group: 'idle', label: 'Idle action 2', once: true },
  { pattern: /\/CommonAnim\/Stand1_Action03\.ueanim$/, group: 'idle', label: 'Idle action 3', once: true },
  { pattern: /\/CommonAnim\/StandChange\.ueanim$/, group: 'idle', label: 'Stance change', once: true },
  { pattern: /\/UiAnim\/Team_Stand1\.ueanim$/, group: 'idle', label: 'Team 1' },
  { pattern: /\/UiAnim\/Team_Stand2\.ueanim$/, group: 'idle', label: 'Team 2' },
  { pattern: /\/UiAnim\/Team_Stand3\.ueanim$/, group: 'idle', label: 'Team 3' },
  { pattern: /\/UiAnim\/Ui_Nature_Loop\.ueanim$/, group: 'idle', label: 'Overview' },
  { pattern: /\/UiAnim\/Ui_Weapon_Loop\.ueanim$/, group: 'idle', label: 'Weapon' },
  { pattern: /\/UiAnim\/Ui_Resonant_Loop\.ueanim$/, group: 'idle', label: 'Resonance' },
  { pattern: /\/UiAnim\/Ui_Chip_Loop\.ueanim$/, group: 'idle', label: 'Echo' },
  { pattern: /\/DialogAnim\/[^/]+\/[^/]+\/Body\/Walk_F\.ueanim$/, group: 'move', label: 'Walk' },
  { pattern: /\/DialogAnim\/[^/]+\/[^/]+\/Body\/Run_F\.ueanim$/, group: 'move', label: 'Run' },
  { pattern: /\/DialogAnim\/[^/]+\/[^/]+\/Body\/Sprint_F\.ueanim$/, group: 'move', label: 'Sprint' },
  { pattern: /\/CommonAnim\/Attack01\.ueanim$/, group: 'combat', label: 'Attack 1', once: true },
  { pattern: /\/CommonAnim\/Attack02\.ueanim$/, group: 'combat', label: 'Attack 2', once: true },
  { pattern: /\/CommonAnim\/Attack03\.ueanim$/, group: 'combat', label: 'Attack 3', once: true },
  { pattern: /\/CommonAnim\/Attack04\.ueanim$/, group: 'combat', label: 'Attack 4', once: true },
  { pattern: /\/CommonAnim\/Skill01\.ueanim$/, group: 'combat', label: 'Skill', once: true },
  { pattern: /\/CommonAnim\/Skill02\.ueanim$/, group: 'combat', label: 'Skill 2', once: true },
  { pattern: /\/CommonAnim\/SkillQte\.ueanim$/, group: 'combat', label: 'Intro', once: true },
  { pattern: /\/CommonAnim\/Burst01\.ueanim$/, group: 'combat', label: 'Ultimate', once: true },
  { pattern: /\/UiAnim\/Cos_Pose\.ueanim$/, group: 'extras', label: 'Pose' },
];

// dataPath returns the path below DATA of an encore URL, or of an Unreal object path such as
// Client/Content/Aki/X/T_Y.1 with the given extension.
function dataPath(value, extension) {
  if (value.startsWith(DATA)) {
    return value.slice(DATA.length).split('/').map(decodeURIComponent).join('/');
  }
  const match = /^(?:Client\/Content|Game)\/(.+?)(?:\.[^/.]+)?$/.exec(value);
  if (!match) {
    throw new Error(`unrecognised object path ${value}`);
  }
  return `Game/${match[1]}${extension}`;
}

async function request(url, attempt = 0) {
  await sleep(PAUSE_MS);
  let response;
  try {
    response = await fetch(url, { headers: { 'User-Agent': USER_AGENT } });
  } catch (error) {
    if (attempt < 4) {
      await sleep(2000 * 2 ** attempt);
      return request(url, attempt + 1);
    }
    throw error;
  }
  if ((response.status === 429 || response.status >= 500) && attempt < 4) {
    await sleep(Number(response.headers.get('retry-after')) * 1000 || 2000 * 2 ** attempt);
    return request(url, attempt + 1);
  }
  return response;
}

class Store {
  constructor(root) {
    this.root = root;
    this.fetched = 0;
    this.bytes = 0;
  }

  file(relative) {
    const resolved = path.resolve(this.root, 'files', relative);
    if (!resolved.startsWith(path.resolve(this.root, 'files') + path.sep)) {
      throw new Error(`path escapes the store: ${relative}`);
    }
    return resolved;
  }

  // get returns the bytes of relative, fetching them when they are not on disk; null when the server has no file.
  async get(relative) {
    const file = this.file(relative);
    try {
      return await readFile(file);
    } catch (error) {
      if (error.code !== 'ENOENT') {
        throw error;
      }
    }
    const missing = `${file}.missing`;
    if (await stat(missing).catch(() => null)) {
      return null;
    }
    const response = await request(DATA + relative.split('/').map(encodeURIComponent).join('/'));
    await mkdir(path.dirname(file), { recursive: true });
    if (response.status === 404) {
      await writeFile(missing, '');
      return null;
    }
    if (!response.ok) {
      throw new Error(`${relative}: HTTP ${response.status}`);
    }
    const bytes = Buffer.from(await response.arrayBuffer());
    await writeFile(`${file}.part`, bytes);
    await rename(`${file}.part`, file);
    this.fetched++;
    this.bytes += bytes.length;
    return bytes;
  }
}

function selectClips(urls) {
  const used = new Set();
  const clips = [];
  for (const rule of CLIPS) {
    const url = urls.find((candidate) => rule.pattern.test(candidate) && !used.has(candidate));
    if (url) {
      used.add(url);
      clips.push({ label: rule.label, group: rule.group, loop: !rule.once, file: dataPath(url) });
    }
  }
  return clips;
}

// textureObjects maps a material's texture slots to the object paths of the game's own textures; engine textures
// are not served.
function textureObjects(material) {
  const out = new Map();
  for (const [slot, value] of Object.entries(material?.Textures ?? {})) {
    if (/^(?:Client\/Content|Game)\//.test(value?.ObjectPath ?? '')) {
      out.set(slot, value.ObjectPath);
    }
  }
  return out;
}

async function captureCharacter(store, entry) {
  const roleId = ROVERS[entry.key] ?? Number(entry.key);
  if (!Number.isInteger(roleId)) {
    throw new Error(`${entry.id}: no encore role id for key ${entry.key}`);
  }
  const response = await request(`${API}/${roleId}`);
  if (!response.ok) {
    throw new Error(`${entry.id}: character ${roleId}: HTTP ${response.status}`);
  }
  const role = await response.json();
  const model = role.Model3D?.find((m) => m.Id === role.SkinId) ?? role.Model3D?.find((m) => m.Kind === 'skin');
  if (!model) {
    throw new Error(`${entry.id}: character ${roleId} has no model`);
  }

  const models = [model.Model, ...(model.Parts ?? [])].map((url) => dataPath(url));
  const materials = {};
  const textures = new Set();
  for (const file of models) {
    const bytes = await store.get(file);
    if (!bytes) {
      throw new Error(`${entry.id}: missing model ${file}`);
    }
    for (const materialPath of uemodelMaterialPaths(bytes)) {
      const json = dataPath(materialPath, '.json');
      if (materials[materialPath] !== undefined) {
        continue;
      }
      const data = await store.get(json);
      materials[materialPath] = data ? json : null;
      if (data) {
        for (const objectPath of textureObjects(JSON.parse(data.toString('utf8'))).values()) {
          textures.add(dataPath(objectPath, '.png'));
        }
      }
    }
  }
  const missingTextures = [];
  for (const texture of textures) {
    if (!(await store.get(texture))) {
      missingTextures.push(texture);
    }
  }
  const clips = selectClips(model.Animations ?? []);
  for (const clip of clips) {
    if (!(await store.get(clip.file))) {
      clip.missing = true;
    }
  }

  const manifest = {
    id: entry.id,
    roleId,
    name: role.Name?.Content ?? role.Name,
    skin: model.Name,
    models,
    materials,
    clips: clips.filter((clip) => !clip.missing),
    missingTextures,
  };
  await mkdir(path.join(store.root, 'characters'), { recursive: true });
  await writeFile(path.join(store.root, 'characters', `${entry.id}.json`), `${JSON.stringify(manifest, null, 2)}\n`);
  return manifest;
}

async function main() {
  const [root, ...ids] = process.argv.slice(2);
  if (!root) {
    console.error('Usage: node scripts/capture-character-wwmi.mjs <source-dir> [id ...]');
    process.exit(2);
  }
  const catalog = JSON.parse(await readFile('internal/mod/character/data/wwmi.json', 'utf8')).characters;
  const entries = ids.length ? catalog.filter((entry) => ids.includes(entry.id)) : catalog;
  const store = new Store(root);
  const failed = [];
  for (const entry of entries) {
    try {
      const manifest = await captureCharacter(store, entry);
      console.log(
        `${entry.id}: ${manifest.models.length} models, ${Object.keys(manifest.materials).length} materials, ` +
          `${manifest.clips.length} clips, ${manifest.missingTextures.length} textures missing ` +
          `(fetched ${store.fetched} files, ${(store.bytes / 1e6).toFixed(0)} MB so far)`,
      );
    } catch (error) {
      failed.push(entry.id);
      console.error(`${entry.id}: ${error.message}`);
    }
  }
  if (failed.length) {
    console.error(`failed: ${failed.join(' ')}`);
    process.exit(1);
  }
}

await main();
