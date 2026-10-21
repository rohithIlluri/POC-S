// Cross-language check: the JS protocol must reproduce the Go vectors.
// Run: node web/protocol.test.mjs
import { readFile } from 'node:fs/promises';
import assert from 'node:assert/strict';
import * as P from './protocol.js';

const v = JSON.parse(await readFile(new URL('../protocol/testdata/vectors.json', import.meta.url)));

assert.equal(P.canonical(v.claim), v.canonical, 'canonical claim bytes');
assert.equal(P.pairKey(v.claim), v.pair_key, 'pair key');
assert.equal(P.b64url(await P.transcript(v.claim)), v.transcript, 'transcript');
assert.ok(await P.verify(v.device_public_key, v.canonical, v.signature), 'Go signature verifies in JS');

const key = await P.generateDeviceKey();
const sig = await P.sign(key.privateKey, v.canonical);
assert.equal(P.fromB64url(sig).length, 64, 'raw r||s signature');
assert.ok(await P.verify(key.publicKey, v.canonical, sig), 'JS signature verifies');
assert.ok(!(await P.verify(key.publicKey, v.canonical + 'x', sig)), 'tampered text fails');

assert.equal(P.geohash(42.6, -5.6, 5), 'ezs42', 'geohash reference value');
console.log('protocol.js matches the Go vectors');
