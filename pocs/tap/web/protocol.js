// The tap protocol in JavaScript — the same bytes as protocol/protocol.go.
// Checked against protocol/testdata/vectors.json by protocol.test.mjs.

export const CLAIM_VERSION = 'tap-claim-v1';
const TRANSCRIPT_VERSION = 'tap-transcript-v1';

export function b64url(bytes) {
  let s = '';
  for (const b of new Uint8Array(bytes)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function fromB64url(s) {
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

const enc = new TextEncoder();

export function canonical(c) {
  return [
    CLAIM_VERSION, c.device_id, c.nonce, c.peer_nonce, c.eph_pub, c.peer_eph_pub,
    String(c.distance_cm), String(c.elapsed_ms), c.geohash, c.transport,
  ].join('\n');
}

export function pairKey(c) {
  const [lo, hi] = [c.nonce, c.peer_nonce].sort();
  return `${lo}.${hi}`;
}

export async function transcript(c) {
  let a = [c.nonce, c.eph_pub];
  let b = [c.peer_nonce, c.peer_eph_pub];
  if (b[0] < a[0]) [a, b] = [b, a];
  const digest = await crypto.subtle.digest('SHA-256', enc.encode([TRANSCRIPT_VERSION, a[0], a[1], b[0], b[1]].join('\n')));
  return new Uint8Array(digest);
}

const ECDSA = { name: 'ECDSA', namedCurve: 'P-256' };
const SIGN = { name: 'ECDSA', hash: 'SHA-256' };

// The device key is non-extractable, like a Secure Enclave key.
export async function generateDeviceKey() {
  const pair = await crypto.subtle.generateKey(ECDSA, false, ['sign', 'verify']);
  const raw = await crypto.subtle.exportKey('raw', pair.publicKey);
  return { privateKey: pair.privateKey, publicKey: b64url(raw) };
}

// WebCrypto ECDSA signatures are raw r||s, the protocol's format.
export async function sign(privateKey, text) {
  return b64url(await crypto.subtle.sign(SIGN, privateKey, enc.encode(text)));
}

export async function verify(publicKeyB64, text, sigB64) {
  const key = await crypto.subtle.importKey('raw', fromB64url(publicKeyB64), ECDSA, false, ['verify']);
  return crypto.subtle.verify(SIGN, key, fromB64url(sigB64), enc.encode(text));
}

// Ephemeral X25519 key; falls back to random bytes on browsers without
// WebCrypto X25519 (the server only compares the keys, never uses them).
export async function generateEphemeral() {
  try {
    const pair = await crypto.subtle.generateKey({ name: 'X25519' }, false, ['deriveBits']);
    return b64url(await crypto.subtle.exportKey('raw', pair.publicKey));
  } catch {
    return b64url(crypto.getRandomValues(new Uint8Array(32)));
  }
}

const GEOHASH = '0123456789bcdefghjkmnpqrstuvwxyz';

export function geohash(lat, lon, precision) {
  let [latLo, latHi, lonLo, lonHi] = [-90, 90, -180, 180];
  let out = '', bit = 0, ch = 0, even = true;
  while (out.length < precision) {
    if (even) {
      const mid = (lonLo + lonHi) / 2;
      if (lon >= mid) { ch |= 1 << (4 - bit); lonLo = mid; } else lonHi = mid;
    } else {
      const mid = (latLo + latHi) / 2;
      if (lat >= mid) { ch |= 1 << (4 - bit); latLo = mid; } else latHi = mid;
    }
    even = !even;
    if (bit < 4) bit++;
    else { out += GEOHASH[ch]; bit = 0; ch = 0; }
  }
  return out;
}
