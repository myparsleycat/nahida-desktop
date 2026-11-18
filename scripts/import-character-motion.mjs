// Converts extracted character motions into the files the model viewer downloads, keyed by the character catalog's
// ids, and writes an index with each file's size and SHA-256.
//
// The input batches were extracted with the operators' permission: Genshin Impact, Honkai: Star Rail and Zenless Zone
// Zero from skowt.cc's 3D viewer, Arknights: Endfield from studio.warfarin.wiki. Each batch is a gzip stream of
// records (u32 name length, u32 data length, name, data) holding, per character:
//   <game>/<key>/model.glb  skeleton nodes, skins and the skinned meshes' POSITION, JOINTS_0 and WEIGHTS_0
//   <game>/<key>/anim.glb   (skowt) every clip as one animation
//   <game>/<key>/clips/<name>.glb  (Warfarin) one animation per clip
//   <game>/<key>/meta.json  source names and clip metadata
//
// Each output file is a gzip-compressed GLB with the skeleton, the skins, the reference meshes (POSITION float32,
// JOINTS_0 u8/u16, WEIGHTS_0 normalized u8) and the clips resampled to 60 fps, the sources' key rate (rotations as
// normalized int16, translations and scales as float32). A channel that never leaves the joint's rest value is
// dropped. The sample rate and the clip metadata are in the root extras.
//
// Usage: node scripts/import-character-motion.mjs <raw-dir> <out-dir> [index.json]

import { createHash } from 'node:crypto';
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { gunzipSync, gzipSync } from 'node:zlib';

const FPS = 60;
const ROTATION_EPSILON = 2 / 32767;
const VECTOR_EPSILON = 1e-5;

// skowt names both Trailblazers and both March 7th forms alike; their material names tell them apart.
const ALIASES = [
  { key: /^trailblazer#\d$/, material: /PlayerGirl/, id: 'stelle' },
  { key: /^trailblazer#\d$/, material: /PlayerBoy/, id: 'caelus' },
  { key: /^march-7th#\d$/, material: /^Mar_7th_00_/, id: 'march-7th' },
  { key: /^march-7th#\d$/, material: /^Mar_7th_10_/, id: 'march-7th-the-hunt' },
];

// Warfarin clip names, by the part after A_actor_<character>_, with the group and label the viewer lists them under.
const EFMI_CLIPS = {
  idle_loop: ['idle', 'Idle'],
  relax_loop: ['idle', 'Relax'],
  relax_sp_01: ['idle', 'Relax 1'],
  relax_sp_02: ['idle', 'Relax 2'],
  ui_overview_loop: ['idle', 'Overview'],
  ui_relax_loop: ['idle', 'Overview relax'],
  uiteam_idle_01: ['idle', 'Team'],
  walk_loop: ['move', 'Walk'],
  run_loop: ['move', 'Run'],
  sprint_loop: ['move', 'Sprint'],
  battle_loop: ['combat', 'Battle stance'],
  battle_normal_skill: ['combat', 'Skill'],
  battle_skill_ult: ['combat', 'Ultimate'],
  gacha_ani: ['extras', 'Showcase'],
  gacha_ani_loop: ['extras', 'Showcase loop'],
  interact_sit_loop: ['extras', 'Sit'],
};

const COMPONENTS = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 };
const BYTES = { 5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4 };
const NORMALIZE = { 5120: 127, 5121: 255, 5122: 32767, 5123: 65535 };

function parseGLB(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (view.getUint32(0, true) !== 0x46546c67) {
    throw new Error('not a GLB');
  }
  const jsonLength = view.getUint32(12, true);
  const json = JSON.parse(new TextDecoder().decode(bytes.subarray(20, 20 + jsonLength)));
  const binStart = 20 + jsonLength;
  const bin = binStart + 8 <= bytes.length ? bytes.subarray(binStart + 8, binStart + 8 + view.getUint32(binStart, true)) : null;
  return { json, bin };
}

function readAccessor({ json, bin }, index) {
  const accessor = json.accessors[index];
  const components = COMPONENTS[accessor.type];
  const size = BYTES[accessor.componentType];
  const out = new Float64Array(accessor.count * components);
  if (accessor.bufferView == null) {
    return out;
  }
  const bufferView = json.bufferViews[accessor.bufferView];
  const stride = bufferView.byteStride || components * size;
  const view = new DataView(bin.buffer, bin.byteOffset + (bufferView.byteOffset || 0) + (accessor.byteOffset || 0));
  const scale = accessor.normalized ? NORMALIZE[accessor.componentType] : 1;
  for (let i = 0; i < accessor.count; i++) {
    for (let c = 0; c < components; c++) {
      const offset = i * stride + c * size;
      let value;
      switch (accessor.componentType) {
        case 5126:
          value = view.getFloat32(offset, true);
          break;
        case 5125:
          value = view.getUint32(offset, true);
          break;
        case 5123:
          value = view.getUint16(offset, true);
          break;
        case 5122:
          value = view.getInt16(offset, true);
          break;
        case 5121:
          value = view.getUint8(offset);
          break;
        default:
          value = view.getInt8(offset);
      }
      out[i * components + c] = accessor.normalized ? Math.max(value / scale, -1) : value;
    }
  }
  return out;
}

class Writer {
  constructor() {
    this.accessors = [];
    this.bufferViews = [];
    this.chunks = [];
    this.length = 0;
  }

  add(typed, accessor) {
    const pad = (4 - (this.length % 4)) % 4;
    if (pad) {
      this.chunks.push(new Uint8Array(pad));
      this.length += pad;
    }
    const bytes = new Uint8Array(typed.buffer, typed.byteOffset, typed.byteLength);
    this.bufferViews.push({ buffer: 0, byteOffset: this.length, byteLength: bytes.length });
    this.chunks.push(bytes);
    this.length += bytes.length;
    this.accessors.push({ bufferView: this.bufferViews.length - 1, ...accessor });
    return this.accessors.length - 1;
  }

  floats(values, type, withBounds) {
    const data = Float32Array.from(values);
    const accessor = { componentType: 5126, count: data.length / COMPONENTS[type], type };
    if (withBounds) {
      const components = COMPONENTS[type];
      accessor.min = [];
      accessor.max = [];
      for (let c = 0; c < components; c++) {
        let min = Infinity;
        let max = -Infinity;
        for (let i = c; i < data.length; i += components) {
          min = Math.min(min, data[i]);
          max = Math.max(max, data[i]);
        }
        accessor.min.push(min);
        accessor.max.push(max);
      }
    }
    return this.add(data, accessor);
  }

  glb(json) {
    const bin = new Uint8Array(this.length);
    let offset = 0;
    for (const chunk of this.chunks) {
      bin.set(chunk, offset);
      offset += chunk.length;
    }
    json.accessors = this.accessors;
    json.bufferViews = this.bufferViews;
    json.buffers = [{ byteLength: bin.length }];
    const text = new TextEncoder().encode(JSON.stringify(json));
    const jsonPad = (4 - (text.length % 4)) % 4;
    const binPad = (4 - (bin.length % 4)) % 4;
    const total = 28 + text.length + jsonPad + bin.length + binPad;
    const out = new Uint8Array(total);
    const view = new DataView(out.buffer);
    view.setUint32(0, 0x46546c67, true);
    view.setUint32(4, 2, true);
    view.setUint32(8, total, true);
    view.setUint32(12, text.length + jsonPad, true);
    view.setUint32(16, 0x4e4f534a, true);
    out.set(text, 20);
    out.fill(0x20, 20 + text.length, 20 + text.length + jsonPad);
    const binHeader = 20 + text.length + jsonPad;
    view.setUint32(binHeader, bin.length + binPad, true);
    view.setUint32(binHeader + 4, 0x004e4942, true);
    out.set(bin, binHeader + 8);
    return out;
  }
}

// sampleChannel reads the channel at t; cursor carries the key search forward, so t must not decrease between calls.
function sampleChannel(times, values, components, interpolation, path, t, cursor) {
  const count = times.length;
  const stride = interpolation === 'CUBICSPLINE' ? components * 3 : components;
  const base = interpolation === 'CUBICSPLINE' ? components : 0;
  const at = (k) => values.subarray(k * stride + base, k * stride + base + components);
  if (t <= times[0] || count === 1) {
    return Array.from(at(0));
  }
  if (t >= times[count - 1]) {
    return Array.from(at(count - 1));
  }
  let hi = Math.max(cursor.hi, 1);
  while (times[hi] < t) {
    hi++;
  }
  cursor.hi = hi;
  const lo = hi - 1;
  if (interpolation === 'STEP') {
    return Array.from(at(lo));
  }
  const f = (t - times[lo]) / (times[hi] - times[lo]);
  const a = at(lo);
  const b = at(hi);
  if (path !== 'rotation') {
    return Array.from(a, (v, i) => v + (b[i] - v) * f);
  }
  let dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3];
  const sign = dot < 0 ? -1 : 1;
  dot *= sign;
  let wa = 1 - f;
  let wb = f * sign;
  if (dot < 0.9995) {
    const theta = Math.acos(dot);
    const s = Math.sin(theta);
    wa = Math.sin((1 - f) * theta) / s;
    wb = (Math.sin(f * theta) / s) * sign;
  }
  const q = [0, 1, 2, 3].map((i) => a[i] * wa + b[i] * wb);
  const length = Math.hypot(...q) || 1;
  return q.map((v) => v / length);
}

function nearlyEqual(a, b, epsilon) {
  return a.every((v, i) => Math.abs(v - b[i]) <= epsilon);
}

function sameRotation(a, b) {
  const dot = Math.abs(a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3]);
  return 1 - dot <= ROTATION_EPSILON * ROTATION_EPSILON;
}

function restValue(node, path) {
  if (path === 'rotation') {
    return node.rotation ?? [0, 0, 0, 1];
  }
  if (path === 'scale') {
    return node.scale ?? [1, 1, 1];
  }
  return node.translation ?? [0, 0, 0];
}

function convertAnimation(source, animation, nodeIndex, nodes, writer, stats) {
  const channels = [];
  let duration = 0;
  for (const channel of animation.channels) {
    const sampler = animation.samplers[channel.sampler];
    const accessor = source.json.accessors[sampler.input];
    duration = Math.max(duration, accessor.max?.[0] ?? Math.max(...readAccessor(source, sampler.input)));
  }
  const frames = Math.max(1, Math.round(duration * FPS) + 1);
  const times = Array.from({ length: frames }, (_, k) => Math.min(k / FPS, duration));
  let timeAccessor = null;
  let singleAccessor = null;
  const out = { name: animation.name, channels: [], samplers: [] };
  for (const channel of animation.channels) {
    const name = source.json.nodes[channel.target.node]?.name;
    const target = nodeIndex.get(name);
    if (target == null) {
      stats.droppedChannels++;
      continue;
    }
    const sampler = animation.samplers[channel.sampler];
    const path = channel.target.path;
    const components = path === 'rotation' ? 4 : 3;
    const input = readAccessor(source, sampler.input);
    const output = readAccessor(source, sampler.output);
    const interpolation = sampler.interpolation || 'LINEAR';
    const cursor = { hi: 1 };
    const samples = times.map((t) => sampleChannel(input, output, components, interpolation, path, t, cursor));
    if (path === 'rotation') {
      for (let k = 1; k < samples.length; k++) {
        const p = samples[k - 1];
        const q = samples[k];
        if (p[0] * q[0] + p[1] * q[1] + p[2] * q[2] + p[3] * q[3] < 0) {
          samples[k] = q.map((v) => -v);
        }
      }
    }
    const same = path === 'rotation' ? sameRotation : (a, b) => nearlyEqual(a, b, VECTOR_EPSILON);
    const constant = samples.every((s) => same(s, samples[0]));
    if (constant && same(samples[0], restValue(nodes[target], path))) {
      stats.restChannels++;
      continue;
    }
    const kept = constant ? [samples[0]] : samples;
    let inputAccessor;
    if (constant) {
      singleAccessor ??= writer.floats([0], 'SCALAR', true);
      inputAccessor = singleAccessor;
    } else {
      timeAccessor ??= writer.floats(times, 'SCALAR', true);
      inputAccessor = timeAccessor;
    }
    let outputAccessor;
    if (path === 'rotation') {
      const data = Int16Array.from(kept.flat(), (v) => Math.round(Math.max(-1, Math.min(1, v)) * 32767));
      outputAccessor = writer.add(data, { componentType: 5122, normalized: true, count: kept.length, type: 'VEC4' });
    } else {
      outputAccessor = writer.floats(kept.flat(), 'VEC3', false);
    }
    out.samplers.push({ input: inputAccessor, output: outputAccessor, interpolation: 'LINEAR' });
    out.channels.push({ sampler: out.samplers.length - 1, target: { node: target, path } });
  }
  return { animation: out, duration };
}

function convertMeshes(model, writer) {
  const meshes = [];
  const map = new Map();
  for (const node of model.json.nodes) {
    if (node.mesh == null || map.has(node.mesh)) {
      continue;
    }
    const mesh = model.json.meshes[node.mesh];
    const positions = [];
    const joints = [];
    const weights = [];
    let maxJoint = 0;
    for (const primitive of mesh.primitives) {
      const { POSITION, JOINTS_0, WEIGHTS_0, JOINTS_1, WEIGHTS_1 } = primitive.attributes;
      if (POSITION == null || JOINTS_0 == null || WEIGHTS_0 == null) {
        continue;
      }
      const p = readAccessor(model, POSITION);
      const j0 = readAccessor(model, JOINTS_0);
      const w0 = readAccessor(model, WEIGHTS_0);
      const j1 = JOINTS_1 != null ? readAccessor(model, JOINTS_1) : null;
      const w1 = WEIGHTS_1 != null ? readAccessor(model, WEIGHTS_1) : null;
      const count = p.length / 3;
      for (let i = 0; i < count; i++) {
        positions.push(p[i * 3], p[i * 3 + 1], p[i * 3 + 2]);
        const influences = [];
        for (let k = 0; k < 4; k++) {
          influences.push([j0[i * 4 + k], w0[i * 4 + k]]);
          if (j1) {
            influences.push([j1[i * 4 + k], w1[i * 4 + k]]);
          }
        }
        influences.sort((a, b) => b[1] - a[1]);
        const top = influences.slice(0, 4);
        const sum = top.reduce((s, [, w]) => s + w, 0) || 1;
        const bytes = top.map(([, w]) => Math.round((w / sum) * 255));
        bytes[0] += 255 - bytes.reduce((s, w) => s + w, 0);
        for (const [joint] of top) {
          joints.push(joint);
          maxJoint = Math.max(maxJoint, joint);
        }
        weights.push(...bytes);
      }
    }
    if (positions.length === 0) {
      continue;
    }
    const count = positions.length / 3;
    const attributes = {
      POSITION: writer.floats(positions, 'VEC3', true),
      JOINTS_0:
        maxJoint < 256
          ? writer.add(Uint8Array.from(joints), { componentType: 5121, count, type: 'VEC4' })
          : writer.add(Uint16Array.from(joints), { componentType: 5123, count, type: 'VEC4' }),
      WEIGHTS_0: writer.add(Uint8Array.from(weights), { componentType: 5121, normalized: true, count, type: 'VEC4' }),
    };
    const materials = mesh.primitives.map((p) => p.extras?.material).filter(Boolean);
    const indexCounts = mesh.primitives.map((p) => p.extras?.indices).filter((n) => n != null);
    const extras = { materials };
    if (indexCounts.length) {
      extras.indices = indexCounts;
    }
    meshes.push({ name: mesh.name, primitives: [{ attributes }], extras });
    map.set(node.mesh, meshes.length - 1);
  }
  return { meshes, map };
}

function convertCharacter(game, id, files) {
  const model = parseGLB(files.get('model.glb'));
  const meta = JSON.parse(new TextDecoder().decode(files.get('meta.json')));
  const writer = new Writer();
  const nodes = model.json.nodes.map((node) => {
    const out = {};
    for (const key of ['name', 'translation', 'rotation', 'scale', 'matrix', 'children']) {
      if (node[key] != null) {
        out[key] = node[key];
      }
    }
    return out;
  });
  const nodeIndex = new Map();
  nodes.forEach((node, index) => {
    if (node.name != null && !nodeIndex.has(node.name)) {
      nodeIndex.set(node.name, index);
    }
  });

  const { meshes, map } = convertMeshes(model, writer);
  model.json.nodes.forEach((node, index) => {
    if (node.mesh != null && map.has(node.mesh)) {
      nodes[index].mesh = map.get(node.mesh);
      nodes[index].skin = node.skin;
    }
  });
  const skins = (model.json.skins ?? []).map((skin) => {
    const out = { joints: skin.joints };
    if (skin.inverseBindMatrices != null) {
      out.inverseBindMatrices = writer.floats(readAccessor(model, skin.inverseBindMatrices), 'MAT4', false);
    }
    if (skin.skeleton != null) {
      out.skeleton = skin.skeleton;
    }
    return out;
  });

  const stats = { droppedChannels: 0, restChannels: 0 };
  const animations = [];
  const clips = [];
  if (game === 'efmi') {
    for (const clip of meta.clips) {
      const source = parseGLB(files.get(`clips/${clip.name}.glb`));
      const [group, label] = EFMI_CLIPS[clip.name] ?? ['extras', clip.name];
      const { animation, duration } = convertAnimation(source, source.json.animations[0], nodeIndex, nodes, writer, stats);
      animation.name = label;
      animations.push(animation);
      clips.push({ name: label, group, mode: clip.loop ? 'loop' : 'once', seconds: Number(duration.toFixed(4)) });
    }
  } else {
    const source = parseGLB(files.get('anim.glb'));
    const info = new Map(meta.animations.map((a) => [a.name, a]));
    for (const animation of source.json.animations) {
      const converted = convertAnimation(source, animation, nodeIndex, nodes, writer, stats);
      animations.push(converted.animation);
      const entry = info.get(animation.name) ?? {};
      const clip = {
        name: animation.name,
        group: entry.group ?? 'extras',
        // Star Rail clips carry no mode; looping keeps idle and locomotion clips from freezing on their last frame.
        mode: entry.mode ?? 'loop',
        seconds: Number(converted.duration.toFixed(4)),
      };
      if (entry.loopFrom != null) {
        clip.loopFrom = entry.loopFrom;
      }
      clips.push(clip);
    }
  }

  const json = {
    asset: { version: '2.0', generator: 'nahida-desktop import-character-motion' },
    extras: {
      format: 2,
      fps: FPS,
      game,
      character: id,
      source: meta.source,
      sourceName: meta.name,
      heightM: meta.heightM ?? null,
      clips,
    },
    scene: 0,
    scenes: model.json.scenes ?? [{ nodes: [0] }],
    nodes,
    meshes,
    skins,
    animations,
  };
  return { glb: writer.glb(json), stats, clips: clips.length };
}

function resolveId(game, key, files) {
  const alias = ALIASES.filter((rule) => rule.key.test(key));
  if (alias.length === 0) {
    return key;
  }
  const meta = JSON.parse(new TextDecoder().decode(files.get('meta.json')));
  const match = alias.find((rule) => meta.materials.some((name) => rule.material.test(name)));
  if (!match) {
    throw new Error(`${game}/${key}: no alias matched materials ${meta.materials.join(', ')}`);
  }
  return match.id;
}

function readBatch(bytes) {
  const data = gunzipSync(bytes);
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const characters = new Map();
  let offset = 0;
  while (offset < data.length) {
    const nameLength = view.getUint32(offset, true);
    const dataLength = view.getUint32(offset + 4, true);
    const name = new TextDecoder().decode(data.subarray(offset + 8, offset + 8 + nameLength));
    const body = data.subarray(offset + 8 + nameLength, offset + 8 + nameLength + dataLength);
    offset += 8 + nameLength + dataLength;
    const [game, key, ...rest] = name.split('/');
    const id = `${game}/${key}`;
    if (!characters.has(id)) {
      characters.set(id, { game, key, files: new Map() });
    }
    characters.get(id).files.set(rest.join('/'), body);
  }
  return characters;
}

async function main() {
  const [rawDir, outDir, indexPath] = process.argv.slice(2);
  if (!rawDir || !outDir) {
    console.error('Usage: node scripts/import-character-motion.mjs <raw-dir> <out-dir> [index.json]');
    process.exit(2);
  }
  const batches = (await readdir(rawDir)).filter((name) => name.endsWith('.bin.gz')).sort();
  const index = {};
  let total = 0;
  for (const batch of batches) {
    const characters = readBatch(await readFile(path.join(rawDir, batch)));
    for (const { game, key, files } of characters.values()) {
      const id = resolveId(game, key, files);
      const { glb, stats, clips } = convertCharacter(game, id, files);
      const packed = gzipSync(glb, { level: 9 });
      const sha256 = createHash('sha256').update(packed).digest('hex');
      const file = `${game}/${id}.${sha256.slice(0, 12)}.glb.gz`;
      await mkdir(path.join(outDir, game), { recursive: true });
      await writeFile(path.join(outDir, file), packed);
      index[game] ??= {};
      if (index[game][id]) {
        throw new Error(`${game}/${id} appears twice`);
      }
      index[game][id] = { file, size: packed.length, sha256 };
      total += packed.length;
      console.log(
        `${game}/${id}: ${clips} clips, ${(glb.length / 1e6).toFixed(2)} MB -> ${(packed.length / 1e6).toFixed(2)} MB` +
          ` (rest ${stats.restChannels}, unmatched ${stats.droppedChannels})`,
      );
    }
  }
  const sorted = Object.fromEntries(
    Object.entries(index)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([game, rows]) => [game, Object.fromEntries(Object.entries(rows).sort(([a], [b]) => a.localeCompare(b)))]),
  );
  const text = `${JSON.stringify({ format: 1, characters: sorted }, null, 2)}\n`;
  await writeFile(path.join(outDir, 'index.json'), text);
  if (indexPath) {
    await mkdir(path.dirname(indexPath), { recursive: true });
    await writeFile(indexPath, text);
  }
  console.log(`${Object.values(index).reduce((n, rows) => n + Object.keys(rows).length, 0)} characters, ${(total / 1e6).toFixed(1)} MB`);
}

await main();
