# DuoPlay

Play one track on two speakers from a website, in time. A test bench for finding out what a browser can and cannot do here, with the two modes that can work.

> A website cannot pair Bluetooth speakers and an iPhone sends audio to one output at a time. So there are two modes, and the "This browser" card at the top of the page tells you which ones your device supports.

| Mode | Setup | Works on iPhone |
|---|---|---|
| **Dual Output** | One computer with both speakers already connected to it. Pick a speaker for each side. Runs fully in the browser, no server. | No |
| **Room Sync** | One device per speaker. Everyone joins the same room by code; each device plays through its own speaker on a shared clock. | Yes |

## Run it

```bash
npm install
npm start            # http://localhost:3000 (set PORT to change)
npm test             # timing maths, room state machine, server end to end
```

Dual Output also works as plain static files: serve `web/` from anything. Room Sync needs `npm start`, because the server relays room commands and keeps uploaded tracks.

## Linux quick start (one machine, two Bluetooth speakers)

Needs only the Linux machine: Bluetooth, Node.js 20+, and Chrome or Chromium (Firefox cannot choose speakers this way).

```bash
git clone -b feat/duoplay https://github.com/rohithIlluri/POC-S.git
cd POC-S/pocs/duoplay
./start-linux.sh
```

The script installs the one dependency, lists the Bluetooth speakers the sound system sees, starts the server and opens the page. Then:

1. Pair and connect both speakers in Bluetooth settings (the page's lists update when they connect).
2. In **Dual Output**, click **Find my speakers** and allow the microphone prompt. Chrome only lists speakers after that permission; the mic is switched off straight away.
3. Pick one speaker per side, press **Test tone** on each, load **Sync click test**, press Play, and nudge until you hear one click.

### Play everything from this computer (Brave, Spotify, YouTube…) — Linux

`./start-linux.sh` adds a virtual output called **DuoPlay** and makes it the computer's default, so every app plays into it. The **Computer sound** card at the top of Dual Output records that output and plays it on your two speakers.

1. Run `./start-linux.sh` (it opens DuoPlay in its own window).
2. Click **Play computer sound on both speakers**. The first time, allow the microphone prompt: that is how the browser hears the DuoPlay output. Your two Bluetooth speakers are picked automatically.
3. Play music in any app. Tick **Start automatically when DuoPlay opens** to skip the click next time.

Keep the terminal open while you listen. **Ctrl+C** (or closing the terminal) puts the computer's normal sound output back. If the script was killed some other way and the laptop is silent, run `./start-linux.sh --restore`.

### Play YouTube (or any site) on both speakers

1. Open YouTube in another Chrome tab and start the video.
2. In DuoPlay's **Dual Output**, click **Play from YouTube or another tab**.
3. Choose the YouTube tab, keep **Also share tab audio** on, and click **Share**.

Chrome mutes the tab and DuoPlay plays its sound on both speakers, with the same nudge, volume and left/right controls. Control the music in the YouTube tab. Click **Stop sharing** (in DuoPlay or Chrome's bar) to end it. This captures the tab's sound only for your own speakers; nothing is downloaded or saved. Some protected content (for example, some paid music services) may come through silent.

If one speaker stutters, the built-in Bluetooth adapter may not keep up with two streams; moving closer usually helps.

## Test it on real speakers

1. **Computer, two Bluetooth speakers (Dual Output).** Pair both speakers in the OS Bluetooth settings. Open the page in Chrome or Edge, pick one per side (use *Choose speaker…* or *Show device names* if the list shows no names), press **Test tone** on each to confirm which is which.
2. **Tune by ear.** Load **Sync click test**, press Play. You should hear one click per second. A double click means the speakers are apart: nudge the one that clicks first later (+), or the other earlier (−). Bluetooth speakers typically differ by tens to hundreds of ms, and the nudge range is ±300 ms.
3. **Phone and laptop (Room Sync).** Run `npm start`, open the laptop's address on both devices (same wifi: `http://<laptop-ip>:3000`). Create a room on one, join on the other with the code or the join link, load the click test or upload a file, press Play. Each device plays through its own speaker. For a phone over mobile data, or to get https, put a tunnel in front of port 3000 (for example `cloudflared tunnel --url http://localhost:3000`).
4. **Stereo pair.** Set one speaker to *Left channel only* and the other to *Right channel only*, in either mode.
5. **Record what you find.** *Copy test report* on the page copies the browser's capabilities. Add device results to the table below.

## What to check

- Does the Drift readout stay within about ±30 ms for a full song? It compares where each speaker is playing against where it should be. In Room Sync, a speaker that slips past 80 ms is re-synced automatically (you may hear a small glitch).
- Does the phone keep playing with the screen dimmed, and with the silent switch on?
- Does the phone stay in the room when you switch apps?

### Results (fill in)

| Device | Browser | Dual Output | Room Sync | Notes |
|---|---|---|---|---|
| | | | | |

## How it works

- **Dual Output**: one `AudioContext` per speaker, each routed to its chosen output with `setSinkId`. Both are started for the same moment using `getOutputTimestamp()`, which accounts for each context's own output latency. A per-speaker nudge compensates for what the browser cannot see (Bluetooth speaker delay).
- **Room Sync**: every device downloads the same file. Each trades pings with the server to learn its clock offset (taking the median of the three fastest round trips). When anyone presses play, the server announces "play from position X at server time T", 1.5 s ahead, and every device converts T to its own clock. A device that is late (slow decode, late join) starts further into the track instead.
- No audio is streamed between devices; the server only carries small messages and the uploaded file.

## Limits

- Streaming services (Spotify, Apple Music, YouTube) are not supported: their audio is protected and a web page cannot re-route it. Play files you upload or link to.
- A pasted link only works if the host allows cross-origin downloads. Uploading always works (40 MB max, held in server memory until the room empties).
- Bluetooth delay can change while playing. The drift readout shows it; Room Sync corrects it, Dual Output does not yet.
- iPhone blocks audio until a tap, so each person taps **Join** or **Create room** to start their audio.
- Room codes are the only access control. Anyone with a code can control playback and see the uploaded file. There are no accounts and nothing is stored on disk.

## Layout

```
web/      the site: index.html, app.js (UI), engine.js (audio), room.js (WebSocket + clock), sync.js (timing maths)
server/   index.js (static files, WebSocket rooms, uploads), rooms.js (room state machine)
tests/    node:test suites
```
