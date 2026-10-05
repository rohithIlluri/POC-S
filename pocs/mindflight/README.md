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

## Limits and honest caveats
- The demo EEG is **synthetic** (sine bursts + noise); detection thresholds are tuned to it. Real Muse signals are noisier; expect per-user tuning of `BLINK_UV`, `CLENCH_X` and the focus thresholds.
- Focus/relax from 4 dry electrodes is a weak, slow signal. Treat it as a novelty control, not a reliable one.
- The agent parser is rule-based. An LLM planner can replace `agent.parse` as long as it emits the same steps; the gate still bounds the outcome.
