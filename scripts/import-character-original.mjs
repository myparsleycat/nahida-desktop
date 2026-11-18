// Converts extracted original character models into the files the model viewer downloads to fill parts and textures a
// mod does not ship, keyed by the character catalog's ids, and writes an index with each model file's size and SHA-256.
//
// The input batches were extracted with the operators' permission: Genshin Impact, Honkai: Star Rail and Zenless Zone
// Zero from skowt.cc's 3D viewer, Arknights: Endfield from studio.warfarin.wiki; Wuthering Waves batches come from
// scripts/convert-character-wwmi.mjs. Each batch holds, per character:
//   <game>/<key>/model.glb           the source model with every mesh attribute, its materials and (skowt, Wuthering
//                                    Waves) images
//   <game>/<key>/toon.glb            (skowt) the material textures that model.glb does not embed
//   <game>/<key>/textures/<hash>     (Warfarin) the external image buffers model.glb references
//   <game>/<key>/meta.json           source names
//
// Each character becomes <game>/<id>.<sha12>.glb.gz: the skeleton, the skins, every skinned primitive with indices and
// all of its attributes (joints and weights folded to the four strongest influences), and one material per source
// material whose extras name the texture of each shader slot. A primitive keeps its shape keys as POSITION morph
// targets with the mesh's default weights; its mesh's extras name them and, for skowt, list the ones that close
// the eyes. Textures are written once as textures/<sha12>.webp,
// shared between characters, and referenced by file, size and SHA-256. skowt splits some images into a colour image
// (<name> or <name>.rgb) and a lossless alpha image (<name>.alpha) whose red channel is the alpha; the reference keeps
// both and says which channel holds the alpha.
//
// Usage: node scripts/import-character-original.mjs <raw-dir> <out-dir> [index.json]
//
// The index goes to <out-dir>/index.json and, when given, to index.json; an index already at index.json keeps the rows
// of the games this run does not import.

import { createHash } from 'node:crypto';
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { gzipSync } from 'node:zlib';

import { parseGLB, readAccessor, readBatch, writeIndex, Writer } from './character-gltf.mjs';

// skowt names both Trailblazers and both March 7th forms alike; their material names tell them apart.
const ALIASES = [
  { key: /^trailblazer#\d$/, material: /PlayerGirl/, id: 'stelle' },
  { key: /^trailblazer#\d$/, material: /PlayerBoy/, id: 'caelus' },
  { key: /^march-7th#\d$/, material: /^Mar_7th_00_/, id: 'march-7th' },
  { key: /^march-7th#\d$/, material: /^Mar_7th_10_/, id: 'march-7th-the-hunt' },
];

// Warfarin's shadow overlay shader: eye and hair shadow cards drawn translucent over the face.
const EFMI_OVERLAY_SHADER = 'Q8fb';

const SKIN_ATTRIBUTES = new Set(['JOINTS_0', 'JOINTS_1', 'WEIGHTS_0', 'WEIGHTS_1']);
const SCALE = { 5120: 127, 5121: 255, 5122: 32767, 5123: 65535 };
const TYPED = { 5120: Int8Array, 5121: Uint8Array, 5122: Int16Array, 5123: Uint16Array };

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex');
}

function partRole(game, meshName, material) {
  if (game === 'efmi' && material?.extras?.shader === EFMI_OVERLAY_SHADER) {
    return 'overlay';
  }
  const name = `${meshName} ${material?.name ?? ''}`.toLowerCase();
  if (name.includes('brow')) {
    return 'brow';
  }
  if (/eye|iris|pupil/.test(name)) {
    return 'eye';
  }
  if (/mouth|teeth|tongue/.test(name)) {
    return 'mouth';
  }
  if (name.includes('face')) {
    return 'face';
  }
  if (name.includes('hair') || (game === 'wwmi' && /bangs?\d*(?:\s|$)/.test(name))) {
    return 'hair';
  }
  return 'body';
}

function hasAlphaChunk(bytes) {
  for (let offset = 12; offset + 8 <= bytes.length; ) {
    const id = String.fromCharCode(...bytes.subarray(offset, offset + 4));
    if (id === 'ALPH') {
      return true;
    }
    const size = new DataView(bytes.buffer, bytes.byteOffset + offset + 4, 4).getUint32(0, true);
    offset += 8 + size + (size & 1);
  }
  return false;
}

function isWebP(bytes) {
  const text = String.fromCharCode(...bytes.subarray(0, 4)) + String.fromCharCode(...bytes.subarray(8, 12));
  return text === 'RIFFWEBP';
}

class TextureStore {
  constructor(outDir) {
    this.outDir = outDir;
    this.written = new Map();
    this.bytes = 0;
  }

  async add(bytes) {
    if (!isWebP(bytes)) {
      throw new Error('texture is not WebP');
    }
    const digest = sha256(bytes);
    const file = `textures/${digest.slice(0, 12)}.webp`;
    if (!this.written.has(file)) {
      await mkdir(path.join(this.outDir, 'textures'), { recursive: true });
      await writeFile(path.join(this.outDir, file), bytes);
      this.written.set(file, digest);
      this.bytes += bytes.length;
    } else if (this.written.get(file) !== digest) {
      throw new Error(`${file}: prefix collision`);
    }
    return { file, size: bytes.length, sha256: digest };
  }
}

function glbImages(glb, external) {
  const images = new Map();
  for (const image of glb.json.images ?? []) {
    if (image.bufferView == null || image.name == null) {
      continue;
    }
    const view = glb.json.bufferViews[image.bufferView];
    const buffer = view.buffer ?? 0;
    const data = buffer === 0 ? glb.bin : external(glb.json.buffers[buffer]);
    images.set(image.name, data.subarray(view.byteOffset ?? 0, (view.byteOffset ?? 0) + view.byteLength));
  }
  return images;
}

async function skowtTexture(images, name, store) {
  const base = images.get(name);
  const rgb = images.get(`${name}.rgb`);
  const alpha = images.get(`${name}.alpha`);
  const color = rgb ?? base;
  if (!color) {
    return null;
  }
  const ref = { name, color: await store.add(color) };
  if (alpha) {
    ref.alpha = { ...(await store.add(alpha)), channel: 'r' };
  } else if (rgb && base && hasAlphaChunk(base)) {
    ref.alpha = { ...(await store.add(base)), channel: 'a' };
  }
  return ref;
}

async function convertMaterials(game, model, images, store) {
  const json = model.json;
  const materials = [];
  for (const material of json.materials ?? []) {
    const textures = {};
    if (game === 'efmi' || game === 'wwmi') {
      for (const [slot, index] of Object.entries(material.extras?.textures ?? {})) {
        const texture = json.textures?.[index];
        const source = texture?.source ?? texture?.extensions?.EXT_texture_webp?.source;
        const name = json.images?.[source]?.name;
        const bytes = name != null ? images.get(name) : null;
        if (bytes) {
          textures[slot] = { name, color: await store.add(bytes) };
        }
      }
    } else {
      for (const [slot, name] of Object.entries(material.extras?.game?.textures ?? {})) {
        const ref = await skowtTexture(images, name, store);
        if (ref) {
          textures[slot] = ref;
        }
      }
    }
    const extras = { textures };
    if ((game === 'efmi' || game === 'wwmi') && material.extras?.shader) {
      extras.shader = material.extras.shader;
    }
    if (game !== 'efmi' && material.extras?.game?.shaderName) {
      extras.shader = material.extras.game.shaderName;
    }
    materials.push({ name: material.name, extras });
  }
  return materials;
}

function writeAttribute(model, writer, accessorIndex, name) {
  const accessor = model.json.accessors[accessorIndex];
  const values = readAccessor(model, accessorIndex);
  if (accessor.componentType === 5126 || !accessor.normalized) {
    return writer.floats(values, accessor.type, name === 'POSITION');
  }
  const scale = SCALE[accessor.componentType];
  const data = TYPED[accessor.componentType].from(values, (v) => Math.round(v * scale));
  return writer.add(data, {
    componentType: accessor.componentType,
    normalized: true,
    count: accessor.count,
    type: accessor.type,
  });
}

function writeSkinning(model, writer, attributes, count) {
  const j0 = readAccessor(model, attributes.JOINTS_0);
  const w0 = readAccessor(model, attributes.WEIGHTS_0);
  const j1 = attributes.JOINTS_1 != null ? readAccessor(model, attributes.JOINTS_1) : null;
  const w1 = attributes.WEIGHTS_1 != null ? readAccessor(model, attributes.WEIGHTS_1) : null;
  const joints = new Uint16Array(count * 4);
  const weights = new Uint8Array(count * 4);
  let maxJoint = 0;
  for (let i = 0; i < count; i++) {
    const influences = [];
    for (let k = 0; k < 4; k++) {
      influences.push([j0[i * 4 + k], w0[i * 4 + k]]);
      if (j1) {
        influences.push([j1[i * 4 + k], w1[i * 4 + k]]);
      }
    }
    influences.sort((a, b) => b[1] - a[1]);
    const top = influences.slice(0, 4);
    const sum = top.reduce((s, [, w]) => s + w, 0);
    // A vertex without weights keeps none, so the viewer leaves it at rest instead of binding it to a joint.
    const bytes = top.map(([, w]) => (sum > 0 ? Math.round((w / sum) * 255) : 0));
    if (sum > 0) {
      bytes[0] += 255 - bytes.reduce((s, w) => s + w, 0);
    }
    top.forEach(([joint], k) => {
      joints[i * 4 + k] = joint;
      maxJoint = Math.max(maxJoint, joint);
    });
    weights.set(bytes, i * 4);
  }
  return {
    JOINTS_0:
      maxJoint < 256
        ? writer.add(Uint8Array.from(joints), { componentType: 5121, count, type: 'VEC4' })
        : writer.add(joints, { componentType: 5123, count, type: 'VEC4' }),
    WEIGHTS_0: writer.add(weights, { componentType: 5121, normalized: true, count, type: 'VEC4' }),
  };
}

function convertMeshes(game, model, writer, stats) {
  const json = model.json;
  const meshes = [];
  const map = new Map();
  // Primitives of one source mesh often share their vertex accessors; each is written once.
  const written = new Map();
  const once = (key, write) => {
    if (!written.has(key)) {
      written.set(key, write());
    }
    return written.get(key);
  };
  for (const node of json.nodes) {
    if (node.mesh == null || node.skin == null || map.has(node.mesh)) {
      continue;
    }
    const mesh = json.meshes[node.mesh];
    const primitives = [];
    for (const primitive of mesh.primitives) {
      const { attributes } = primitive;
      if (attributes.POSITION == null || attributes.JOINTS_0 == null || attributes.WEIGHTS_0 == null) {
        continue;
      }
      if (primitive.indices == null) {
        throw new Error(`${mesh.name}: primitive without indices`);
      }
      if ((primitive.mode ?? 4) !== 4) {
        throw new Error(`${mesh.name}: primitive mode ${primitive.mode}`);
      }
      const count = json.accessors[attributes.POSITION].count;
      const out = {};
      for (const [name, index] of Object.entries(attributes)) {
        if (!SKIN_ATTRIBUTES.has(name)) {
          out[name] = once(`${name}:${index}`, () => writeAttribute(model, writer, index, name));
        }
      }
      const skinKey = ['JOINTS_0', 'WEIGHTS_0', 'JOINTS_1', 'WEIGHTS_1'].map((name) => attributes[name]).join(',');
      Object.assign(out, once(`skin:${skinKey}`, () => writeSkinning(model, writer, attributes, count)));

      const indices = readAccessor(model, primitive.indices);
      const maxIndex = indices.reduce((m, v) => Math.max(m, v), 0);
      if (maxIndex >= count) {
        throw new Error(`${mesh.name}: index ${maxIndex} past ${count} vertices`);
      }
      const indexAccessor =
        count <= 65536
          ? writer.add(Uint16Array.from(indices), { componentType: 5123, count: indices.length, type: 'SCALAR' })
          : writer.add(Uint32Array.from(indices), { componentType: 5125, count: indices.length, type: 'SCALAR' });
      const material = json.materials?.[primitive.material];
      const converted = {
        attributes: out,
        indices: indexAccessor,
        material: primitive.material,
        extras: { role: partRole(game, mesh.name, material) },
      };
      if (primitive.targets?.length) {
        converted.targets = primitive.targets.map((target, k) => {
          if (target.POSITION == null) {
            throw new Error(`${mesh.name}: shape key ${k} has no POSITION`);
          }
          return {
            POSITION: once(`target:${target.POSITION}`, () =>
              writer.floats(readAccessor(model, target.POSITION), 'VEC3', false),
            ),
          };
        });
        stats.targets += primitive.targets.length;
      }
      primitives.push(converted);
      if (!written.has(`counted:${attributes.POSITION}`)) {
        written.set(`counted:${attributes.POSITION}`, true);
        stats.vertices += count;
      }
      stats.triangles += indices.length / 3;
    }
    if (primitives.length === 0) {
      continue;
    }
    const out = { name: mesh.name, primitives };
    const names = mesh.extras?.targetNames;
    if (primitives.some((primitive) => primitive.targets)) {
      const count = primitives.find((primitive) => primitive.targets).targets.length;
      if (!Array.isArray(names) || names.length !== count) {
        throw new Error(`${mesh.name}: ${count} shape keys but ${names?.length ?? 0} names`);
      }
      out.extras = { targetNames: names };
      if (mesh.weights?.some((weight) => weight !== 0)) {
        if (mesh.weights.length !== count) {
          throw new Error(`${mesh.name}: ${count} shape keys but ${mesh.weights.length} default weights`);
        }
        out.weights = mesh.weights;
      }
      const blink = (mesh.extras?.blink ?? mesh.extras?.skowt?.blink)?.filter((name) => names.includes(name));
      if (blink?.length) {
        out.extras.blink = blink;
      }
    }
    meshes.push(out);
    map.set(node.mesh, meshes.length - 1);
  }
  return { meshes, map };
}

async function convertCharacter(game, id, files, store) {
  const meta = JSON.parse(new TextDecoder().decode(files.get('meta.json')));
  const model = parseGLB(files.get('model.glb'));
  const external = (buffer) => {
    const hash = buffer.uri?.split('/').pop();
    const bytes = hash != null ? files.get(`textures/${hash}`) : null;
    if (!bytes) {
      throw new Error(`missing external buffer ${buffer.uri}`);
    }
    return bytes;
  };
  const images = glbImages(model, external);
  if (files.has('toon.glb')) {
    for (const [name, bytes] of glbImages(parseGLB(files.get('toon.glb')), external)) {
      if (!images.has(name)) {
        images.set(name, bytes);
      }
    }
  }
  for (const accessor of model.json.accessors) {
    if (accessor.bufferView != null && (model.json.bufferViews[accessor.bufferView].buffer ?? 0) !== 0) {
      throw new Error('accessor outside the GLB buffer');
    }
  }

  const writer = new Writer();
  const stats = { vertices: 0, triangles: 0, targets: 0 };
  const nodes = model.json.nodes.map((node) => {
    const out = {};
    for (const key of ['name', 'translation', 'rotation', 'scale', 'matrix', 'children']) {
      if (node[key] != null) {
        out[key] = node[key];
      }
    }
    return out;
  });
  const { meshes, map } = convertMeshes(game, model, writer, stats);
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
  const materials = await convertMaterials(game, model, images, store);

  const json = {
    asset: { version: '2.0', generator: 'nahida-desktop import-character-original' },
    extras: { format: 1, game, character: id, source: meta.source, sourceName: meta.name },
    scene: 0,
    scenes: model.json.scenes ?? [{ nodes: [0] }],
    nodes,
    meshes,
    materials,
    skins,
  };
  const textureBytes = new Map();
  for (const material of materials) {
    for (const ref of Object.values(material.extras.textures)) {
      for (const part of [ref.color, ref.alpha]) {
        if (part) {
          textureBytes.set(part.file, part.size);
        }
      }
    }
  }
  stats.textures = textureBytes.size;
  stats.textureBytes = [...textureBytes.values()].reduce((s, n) => s + n, 0);
  return { glb: writer.glb(json), stats };
}

function resolveId(game, key, files) {
  const alias = ALIASES.filter((rule) => rule.key.test(key));
  if (alias.length === 0) {
    return key;
  }
  const names = (parseGLB(files.get('model.glb')).json.materials ?? []).map((m) => m.name);
  const match = alias.find((rule) => names.some((name) => rule.material.test(name)));
  if (!match) {
    throw new Error(`${game}/${key}: no alias matched materials ${names.join(', ')}`);
  }
  return match.id;
}

async function main() {
  const [rawDir, outDir, indexPath] = process.argv.slice(2);
  if (!rawDir || !outDir) {
    console.error('Usage: node scripts/import-character-original.mjs <raw-dir> <out-dir> [index.json]');
    process.exit(2);
  }
  const batches = (await readdir(rawDir)).filter((name) => name.endsWith('.bin.gz')).sort();
  const store = new TextureStore(outDir);
  const index = {};
  let total = 0;
  for (const batch of batches) {
    const characters = readBatch(await readFile(path.join(rawDir, batch)));
    for (const { game, key, files } of characters.values()) {
      const id = resolveId(game, key, files);
      const { glb, stats } = await convertCharacter(game, id, files, store);
      const packed = gzipSync(glb, { level: 9 });
      const digest = sha256(packed);
      const file = `${game}/${id}.${digest.slice(0, 12)}.glb.gz`;
      await mkdir(path.join(outDir, game), { recursive: true });
      await writeFile(path.join(outDir, file), packed);
      index[game] ??= {};
      if (index[game][id]) {
        throw new Error(`${game}/${id} appears twice`);
      }
      index[game][id] = { file, size: packed.length, sha256: digest };
      total += packed.length;
      console.log(
        `${game}/${id}: ${stats.vertices} vertices, ${stats.triangles} triangles, ${stats.targets} shape keys,` +
          ` ${(packed.length / 1e6).toFixed(2)} MB; ${stats.textures} textures ${(stats.textureBytes / 1e6).toFixed(2)} MB`,
      );
    }
  }
  await writeIndex(outDir, indexPath, index);
  const count = Object.values(index).reduce((n, rows) => n + Object.keys(rows).length, 0);
  console.log(
    `${count} characters, models ${(total / 1e6).toFixed(1)} MB, ` +
      `${store.written.size} textures ${(store.bytes / 1e6).toFixed(1)} MB`,
  );
}

await main();
