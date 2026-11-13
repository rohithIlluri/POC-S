import { Channel, Player, LiveRouter, clickBuffer, hasContextSink, canCaptureTab } from './engine.js';
import { RoomClient } from './room.js';

const $ = (sel) => document.querySelector(sel);
const fmt = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---------------------------------------------------------------- capability check
const canSink = hasContextSink();
const hasAudio = typeof AudioContext !== 'undefined';
const checks = [
  ['Web Audio', hasAudio, 'Needed for everything'],
  ['Choose output speaker (AudioContext.setSinkId)', canSink, 'Needed for Dual Output'],
  ['Browser speaker prompt (selectAudioOutput)', !!navigator.mediaDevices?.selectAudioOutput, 'Lets you pick a speaker the list does not show'],
  ['Share a tab\'s audio (getDisplayMedia)', canCaptureTab(), 'Needed to play YouTube or any website on both speakers'],
  ['List outputs (enumerateDevices)', !!navigator.mediaDevices?.enumerateDevices, 'Needed to fill the speaker lists'],
  ['Keep screen awake (Wake Lock)', 'wakeLock' in navigator, 'Helps phones stay in Room Sync'],
  ['iOS audio session (audioSession)', 'audioSession' in navigator, 'Lets audio play with the silent switch on'],
  ['Secure page (https or localhost)', window.isSecureContext, 'Some features above need it'],
];

function renderSupport() {
  $('#support-list').innerHTML = checks
    .map(([name, ok, note]) => {
      const cls = ok ? 'ok' : name.startsWith('Choose output') || name === 'Web Audio' ? 'no' : 'maybe';
      return `<li><span class="mark ${cls}">${ok ? '✓' : '✗'}</span><span>${name} <span class="note">· ${note}</span></span></li>`;
    })
    .join('');
  $('#verdict').textContent = !hasAudio
    ? 'This browser cannot play Web Audio, so nothing here will work.'
    : canSink
      ? 'Dual Output: available here. Room Sync: available.'
      : 'Dual Output: not available in this browser. Room Sync: available. Use it with one device per speaker.';
}
renderSupport();

$('#copy-report').addEventListener('click', async () => {
  const lines = [
    `DuoPlay test report`,
    `User agent: ${navigator.userAgent}`,
    ...checks.map(([name, ok]) => `${ok ? 'yes' : 'NO '}  ${name}`),
  ];
  try {
    await navigator.clipboard.writeText(lines.join('\n'));
    $('#copy-report').textContent = 'Copied';
  } catch {
    $('#copy-report').textContent = 'Copy failed: select the list above instead';
  }
});

if (!hasAudio) {
  $('.tabs').hidden = true;
  $('#panel-dual').hidden = true;
  throw new Error('Web Audio unavailable');
}

// iOS: let Web Audio play even with the ringer switch on silent.
try {
  if (navigator.audioSession) navigator.audioSession.type = 'playback';
} catch {
  /* optional */
}

// ---------------------------------------------------------------- speaker cards
function speakerCard(id, title) {
  return `<div class="card" id="spk-${id}">
    <h3>${title}</h3>
    <div ${canSink ? '' : 'hidden'}>
      <label>Output<select id="dev-${id}"><option value="">System default</option></select></label>
      <div class="row">
        <button id="pick-${id}" hidden>Choose speaker…</button>
        <button id="names-${id}" hidden>Find my speakers</button>
      </div>
      <p class="hint" id="hint-${id}" hidden>Only "System default" is listed. Click <b>Find my speakers</b> and allow the microphone prompt: browsers only list speakers after that permission. The microphone is switched off straight away and nothing is recorded.</p>
    </div>
    <label>Channel
      <select id="ch-${id}">
        <option value="both">Stereo (both sides)</option>
        <option value="left">Left channel only</option>
        <option value="right">Right channel only</option>
      </select>
    </label>
    <label>Volume <output id="vol-out-${id}">100%</output><input id="vol-${id}" type="range" min="0" max="100" value="100" /></label>
    <label>Nudge (+ plays later) <output id="nudge-out-${id}">0 ms</output>
      <div class="row">
        <button id="nm-${id}" aria-label="Nudge 5 ms earlier">−5</button>
        <input id="nudge-${id}" type="range" min="-300" max="300" value="0" />
        <button id="np-${id}" aria-label="Nudge 5 ms later">+5</button>
      </div>
    </label>
    <label>Signal <meter id="level-${id}" min="0" max="1" value="0"></meter></label>
    <div class="row"><button id="tone-${id}">Test tone</button></div>
    <p class="hint" id="drift-${id}"></p>
  </div>`;
}

let onNudge = () => {}; // set per mode: restart playback so a nudge takes effect

async function listOutputs(selectEl, wanted) {
  if (!navigator.mediaDevices?.enumerateDevices) return;
  const outs = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'audiooutput');
  const known = new Set([...selectEl.options].map((o) => o.value));
  for (const d of outs) {
    if (!d.deviceId || d.deviceId === 'default' || known.has(d.deviceId)) continue;
    selectEl.add(new Option(d.label || `Output ${selectEl.options.length}`, d.deviceId));
  }
  if (wanted) selectEl.value = wanted;
}

const devSelects = []; // [selectEl, id] for every speaker card

async function refreshOutputs() {
  for (const [sel, id] of devSelects) {
    const keep = sel.value;
    for (const o of [...sel.options].slice(1)) o.remove();
    await listOutputs(sel);
    sel.value = [...sel.options].some((o) => o.value === keep) ? keep : '';
    if (sel.value !== keep) sel.dispatchEvent(new Event('change')); // chosen speaker went away
    $(`#hint-${id}`).hidden = sel.options.length > 1;
  }
}
// Speakers connected or disconnected while the page is open.
navigator.mediaDevices?.addEventListener?.('devicechange', refreshOutputs);

function bindSpeaker(id, channel) {
  const dev = $(`#dev-${id}`);
  const status = $(`#drift-${id}`);

  if (canSink) {
    devSelects.push([dev, id]);
    dev.addEventListener('change', async () => {
      try {
        await channel.setSink(dev.value);
        status.textContent = '';
      } catch (e) {
        status.textContent = e.message;
      }
    });
    if (navigator.mediaDevices?.selectAudioOutput) {
      const pick = $(`#pick-${id}`);
      pick.hidden = false;
      pick.addEventListener('click', async () => {
        try {
          const d = await navigator.mediaDevices.selectAudioOutput();
          if (![...dev.options].some((o) => o.value === d.deviceId)) dev.add(new Option(d.label || 'Chosen speaker', d.deviceId));
          dev.value = d.deviceId;
          dev.dispatchEvent(new Event('change'));
        } catch {
          /* dismissed */
        }
      });
    }
    if (navigator.mediaDevices?.getUserMedia) {
      const names = $(`#names-${id}`);
      names.hidden = false;
      names.addEventListener('click', async () => {
        try {
          // Browsers only reveal speaker names after a microphone permission; we stop the mic right away.
          const s = await navigator.mediaDevices.getUserMedia({ audio: true });
          s.getTracks().forEach((t) => t.stop());
          await refreshOutputs();
          status.textContent = '';
        } catch (e) {
          status.textContent =
            e.name === 'NotFoundError'
              ? 'This browser needs a microphone to exist before it lists speakers. Connect any mic or headset, then try again.'
              : 'Permission denied: the speaker list stays hidden. Allow the microphone for this page in the address bar, then try again.';
        }
      });
    }
  }

  $(`#ch-${id}`).addEventListener('change', (e) => {
    channel.mode = e.target.value;
    if (live.active && channel !== chR) live.wire();
    else onNudge();
  });
  $(`#vol-${id}`).addEventListener('input', (e) => {
    channel.setVolume(e.target.value / 100);
    $(`#vol-out-${id}`).textContent = `${e.target.value}%`;
  });

  const nudge = $(`#nudge-${id}`);
  let timer;
  const setNudge = (v) => {
    v = Math.max(-300, Math.min(300, v));
    nudge.value = v;
    channel.nudgeMs = v;
    $(`#nudge-out-${id}`).textContent = `${v > 0 ? '+' : ''}${v} ms`;
    clearTimeout(timer);
    timer = setTimeout(() => onNudge(), 150); // wait for the slider to settle
  };
  nudge.addEventListener('input', () => setNudge(Number(nudge.value)));
  $(`#nm-${id}`).addEventListener('click', () => setNudge(channel.nudgeMs - 5));
  $(`#np-${id}`).addEventListener('click', () => setNudge(channel.nudgeMs + 5));

  $(`#tone-${id}`).addEventListener('click', async () => {
    await channel.resume();
    channel.tone();
  });
}

// ---------------------------------------------------------------- shared state
const chA = new Channel('A');
const chB = new Channel('B');
const chR = new Channel('Room');
const dual = new Player([chA, chB]);
const live = new LiveRouter([chA, chB]); // another tab's sound (YouTube) on both speakers
const room = new Player([chR]);

$('#speakers').innerHTML = speakerCard('a', 'Speaker A') + speakerCard('b', 'Speaker B');
$('#room-speaker').innerHTML = speakerCard('r', 'This device');
bindSpeaker('a', chA);
bindSpeaker('b', chB);
bindSpeaker('r', chR);
refreshOutputs();
// Pre-set the stereo pair so "Left"/"Right" is one click away on each side.
$('#ch-a').value = 'both';
$('#ch-b').value = 'both';

let mode = 'dual';
let roomClient = null;
let roomCode = null;
let roomState = null;
let loadedUrl = null;
let wakeLock = null;
const active = () => (mode === 'dual' ? dual : room);
const setStatus = (msg) => ($('#status').textContent = msg);

// ---------------------------------------------------------------- tabs
function setMode(next) {
  mode = next;
  for (const m of ['dual', 'room']) {
    $(`#tab-${m}`).setAttribute('aria-selected', String(m === next));
    $(`#panel-${m}`).hidden = m !== next;
  }
  if (next === 'dual') {
    $('#player').hidden = false;
    onNudge = () => (live.active ? live.applyNudges() : restartDual());
  } else {
    if (live.active) stopLive();
    $('#player').hidden = !roomCode;
    onNudge = () => roomClient && syncRoomPlayer();
  }
  // Tab sharing feeds this computer's speakers only, so it belongs to Dual Output.
  $('#tab-share').hidden = next !== 'dual' || !canSink || !canCaptureTab();
  refreshTransport();
  setStatus('');
}
$('#tab-dual').addEventListener('click', () => setMode('dual'));
$('#tab-room').addEventListener('click', () => setMode('room'));

// ---------------------------------------------------------------- loading tracks
async function decode(arrayBuffer, ctx) {
  return ctx.decodeAudioData(arrayBuffer);
}

async function fetchBuffer(url, ctx) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Download failed (${res.status})`);
  return decode(await res.arrayBuffer(), ctx);
}

function trackLoaded(name) {
  if (!name.startsWith('Live from tab')) $('#track-name').dataset.file = name;
  $('#track-name').textContent = name;
  refreshTransport();
}

async function loadLocal(buffer, loop, name) {
  if (live.active) stopLive();
  dual.setTrack(buffer, loop);
  trackLoaded(name);
}

$('#load-click').addEventListener('click', async () => {
  if (mode === 'dual') {
    await loadLocal(clickBuffer(chA.ctx), true, 'Sync click test (loops)');
  } else {
    roomClient?.cmd({ type: 'load', url: 'builtin:click', name: 'Sync click test (loops)', loop: true });
  }
});

$('#load-file').addEventListener('change', async (e) => {
  const file = e.target.files[0];
  e.target.value = '';
  if (!file) return;
  try {
    if (mode === 'dual') {
      setStatus('Decoding…');
      await loadLocal(await decode(await file.arrayBuffer(), chA.ctx), false, file.name);
      setStatus('');
    } else {
      setStatus('Uploading…');
      const res = await fetch(`/api/upload?room=${roomCode}`, { method: 'POST', body: file });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error);
      roomClient.cmd({ type: 'load', url: body.url, name: file.name });
      setStatus('');
    }
  } catch (err) {
    setStatus(`Could not load that file: ${err.message}`);
  }
});

$('#load-url-go').addEventListener('click', async () => {
  const url = $('#load-url').value.trim();
  if (!url) return;
  const name = decodeURIComponent(url.split('/').pop().split('?')[0] || url);
  if (mode === 'room') return roomClient.cmd({ type: 'load', url, name });
  try {
    setStatus('Downloading…');
    await loadLocal(await fetchBuffer(url, chA.ctx), false, name);
    setStatus('');
  } catch (err) {
    setStatus(`Could not load that link: ${err.message}. The site hosting it must allow cross-origin downloads; uploading the file always works.`);
  }
});

// ---------------------------------------------------------------- transport
function refreshTransport() {
  if (mode === 'dual' && live.active) {
    $('#play').disabled = false;
    $('#play').textContent = 'Stop sharing';
    $('#seek').disabled = true;
    return;
  }
  const p = active();
  const ready = !!p.buffer;
  $('#play').disabled = !ready;
  $('#play').textContent = (mode === 'room' ? roomState?.playing : p.playing) ? 'Pause' : 'Play';
  $('#seek').disabled = !ready || p.loop;
}

async function resumeAll() {
  await Promise.all((mode === 'dual' ? [chA, chB] : [chR]).map((c) => c.resume()));
}

function restartDual() {
  if (dual.playing) dual.play(dual.position(), performance.now() + 150);
}

$('#play').addEventListener('click', async () => {
  await resumeAll();
  if (mode === 'dual' && live.active) {
    stopLive();
  } else if (mode === 'dual') {
    if (dual.playing) dual.stop();
    else dual.play(dual.parked >= dual.duration && !dual.loop ? 0 : dual.parked, performance.now() + 250);
  } else if (roomState?.playing) {
    roomClient.cmd({ type: 'pause' });
  } else {
    const atEnd = !room.loop && room.parked >= room.duration - 0.05;
    roomClient.cmd(atEnd ? { type: 'play', pos: 0 } : { type: 'play' });
  }
  refreshTransport();
});

let seeking = false;
$('#seek').addEventListener('input', () => (seeking = true));
$('#seek').addEventListener('change', async () => {
  seeking = false;
  const p = active();
  const pos = ($('#seek').value / 1000) * p.duration;
  if (mode === 'room') return roomClient.cmd({ type: 'seek', pos });
  if (dual.playing) dual.play(pos, performance.now() + 150);
  else dual.parked = pos;
});

setInterval(() => {
  const p = active();
  if (mode === 'dual' && p.hasEnded()) {
    p.stop(p.duration);
    refreshTransport();
  }
  if (mode === 'room' && p.hasEnded()) p.stop(p.duration);
  for (const [id, ch] of [['a', chA], ['b', chB], ['r', chR]]) $(`#level-${id}`).value = ch.level();
  if (mode === 'dual' && live.active) {
    $('#time').textContent = 'Live';
    for (const id of ['a', 'b']) {
      const el = $(`#drift-${id}`);
      if (!el.textContent.startsWith('Permission') && !el.textContent.startsWith('This browser')) el.textContent = '';
    }
    return;
  }
  const pos = p.position();
  if (!seeking && p.duration) $('#seek').value = p.loop ? 0 : Math.round((pos / p.duration) * 1000);
  $('#time').textContent = p.loop ? (p.playing ? 'Looping' : 'Stopped') : `${fmt(pos)} / ${fmt(p.duration)}`;

  const drifts = p.driftsMs();
  const targets = mode === 'dual' ? ['a', 'b'] : ['r'];
  targets.forEach((id, i) => {
    const el = $(`#drift-${id}`);
    if (el.textContent.startsWith('Permission') || el.textContent.startsWith('This browser')) return;
    el.textContent = drifts[i] === null ? '' : `Drift: ${drifts[i] > 0 ? '+' : ''}${drifts[i].toFixed(0)} ms`;
  });
}, 100);

// ---------------------------------------------------------------- YouTube / any tab
function stopLive() {
  live.stop();
  trackLoaded(dual.buffer ? $('#track-name').dataset.file || 'Track' : 'Nothing loaded');
  setStatus('');
  refreshTransport();
}
live.onEnded = () => {
  trackLoaded(dual.buffer ? $('#track-name').dataset.file || 'Track' : 'Nothing loaded');
  setStatus('Tab sharing stopped.');
  refreshTransport();
};

$('#load-tab').addEventListener('click', async () => {
  await resumeAll(); // inside the click, so audio is allowed to start
  if (dual.playing) dual.stop();
  setStatus('Choose the YouTube tab in the browser window, keep "Also share tab audio" on, then click Share.');
  try {
    await live.start();
    trackLoaded('Live from tab: the shared tab is playing on both speakers');
    setStatus('Playing that tab on both speakers. Control the music in the YouTube tab; nudge here if you hear an echo.');
  } catch (e) {
    setStatus(
      e.name === 'NotAllowedError'
        ? 'Sharing was cancelled.'
        : e.name === 'NoAudioError'
          ? 'No sound came through. Choose a browser tab (not a window or the whole screen) and keep "Also share tab audio" switched on.'
          : `Could not share the tab: ${e.message}`,
    );
  }
  refreshTransport();
});

// ---------------------------------------------------------------- Room Sync
async function keepAwake() {
  try {
    wakeLock = await navigator.wakeLock?.request('screen');
  } catch {
    /* optional: not available on http or without a gesture */
  }
}
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState !== 'visible' || !roomCode) return;
  keepAwake();
  // Phones pause audio in the background; line back up with the room on return.
  chR.resume().then(syncRoomPlayer);
});

function syncRoomPlayer() {
  const s = roomState;
  if (!s || !room.buffer) return;
  if (s.playing) room.play(s.pos, roomClient.toLocalPerf(s.at));
  else room.stop(s.pos);
  refreshTransport();
}

async function applyRoomState(state) {
  roomState = state;
  if (state.url !== loadedUrl) {
    loadedUrl = state.url;
    trackLoaded(state.name || (state.url ? 'Shared track' : 'Nothing loaded'));
    room.setTrack(null);
    if (state.url) {
      setStatus('Loading track…');
      try {
        const buf = state.url === 'builtin:click' ? clickBuffer(chR.ctx) : await fetchBuffer(state.url, chR.ctx);
        if (loadedUrl !== state.url) return; // a newer track was chosen meanwhile
        room.setTrack(buf, state.loop);
        setStatus('');
      } catch (e) {
        setStatus(`Could not load the shared track: ${e.message}`);
        return;
      }
    }
  }
  syncRoomPlayer();
  refreshTransport();
}

function showRoom(code) {
  roomCode = code;
  $('#room-lobby').hidden = true;
  $('#room-live').hidden = false;
  $('#room-code-big').textContent = code;
  $('#player').hidden = false;
  history.replaceState(null, '', `?room=${code}`);
}

function leaveRoom() {
  roomClient?.close();
  roomClient = null;
  roomCode = null;
  roomState = null;
  loadedUrl = null;
  room.setTrack(null);
  wakeLock?.release?.().catch(() => {});
  $('#room-lobby').hidden = false;
  $('#room-live').hidden = true;
  $('#player').hidden = true;
  $('#track-name').textContent = 'Nothing loaded';
  history.replaceState(null, '', location.pathname);
  refreshTransport();
}

async function enterRoom(action) {
  $('#room-error').textContent = '';
  await resumeAll(); // must happen inside the click, or iOS keeps audio blocked
  keepAwake();
  roomClient = new RoomClient({
    onJoined: (code, state, members) => {
      showRoom(code);
      $('#room-members').textContent = members;
      applyRoomState(state);
    },
    onState: applyRoomState,
    onMembers: (n) => ($('#room-members').textContent = n),
    onError: (msg) => {
      $('#room-error').textContent = msg;
      setStatus(msg);
    },
    onClock: (clock, previous) => {
      // A better clock estimate moves the shared start time on this device: re-line up if it moved much.
      if (previous && Math.abs(clock.offset - previous.offset) > 15 && roomState?.playing) syncRoomPlayer();
    },
    onClose: () => {
      if (roomCode) {
        const code = roomCode;
        leaveRoom();
        $('#room-code').value = code;
        $('#room-error').textContent = 'Lost connection to the room. Tap Join to rejoin.';
      }
    },
  });
  try {
    await roomClient.connect();
  } catch (e) {
    roomClient = null;
    $('#room-error').textContent = `${e.message}. Room Sync needs the DuoPlay server (npm start).`;
    return;
  }
  // Measure the clock before joining, so the first play is scheduled on the right timeline.
  $('#room-error').textContent = 'Connecting…';
  await roomClient.startClockSync();
  $('#room-error').textContent = '';
  action(roomClient);
}

$('#room-create').addEventListener('click', () => enterRoom((c) => c.create()));
$('#room-join').addEventListener('click', () => {
  const code = $('#room-code').value.trim().toUpperCase();
  if (code.length !== 4) return ($('#room-error').textContent = 'Enter the 4-letter room code.');
  enterRoom((c) => c.join(code));
});
$('#room-leave').addEventListener('click', leaveRoom);
$('#room-copy').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(`${location.origin}${location.pathname}?room=${roomCode}`);
    $('#room-copy').textContent = 'Copied';
    await sleep(1500);
  } catch {
    /* clipboard blocked */
  }
  $('#room-copy').textContent = 'Copy join link';
});

setInterval(() => {
  const c = roomClient?.clock;
  $('#room-clock').textContent = c
    ? `Clock: offset ${c.offset.toFixed(0)} ms, round trip ${c.rtt.toFixed(0)} ms`
    : 'Clock: measuring…';
}, 1000);

// Hard-resync a speaker that has slipped (Bluetooth latency changes, background throttling).
setInterval(() => {
  if (mode !== 'room' || !roomState?.playing) return;
  const [d] = room.driftsMs();
  if (d !== null && Math.abs(d) > 80) syncRoomPlayer();
}, 3000);

// A join link (?room=CODE) opens the Room Sync tab with the code filled in.
const linked = new URLSearchParams(location.search).get('room');
if (linked) {
  $('#room-code').value = linked.slice(0, 4).toUpperCase();
  setMode('room');
} else {
  setMode('dual');
}

// Read-only handle for debugging from the browser console (and the smoke test).
window.__duoplay = { dual, room, live, chA, chB, clock: () => roomClient?.clock ?? null };
