#!/usr/bin/env bash
# Offline behavior tests for scripts/install-autonomy-service.sh.
#
# Every case builds a disposable sandbox (a HOME-like tree with the EXACT real
# unit texts, a real git checkout with a bare origin, a fake maintenance script
# and prompt, a fake guard) and runs the real installer with fakes first on
# PATH. Nothing reaches the real user systemd, the real host files, real locks
# or GitHub. Output is `ok`/`FAIL` lines; non-zero exit if any case fails.
set -uo pipefail

SOURCE_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
INSTALLER="${SOURCE_ROOT}/scripts/install-autonomy-service.sh"
MIGRATION_TEST="${SOURCE_ROOT}/scripts/test_autonomy_migration.py"
REAL_MIGRATION="${SOURCE_ROOT}/scripts/autonomy_migration.py"
UID_NUM=$(id -u)

WORK=$(mktemp -d)
holders=()
cleanup() {
  local pid
  for pid in "${holders[@]:-}"; do
    [[ -n $pid ]] && kill "$pid" 2>/dev/null
  done
  pkill -KILL -f "${WORK}/" 2>/dev/null
  [[ ${KEEP_WORK:-} == 1 ]] || rm -rf "$WORK"
}
trap cleanup EXIT

failures=0
pass() { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2" >&2; failures=$((failures + 1)); }

EXTRA_ENV=()
OUT=""
RC=0
CD=""

assert_eq() { # name want got
  if [[ $3 == "$2" ]]; then pass "$1"; else fail "$1" "want '$2' got '$3'"; fi
}
assert_file_contains() { # name file needle
  if grep -qF -- "$3" "$2" 2>/dev/null; then pass "$1"; else fail "$1" "$2 does not contain '$3'"; fi
}
assert_file_lacks() {
  if grep -qF -- "$3" "$2" 2>/dev/null; then fail "$1" "$2 unexpectedly contains '$3'"; else pass "$1"; fi
}
assert_output_contains() {
  case $OUT in
    *"$2"*) pass "$1" ;;
    *) fail "$1" "output missing '$2'"; printf '%s\n' "$OUT" | sed 's/^/     /' >&2 ;;
  esac
}
assert_output_lacks() {
  case $OUT in
    *"$2"*) fail "$1" "output unexpectedly contains '$2'" ;;
    *) pass "$1" ;;
  esac
}
assert_rc() { assert_eq "$1" "$2" "$RC"; }

tree_hash() {
  # Working tree only: `git status` legitimately refreshes .git/index.
  ( cd "$1" && find . -path ./.git -prune -o -path '*/.git' -prune -o -type f -print0 |
      sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1 )
}

file_mode() { stat -c '%a' "$1"; }

# ---------------------------------------------------------------------------
# sandbox construction
# ---------------------------------------------------------------------------
write_units() {
  local unit_dir=$1
  mkdir -p "${unit_dir}/tendo-autonomous.service.d"
  cat >"${unit_dir}/tendo-autonomous.service" <<'EOF'
[Unit]
Description=Tendo autonomous development loop
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/srv/tendo-checkout
Environment=PATH=/opt/opencode/bin:/home/tester/.local/bin:/usr/local/bin:/usr/bin:/bin
Environment=OPENCODE_BIN=/opt/opencode/bin/opencode
Environment=GITHUB_TOKEN=unit-secret-value
ExecStart=/srv/tendo-checkout/scripts/autonomous-loop.sh
Restart=on-failure
RestartSec=30
TimeoutStopSec=90
KillMode=control-group

[Install]
WantedBy=default.target
EOF
  printf '[Service]\nEnvironment=OPENCODE_BIN=/home/tester/.local/bin/opencode-tendo-attach\n' \
    >"${unit_dir}/tendo-autonomous.service.d/10-attach.conf"
  printf '[Service]\nEnvironment=OPENCODE_BIN=/home/tester/.local/bin/opencode-tendo-safe\nRestart=no\n' \
    >"${unit_dir}/tendo-autonomous.service.d/20-safe.conf"
  printf '[Service]\nEnvironment=SUCCESS_SLEEP=300\n' \
    >"${unit_dir}/tendo-autonomous.service.d/30-cycle-spacing.conf"
  printf '[Unit]\nWants=opencode-v2.service\nAfter=opencode-v2.service\n' \
    >"${unit_dir}/tendo-autonomous.service.d/40-opencode-dependency.conf"

  cat >"${unit_dir}/tendo-maintenance.service" <<'EOF'
[Unit]
Description=Tendo post-release maintenance

[Service]
Type=oneshot
ExecStart=%h/.local/bin/tendo-maintenance.sh
EOF
  cat >"${unit_dir}/tendo-maintenance.timer" <<'EOF'
[Unit]
Description=Tendo post-release maintenance timer

[Timer]
OnCalendar=*-*-* 04:00:00
Persistent=true

[Install]
WantedBy=timers.target
EOF
}

write_fakes() {
  local bin=$1
  mkdir -p "$bin"

  # systemctl: file-backed state machine that computes unit properties from the
  # sandbox unit files. Every call is appended to the shared call log.
  cat >"${bin}/systemctl" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE}/systemd"
mkdir -p "$state" "${FAKE_STATE}/journal"
printf 'systemctl %s\n' "$*" >>"${FAKE_STATE}/calls.log"

stop_unit() { # remove the fake process of a stopped unit
  local unit=$1
  case $unit in
    tendo-autonomous.service)
      grep -v -x 'scripts/autonomy-controller.sh' "${FAKE_STATE}/procs" \
        2>/dev/null >"${FAKE_STATE}/procs.tmp" || true
      mv "${FAKE_STATE}/procs.tmp" "${FAKE_STATE}/procs" 2>/dev/null || true
      ;;
  esac
}

start_unit() {
  local unit=$1
  if [[ $unit == tendo-autonomous.service ]]; then
    printf '%s %s\n' "$(date +%s)" '[autonomy] controller started idle_poll=300s' \
      >>"${FAKE_STATE}/journal/tendo-autonomous.service.log"
    if ! grep -qx 'scripts/autonomy-controller.sh' "${FAKE_STATE}/procs" 2>/dev/null; then
      printf 'scripts/autonomy-controller.sh\n' >>"${FAKE_STATE}/procs"
    fi
  fi
}

# Emulates the REAL `systemctl --user show` formats captured on the host:
#   Restart=no
#   TimeoutStopUSec=1min 30s            (there is no TimeoutStopSec property)
#   ExecStart={ path=... ; argv[]=... ; ... start_time=[Thu ...] ; ... }
#   Environment=K=V K2=V2               (space separated, continuations joined)
#   DropInPaths=/a.conf /b.conf
show_unit() {
  local unit=$1
  shift
  local dir="${AUTONOMY_UNIT_DIR}" files=() prop line exec_start restart killmode timeout_sec timeout_span
  local started stopped pid code status active_state environment dropins
  files=("${dir}/${unit}")
  if [[ -d "${dir}/${unit}.d" ]]; then
    for line in "${dir}/${unit}.d"/*.conf; do
      [[ -e $line ]] && files+=("$line")
    done
  fi
  exec_start=$(grep -h '^ExecStart=' "${files[@]}" 2>/dev/null | tail -1 | cut -d= -f2-)
  restart=$(grep -h '^Restart=' "${files[@]}" 2>/dev/null | tail -1 | cut -d= -f2-)
  killmode=$(grep -h '^KillMode=' "${files[@]}" 2>/dev/null | tail -1 | cut -d= -f2-)
  timeout_sec=$(grep -h '^TimeoutStopSec=' "${files[@]}" 2>/dev/null | tail -1 | cut -d= -f2-)
  if [[ $timeout_sec =~ ^[0-9]+$ ]]; then
    if ((timeout_sec % 60 == 0)); then
      timeout_span="$((timeout_sec / 60))min"
    elif ((timeout_sec > 60)); then
      timeout_span="$((timeout_sec / 60))min $((timeout_sec % 60))s"
    else
      timeout_span="${timeout_sec}s"
    fi
  else
    timeout_span="${timeout_sec:-1min 30s}"
  fi
  environment=$(python3 - "${dir}" "${unit}" <<'PY'
import os
import sys

directory, unit = sys.argv[1], sys.argv[2]
files = [os.path.join(directory, unit)]
dropin_dir = os.path.join(directory, unit + ".d")
if os.path.isdir(dropin_dir):
    files += [os.path.join(dropin_dir, name) for name in sorted(os.listdir(dropin_dir))
              if os.path.isfile(os.path.join(dropin_dir, name))]
values = []
for path in files:
    logical = []
    buffer = None
    for raw in open(path, encoding="utf-8", errors="replace").read().splitlines():
        stripped = raw.lstrip()
        is_comment = stripped.startswith("#") or stripped.startswith(";")
        if buffer is not None:
            if is_comment:
                continue
            line = buffer + " " + stripped
            buffer = None
        else:
            if is_comment:
                continue
            line = raw
        trailing = len(line) - len(line.rstrip("\\"))
        if trailing % 2 == 1:
            buffer = line[:-1]
            continue
        logical.append(line)
    if buffer is not None:
        logical.append(buffer)
    for line in logical:
        stripped = line.strip()
        if stripped.startswith("Environment="):
            values.append(stripped.split("=", 1)[1])
print(" ".join(values))
PY
)
  dropins=$(ls "${dir}/${unit}.d" 2>/dev/null | sed "s#^#${dir}/${unit}.d/#" | paste -sd' ' -)
  active_state=$(cat "${state}/${unit}.active" 2>/dev/null || printf 'inactive\n')
  if [[ $active_state == active ]]; then
    started='[Thu 2026-11-12 07:16:06 CET]'
    stopped='[Thu 2026-11-12 08:34:25 CET]'
    pid=137660
    code=exited
    status=143
  else
    started='[n/a]'
    stopped='[n/a]'
    pid=0
    code='(null)'
    status='0/0'
  fi
  for prop in "$@"; do
    case $prop in
      ExecStart) printf 'ExecStart={ path=%s ; argv[]=%s ; ignore_errors=no ; start_time=%s ; stop_time=%s ; pid=%s ; code=%s ; status=%s }\n' "$exec_start" "$exec_start" "$started" "$stopped" "$pid" "$code" "$status" ;;
      Restart) printf 'Restart=%s\n' "${restart:-no}" ;;
      KillMode) printf 'KillMode=%s\n' "${killmode:-control-group}" ;;
      TimeoutStopUSec) printf 'TimeoutStopUSec=%s\n' "$timeout_span" ;;
      Environment) printf 'Environment=%s\n' "$environment" ;;
      DropInPaths) printf 'DropInPaths=%s\n' "$dropins" ;;
      *) : ;;
    esac
  done
}

args=("$@")
[[ ${args[0]:-} == --user ]] && args=("${args[@]:1}")
cmd=${args[0]:-}
unit=${args[1]:-}
enabled_file="${state}/${unit}.enabled"
active_file="${state}/${unit}.active"

# Failure injection: FAKE_SYSTEMCTL_FAIL="cmd:unit;cmd:*" (exit 1, optional
# FAKE_SYSTEMCTL_FAIL_OUTPUT). Barrier: FAKE_SYSTEMCTL_BARRIER=cmd blocks until
# ${FAKE_STATE}/barrier.release exists (used to test installer serialization).
if [[ -n ${FAKE_SYSTEMCTL_FAIL:-} ]]; then
  IFS=';' read -r -a injected <<<"${FAKE_SYSTEMCTL_FAIL}"
  for failure in "${injected[@]}"; do
    if [[ $failure == "${cmd}:${unit}" || $failure == "${cmd}:*" ]]; then
      [[ -n ${FAKE_SYSTEMCTL_FAIL_OUTPUT:-} ]] && printf '%s\n' "${FAKE_SYSTEMCTL_FAIL_OUTPUT}"
      printf 'injected systemctl failure: %s %s\n' "$cmd" "$unit" >&2
      exit 1
    fi
  done
fi
if [[ -n ${FAKE_SYSTEMCTL_BARRIER:-} && ${FAKE_SYSTEMCTL_BARRIER} == "$cmd" ]]; then
  : >"${FAKE_STATE}/barrier.ready"
  for _ in $(seq 1 300); do
    [[ -e "${FAKE_STATE}/barrier.release" ]] && break
    sleep 0.1
  done
fi

case $cmd in
  is-enabled)
    value=$(cat "$enabled_file" 2>/dev/null || printf 'disabled\n')
    printf '%s\n' "$value"
    [[ $value == enabled || $value == static ]] && exit 0 || exit 1
    ;;
  is-active)
    value=$(cat "$active_file" 2>/dev/null || printf 'inactive\n')
    printf '%s\n' "$value"
    [[ $value == active ]] && exit 0 || exit 3
    ;;
  enable) printf 'enabled\n' >"$enabled_file" ;;
  disable)
    if [[ ${args[1]:-} == --now ]]; then
      unit=${args[2]:-}
      printf 'disabled\n' >"${state}/${unit}.enabled"
      printf 'inactive\n' >"${state}/${unit}.active"
      stop_unit "$unit"
    else
      printf 'disabled\n' >"$enabled_file"
    fi
    ;;
  start)
    printf 'active\n' >"$active_file"
    start_unit "$unit"
    ;;
  stop)
    for u in "${args[@]:1}"; do
      printf 'inactive\n' >"${state}/${u}.active"
      stop_unit "$u"
    done
    # Simulates a new task appearing right after the stop (rollback race test).
    if [[ ${FAKE_STOP_HOOK:-} == busy ]]; then
      printf '1\n' >"${FAKE_STATE}/guard-sessions"
    fi
    ;;
  daemon-reload) : ;;
  show) show_unit "$unit" "${args[@]:2}" ;;
  *)
    printf 'fake systemctl: unsupported command %s\n' "$cmd" >&2
    exit 99
    ;;
esac
EOF

  # pgrep: matches the fake process-name file (no real process inspection).
  cat >"${bin}/pgrep" <<'EOF'
#!/usr/bin/env bash
count_only=0
pattern=""
while (($#)); do
  case $1 in
    -c) count_only=1; shift ;;
    -u) shift 2 ;;
    -f) pattern=$2; shift 2 ;;
    *) [[ -z $pattern ]] && pattern=$1; shift ;;
  esac
done
printf 'pgrep %s\n' "$*" >>"${FAKE_STATE}/calls.log"
if [[ -n ${FAKE_PGREP_FAIL_RC:-} ]]; then
  printf 'injected pgrep failure (rc=%s)\n' "${FAKE_PGREP_FAIL_RC}" >&2
  exit "${FAKE_PGREP_FAIL_RC}"
fi
python3 - "${FAKE_STATE}/procs" "$pattern" "$count_only" <<'PY'
import re
import sys

path, pattern, count_only = sys.argv[1], sys.argv[2], sys.argv[3] == "1"
try:
    lines = [line.strip() for line in open(path, encoding="utf-8") if line.strip()]
except FileNotFoundError:
    lines = []
rx = re.compile(pattern)
hits = [line for line in lines if rx.search(line)]
if count_only:
    print(len(hits))
    raise SystemExit(0 if hits else 1)
for index, _ in enumerate(hits, 1):
    print(10000 + index)
raise SystemExit(0 if hits else 1)
PY
EOF

  # journalctl: reads the fake epoch-prefixed journal file.
  cat >"${bin}/journalctl" <<'EOF'
#!/usr/bin/env bash
printf 'journalctl %s\n' "$*" >>"${FAKE_STATE}/calls.log"
unit=""
since=0
while (($#)); do
  case $1 in
    --user) shift ;;
    -u) unit=$2; shift 2 ;;
    --since) since=${2#@}; shift 2 ;;
    -o) shift 2 ;;
    *) shift ;;
  esac
done
file="${FAKE_STATE}/journal/${unit}.log"
[[ -f $file ]] || exit 0
python3 - "$file" "${since:-0}" <<'PY'
import sys

path, since = sys.argv[1], int(sys.argv[2] or 0)
for raw in open(path, encoding="utf-8"):
    epoch, _, text = raw.rstrip("\n").partition(" ")
    try:
        if int(epoch) >= since:
            print(text)
    except ValueError:
        continue
PY
EOF

  cat >"${bin}/loginctl" <<'EOF'
#!/usr/bin/env bash
printf 'loginctl %s\n' "$*" >>"${FAKE_STATE}/calls.log"
cat "${FAKE_STATE}/linger" 2>/dev/null || printf 'Linger=yes\n'
EOF

  # gh: delegate to the migration test module's REST shim (same fake store as
  # the Python unit tests).
  cat >"${bin}/gh" <<'EOF'
#!/usr/bin/env bash
exec python3 "${TENDO_TEST_MIGRATION}" --gh-shim "$@"
EOF
  chmod +x "${bin}"/*
}

make_store() { # case_dir sha [mode]
  local cd=$1 sha=$2 mode=${3:-normal}
  python3 - "$cd/state/github.json" "$sha" "$mode" <<'PY'
import json
import sys

path, sha, mode = sys.argv[1], sys.argv[2], sys.argv[3]
milestone = {"title": "Tendo v5.0.0 - Refined & Reliable"}
issues = {}
for number in range(445, 462):
    issues[str(number)] = {
        "number": number,
        "title": f"Ship work {number}",
        "state": "open",
        "labels": [{"name": "status:idle"}, {"name": "tendo:v5-autonomous"}],
        "milestone": milestone,
    }
issues["444"] = {
    "number": 444,
    "title": "Closed historical issue",
    "state": "closed",
    "labels": [{"name": "tendo:v5-autonomous"}],
    "milestone": {"title": "Tendo v4.0.0 - Modern Everyday"},
}
issues["500"] = {
    "number": 500,
    "title": "Unowned status:idle lookalike",
    "state": "open",
    "labels": [{"name": "status:idle"}],
    "milestone": None,
}
issues["501"] = {
    "number": 501,
    "title": "V5 milestone without source label",
    "state": "open",
    "labels": [{"name": "status:idle"}],
    "milestone": milestone,
}
labels = [
    {"name": "status:idle", "color": "ededed", "description": ""},
    {"name": "status:in-progress", "color": "ededed", "description": ""},
    {"name": "status:blocked", "color": "ededed", "description": ""},
    {"name": "tendo:v5-autonomous", "color": "ededed", "description": ""},
]
if mode == "missing":
    check_runs = {}
elif mode == "red":
    check_runs = {sha: [{"name": "required", "status": "completed", "conclusion": "failure",
                         "started_at": "2026-01-01T00:00:00Z"}]}
else:
    check_runs = {sha: [{"name": "required", "status": "completed", "conclusion": "success",
                         "started_at": "2026-01-01T00:00:00Z"}]}
store = {"issues": issues, "labels": labels, "check_runs": check_runs, "page_size": 100,
         "drop_writes": False, "create_label_race": False}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(store, handle, indent=1, sort_keys=True)
PY
}

TEMPLATE="${WORK}/template"

build_sandbox() { # dir -> pristine sandbox used as the per-case template
  local CD=$1
  local sb="${CD}/sandbox"
  mkdir -p "${CD}/bin" "${CD}/state/systemd" "${CD}/state/journal" "${CD}/runtime" "${CD}/backups"
  mkdir -p "${sb}/home/.config/systemd/user" "${sb}/home/.config/tendo-maintenance" \
           "${sb}/home/.local/bin" "${sb}/home/.local/state" "${sb}/repo/scripts" \
           "${sb}/repo/.autonomous"
  : >"${CD}/state/procs"
  : >"${CD}/state/calls.log"
  printf 'Linger=yes\n' >"${CD}/state/linger"
  printf '0\n' >"${CD}/state/guard-sessions"
  printf 'False\n' >"${CD}/state/guard-pending"
  printf 'True\n' >"${CD}/state/guard-ready"

  write_units "${sb}/home/.config/systemd/user"
  write_fakes "${CD}/bin"

  printf 'enabled\n' >"${CD}/state/systemd/tendo-autonomous.service.enabled"
  printf 'inactive\n' >"${CD}/state/systemd/tendo-autonomous.service.active"
  printf 'static\n' >"${CD}/state/systemd/tendo-maintenance.service.enabled"
  printf 'inactive\n' >"${CD}/state/systemd/tendo-maintenance.service.active"
  printf 'enabled\n' >"${CD}/state/systemd/tendo-maintenance.timer.enabled"
  printf 'inactive\n' >"${CD}/state/systemd/tendo-maintenance.timer.active"

  cat >"${sb}/home/.local/bin/opencode-tendo-safe" <<'EOF'
#!/usr/bin/env bash
state=$FAKE_STATE
mode=$(cat "$state/guard-mode" 2>/dev/null || echo ok)
case ${1:-} in
  --check)
    case $mode in
      fail-exit2) printf 'GUARD CHECK FAILED: injected guard failure\n'; exit 2 ;;
      garbage) printf 'not a guard report\n'; exit 0 ;;
      sessions-garbage) printf 'Repository ready: True\nPending marker: False\nActive Tendo sessions: many\n'; exit 0 ;;
    esac
    sessions=$(cat "$state/guard-sessions" 2>/dev/null || echo 0)
    pending=$(cat "$state/guard-pending" 2>/dev/null || echo False)
    ready=$(cat "$state/guard-ready" 2>/dev/null || echo True)
    printf 'Repository ready: %s\n' "$ready"
    printf 'Pending marker: %s\n' "$pending"
    printf 'Active Tendo sessions: %s\n' "$sessions"
    if [[ $sessions != 0 || $pending == True || $ready != True ]]; then
      printf 'GUARD CHECK FAILED: work is active\n'
      exit 2
    fi
    printf 'GUARD CHECK PASSED\n'
    exit 0
    ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "${sb}/home/.local/bin/opencode-tendo-safe"

  cat >"${sb}/home/.local/bin/tendo-maintenance.sh" <<'EOF'
#!/usr/bin/env bash
echo "maintenance ran"
EOF
  chmod +x "${sb}/home/.local/bin/tendo-maintenance.sh"

  cat >"${sb}/home/.config/tendo-maintenance/prompt.md" <<'EOF'
# Maintenance prompt
TOKEN=supersecret-do-not-print
Only the owner may read this file.
EOF

  cat >"${sb}/repo/scripts/autonomy-controller.sh" <<'EOF'
#!/usr/bin/env bash
echo "[autonomy] controller started idle_poll=300s"
EOF
  chmod +x "${sb}/repo/scripts/autonomy-controller.sh"
  cat >"${sb}/repo/scripts/autonomy-work-selection.py" <<'EOF'
#!/usr/bin/env python3
print("SELF-CHECK OK")
EOF
  cp "$REAL_MIGRATION" "${sb}/repo/scripts/autonomy_migration.py"
  printf '# Autonomy prompt (test fixture)\n' >"${sb}/repo/.autonomous/AUTONOMY_PROMPT.md"

  git -C "${sb}/repo" init -q -b main
  git -C "${sb}/repo" config user.email "test@example.com"
  git -C "${sb}/repo" config user.name "Test"
  git -C "${sb}/repo" add -A
  git -C "${sb}/repo" commit -qm "initial"
  git -C "${sb}/repo" init -q --bare "${CD}/origin.git"
  git -C "${sb}/repo" remote add origin "${CD}/origin.git"
  git -C "${sb}/repo" push -q origin main
  local sha
  sha=$(git -C "${sb}/repo" rev-parse HEAD)
  make_store "$CD" "$sha"
}

build_case() { # name -> sets CD from a fast copy of the pristine template
  local name=$1
  CD="${WORK}/case-${name}"
  rm -rf "$CD"
  cp -a "$TEMPLATE" "$CD"
  git -C "${CD}/sandbox/repo" remote set-url origin "${CD}/origin.git"
}

run_as() { # case_dir [installer args...]
  local cd=$1
  shift
  local -a envs=(
    "HOME=${cd}/sandbox/home"
    "TENDO_ROOT=${cd}/sandbox/repo"
    "AUTONOMY_UNIT_DIR=${cd}/sandbox/home/.config/systemd/user"
    "AUTONOMY_BACKUP_ROOT=${cd}/backups"
    "AUTONOMY_GUARD=${cd}/sandbox/home/.local/bin/opencode-tendo-safe"
    "AUTONOMY_MAINT_SCRIPT=${cd}/sandbox/home/.local/bin/tendo-maintenance.sh"
    "AUTONOMY_MAINT_PROMPT=${cd}/sandbox/home/.config/tendo-maintenance/prompt.md"
    "AUTONOMY_REPO=bigtcze/tendo"
    "AUTONOMY_SELECTOR=${cd}/sandbox/repo/scripts/autonomy-work-selection.py"
    "AUTONOMY_START_TIMEOUT=5"
    "XDG_RUNTIME_DIR=${cd}/runtime"
    "USER=tester"
    "PATH=${cd}/bin:/usr/bin:/bin"
    "FAKE_STATE=${cd}/state"
    "FAKE_GH_STORE=${cd}/state/github.json"
    "FAKE_GH_LOG=${cd}/state/calls.log"
    "FAKE_UNIT_DIR=${cd}/sandbox/home/.config/systemd/user"
    "TENDO_TEST_MIGRATION=${MIGRATION_TEST}"
  )
  if ((${#EXTRA_ENV[@]})); then
    envs+=("${EXTRA_ENV[@]}")
  fi
  OUT=$(env "${envs[@]}" bash "$INSTALLER" "$@" 2>&1) && RC=0 || RC=$?
  printf '%s\n' "$OUT" >"${cd}/last-output"
}

# ---------------------------------------------------------------------------
# assertions shared by the migration cases
# ---------------------------------------------------------------------------
assert_labels_migrated() { # case_dir name
  local store="$1/state/github.json"
  local output
  if output=$(python3 - "$store" <<'PY'
import json
import sys

store = json.load(open(sys.argv[1], encoding="utf-8"))
problems = []

def names(number):
    return sorted(label["name"] for label in store["issues"][str(number)]["labels"])

for number in range(445, 462):
    got = names(number)
    if got != ["release", "status:idle"]:
        problems.append(f"#{number}: {got}")
for number, want in ((444, ["tendo:v5-autonomous"]), (500, ["status:idle"]),
                     (501, ["status:idle"])):
    if names(number) != want:
        problems.append(f"#{number}: {names(number)} != {want}")
if "release" not in {label["name"] for label in store["labels"]}:
    problems.append("release label missing")
print("; ".join(problems))
raise SystemExit(0 if not problems else 1)
PY
  ); then
    pass "$2"
  else
    fail "$2" "$output"
  fi
}

assert_labels_original() { # case_dir name
  local store="$1/state/github.json"
  local output
  if output=$(python3 - "$store" <<'PY'
import json
import sys

store = json.load(open(sys.argv[1], encoding="utf-8"))
problems = []

def names(number):
    return sorted(label["name"] for label in store["issues"][str(number)]["labels"])

for number in range(445, 462):
    got = names(number)
    if got != ["status:idle", "tendo:v5-autonomous"]:
        problems.append(f"#{number}: {got}")
for number, want in ((444, ["tendo:v5-autonomous"]), (500, ["status:idle"]),
                     (501, ["status:idle"])):
    if names(number) != want:
        problems.append(f"#{number}: {names(number)} != {want}")
print("; ".join(problems))
raise SystemExit(0 if not problems else 1)
PY
  ); then
    pass "$2"
  else
    fail "$2" "$output"
  fi
}

apply_ok() { # case_dir
  run_as "$1" --apply
  assert_rc "apply succeeds" 0
}

backup_dirs() { # case_dir -> number of backup transaction directories
  find "$1/backups" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l
}

phase_of() { # backup dir
  python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["phase"])' "$1/state.json"
}

shrink_store() { # case_dir count -> keep only the first <count> targets
  python3 - "$1/state/github.json" "$2" <<'PY'
import json
import sys

path, count = sys.argv[1], int(sys.argv[2])
store = json.load(open(path, encoding="utf-8"))
keep = {str(number) for number in range(445, 445 + count)} | {"444", "500", "501"}
store["issues"] = {number: issue for number, issue in store["issues"].items() if number in keep}
json.dump(store, open(path, "w", encoding="utf-8"), indent=1, sort_keys=True)
PY
}

assert_small_labels() { # case_dir migrated|original count
  local store="$1/state/github.json" mode=$2 count=$3
  local output
  if output=$(python3 - "$store" "$mode" "$count" <<'PY'
import json
import sys

store = json.load(open(sys.argv[1], encoding="utf-8"))
mode, count = sys.argv[2], int(sys.argv[3])
problems = []

def names(number):
    return sorted(label["name"] for label in store["issues"][str(number)]["labels"])

for number in range(445, 445 + count):
    got = names(number)
    want = ["release", "status:idle"] if mode == "migrated" else ["status:idle", "tendo:v5-autonomous"]
    if got != want:
        problems.append(f"#{number}: {got} != {want}")
if names(444) != ["tendo:v5-autonomous"]:
    problems.append(f"#444: {names(444)}")
print("; ".join(problems))
raise SystemExit(0 if not problems else 1)
PY
  ); then
    pass "labels ${mode} for ${count} issue(s)"
  else
    fail "labels ${mode} for ${count} issue(s)" "$output"
  fi
}

# ---------------------------------------------------------------------------
# case 1: --check is a pure dry run
# ---------------------------------------------------------------------------
case_check_dry_run() {
  build_case check
  local cd=$CD
  local sb="${CD}/sandbox"
  local before after
  mkdir -p "${sb}/home/.config/systemd/user/tendo-maintenance.timer.d"
  printf '[Timer]\nRandomizedDelaySec=30\n' \
    >"${sb}/home/.config/systemd/user/tendo-maintenance.timer.d/override.conf"
  before=$(tree_hash "$sb")

  run_as "$cd" --check
  assert_rc "case1 check exits 0" 0
  assert_output_contains "case1 prints the label plan" "LABEL PLAN repo=bigtcze/tendo"
  assert_output_contains "case1 plans +release" "#445 Ship work 445: +release -tendo:v5-autonomous"
  assert_output_contains "case1 lists issue 461" "#461"
  assert_output_contains "case1 plans legacy drop-in removal" "10-attach.conf"
  assert_output_contains "case1 prints new ExecStart" "ExecStart=${sb}/repo/scripts/autonomy-controller.sh"
  assert_output_contains "case1 prints the kept drop-in" "40-opencode-dependency.conf"
  assert_output_contains "case1 lists maintenance drop-ins without editing them" \
    "tendo-maintenance.timer has drop-ins (left untouched): override.conf"
  assert_output_contains "case1 redacts a secret Environment value" "Environment=GITHUB_TOKEN=<redacted>"
  assert_output_lacks "case1 never prints the unit secret" "unit-secret-value"
  assert_output_lacks "case1 never prints the prompt secret" "supersecret-do-not-print"

  after=$(tree_hash "$sb")
  assert_eq "case1 sandbox tree unchanged" "$before" "$after"
  assert_eq "case1 no mutating gh calls" "0" "$(grep -cE '^gh (POST|DELETE)' "${cd}/state/calls.log")"
  assert_eq "case1 no mutating systemctl calls" "0" \
    "$(grep -cE '^systemctl (--user )?(disable|stop|start|enable|daemon-reload)( |$)' "${cd}/state/calls.log")"
  assert_eq "case1 no backup created" "0" "$(backup_dirs "$cd")"
}

# ---------------------------------------------------------------------------
# case 2: --apply success
# ---------------------------------------------------------------------------
case_apply_success() {
  build_case apply
  local cd=$CD
  local sb="${CD}/sandbox"
  local unit_dir="${sb}/home/.config/systemd/user"
  cp -a "$unit_dir" "${cd}/original-systemd"

  run_as "$cd" --apply
  assert_rc "case2 apply exits 0" 0
  assert_output_contains "case2 reports complete" "step 7/7 complete"

  assert_file_contains "case2 new ExecStart" "${unit_dir}/tendo-autonomous.service" "autonomy-controller.sh"
  assert_file_contains "case2 WorkingDirectory is TENDO_ROOT" "${unit_dir}/tendo-autonomous.service" \
    "WorkingDirectory=${sb}/repo"
  assert_file_lacks "case2 legacy loop removed" "${unit_dir}/tendo-autonomous.service" "autonomous-loop.sh"
  assert_file_contains "case2 guard wired in" "${unit_dir}/tendo-autonomous.service" \
    "Environment=OPENCODE_BIN=${sb}/home/.local/bin/opencode-tendo-safe"
  assert_file_contains "case2 IdlePoll" "${unit_dir}/tendo-autonomous.service" "IDLE_POLL_SECONDS=300"
  assert_file_contains "case2 keeps the original unit PATH" "${unit_dir}/tendo-autonomous.service" \
    "Environment=PATH=/opt/opencode/bin:/home/tester/.local/bin:/usr/local/bin:/usr/bin:/bin"

  assert_eq "case2 10-attach removed" "absent" "$( [[ -e ${unit_dir}/tendo-autonomous.service.d/10-attach.conf ]] && echo present || echo absent )"
  assert_eq "case2 20-safe removed" "absent" "$( [[ -e ${unit_dir}/tendo-autonomous.service.d/20-safe.conf ]] && echo present || echo absent )"
  assert_eq "case2 30-cycle-spacing removed" "absent" "$( [[ -e ${unit_dir}/tendo-autonomous.service.d/30-cycle-spacing.conf ]] && echo present || echo absent )"
  assert_eq "case2 40 kept" "present" "$( [[ -e ${unit_dir}/tendo-autonomous.service.d/40-opencode-dependency.conf ]] && echo present || echo absent )"

  local backup
  backup=$(cat "${cd}/backups/latest")
  assert_eq "case2 backup dir mode" "700" "$(file_mode "$backup")"
  assert_eq "case2 state.json mode" "600" "$(file_mode "${backup}/state.json")"
  assert_eq "case2 labels.json mode" "600" "$(file_mode "${backup}/labels.json")"
  assert_eq "case2 unit copy unchanged" "same" \
    "$(cmp -s "${backup}/units/tendo-autonomous.service" "${cd}/original-systemd/tendo-autonomous.service" && echo same || echo different)"
  assert_eq "case2 drop-in copy unchanged" "same" \
    "$(cmp -s "${backup}/units/tendo-autonomous.service.d/20-safe.conf" "${cd}/original-systemd/tendo-autonomous.service.d/20-safe.conf" && echo same || echo different)"
  assert_eq "case2 removed drop-ins archived" "3" "$(ls -1 "${backup}/removed-dropins" | wc -l)"
  assert_eq "case2 phase complete" "complete" \
    "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["phase"])' "${backup}/state.json")"

  assert_eq "case2 timer disabled" "disabled" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.enabled")"
  assert_eq "case2 timer inactive" "inactive" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.active")"
  assert_eq "case2 maintenance service inactive" "inactive" "$(cat "${cd}/state/systemd/tendo-maintenance.service.active")"
  assert_eq "case2 service active" "active" "$(cat "${cd}/state/systemd/tendo-autonomous.service.active")"
  assert_eq "case2 one controller process" "scripts/autonomy-controller.sh" "$(cat "${cd}/state/procs")"
  assert_file_contains "case2 journal startup line" "${cd}/state/journal/tendo-autonomous.service.log" \
    "[autonomy] controller started"

  assert_labels_migrated "$cd" "case2 labels migrated, #444/#500/#501 untouched"

  local timer_line first_mutation
  timer_line=$(grep -n 'disable --now tendo-maintenance.timer' "${cd}/state/calls.log" | head -1 | cut -d: -f1)
  first_mutation=$(grep -n '^gh POST .*labels' "${cd}/state/calls.log" | head -1 | cut -d: -f1)
  if [[ -n $timer_line && -n $first_mutation && $timer_line -lt $first_mutation ]]; then
    pass "case2 timer stopped before the first label mutation"
  else
    fail "case2 timer stopped before the first label mutation" "timer=${timer_line:-none} mutation=${first_mutation:-none}"
  fi

  local number add_line remove_line ordered=1
  for number in 445 453 461; do
    add_line=$(grep -n "^gh POST .*issues/${number}/labels" "${cd}/state/calls.log" | head -1 | cut -d: -f1)
    remove_line=$(grep -n "^gh DELETE .*issues/${number}/labels/tendo%3Av5-autonomous" "${cd}/state/calls.log" | head -1 | cut -d: -f1)
    if [[ -z $add_line || -z $remove_line || $add_line -ge $remove_line ]]; then
      ordered=0
    fi
  done
  assert_eq "case2 add-before-remove per issue" "1" "$ordered"
}

# ---------------------------------------------------------------------------
# case 3: second --apply is a no-op
# ---------------------------------------------------------------------------
case_second_apply() {
  build_case second
  local cd=$CD
  shrink_store "$cd" 3 unit_dir="${CD}/sandbox/home/.config/systemd/user"
  apply_ok "$cd"
  local unit_sha backups_before marker
  unit_sha=$(sha256sum "${unit_dir}/tendo-autonomous.service" | cut -d' ' -f1)
  backups_before=$(backup_dirs "$cd")
  marker=$(wc -l <"${cd}/state/calls.log")

  local lock="${cd}/runtime/tendo-autonomous-${UID_NUM}.lock"
  ( exec 9>"$lock"; flock -n 9; sleep 120 ) &
  local lock_holder=$!
  holders+=("$lock_holder")
  sleep 0.3

  run_as "$cd" --apply
  assert_rc "case3 second apply exits 0" 0
  assert_output_contains "case3 reports already complete" "already complete"
  kill "$lock_holder" 2>/dev/null
  wait "$lock_holder" 2>/dev/null
  assert_eq "case3 no mutating systemctl calls" "0" \
    "$(tail -n +$((marker + 1)) "${cd}/state/calls.log" | grep -cE '^systemctl (--user )?(disable|stop|start|enable|daemon-reload)( |$)')"
  assert_eq "case3 unit file unchanged" "$unit_sha" \
    "$(sha256sum "${unit_dir}/tendo-autonomous.service" | cut -d' ' -f1)"
  assert_eq "case3 no new backup" "$backups_before" "$(backup_dirs "$cd")"
  assert_eq "case3 zero mutating gh calls" "0" \
    "$(tail -n +$((marker + 1)) "${cd}/state/calls.log" | grep -cE '^gh (POST|DELETE)')"
}

# ---------------------------------------------------------------------------
# case 4: --rollback after success restores everything
# ---------------------------------------------------------------------------
case_rollback_after_success() {
  build_case rollback
  local cd=$CD
  local sb="${CD}/sandbox"
  local unit_dir="${sb}/home/.config/systemd/user"
  cp -a "$unit_dir" "${cd}/original-systemd"
  local maint_sha prompt_sha
  shrink_store "$cd" 3
  maint_sha=$(sha256sum "${sb}/home/.local/bin/tendo-maintenance.sh" | cut -d' ' -f1)
  prompt_sha=$(sha256sum "${sb}/home/.config/tendo-maintenance/prompt.md" | cut -d' ' -f1)
  apply_ok "$cd"
  printf 'drifted after the backup\n' >>"${sb}/home/.config/tendo-maintenance/prompt.md"

  run_as "$cd" --rollback
  assert_rc "case4 rollback exits 0" 0

  local diff_output
  if diff_output=$(diff -r "${cd}/original-systemd" "$unit_dir" 2>&1); then
    pass "case4 unit files and drop-ins byte-identical"
  else
    fail "case4 unit files and drop-ins byte-identical" "$diff_output"
  fi
  assert_eq "case4 timer enabled again" "enabled" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.enabled")"
  assert_eq "case4 autonomous enabled again" "enabled" "$(cat "${cd}/state/systemd/tendo-autonomous.service.enabled")"
  assert_eq "case4 maintenance script unchanged" "$maint_sha" \
    "$(sha256sum "${sb}/home/.local/bin/tendo-maintenance.sh" | cut -d' ' -f1)"
  assert_eq "case4 maintenance prompt restored byte-for-byte" "$prompt_sha" \
    "$(sha256sum "${sb}/home/.config/tendo-maintenance/prompt.md" | cut -d' ' -f1)"
  assert_file_lacks "case4 drift line removed" "${sb}/home/.config/tendo-maintenance/prompt.md" "drifted after the backup"
  assert_small_labels "$cd" original 3
  assert_eq "case4 phase rolled-back" "rolled-back" \
    "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["phase"])' "$(cat "${cd}/backups/latest")/state.json")"
}

# ---------------------------------------------------------------------------
# case 5: API failure mid-migration, rollback, then resume
# ---------------------------------------------------------------------------
case_failure_and_resume() {
  build_case failure
  local cd=$CD
  shrink_store "$cd" 5
  local sb="${CD}/sandbox"
  local unit_dir="${sb}/home/.config/systemd/user"

  EXTRA_ENV=("FAKE_GH_FAIL=DELETE:issues/447/labels")
  run_as "$cd" --apply
  EXTRA_ENV=()
  if [[ $RC -ne 0 ]]; then
    pass "case5 apply fails on the injected DELETE"
  else
    fail "case5 apply fails on the injected DELETE" "apply returned 0"
  fi
  assert_output_contains "case5 reports the injected failure" "injected"
  assert_file_contains "case5 unit not replaced yet" "${unit_dir}/tendo-autonomous.service" "autonomous-loop.sh"
  assert_eq "case5 timer already stopped" "disabled" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.enabled")"
  local first_backup
  first_backup=$(cat "${cd}/backups/latest")

  run_as "$cd" --apply
  assert_rc "case5 apply resumes the interrupted backup" 0
  assert_output_contains "case5 reports the resume" "resuming interrupted migration"
  assert_eq "case5 resume reuses the backup" "$first_backup" "$(cat "${cd}/backups/latest")"
  assert_file_contains "case5 unit now new" "${unit_dir}/tendo-autonomous.service" "autonomy-controller.sh"
  assert_small_labels "$cd" migrated 5
  assert_eq "case5 phase complete" "complete" \
    "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["phase"])' "$(cat "${cd}/backups/latest")/state.json")"

  run_as "$cd" --rollback
  assert_rc "case5 rollback after apply exits 0" 0
  assert_small_labels "$cd" original 5

  run_as "$cd" --apply
  assert_rc "case5 fresh apply after rollback exits 0" 0
  assert_eq "case5 fresh apply creates a new backup" "different" \
    "$( [[ "$(cat "${cd}/backups/latest")" != "$first_backup" ]] && echo different || echo same )"
  assert_small_labels "$cd" migrated 5
}

# ---------------------------------------------------------------------------
# case 6: label rollback failure keeps both controllers stopped
# ---------------------------------------------------------------------------
case_rollback_failure() {
  build_case rollbackfail
  local cd=$CD
  apply_ok "$cd"

  EXTRA_ENV=("FAKE_GH_FAIL=POST:issues/459/labels")
  run_as "$cd" --rollback
  EXTRA_ENV=()
  assert_rc "case6 rollback failure exits 6" 6
  assert_output_contains "case6 prints manual commands" \
    "gh issue edit 459 --repo bigtcze/tendo --add-label tendo:v5-autonomous --remove-label release"
  assert_output_contains "case6 states both controllers remain stopped" "both controllers remain stopped"
  assert_eq "case6 service stopped" "inactive" "$(cat "${cd}/state/systemd/tendo-autonomous.service.active")"
  assert_eq "case6 timer still disabled" "disabled" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.enabled")"
}

# ---------------------------------------------------------------------------
# case 7: active work and held state block --check and --apply
# ---------------------------------------------------------------------------
case_active_worker() {
  build_case worker
  local cd=$CD
  local sb="${CD}/sandbox"
  local unit_sha lock
  unit_sha=$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)

  # a real background worker must survive any refusal (nothing is ever killed)
  sleep 300 &
  local worker=$!
  holders+=("$worker")

  printf '1\n' >"${cd}/state/guard-sessions"
  run_as "$cd" --check
  assert_rc "case7 active session: --check refused" 3
  assert_output_contains "case7 prints the guard detail" "Active Tendo sessions: 1"
  run_as "$cd" --apply
  assert_rc "case7 active session: --apply refused" 3
  if kill -0 "$worker" 2>/dev/null; then
    pass "case7 worker process still alive"
  else
    fail "case7 worker process still alive" "the worker was killed"
  fi
  printf '0\n' >"${cd}/state/guard-sessions"

  printf 'True\n' >"${cd}/state/guard-pending"
  run_as "$cd" --apply
  assert_rc "case7 pending marker: --apply refused" 3
  assert_output_contains "case7 prints the pending marker remediation" "tendo-cycle.pending"
  assert_output_contains "case7 prints the archive step" "stale-\$(date +%Y%m%d-%H%M%S)"
  printf 'False\n' >"${cd}/state/guard-pending"

  lock="${cd}/runtime/tendo-autonomous-${UID_NUM}.lock"
  ( exec 9>"$lock"; flock -n 9; sleep 120 ) &
  local lock_holder=$!
  holders+=("$lock_holder")
  sleep 0.3
  run_as "$cd" --check
  assert_rc "case7 held lock: --check refused" 3
  assert_output_contains "case7 names the held lock" "is held"
  kill "$lock_holder" 2>/dev/null
  wait "$lock_holder" 2>/dev/null

  printf 'opencode run --auto --agent orchestrator\n' >"${cd}/state/procs"
  run_as "$cd" --check
  assert_rc "case7 opencode run: --check refused" 3
  assert_output_contains "case7 detects opencode run" "opencode run"
  : >"${cd}/state/procs"

  printf 'active\n' >"${cd}/state/systemd/tendo-maintenance.service.active"
  run_as "$cd" --apply
  assert_rc "case7 maintenance active: --apply refused" 3
  assert_output_contains "case7 names the active maintenance service" "tendo-maintenance.service is active"
  printf 'inactive\n' >"${cd}/state/systemd/tendo-maintenance.service.active"

  assert_eq "case7 unit file untouched" "$unit_sha" \
    "$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)"
  assert_eq "case7 no backup created" "0" "$(backup_dirs "$cd")"
  assert_eq "case7 no label mutation" "0" "$(grep -cE '^gh (POST|DELETE)' "${cd}/state/calls.log")"
}

# ---------------------------------------------------------------------------
# case 8: preflight refusals mutate nothing
# ---------------------------------------------------------------------------
case_refusals() {
  local cd sb unit_sha

  run_refusal() { # name expected-text setup-shell
    local sub=$1 want=$2 setup=$3
    build_case "refusal-${sub}"
    cd=$CD
    sb="${CD}/sandbox"
    eval "$setup"
    unit_sha=$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)
    run_as "$cd" --apply
    assert_rc "case8 ${sub}: refused" 3
    assert_output_contains "case8 ${sub}: reason" "$want"
    assert_eq "case8 ${sub}: unit untouched" "$unit_sha" \
      "$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)"
    assert_eq "case8 ${sub}: no backup" "0" "$(backup_dirs "$cd")"
    assert_eq "case8 ${sub}: no mutation" "0" \
      "$(grep -cE '^gh (POST|DELETE)|^systemctl (--user )?(disable|stop|start|enable|daemon-reload)( |$)' "${cd}/state/calls.log")"
  }

  # The setup snippets are single-quoted so they expand (via eval) only after
  # run_refusal has set $sb/$cd for the freshly built sandbox.
  run_refusal dirty "uncommitted changes" 'printf "x\n" >>"${sb}/repo/README.md"'
  run_refusal branch "expected main" 'git -C "${sb}/repo" checkout -q -b feature'
  run_refusal head "differs from origin" 'git -C "${sb}/repo" commit -q --allow-empty -m "local only"'
  run_refusal ci-red "required CI not green" 'python3 - "${cd}/state/github.json" <<PY
import json,sys
p=sys.argv[1]; d=json.load(open(p))
for runs in d["check_runs"].values():
    for run in runs: run["conclusion"]="failure"
json.dump(d,open(p,"w"))
PY'
  run_refusal ci-missing "required CI not green" 'python3 - "${cd}/state/github.json" <<PY
import json,sys
p=sys.argv[1]; d=json.load(open(p)); d["check_runs"]={}
json.dump(d,open(p,"w"))
PY'
  run_refusal linger "linger is not enabled" 'printf "Linger=no\n" >"${cd}/state/linger"'
  run_refusal unknown-dropin "not verified safe to remove" 'printf "[Service]\nExecStartPre=/bin/true\n" >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/50-extra.conf"' 

  run_refusal unknown-key "not verified safe to remove" \
    'printf "[Service]\nEnvironment=OPENCODE_BIN=/x\nExecStartPre=/bin/true\n" >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/10-attach.conf"'

  # A newer green required run wins over an older red one (latest by started_at).
  build_case "ci-latest"
  cd=$CD
  sb="${CD}/sandbox"
  python3 - "${cd}/state/github.json" <<'PATCH'
import json
import sys

path = sys.argv[1]
store = json.load(open(path, encoding="utf-8"))
sha = next(iter(store["check_runs"]))
store["check_runs"][sha] = [
    {"name": "required", "status": "completed", "conclusion": "failure",
     "started_at": "2026-01-01T00:00:00Z"},
    {"name": "required", "status": "completed", "conclusion": "success",
     "started_at": "2026-01-02T00:00:00Z"},
]
json.dump(store, open(path, "w", encoding="utf-8"))
PATCH
  run_as "$cd" --check
  assert_rc "case8 latest green check run wins over an older red one" 0
}

# ---------------------------------------------------------------------------
# case 9: rollback is refused while a task runs
# ---------------------------------------------------------------------------
case_rollback_refused() {
  build_case rollbackrefused
  local cd=$CD
  apply_ok "$cd"
  local unit_sha
  unit_sha=$(sha256sum "${cd}/sandbox/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)

  printf '1\n' >"${cd}/state/guard-sessions"
  run_as "$cd" --rollback
  assert_rc "case9 rollback refused while a task runs" 5
  assert_output_contains "case9 asks to wait for the cycle" "wait for the cycle to finish"
  assert_eq "case9 unit unchanged" "$unit_sha" \
    "$(sha256sum "${cd}/sandbox/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)"
  assert_eq "case9 labels still migrated" "release,status:idle" \
    "$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(",".join(sorted(l["name"] for l in d["issues"]["450"]["labels"])))' "${cd}/state/github.json")"
}

# ---------------------------------------------------------------------------
# case 10: single scheduler and no Restart=no in the effective config
# ---------------------------------------------------------------------------
case_no_second_scheduler() {
  build_case scheduler
  local cd=$CD unit_dir="${CD}/sandbox/home/.config/systemd/user"
  shrink_store "$cd" 3
  apply_ok "$cd"

  local unit="${unit_dir}/tendo-autonomous.service"
  assert_file_contains "case10 Restart=on-failure" "$unit" "Restart=on-failure"
  assert_file_contains "case10 KillMode=control-group" "$unit" "KillMode=control-group"
  assert_file_contains "case10 TimeoutStopSec=120" "$unit" "TimeoutStopSec=120"
  assert_file_contains "case10 IDLE_POLL_SECONDS=300" "$unit" "IDLE_POLL_SECONDS=300"
  assert_file_lacks "case10 no Restart=no in the unit" "$unit" "Restart=no"

  local effective
  effective=$(cat "$unit" "${unit_dir}"/tendo-autonomous.service.d/*.conf)
  case $effective in
    *"Restart=no"*) fail "case10 no Restart=no in the effective config" "found Restart=no" ;;
    *) pass "case10 no Restart=no in the effective config" ;;
  esac

  assert_eq "case10 timer not enabled" "disabled" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.enabled")"
  assert_eq "case10 timer inactive" "inactive" "$(cat "${cd}/state/systemd/tendo-maintenance.timer.active")"
  assert_eq "case10 exactly one controller" "1" "$(grep -cx 'scripts/autonomy-controller.sh' "${cd}/state/procs")"
  assert_eq "case10 no legacy loop process" "0" "$(grep -c 'autonomous-loop' "${cd}/state/procs")"
}

# ---------------------------------------------------------------------------
# case 11: redaction of every systemd Environment form
# ---------------------------------------------------------------------------
case_redaction_forms() {
  build_case redaction
  local cd=$CD
  local sb="${cd}/sandbox"
  local unit="${sb}/home/.config/systemd/user/tendo-autonomous.service"
  cat >>"$unit" <<'EOF'
Environment="GITHUB_TOKEN=quoted-secret-value"
Environment=PATH=/bin GITHUB_TOKEN=multi-secret-value
Environment='A=1' "API_KEY=space secret value"
EnvironmentFile=/etc/tendo/secret.env
Environment="BROKEN_TOKEN=unterminated-secret
EOF
  run_as "$cd" --check
  assert_rc "case11 redaction check exits 0" 0
  assert_output_lacks "case11 quoted secret never printed" "quoted-secret-value"
  assert_output_lacks "case11 multi-assignment secret never printed" "multi-secret-value"
  assert_output_lacks "case11 space-quoted secret never printed" "space secret value"
  assert_output_lacks "case11 single-assignment secret never printed" "unit-secret-value"
  assert_output_contains "case11 quoted variable redacted" 'Environment=GITHUB_TOKEN=<redacted>'
  assert_output_contains "case11 multi-assignment variable redacted" 'GITHUB_TOKEN=<redacted>'
  assert_output_contains "case11 second variable of a line redacted" 'API_KEY=<redacted>'
  assert_output_contains "case11 non-sensitive value kept" 'A=1'
  assert_output_contains "case11 EnvironmentFile path kept" 'EnvironmentFile=/etc/tendo/secret.env'
  assert_output_lacks "case11 unparseable value never printed" "unterminated-secret"
  assert_output_contains "case11 unparseable Environment redacted" "Environment=<redacted>"

  # Rejected drop-in: only file and key names, never values.
  build_case redaction-dropin
  cd=$CD
  sb="${cd}/sandbox"
  printf '[Service]\nEnvironment=OPENCODE_BIN=/bin/true GITHUB_TOKEN=dropin-secret-value\n' \
    >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/10-attach.conf"
  run_as "$cd" --check
  assert_rc "case11 rejected drop-in exits 3" 3
  assert_output_lacks "case11 rejected drop-in secret never printed" "dropin-secret-value"
  assert_output_lacks "case11 rejected drop-in values never printed" "/bin/true"
  assert_output_contains "case11 rejection names the file" "10-attach.conf"
  assert_output_contains "case11 rejection names the key" "GITHUB_TOKEN"
}

# ---------------------------------------------------------------------------
# case 12: rollback fails closed on guard uncertainty and on a late task
# ---------------------------------------------------------------------------
case_rollback_fail_closed() {
  build_case rollback-guard
  local cd=$CD
  local sb="${cd}/sandbox"
  shrink_store "$cd" 3
  apply_ok "$cd"
  local unit_sha
  unit_sha=$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)
  local marker
  marker=$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")

  local mode
  for mode in fail-exit2 garbage sessions-garbage; do
    printf '%s\n' "$mode" >"${cd}/state/guard-mode"
    run_as "$cd" --rollback
    assert_rc "case12 ${mode}: rollback refuses" 5
    assert_eq "case12 ${mode}: unit unchanged" "$unit_sha" \
      "$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)"
    assert_small_labels "$cd" migrated 3
  done
  rm -f "${cd}/state/guard-mode"
  assert_eq "case12 guard failures made no label mutation" "$marker" \
    "$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")"

  # A task appears between the idle check and the stop: re-check after locking.
  EXTRA_ENV=("FAKE_STOP_HOOK=busy")
  run_as "$cd" --rollback
  EXTRA_ENV=()
  assert_rc "case12 late task: rollback aborts" 5
  assert_output_contains "case12 late task: says nothing changed" "no label"
  assert_eq "case12 late task: service stays stopped" "inactive" \
    "$(cat "${cd}/state/systemd/tendo-autonomous.service.active")"
  assert_eq "case12 late task: timer stays disabled" "disabled" \
    "$(cat "${cd}/state/systemd/tendo-maintenance.timer.enabled")"
  assert_small_labels "$cd" migrated 3
  assert_eq "case12 late task made no label mutation" "$marker" \
    "$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")"
}

# ---------------------------------------------------------------------------
# case 13: an interruption at every persisted phase is resumable
# ---------------------------------------------------------------------------
interruption_flow() { # name expected-phase [env...]
  local job=$1 expected=$2
  shift 2
  build_case "interrupt-${job}"
  local cd=$CD
  local sb="${cd}/sandbox"
  local unit_dir="${sb}/home/.config/systemd/user"
  shrink_store "$cd" 3
  cp -a "$unit_dir" "${cd}/original-systemd"

  EXTRA_ENV=("$@")
  run_as "$cd" --apply
  EXTRA_ENV=()
  if [[ $RC -ne 0 ]]; then
    pass "case13 ${job}: apply interrupted"
  else
    fail "case13 ${job}: apply interrupted" "apply returned 0"
  fi
  local backup phase
  backup=$(cat "${cd}/backups/latest")
  phase=$(phase_of "$backup")
  assert_eq "case13 ${job}: persisted phase" "$expected" "$phase"
  assert_eq "case13 ${job}: one backup before resume" "1" "$(backup_dirs "$cd")"

  run_as "$cd" --apply
  assert_rc "case13 ${job}: resume completes" 0
  assert_eq "case13 ${job}: latest unchanged" "$backup" "$(cat "${cd}/backups/latest")"
  assert_eq "case13 ${job}: no extra backup" "1" "$(backup_dirs "$cd")"
  assert_eq "case13 ${job}: phase complete" "complete" "$(phase_of "$backup")"
  assert_small_labels "$cd" migrated 3

  run_as "$cd" --rollback
  assert_rc "case13 ${job}: rollback exits 0" 0
  local diff_output
  if diff_output=$(diff -r "${cd}/original-systemd" "$unit_dir" 2>&1); then
    pass "case13 ${job}: units restored byte-exact"
  else
    fail "case13 ${job}: units restored byte-exact" "$diff_output"
  fi
  assert_small_labels "$cd" original 3
}

case_interruption_phases() {
  interruption_flow backed-up backed-up "FAKE_GH_FAIL=GET:issues?state=all"
  interruption_flow quiesced quiesced "FAKE_GH_FAIL=DELETE:issues/446/labels"
  interruption_flow labels-migrated labels-migrated "FAKE_SYSTEMCTL_FAIL=daemon-reload:*"
  interruption_flow units-replaced units-replaced "FAKE_SYSTEMCTL_FAIL=start:tendo-autonomous.service"

  # units-running is persisted after a verified start; simulate the crash there.
  build_case interrupt-units-running
  local cd=$CD
  local sb="${cd}/sandbox"
  local unit_dir="${sb}/home/.config/systemd/user"
  shrink_store "$cd" 3
  cp -a "$unit_dir" "${cd}/original-systemd"
  apply_ok "$cd"
  local backup
  backup=$(cat "${cd}/backups/latest")
  python3 - "$backup/state.json" <<'PY'
import json
import sys

path = sys.argv[1]
state = json.load(open(path, encoding="utf-8"))
state["phase"] = "units-running"
json.dump(state, open(path, "w", encoding="utf-8"), indent=2, sort_keys=True)
PY
  run_as "$cd" --apply
  assert_rc "case13 units-running: resume completes" 0
  assert_eq "case13 units-running: latest unchanged" "$backup" "$(cat "${cd}/backups/latest")"
  assert_eq "case13 units-running: no extra backup" "1" "$(backup_dirs "$cd")"
  assert_eq "case13 units-running: phase complete" "complete" "$(phase_of "$backup")"

  run_as "$cd" --rollback
  assert_rc "case13 units-running: rollback exits 0" 0
  if diff -r "${cd}/original-systemd" "$unit_dir" >/dev/null 2>&1; then
    pass "case13 units-running: units restored byte-exact"
  else
    fail "case13 units-running: units restored byte-exact" "unit tree differs"
  fi
  assert_small_labels "$cd" original 3

  # A completed transaction whose host no longer matches must never be
  # replaced by a new backup (that would lose the rollback baseline).
  build_case interrupt-complete-mismatch
  cd=$CD
  sb="${cd}/sandbox"
  unit_dir="${sb}/home/.config/systemd/user"
  shrink_store "$cd" 3
  cp -a "$unit_dir" "${cd}/original-systemd"
  apply_ok "$cd"
  backup=$(cat "${cd}/backups/latest")
  cp -a "${cd}/original-systemd/tendo-autonomous.service" "$unit_dir/tendo-autonomous.service"
  printf 'inactive\n' >"${cd}/state/systemd/tendo-autonomous.service.active"
  : >"${cd}/state/procs"
  run_as "$cd" --apply
  assert_rc "case13 complete-mismatch: refuses instead of overwriting" 3
  assert_output_contains "case13 complete-mismatch: reports the drifted ExecStart" "ExecStart: expected"
  assert_eq "case13 complete-mismatch: latest unchanged" "$backup" "$(cat "${cd}/backups/latest")"
  assert_eq "case13 complete-mismatch: no extra backup" "1" "$(backup_dirs "$cd")"
}

# ---------------------------------------------------------------------------
# case 14: concurrent installer invocations are serialized
# ---------------------------------------------------------------------------
case_installer_lock() {
  build_case lock
  local cd=$CD
  local sb="${cd}/sandbox"
  shrink_store "$cd" 3
  local unit_sha
  unit_sha=$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)

  EXTRA_ENV=("FAKE_SYSTEMCTL_BARRIER=stop")
  ( run_as "$cd" --apply; printf '%s\n' "$RC" >"${cd}/first.rc" ) &
  local first_pid=$!
  holders+=("$first_pid")
  local i
  for i in $(seq 1 200); do
    [[ -e "${cd}/state/barrier.ready" ]] && break
    sleep 0.1
  done
  if [[ -e "${cd}/state/barrier.ready" ]]; then
    pass "case14 first apply holds the transaction lock"
  else
    fail "case14 first apply holds the transaction lock" "first apply never reached the barrier"
  fi
  local marker
  marker=$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")

  EXTRA_ENV=()
  run_as "$cd" --check
  assert_rc "case14 --check refuses while apply runs" 75
  assert_output_contains "case14 --check busy message" "another installer run"
  run_as "$cd" --apply
  assert_rc "case14 second apply refuses" 75
  assert_output_contains "case14 second apply busy message" "another installer run"
  assert_eq "case14 second apply made no label mutation" "$marker" \
    "$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")"
  assert_eq "case14 second apply left the unit untouched" "$unit_sha" \
    "$(sha256sum "${sb}/home/.config/systemd/user/tendo-autonomous.service" | cut -d' ' -f1)"

  : >"${cd}/state/barrier.release"
  wait "$first_pid" 2>/dev/null
  assert_eq "case14 first apply completes" "0" "$(cat "${cd}/first.rc")"
  assert_small_labels "$cd" migrated 3
}

# ---------------------------------------------------------------------------
# case 15: systemctl/pgrep command failures fail closed
# ---------------------------------------------------------------------------
case_command_failures() {
  build_case cmdfail
  local cd=$CD
  local sb="${cd}/sandbox"
  shrink_store "$cd" 3

  EXTRA_ENV=("FAKE_SYSTEMCTL_FAIL=is-active:tendo-autonomous.service")
  run_as "$cd" --apply
  EXTRA_ENV=()
  assert_rc "case15 is-active failure refuses" 3
  assert_output_contains "case15 names the failing state query" "cannot determine"
  assert_eq "case15 no backup created" "0" "$(backup_dirs "$cd")"

  EXTRA_ENV=("FAKE_SYSTEMCTL_FAIL=is-active:tendo-autonomous.service" "FAKE_SYSTEMCTL_FAIL_OUTPUT=garbage")
  run_as "$cd" --apply
  EXTRA_ENV=()
  assert_rc "case15 unparseable is-active output refuses" 3
  assert_output_contains "case15 reports an unknown state" "unknown"

  EXTRA_ENV=("FAKE_SYSTEMCTL_FAIL=is-enabled:tendo-maintenance.timer")
  run_as "$cd" --apply
  EXTRA_ENV=()
  assert_rc "case15 is-enabled failure refuses" 3
  assert_output_contains "case15 names the failing enabled query" "cannot determine is-enabled"

  EXTRA_ENV=("FAKE_PGREP_FAIL_RC=2")
  run_as "$cd" --apply
  EXTRA_ENV=()
  assert_rc "case15 pgrep failure refuses" 3
  assert_output_contains "case15 names pgrep" "pgrep"
  assert_eq "case15 pgrep failure created no backup" "0" "$(backup_dirs "$cd")"

  apply_ok "$cd"
  local marker
  marker=$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")
  EXTRA_ENV=("FAKE_SYSTEMCTL_FAIL=is-active:tendo-autonomous.service")
  run_as "$cd" --rollback
  EXTRA_ENV=()
  if [[ $RC -ne 0 ]]; then
    pass "case15 rollback state failure aborts"
  else
    fail "case15 rollback state failure aborts" "rollback returned 0"
  fi
  assert_small_labels "$cd" migrated 3
  assert_eq "case15 rollback state failure made no label mutation" "$marker" \
    "$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")"
}

# ---------------------------------------------------------------------------
# case 16: secrets on systemd continuation lines are never printed
# ---------------------------------------------------------------------------
case_multiline_redaction() {
  build_case multiline
  local cd=$CD
  local sb="${cd}/sandbox"
  local unit="${sb}/home/.config/systemd/user/tendo-autonomous.service"
  cat >>"$unit" <<'EOF'
Environment="GITHUB_TOKEN=continuation-first\
  continuation-secret-value"
Environment=PATH=/bin \
  GITHUB_TOKEN=multi-continuation-secret
EOF
  run_as "$cd" --check
  assert_rc "case16 unit continuation check exits 0" 0
  assert_output_lacks "case16 quoted continuation secret never printed" "continuation-secret-value"
  assert_output_lacks "case16 multi-assignment continuation secret never printed" "multi-continuation-secret"
  assert_output_contains "case16 quoted continuation variable redacted" 'Environment=GITHUB_TOKEN=<redacted>'
  assert_output_contains "case16 multi-assignment redaction keeps PATH" 'PATH=/bin'
  assert_output_contains "case16 multi-assignment variable redacted" 'GITHUB_TOKEN=<redacted>'

  # Comments inside a continuation are dropped by systemd; the continuation
  # stays pending, so the secret on the following line must still be redacted.
  build_case multiline-comment
  cd=$CD
  sb="${cd}/sandbox"
  unit="${sb}/home/.config/systemd/user/tendo-autonomous.service"
  cat >>"$unit" <<'EOF'
Environment="GITHUB_TOKEN=hash-first\
# hash comment inside the continuation
  hash-continuation-secret"
Environment="GITHUB_TOKEN=semicolon-first\
; semicolon comment inside the continuation
  semicolon-continuation-secret"
EOF
  run_as "$cd" --check
  assert_rc "case16 comment continuation check exits 0" 0
  assert_output_lacks "case16 hash-comment continuation secret never printed" "hash-continuation-secret"
  assert_output_lacks "case16 semicolon-comment continuation secret never printed" "semicolon-continuation-secret"
  assert_output_contains "case16 comment continuation variable redacted" 'GITHUB_TOKEN=<redacted>'

  # Accepted path: a continued drop-in with only allowed keys passes.
  build_case multiline-dropin-ok
  cd=$CD
  sb="${cd}/sandbox"
  printf '[Service]\nEnvironment=OPENCODE_BIN=/x \\\n  SUCCESS_SLEEP=300\n' \
    >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/10-attach.conf"
  run_as "$cd" --check
  assert_rc "case16 accepted continued drop-in passes" 0
  assert_output_lacks "case16 accepted continued drop-in values not printed" "/x"

  # Rejected path: the secret is on the continuation line of a drop-in.
  build_case multiline-dropin-bad
  cd=$CD
  sb="${cd}/sandbox"
  printf '[Service]\nEnvironment=OPENCODE_BIN=/x \\\n  GITHUB_TOKEN=dropin-continuation-secret\n' \
    >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/10-attach.conf"
  run_as "$cd" --check
  assert_rc "case16 rejected continued drop-in exits 3" 3
  assert_output_lacks "case16 rejected continuation secret never printed" "dropin-continuation-secret"
  assert_output_lacks "case16 rejected continuation value never printed" "/x"
  assert_output_contains "case16 rejection names the key" "GITHUB_TOKEN"

  # Comments inside a continuation of a rejected drop-in must not expose the
  # values on either side of the comment.
  build_case multiline-dropin-comment-ok
  cd=$CD
  sb="${cd}/sandbox"
  printf '[Service]\nEnvironment=OPENCODE_BIN=/x \\\n# note\n  SUCCESS_SLEEP=300\n' \
    >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/10-attach.conf"
  run_as "$cd" --check
  assert_rc "case16 accepted drop-in with comment passes" 0
  assert_output_lacks "case16 accepted drop-in comment values not printed" "/x"

  build_case multiline-dropin-comment-bad
  cd=$CD
  sb="${cd}/sandbox"
  printf '[Service]\nEnvironment=OPENCODE_BIN=/x \\\n; note\n  GITHUB_TOKEN=comment-dropin-secret\n' \
    >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/10-attach.conf"
  run_as "$cd" --check
  assert_rc "case16 rejected drop-in with comment exits 3" 3
  assert_output_lacks "case16 comment drop-in secret never printed" "comment-dropin-secret"
  assert_output_lacks "case16 comment drop-in value never printed" "/x"
  assert_output_contains "case16 comment drop-in names the key" "GITHUB_TOKEN"

  # A standalone ';' comment in a legacy drop-in is a comment, not a key.
  build_case dropin-semicolon-comment
  cd=$CD
  sb="${cd}/sandbox"
  printf '[Service]\n; standalone comment\nEnvironment=SUCCESS_SLEEP=300\n' \
    >"${sb}/home/.config/systemd/user/tendo-autonomous.service.d/30-cycle-spacing.conf"
  run_as "$cd" --check
  assert_rc "case16 standalone semicolon comment is ignored" 0
}

# ---------------------------------------------------------------------------
# case 17: already-complete requires the full expected effective configuration
# ---------------------------------------------------------------------------
case_effective_config_drift() {
  build_case configdrift
  local cd=$CD
  local sb="${cd}/sandbox"
  local unit_dir="${sb}/home/.config/systemd/user"
  shrink_store "$cd" 3
  apply_ok "$cd"
  local backup good
  backup=$(cat "${cd}/backups/latest")
  good="${cd}/good-config"
  cp -a "$unit_dir" "$good"

  drift_expect() { # label expected-message edit
    local label=$1 expected=$2 edit=$3 unit_sha gh_marker
    rm -rf "$unit_dir"
    cp -a "$good" "$unit_dir"
    eval "$edit"
    unit_sha=$(sha256sum "${unit_dir}/tendo-autonomous.service" | cut -d' ' -f1)
    gh_marker=$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")
    run_as "$cd" --apply
    assert_rc "case17 ${label}: refused" 3
    assert_output_contains "case17 ${label}: per-key message" "$expected"
    assert_eq "case17 ${label}: latest unchanged" "$backup" "$(cat "${cd}/backups/latest")"
    assert_eq "case17 ${label}: no new backup" "1" "$(backup_dirs "$cd")"
    assert_eq "case17 ${label}: installer did not rewrite the unit" "$unit_sha" \
      "$(sha256sum "${unit_dir}/tendo-autonomous.service" | cut -d' ' -f1)"
    assert_eq "case17 ${label}: no label mutation" "$gh_marker" \
      "$(grep -c '^gh \(POST\|DELETE\)' "${cd}/state/calls.log")"
  }

  drift_expect restart "Restart: expected on-failure" \
    "sed -i 's/^Restart=on-failure/Restart=always/' \"${unit_dir}/tendo-autonomous.service\""
  drift_expect killmode "KillMode: expected control-group" \
    "sed -i 's/^KillMode=control-group/KillMode=mixed/' \"${unit_dir}/tendo-autonomous.service\""
  drift_expect opencode-bin "Environment: OPENCODE_BIN expected" \
    "sed -i 's#^Environment=OPENCODE_BIN=.*#Environment=OPENCODE_BIN=/usr/bin/false#' \"${unit_dir}/tendo-autonomous.service\""
  drift_expect execstart "ExecStart: expected ${sb}/repo/scripts/autonomy-controller.sh" \
    "sed -i 's#^ExecStart=.*#ExecStart=${sb}/repo/scripts/autonomous-loop.sh#' \"${unit_dir}/tendo-autonomous.service\""
  drift_expect timeout "TimeoutStopSec: expected 120s" \
    "sed -i 's/^TimeoutStopSec=120/TimeoutStopSec=90/' \"${unit_dir}/tendo-autonomous.service\""
  drift_expect restart-no "Restart=no is not allowed" \
    "printf '[Service]\\nRestart=no\\n' >>\"${unit_dir}/tendo-autonomous.service.d/40-opencode-dependency.conf\""
  drift_expect legacy-dropin "legacy drop-in still active" \
    "printf '[Service]\\nEnvironment=SUCCESS_SLEEP=300\\n' >\"${unit_dir}/tendo-autonomous.service.d/10-attach.conf\""

  # --check reports drift too, without mutation.
  rm -rf "$unit_dir"
  cp -a "$good" "$unit_dir"
  sed -i 's/^Restart=on-failure/Restart=always/' "${unit_dir}/tendo-autonomous.service"
  run_as "$cd" --check
  assert_rc "case17 --check refuses on drift" 3
  assert_output_contains "case17 --check reports the drifted key" "Restart: expected on-failure"

  # A healthy completed install remains idempotent.
  rm -rf "$unit_dir"
  cp -a "$good" "$unit_dir"
  run_as "$cd" --apply
  assert_rc "case17 healthy completed install remains idempotent" 0
  assert_output_contains "case17 reports already complete" "already complete"
}

# ---------------------------------------------------------------------------
# run all cases
# ---------------------------------------------------------------------------
build_sandbox "$TEMPLATE"

case_check_dry_run
case_apply_success
case_second_apply
case_rollback_after_success
case_failure_and_resume
case_rollback_failure
case_active_worker
case_refusals
case_rollback_refused
case_no_second_scheduler
case_redaction_forms
case_rollback_fail_closed
case_interruption_phases
case_installer_lock
case_command_failures
case_multiline_redaction
case_effective_config_drift

printf '\n'
if ((failures == 0)); then
  printf 'all installer tests passed\n'
  exit 0
fi
printf '%d installer test(s) failed\n' "$failures" >&2
exit 1
