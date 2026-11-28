// In-page capture helper for the skowt character data refresh.
//
// Paste the whole file into browser.evaluate({ tabID, script: <file text> }) on a
// skowt.cc model page. It installs window.__cap / window.__job and loads the
// remaining targets from the local receiver (scripts/skowt/server.mjs).
//
// Do not run this unless access to skowt's viewer is working normally for this
// client. The receiver only accepts the gzip record stream; it does not post
// anything to skowt.

(async () => {
  const { MeshoptDecoder } = await import('https://cdn.jsdelivr.net/npm/meshoptimizer@0.22.0/meshopt_decoder.module.js');
  await MeshoptDecoder.ready;

  const enc = new TextEncoder();
  const wasmCache = new Map();

  function b64u(s) {
    if (!s) return new Uint8Array(0);
    const t = s.replace(/-/g, '+').replace(/_/g, '/') + '==';
    const e = atob(t);
    const o = new Uint8Array(e.length);
    for (let i = 0; i < e.length; i++) o[i] = e.charCodeAt(i);
    return o;
  }

  async function getWasm(id) {
    if (wasmCache.has(id)) return wasmCache.get(id);
    const p = (async () => {
      const r = await fetch('https://den.skowt.cc/viewer/decoder/' + id, { cache: 'no-store' });
      if (!r.ok) throw new Error('decoder ' + r.status);
      const b = new Uint8Array(await r.arrayBuffer());
      const { instance } = await WebAssembly.instantiate(b);
      return { ex: instance.exports, bytes: b };
    })();
    wasmCache.set(id, p);
    try {
      return await p;
    } catch (e) {
      wasmCache.delete(id);
      throw e;
    }
  }

  async function visit() {
    try {
      await fetch('https://skowt.cc/_visit', { method: 'POST', credentials: 'same-origin', cache: 'no-store' });
    } catch {}
  }

  async function decode(assetId, role) {
    for (let attempt = 0; attempt < 8; attempt++) {
      const lr = await fetch(
        'https://den.skowt.cc/viewer/links?a=' + encodeURIComponent(assetId + ':' + role) + '&g=3,4&l=1&vr=1',
        { credentials: 'include', cache: 'no-store' },
      );
      if (lr.status === 401) {
        await visit();
        await new Promise((r) => setTimeout(r, 1500));
        continue;
      }
      if (lr.status === 429) {
        await new Promise((r) => setTimeout(r, 8000));
        continue;
      }
      if (!lr.ok) {
        const text = await lr.text().catch(() => '');
        throw new Error('links ' + lr.status + ' ' + text.slice(0, 160));
      }
      const L = await lr.json();
      const name = L.assets?.[assetId]?.[role];
      if (!name) throw new Error('no ' + role + ' link');
      const fr = await fetch('https://pack.skowt.cc/v/' + name + '?' + L.links[name], {
        credentials: 'include',
        cache: 'no-store',
      });
      if (fr.status === 429) {
        await new Promise((r) => setTimeout(r, 8000));
        continue;
      }
      if (!fr.ok) throw new Error('file ' + fr.status);
      const bytes = new Uint8Array(await fr.arrayBuffer());
      const { ex, bytes: wasmBytes } = await getWasm(L.decoder);
      const ptr = ex.h(bytes.length);
      let mem = new Uint8Array(ex.m.buffer);
      mem.set(bytes, ptr);
      mem.fill(0, 0, 16);
      mem.fill(0, 5376, 5412);
      mem.set(b64u(L.keys[name]), 0);
      mem.set(new Uint8Array(await crypto.subtle.digest('SHA-256', wasmBytes)), 16);
      const mark = b64u(L.mark);
      if (mark.length === 16) mem.set(mark, 5376);
      const digest = b64u(L.digests[name]);
      if (digest.length === 16) mem.set(digest, 5392);
      if (L.locks && L.locks[name]) new DataView(ex.m.buffer).setUint32(5408, L.locks[name] >>> 0, true);
      const err = ex.o(ptr, bytes.length);
      if (err) throw new Error('open ' + err);
      mem = new Uint8Array(ex.m.buffer);
      const dv = new DataView(ex.m.buffer);
      const v = [0, 1, 2, 3, 4].map((i) => dv.getUint32(64 + 4 * i, true));
      return {
        role,
        json: JSON.parse(new TextDecoder().decode(mem.subarray(v[1], v[1] + v[2]))),
        bin: v[4] ? mem.slice(v[3], v[3] + v[4]) : null,
      };
    }
    throw new Error('rate limited');
  }

  const writeGLB = (json, bin) => {
    const jb = new TextEncoder().encode(JSON.stringify(json));
    const jp = (4 - (jb.length % 4)) % 4;
    const bp = bin ? (4 - (bin.length % 4)) % 4 : 0;
    const total = 12 + 8 + jb.length + jp + (bin ? 8 + bin.length + bp : 0);
    const out = new Uint8Array(total);
    const dv = new DataView(out.buffer);
    dv.setUint32(0, 0x46546c67, true);
    dv.setUint32(4, 2, true);
    dv.setUint32(8, total, true);
    dv.setUint32(12, jb.length + jp, true);
    dv.setUint32(16, 0x4e4f534a, true);
    out.set(jb, 20);
    out.fill(0x20, 20 + jb.length, 20 + jb.length + jp);
    if (bin) {
      const o = 20 + jb.length + jp;
      dv.setUint32(o, bin.length + bp, true);
      dv.setUint32(o + 4, 0x004e4942, true);
      out.set(bin, o + 8);
    }
    return out;
  };

  const nodeCopy = (n) => {
    const o = {};
    for (const k of ['name', 'translation', 'rotation', 'scale', 'matrix', 'children']) if (n[k] != null) o[k] = n[k];
    return o;
  };

  function builder(src) {
    const { json, bin } = src;
    const views = [];
    const accs = [];
    const chunks = [];
    let len = 0;
    const vmap = new Map();
    const amap = new Map();

    const viewBytes = (i) => {
      const bv = json.bufferViews[i];
      const e = bv.extensions?.EXT_meshopt_compression;
      if (e) {
        if ((e.buffer || 0) !== 0) throw new Error('meshopt buffer ' + e.buffer);
        const s = bin.subarray(e.byteOffset || 0, (e.byteOffset || 0) + e.byteLength);
        const dst = new Uint8Array(e.count * e.byteStride);
        MeshoptDecoder.decodeGltfBuffer(dst, e.count, e.byteStride, s, e.mode, e.filter || 'NONE');
        return dst;
      }
      if ((bv.buffer || 0) !== 0) throw new Error('buffer ' + bv.buffer);
      return bin.subarray(bv.byteOffset || 0, (bv.byteOffset || 0) + bv.byteLength);
    };

    const addView = (i) => {
      if (vmap.has(i)) return vmap.get(i);
      const b = viewBytes(i);
      const pad = (4 - (len % 4)) % 4;
      if (pad) {
        chunks.push(new Uint8Array(pad));
        len += pad;
      }
      const v = { buffer: 0, byteOffset: len, byteLength: b.length };
      const bv = json.bufferViews[i];
      if (bv.byteStride) v.byteStride = bv.byteStride;
      chunks.push(b.slice());
      len += b.length;
      views.push(v);
      vmap.set(i, views.length - 1);
      return views.length - 1;
    };

    const addAcc = (i) => {
      if (i == null) return undefined;
      if (amap.has(i)) return amap.get(i);
      const a = json.accessors[i];
      if (a.sparse) throw new Error('sparse accessor');
      const n = { componentType: a.componentType, count: a.count, type: a.type };
      if (a.bufferView != null) {
        n.bufferView = addView(a.bufferView);
        if (a.byteOffset) n.byteOffset = a.byteOffset;
      }
      if (a.normalized) n.normalized = true;
      if (a.min) n.min = a.min;
      if (a.max) n.max = a.max;
      accs.push(n);
      amap.set(i, accs.length - 1);
      return accs.length - 1;
    };

    const finish = (out) => {
      const b = new Uint8Array(len);
      let o = 0;
      for (const c of chunks) {
        b.set(c, o);
        o += c.length;
      }
      out.accessors = accs;
      out.bufferViews = views;
      out.buffers = [{ byteLength: len }];
      return writeGLB(out, b);
    };

    return { addAcc, finish };
  }

  const liteModel = (src) => {
    const { json } = src;
    const b = builder(src);
    const out = {
      asset: { version: '2.0', generator: 'nhd character motion extractor' },
      scene: json.scene || 0,
      scenes: json.scenes,
      nodes: [],
      meshes: [],
      skins: [],
    };
    const meshMap = new Map();
    json.nodes.forEach((n) => {
      const o = nodeCopy(n);
      if (n.mesh != null && n.skin != null) {
        if (!meshMap.has(n.mesh)) {
          const m = json.meshes[n.mesh];
          out.meshes.push({
            name: m.name,
            primitives: m.primitives.map((p) => {
              const at = {};
              for (const k of ['POSITION', 'JOINTS_0', 'WEIGHTS_0', 'JOINTS_1', 'WEIGHTS_1'])
                if (p.attributes[k] != null) at[k] = b.addAcc(p.attributes[k]);
              return { attributes: at, extras: { material: json.materials?.[p.material]?.name } };
            }),
          });
          meshMap.set(n.mesh, out.meshes.length - 1);
        }
        o.mesh = meshMap.get(n.mesh);
        o.skin = n.skin;
      }
      out.nodes.push(o);
    });
    out.skins = (json.skins || []).map((s) => {
      const o = { joints: s.joints };
      if (s.inverseBindMatrices != null) o.inverseBindMatrices = b.addAcc(s.inverseBindMatrices);
      if (s.skeleton != null) o.skeleton = s.skeleton;
      return o;
    });
    if (json.extensionsUsed?.includes('KHR_mesh_quantization')) out.extensionsUsed = ['KHR_mesh_quantization'];
    return b.finish(out);
  };

  const liteAnim = (src, model) => {
    const { json } = src;
    const b = builder(src);
    // Shape keys are named on the mesh, but the motion needs them on the node that plays them, and the app
    // matches that node's morph to the original model's mesh by mesh name. Mesh and node names differ for many
    // characters, so resolve the mesh through the node's mesh index instead of by name.
    const byNode = new Map();
    for (const n of model.json.nodes || []) {
      if (n.mesh == null) continue;
      const mesh = model.json.meshes[n.mesh];
      const names = mesh?.extras?.targetNames;
      if (names?.length) byNode.set(n.name, { targetNames: names, mesh: mesh.name });
    }
    const out = {
      asset: { version: '2.0', generator: 'nhd character motion extractor' },
      scene: json.scene || 0,
      scenes: json.scenes,
      nodes: json.nodes.map((n) => {
        const o = nodeCopy(n);
        const t = byNode.get(n.name);
        if (t) o.extras = t;
        return o;
      }),
      animations: [],
    };
    for (const a of json.animations || []) {
      const samplers = [];
      const smap = new Map();
      const channels = [];
      for (const c of a.channels) {
        if (c.target.node == null) continue;
        let si = smap.get(c.sampler);
        if (si == null) {
          const s = a.samplers[c.sampler];
          samplers.push({
            input: b.addAcc(s.input),
            output: b.addAcc(s.output),
            interpolation: s.interpolation || 'LINEAR',
          });
          si = samplers.length - 1;
          smap.set(c.sampler, si);
        }
        channels.push({ sampler: si, target: { node: c.target.node, path: c.target.path } });
      }
      out.animations.push({ name: a.name, channels, samplers });
    }
    if (json.extensionsUsed?.includes('KHR_mesh_quantization')) out.extensionsUsed = ['KHR_mesh_quantization'];
    return b.finish(out);
  };

  const gzipBytes = async (u8) => {
    const cs = new CompressionStream('gzip');
    const w = cs.writable.getWriter();
    w.write(u8);
    w.close();
    return new Uint8Array(await new Response(cs.readable).arrayBuffer());
  };

  async function buildPayload(target) {
    const { game, key, id, meta } = target;
    const roles = meta.roles || ['model', 'anim', 'toon'];
    const model = await decode(id, 'model');
    const anim = await decode(id, 'anim');
    const toon = roles.includes('toon') ? await decode(id, 'toon') : null;
    const liteM = liteModel(model);
    const liteA = liteAnim(anim, model);
    const fullM = writeGLB(model.json, model.bin);
    const fullT = toon ? writeGLB(toon.json, toon.bin) : null;
    const metaM = {
      ...meta,
      materials: (model.json.materials || []).map((m) => m.name),
      meshes: (model.json.meshes || []).map((m) => m.name),
    };
    if (!metaM.animations) metaM.animations = [];
    const entries = [
      ['motion/' + game + '/' + key + '/model.glb', liteM],
      ['motion/' + game + '/' + key + '/anim.glb', liteA],
      ['motion/' + game + '/' + key + '/meta.json', enc.encode(JSON.stringify(metaM))],
      ['original/' + game + '/' + key + '/model.glb', fullM],
    ];
    if (fullT) entries.push(['original/' + game + '/' + key + '/toon.glb', fullT]);
    entries.push([
      'original/' + game + '/' + key + '/meta.json',
      enc.encode(JSON.stringify({ source: 'skowt.cc', key, name: meta.name })),
    ]);
    let total = 0;
    const parts = [];
    for (const [name, data] of entries) {
      const nb = enc.encode(name);
      const h = new Uint8Array(8);
      const dv = new DataView(h.buffer);
      dv.setUint32(0, nb.length, true);
      dv.setUint32(4, data.length, true);
      parts.push(h, nb, data);
      total += 8 + nb.length + data.length;
    }
    const raw = new Uint8Array(total);
    let o = 0;
    for (const p of parts) {
      raw.set(p, o);
      o += p.length;
    }
    return gzipBytes(raw);
  }

  window.__cap = { decode, writeGLB, liteModel, liteAnim, buildPayload };
  window.__job = { targets: null, idx: 0, done: [], errors: [], running: false };

  window.__loadTargets = async () => {
    const r = await fetch('http://127.0.0.1:8788/targets');
    const j = await r.json();
    const rr = await fetch('http://127.0.0.1:8788/received');
    const received = new Set((await rr.json()).received);
    const all = j.remaining ?? j.targets;
    window.__job.targets = all.filter((t) => !received.has(t.game + '-' + t.key));
    window.__job.idx = 0;
    window.__job.skipped = all.length - window.__job.targets.length;
    return window.__job.targets.length;
  };

  // One access check. Does not post anything. Stop if this throws.
  window.__testOne = async (i = 0) => {
    const t = window.__job.targets[i];
    const started = Date.now();
    const bytes = await buildPayload(t);
    return { key: t.game + '/' + t.key, bytes: bytes.length, ms: Date.now() - started };
  };

  window.__runBatch = async (count) => {
    const j = window.__job;
    if (!j.targets) throw new Error('no targets');
    j.running = true;
    const results = [];
    for (let n = 0; n < count && j.idx < j.targets.length; n++) {
      const t = j.targets[j.idx];
      const started = Date.now();
      try {
        const payload = await buildPayload(t);
        const name = t.game + '-' + t.key + '.bin.gz';
        const res = await fetch('http://127.0.0.1:8788/upload?name=' + encodeURIComponent(name), {
          method: 'POST',
          body: payload,
        });
        if (!res.ok) throw new Error('upload ' + res.status);
        j.done.push(t.game + '/' + t.key);
        results.push({ key: t.game + '/' + t.key, bytes: payload.length, ms: Date.now() - started });
      } catch (e) {
        j.errors.push({ key: t.game + '/' + t.key, target: t, error: String(e) });
        results.push({ key: t.game + '/' + t.key, error: String(e) });
      }
      j.idx++;
      await new Promise((r) => setTimeout(r, 2000));
    }
    j.running = false;
    return { idx: j.idx, total: j.targets.length, results, errorCount: j.errors.length };
  };

  window.__report = () => ({
    idx: window.__job.idx,
    total: window.__job.targets?.length ?? 0,
    done: window.__job.done.length,
    errors: window.__job.errors,
    running: window.__job.running,
  });

  // Retries only the targets that threw in the last run. Use at the end of the
  // capture, after __report().idx === __report().total. Returns the batch result.
  window.__retryErrors = async () => {
    const j = window.__job;
    const targets = j.errors.map((e) => e.target).filter(Boolean);
    if (!targets.length) return { retried: 0 };
    j.errors = [];
    j.targets = targets;
    j.idx = 0;
    j.done = [];
    return await window.__runBatch(targets.length);
  };

  const n = await window.__loadTargets();
  return {
    targets: n,
    skipped: window.__job.skipped ?? 0,
    first: window.__job.targets[0] ? window.__job.targets[0].game + '/' + window.__job.targets[0].key : null,
  };
})();
