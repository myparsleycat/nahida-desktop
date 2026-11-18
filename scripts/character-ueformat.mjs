// Reader for UEFormat files (.uemodel skeletal meshes and .ueanim animation sequences), the format FModel exports
// Unreal Engine assets to. Values stay in Unreal's own space: centimetres, Z up, left-handed, quaternions as
// (x, y, z, w).
//
// A file starts with "UEFORMAT", the identifier string, a version byte, the object name, from version 10 the
// object path, and a compression flag. Before version 10 the body is a run of chunks (name, count, byte size);
// from version 10 it is a chunk count followed by chunks (name, byte size) whose payload starts with its count.

const OBJECT_PATH_VERSION = 10;
const LOD_VERSION = 4;
const METADATA_VERSION = 7;

class Reader {
  constructor(bytes, start = 0, end = bytes.length) {
    this.bytes = bytes;
    this.view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    this.pos = start;
    this.end = end;
  }

  eof() {
    return this.pos >= this.end;
  }

  need(size) {
    if (size < 0 || this.pos + size > this.end) {
      throw new Error(`UEFormat read past the end at ${this.pos} (+${size})`);
    }
  }

  u8() {
    this.need(1);
    return this.view.getUint8(this.pos++);
  }

  u16() {
    this.need(2);
    const value = this.view.getUint16(this.pos, true);
    this.pos += 2;
    return value;
  }

  i32() {
    this.need(4);
    const value = this.view.getInt32(this.pos, true);
    this.pos += 4;
    return value;
  }

  u32() {
    this.need(4);
    const value = this.view.getUint32(this.pos, true);
    this.pos += 4;
    return value;
  }

  f32() {
    this.need(4);
    const value = this.view.getFloat32(this.pos, true);
    this.pos += 4;
    return value;
  }

  floats(count) {
    const out = new Array(count);
    for (let i = 0; i < count; i++) {
      out[i] = this.f32();
    }
    return out;
  }

  f32Array(count) {
    this.need(count * 4);
    const out = new Float32Array(count);
    for (let i = 0; i < count; i++) {
      out[i] = this.view.getFloat32(this.pos + i * 4, true);
    }
    this.pos += count * 4;
    return out;
  }

  u32Array(count) {
    this.need(count * 4);
    const out = new Uint32Array(count);
    for (let i = 0; i < count; i++) {
      out[i] = this.view.getUint32(this.pos + i * 4, true);
    }
    this.pos += count * 4;
    return out;
  }

  str() {
    const length = this.i32();
    this.need(length);
    const text = new TextDecoder().decode(this.bytes.subarray(this.pos, this.pos + length));
    this.pos += length;
    return text.replace(/\0+$/, '');
  }

  sub(size) {
    this.need(size);
    const reader = new Reader(this.bytes, this.pos, this.pos + size);
    this.pos += size;
    return reader;
  }
}

function header(bytes, identifier) {
  const magic = new TextDecoder().decode(bytes.subarray(0, 8));
  if (magic !== 'UEFORMAT') {
    throw new Error('not a UEFormat file');
  }
  const reader = new Reader(bytes, 8);
  const id = reader.str();
  if (id !== identifier) {
    throw new Error(`expected ${identifier}, got ${id}`);
  }
  const version = reader.u8();
  const name = reader.str();
  const objectPath = version >= OBJECT_PATH_VERSION ? reader.str() : null;
  if (reader.u8()) {
    throw new Error(`${name}: compressed UEFormat files are not supported`);
  }
  return { reader, version, name, objectPath };
}

// chunks yields [name, count, reader] for each chunk. From version 10 the count is the first value of the payload,
// read by the caller; count is then null.
function* chunks(reader, counted) {
  if (counted) {
    const total = reader.i32();
    for (let k = 0; k < total; k++) {
      const name = reader.str();
      const size = reader.i32();
      yield [name, null, reader.sub(size)];
    }
    return;
  }
  while (!reader.eof()) {
    const name = reader.str();
    const count = reader.i32();
    const size = reader.i32();
    yield [name, count, reader.sub(size)];
  }
}

function readLOD(name, body, counted) {
  const lod = {
    name,
    positions: new Float32Array(0),
    normals: null,
    binormalSigns: null,
    tangents: null,
    uvs: [],
    indices: new Uint32Array(0),
    colors: [],
    materials: [],
    weights: { bones: new Uint16Array(0), vertices: new Uint32Array(0), weights: new Float32Array(0) },
    morphs: [],
  };
  for (const [chunk, count, r] of chunks(body, counted)) {
    const total = () => (counted ? r.i32() : count);
    switch (chunk) {
      case 'VERTICES':
        lod.positions = r.f32Array(total() * 3);
        break;
      case 'INDICES':
        lod.indices = r.u32Array(total());
        break;
      case 'NORMALS': {
        const n = total();
        const packed = r.f32Array(n * 4);
        lod.normals = new Float32Array(n * 3);
        lod.binormalSigns = new Float32Array(n);
        for (let i = 0; i < n; i++) {
          lod.binormalSigns[i] = packed[i * 4];
          lod.normals.set(packed.subarray(i * 4 + 1, i * 4 + 4), i * 3);
        }
        break;
      }
      case 'TANGENTS':
        lod.tangents = r.f32Array(total() * 3);
        break;
      case 'TEXCOORDS': {
        const n = total();
        for (let k = 0; k < n; k++) {
          if (counted) {
            r.str();
          }
          lod.uvs.push(r.f32Array(r.i32() * 2));
        }
        break;
      }
      case 'VERTEXCOLORS': {
        const n = total();
        for (let k = 0; k < n; k++) {
          const colorName = r.str();
          const size = r.i32() * 4;
          r.need(size);
          lod.colors.push({ name: colorName, rgba: r.bytes.slice(r.pos, r.pos + size) });
          r.pos += size;
        }
        break;
      }
      case 'MATERIALS': {
        const n = total();
        for (let k = 0; k < n; k++) {
          lod.materials.push({ name: r.str(), path: r.str(), firstIndex: r.i32(), numFaces: r.i32() });
        }
        break;
      }
      case 'WEIGHTS': {
        const n = total();
        const bones = new Uint16Array(n);
        const vertices = new Uint32Array(n);
        const weights = new Float32Array(n);
        for (let k = 0; k < n; k++) {
          bones[k] = r.u16();
          vertices[k] = r.i32();
          weights[k] = r.f32();
        }
        lod.weights = { bones, vertices, weights };
        break;
      }
      case 'MORPHTARGETS': {
        const n = total();
        for (let k = 0; k < n; k++) {
          const morphName = r.str();
          const deltas = r.i32();
          const vertices = new Uint32Array(deltas);
          const positions = new Float32Array(deltas * 3);
          for (let d = 0; d < deltas; d++) {
            positions[d * 3] = r.f32();
            positions[d * 3 + 1] = r.f32();
            positions[d * 3 + 2] = r.f32();
            r.pos += 12;
            vertices[d] = r.u32();
          }
          if (deltas) {
            lod.morphs.push({ name: morphName, vertices, positions });
          }
        }
        break;
      }
      default:
        break;
    }
  }
  return lod;
}

function readBones(r, count, counted) {
  const bones = [];
  for (let k = 0; k < count; k++) {
    bones.push({
      name: r.str(),
      parent: r.i32(),
      translation: r.floats(3),
      rotation: r.floats(4),
      scale: counted ? r.floats(3) : [1, 1, 1],
    });
  }
  return bones;
}

// readUEModel returns the skeletal mesh's LODs and its reference skeleton (bone names, parents and local rest
// transforms).
export function readUEModel(bytes) {
  const { reader, version, name, objectPath } = header(bytes, 'UEMODEL');
  if (version < LOD_VERSION) {
    throw new Error(`${name}: UEFormat version ${version} has no LODs`);
  }
  const counted = version >= OBJECT_PATH_VERSION;
  const model = { name, objectPath, version, lods: [], bones: [], sockets: [] };
  for (const [chunk, count, r] of chunks(reader, counted)) {
    if (chunk === 'LODS') {
      const lods = counted ? r.i32() : count;
      for (let k = 0; k < lods; k++) {
        const lodName = r.str();
        model.lods.push(readLOD(lodName, counted ? r : r.sub(r.i32()), counted));
      }
    } else if (chunk === 'SKELETON') {
      for (const [part, partCount, s] of chunks(r, counted)) {
        if (part === 'BONES') {
          model.bones = readBones(s, counted ? s.i32() : partCount, counted);
        } else if (part === 'SOCKETS' && counted) {
          const sockets = s.i32();
          for (let k = 0; k < sockets; k++) {
            model.sockets.push({
              name: s.str(),
              bone: s.str(),
              translation: s.floats(3),
              rotation: s.floats(4),
              scale: s.floats(3),
            });
          }
        }
      }
    }
  }
  return model;
}

function readTracks(r, count) {
  const keys = (components) => {
    const n = r.i32();
    const out = new Array(n);
    for (let k = 0; k < n; k++) {
      out[k] = { frame: r.i32(), value: r.floats(components) };
    }
    return out;
  };
  const tracks = [];
  for (let k = 0; k < count; k++) {
    tracks.push({ name: r.str(), translation: keys(3), rotation: keys(4), scale: keys(3) });
  }
  return tracks;
}

function readCurves(r, count) {
  const curves = [];
  for (let k = 0; k < count; k++) {
    const name = r.str();
    const n = r.i32();
    const keys = new Array(n);
    for (let j = 0; j < n; j++) {
      keys[j] = { frame: r.i32(), value: r.f32() };
    }
    curves.push({ name, keys });
  }
  return curves;
}

// readUEAnim returns the sequence's frame count and rate, whether it is additive, its bone tracks (local
// transforms keyed by frame) and its float curves, which drive morph targets by name.
export function readUEAnim(bytes) {
  const { reader, version, name } = header(bytes, 'UEANIM');
  const anim = { name, version, frames: 0, fps: 30, additive: 0, tracks: [], curves: [] };
  const metadata = (r) => {
    anim.frames = r.i32();
    anim.fps = r.f32();
    r.str();
    anim.additive = r.u8();
  };
  const counted = version >= OBJECT_PATH_VERSION;
  if (!counted && version < METADATA_VERSION) {
    anim.frames = reader.i32();
    anim.fps = reader.f32();
  }
  for (const [chunk, count, r] of chunks(reader, counted)) {
    if (chunk === 'METADATA') {
      metadata(r);
    } else if (chunk === 'TRACKS') {
      anim.tracks = readTracks(r, counted ? r.i32() : count);
    } else if (chunk === 'CURVES') {
      anim.curves = readCurves(r, counted ? r.i32() : count);
    }
  }
  return anim;
}

// uemodelMaterialPaths lists the material paths of a model's first LOD.
export function uemodelMaterialPaths(bytes) {
  return readUEModel(bytes).lods[0]?.materials.map((material) => material.path) ?? [];
}
