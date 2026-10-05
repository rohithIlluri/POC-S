# mindflight

Fly a **simulated DJI-Mini-class drone with your brain**. A Muse EEG headband (or a synthetic stand-in) is decoded into a few discrete intents, passed through a safety gate, and sent to a flight simulator. Simulation only. Nothing here talks to a real DJI aircraft.

```
Muse (4ch, 256 Hz) ─► band power ─► intent decoder ─► SAFETY GATE ─► backend
 TP9 AF7 AF8 TP10     (Goertzel)    focus/relax,       limits,       sim | ArduPilot SITL
                                    blink×2, clench    fail-safes
text agent ("takeoff, forward 5, turn right, land") ──────────────►┘ (same gate)
```

## Why a simulator, not the DJI Mini itself
DJI Minis expose no MAVLink and the consumer SDK doesn't cover them for autonomous control, so a "Mini" can't be the target of this loop directly. The standard way to prototype is a MAVLink autopilot in simulation (ArduPilot SITL). Control logic written against `Backend` transfers to any MAVLink craft later. That step would need its own safety review.

## Quick start (no dependencies, no hardware)
```sh
cd pocs/mindflight
python3 -m mindflight demo                       # synthetic Muse -> built-in sim
python3 -m mindflight agent "takeoff, forward 5, turn right, forward 5, land"
python3 -m unittest discover -s tests -v
```

## Controls
| Signal | Action |
|---|---|
| Focus (frontal beta/alpha up vs. your baseline) | fly forward (≤ 1.5 m/s) |
| Neutral / relax | hover |
| Double blink (< 1.5 s apart) | yaw right 90° |
| Jaw clench (EMG burst) | **land now**, latched |

First ~3 s of signal are used to calibrate your neutral baseline; takeoff happens only after ≥ 2 s of good electrode contact.

## Safety gate (`mindflight/safety.py`)
Every command from every source passes through it: speed cap, 25 m geofence (forward blocked at the edge), 120 s max flight, hover after 0.5 s of bad signal, land after 3 s, no arming without good signal, landing is latched. Tests cover each.

## Real hardware (untested here)
- **Muse**: `pip install pylsl muselsl`, run `muselsl stream`, then `python3 -m mindflight muse`. Implemented against the LSL API but not run against a headband in CI.
- **ArduPilot SITL**: `pip install pymavlink`, start `sim_vehicle.py -v ArduCopter`, then `python3 -m mindflight --backend mavlink demo`. Written, not yet exercised against a live SITL; expect to tune `MavlinkBackend`.

## Meta Muse Gadgets: on/off command
[Muse Gadgets](https://github.com/facebookincubator/muse-gadget-sdk) (Meta's open-source gadget SDKs for its Muse AI agent) lets a Linux box or ESP32 expose custom commands the agent can call. First integration: **"turn the drone on / off"**.

| Command | Params | Effect |
|---|---|---|
| `drone.power` | `state=on\|off` | on = arm, take off to 3 m, hover; off = land (latched). Simulation only |
| `drone.status` | none | phase, power, altitude, position |

Try it offline (emulates the Muse agent calling the gadget; no SDK, app or Bluetooth needed):
```sh
python3 -m mindflight gadget drone.power state=on
```

Wire it into the Linux Device SDK (`linux/src/musegadget/executor.py`), per the SDK's `AGENTS.md`:
```python
from mindflight.gadget import COMMAND_SPECS as DRONE_SPECS, handle as drone_handle
COMMAND_SPECS.update(DRONE_SPECS)          # next to the existing specs

# at the top of Executor.run(self, command, params, timeout_ms=None):
r = drone_handle(command, params)
if r is not None:
    return r
```
Then `pip install -e pocs/mindflight` on the gadget machine, pair it in the Muse app (Settings > Devices > Developer mode > Add Device), and ask Muse to turn the drone on.

Status: the handler and the SDK result shape (`{"ok": True, "payload": ...}` / `{"ok": False, "error": ...}`) are unit-tested here. It has **not** been run against the real SDK or a paired Muse app. The SDK docs site (gadgets.muse.ai) was unreachable from the build environment, so the spec format follows the SDK source (`required`/`optional` as name-keyed dicts; its `AGENTS.md` shows lists, so check which your SDK version expects). Pairing has no manufacturer verification, so use a trusted network.

## Limits and honest caveats
- The demo EEG is **synthetic** (sine bursts + noise); detection thresholds are tuned to it. Real Muse signals are noisier; expect per-user tuning of `BLINK_UV`, `CLENCH_X` and the focus thresholds.
- Focus/relax from 4 dry electrodes is a weak, slow signal. Treat it as a novelty control, not a reliable one.
- The agent parser is rule-based. An LLM planner can replace `agent.parse` as long as it emits the same steps; the gate still bounds the outcome.
