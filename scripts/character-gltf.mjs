// glTF and record-batch helpers shared by the character import scripts.
//
// A batch is a gzip stream of records (u32 name length, u32 data length, name, data) named <game>/<key>/<file>.

import { gunzipSync } from 'node:zlib';

export const COMPONENTS = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 };
const BYTES = { 5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4 };
const NORMALIZE = { 5120: 127, 5121: 255, 5122: 32767, 5123: 65535 };

export function parseGLB(bytes) {
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

function readComponent(view, offset, componentType) {
  switch (componentType) {
    case 5126:
      return view.getFloat32(offset, true);
    case 5125:
      return view.getUint32(offset, true);
    case 5123:
      return view.getUint16(offset, true);
    case 5122:
      return view.getInt16(offset, true);
    case 5121:
      return view.getUint8(offset);
    default:
      return view.getInt8(offset);
  }
}

function viewOf({ json, bin }, bufferViewIndex, byteOffset) {
  const bufferView = json.bufferViews[bufferViewIndex];
  return {
    view: new DataView(bin.buffer, bin.byteOffset + (bufferView.byteOffset || 0) + (byteOffset || 0)),
    stride: bufferView.byteStride,
  };
}

// readAccessor reads an accessor as float64 values, normalized integers scaled to [-1, 1] or [0, 1] and sparse
// substitutions applied.
export function readAccessor(model, index) {
  const accessor = model.json.accessors[index];
  const components = COMPONENTS[accessor.type];
  const size = BYTES[accessor.componentType];
  const out = new Float64Array(accessor.count * components);
  const scale = accessor.normalized ? NORMALIZE[accessor.componentType] : 1;
  const value = (raw) => (accessor.normalized ? Math.max(raw / scale, -1) : raw);
  if (accessor.bufferView != null) {
    const { view, stride: byteStride } = viewOf(model, accessor.bufferView, accessor.byteOffset);
    const stride = byteStride || components * size;
    for (let i = 0; i < accessor.count; i++) {
      for (let c = 0; c < components; c++) {
        out[i * components + c] = value(readComponent(view, i * stride + c * size, accessor.componentType));
      }
    }
  }
  const sparse = accessor.sparse;
  if (sparse) {
    const indices = viewOf(model, sparse.indices.bufferView, sparse.indices.byteOffset).view;
    const values = viewOf(model, sparse.values.bufferView, sparse.values.byteOffset).view;
    const indexSize = BYTES[sparse.indices.componentType];
    for (let k = 0; k < sparse.count; k++) {
      const i = readComponent(indices, k * indexSize, sparse.indices.componentType);
      for (let c = 0; c < components; c++) {
        out[i * components + c] = value(readComponent(values, (k * components + c) * size, accessor.componentType));
      }
    }
  }
  return out;
}

export class Writer {
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

export function readBatch(bytes) {
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
