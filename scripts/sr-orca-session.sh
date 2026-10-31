#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
runtime=$1
debug=$2
out=$3
mkdir -p "$runtime/home/.config/speech-dispatcher" "$runtime/home/orca" "$runtime/xdg-runtime" "$runtime/xdg-config/speech-dispatcher" "$runtime/xdg-data" "$out" "$(dirname "$debug")"
chmod 700 "$runtime" "$runtime/home" "$runtime/xdg-runtime" "$out" "$(dirname "$debug")"
: > "$debug"
chmod 600 "$debug"
export HOME="$runtime/home" XDG_RUNTIME_DIR="$runtime/xdg-runtime" XDG_CONFIG_HOME="$runtime/xdg-config" XDG_DATA_HOME="$runtime/xdg-data"
printf 'pcm.!default { type file slave.pcm "null" file "%s/audio.raw" format "raw" }\npcm.null { type null }\n' "$out" > "$HOME/.asoundrc"
cp /etc/speech-dispatcher/speechd.conf "$XDG_CONFIG_HOME/speech-dispatcher/speechd.conf"
sed -i 's/^#\? *AudioOutputMethod .*/AudioOutputMethod "alsa"/' "$XDG_CONFIG_HOME/speech-dispatcher/speechd.conf"
if ! grep -q '^AudioOutputMethod' "$XDG_CONFIG_HOME/speech-dispatcher/speechd.conf"; then echo 'AudioOutputMethod "alsa"' >> "$XDG_CONFIG_HOME/speech-dispatcher/speechd.conf"; fi
# Orca opens --debug-file block-buffered, so speech lines would reach the file seconds late and
# break action correlation. Its private-settings customizations hook switches it to line buffering.
cat > "$runtime/home/orca/orca-customizations.py" <<'PY'
from orca import debug
if debug.debugFile is not None:
    debug.debugFile.reconfigure(line_buffering=True)
PY
# Xvfb owns display allocation and its lock; do not pre-create X lock files.
export DISPLAY=""
if [[ -z "$DISPLAY" ]]; then
  Xvfb -displayfd 3 -screen 0 1280x900x24 -nolisten tcp -noreset 3>"$runtime/display-number" >"$out/xvfb.log" 2>&1 &
  xvfb_pid=$!
  for _ in $(seq 100); do [[ -s "$runtime/display-number" ]] && break; kill -0 "$xvfb_pid" 2>/dev/null || { cat "$out/xvfb.log" >&2; exit 1; }; sleep .1; done
  [[ -s "$runtime/display-number" ]] || { echo 'Xvfb did not allocate a display' >&2; exit 1; }
  export DISPLAY=":$(cat "$runtime/display-number")"
else
  Xvfb "$DISPLAY" -screen 0 1280x900x24 -nolisten tcp -noreset >"$out/xvfb.log" 2>&1 &
fi
printf '%s' "$DISPLAY" > "$runtime/display"
cleanup_xvfb() { kill -TERM "$xvfb_pid" 2>/dev/null || true; wait "$xvfb_pid" 2>/dev/null || true; }
trap cleanup_xvfb EXIT INT TERM
for _ in $(seq 100); do
  kill -0 "$xvfb_pid" 2>/dev/null || { cat "$out/xvfb.log" >&2; exit 1; }
  [[ -S "/tmp/.X11-unix/X${DISPLAY#:}" ]] && break
  sleep .1
done
[[ -S "/tmp/.X11-unix/X${DISPLAY#:}" ]] || { echo "Xvfb did not start on $DISPLAY" >&2; exit 1; }
# A socket can appear just before Xvfb accepts clients. Verify a Unix-socket
# connection before starting Orca, which otherwise can log Cannot open display.
for _ in $(seq 100); do
  kill -0 "$xvfb_pid" 2>/dev/null || { cat "$out/xvfb.log" >&2; exit 1; }
  if python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.settimeout(.2); s.connect(sys.argv[1]); s.close()' "/tmp/.X11-unix/X${DISPLAY#:}" >/dev/null 2>&1; then break; fi
  sleep .1
done
python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.settimeout(.2); s.connect(sys.argv[1]); s.close()' "/tmp/.X11-unix/X${DISPLAY#:}" || { echo "Xvfb did not accept connections on $DISPLAY" >&2; exit 1; }
# D-Bus and Orca run in the private session bus, with their service descendants supervised by this group.
dbus-run-session -- bash -Eeuo pipefail -c '
  /usr/libexec/at-spi-bus-launcher --launch-immediately >"$1/atspi.log" 2>&1 &
  atspi=$!
  openbox >"$1/openbox.log" 2>&1 &
  wm=$!
  orca_args=(--replace --debug-file "$2" -u "$3/home/orca")
  if [[ "${TENDO_SR_ORCA_NO_START:-0}" == 1 ]]; then
    printf "%s\n" "TENDO_SR_ORCA_NO_START=1; intentionally not starting Orca" >"$1/orca.log"
    sleep 60 &
    orca_pid=$!
  else
    orca "${orca_args[@]}" >"$1/orca.log" 2>&1 &
    orca_pid=$!
  fi
  cleanup() {
    kill -TERM "$orca_pid" "$wm" "$atspi" 2>/dev/null || true
    for child in $(pgrep -P "$atspi" 2>/dev/null || true); do kill -TERM "$child" 2>/dev/null || true; done
    wait "$orca_pid" "$wm" "$atspi" 2>/dev/null || true
  }
  trap cleanup EXIT INT TERM
  # Publish the private bus before readiness so Playwright inherits its address.
  printf "%s\n" "$DBUS_SESSION_BUS_ADDRESS" > "$1/bus-address"
  # Orca being alive for a fixed interval is not readiness: it can still be
  # initializing, or can exit after failing to connect to the display/bus.
  ready_timeout=${TENDO_SR_READY_TIMEOUT_SECONDS:-30}
  [[ "$ready_timeout" =~ ^[1-9][0-9]*$ ]] || { echo "TENDO_SR_READY_TIMEOUT_SECONDS must be a positive integer" >&2; exit 1; }
  for _ in $(seq "$((ready_timeout * 10))"); do
    kill -0 "$orca_pid" 2>/dev/null || { echo "Orca exited during startup" >&2; exit 1; }
    if [[ "${TENDO_SR_ORCA_NO_START:-0}" != 1 ]] && grep -q "SPEECH OUTPUT:.*Screen reader on" "$2"; then
      touch "$1/ready"
      break
    fi
    sleep .1
  done
  [[ -e "$1/ready" ]] || { echo "Orca did not announce screen-reader readiness within $ready_timeout seconds" >&2; exit 1; }
  wait "$orca_pid"
' _ "$out" "$debug" "$runtime" &
wait $!
