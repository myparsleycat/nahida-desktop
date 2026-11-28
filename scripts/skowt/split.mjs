// Splits combined per-character capture streams into the importer batch format.
//
//   bun scripts/skowt/split.mjs
//
// Reads  work/incoming/<game>-<key>.bin.gz   (records prefixed motion/ or original/)
// Writes work/batches/motion/<game>-<key>.bin.gz    <game>/<key>/{model.glb,anim.glb,meta.json}
//        work/batches/original/<game>-<key>.bin.gz  <game>/<key>/{model.glb,toon.glb,meta.json}
//        work/raw-original/<game>/<key>/...         extracted original files (for merge-toon.mjs)

import { createReadStream, readdirSync, mkdirSync, writeFileSync } from 'node:fs';
import { createGunzip, gzipSync } from 'node:zlib';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const IN = path.join(ROOT, 'work', 'incoming');
const OUT = path.join(ROOT, 'work');

function readRecords(file) {
  return new Promise((resolve, reject) => {
    let buf = Buffer.alloc(0);
    const records = [];
    const rs = createReadStream(file).pipe(createGunzip());
    rs.on('data', (chunk) => {
      buf = Buffer.concat([buf, chunk]);
      for (;;) {
        if (buf.length < 8) break;
        const nameLen = buf.readUInt32LE(0);
        const dataLen = buf.readUInt32LE(4);
        if (buf.length < 8 + nameLen + dataLen) break;
        const name = buf.subarray(8, 8 + nameLen).toString('utf8');
        const data = Buffer.from(buf.subarray(8 + nameLen, 8 + nameLen + dataLen));
        records.push({ name, data });
        buf = buf.subarray(8 + nameLen + dataLen);
      }
    });
    rs.on('end', () => resolve(records));
    rs.on('error', reject);
  });
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
  const records = await readRecords(path.join(IN, file));
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
