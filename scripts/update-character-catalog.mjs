// Regenerates internal/mod/character/data/*.json, the character catalog the mod manager offers for folders,
// from public game data:
//   Genshin Impact: theBowja/genshin-db (MIT) for English names and body types, joined by avatar ID with
//                   EnkaNetwork/API-docs for localized names and the internal avatar name of the ramp files.
//   Honkai: Star Rail, Zenless Zone Zero, Arknights: Endfield: EnkaNetwork/API-docs.
//   Wuthering Waves: Arikatsu/WutheringWaves_Data (role info and the multi-text maps, about 230 MB).
// Only names, body types and internal names are taken. An entry keeps the id it already has in the catalog,
// because folders and settings store ids.
//
// Usage: node scripts/update-character-catalog.mjs [game ...]   (gimi, srmi, zzmi, wwmi, efmi)

import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';

const OUTPUT = path.join(process.cwd(), 'internal/mod/character/data');
const ENKA = 'https://raw.githubusercontent.com/EnkaNetwork/API-docs/master/store';
const GENSHIN_DB = 'https://raw.githubusercontent.com/theBowja/genshin-db/main/src/data/English/characters';
const GENSHIN_DB_TREE = 'https://api.github.com/repos/theBowja/genshin-db/git/trees/main?recursive=1';
const WUWA = 'https://raw.githubusercontent.com/Arikatsu/WutheringWaves_Data/HEAD';
const LOCALES = { en: 'en', ko: 'ko', ja: 'ja', zh: 'zh-cn' };
const GENDER = {
  male: { en: ' (Male)', ko: ' (남)', ja: '（男性）', zh: '（男）' },
  female: { en: ' (Female)', ko: ' (여)', ja: '（女性）', zh: '（女）' },
};

async function fetchJSON(url) {
  const response = await fetch(url, { headers: { 'User-Agent': 'nahida-desktop-catalog' } });
  if (!response.ok) {
    throw new Error(`${url}: ${response.status}`);
  }
  return response.json();
}

function slug(name) {
  return name
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

function localized(lookup) {
  const names = {};
  for (const [locale, source] of Object.entries(LOCALES)) {
    const name = lookup(source)?.trim();
    if (name) {
      names[locale] = name;
    }
  }
  return names;
}

function withSuffix(names, suffixes) {
  return Object.fromEntries(Object.entries(names).map(([locale, name]) => [locale, name + suffixes[locale]]));
}

async function genshin() {
  const [avatars, locs, tree] = await Promise.all([
    fetchJSON(`${ENKA}/gi/avatars.json`),
    fetchJSON(`${ENKA}/gi/locs.json`),
    fetchJSON(GENSHIN_DB_TREE),
  ]);
  const files = tree.tree
    .map((entry) => entry.path)
    .filter((file) => file.startsWith('src/data/English/characters/') && file.endsWith('.json'));
  const characters = await Promise.all(
    files.map((file) => fetchJSON(`${GENSHIN_DB}/${path.posix.basename(file)}`)),
  );
  const entries = [];
  for (const character of characters) {
    const avatar = avatars[String(character.id)];
    const body = /^BODY_([A-Z]+)$/.exec(character.bodyType ?? '')?.[1]?.toLowerCase();
    const internal = /UI_AvatarIcon_Side_(\w+)\.png$/.exec(avatar?.SideIconName ?? '')?.[1];
    let names = avatar ? localized((locale) => locs[locale]?.[avatar.NameTextMapHash]) : {};
    names.en ??= character.name;
    let id = slug(character.name);
    const aliases = [];
    if (internal === 'PlayerBoy' || internal === 'PlayerGirl') {
      names = withSuffix(names, GENDER[internal === 'PlayerBoy' ? 'male' : 'female']);
      aliases.push(character.name);
    }
    const entry = { key: String(character.id), id, names };
    if (aliases.length > 0) {
      entry.aliases = aliases;
    }
    if (body) {
      entry.genshin = { body, ...(internal ? { avatar: internal } : {}) };
    }
    entries.push(entry);
  }
  return entries;
}

const MARCH_HUNT = { en: ' (The Hunt)', ko: ' (수렵)', ja: '（巡狩）', zh: '（巡猎）' };

async function starRail() {
  const [avatars, text] = await Promise.all([fetchJSON(`${ENKA}/hsr/avatars.json`), fetchJSON(`${ENKA}/hsr/hsr.json`)]);
  const entries = [];
  const trailblazers = new Set();
  for (const [key, avatar] of Object.entries(avatars)) {
    let names = localized((locale) => text[locale]?.[avatar.AvatarName.Hash]);
    if (!names.en) {
      continue;
    }
    if (key.startsWith('80')) {
      // Trailblazer IDs alternate between Caelus and Stelle, one per path; each shares the model of its gender.
      const male = Number(key) % 2 === 1;
      const id = male ? 'caelus' : 'stelle';
      if (!trailblazers.has(id)) {
        trailblazers.add(id);
        names = withSuffix(names, GENDER[male ? 'male' : 'female']);
        entries.push({ key: id, id, names, aliases: [male ? 'Caelus' : 'Stelle'] });
      }
      continue;
    }
    let id = slug(names.en);
    if (key === '1224') {
      names = withSuffix(names, MARCH_HUNT);
      id = 'march-7th-the-hunt';
    }
    entries.push({ key, id, names });
  }
  return entries;
}

const ZZZ_PROXIES = [
  { key: 'belle', id: 'belle', names: { en: 'Belle', ko: '벨', ja: 'リン', zh: '铃' } },
  { key: 'wise', id: 'wise', names: { en: 'Wise', ko: '와이즈', ja: 'アキラ', zh: '哲' } },
];

async function zenless() {
  const [avatars, locs] = await Promise.all([fetchJSON(`${ENKA}/zzz/avatars.json`), fetchJSON(`${ENKA}/zzz/locs.json`)]);
  const entries = [...ZZZ_PROXIES];
  for (const [key, avatar] of Object.entries(avatars)) {
    const names = localized((locale) => locs[locale]?.[avatar.Name]);
    if (names.en) {
      entries.push({ key, id: slug(names.en), names });
    }
  }
  return entries;
}

const ENDMINISTRATOR = { chr_0002_endminm: 'male', chr_0003_endminf: 'female' };

async function endfield() {
  const [avatars, locs] = await Promise.all([fetchJSON(`${ENKA}/ef/avatars.json`), fetchJSON(`${ENKA}/ef/locs.json`)]);
  const entries = [];
  for (const avatar of Object.values(avatars)) {
    let names = localized((locale) => locs[locale]?.[avatar.NameHash]);
    if (!names.en || avatar.StrId.startsWith('chr_9')) {
      continue;
    }
    let id = slug(names.en);
    const gender = ENDMINISTRATOR[avatar.StrId];
    if (gender) {
      names = withSuffix(names, GENDER[gender]);
      id = `${id}-${gender}`;
    }
    entries.push({ key: avatar.StrId, id, names });
  }
  return entries;
}

const WUWA_LOCALES = { en: 'en', ko: 'ko', ja: 'ja', zh: 'zh-Hans' };
const ROVER = { en: 'Rover', ko: '방랑자', ja: '漂泊者', zh: '漂泊者' };

async function wutheringWaves() {
  const roles = await fetchJSON(`${WUWA}/BinData/role/roleinfo.json`);
  const text = {};
  for (const [locale, source] of Object.entries(WUWA_LOCALES)) {
    const entries = await fetchJSON(`${WUWA}/Textmaps/${source}/multi_text/MultiText.json`);
    text[locale] = new Map(entries.map((entry) => [entry.Id, entry.Content]));
  }
  const entries = [];
  const rovers = new Set();
  for (const role of roles) {
    if (role.RoleType !== 1 || role.IsTrial) {
      continue;
    }
    if (/^Rover\b/.test(text.en.get(role.Name) ?? '')) {
      // Every Rover element shares the model of its gender.
      const gender = role.RoleBody?.startsWith('Female') ? 'female' : 'male';
      if (!rovers.has(gender)) {
        rovers.add(gender);
        entries.push({ key: `rover-${gender}`, id: `rover-${gender}`, names: withSuffix(ROVER, GENDER[gender]) });
      }
      continue;
    }
    const names = {};
    for (const locale of Object.keys(WUWA_LOCALES)) {
      const name = text[locale].get(role.Name)?.trim();
      if (name) {
        names[locale] = name;
      }
    }
    if (names.en) {
      entries.push({ key: String(role.Id), id: slug(names.en), names });
    }
  }
  return entries;
}

const GAMES = {
  gimi: { game: 'GIMI', build: genshin },
  srmi: { game: 'SRMI', build: starRail },
  zzmi: { game: 'ZZMI', build: zenless },
  wwmi: { game: 'WWMI', build: wutheringWaves },
  efmi: { game: 'EFMI', build: endfield },
};

async function readCatalog(file) {
  try {
    return JSON.parse(await readFile(file, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT') {
      return { characters: [] };
    }
    throw error;
  }
}

function format(catalog) {
  const lines = catalog.characters.map((entry) => `    ${JSON.stringify(entry)}`);
  return `{\n  "game": ${JSON.stringify(catalog.game)},\n  "characters": [\n${lines.join(',\n')}\n  ]\n}\n`;
}

const selected = process.argv.slice(2);
for (const [file, { game, build }] of Object.entries(GAMES)) {
  if (selected.length > 0 && !selected.includes(file)) {
    continue;
  }
  const target = path.join(OUTPUT, `${file}.json`);
  const existing = await readCatalog(target);
  const kept = new Map(existing.characters.map((entry) => [entry.key, entry.id]));
  const entries = await build();
  const used = new Set();
  for (const entry of entries) {
    entry.id = kept.get(entry.key) ?? entry.id;
    if (used.has(entry.id)) {
      throw new Error(`${file}: duplicate id ${entry.id} (key ${entry.key})`);
    }
    used.add(entry.id);
  }
  entries.sort((a, b) => a.id.localeCompare(b.id));
  await writeFile(target, format({ game, characters: entries }));
  console.log(`${file}: ${entries.length} characters`);
}
