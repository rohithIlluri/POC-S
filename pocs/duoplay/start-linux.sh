#!/usr/bin/env bash
# Start DuoPlay on a Linux machine.
#
#   ./start-linux.sh            set up "Computer sound", start the server, open the DuoPlay window
#   ./start-linux.sh --restore  only put the computer's normal sound output back (if a run was killed)
#   PORT=4000 ./start-linux.sh  use another port
#
# While it runs, every app (Brave, Spotify, ...) plays into a virtual output called
# DuoPlay, and the DuoPlay window plays that sound on your two speakers. Stopping
# this script (Ctrl+C or closing the terminal) puts the normal sound output back.
set -euo pipefail
cd "$(dirname "$0")"
PORT="${PORT:-3000}"

command -v node >/dev/null || { echo "Node.js 20 or newer is required: https://nodejs.org"; exit 1; }
major="$(node -p 'process.versions.node.split(".")[0]')"
[ "$major" -ge 20 ] || { echo "Node.js 20 or newer is required (found $(node -v))."; exit 1; }

if [ "${1:-}" = "--restore" ]; then
  node server/linux-audio.js restore && echo "Normal sound output restored."
  exit 0
fi

[ -d node_modules ] || npm install --omit=dev

# Chrome-family browser for the DuoPlay window (Firefox cannot choose speakers this way).
browser=""
for b in google-chrome google-chrome-stable chromium chromium-browser brave-browser microsoft-edge; do
  if command -v "$b" >/dev/null; then browser="$b"; break; fi
done
case "$browser" in
  google-chrome*) keep=chrome ;;
  chromium*) keep=chromium ;;
  brave*) keep=brave ;;
  microsoft-edge*) keep=msedge ;;
  *) keep="" ;;
esac

audio_ready=0
cleanup() {
  [ -n "${server:-}" ] && kill "$server" 2>/dev/null || true
  if [ "$audio_ready" = 1 ]; then
    node server/linux-audio.js restore >/dev/null 2>&1 && echo "Normal sound output restored."
  fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

if command -v pactl >/dev/null; then
  if out="$(node server/linux-audio.js setup --keep "$keep" 2>&1)"; then
    audio_ready=1
    echo "Computer sound: ready. Apps on this computer now play into DuoPlay."
    node -e '
      const r = JSON.parse(process.argv[1]);
      console.log(`Bluetooth speakers connected: ${r.speakers.length}`);
      for (const s of r.speakers) console.log(`  - ${s}`);
      if (r.speakers.length < 2) console.log("Connect both speakers in Bluetooth settings; DuoPlay picks them up when they connect.");
      if (r.moved) console.log(`Moved ${r.moved} playing app(s) onto DuoPlay.`);
    ' "$out"
  else
    echo "Computer sound could not be set up ($out). The rest of DuoPlay still works."
  fi
else
  echo "pactl not found, so Computer sound is off. Install it, then run this script again:"
  if command -v apt >/dev/null; then echo "  sudo apt install -y pulseaudio-utils"
  elif command -v dnf >/dev/null; then echo "  sudo dnf install -y pulseaudio-utils"
  elif command -v pacman >/dev/null; then echo "  sudo pacman -S libpulse"
  else echo "  install the package that provides pactl (pulseaudio-utils or libpulse)"; fi
  echo "The rest of DuoPlay still works."
fi

PORT="$PORT" node server/index.js &
server=$!
sleep 1
url="http://localhost:$PORT"
if [ -n "$browser" ]; then
  # Its own window and profile, allowed to start audio without a click, so DuoPlay
  # can start playing by itself when "Start automatically" is on.
  "$browser" --user-data-dir="$HOME/.config/duoplay-browser" --no-first-run --no-default-browser-check \
    --autoplay-policy=no-user-gesture-required --app="$url" >/dev/null 2>&1 &
  echo "DuoPlay window opened ($browser)."
else
  echo "Chrome or Chromium was not found. Install one and open $url in it."
fi
echo "Keep this terminal open while you listen. Press Ctrl+C to stop and restore normal sound."
wait "$server"
