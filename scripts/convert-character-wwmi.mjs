// Turns the Wuthering Waves character data downloaded by scripts/capture-character-wwmi.mjs into raw batches that
// scripts/import-character-motion.mjs and scripts/import-character-original.mjs read like the other games' batches.
// Each batch is a gzip stream of records (u32 name length, u32 data length, name, data) holding, per character:
//   wwmi/<id>/model.glb             skeleton nodes, one skin, one mesh per material section with indices, all
//                                   vertex attributes and its shape keys; materials name their textures by slot;
//                                   the textures are embedded as WebP images
//   wwmi/<id>/clips/<name>.glb      one animation per clip, shape key curves as weights channels
//   wwmi/<id>/meta.json             source names and clip metadata
//
// Everything stays in Unreal's model space, which is the space WWMI mod buffers use: centimetres, Z up. A model's
// parts share the skeleton, matched by bone name. Like the other games' original models, UVs are written as 1 - v of
// the game's and images flipped upside down to match, so the model viewer handles all of them alike.
//
// Usage: node scripts/convert-character-wwmi.mjs <source-dir> <raw-dir> [id ...]

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';
import { gzipSync } from 'node:zlib';

import { Writer } from './character-gltf.mjs';
import { readUEAnim, readUEModel } from './character-ueformat.mjs';

const BATCH_BYTES = 256e6;
// Texture slots kept per material: colour, normal with roughness and metal, type mask, hair or face mask, ramp.
const TEXTURE_SLOTS = ['MainTex', 'Normal_Roughness_Metallic', 'TypeMask', 'MaskTex', 'Ramp'];
const LOSSLESS_SLOTS = new Set(['TypeMask', 'MaskTex', 'Ramp']);
const BLINK_TARGETS = ['E_Close'];
// REST_TOLERANCE bounds how far apart, in centimetres or quaternion components, the rest transforms of a bone that
// the model and its parts share may lie: they are bound with one set of inverse bind matrices.
const REST_TOLERANCE = 1e-3;

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex');
}

// trsMatrix returns the column-major 4x4 matrix of translation t, rotation q and scale s.
function trsMatrix(t, q, s) {
  const [x, y, z, w] = q;
  const r = [
    1 - 2 * (y * y + z * z),
    2 * (x * y + z * w),
    2 * (x * z - y * w),
    2 * (x * y - z * w),
    1 - 2 * (x * x + z * z),
    2 * (y * z + x * w),
    2 * (x * z + y * w),
    2 * (y * z - x * w),
    1 - 2 * (x * x + y * y),
  ];
  return [
    r[0] * s[0], r[1] * s[0], r[2] * s[0], 0,
    r[3] * s[1], r[4] * s[1], r[5] * s[1], 0,
    r[6] * s[2], r[7] * s[2], r[8] * s[2], 0,
    t[0], t[1], t[2], 1,
  ];
}

function mat4Mul(a, b) {
  const out = new Array(16).fill(0);
  for (let c = 0; c < 4; c++) {
    for (let r = 0; r < 4; r++) {
      let sum = 0;
      for (let k = 0; k < 4; k++) {
        sum += a[k * 4 + r] * b[c * 4 + k];
      }
      out[c * 4 + r] = sum;
    }
  }
  return out;
}

function mat4Invert(m) {
  const inv = new Array(16);
  inv[0] = m[5] * m[10] * m[15] - m[5] * m[11] * m[14] - m[9] * m[6] * m[15] + m[9] * m[7] * m[14] + m[13] * m[6] * m[11] - m[13] * m[7] * m[10];
  inv[4] = -m[4] * m[10] * m[15] + m[4] * m[11] * m[14] + m[8] * m[6] * m[15] - m[8] * m[7] * m[14] - m[12] * m[6] * m[11] + m[12] * m[7] * m[10];
  inv[8] = m[4] * m[9] * m[15] - m[4] * m[11] * m[13] - m[8] * m[5] * m[15] + m[8] * m[7] * m[13] + m[12] * m[5] * m[11] - m[12] * m[7] * m[9];
  inv[12] = -m[4] * m[9] * m[14] + m[4] * m[10] * m[13] + m[8] * m[5] * m[14] - m[8] * m[6] * m[13] - m[12] * m[5] * m[10] + m[12] * m[6] * m[9];
  inv[1] = -m[1] * m[10] * m[15] + m[1] * m[11] * m[14] + m[9] * m[2] * m[15] - m[9] * m[3] * m[14] - m[13] * m[2] * m[11] + m[13] * m[3] * m[10];
  inv[5] = m[0] * m[10] * m[15] - m[0] * m[11] * m[14] - m[8] * m[2] * m[15] + m[8] * m[3] * m[14] + m[12] * m[2] * m[11] - m[12] * m[3] * m[10];
  inv[9] = -m[0] * m[9] * m[15] + m[0] * m[11] * m[13] + m[8] * m[1] * m[15] - m[8] * m[3] * m[13] - m[12] * m[1] * m[11] + m[12] * m[3] * m[9];
  inv[13] = m[0] * m[9] * m[14] - m[0] * m[10] * m[13] - m[8] * m[1] * m[14] + m[8] * m[2] * m[13] + m[12] * m[1] * m[10] - m[12] * m[2] * m[9];
  inv[2] = m[1] * m[6] * m[15] - m[1] * m[7] * m[14] - m[5] * m[2] * m[15] + m[5] * m[3] * m[14] + m[13] * m[2] * m[7] - m[13] * m[3] * m[6];
  inv[6] = -m[0] * m[6] * m[15] + m[0] * m[7] * m[14] + m[4] * m[2] * m[15] - m[4] * m[3] * m[14] - m[12] * m[2] * m[7] + m[12] * m[3] * m[6];
  inv[10] = m[0] * m[5] * m[15] - m[0] * m[7] * m[13] - m[4] * m[1] * m[15] + m[4] * m[3] * m[13] + m[12] * m[1] * m[7] - m[12] * m[3] * m[5];
  inv[14] = -m[0] * m[5] * m[14] + m[0] * m[6] * m[13] + m[4] * m[1] * m[14] - m[4] * m[2] * m[13] - m[12] * m[1] * m[6] + m[12] * m[2] * m[5];
  inv[3] = -m[1] * m[6] * m[11] + m[1] * m[7] * m[10] + m[5] * m[2] * m[11] - m[5] * m[3] * m[10] - m[9] * m[2] * m[7] + m[9] * m[3] * m[6];
  inv[7] = m[0] * m[6] * m[11] - m[0] * m[7] * m[10] - m[4] * m[2] * m[11] + m[4] * m[3] * m[10] + m[8] * m[2] * m[7] - m[8] * m[3] * m[6];
  inv[11] = -m[0] * m[5] * m[11] + m[0] * m[7] * m[9] + m[4] * m[1] * m[11] - m[4] * m[3] * m[9] - m[8] * m[1] * m[7] + m[8] * m[3] * m[5];
  inv[15] = m[0] * m[5] * m[10] - m[0] * m[6] * m[9] - m[4] * m[1] * m[10] + m[4] * m[2] * m[9] + m[8] * m[1] * m[6] - m[8] * m[2] * m[5];
  const det = m[0] * inv[0] + m[1] * inv[4] + m[2] * inv[8] + m[3] * inv[12];
  if (Math.abs(det) < 1e-12) {
    throw new Error('singular bone matrix');
  }
  return inv.map((v) => v / det);
}

function normalizeQuat(q) {
  const length = Math.hypot(...q) || 1;
  return q.map((v) => v / length);
}

// mergeSkeleton joins the bones of a model and its parts by name, keeping the first model's order.
function mergeSkeleton(models) {
  const bones = [];
  const byName = new Map();
  const remaps = [];
  for (const model of models) {
    const remap = [];
    model.bones.forEach((bone, index) => {
      let at = byName.get(bone.name);
      if (at != null) {
        const other = bones[at];
        const difference = Math.max(
          ...bone.translation.map((v, k) => Math.abs(v - other.translation[k])),
          ...bone.scale.map((v, k) => Math.abs(v - other.scale[k])),
          ...normalizeQuat(bone.rotation).map((v, k) =>
            Math.min(Math.abs(v - other.rotation[k]), Math.abs(v + other.rotation[k])),
          ),
        );
        if (difference > REST_TOLERANCE) {
          throw new Error(`${model.name}: bone ${bone.name} rests ${difference} away from ${other.model}'s`);
        }
      }
      if (at == null) {
        at = bones.length;
        byName.set(bone.name, at);
        const parentName = bone.parent >= 0 ? model.bones[bone.parent].name : null;
        bones.push({ ...bone, rotation: normalizeQuat(bone.rotation), parentName, model: model.name });
      }
      remap[index] = at;
    });
    remaps.push(remap);
  }
  for (const bone of bones) {
    bone.parent = bone.parentName == null ? -1 : byName.get(bone.parentName);
    if (bone.parent === undefined) {
      throw new Error(`bone ${bone.name} has an unknown parent ${bone.parentName}`);
    }
  }
  const worlds = [];
  const visit = (index) => {
    if (worlds[index]) {
      return;
    }
    const bone = bones[index];
    if (bone.parent >= 0) {
      visit(bone.parent);
    }
    const local = trsMatrix(bone.translation, bone.rotation, bone.scale);
    worlds[index] = bone.parent >= 0 ? mat4Mul(worlds[bone.parent], local) : local;
  };
  bones.forEach((_, index) => visit(index));
  return { bones, byName, remaps, worlds };
}

// influences returns, per vertex, its bone influences sorted by weight.
function influences(model, remap) {
  const lod = model.lods[0];
  const out = Array.from({ length: lod.positions.length / 3 }, () => []);
  const { bones, vertices, weights } = lod.weights;
  for (let k = 0; k < vertices.length; k++) {
    if (weights[k] > 0 && vertices[k] < out.length) {
      if (remap[bones[k]] === undefined) {
        throw new Error(`${model.name}: weight bone ${bones[k]} past ${model.bones.length} bones`);
      }
      out[vertices[k]].push([remap[bones[k]], weights[k]]);
    }
  }
  for (const list of out) {
    list.sort((a, b) => b[1] - a[1]);
  }
  return out;
}

function sectionMeshes(model, remap, writer, materialIndex, meshName) {
  const lod = model.lods[0];
  const vertexCount = lod.positions.length / 3;
  const weights = influences(model, remap);
  const slots = weights.some((list) => list.length > 4) ? 8 : 4;
  const meshes = [];
  for (const section of lod.materials) {
    const first = section.firstIndex;
    const end = Math.min(first + section.numFaces * 3, lod.indices.length);
    if (end <= first) {
      continue;
    }
    const used = new Map();
    const order = [];
    const indices = new Uint32Array(end - first);
    for (let i = first; i < end; i++) {
      const v = lod.indices[i];
      if (v >= vertexCount) {
        throw new Error(`${model.name}: index ${v} past ${vertexCount} vertices`);
      }
      let local = used.get(v);
      if (local == null) {
        local = order.length;
        used.set(v, local);
        order.push(v);
      }
      indices[i - first] = local;
    }
    const count = order.length;
    const pick = (source, width) => {
      const out = new Float32Array(count * width);
      order.forEach((v, i) => out.set(source.subarray(v * width, v * width + width), i * width));
      return out;
    };

    const attributes = { POSITION: writer.floats(pick(lod.positions, 3), 'VEC3', true) };
    if (lod.normals) {
      attributes.NORMAL = writer.floats(pick(lod.normals, 3), 'VEC3', false);
    }
    if (lod.tangents && lod.binormalSigns) {
      const tangents = new Float32Array(count * 4);
      order.forEach((v, i) => {
        tangents.set(lod.tangents.subarray(v * 3, v * 3 + 3), i * 4);
        tangents[i * 4 + 3] = lod.binormalSigns[v] < 0 ? -1 : 1;
      });
      attributes.TANGENT = writer.floats(tangents, 'VEC4', false);
    }
    lod.uvs.forEach((uv, k) => {
      const flipped = pick(uv, 2);
      for (let i = 1; i < flipped.length; i += 2) {
        flipped[i] = 1 - flipped[i];
      }
      attributes[`TEXCOORD_${k}`] = writer.floats(flipped, 'VEC2', false);
    });
    if (lod.colors.length) {
      const rgba = lod.colors[0].rgba;
      const colors = new Uint8Array(count * 4);
      order.forEach((v, i) => colors.set(rgba.subarray(v * 4, v * 4 + 4), i * 4));
      attributes.COLOR_0 = writer.add(colors, { componentType: 5121, normalized: true, count, type: 'VEC4' });
    }
    const joints = new Uint16Array(count * slots);
    const jointWeights = new Float32Array(count * slots);
    order.forEach((v, i) => {
      const list = weights[v].slice(0, slots);
      const sum = list.reduce((s, [, w]) => s + w, 0) || 1;
      list.forEach(([joint, w], k) => {
        joints[i * slots + k] = joint;
        jointWeights[i * slots + k] = w / sum;
      });
    });
    for (let set = 0; set < slots / 4; set++) {
      const j = new Uint16Array(count * 4);
      const w = new Float32Array(count * 4);
      for (let i = 0; i < count; i++) {
        j.set(joints.subarray(i * slots + set * 4, i * slots + set * 4 + 4), i * 4);
        w.set(jointWeights.subarray(i * slots + set * 4, i * slots + set * 4 + 4), i * 4);
      }
      attributes[`JOINTS_${set}`] = writer.add(j, { componentType: 5123, count, type: 'VEC4' });
      attributes[`WEIGHTS_${set}`] = writer.floats(w, 'VEC4', false);
    }

    const targetNames = [];
    const targets = [];
    for (const morph of lod.morphs) {
      const deltas = new Float32Array(count * 3);
      let touched = false;
      morph.vertices.forEach((v, d) => {
        const local = used.get(v);
        if (local != null) {
          deltas.set(morph.positions.subarray(d * 3, d * 3 + 3), local * 3);
          touched ||= morph.positions[d * 3] !== 0 || morph.positions[d * 3 + 1] !== 0 || morph.positions[d * 3 + 2] !== 0;
        }
      });
      if (touched) {
        targetNames.push(morph.name);
        targets.push({ POSITION: writer.floats(deltas, 'VEC3', false) });
      }
    }

    const primitive = {
      attributes,
      indices:
        count <= 65536
          ? writer.add(Uint16Array.from(indices), { componentType: 5123, count: indices.length, type: 'SCALAR' })
          : writer.add(indices, { componentType: 5125, count: indices.length, type: 'SCALAR' }),
      material: materialIndex(section),
      extras: { material: section.name, indices: indices.length },
    };
    const mesh = { name: meshName(section), primitives: [primitive] };
    if (targets.length) {
      primitive.targets = targets;
      mesh.extras = { targetNames };
      const blink = BLINK_TARGETS.filter((name) => targetNames.includes(name));
      if (blink.length) {
        mesh.extras.blink = blink;
      }
    }
    meshes.push(mesh);
  }
  return meshes;
}

// webp flips png upside down with ffmpeg and encodes it with cwebp, caching the result in cacheDir by the PNG's
// hash.
async function webp(png, lossless, cacheDir) {
  const key = `${sha256(png).slice(0, 24)}-v${lossless ? 'l' : ''}`;
  const cached = path.join(cacheDir, `${key}.webp`);
  try {
    return await readFile(cached);
  } catch {
    // Not encoded yet.
  }
  const source = path.join(os.tmpdir(), `nhd-wwmi-${process.pid}-${key}-source.png`);
  const input = path.join(os.tmpdir(), `nhd-wwmi-${process.pid}-${key}.png`);
  await writeFile(source, png);
  const args = lossless
    ? ['-quiet', '-lossless', '-exact', '-z', '6']
    : ['-quiet', '-q', '90', '-exact', '-alpha_q', '100', '-m', '6'];
  try {
    execFileSync('ffmpeg', ['-v', 'error', '-y', '-i', source, '-vf', 'vflip', '-pix_fmt', 'rgba', input]);
    execFileSync('cwebp', [...args, input, '-o', cached]);
  } finally {
    await rm(source, { force: true });
    await rm(input, { force: true });
  }
  return readFile(cached);
}

async function convertCharacter(sourceDir, manifest) {
  const files = path.join(sourceDir, 'files');
  const models = [];
  for (const file of manifest.models) {
    models.push(readUEModel(await readFile(path.join(files, file))));
  }
  const skeleton = mergeSkeleton(models);
  const writer = new Writer();

  const nodes = skeleton.bones.map((bone) => ({
    name: bone.name,
    translation: bone.translation,
    rotation: bone.rotation,
    scale: bone.scale,
  }));
  skeleton.bones.forEach((bone, index) => {
    if (bone.parent >= 0) {
      (nodes[bone.parent].children ??= []).push(index);
    }
  });
  const ibm = skeleton.worlds.flatMap((world) => mat4Invert(world));
  const skins = [{ joints: skeleton.bones.map((_, index) => index), inverseBindMatrices: writer.floats(ibm, 'MAT4', false) }];

  const materials = [];
  const materialByPath = new Map();
  const images = [];
  const textures = [];
  const textureByFile = new Map();
  const webpCache = path.join(sourceDir, 'webp');
  await mkdir(webpCache, { recursive: true });
  const textureIndex = async (objectPath, slot) => {
    const match = /^(?:Client\/Content|Game)\/(.+?)(?:\.[^/.]+)?$/.exec(objectPath);
    if (!match) {
      return null;
    }
    const file = `Game/${match[1]}.png`;
    if (textureByFile.has(file)) {
      return textureByFile.get(file);
    }
    let png;
    try {
      png = await readFile(path.join(files, file));
    } catch {
      textureByFile.set(file, null);
      return null;
    }
    const bytes = await webp(png, LOSSLESS_SLOTS.has(slot), webpCache);
    images.push({ name: path.posix.basename(file, '.png'), bufferView: writer.view(bytes), mimeType: 'image/webp' });
    textures.push({ source: images.length - 1 });
    textureByFile.set(file, textures.length - 1);
    return textures.length - 1;
  };
  for (const model of models) {
    for (const section of model.lods[0].materials) {
      if (materialByPath.has(section.path)) {
        continue;
      }
      const json = manifest.materials[section.path];
      const data = json ? JSON.parse(await readFile(path.join(files, json), 'utf8')) : {};
      const slots = {};
      for (const slot of TEXTURE_SLOTS) {
        const objectPath = data.Textures?.[slot]?.ObjectPath;
        if (objectPath) {
          const index = await textureIndex(objectPath, slot);
          if (index != null) {
            slots[slot] = index;
          }
        }
      }
      const extras = { textures: slots };
      const shading = data.Properties?.BasePropertyOverrides?.ShadingModel;
      if (shading) {
        extras.shader = shading;
      }
      materials.push({ name: section.name, extras });
      materialByPath.set(section.path, materials.length - 1);
    }
  }

  const meshes = [];
  const meshNames = new Set();
  models.forEach((model, m) => {
    const name = (section) => {
      let base = section.name.replace(/^MI_/, '');
      for (let k = 2; meshNames.has(base); k++) {
        base = `${section.name.replace(/^MI_/, '')}_${k}`;
      }
      meshNames.add(base);
      return base;
    };
    meshes.push(...sectionMeshes(model, skeleton.remaps[m], writer, (section) => materialByPath.get(section.path), name));
  });
  const meshNodes = meshes.map((mesh, index) => {
    nodes.push({ name: mesh.name, mesh: index, skin: 0 });
    return nodes.length - 1;
  });
  const roots = skeleton.bones.flatMap((bone, index) => (bone.parent < 0 ? [index] : []));

  let top = -Infinity;
  for (const model of models) {
    const positions = model.lods[0].positions;
    for (let i = 2; i < positions.length; i += 3) {
      top = Math.max(top, positions[i]);
    }
  }

  const modelJSON = {
    asset: { version: '2.0', generator: 'nahida-desktop convert-character-wwmi' },
    scene: 0,
    scenes: [{ nodes: [...roots, ...meshNodes] }],
    nodes,
    meshes,
    materials,
    skins,
    textures,
    images,
  };
  const modelGLB = writer.glb(modelJSON);

  const clipNodes = nodes.map((node) => {
    const out = { name: node.name };
    if (node.mesh != null && meshes[node.mesh].extras?.targetNames) {
      out.extras = { mesh: meshes[node.mesh].name, targetNames: meshes[node.mesh].extras.targetNames };
    }
    return out;
  });
  const clips = [];
  const clipFiles = new Map();
  for (const [k, clip] of manifest.clips.entries()) {
    const anim = readUEAnim(await readFile(path.join(files, clip.file)));
    if (anim.additive) {
      console.warn(`${manifest.id}: ${clip.label} is additive, skipped`);
      continue;
    }
    const name = `clip-${String(k).padStart(2, '0')}`;
    clipFiles.set(name, convertClip(anim, skeleton, clipNodes));
    clips.push({ name, label: clip.label, group: clip.group, loop: clip.loop, source: path.posix.basename(clip.file, '.ueanim') });
  }

  const meta = {
    source: 'encore.moe',
    name: manifest.name,
    skin: manifest.skin,
    materials: materials.map((material) => material.name),
    heightM: Number((top / 100).toFixed(3)),
    clips,
  };
  return { modelGLB, clipFiles, meta };
}

// convertClip writes one animation: bone tracks keyed at their frames, and per mesh with shape keys one weights
// channel sampled at every frame from the curves named after its keys.
function convertClip(anim, skeleton, clipNodes) {
  const writer = new Writer();
  const frames = Math.max(anim.frames, 1);
  // UEFormat's rate is frames over length, so frame k sits at k / (frames - 1) of the length.
  const step = frames > 1 ? frames / anim.fps / (frames - 1) : 0;
  const timeAccessors = new Map();
  const timesOf = (keys) => {
    const times = keys.map((key) => key.frame * step);
    const id = times.join(',');
    if (!timeAccessors.has(id)) {
      timeAccessors.set(id, writer.floats(times, 'SCALAR', true));
    }
    return timeAccessors.get(id);
  };
  const animation = { name: anim.name, channels: [], samplers: [] };
  const add = (node, pathName, keys, type) => {
    if (!keys.length) {
      return;
    }
    const values = pathName === 'rotation' ? keys.flatMap((key) => normalizeQuat(key.value)) : keys.flatMap((key) => key.value);
    animation.samplers.push({ input: timesOf(keys), output: writer.floats(values, type, false), interpolation: 'LINEAR' });
    animation.channels.push({ sampler: animation.samplers.length - 1, target: { node, path: pathName } });
  };
  for (const track of anim.tracks) {
    const node = skeleton.byName.get(track.name);
    if (node == null) {
      continue;
    }
    add(node, 'translation', track.translation, 'VEC3');
    add(node, 'rotation', track.rotation, 'VEC4');
    add(node, 'scale', track.scale, 'VEC3');
  }

  const curves = new Map(anim.curves.filter((curve) => curve.keys.length).map((curve) => [curve.name, curve.keys]));
  const allFrames = Array.from({ length: frames }, (_, k) => ({ frame: k }));
  clipNodes.forEach((node, index) => {
    const names = node.extras?.targetNames;
    if (!names || !names.some((name) => curves.has(name))) {
      return;
    }
    const values = new Float32Array(frames * names.length);
    names.forEach((name, t) => {
      const keys = curves.get(name);
      if (!keys) {
        return;
      }
      let hi = 0;
      for (let k = 0; k < frames; k++) {
        while (hi < keys.length && keys[hi].frame < k) {
          hi++;
        }
        let value;
        if (hi === 0) {
          value = keys[0].value;
        } else if (hi >= keys.length) {
          value = keys[keys.length - 1].value;
        } else {
          const a = keys[hi - 1];
          const b = keys[hi];
          value = a.value + ((b.value - a.value) * (k - a.frame)) / (b.frame - a.frame || 1);
        }
        values[k * names.length + t] = value;
      }
    });
    animation.samplers.push({ input: timesOf(allFrames), output: writer.floats(values, 'SCALAR', false), interpolation: 'LINEAR' });
    animation.channels.push({ sampler: animation.samplers.length - 1, target: { node: index, path: 'weights' } });
  });

  return writer.glb({
    asset: { version: '2.0', generator: 'nahida-desktop convert-character-wwmi' },
    nodes: clipNodes,
    animations: [animation],
  });
}

function record(name, data) {
  const nameBytes = Buffer.from(name, 'utf8');
  const header = Buffer.alloc(8);
  header.writeUInt32LE(nameBytes.length, 0);
  header.writeUInt32LE(data.length, 4);
  return [header, nameBytes, Buffer.from(data.buffer, data.byteOffset, data.byteLength)];
}

async function main() {
  const [sourceDir, rawDir, ...ids] = process.argv.slice(2);
  if (!sourceDir || !rawDir) {
    console.error('Usage: node scripts/convert-character-wwmi.mjs <source-dir> <raw-dir> [id ...]');
    process.exit(2);
  }
  const manifests = [];
  for (const file of (await readdir(path.join(sourceDir, 'characters'))).sort()) {
    const manifest = JSON.parse(await readFile(path.join(sourceDir, 'characters', file), 'utf8'));
    if (!ids.length || ids.includes(manifest.id)) {
      manifests.push(manifest);
    }
  }
  await mkdir(rawDir, { recursive: true });
  let parts = [];
  let size = 0;
  let batch = 0;
  const flush = async () => {
    if (!parts.length) {
      return;
    }
    const file = path.join(rawDir, `wwmi-${String(batch++).padStart(3, '0')}.bin.gz`);
    await writeFile(file, gzipSync(Buffer.concat(parts), { level: 6 }));
    parts = [];
    size = 0;
  };
  for (const manifest of manifests) {
    const { modelGLB, clipFiles, meta } = await convertCharacter(sourceDir, manifest);
    const prefix = `wwmi/${manifest.id}/`;
    const add = (name, data) => {
      parts.push(...record(prefix + name, data));
      size += data.length;
    };
    add('model.glb', modelGLB);
    for (const [name, glb] of clipFiles) {
      add(`clips/${name}.glb`, glb);
    }
    add('meta.json', new TextEncoder().encode(JSON.stringify(meta)));
    console.log(`${manifest.id}: model ${(modelGLB.length / 1e6).toFixed(1)} MB, ${meta.clips.length} clips`);
    if (size >= BATCH_BYTES) {
      await flush();
    }
  }
  await flush();
}

await main();
