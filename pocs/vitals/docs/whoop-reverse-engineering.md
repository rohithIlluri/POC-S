# WHOOP 4.0 protocol study — brief for REA + Claude Code

This is the plan for learning the rest of the WHOOP 4.0 Bluetooth protocol
— chiefly **downloading the history stored on the strap** — so vitals can
read *your own strap's data* without the WHOOP app or a membership.

It is written to be handed to Claude Code running on your own machine with
[REA](https://github.com/morluto/rea) installed:

> Read `pocs/vitals/docs/whoop-reverse-engineering.md` and follow it.

## Ground rules

These keep the work on the right side of the line (they match REA's own
[disclaimer](https://github.com/morluto/rea/blob/main/DISCLAIMER.md)):

- **Goal is interoperability with a device you own** — reading the data your
  strap recorded about you. Not: unlocking paid features of WHOOP's app or
  cloud, bypassing login or subscription checks, or touching WHOOP servers.
- **Extract facts, not code.** Opcodes, byte offsets, field meanings, unit
  conversions and command order are facts. Do **not** copy WHOOP source,
  decompiled code, assets, or proprietary algorithms (recovery/strain/sleep
  formulas) into this repo. vitals computes its own metrics from published
  research.
- **Use a WHOOP APK you obtained legitimately** — pulled from your own
  Android phone (below). Don't download APKs from mirror sites (common
  malware vector). Don't decrypt iOS apps (that is DRM circumvention).
- **Never send commands to the strap during the study.** Analysis is
  static (the APK) and passive (captures). New commands only go into vitals
  after they're understood, behind tests, with a capture from your strap.
- Never build or send the strap's dangerous commands: flash erase/trim
  (`0x19`), set read pointer, reboot (`0x1D`), power-cycle, forget bonds
  (`0x0F`), firmware update, or persistent config writes. vitals' codec
  can't construct them by design (`internal/whoopble`).

## Setup (on your Mac)

```sh
# REA (needs Node 22.19+)
npx rea-agents setup            # pick Claude Code, approve, restart Claude Code

# Android tooling: JDK + JADX for APK analysis, adb to pull the APK
brew install openjdk jadx android-platform-tools
```

Get the APK from an Android phone with the WHOOP app installed (enable USB
debugging, connect it):

```sh
adb shell pm list packages | grep -i whoop          # find the package name
adb shell pm path <package>                          # lists base.apk (+ splits)
adb pull <path-to-base.apk> ~/whoop-study/whoop.apk
```

If you only have an iPhone, skip the APK and go straight to **captures**
(below) — they are the ground truth anyway.

## Questions to answer

Already known and tested in `internal/whoopble` (confirm, don't re-derive):
framing `0xAA | len u16 | crc8 | body | crc32`, CRC-8/SMBUS header,
CRC-32 body; command packet `0x23 seq cmd payload`; realtime packet `0x28`
(HR @8, RR count @9, RR u16 ms @10); history record `0x2F` (unix @7,
HR @17, RR count @18, RR @19); realtime toggle command `0x03`.

Needed, in priority order:

1. **Clock.** `SET_CLOCK` (`0x0A`?) payload — exact length/layout per
   firmware; `GET_CLOCK` response layout. How the app decides firmware
   version / protocol revision (`0x02`, `0x07`?) and response layouts.
2. **History sync sequence.** Every command the app sends from connect to
   "sync complete", in order, with payloads: the start command(s) (`0x16`?),
   any "init" packets, abort (`0x14`?).
3. **End-of-batch marker and ack.** Which packet type/fields carry the batch
   token (`0x31`?), its exact length, and the ack command (`0x17`?) layout.
   Whether the ack must be a write-with-response. What tells the app the
   whole history is drained.
4. **History record `0x2F` field map** beyond HR/RR: what bytes 11–16 and
   27–91 hold (accelerometer, SpO2/skin-temp raw values, wear/charging
   flags…), units and scaling.
5. **Other notifications:** `0x30`/events types and fields; command
   responses on `CMD_FROM_STRAP`; battery level command/response.
6. **Safety list:** every opcode the app treats as destructive or
   persistent, so vitals can block them explicitly.

## What to bring back

A single file, `whoop-protocol-findings.md`, with one entry per fact:

```
### SET_CLOCK
- opcode: 0x0A, payload: [u32 LE unix seconds][u32 LE subseconds 1/32768]
- evidence: <class / method / string where this was observed>
- confidence: confirmed in capture | static only | guess
```

No decompiled code in it — descriptions and byte layouts only. Then a
capture from your strap (next section) to check against.

## Captures from your strap (ground truth)

vitals records everything it exchanges with a WHOOP:

1. `vitals serve --open` in Chrome → **Connect strap** (WHOOP direct).
2. Let it run a minute, then **Download capture** →
   `whoop-capture-<time>.txt`.
3. `vitals strap decode whoop-capture-<time>.txt` summarises it: packet
   types per channel, heart-rate range, RR count, and **the strap's clock**
   (if it reads 1970 or years off, SET_CLOCK is the first thing to build).

An Android phone can also record the WHOOP app's own sync: Developer
options → **Enable Bluetooth HCI snoop log**, open the WHOOP app and let it
sync, then `adb bugreport` and analyse `btsnoop_hci.log` with REA's packet
capture tools (Wireshark/TShark). That capture of the official sync is the
best possible evidence for questions 1–3.

## How findings become code

For each confirmed fact: a builder or decoder in `internal/whoopble`, a
test using real bytes from a capture, and only then a UI step. History sync
will persist each batch to disk **before** acknowledging it (an ack lets the
strap free that memory), and stops on anything unexpected.
