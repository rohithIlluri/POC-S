import test from 'node:test';
import assert from 'node:assert/strict';
import { reduce, newState, makeCode, LEAD_MS } from '../server/rooms.js';

const loaded = (now = 0) => reduce(newState(now), { type: 'load', url: 'builtin:click', name: 'Click', loop: true }, now);

test('load resets playback and bumps the revision', () => {
  const s = loaded(100);
  assert.equal(s.url, 'builtin:click');
  assert.equal(s.playing, false);
  assert.equal(s.pos, 0);
  assert.equal(s.rev, 1);
});

test('play is scheduled LEAD_MS ahead so every device can start together', () => {
  const s = reduce(loaded(), { type: 'play' }, 1000);
  assert.equal(s.playing, true);
  assert.equal(s.at, 1000 + LEAD_MS);
});

test('a second play while playing does not re-anchor', () => {
  const a = reduce(loaded(), { type: 'play' }, 1000);
  const b = reduce(a, { type: 'play' }, 1200);
  assert.equal(b, a);
});

test('pause records where the track had got to', () => {
  const playing = reduce(loaded(), { type: 'play', pos: 10 }, 1000); // starts at 2500
  const paused = reduce(playing, { type: 'pause' }, 4500);
  assert.equal(paused.playing, false);
  assert.equal(paused.pos, 12);
});

test('pause before the scheduled start keeps the old position', () => {
  const playing = reduce(loaded(), { type: 'play', pos: 10 }, 1000);
  assert.equal(reduce(playing, { type: 'pause' }, 1200).pos, 10);
});

test('seek while playing reschedules; while paused it does not', () => {
  const playing = reduce(loaded(), { type: 'play' }, 1000);
  assert.equal(reduce(playing, { type: 'seek', pos: 30 }, 2000).at, 2000 + LEAD_MS);
  const paused = reduce(playing, { type: 'pause' }, 3000);
  const sought = reduce(paused, { type: 'seek', pos: 30 }, 4000);
  assert.deepEqual([sought.pos, sought.playing, sought.at], [30, false, 4000]);
});

test('rejects bad input', () => {
  assert.throws(() => reduce(newState(0), { type: 'play' }, 0), /Load a track first/);
  assert.throws(() => reduce(loaded(), { type: 'seek', pos: -1 }, 0), /Invalid position/);
  assert.throws(() => reduce(loaded(), { type: 'seek', pos: '5' }, 0), /Invalid position/);
  assert.throws(() => reduce(newState(0), { type: 'load', url: 'javascript:alert(1)' }, 0), /Track must be/);
  assert.throws(() => reduce(newState(0), { type: 'load', url: 'file:///etc/passwd' }, 0), /Track must be/);
  assert.throws(() => reduce(newState(0), { type: 'nope' }, 0), /Unknown command/);
});

test('names are trimmed and stripped of control characters', () => {
  const s = reduce(newState(0), { type: 'load', url: 'builtin:click', name: 'a\u0000b' + 'x'.repeat(200) }, 0);
  assert.equal(s.name.length, 80);
  assert.ok(!s.name.includes('\u0000'));
});

test('makeCode gives 4 readable letters and avoids existing rooms', () => {
  const taken = new Set(['AAAA']);
  let calls = 0;
  const rng = () => (calls++ < 4 ? 0 : 0.5); // first attempt is AAAA (taken), then something else
  const code = makeCode((c) => taken.has(c), rng);
  assert.match(code, /^[A-HJ-NP-Z]{4}$/);
  assert.notEqual(code, 'AAAA');
});
