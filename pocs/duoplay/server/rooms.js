// Room state machine. Pure functions: the server owns the clock and passes `now` in.
import { expectedPosition } from '../web/sync.js';

/** How far ahead of "now" a play is scheduled, so slow connections can still start on time. */
export const LEAD_MS = 1500;
export const MAX_MEMBERS = 20;

const ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZ'; // no I or O: easy to read out loud

export function makeCode(exists = () => false, rng = Math.random) {
  for (;;) {
    let code = '';
    for (let i = 0; i < 4; i++) code += ALPHABET[Math.floor(rng() * ALPHABET.length)];
    if (!exists(code)) return code;
  }
}

export function newState(now) {
  return { url: null, name: '', loop: false, playing: false, pos: 0, at: now, rev: 0 };
}

function cleanName(name) {
  return typeof name === 'string' ? name.replace(/[\u0000-\u001f]/g, '').slice(0, 80) : '';
}

function cleanUrl(url) {
  if (typeof url !== 'string' || url.length > 2000) throw new Error('Invalid track link');
  if (url === 'builtin:click' || url.startsWith('/api/track/')) return url;
  if (/^https?:\/\//i.test(url)) return url;
  throw new Error('Track must be an uploaded file, the built-in test, or an http(s) link');
}

function cleanPos(pos) {
  if (typeof pos !== 'number' || !Number.isFinite(pos) || pos < 0 || pos > 86400) {
    throw new Error('Invalid position');
  }
  return pos;
}

/** Apply one command to the room state and return the new state. Throws on bad input. */
export function reduce(state, cmd, now) {
  const rev = state.rev + 1;
  switch (cmd?.type) {
    case 'load':
      return { ...newState(now), url: cleanUrl(cmd.url), name: cleanName(cmd.name), loop: !!cmd.loop, rev };
    case 'play': {
      if (!state.url) throw new Error('Load a track first');
      if (state.playing && cmd.pos === undefined) return state; // already playing: don't re-anchor
      const pos = cmd.pos === undefined ? state.pos : cleanPos(cmd.pos);
      return { ...state, playing: true, pos, at: now + LEAD_MS, rev };
    }
    case 'pause':
      return { ...state, playing: false, pos: expectedPosition(state, now), at: now, rev };
    case 'seek':
      return { ...state, pos: cleanPos(cmd.pos), at: state.playing ? now + LEAD_MS : now, rev };
    default:
      throw new Error('Unknown command');
  }
}

export class Room {
  constructor(code, now) {
    this.code = code;
    this.clients = new Set();
    this.state = newState(now);
  }
  apply(cmd, now) {
    this.state = reduce(this.state, cmd, now);
    return this.state;
  }
}
