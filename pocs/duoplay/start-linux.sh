#!/usr/bin/env bash
# Start DuoPlay on a Linux machine and open it in Chrome/Chromium.
# Usage: ./start-linux.sh        (PORT=4000 ./start-linux.sh to change the port)
set -euo pipefail
cd "$(dirname "$0")"
PORT="${PORT:-3000}"

command -v node >/dev/null || { echo "Node.js 20 or newer is required: https://nodejs.org"; exit 1; }
major="$(node -p 'process.versions.node.split(".")[0]')"
[ "$major" -ge 20 ] || { echo "Node.js 20 or newer is required (found $(node -v))."; exit 1; }
[ -d node_modules ] || npm install --omit=dev

# Report the Bluetooth speakers the sound system can see (PipeWire or PulseAudio).
if command -v pactl >/dev/null; then
  sinks="$(pactl list short sinks 2>/dev/null | grep -i bluez || true)"
  count="$(printf '%s' "$sinks" | grep -c . || true)"
  echo "Bluetooth speakers connected: $count"
  [ -n "$sinks" ] && printf '%s\n' "$sinks" | awk '{print "  - " $2}'
  [ "$count" -ge 2 ] || echo "Dual Output needs 2. Pair and connect both in Bluetooth settings; the page updates when they connect."
else
  echo "pactl not found, so connected speakers were not checked."
fi

browser=""
for b in google-chrome google-chrome-stable chromium chromium-browser brave-browser microsoft-edge; do
  if command -v "$b" >/dev/null; then browser="$b"; break; fi
done

PORT="$PORT" node server/index.js &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT INT TERM
sleep 1
url="http://localhost:$PORT"
if [ -n "$browser" ]; then
  "$browser" "$url" >/dev/null 2>&1 &
else
  echo "Chrome or Chromium was not found. Dual Output needs one of them (Firefox cannot choose speakers this way)."
  echo "Open $url in Chrome/Chromium once installed."
fi
echo "Press Ctrl+C to stop."
wait "$server"
