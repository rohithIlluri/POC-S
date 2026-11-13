import test from 'node:test';
import assert from 'node:assert/strict';
import { WebSocket } from 'ws';
import { startServer } from '../server/index.js';

async function client(port) {
  const ws = new WebSocket(`ws://127.0.0.1:${port}/ws`);
  const inbox = [];
  const waiting = [];
  ws.on('message', (raw) => {
    const m = JSON.parse(raw);
    const i = waiting.findIndex((w) => w.t === m.t);
    if (i >= 0) waiting.splice(i, 1)[0].resolve(m);
    else inbox.push(m);
  });
  await new Promise((r) => ws.on('open', r));
  return {
    ws,
    send: (m) => ws.send(JSON.stringify(m)),
    next: (t) =>
      new Promise((resolve, reject) => {
        const i = inbox.findIndex((m) => m.t === t);
        if (i >= 0) return resolve(inbox.splice(i, 1)[0]);
        const w = { t, resolve };
        waiting.push(w);
        setTimeout(() => reject(new Error(`timed out waiting for ${t}`)), 2000).unref();
      }),
  };
}

test('two devices share a room: create, join, play, late join, leave', async (t) => {
  const srv = await startServer({ port: 0, host: '127.0.0.1' });
  t.after(() => srv.close());

  const host = await client(srv.port);
  host.send({ t: 'create' });
  const created = await host.next('joined');
  assert.match(created.code, /^[A-Z]{4}$/);

  const guest = await client(srv.port);
  guest.send({ t: 'join', code: created.code.toLowerCase() }); // codes are case-insensitive
  assert.equal((await guest.next('joined')).members, 2);
  assert.equal((await host.next('members')).n, 1); // the host's own join
  assert.equal((await host.next('members')).n, 2); // then the guest's

  host.send({ t: 'cmd', cmd: { type: 'load', url: 'builtin:click', name: 'Click', loop: true } });
  await host.next('state');
  await guest.next('state');
  guest.send({ t: 'cmd', cmd: { type: 'play' } }); // any member can control
  const [a, b] = await Promise.all([host.next('state'), guest.next('state')]);
  assert.equal(a.state.playing, true);
  assert.deepEqual(a.state, b.state); // both devices get the identical start time
  assert.ok(a.state.at > a.now, 'start is scheduled in the future');

  const late = await client(srv.port);
  late.send({ t: 'join', code: created.code });
  const joined = await late.next('joined');
  assert.equal(joined.state.playing, true);
  assert.equal(joined.state.at, a.state.at);

  late.send({ t: 'ping', c0: 123 });
  const pong = await late.next('pong');
  assert.equal(pong.c0, 123);
  assert.ok(Math.abs(pong.s - Date.now()) < 1000);

  guest.send({ t: 'cmd', cmd: { type: 'seek', pos: -5 } });
  assert.match((await guest.next('error')).message, /Invalid position/);

  for (const c of [host, guest, late]) c.ws.close();
});

test('unknown room and full lifecycle cleanup', async (t) => {
  const srv = await startServer({ port: 0, host: '127.0.0.1' });
  t.after(() => srv.close());
  const c = await client(srv.port);
  c.send({ t: 'join', code: 'ZZZZ' });
  assert.match((await c.next('error')).message, /No room/);

  c.send({ t: 'create' });
  const { code } = await c.next('joined');
  const up = await fetch(`http://127.0.0.1:${srv.port}/api/upload?room=${code}`, { method: 'POST', body: 'abc' });
  const { url } = await up.json();
  const got = await fetch(`http://127.0.0.1:${srv.port}${url}`);
  assert.equal(await got.text(), 'abc');
  assert.equal(got.headers.get('content-type'), 'application/octet-stream');

  c.ws.close();
  await new Promise((r) => setTimeout(r, 100));
  assert.equal(srv.rooms.size, 0);
  assert.equal(srv.uploads.size, 0);
});

test('upload needs a real room, and the static server blocks path traversal', async (t) => {
  const srv = await startServer({ port: 0, host: '127.0.0.1' });
  t.after(() => srv.close());
  const base = `http://127.0.0.1:${srv.port}`;
  assert.equal((await fetch(`${base}/api/upload?room=NOPE`, { method: 'POST', body: 'x' })).status, 404);
  assert.equal((await fetch(`${base}/`)).status, 200);
  assert.equal((await fetch(`${base}/app.js`)).status, 200);
  assert.equal((await fetch(`${base}/..%2fserver%2findex.js`)).status, 403);
  assert.equal((await fetch(`${base}/%2e%2e/package.json`)).status === 200, false);
});
