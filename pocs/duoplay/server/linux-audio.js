// Linux sound-system helper (PipeWire or PulseAudio, through `pactl`).
//
// "Computer sound" mode: we add a virtual output called DuoPlay and make it the
// default, so every app (Brave, Spotify, ...) plays into it silently. The DuoPlay
// page records that output's monitor and plays it on the two real speakers.
// `restore` puts the previous default output back and removes the virtual one.
//
// CLI: node server/linux-audio.js status | setup [--keep <browser-binary>] | restore
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { readFile, writeFile, mkdir, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const run = promisify(execFile);

export const SINK = 'duoplay_input';
export const DESCRIPTION = 'DuoPlay';

const stateDir = () => process.env.DUOPLAY_STATE_DIR || path.join(os.homedir(), '.cache', 'duoplay');
const statePath = () => path.join(stateDir(), 'linux-audio.json');

async function pactl(...args) {
  // LC_ALL=C: pactl translates its output, and we parse it.
  const { stdout } = await run(process.env.PACTL || 'pactl', args, {
    env: { ...process.env, LC_ALL: 'C' },
    timeout: 5000,
  });
  return stdout;
}

/** Parse `pactl list sinks` / `pactl list sink-inputs` text into blocks of fields and properties. */
export function parseBlocks(text, header) {
  const start = new RegExp(`^${header} #(\\d+)\\s*$`);
  const blocks = [];
  let cur = null;
  for (const line of text.split('\n')) {
    const m = line.match(start);
    if (m) {
      cur = { index: Number(m[1]), fields: {}, props: {} };
      blocks.push(cur);
      continue;
    }
    if (!cur) continue;
    if (/^\S/.test(line)) {
      cur = null; // a block of some other kind
      continue;
    }
    const prop = line.match(/^\t\t([\w.-]+) = "(.*)"$/);
    if (prop) {
      cur.props[prop[1]] = prop[2];
      continue;
    }
    const field = line.match(/^\t([^\t:][^:]*):\s?(.*)$/);
    if (field) cur.fields[field[1].trim()] = field[2].trim();
  }
  return blocks;
}

export async function listSinks() {
  return parseBlocks(await pactl('list', 'sinks'), 'Sink').map((b) => {
    const name = b.fields.Name ?? '';
    return {
      index: b.index,
      name,
      description: b.fields.Description ?? name,
      bluetooth: name.startsWith('bluez_output') || b.props['device.bus'] === 'bluetooth',
    };
  });
}

export async function listInputs() {
  return parseBlocks(await pactl('list', 'sink-inputs'), 'Sink Input').map((b) => ({
    index: b.index,
    sink: Number(b.fields.Sink),
    binary: b.props['application.process.binary'] ?? '',
    app: b.props['application.name'] ?? '',
  }));
}

export async function getDefaultSink() {
  try {
    return (await pactl('get-default-sink')).trim();
  } catch {
    const m = (await pactl('info')).match(/^Default Sink:\s*(.+)$/m);
    return m ? m[1].trim() : '';
  }
}

async function readState() {
  try {
    return JSON.parse(await readFile(statePath(), 'utf8'));
  } catch {
    return null;
  }
}

/** What the page needs: is the DuoPlay input there, and which speakers are Bluetooth. */
export async function status() {
  try {
    const sinks = await listSinks();
    return {
      available: true,
      inputReady: sinks.some((s) => s.name === SINK),
      speakers: sinks.filter((s) => s.bluetooth).map((s) => s.description),
      defaultSink: await getDefaultSink(),
    };
  } catch {
    return { available: false, inputReady: false, speakers: [], defaultSink: '' };
  }
}

const keeps = (binary, keep) => keep.some((k) => k && binary.startsWith(k));

/**
 * Create the DuoPlay output, make it the default, and move apps that are already
 * playing onto it. Streams from `keep` (the browser running DuoPlay) are left alone,
 * so DuoPlay's own speaker output is never captured back into itself.
 */
export async function setup({ keep = [] } = {}) {
  const saved = await readState();
  let sinks = await listSinks();
  let moduleId = saved?.moduleId ?? null;
  if (!sinks.some((s) => s.name === SINK)) {
    const out = await pactl('load-module', 'module-null-sink', `sink_name=${SINK}`, `sink_properties=device.description=${DESCRIPTION}`);
    moduleId = Number(out.trim());
    sinks = await listSinks();
  }
  const current = await getDefaultSink();
  const previousDefault = current === SINK ? (saved?.previousDefault ?? null) : current;
  await pactl('set-default-sink', SINK);

  const ours = sinks.find((s) => s.name === SINK);
  let moved = 0;
  for (const input of await listInputs()) {
    if (input.sink === ours?.index || keeps(input.binary, keep)) continue;
    await pactl('move-sink-input', String(input.index), SINK);
    moved++;
  }
  await mkdir(stateDir(), { recursive: true });
  await writeFile(statePath(), JSON.stringify({ moduleId, previousDefault }));
  return { previousDefault, moved, speakers: sinks.filter((s) => s.bluetooth).map((s) => s.description) };
}

/** Undo setup: previous default output back, apps moved back, DuoPlay output removed. */
export async function restore() {
  const saved = await readState();
  const sinks = await listSinks();
  const ours = sinks.find((s) => s.name === SINK);
  const target =
    saved?.previousDefault && sinks.some((s) => s.name === saved.previousDefault)
      ? saved.previousDefault
      : sinks.find((s) => s.name !== SINK)?.name;
  if (target) {
    await pactl('set-default-sink', target);
    if (ours) {
      for (const input of await listInputs()) {
        if (input.sink === ours.index) await pactl('move-sink-input', String(input.index), target);
      }
    }
  }
  if (saved?.moduleId != null && ours) {
    await pactl('unload-module', String(saved.moduleId)).catch(() => {});
  }
  await rm(statePath(), { force: true });
  return { restoredTo: target ?? null };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [cmd, ...rest] = process.argv.slice(2);
  const keepAt = rest.indexOf('--keep');
  const keep = keepAt >= 0 ? [rest[keepAt + 1]] : [];
  const actions = { status, setup: () => setup({ keep }), restore };
  if (!actions[cmd]) {
    console.error('Usage: node server/linux-audio.js status | setup [--keep <browser>] | restore');
    process.exit(2);
  }
  try {
    console.log(JSON.stringify(await actions[cmd]()));
  } catch (e) {
    console.error(`linux-audio ${cmd} failed: ${e.message}`);
    process.exit(1);
  }
}
