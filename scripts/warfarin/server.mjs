// Local receiver for the in-page Warfarin (EFMI) capture helper.
//
//   bun scripts/warfarin/server.mjs
//
// GET  /ping       health check
// GET  /targets    state/remaining.json
// GET  /received   capture stems already written to work/incoming
// GET  /script     capture-page.js
// POST /catalog    studio catalog snapshot -> state/live.json
// POST /upload?name=<stem>.bin.gz   writes work/incoming/<stem>.bin.gz
//
// Binds 127.0.0.1 only. The page posts here because assets.warfarin.wiki answers 403 outside its own page.

import { mkdir, readdir } from 'node:fs/promises';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir);
const STATE = path.join(ROOT, 'state');
const INCOMING = path.join(ROOT, 'work', 'incoming');

await mkdir(INCOMING, { recursive: true });

const headers = {
  'access-control-allow-origin': 'https://studio.warfarin.wiki',
  'access-control-allow-methods': 'GET, POST, OPTIONS',
  'access-control-allow-headers': '*',
  'access-control-allow-private-network': 'true',
  'access-control-allow-credentials': 'true',
};

const server = Bun.serve({
  port: 8789,
  hostname: '127.0.0.1',
  async fetch(req) {
    const url = new URL(req.url);
    if (req.method === 'OPTIONS') return new Response(null, { status: 204, headers });
    if (url.pathname === '/ping') return new Response('pong', { headers });
    if (url.pathname === '/script') {
      return new Response(await Bun.file(path.join(ROOT, 'capture-page.js')).arrayBuffer(), {
        headers: { ...headers, 'content-type': 'text/javascript; charset=utf-8' },
      });
    }
    if (url.pathname === '/targets') {
      try {
        return new Response(await Bun.file(path.join(STATE, 'remaining.json')).arrayBuffer(), { headers });
      } catch {
        // diff.mjs has not run yet.
        return new Response('{"targets":[]}', {
          headers: { ...headers, 'content-type': 'application/json; charset=utf-8' },
        });
      }
    }
    if (url.pathname === '/received') {
      let files = [];
      try {
        files = await readdir(INCOMING);
      } catch {}
      const received = files.filter((f) => f.endsWith('.bin.gz')).map((f) => f.replace(/\.bin\.gz$/, ''));
      return new Response(JSON.stringify({ received }), {
        headers: { ...headers, 'content-type': 'application/json; charset=utf-8' },
      });
    }
    if (url.pathname === '/catalog' && req.method === 'POST') {
      const body = new Uint8Array(await req.arrayBuffer());
      await Bun.write(path.join(STATE, 'live.json'), body);
      return new Response(JSON.stringify({ ok: true, bytes: body.length }), {
        headers: { ...headers, 'content-type': 'application/json; charset=utf-8' },
      });
    }
    if (url.pathname === '/upload' && req.method === 'POST') {
      const name = url.searchParams.get('name') ?? '';
      if (!/^efmi-[a-z0-9-]+\.bin\.gz$/.test(name)) {
        return new Response('bad name', { status: 400, headers });
      }
      const body = new Uint8Array(await req.arrayBuffer());
      await Bun.write(path.join(INCOMING, name), body);
      return new Response(JSON.stringify({ ok: true, name, bytes: body.length }), { headers });
    }
    return new Response('not found', { status: 404, headers });
  },
});

console.log(`warfarin receiver: ${server.url}`);
