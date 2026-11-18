// Converts extracted character motions into the files the model viewer downloads, keyed by the character catalog's
// ids, and writes an index with each file's size and SHA-256.
//
// The input batches were extracted with the operators' permission: Genshin Impact, Honkai: Star Rail and Zenless Zone
// Zero from skowt.cc's 3D viewer, Arknights: Endfield from studio.warfarin.wiki; Wuthering Waves batches come from
// scripts/convert-character-wwmi.mjs. Each batch is a gzip stream of records (u32 name length, u32 data length,
// name, data) holding, per character:
//   <game>/<key>/model.glb  skeleton nodes, skins and the skinned meshes' POSITION, JOINTS_0 and WEIGHTS_0
//   <game>/<key>/anim.glb   (skowt) every clip as one animation
//   <game>/<key>/clips/<name>.glb  (Warfarin, Wuthering Waves) one animation per clip
//   <game>/<key>/meta.json  source names and clip metadata
//
// Each output file is a gzip-compressed GLB with the skeleton, the skins, the reference meshes (POSITION float32,
// JOINTS_0 u8/u16, WEIGHTS_0 normalized u8) and the clips resampled to 60 fps, the sources' key rate (rotations as
// normalized int16, translations and scales as float32, shape key weights as normalized int16 on the mesh's node,
// whose extras name the mesh and its keys). A channel that never leaves the joint's rest value, or holds every
// shape key at zero, is dropped. The sample rate and the clip metadata are in the root extras.
//
// Usage: node scripts/import-character-motion.mjs <raw-dir> <out-dir> [index.json]
//
// The index goes to <out-dir>/index.json and, when given, to index.json; an index already at index.json keeps the rows
// of the games this run does not import.

import { createHash } from 'node:crypto';
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { gzipSync } from 'node:zlib';

import { parseGLB, readAccessor, readBatch, writeIndex, Writer } from './character-gltf.mjs';

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
    if (path === 'weights') {
      const kept = convertWeights(source, channel, sampler, times, target, nodes, writer, stats);
      if (kept) {
        let input;
        if (kept.constant) {
          singleAccessor ??= writer.floats([0], 'SCALAR', true);
          input = singleAccessor;
        } else {
          timeAccessor ??= writer.floats(times, 'SCALAR', true);
          input = timeAccessor;
        }
        out.samplers.push({ input, output: kept.output, interpolation: 'LINEAR' });
        out.channels.push({ sampler: out.samplers.length - 1, target: { node: target, path } });
      }
      continue;
    }
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

// convertWeights resamples a shape key channel to times as normalized int16 weights, and names the keys on the
// output node. It returns null for a channel that holds every key at zero.
function convertWeights(source, channel, sampler, times, target, nodes, writer, stats) {
  const sourceNode = source.json.nodes[channel.target.node];
  const names = sourceNode.extras?.targetNames;
  const mesh = sourceNode.extras?.mesh;
  if (!Array.isArray(names) || names.length === 0 || !mesh) {
    stats.droppedChannels++;
    return null;
  }
  const count = names.length;
  const input = readAccessor(source, sampler.input);
  const output = readAccessor(source, sampler.output);
  const interpolation = sampler.interpolation || 'LINEAR';
  const cursor = { hi: 1 };
  const samples = times.map((t) => sampleChannel(input, output, count, interpolation, 'weights', t, cursor));
  if (samples.every((frame) => frame.every((v) => Math.abs(v) < 1 / 32767))) {
    stats.restChannels++;
    return null;
  }
  const morph = { mesh, targets: names };
  const existing = nodes[target].extras?.morph;
  if (existing && (existing.mesh !== mesh || JSON.stringify(existing.targets) !== JSON.stringify(names))) {
    throw new Error(`${sourceNode.name}: shape keys differ between clips`);
  }
  nodes[target].extras = { ...nodes[target].extras, morph };
  const constant = samples.every((frame) => nearlyEqual(frame, samples[0], 1 / 32767));
  const values = (constant ? [samples[0]] : samples).flat();
  stats.weightChannels++;
  // Weights may be negative; ones past [-1, 1] keep float precision.
  if (values.some((v) => Math.abs(v) > 1)) {
    return { constant, output: writer.floats(values, 'SCALAR', false) };
  }
  const data = Int16Array.from(values, (v) => Math.round(v * 32767));
  return {
    constant,
    output: writer.add(data, { componentType: 5122, normalized: true, count: data.length, type: 'SCALAR' }),
  };
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

  const stats = { droppedChannels: 0, restChannels: 0, weightChannels: 0 };
  const animations = [];
  const clips = [];
  if (game === 'efmi' || game === 'wwmi') {
    for (const clip of meta.clips) {
      const source = parseGLB(files.get(`clips/${clip.name}.glb`));
      const [group, label] = EFMI_CLIPS[clip.name] ?? [clip.group ?? 'extras', clip.label ?? clip.name];
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
          ` (rest ${stats.restChannels}, unmatched ${stats.droppedChannels}, shape keys ${stats.weightChannels})`,
      );
    }
  }
  await writeIndex(outDir, indexPath, index);
  console.log(`${Object.values(index).reduce((n, rows) => n + Object.keys(rows).length, 0)} characters, ${(total / 1e6).toFixed(1)} MB`);
}

await main();
