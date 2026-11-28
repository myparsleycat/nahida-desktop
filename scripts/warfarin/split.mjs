// Splits combined per-character capture streams into the importer batch format.
//
//   bun scripts/warfarin/split.mjs
//
// Reads  work/incoming/efmi-<slug>.bin.gz        (records prefixed motion/ or original/)
// Writes work/batches/motion/efmi-<slug>.bin.gz   efmi/<slug>/{model.glb,clips/*.glb,meta.json}
//        work/batches/original/efmi-<slug>.bin.gz efmi/<slug>/{model.glb,textures/*,meta.json}
//        work/raw-original/efmi/<slug>/...        extracted original files

import { readFileSync, readdirSync, mkdirSync, writeFileSync } from 'node:fs';
import { gunzipSync, gzipSync } from 'node:zlib';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const IN = path.join(ROOT, 'work', 'incoming');
const OUT = path.join(ROOT, 'work');

function readRecords(file) {
  const buf = gunzipSync(readFileSync(file));
  const records = [];
  let offset = 0;
  while (offset < buf.length) {
    const nameLen = buf.readUInt32LE(offset);
    const dataLen = buf.readUInt32LE(offset + 4);
    offset += 8;
    const name = buf.toString('utf8', offset, offset + nameLen);
    offset += nameLen;
    records.push({ name, data: Buffer.from(buf.subarray(offset, offset + dataLen)) });
    offset += dataLen;
  }
  return records;
}

function writeRecords(records) {
  const parts = [];
  for (const { name, data } of records) {
    const nameBytes = Buffer.from(name, 'utf8');
    const header = Buffer.alloc(8);
    header.writeUInt32LE(nameBytes.length, 0);
    header.writeUInt32LE(data.length, 4);
    parts.push(header, nameBytes, data);
  }
  return gzipSync(Buffer.concat(parts), { level: 9 });
}

for (const file of readdirSync(IN).filter((f) => f.endsWith('.bin.gz')).sort()) {
  const records = readRecords(path.join(IN, file));
  const groups = new Map();
  for (const record of records) {
    const [group, ...rest] = record.name.split('/');
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push({ name: rest.join('/'), data: record.data });
  }
  const stem = file.replace(/\.bin\.gz$/, '');
  for (const [group, groupRecords] of groups) {
    const dir = path.join(OUT, group === 'motion' ? 'batches/motion' : 'batches/original');
    mkdirSync(dir, { recursive: true });
    writeFileSync(path.join(dir, `${stem}.bin.gz`), writeRecords(groupRecords));
    if (group === 'original') {
      const rawDir = path.join(OUT, 'raw-original');
      for (const record of groupRecords) {
        const target = path.join(rawDir, record.name);
        mkdirSync(path.dirname(target), { recursive: true });
        writeFileSync(target, record.data);
      }
    }
  }
  console.log(file, [...groups.keys()].join(','));
}
