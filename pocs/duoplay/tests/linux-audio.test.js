import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, readFile, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));

async function fakeMachine() {
  const dir = await mkdtemp(path.join(tmpdir(), 'duoplay-'));
  const state = path.join(dir, 'pactl.json');
  await writeFile(
    state,
    JSON.stringify({
      default: 'alsa_output.pci.analog-stereo',
      sinks: [
        { index: 40, name: 'alsa_output.pci.analog-stereo', description: 'Built-in Audio Analog Stereo' },
        { index: 52, name: 'bluez_output.AA_BB.1', description: 'JBL Flip 5', bus: 'bluetooth' },
        { index: 53, name: 'bluez_output.CC_DD.1', description: 'Sony SRS-XB13', bus: 'bluetooth' },
      ],
      inputs: [
        { index: 85, sink: 40, app: 'Brave', binary: 'brave' },
        { index: 86, sink: 40, app: 'Google Chrome', binary: 'chrome' },
      ],
    }),
  );
  const pactl = path.join(here, 'fake-pactl.mjs');
  await chmod(pactl, 0o755);
  process.env.PACTL = pactl;
  process.env.FAKE_PACTL_STATE = state;
  process.env.DUOPLAY_STATE_DIR = dir;
  const read = async () => JSON.parse(await readFile(state, 'utf8'));
  return { read };
}

// Import after env is set so every call uses the fake pactl.
const audio = await import('../server/linux-audio.js');

test('parseBlocks reads fields and properties, and ignores multi-line values', () => {
  const text = `Sink #52
\tState: RUNNING
\tName: bluez_output.AA_BB.1
\tDescription: JBL Flip 5
\tVolume: front-left: 65536 / 100% / 0.00 dB
\t        balance 0.00
\tProperties:
\t\tdevice.bus = "bluetooth"
Source #3
\tName: something.else
`;
  const [sink, ...rest] = audio.parseBlocks(text, 'Sink');
  assert.equal(rest.length, 0, 'the Source block is not a Sink');
  assert.equal(sink.index, 52);
  assert.equal(sink.fields.Description, 'JBL Flip 5');
  assert.equal(sink.props['device.bus'], 'bluetooth');
  assert.equal(audio.parseBlocks('Sink Input #9\n\tSink: 1\n', 'Sink').length, 0, '"Sink Input" is not "Sink"');
});

test('status lists Bluetooth speakers and whether the DuoPlay input exists', async () => {
  await fakeMachine();
  const st = await audio.status();
  assert.equal(st.available, true);
  assert.equal(st.inputReady, false);
  assert.deepEqual(st.speakers, ['JBL Flip 5', 'Sony SRS-XB13']);
});

test('setup adds the DuoPlay output, makes it default, and moves apps except the DuoPlay browser', async () => {
  const m = await fakeMachine();
  const res = await audio.setup({ keep: ['chrome'] });
  const s = await m.read();
  const ours = s.sinks.find((k) => k.name === audio.SINK);
  assert.ok(ours, 'virtual output created');
  assert.equal(ours.description, 'DuoPlay');
  assert.equal(s.default, audio.SINK);
  assert.equal(s.inputs.find((i) => i.app === 'Brave').sink, ours.index, 'Brave now plays into DuoPlay');
  assert.equal(s.inputs.find((i) => i.binary === 'chrome').sink, 40, 'the DuoPlay browser is left alone');
  assert.equal(res.moved, 1);
  assert.equal(res.previousDefault, 'alsa_output.pci.analog-stereo');
  assert.equal((await audio.status()).inputReady, true);
});

test('running setup twice does not lose the original default output', async () => {
  const m = await fakeMachine();
  await audio.setup({ keep: ['chrome'] });
  await audio.setup({ keep: ['chrome'] });
  const sinks = (await m.read()).sinks.filter((k) => k.name === audio.SINK);
  assert.equal(sinks.length, 1, 'only one DuoPlay output');
  const res = await audio.restore();
  assert.equal(res.restoredTo, 'alsa_output.pci.analog-stereo');
});

test('restore puts the old default back, moves apps back, and removes the DuoPlay output', async () => {
  const m = await fakeMachine();
  await audio.setup({ keep: ['chrome'] });
  await audio.restore();
  const s = await m.read();
  assert.equal(s.default, 'alsa_output.pci.analog-stereo');
  assert.ok(!s.sinks.some((k) => k.name === audio.SINK), 'virtual output removed');
  assert.equal(s.inputs.find((i) => i.app === 'Brave').sink, 40, 'Brave is back on the old output');
});

test('status reports unavailable when pactl is missing', async () => {
  process.env.PACTL = '/nonexistent/pactl';
  const st = await audio.status();
  assert.equal(st.available, false);
});
