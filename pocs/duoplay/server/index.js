// DuoPlay server: serves the static site, relays room commands over WebSocket, and
// keeps uploaded tracks in memory for the life of their room. No audio is streamed
// through it: every device downloads the same file and plays it on a shared clock.
import http from 'node:http';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomBytes } from 'node:crypto';
import { WebSocketServer } from 'ws';
import { Room, makeCode, MAX_MEMBERS } from './rooms.js';

const WEB_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../web');
const MAX_UPLOAD = 40 * 1024 * 1024; // per file
const MAX_UPLOAD_TOTAL = 200 * 1024 * 1024; // across all rooms, held in memory
const MAX_UPLOADS_PER_ROOM = 3; // older tracks in a room are dropped
const MAX_ROOMS = 100;

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
};

export async function startServer({ port = 3000, host = '0.0.0.0' } = {}) {
  const rooms = new Map(); // code -> Room
  const uploads = new Map(); // id -> { buf, room }

  const json = (res, status, body) => {
    res.writeHead(status, { 'content-type': 'application/json', 'x-content-type-options': 'nosniff' });
    res.end(JSON.stringify(body));
  };

  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://x');

      if (req.method === 'POST' && url.pathname === '/api/upload') {
        const room = url.searchParams.get('room') ?? '';
        if (!rooms.has(room)) return json(res, 404, { error: 'No such room' });
        const chunks = [];
        let size = 0;
        for await (const chunk of req) {
          size += chunk.length;
          if (size > MAX_UPLOAD) {
            json(res, 413, { error: 'File too large (40 MB max)' });
            return req.destroy();
          }
          chunks.push(chunk);
        }
        const inRoom = [...uploads].filter(([, u]) => u.room === room);
        while (inRoom.length >= MAX_UPLOADS_PER_ROOM) uploads.delete(inRoom.shift()[0]);
        const total = [...uploads.values()].reduce((n, u) => n + u.buf.length, 0);
        if (total + size > MAX_UPLOAD_TOTAL) return json(res, 507, { error: 'Server is full, try again later' });
        const id = randomBytes(8).toString('hex');
        uploads.set(id, { buf: Buffer.concat(chunks), room });
        return json(res, 200, { url: `/api/track/${id}` });
      }

      if (req.method === 'GET' && url.pathname.startsWith('/api/track/')) {
        const track = uploads.get(url.pathname.slice('/api/track/'.length));
        if (!track) return json(res, 404, { error: 'No such track' });
        // Never echo a user-supplied content type from our origin.
        res.writeHead(200, {
          'content-type': 'application/octet-stream',
          'content-length': track.buf.length,
          'x-content-type-options': 'nosniff',
        });
        return res.end(track.buf);
      }

      if (req.method !== 'GET' && req.method !== 'HEAD') return json(res, 405, { error: 'Method not allowed' });

      const rel = url.pathname === '/' ? '/index.html' : decodeURIComponent(url.pathname);
      const file = path.resolve(WEB_DIR, '.' + rel);
      if (file !== WEB_DIR && !file.startsWith(WEB_DIR + path.sep)) return json(res, 403, { error: 'Forbidden' });
      const body = await readFile(file).catch(() => null);
      if (!body) return json(res, 404, { error: 'Not found' });
      res.writeHead(200, {
        'content-type': MIME[path.extname(file)] ?? 'application/octet-stream',
        'cache-control': 'no-cache',
        'x-content-type-options': 'nosniff',
      });
      res.end(req.method === 'HEAD' ? undefined : body);
    } catch {
      if (!res.headersSent) json(res, 500, { error: 'Server error' });
    }
  });

  const wss = new WebSocketServer({ server, path: '/ws', maxPayload: 4096 });

  const broadcast = (room, msg) => {
    const data = JSON.stringify(msg);
    for (const c of room.clients) if (c.readyState === 1) c.send(data);
  };

  wss.on('connection', (ws) => {
    ws.isAlive = true;
    ws.on('pong', () => (ws.isAlive = true));
    const send = (msg) => ws.send(JSON.stringify(msg));

    const enter = (room) => {
      ws.room = room;
      room.clients.add(ws);
      send({ t: 'joined', code: room.code, state: room.state, now: Date.now(), members: room.clients.size });
      broadcast(room, { t: 'members', n: room.clients.size });
    };

    ws.on('message', (raw) => {
      let msg;
      try {
        msg = JSON.parse(raw);
      } catch {
        return;
      }
      if (msg.t === 'ping') return send({ t: 'pong', c0: msg.c0, s: Date.now() });

      if (msg.t === 'create' && !ws.room) {
        if (rooms.size >= MAX_ROOMS) return send({ t: 'error', message: 'Too many rooms open, try again later' });
        const code = makeCode((c) => rooms.has(c));
        const room = new Room(code, Date.now());
        rooms.set(code, room);
        return enter(room);
      }
      if (msg.t === 'join' && !ws.room) {
        const room = rooms.get(String(msg.code ?? '').toUpperCase());
        if (!room) return send({ t: 'error', message: 'No room with that code' });
        if (room.clients.size >= MAX_MEMBERS) return send({ t: 'error', message: 'Room is full' });
        return enter(room);
      }
      if (msg.t === 'cmd' && ws.room) {
        try {
          const state = ws.room.apply(msg.cmd, Date.now());
          broadcast(ws.room, { t: 'state', state, now: Date.now() });
        } catch (e) {
          send({ t: 'error', message: e.message });
        }
      }
    });

    ws.on('close', () => {
      const room = ws.room;
      if (!room) return;
      room.clients.delete(ws);
      if (room.clients.size === 0) {
        rooms.delete(room.code);
        for (const [id, u] of uploads) if (u.room === room.code) uploads.delete(id);
      } else {
        broadcast(room, { t: 'members', n: room.clients.size });
      }
    });
  });

  // Drop connections that stopped answering (phones that locked or left the wifi).
  const heartbeat = setInterval(() => {
    for (const ws of wss.clients) {
      if (!ws.isAlive) {
        ws.terminate();
        continue;
      }
      ws.isAlive = false;
      ws.ping();
    }
  }, 30_000);
  heartbeat.unref();

  await new Promise((resolve) => server.listen(port, host, resolve));
  return {
    port: server.address().port,
    rooms,
    uploads,
    close: () =>
      new Promise((resolve) => {
        clearInterval(heartbeat);
        for (const ws of wss.clients) ws.terminate();
        wss.close(() => server.close(() => resolve()));
      }),
  };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { port } = await startServer({ port: Number(process.env.PORT) || 3000 });
  console.log(`DuoPlay running at http://localhost:${port}`);
}
