#!/usr/bin/env bash
# One-time host cutover to the evergreen autonomy controller.
#
# Replaces the V5 feature loop (scripts/autonomous-loop.sh) in
# tendo-autonomous.service with scripts/autonomy-controller.sh, retires the
# three legacy environment drop-ins, disables the redundant daily maintenance
# timer (the controller now owns maintenance, dependencies and releases) and
# migrates the backlog from `tendo:v5-autonomous` to the version-neutral
# `release` ownership label. Everything is backed up first and reversible.
#
# Usage: install-autonomy-service.sh --check | --apply | --rollback [--backup DIR] | --help
#
#   --check     preflight + full redacted plan, no mutation (exit 3 if refused)
#   --apply     transactional cutover (steps 1..7); resume-aware and idempotent
#   --rollback  restore units, drop-ins, timer state and ownership labels
#
# Test-only overrides: TENDO_ROOT, AUTONOMY_UNIT_DIR, AUTONOMY_BACKUP_ROOT,
# AUTONOMY_GUARD, AUTONOMY_MAINT_SCRIPT, AUTONOMY_MAINT_PROMPT, XDG_RUNTIME_DIR,
# AUTONOMY_REPO, AUTONOMY_SELECTOR, AUTONOMY_START_TIMEOUT (and PATH for fakes).
# Nothing here ever kills a process, edits ~/.config/tendo-maintenance/prompt.md
# or prints secret-bearing Environment values (all Environment assignments are
# parsed, including quoted and multi-assignment forms).
#
# Concurrency: --apply and --rollback hold an exclusive transaction flock
# ($AUTONOMY_BACKUP_ROOT/.install.lock) from before inspection until completion;
# --check takes it shared so a dry run can coexist with another dry run but
# refuses (exit 75) while a real installation is running.
set -Eeuo pipefail
umask 077

TENDO_ROOT=${TENDO_ROOT:-/home/tomas/repositories/tendo}
UNIT_DIR=${AUTONOMY_UNIT_DIR:-${HOME}/.config/systemd/user}
BACKUP_ROOT=${AUTONOMY_BACKUP_ROOT:-${HOME}/.local/state/tendo-autonomy-migration}
GUARD=${AUTONOMY_GUARD:-${HOME}/.local/bin/opencode-tendo-safe}
MAINT_SCRIPT=${AUTONOMY_MAINT_SCRIPT:-${HOME}/.local/bin/tendo-maintenance.sh}
MAINT_PROMPT=${AUTONOMY_MAINT_PROMPT:-${HOME}/.config/tendo-maintenance/prompt.md}
RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/${UID}}
REPO=${AUTONOMY_REPO:-bigtcze/tendo}
SELECTOR=${AUTONOMY_SELECTOR:-${TENDO_ROOT}/scripts/autonomy-work-selection.py}
START_TIMEOUT=${AUTONOMY_START_TIMEOUT:-30}

AUTONOMOUS_UNIT=tendo-autonomous.service
MAINT_SERVICE=tendo-maintenance.service
MAINT_TIMER=tendo-maintenance.timer
AUTONOMOUS_DROPINS="10-attach.conf 20-safe.conf 30-cycle-spacing.conf"
KEEP_DROPIN=40-opencode-dependency.conf

UID_NUM=${UID:-$(id -u)}
LOCK_AUTONOMOUS="${RUNTIME_DIR}/tendo-autonomous-${UID_NUM}.lock"
LOCK_MAINT="${RUNTIME_DIR}/tendo-maintenance-${UID_NUM}.lock"
LOCK_INSTALL="${BACKUP_ROOT}/.install.lock"
MIGRATION="${TENDO_ROOT}/scripts/autonomy_migration.py"

HEAD_SHA=""
ALREADY_MIGRATED=0
BACKUP=""
LOCKS_HELD=0
GUARD_OUT=""
GUARD_RC=0
GUARD_ERR=""

log() { printf '[autonomy-install] %s\n' "$*"; }
logerr() { printf '[autonomy-install] %s\n' "$*" >&2; }
usage() {
  sed -n '2,26p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}
usage_error() { logerr "usage error: $*"; usage >&2; exit 2; }
refuse() { logerr "REFUSED: $*"; exit 3; }
fail() { logerr "ERROR: $*"; exit 2; }

# ---------------------------------------------------------------------------
# redaction (never print secret-bearing Environment values)
# ---------------------------------------------------------------------------
# systemd joins lines ending in an odd number of backslashes with the next line
# (the backslash becomes a space). Redaction, validation and display therefore
# operate on logical lines, never on physical lines, so a secret in a
# continuation line can never be printed verbatim. Environment= values are
# parsed with systemd quoting rules (single/double quotes, escapes, several
# assignments per logical line); unparseable values are redacted wholesale.
unit_text_tool() {
  python3 - "$@" <<'PY'
import os
import re
import sys

SENSITIVE = re.compile(r"TOKEN|KEY|SECRET|PASSWORD|AUTH", re.IGNORECASE)
ALLOWED_ENV = {"OPENCODE_BIN", "SUCCESS_SLEEP"}
LEGACY_DROPINS = {"10-attach.conf", "20-safe.conf", "30-cycle-spacing.conf"}


def read_lines(path):
    if path == "-":
        return sys.stdin.read().splitlines()
    with open(path, encoding="utf-8", errors="replace") as handle:
        return handle.read().splitlines()


def join_continuations(lines):
    logical = []
    buffer = None
    for raw in lines:
        line = raw if buffer is None else buffer + " " + raw.lstrip()
        buffer = None
        trailing = len(line) - len(line.rstrip("\\"))
        if trailing % 2 == 1:
            buffer = line[:-1]
            continue
        logical.append(line)
    if buffer is not None:
        logical.append(buffer)
    return logical


def parse_assignments(value):
    spans = []
    index, length = 0, len(value)
    while index < length:
        while index < length and value[index].isspace():
            index += 1
        if index >= length:
            break
        start = index
        quote = None
        while index < length:
            char = value[index]
            if quote:
                if quote == '"' and char == "\\":
                    index += 2
                    continue
                if char == quote:
                    quote = None
                index += 1
                continue
            if char in ("'", '"'):
                quote = char
                index += 1
                continue
            if char.isspace():
                break
            index += 1
        if quote is not None:
            return None
        token_end = index
        token = value[start:token_end]
        equals = token.find("=")
        if equals <= 0:
            spans.append((start, token_end, None, None))
            continue
        name = token[:equals].strip().strip("'\"")
        spans.append((start, token_end, name, start + equals + 1))
    return spans


def unquote(raw):
    if len(raw) >= 2 and raw[0] == raw[-1] and raw[0] in "'\"":
        inner = raw[1:-1]
        if raw[0] == "'":
            return inner
        out = []
        index = 0
        while index < len(inner):
            if inner[index] == "\\" and index + 1 < len(inner):
                out.append(inner[index + 1])
                index += 2
                continue
            out.append(inner[index])
            index += 1
        return "".join(out)
    return raw


def env_pairs(value):
    spans = parse_assignments(value)
    if spans is None:
        return None
    pairs = {}
    for start, end, name, value_start in spans:
        if name is None or value_start is None:
            continue
        pairs[name] = unquote(value[value_start:end])
    return pairs


def env_keys(value):
    spans = parse_assignments(value)
    if spans is None:
        return None
    names = [span[2] for span in spans if span[2] is not None]
    if len(names) != len(spans):
        return None
    return names


def redact(line):
    match = re.match(r"^\s*([A-Za-z0-9_]+)\s*=", line)
    if not match:
        return line
    if match.group(1).upper() != "ENVIRONMENT":
        # EnvironmentFile= is a path, never a secret value.
        return line
    prefix, sep, value = line.partition("=")
    if not sep:
        return line
    spans = parse_assignments(value)
    if spans is None or (value.strip() and not any(span[2] for span in spans)):
        return prefix + "=<redacted>"
    out = []
    previous = 0
    for start, end, name, value_start in spans:
        if name is None or value_start is None or not SENSITIVE.search(name):
            continue
        out.append(value[previous:start])
        out.append(name + "=<redacted>")
        previous = end
    out.append(value[previous:])
    return prefix + "=" + "".join(out)


def dropin_bad_keys(path):
    bad = []
    for line in join_continuations(read_lines(path)):
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or (
            stripped.startswith("[") and stripped.endswith("]")
        ):
            continue
        key, sep, value = stripped.partition("=")
        key, value = key.strip(), value.strip()
        if not sep:
            bad.append(key or "<line>")
            continue
        if key == "Environment":
            names = env_keys(value)
            if names is None:
                bad.append("Environment")
                continue
            for name in names:
                if name not in ALLOWED_ENV:
                    bad.append(f"Environment:{name}")
        elif key == "Restart" and value == "no":
            continue
        else:
            bad.append(key)
    seen = []
    for item in bad:
        if item not in seen:
            seen.append(item)
    if seen:
        print(", ".join(seen))
        raise SystemExit(1)
    print("")
    raise SystemExit(0)


def seconds(raw):
    raw = raw.strip()
    if not raw:
        return None
    if raw.isdigit():
        return float(raw)
    total = 0.0
    found = False
    for number, unit in re.findall(r"(\d+(?:\.\d+)?)\s*(us|ms|s|min|h|d)", raw):
        total += float(number) * {
            "us": 1e-6, "ms": 1e-3, "s": 1.0,
            "min": 60.0, "h": 3600.0, "d": 86400.0,
        }[unit]
        found = True
    return total if found else None


def check_effective(args):
    root, guard, fragment, dropin_dir, show_path = args
    props = {}
    for raw in read_lines(show_path):
        key, sep, value = raw.partition("=")
        if sep:
            props[key.strip()] = value
    problems = []

    expected_exec = os.path.join(root, "scripts", "autonomy-controller.sh")
    exec_start = props.get("ExecStart", "")
    argv = []
    match = re.search(r"argv\[\]=([^;]*)", exec_start)
    if match:
        argv = match.group(1).strip().split()
    if not argv:
        match = re.search(r"path=([^ ;]+)", exec_start)
        if match:
            argv = [match.group(1)]
    if argv != [expected_exec]:
        found = " ".join(argv) or exec_start or "<empty>"
        problems.append(f"ExecStart: expected {expected_exec}, found {found}")

    restart = props.get("Restart", "")
    if restart != "on-failure":
        problems.append(f"Restart: expected on-failure, found {restart or '<empty>'}")

    killmode = props.get("KillMode", "")
    if killmode != "control-group":
        problems.append(f"KillMode: expected control-group, found {killmode or '<empty>'}")

    timeout_raw = props.get("TimeoutStopSec") or props.get("TimeoutStopUSec") or ""
    timeout = seconds(timeout_raw)
    if timeout is None or abs(timeout - 120.0) > 0.5:
        problems.append(f"TimeoutStopSec: expected 120s, found {timeout_raw or '<empty>'}")

    environment = props.get("Environment", "")
    pairs = env_pairs(environment) if environment else {}
    if pairs is None:
        problems.append("Environment: could not be parsed")
        pairs = {}
    for name, want in (("OPENCODE_BIN", guard), ("IDLE_POLL_SECONDS", "300")):
        got = pairs.get(name, "")
        if got != want:
            problems.append(f"Environment: {name} expected {want}, found {got or '<unset>'}")

    for dropin in props.get("DropInPaths", "").split():
        base = os.path.basename(dropin)
        if base in LEGACY_DROPINS:
            problems.append(f"DropInPaths: legacy drop-in still active: {base}")

    files = []
    if os.path.isfile(fragment):
        files.append(fragment)
    if os.path.isdir(dropin_dir):
        for name in sorted(os.listdir(dropin_dir)):
            path = os.path.join(dropin_dir, name)
            if os.path.isfile(path):
                files.append(path)
    for path in files:
        for line in join_continuations(read_lines(path)):
            if re.match(r"^\s*Restart\s*=\s*no\s*$", line):
                problems.append(f"{os.path.basename(path)}: Restart=no is not allowed")

    for problem in problems:
        print(problem)
    raise SystemExit(0 if not problems else 1)


mode = sys.argv[1]
args = sys.argv[2:]
if mode == "redact-line":
    print(redact(args[0]))
elif mode == "redact-file":
    for line in join_continuations(read_lines(args[0])):
        print(redact(line))
elif mode == "first-line":
    pattern = re.compile(args[1])
    for line in join_continuations(read_lines(args[0])):
        if pattern.search(line):
            print(line)
            raise SystemExit(0)
    raise SystemExit(1)
elif mode == "env-value":
    pairs = {}
    for line in join_continuations(read_lines(args[0])):
        stripped = line.strip()
        if stripped.startswith("Environment="):
            found = env_pairs(stripped.split("=", 1)[1])
            if found:
                pairs.update(found)
    print(pairs.get(args[1], ""))
elif mode == "dropin-bad-keys":
    dropin_bad_keys(args[0])
elif mode == "check-effective":
    check_effective(args)
else:
    raise SystemExit(f"unknown unit_text_tool mode {mode}")
PY
}

redact_line() { unit_text_tool redact-line "$1"; }

print_redacted() {
  local line
  while IFS= read -r line; do
    log "  $line"
  done < <(unit_text_tool redact-file "$1")
}

# ---------------------------------------------------------------------------
# small state helpers
# ---------------------------------------------------------------------------
# systemd state queries. Only documented state strings are accepted; a failed
# or unparseable query yields "unknown" and a non-zero status so callers fail
# closed instead of guessing.
unit_state() { # active|enabled unit
  local mode=$1 unit=$2 out="" rc=0
  out=$(systemctl --user "is-${mode}" "$unit" 2>/dev/null) || rc=$?
  out=${out%%$'\n'*}
  case $mode in
    active)
      case $rc in 0|3|4) ;; *) printf 'unknown\n'; return 4 ;; esac
      case $out in
        active|activating|deactivating|inactive|failed) printf '%s\n' "$out"; return 0 ;;
      esac
      ;;
    enabled)
      case $rc in 0|1) ;; *) printf 'unknown\n'; return 4 ;; esac
      case $out in
        enabled|enabled-runtime|linked|linked-runtime|alias|masked|masked-runtime|static|indirect|disabled|generated|transient|bad)
          printf '%s\n' "$out"; return 0 ;;
      esac
      ;;
  esac
  printf 'unknown\n'
  return 4
}
unit_active() { unit_state active "$1"; }
unit_enabled() { unit_state enabled "$1"; }

# Process queries: 0 = match, 1 = no match, 2 = the query itself failed.
pgrep_present() {
  local pattern=$1 rc=0
  pgrep -u "$UID_NUM" -f "$pattern" >/dev/null 2>&1 || rc=$?
  case $rc in
    0) return 0 ;;
    1) return 1 ;;
    *) return 2 ;;
  esac
}

pgrep_count() {
  local pattern=$1 out="" rc=0
  out=$(pgrep -c -u "$UID_NUM" -f "$pattern" 2>/dev/null) || rc=$?
  case $rc in
    0|1) printf '%s\n' "${out:-0}"; return 0 ;;
    *) return 2 ;;
  esac
}

state_value() { # state.json key
  python3 - "$1" "$2" <<'PY'
import json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
value = state.get(sys.argv[2], "")
if isinstance(value, bool):
    print("true" if value else "false")
else:
    print(value)
PY
}

state_unit_field() { # state.json unit field
  python3 - "$1" "$2" "$3" <<'PY'
import json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
value = (state.get("units") or {}).get(sys.argv[2], {}).get(sys.argv[3], "")
if isinstance(value, bool):
    print("true" if value else "false")
else:
    print(value)
PY
}

state_file_sha() { # state.json absolute-path
  python3 - "$1" "$2" <<'PY'
import json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
print((state.get("files") or {}).get(sys.argv[2], ""))
PY
}

manifest_phase() {
  [[ -f "$1/state.json" ]] || { printf '\n'; return 0; }
  state_value "$1/state.json" phase
}

dropin_list() {
  local dir="${UNIT_DIR}/$1.d"
  [[ -d $dir ]] || { printf '\n'; return 0; }
  find "$dir" -maxdepth 1 -type f -printf '%f\n' 2>/dev/null | sort | paste -sd, -
}

lock_free() {
  local path=$1
  [[ -e $path ]] || return 0
  ( exec 9>"$path"; flock -n 9 )
}

# ---------------------------------------------------------------------------
# installer transaction lock
# ---------------------------------------------------------------------------
acquire_install_lock() { # shared|exclusive
  local mode=$1
  mkdir -p "$BACKUP_ROOT"
  chmod 0700 "$BACKUP_ROOT" 2>/dev/null || true
  exec 7>"${LOCK_INSTALL}"
  if [[ $mode == shared ]]; then
    if ! flock -s -n 7; then
      logerr "another installer run is in progress (${LOCK_INSTALL}); refusing to run concurrently"
      exit 75
    fi
  else
    if ! flock -n 7; then
      logerr "another installer run is in progress (${LOCK_INSTALL}); refusing to run concurrently"
      exit 75
    fi
  fi
}

# ---------------------------------------------------------------------------
# preflight
# ---------------------------------------------------------------------------
check_tools() {
  local tool missing=()
  for tool in systemctl git gh flock python3 loginctl journalctl pgrep sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  [[ ${#missing[@]} -eq 0 ]] || refuse "missing required tool(s): ${missing[*]}"
  log "P1 tooling: present"
}

check_repo() {
  local branch status remote_line remote_sha path
  [[ -d $TENDO_ROOT ]] || refuse "TENDO_ROOT ${TENDO_ROOT} does not exist"
  git -C "$TENDO_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 || \
    refuse "${TENDO_ROOT} is not a git work tree"
  branch=$(git -C "$TENDO_ROOT" symbolic-ref --quiet --short HEAD 2>/dev/null || true)
  [[ $branch == main ]] || refuse "TENDO_ROOT is on '${branch:-detached HEAD}', expected main"
  status=$(git -C "$TENDO_ROOT" status --porcelain)
  [[ -z $status ]] || refuse "TENDO_ROOT has uncommitted changes; cut over from committed main only"
  HEAD_SHA=$(git -C "$TENDO_ROOT" rev-parse HEAD)
  remote_line=$(git -C "$TENDO_ROOT" ls-remote origin refs/heads/main 2>/dev/null || true)
  remote_sha=${remote_line%%[[:space:]]*}
  [[ -n $remote_sha ]] || refuse "cannot read origin/main from ${TENDO_ROOT} (ls-remote failed)"
  [[ $HEAD_SHA == "$remote_sha" ]] || \
    refuse "HEAD ${HEAD_SHA:0:12} differs from origin/main ${remote_sha:0:12}; push/pull first"
  for path in scripts/autonomy-controller.sh scripts/autonomy-work-selection.py \
              .autonomous/AUTONOMY_PROMPT.md scripts/autonomy_migration.py; do
    [[ -e "${TENDO_ROOT}/${path}" ]] || refuse "missing required file ${TENDO_ROOT}/${path}"
  done
  [[ -x "${TENDO_ROOT}/scripts/autonomy-controller.sh" ]] || \
    refuse "scripts/autonomy-controller.sh is not executable"
  log "P2 checkout: main at ${HEAD_SHA:0:12}, clean, required files present"
}

check_required_ci() {
  local payload reason
  if ! payload=$(gh api "repos/${REPO}/commits/${HEAD_SHA}/check-runs?check_name=required&per_page=100" 2>&1); then
    refuse "cannot read required check-runs for ${HEAD_SHA:0:12}: ${payload:0:300}"
  fi
  if reason=$(printf '%s' "$payload" | python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
except Exception as exc:
    print(f"invalid JSON: {exc}")
    raise SystemExit(1)
runs = [run for run in (data.get("check_runs") or []) if run.get("name") == "required"]
runs.sort(key=lambda run: (run.get("started_at") or "", run.get("id") or 0))
if not runs:
    print("no required check run on HEAD")
    raise SystemExit(1)
latest = runs[-1]
status = latest.get("status")
conclusion = latest.get("conclusion")
if status != "completed" or conclusion != "success":
    print(f"latest required check is {status}/{conclusion}")
    raise SystemExit(1)
print("ok")
'); then
    log "P3 required CI: green on ${HEAD_SHA:0:12}"
  else
    refuse "required CI not green on ${HEAD_SHA:0:12}: ${reason}"
  fi
}

check_linger() {
  local line
  line=$(loginctl show-user "${USER:-$(id -un)}" -p Linger 2>/dev/null || true)
  [[ $line == *Linger=yes* ]] || \
    refuse "user linger is not enabled (${line:-loginctl failed}); run: loginctl enable-linger ${USER:-$(id -un)}"
  log "P4 linger: enabled"
}

guard_report() {
  GUARD_OUT=""
  GUARD_RC=0
  GUARD_ERR=""
  if [[ ! -x $GUARD ]]; then
    GUARD_ERR="guard ${GUARD} is not executable"
    return 1
  fi
  GUARD_OUT=$("$GUARD" --check 2>&1) || GUARD_RC=$?
  return 0
}

guard_sessions() {
  printf '%s\n' "${GUARD_OUT:-}" | sed -n 's/^Active Tendo sessions: *\([0-9][0-9]*\)$/\1/p' | head -1
}

guard_pending() {
  printf '%s\n' "${GUARD_OUT:-}" | sed -n 's/^Pending marker: *\(True\|False\)$/\1/p' | head -1
}

# 0 = provably idle, 1 = not idle, 2 = cannot determine (guard failed/unparseable).
guard_idle() {
  local rc=0 pending sessions
  guard_report
  [[ -z $GUARD_ERR ]] || return 2
  pending=$(guard_pending)
  sessions=$(guard_sessions)
  # Parseable evidence of work counts as "not idle" even when the guard exits
  # non-zero; anything unparseable is "cannot determine" below.
  [[ $pending == True ]] && return 1
  [[ -n $sessions && $sessions -gt 0 ]] && return 1
  [[ $GUARD_RC -eq 0 ]] || return 2
  [[ $pending == False ]] || return 1
  [[ $sessions == 0 ]] || return 1
  pgrep_present '(^|/)opencode run( |$)' || rc=$?
  case $rc in
    0) return 1 ;;
    1) return 0 ;;
    *) return 2 ;;
  esac
}

require_guard_idle() { # exit-code context
  local code=$1 context=$2 rc=0
  guard_idle || rc=$?
  case $rc in
    0) return 0 ;;
    1)
      guard_detail
      local prc=0
      pgrep_present '(^|/)opencode run( |$)' || prc=$?
      if [[ $prc -eq 0 ]]; then
        logerr "REFUSED: an 'opencode run' process is active"
      fi
      [[ $(guard_pending) == True ]] && print_pending_remediation
      logerr "REFUSED: active Tendo work detected (${context}); wait for the cycle to finish and reconcile it first"
      exit "$code"
      ;;
    *)
      guard_detail
      [[ -n $GUARD_ERR ]] && logerr "REFUSED: ${GUARD_ERR} (${context})"
      logerr "REFUSED: cannot prove the Tendo guard is idle (${context}; guard rc=${GUARD_RC:-n/a}); a guard or pgrep query failed"
      exit "$code"
      ;;
  esac
}

guard_detail() {
  local line
  while IFS= read -r line; do
    log "guard: $line"
  done < <(printf '%s\n' "${GUARD_OUT:-}" |
    grep -E '^(Repository ready:|Pending marker:|Active Tendo sessions:)' || true)
}

print_pending_remediation() {
  log "a pending cycle marker blocks the cutover; this installer never moves it."
  log "  1. inspect the recorded cycle: python3 -m json.tool \"${HOME}/.local/state/tendo-cycle.pending\""
  log "  2. reconcile it with the wrapper: opencode-tendo-safe --checkpoint"
  log "  3. only if that cycle's work is verifiably finished, archive the marker:"
  log "     mv \"${HOME}/.local/state/tendo-cycle.pending\" \"${HOME}/.local/state/tendo-cycle.pending.stale-\$(date +%Y%m%d-%H%M%S)\""
  log "  4. rerun: bash $0 --check"
}

check_no_active_work() {
  local state rc
  require_guard_idle 3 "cutover preflight"

  state=$(unit_active "$MAINT_SERVICE") || \
    refuse "cannot determine is-active for ${MAINT_SERVICE} (systemctl failed, state '${state}')"
  [[ $state != active && $state != activating ]] || refuse "${MAINT_SERVICE} is ${state}"
  state=$(unit_active "$AUTONOMOUS_UNIT") || \
    refuse "cannot determine is-active for ${AUTONOMOUS_UNIT} (systemctl failed, state '${state}')"
  if [[ $state == active || $state == activating ]]; then
    if [[ $ALREADY_MIGRATED -eq 1 ]]; then
      log "note: ${AUTONOMOUS_UNIT} already runs the new controller"
    else
      refuse "${AUTONOMOUS_UNIT} is ${state}; stop the legacy loop before cutover"
    fi
  fi

  # An already-migrated host legitimately has its own controller holding both
  # locks, so the idle checks below only apply to the one-time cutover.
  if [[ $ALREADY_MIGRATED -eq 1 ]]; then
    log "P5 work: migrated controller owns the locks; idempotent re-run continues"
    return 0
  fi

  rc=0
  pgrep_present 'scripts/autonomous-loop\.sh|tendo-maintenance\.sh' || rc=$?
  case $rc in
    0) refuse "a legacy loop or maintenance script process is active" ;;
    1) : ;;
    *) refuse "cannot determine whether a legacy loop is running (pgrep failed)" ;;
  esac

  lock_free "$LOCK_AUTONOMOUS" || refuse "lock ${LOCK_AUTONOMOUS} is held by another runner"
  lock_free "$LOCK_MAINT" || refuse "lock ${LOCK_MAINT} is held by a maintenance run"
  log "P5 work: idle (guard ok, no legacy processes, both locks free)"
}

# Validate a legacy drop-in on logical lines (continuations joined). Failure
# output lists key names only; values are never printed.
check_legacy_dropin() {
  local file=$1 output
  if output=$(unit_text_tool dropin-bad-keys "$file"); then
    return 0
  fi
  logerr "not verified safe to remove: ${file} has unsupported key(s): ${output}"
  return 1
}

check_units_sane() {
  local fragment="${UNIT_DIR}/${AUTONOMOUS_UNIT}" dir name file
  [[ -f $fragment ]] || refuse "missing unit fragment ${fragment}"
  unit_text_tool first-line "$fragment" '^ExecStart=' >/dev/null || \
    refuse "${fragment} has no ExecStart="
  [[ -n $(unit_text_tool env-value "$fragment" PATH) ]] || \
    refuse "${fragment} has no Environment=PATH= (cannot build the replacement unit)"

  dir="${UNIT_DIR}/${AUTONOMOUS_UNIT}.d"
  if [[ -d $dir ]]; then
    while IFS= read -r name; do
      [[ -n $name ]] || continue
      file="${dir}/${name}"
      case $name in
        "$KEEP_DROPIN") continue ;;
        10-attach.conf|20-safe.conf|30-cycle-spacing.conf)
          check_legacy_dropin "$file" || refuse "${file} is not verified safe to remove" ;;
        *) refuse "unknown drop-in ${file}; not verified safe to remove" ;;
      esac
    done < <(find "$dir" -maxdepth 1 -type f -printf '%f\n' 2>/dev/null | sort)
  fi

  local unit listed
  for unit in "$MAINT_SERVICE" "$MAINT_TIMER"; do
    if [[ -d "${UNIT_DIR}/${unit}.d" ]]; then
      listed=$(dropin_list "$unit")
      log "note: ${unit} has drop-ins (left untouched): ${listed}"
    fi
  done
  log "P6 units: fragment + drop-ins verified"
}

layout_migrated() {
  local fragment="${UNIT_DIR}/${AUTONOMOUS_UNIT}" exec_start name
  [[ -f $fragment ]] || return 1
  exec_start=$(unit_text_tool first-line "$fragment" '^ExecStart=' || true)
  [[ $exec_start == *autonomy-controller.sh* ]] || return 1
  for name in $AUTONOMOUS_DROPINS; do
    [[ -e "${UNIT_DIR}/${AUTONOMOUS_UNIT}.d/${name}" ]] && return 1
  done
  local enabled active
  enabled=$(unit_enabled "$MAINT_TIMER") || return 1
  active=$(unit_active "$MAINT_TIMER") || return 1
  [[ $enabled != enabled ]] || return 1
  [[ $active != active ]] || return 1
  return 0
}

latest_backup() {
  local pointer="${BACKUP_ROOT}/latest" dir
  [[ -f $pointer ]] || return 1
  dir=$(head -n1 "$pointer")
  [[ -n $dir && -f "$dir/state.json" ]] || return 1
  printf '%s\n' "$dir"
}

already_complete() {
  layout_migrated || return 1
  local backup phase
  backup=$(latest_backup) || return 1
  phase=$(manifest_phase "$backup")
  case $phase in
    units-replaced|units-running|complete) return 0 ;;
    *) return 1 ;;
  esac
}

# True when the unit fragment already starts the evergreen controller.
unit_installed() {
  local fragment="${UNIT_DIR}/${AUTONOMOUS_UNIT}" exec_start
  [[ -f $fragment ]] || return 1
  exec_start=$(unit_text_tool first-line "$fragment" '^ExecStart=') || return 1
  [[ $exec_start == *autonomy-controller.sh* ]]
}

# Validate the full effective configuration of an installed controller from
# `systemctl show` (plus the unit/drop-in files for the Restart=no rule).
# Prints one "key: expected ..., found ..." problem per line.
expected_config_problems() {
  local fragment="${UNIT_DIR}/${AUTONOMOUS_UNIT}"
  local dropin_dir="${UNIT_DIR}/${AUTONOMOUS_UNIT}.d"
  local show_file problems status=0
  show_file=$(mktemp)
  if ! systemctl --user show "$AUTONOMOUS_UNIT" \
      -p ExecStart -p Restart -p KillMode -p TimeoutStopSec \
      -p DropInPaths -p Environment >"$show_file" 2>&1; then
    printf 'systemctl show failed for %s\n' "$AUTONOMOUS_UNIT"
    rm -f "$show_file"
    return 1
  fi
  problems=$(unit_text_tool check-effective "$TENDO_ROOT" "$GUARD" \
    "$fragment" "$dropin_dir" "$show_file") || status=$?
  rm -f "$show_file"
  [[ $status -eq 0 ]] && return 0
  printf '%s\n' "$problems"
  return 1
}

require_expected_config() { # exit-code context
  local code=$1 context=$2 problems
  if problems=$(expected_config_problems); then
    return 0
  fi
  logerr "REFUSED: the installed ${AUTONOMOUS_UNIT} does not match the expected configuration (${context}):"
  while IFS= read -r line; do
    [[ -n $line ]] && logerr "  ${line}"
  done <<<"$problems"
  logerr "Nothing was changed and no backup was created; fix the unit or roll back the migration."
  exit "$code"
}

# Validate whenever the migration believes the controller is installed, even
# if the fragment itself drifted (per-key diagnostics instead of a new backup).
guard_installed_config() { # exit code
  local backup phase
  if backup=$(latest_backup); then
    phase=$(manifest_phase "$backup")
    case $phase in
      units-replaced|units-running|complete)
        require_expected_config "$1" "migration phase ${phase}"
        return 0
        ;;
    esac
  fi
  if unit_installed; then
    require_expected_config "$1" "installed controller"
  fi
}

preflight() {
  log "preflight: reading host state (no changes)"
  [[ ${EUID:-$(id -u)} -ne 0 ]] || refuse "must not run as root; this is a user-service cutover"
  check_tools
  check_repo
  check_required_ci
  check_linger
  ALREADY_MIGRATED=0
  local transaction_phase="" backup_hint
  if backup_hint=$(latest_backup); then
    transaction_phase=$(manifest_phase "$backup_hint")
  fi
  case $transaction_phase in
    units-replaced|units-running|complete)
      # This backup says our controller is installed; it legitimately owns the
      # locks. Full configuration drift is diagnosed after preflight.
      ALREADY_MIGRATED=1
      ;;
  esac
  layout_migrated && ALREADY_MIGRATED=1
  # Explicit fail-closed state reads: an unreadable timer state must never make
  # the idempotence/plan decisions above look successful.
  local timer_enabled timer_active
  timer_enabled=$(unit_enabled "$MAINT_TIMER") || \
    refuse "cannot determine is-enabled for ${MAINT_TIMER} (systemctl failed)"
  timer_active=$(unit_active "$MAINT_TIMER") || \
    refuse "cannot determine is-active for ${MAINT_TIMER} (systemctl failed)"
  if [[ $timer_enabled == enabled && $ALREADY_MIGRATED -eq 0 ]]; then
    log "note: ${MAINT_TIMER} is still enabled; --apply will disable it"
  fi
  check_no_active_work
  check_units_sane
  log "preflight: OK (HEAD ${HEAD_SHA:0:12}, repo ${REPO})"
}

# ---------------------------------------------------------------------------
# backup
# ---------------------------------------------------------------------------
write_initial_state() {
  local dir=$1 meta unit enabled active
  meta=$(mktemp)
  for unit in "$AUTONOMOUS_UNIT" "$MAINT_SERVICE" "$MAINT_TIMER"; do
    enabled=$(unit_enabled "$unit") || fail "cannot record is-enabled for ${unit} (systemctl failed)"
    active=$(unit_active "$unit") || fail "cannot record is-active for ${unit} (systemctl failed)"
    printf '%s\t%s\t%s\t%s\t%s\n' \
      "$unit" "${UNIT_DIR}/${unit}" "$enabled" "$active" \
      "$(dropin_list "$unit")" >>"$meta"
  done
  python3 - "$dir/state.json" "$HEAD_SHA" "$meta" "$MAINT_SCRIPT" "$MAINT_PROMPT" <<'PY'
import datetime
import hashlib
import json
import os
import sys

state_path, head, meta_path, maint_script, maint_prompt = sys.argv[1:6]
units = {}
for raw in open(meta_path, encoding="utf-8"):
    raw = raw.rstrip("\n")
    if not raw:
        continue
    name, fragment, enabled, active, dropins = raw.split("\t")
    units[name] = {
        "fragment": fragment,
        "enabled": enabled,
        "active": active,
        "dropins": [item for item in dropins.split(",") if item],
    }

def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()

state = {
    "units": units,
    "files": {maint_script: sha256(maint_script), maint_prompt: sha256(maint_prompt)},
    "root_head": head,
    "created_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "phase": "backed-up",
}
tmp = state_path + ".tmp"
with open(tmp, "w", encoding="utf-8") as handle:
    json.dump(state, handle, indent=2, sort_keys=True)
    handle.write("\n")
os.chmod(tmp, 0o600)
os.replace(tmp, state_path)
PY
  rm -f "$meta"
}

write_phase() {
  local phase=$1
  python3 - "$BACKUP/state.json" "$phase" <<'PY'
import json
import os
import sys

path, phase = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as handle:
    state = json.load(handle)
state["phase"] = phase
tmp = path + ".tmp"
with open(tmp, "w", encoding="utf-8") as handle:
    json.dump(state, handle, indent=2, sort_keys=True)
    handle.write("\n")
os.chmod(tmp, 0o600)
os.replace(tmp, path)
PY
  RUNNING_PHASE=$phase
}

create_backup() {
  local stamp dir unit n=1
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  mkdir -p "$BACKUP_ROOT"
  chmod 0700 "$BACKUP_ROOT" 2>/dev/null || true
  dir="${BACKUP_ROOT}/${stamp}"
  while [[ -e $dir ]]; do
    dir="${BACKUP_ROOT}/${stamp}-${n}"
    n=$((n + 1))
  done
  mkdir -m 0700 "$dir"
  mkdir -p "$dir/units" "$dir/removed-dropins" "$dir/files"

  for unit in "$AUTONOMOUS_UNIT" "$MAINT_SERVICE" "$MAINT_TIMER"; do
    [[ -f "${UNIT_DIR}/${unit}" ]] && cp -a "${UNIT_DIR}/${unit}" "$dir/units/${unit}"
    [[ -d "${UNIT_DIR}/${unit}.d" ]] && cp -a "${UNIT_DIR}/${unit}.d" "$dir/units/${unit}.d"
  done
  cp -a "$MAINT_SCRIPT" "$dir/files/$(basename "$MAINT_SCRIPT")"
  cp -a "$MAINT_PROMPT" "$dir/files/$(basename "$MAINT_PROMPT")"
  write_initial_state "$dir"

  BACKUP=$dir
  log "step 2/7 backup: ${BACKUP} (mode 0700)"
  # Point `latest` at the new transaction before doing anything else: an
  # interruption from here on must resume this backup, never overwrite it.
  printf '%s\n' "$BACKUP" >"${BACKUP_ROOT}/latest"
  ensure_label_plan
}

ensure_label_plan() {
  if [[ -f "$BACKUP/labels.json" ]]; then
    python3 "$MIGRATION" show-plan --manifest "$BACKUP/labels.json"
    return 0
  fi
  if ! python3 "$MIGRATION" plan --manifest "$BACKUP/labels.json" --repo "$REPO"; then
    fail "label plan failed; backup kept at ${BACKUP} (host unchanged; rerun --apply to resume)"
  fi
  python3 "$MIGRATION" show-plan --manifest "$BACKUP/labels.json"
  log "label manifest: ${BACKUP}/labels.json"
}

# ---------------------------------------------------------------------------
# steps 3..7
# ---------------------------------------------------------------------------
quiesce() {
  log "step 3/7 quiesce: disabling the maintenance timer and stopping legacy units"
  systemctl --user disable --now "$MAINT_TIMER"
  systemctl --user stop "$MAINT_SERVICE" "$AUTONOMOUS_UNIT"

  local unit state
  state=$(unit_enabled "$MAINT_TIMER") || fail "cannot determine is-enabled for ${MAINT_TIMER} (systemctl failed)"
  [[ $state != enabled ]] || fail "${MAINT_TIMER} is still enabled"
  for unit in "$AUTONOMOUS_UNIT" "$MAINT_SERVICE" "$MAINT_TIMER"; do
    state=$(unit_active "$unit") || fail "cannot determine is-active for ${unit} (systemctl failed, state '${state}')"
    [[ $state != active && $state != activating ]] || fail "${unit} is still ${state}"
  done
  check_no_active_work
  write_phase quiesced
  hold_locks
}

hold_locks() {
  [[ $LOCKS_HELD -eq 0 ]] || return 0
  mkdir -p "$RUNTIME_DIR"
  exec 8>"$LOCK_MAINT" 9>"$LOCK_AUTONOMOUS"
  flock -n 9 || fail "another autonomy runner took ${LOCK_AUTONOMOUS}"
  flock -n 8 || fail "another maintenance run took ${LOCK_MAINT}"
  LOCKS_HELD=1
  log "holding both controller locks until the new unit is installed"
}

release_locks() {
  [[ $LOCKS_HELD -eq 1 ]] || return 0
  exec 8>&- || true
  exec 9>&- || true
  LOCKS_HELD=0
}

migrate_labels() {
  log "step 4/7 labels: migrating ${REPO} ownership to 'release'"
  if ! python3 "$MIGRATION" apply --manifest "$BACKUP/labels.json" --repo "$REPO"; then
    logerr "label migration failed; run --rollback (labels.json records partial progress)"
    exit 4
  fi
  if ! python3 "$MIGRATION" validate --manifest "$BACKUP/labels.json" --repo "$REPO" --selector "$SELECTOR"; then
    logerr "label validation failed; run --rollback"
    exit 4
  fi
  write_phase labels-migrated
}

install_unit() {
  local fragment="${UNIT_DIR}/${AUTONOMOUS_UNIT}" path_value tmp name dir
  path_value=$(unit_text_tool env-value "$fragment" PATH)
  [[ -n $path_value ]] || fail "cannot read Environment=PATH from ${fragment}"
  log "step 5/7 unit: writing the controller unit and retiring legacy drop-ins"

  tmp="${UNIT_DIR}/.${AUTONOMOUS_UNIT}.new.$$"
  {
    printf '[Unit]\n'
    printf 'Description=Tendo autonomy controller (release Issues, dependencies, security, CI, patch releases)\n'
    printf 'After=network-online.target\n'
    printf 'Wants=network-online.target\n'
    printf '\n[Service]\n'
    printf 'Type=simple\n'
    printf 'WorkingDirectory=%s\n' "$TENDO_ROOT"
    printf 'Environment=PATH=%s\n' "$path_value"
    printf 'Environment=OPENCODE_BIN=%s\n' "$GUARD"
    printf 'Environment=IDLE_POLL_SECONDS=300\n'
    printf 'ExecStart=%s/scripts/autonomy-controller.sh\n' "$TENDO_ROOT"
    printf 'Restart=on-failure\n'
    printf 'RestartSec=60\n'
    printf 'TimeoutStopSec=120\n'
    printf 'KillMode=control-group\n'
    printf '\n[Install]\n'
    printf 'WantedBy=default.target\n'
  } >"$tmp"
  chmod 0644 "$tmp"
  mv -f "$tmp" "$fragment"

  dir="${UNIT_DIR}/${AUTONOMOUS_UNIT}.d"
  for name in $AUTONOMOUS_DROPINS; do
    if [[ -e "${dir}/${name}" ]]; then
      mv "${dir}/${name}" "$BACKUP/removed-dropins/${name}"
      log "moved legacy drop-in ${name} to the backup"
    fi
  done

  systemctl --user daemon-reload
  local shown
  shown=$(systemctl --user show "$AUTONOMOUS_UNIT" \
    -p ExecStart -p Restart -p KillMode -p DropInPaths)
  [[ $shown == *autonomy-controller.sh* ]] || fail "unit ExecStart does not reference autonomy-controller.sh"
  [[ $shown == *$'Restart=on-failure'* ]] || fail "unit Restart is not on-failure"
  [[ $shown == *'KillMode=control-group'* ]] || fail "unit KillMode is not control-group"
  for name in $AUTONOMOUS_DROPINS; do
    if [[ $shown == *"${name}"* ]]; then
      fail "legacy drop-in ${name} is still in the effective unit"
    fi
  done
  write_phase units-replaced
}

wait_for_controller() {
  local deadline
  deadline=$(( $(date +%s) + START_TIMEOUT ))
  while true; do
    if [[ $(unit_active "$AUTONOMOUS_UNIT") == active ]] &&
       journalctl --user -u "$AUTONOMOUS_UNIT" --since "@$START_EPOCH" -o cat 2>/dev/null |
         grep -qF '[autonomy] controller started'; then
      return 0
    fi
    (( $(date +%s) < deadline )) || return 1
    sleep 1
  done
}

verify_no_second_scheduler() {
  local controllers rc=0 enabled active
  controllers=$(pgrep_count 'scripts/autonomy-controller\.sh') || \
    fail "cannot count autonomy-controller processes (pgrep failed)"
  [[ ${controllers:-0} -eq 1 ]] || fail "expected exactly one autonomy-controller process, found ${controllers:-0}"
  pgrep_present 'scripts/autonomous-loop\.sh' || rc=$?
  case $rc in
    0) fail "a legacy autonomous-loop.sh process is still running" ;;
    1) : ;;
    *) fail "cannot determine whether a legacy autonomous-loop.sh process is running" ;;
  esac
  enabled=$(unit_enabled "$MAINT_TIMER") || fail "cannot determine is-enabled for ${MAINT_TIMER}"
  [[ $enabled != enabled ]] || fail "${MAINT_TIMER} is enabled again"
  active=$(unit_active "$MAINT_TIMER") || fail "cannot determine is-active for ${MAINT_TIMER}"
  [[ $active != active ]] || fail "${MAINT_TIMER} is active"
  active=$(unit_active "$MAINT_SERVICE") || fail "cannot determine is-active for ${MAINT_SERVICE}"
  [[ $active != active ]] || fail "${MAINT_SERVICE} is active"
}

start_service() {
  local state was_active=0
  state=$(unit_active "$AUTONOMOUS_UNIT") || fail "cannot determine is-active for ${AUTONOMOUS_UNIT}"
  [[ $state == active ]] && was_active=1
  log "step 6/7 start: enabling and starting ${AUTONOMOUS_UNIT}"
  release_locks
  systemctl --user enable "$AUTONOMOUS_UNIT"
  START_EPOCH=$(date +%s)
  systemctl --user start "$AUTONOMOUS_UNIT"
  if [[ $was_active -eq 0 ]] && ! wait_for_controller; then
    fail "controller did not log '[autonomy] controller started' within ${START_TIMEOUT}s"
  fi
  verify_no_second_scheduler
  write_phase units-running
}

finish_apply() {
  write_phase complete
  log "step 7/7 complete: evergreen autonomy controller is active"
  log "monitor: systemctl --user status ${AUTONOMOUS_UNIT}"
  log "monitor: journalctl --user -u ${AUTONOMOUS_UNIT} -f"
  log "labels:  gh issue list --repo ${REPO} --label release --state open"
  log "rollback: bash $0 --rollback"
}

# ---------------------------------------------------------------------------
# --check
# ---------------------------------------------------------------------------
print_plan() {
  local fragment="${UNIT_DIR}/${AUTONOMOUS_UNIT}" path_value plan_dir
  path_value=$(unit_text_tool env-value "$fragment" PATH)

  log "plan: unit dir         ${UNIT_DIR}"
  log "plan: guard            ${GUARD}"
  log "plan: maintenance prompt ${MAINT_PROMPT} (read-only; sha256 $(sha256sum "$MAINT_PROMPT" | cut -d' ' -f1))"
  log "plan: backup root      ${BACKUP_ROOT}"
  log "plan: legacy drop-ins to REMOVE (moved to backup, never deleted): ${AUTONOMOUS_DROPINS}"
  log "plan: drop-in KEPT: ${KEEP_DROPIN} (OpenCode server dependency)"
  log "plan: replacement unit (redacted):"
  {
    printf '    [Unit]\n'
    printf '    Description=Tendo autonomy controller (release Issues, dependencies, security, CI, patch releases)\n'
    printf '    [Service]\n'
    printf '    WorkingDirectory=%s\n' "$TENDO_ROOT"
    printf '    Environment=PATH=%s\n' "$path_value"
    printf '    Environment=OPENCODE_BIN=%s\n' "$GUARD"
    printf '    Environment=IDLE_POLL_SECONDS=300\n'
    printf '    ExecStart=%s/scripts/autonomy-controller.sh\n' "$TENDO_ROOT"
    printf '    Restart=on-failure RestartSec=60 TimeoutStopSec=120 KillMode=control-group\n'
  } | while IFS= read -r line; do redact_line "${line#    }"; done
  log "plan: current unit (redacted):"
  print_redacted "$fragment"
  log "plan: maintenance timer will be disabled and stopped; service remains static/oneshot"
  log "plan: label migration (read-only inventory):"

  plan_dir=$(mktemp -d)
  if ! python3 "$MIGRATION" plan --manifest "$plan_dir/labels.json" --repo "$REPO"; then
    rm -rf "$plan_dir"
    fail "label plan failed; cannot print the label migration plan"
  fi
  python3 "$MIGRATION" show-plan --manifest "$plan_dir/labels.json"
  rm -rf "$plan_dir"
}

run_check() {
  acquire_install_lock shared
  preflight
  guard_installed_config 3
  print_plan
  log "dry run: no unit, timer, drop-in or label was modified"
  log "check OK: run 'bash $0 --apply' to perform the cutover"
  exit 0
}

# ---------------------------------------------------------------------------
# --apply
# ---------------------------------------------------------------------------
run_apply() {
  acquire_install_lock exclusive
  preflight
  guard_installed_config 3

  if already_complete; then
    BACKUP=$(latest_backup)
    log "already complete: ${AUTONOMOUS_UNIT} runs the controller, legacy drop-ins are gone,"
    log "                ${MAINT_TIMER} is disabled/inactive (backup ${BACKUP})"
    python3 "$MIGRATION" validate --manifest "$BACKUP/labels.json" --repo "$REPO" --selector "$SELECTOR" ||
      fail "post-migration validation failed"
    local state
    state=$(unit_active "$AUTONOMOUS_UNIT") || \
      fail "cannot determine is-active for ${AUTONOMOUS_UNIT} (systemctl failed)"
    if [[ $state != active ]]; then
      systemctl --user enable "$AUTONOMOUS_UNIT"
      START_EPOCH=$(date +%s)
      systemctl --user start "$AUTONOMOUS_UNIT"
      wait_for_controller || fail "controller did not start within ${START_TIMEOUT}s"
    fi
    verify_no_second_scheduler
    if [[ $(manifest_phase "$BACKUP") != complete ]]; then
      write_phase complete
    fi
    log "already complete"
    exit 0
  fi

  log "step 1/7 preflight: OK"

  # Never create a replacement backup while a transaction of this migration is
  # unfinished (or finished but unmatched): that would capture already-migrated
  # state as "original" and lose the rollback baseline. Only a completed
  # rollback ends the transaction.
  local latest phase
  if latest=$(latest_backup); then
    phase=$(manifest_phase "$latest")
    case $phase in
      backed-up|quiesced|labels-migrated|units-replaced|units-running)
        BACKUP=$latest
        log "step 2/7 backup: resuming interrupted migration from ${BACKUP}"
        ;;
      rolled-back)
        create_backup
        ;;
      *)
        refuse "backup ${latest} has phase '${phase}' and cannot be resumed safely; refusing to create a replacement backup (inspect it, or use --rollback --backup ${latest})"
        ;;
    esac
  else
    create_backup
  fi
  log "phase: $(manifest_phase "$BACKUP")"

  case $(manifest_phase "$BACKUP") in
    backed-up)
      ensure_label_plan
      quiesce
      ;;
    quiesced|labels-migrated|units-replaced) hold_locks ;;
    units-running) ;;
    *) fail "backup phase $(manifest_phase "$BACKUP") cannot be resumed; use --rollback" ;;
  esac

  case $(manifest_phase "$BACKUP") in
    labels-migrated|units-replaced|units-running) log "labels already migrated" ;;
    quiesced) migrate_labels ;;
    *) fail "unexpected phase $(manifest_phase "$BACKUP")" ;;
  esac

  case $(manifest_phase "$BACKUP") in
    units-running) log "unit already replaced and started" ;;
    units-replaced) log "unit already replaced" ;;
    labels-migrated) install_unit ;;
    *) fail "unexpected phase $(manifest_phase "$BACKUP")" ;;
  esac

  if [[ $(manifest_phase "$BACKUP") == units-running ]]; then
    verify_no_second_scheduler
  else
    start_service
  fi
  finish_apply
}

# ---------------------------------------------------------------------------
# --rollback
# ---------------------------------------------------------------------------
resolve_backup() {
  local dir
  if [[ -n ${BACKUP_ARG:-} ]]; then
    dir=$BACKUP_ARG
  else
    dir=$(latest_backup) || fail "no backup found under ${BACKUP_ROOT}; pass --backup DIR"
  fi
  [[ -d $dir && -f "$dir/state.json" ]] || fail "backup ${dir} has no state.json"
  printf '%s\n' "$dir"
}

# R1: the new controller must be provably idle. A failed or unparseable guard
# is not "zero sessions": it exits 5 like active work.
rollback_refuse_running() {
  require_guard_idle 5 "rollback precheck"
}

# Re-checked after stopping the service and taking both locks, before any label
# change. If a cycle started meanwhile (or cannot be ruled out), abort with the
# service stopped and no label mutation.
rollback_recheck_idle() {
  local rc=0
  guard_idle || rc=$?
  case $rc in
    0) return 0 ;;
    *)
      guard_detail
      logerr "REFUSED: a Tendo cycle is active or cannot be ruled out after stopping the controller."
      logerr "no label was changed; the service remains stopped and the maintenance timer stays disabled."
      logerr "Wait for the cycle to finish, then rerun 'bash $0 --rollback --backup ${BACKUP}'."
      exit 5
      ;;
  esac
}

rollback_units() {
  local unit source want
  for unit in "$AUTONOMOUS_UNIT" "$MAINT_SERVICE" "$MAINT_TIMER"; do
    source="${BACKUP}/units/${unit}"
    if [[ -f $source ]]; then
      cp -a "$source" "${UNIT_DIR}/${unit}"
      log "restored ${UNIT_DIR}/${unit}"
    fi
    if [[ -d "${BACKUP}/units/${unit}.d" ]]; then
      mkdir -p "${UNIT_DIR}/${unit}.d"
      cp -a "${BACKUP}/units/${unit}.d/." "${UNIT_DIR}/${unit}.d/"
      log "restored ${UNIT_DIR}/${unit}.d"
    fi
  done

  local file recorded actual
  for file in "$MAINT_SCRIPT" "$MAINT_PROMPT"; do
    recorded=$(state_file_sha "$BACKUP/state.json" "$file")
    actual=$(sha256sum "$file" | cut -d' ' -f1)
    if [[ -n $recorded && $actual != "$recorded" ]]; then
      logerr "warning: ${file} changed since the backup; restoring the backed-up copy"
      cp -a "${BACKUP}/files/$(basename "$file")" "$file"
    fi
  done

  systemctl --user daemon-reload

  for unit in "$AUTONOMOUS_UNIT" "$MAINT_SERVICE" "$MAINT_TIMER"; do
    want=$(state_unit_field "$BACKUP/state.json" "$unit" enabled)
    case $want in
      enabled) systemctl --user enable "$unit" ;;
      disabled) systemctl --user disable "$unit" ;;
      *) : ;;
    esac
  done
  for unit in "$AUTONOMOUS_UNIT" "$MAINT_SERVICE" "$MAINT_TIMER"; do
    want=$(state_unit_field "$BACKUP/state.json" "$unit" active)
    if [[ $want == active || $want == activating ]]; then
      systemctl --user start "$unit"
    fi
  done
}

run_rollback() {
  acquire_install_lock exclusive
  BACKUP=$(resolve_backup)
  log "rollback: using backup ${BACKUP} (phase $(manifest_phase "$BACKUP"))"

  rollback_refuse_running

  log "rollback: stopping ${AUTONOMOUS_UNIT}"
  systemctl --user stop "$AUTONOMOUS_UNIT"
  local state
  state=$(unit_active "$AUTONOMOUS_UNIT") || \
    fail "cannot determine is-active for ${AUTONOMOUS_UNIT} (systemctl failed)"
  [[ $state != active && $state != activating ]] || fail "${AUTONOMOUS_UNIT} is still ${state}"

  mkdir -p "$RUNTIME_DIR"
  exec 8>"$LOCK_MAINT" 9>"$LOCK_AUTONOMOUS"
  flock -n 9 || fail "another autonomy runner holds ${LOCK_AUTONOMOUS}"
  flock -n 8 || fail "another maintenance run holds ${LOCK_MAINT}"
  LOCKS_HELD=1

  rollback_recheck_idle

  local phase
  phase=$(manifest_phase "$BACKUP")
  case $phase in
    quiesced|labels-migrated|units-replaced|units-running|complete)
      if [[ -f "$BACKUP/labels.json" ]]; then
        log "rollback: restoring ownership labels"
        if ! python3 "$MIGRATION" rollback --manifest "$BACKUP/labels.json" --repo "$REPO"; then
          # Both stops are idempotent; do not depend on a state query that may
          # itself be failing in this degraded path.
          systemctl --user stop "$AUTONOMOUS_UNIT" || true
          systemctl --user disable "$MAINT_TIMER" || true
          logerr "both controllers remain stopped; restore the labels with the commands above,"
          logerr "then rerun 'bash $0 --rollback --backup ${BACKUP}'"
          exit 6
        fi
      fi
      ;;
    *)
      log "rollback: labels were never touched (phase ${phase})"
      ;;
  esac

  rollback_units
  release_locks
  write_phase rolled-back
  log "rollback complete: original unit, drop-ins, timer state and ownership labels restored"
  log "verify: systemctl --user status ${AUTONOMOUS_UNIT} ${MAINT_TIMER}"
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
ACTION=""
BACKUP_ARG=""
while (($#)); do
  case $1 in
    --check|--apply|--rollback)
      [[ -z $ACTION ]] || usage_error "only one action is allowed"
      ACTION=${1#--}
      shift
      ;;
    --backup)
      (($# >= 2)) || usage_error "--backup needs a directory"
      BACKUP_ARG=$2
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage_error "unknown argument: $1"
      ;;
  esac
done

[[ -n $ACTION ]] || usage_error "choose one of --check, --apply, --rollback"
[[ -z $BACKUP_ARG || $ACTION == rollback ]] || usage_error "--backup is only valid with --rollback"

case $ACTION in
  check) run_check ;;
  apply) run_apply ;;
  rollback) run_rollback ;;
esac
