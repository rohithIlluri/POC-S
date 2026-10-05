"""Muse Gadgets (facebookincubator/muse-gadget-sdk, Linux SDK) integration.

The SDK's extension point (linux/AGENTS.md) is: add a spec to COMMAND_SPECS in
src/musegadget/executor.py and a branch in Executor.run that returns
{"ok": True, "payload": ...} or {"ok": False, "error": ...}. This module provides
both halves so the executor change is two lines (see README). It is written from the
SDK's public source summary and has NOT been run against the real SDK or a paired
Muse app; the docs site was unreachable from the build environment."""

from .power import DronePower

COMMAND_SPECS = {
    "drone.power": {
        "description": "Turn the SIMULATED drone on (arm, take off, hover) or off (land). "
                       "Simulation only; never touches real aircraft.",
        "required": {"state": {"type": "string", "description": "'on' or 'off'."}},
        "optional": {},
        "timeout_ms": 5000,
    },
    "drone.status": {
        "description": "Report the simulated drone's phase, power state and position.",
        "required": {},
        "optional": {},
        "timeout_ms": 5000,
    },
}

_drone = DronePower()


def handle(command, params, drone=None):
    """Returns the SDK result dict, or None if the command isn't ours."""
    d = drone or _drone
    if command == "drone.power":
        state = str(params.get("state", "")).strip().lower()
        if state not in ("on", "off"):
            return {"ok": False, "error": "state must be 'on' or 'off'"}
        if drone is None:
            d.start()
        return {"ok": True, "payload": d.set(state)}
    if command == "drone.status":
        return {"ok": True, "payload": d.status()}
    return None
