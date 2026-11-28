// In-page capture helper for studio.warfarin.wiki character data.
//
// Paste the whole file into browser.evaluate({ tabID, script: <file text> }) on
// https://studio.warfarin.wiki . It installs window.__war, posts the studio catalog to the local receiver
// (scripts/warfarin/server.mjs) and loads the remaining targets from it.
//
// assets.warfarin.wiki answers 403 outside its own page, so every fetch has to run here. The receiver only
// accepts the gzip record stream; nothing is posted back to the site.

(async () => {
  const RECEIVER = 'http://127.0.0.1:8789';

  // Mirrors EFMI_CLIPS in scripts/import-character-motion.mjs: the clips the viewer shows. The order matches the
  // batches captured on 2026-11-28, so re-capturing an unchanged character reproduces the same file.
  const CLIP_NAMES = [
    'idle_loop',
    'run_loop',
    'sprint_loop',
    'walk_loop',
    'battle_loop',
    'battle_normal_skill',
    'battle_skill_ult',
    'gacha_ani',
    'gacha_ani_loop',
    'interact_sit_loop',
    'relax_loop',
    'relax_sp_01',
    'relax_sp_02',
    'ui_overview_loop',
    'ui_relax_loop',
    'uiteam_idle_01',
  ];

  const enc = new TextEncoder();

  const slugify = (name) =>
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '');

  function studioVersion() {
    const seen = performance.getEntriesByType('resource').map((entry) => entry.name);
    for (const url of seen) {
      const match = url.match(/\/studio\/([0-9a-f]{32})\//);
      if (match) return match[1];
    }
    const match = document.documentElement.innerHTML.match(/\/studio\/([0-9a-f]{32})\//);
    if (match) return match[1];
    throw new Error('studio version not found on this page; open https://studio.warfarin.wiki first');
  }

  async function loadCatalog() {
    const base = `https://assets.warfarin.wiki/studio/${studioVersion()}/`;
    for (const file of ['catalog-v8.json', 'catalog-v7.json', 'catalog-v6.json']) {
      const res = await fetch(`${base}data/${file}`, { cache: 'no-store' });
      if (res.ok) return { base, file, catalog: await res.json() };
    }
    throw new Error('studio catalog not found');
  }

  const readView = (bytes) => {
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const length = view.getUint32(12, true);
    return JSON.parse(new TextDecoder().decode(bytes.subarray(20, 20 + length)));
  };

  const writeGLB = (json, bin) => {
    const jsonBytes = new TextEncoder().encode(JSON.stringify(json));
    const jsonPad = (4 - (jsonBytes.length % 4)) % 4;
    const binPad = bin ? (4 - (bin.length % 4)) % 4 : 0;
    const total = 12 + 8 + jsonBytes.length + jsonPad + (bin ? 8 + bin.length + binPad : 0);
    const out = new Uint8Array(total);
    const view = new DataView(out.buffer);
    view.setUint32(0, 0x46546c67, true);
    view.setUint32(4, 2, true);
    view.setUint32(8, total, true);
    view.setUint32(12, jsonBytes.length + jsonPad, true);
    view.setUint32(16, 0x4e4f534a, true);
    out.set(jsonBytes, 20);
    out.fill(0x20, 20 + jsonBytes.length, 20 + jsonBytes.length + jsonPad);
    if (bin) {
      const offset = 20 + jsonBytes.length + jsonPad;
      view.setUint32(offset, bin.length + binPad, true);
      view.setUint32(offset + 4, 0x004e4942, true);
      out.set(bin, offset + 8);
    }
    return out;
  };

  const nodeCopy = (node) => {
    const out = {};
    for (const key of ['name', 'translation', 'rotation', 'scale', 'matrix', 'children']) {
      if (node[key] != null) out[key] = node[key];
    }
    return out;
  };

  // Copies only the accessors and buffer views the clip playback needs, plus the material name and index count
  // per primitive that import-character-motion.mjs reads from extras.
  function builder(src) {
    const { json, bin } = src;
    const views = [];
    const accessors = [];
    const chunks = [];
    let length = 0;
    const viewMap = new Map();
    const accessorMap = new Map();

    const viewBytes = (index) => {
      const view = json.bufferViews[index];
      if ((view.buffer ?? 0) !== 0) throw new Error(`buffer ${view.buffer}`);
      return bin.subarray(view.byteOffset ?? 0, (view.byteOffset ?? 0) + view.byteLength);
    };

    const addView = (index) => {
      if (viewMap.has(index)) return viewMap.get(index);
      const bytes = viewBytes(index);
      const pad = (4 - (length % 4)) % 4;
      if (pad) {
        chunks.push(new Uint8Array(pad));
        length += pad;
      }
      const view = { buffer: 0, byteOffset: length, byteLength: bytes.length };
      const source = json.bufferViews[index];
      if (source.byteStride) view.byteStride = source.byteStride;
      chunks.push(bytes.slice());
      length += bytes.length;
      views.push(view);
      viewMap.set(index, views.length - 1);
      return views.length - 1;
    };

    const addAccessor = (index) => {
      if (index == null) return undefined;
      if (accessorMap.has(index)) return accessorMap.get(index);
      const source = json.accessors[index];
      if (source.sparse) throw new Error('sparse accessor');
      const out = { componentType: source.componentType, count: source.count, type: source.type };
      if (source.bufferView != null) {
        out.bufferView = addView(source.bufferView);
        if (source.byteOffset) out.byteOffset = source.byteOffset;
      }
      if (source.normalized) out.normalized = true;
      if (source.min) out.min = source.min;
      if (source.max) out.max = source.max;
      accessors.push(out);
      accessorMap.set(index, accessors.length - 1);
      return accessors.length - 1;
    };

    const finish = (out) => {
      const bin = new Uint8Array(length);
      let offset = 0;
      for (const chunk of chunks) {
        bin.set(chunk, offset);
        offset += chunk.length;
      }
      out.accessors = accessors;
      out.bufferViews = views;
      out.buffers = [{ byteLength: length }];
      return writeGLB(out, bin);
    };

    return { addAccessor, finish };
  }

  function liteModel(src) {
    const { json } = src;
    const build = builder(src);
    const out = {
      asset: { version: '2.0', generator: 'nhd character motion extractor' },
      scene: json.scene || 0,
      scenes: json.scenes,
      nodes: [],
      meshes: [],
      skins: [],
    };
    const meshMap = new Map();
    json.nodes.forEach((node) => {
      const copy = nodeCopy(node);
      if (node.mesh != null && node.skin != null) {
        if (!meshMap.has(node.mesh)) {
          const mesh = json.meshes[node.mesh];
          out.meshes.push({
            name: mesh.name,
            primitives: mesh.primitives.map((primitive) => {
              const attributes = {};
              for (const name of ['POSITION', 'JOINTS_0', 'WEIGHTS_0', 'JOINTS_1', 'WEIGHTS_1']) {
                if (primitive.attributes[name] != null) attributes[name] = build.addAccessor(primitive.attributes[name]);
              }
              const extras = { material: json.materials?.[primitive.material]?.name };
              if (primitive.indices != null) extras.indices = json.accessors[primitive.indices].count;
              return { attributes, extras };
            }),
          });
          meshMap.set(node.mesh, out.meshes.length - 1);
        }
        copy.mesh = meshMap.get(node.mesh);
        copy.skin = node.skin;
      }
      out.nodes.push(copy);
    });
    out.skins = (json.skins ?? []).map((skin) => {
      const copy = { joints: skin.joints };
      if (skin.inverseBindMatrices != null) copy.inverseBindMatrices = build.addAccessor(skin.inverseBindMatrices);
      if (skin.skeleton != null) copy.skeleton = skin.skeleton;
      return copy;
    });
    return build.finish(out);
  }

  async function fetchBytes(url) {
    const res = await fetch(url, { cache: 'no-store' });
    if (!res.ok) throw new Error(`${res.status} ${url.slice(0, 120)}`);
    return new Uint8Array(await res.arrayBuffer());
  }

  // The glTF names an external buffer "../textures/<hash>" while the studio stores it as "<hash>.bin".
  async function fetchBuffer(uri, modelUrl) {
    const url = new URL(uri, modelUrl);
    const probe = await fetch(url.href, { cache: 'no-store' });
    if (probe.ok) return new Uint8Array(await probe.arrayBuffer());
    const fallback = new URL(`${url.href}.bin`);
    const res = await fetch(fallback.href, { cache: 'no-store' });
    if (!res.ok) throw new Error(`${res.status} ${fallback.pathname}`);
    return new Uint8Array(await res.arrayBuffer());
  }

  async function fetchModel(url) {
    const bytes = await fetchBytes(url);
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const jsonLength = view.getUint32(12, true);
    const json = JSON.parse(new TextDecoder().decode(bytes.subarray(20, 20 + jsonLength)));
    const binHeader = 20 + jsonLength;
    const binLength = view.getUint32(binHeader, true);
    const bin = bytes.subarray(binHeader + 8, binHeader + 8 + binLength);
    return { bytes, json, bin };
  }

  async function clipsFor(base, entry) {
    const clips = await (await fetch(`${base}data/${entry.clips}`, { cache: 'no-store' })).json();
    const byName = new Map();
    for (const clip of clips) {
      const key = clip.name.replace(/^A_actor_[a-z0-9]+_/, '');
      if (!byName.has(key)) byName.set(key, clip);
    }
    const picked = [];
    for (const name of CLIP_NAMES) {
      const clip = byName.get(name);
      if (clip) picked.push({ name, clip });
    }
    return picked;
  }

  async function buildPayload(target) {
    const modelUrl = `${target.base}models/${target.model}`;
    const model = await fetchModel(modelUrl);
    const slug = target.slug;

    const motion = [['motion/efmi/' + slug + '/model.glb', liteModel(model)]];
    const clips = await clipsFor(target.base, target.entry);
    if (!clips.length) throw new Error('no known clips');
    for (const { name, clip } of clips) {
      const bytes = await fetchBytes(`${target.base}clips/${clip.file}`);
      motion.push([`motion/efmi/${slug}/clips/${name}.glb`, bytes]);
    }
    const meta = {
      source: 'studio.warfarin.wiki',
      key: target.key,
      name: target.entry.name,
      clips: clips.map(({ name, clip }) => ({
        name,
        source: clip.name,
        duration: clip.duration,
        samples: clip.samples,
        loop: clip.loop,
      })),
      materials: (model.json.materials ?? []).map((material) => material.name),
    };
    motion.push([`motion/efmi/${slug}/meta.json`, enc.encode(JSON.stringify(meta))]);

    const original = [['original/efmi/' + slug + '/model.glb', model.bytes]];
    for (const buffer of (model.json.buffers ?? []).slice(1)) {
      if (!buffer.uri) continue;
      const bytes = await fetchBuffer(buffer.uri, modelUrl);
      original.push([`original/efmi/${slug}/textures/${buffer.uri.split('/').pop()}`, bytes]);
    }
    original.push([
      `original/efmi/${slug}/meta.json`,
      enc.encode(JSON.stringify({ source: 'studio.warfarin.wiki', key: target.key, name: target.entry.name })),
    ]);

    return gzipRecords([...motion, ...original]);
  }

  async function gzipRecords(entries) {
    const parts = [];
    for (const [name, data] of entries) {
      const nameBytes = enc.encode(name);
      const header = new Uint8Array(8);
      const view = new DataView(header.buffer);
      view.setUint32(0, nameBytes.length, true);
      view.setUint32(4, data.length, true);
      parts.push(header, nameBytes, data);
    }
    const total = parts.reduce((sum, part) => sum + part.length, 0);
    const raw = new Uint8Array(total);
    let offset = 0;
    for (const part of parts) {
      raw.set(part, offset);
      offset += part.length;
    }
    const stream = new CompressionStream('gzip');
    const writer = stream.writable.getWriter();
    writer.write(raw);
    writer.close();
    return new Uint8Array(await new Response(stream.readable).arrayBuffer());
  }

  const { base, file, catalog } = await loadCatalog();
  const characters = {};
  for (const [key, entry] of Object.entries(catalog.models)) {
    if (!key.startsWith('chr_')) continue;
    characters[key] = { name: entry.name, model: entry.model.glb, clips: entry.clips };
  }
  await fetch(`${RECEIVER}/catalog`, {
    method: 'POST',
    body: JSON.stringify({ version: base, catalog: file, characters }),
  });

  window.__war = {
    base,
    catalogFile: file,
    characters,
    targets: null,
    idx: 0,
    done: [],
    errors: [],
    running: false,
  };

  window.__war.loadTargets = async () => {
    const wanted = await (await fetch(`${RECEIVER}/targets`)).json();
    const received = new Set((await (await fetch(`${RECEIVER}/received`)).json()).received);
    const all = (wanted.targets ?? []).map((target) => ({
      ...target,
      base,
      entry: catalog.models[target.key],
    }));
    window.__war.targets = all.filter((target) => !received.has(`efmi-${target.slug}`));
    window.__war.idx = 0;
    window.__war.skipped = all.length - window.__war.targets.length;
    return window.__war.targets.length;
  };

  // Builds one payload without posting. Run once to prove the site serves the character.
  window.__war.testOne = async (i = 0) => {
    const target = window.__war.targets[i];
    if (!target) throw new Error('no target');
    const started = Date.now();
    const bytes = await buildPayload(target);
    return { key: target.key, slug: target.slug, bytes: bytes.length, ms: Date.now() - started };
  };

  window.__war.runBatch = async (count) => {
    const job = window.__war;
    if (!job.targets) throw new Error('no targets');
    if (job.running) throw new Error('already running');
    job.running = true;
    const results = [];
    try {
      for (let n = 0; n < count && job.idx < job.targets.length; n++) {
        const target = job.targets[job.idx];
        const started = Date.now();
        try {
          const payload = await buildPayload(target);
          const name = `efmi-${target.slug}.bin.gz`;
          const res = await fetch(`${RECEIVER}/upload?name=${encodeURIComponent(name)}`, {
            method: 'POST',
            body: payload,
          });
          if (!res.ok) throw new Error(`upload ${res.status}`);
          job.done.push(target.slug);
          results.push({ slug: target.slug, bytes: payload.length, ms: Date.now() - started });
        } catch (error) {
          job.errors.push({ key: target.key, slug: target.slug, target, error: String(error) });
          results.push({ slug: target.slug, error: String(error) });
        }
        job.idx++;
        await new Promise((resolve) => setTimeout(resolve, 2000));
      }
    } finally {
      job.running = false;
    }
    return { idx: job.idx, total: job.targets.length, results, errorCount: job.errors.length };
  };

  window.__war.report = () => ({
    idx: window.__war.idx,
    total: window.__war.targets?.length ?? 0,
    done: window.__war.done.length,
    errors: window.__war.errors,
    running: window.__war.running,
  });

  window.__war.retryErrors = async () => {
    const job = window.__war;
    const targets = job.errors.map((entry) => entry.target).filter(Boolean);
    if (!targets.length) return { retried: 0 };
    job.errors = [];
    job.targets = targets;
    job.idx = 0;
    job.done = [];
    return await window.__war.runBatch(targets.length);
  };

  const n = await window.__war.loadTargets();
  return {
    version: base,
    characters: Object.keys(characters).length,
    targets: n,
    skipped: window.__war.skipped ?? 0,
    first: window.__war.targets[0]?.slug ?? null,
  };
})();
