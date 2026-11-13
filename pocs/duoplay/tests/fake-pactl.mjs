#!/usr/bin/env node
// A stand-in for `pactl` with just enough behaviour for the linux-audio tests.
// State (sinks, playing apps, default output, modules) lives in $FAKE_PACTL_STATE.
import { readFileSync, writeFileSync, appendFileSync } from 'node:fs';

const file = process.env.FAKE_PACTL_STATE;
const s = JSON.parse(readFileSync(file, 'utf8'));
const save = () => writeFileSync(file, JSON.stringify(s));
const args = process.argv.slice(2);
appendFileSync(file + '.log', args.join(' ') + '\n');

const sinkText = () =>
  s.sinks
    .map(
      (k) => `Sink #${k.index}
\tState: RUNNING
\tName: ${k.name}
\tDescription: ${k.description}
\tVolume: front-left: 65536 / 100% / 0.00 dB,   front-right: 65536 / 100% / 0.00 dB
\t        balance 0.00
\tProperties:
\t\tdevice.description = "${k.description}"
${k.bus ? `\t\tdevice.bus = "${k.bus}"\n` : ''}`,
    )
    .join('\n');

const inputText = () =>
  s.inputs
    .map(
      (i) => `Sink Input #${i.index}
\tDriver: protocol-native.c
\tSink: ${i.sink}
\tProperties:
\t\tapplication.name = "${i.app}"
\t\tapplication.process.binary = "${i.binary}"
`,
    )
    .join('\n');

const [a, b, c, d] = args;
if (a === 'list' && b === 'sinks') process.stdout.write(sinkText());
else if (a === 'list' && b === 'sink-inputs') process.stdout.write(inputText());
else if (a === 'get-default-sink') process.stdout.write(s.default + '\n');
else if (a === 'set-default-sink') {
  if (!s.sinks.some((k) => k.name === b)) process.exit(1);
  s.default = b;
  save();
} else if (a === 'load-module' && b === 'module-null-sink') {
  const name = c.split('=')[1];
  const description = d.split('device.description=')[1];
  const index = Math.max(...s.sinks.map((k) => k.index)) + 1;
  const module = 500 + index;
  s.sinks.push({ index, name, description, module });
  save();
  process.stdout.write(module + '\n');
} else if (a === 'unload-module') {
  s.sinks = s.sinks.filter((k) => String(k.module) !== b);
  save();
} else if (a === 'move-sink-input') {
  const target = s.sinks.find((k) => k.name === c);
  s.inputs.find((i) => String(i.index) === b).sink = target.index;
  save();
} else process.exit(1);
