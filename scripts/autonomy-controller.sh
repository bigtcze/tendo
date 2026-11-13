#!/usr/bin/env bash
set -u -o pipefail

# Tendo evergreen autonomy controller (tendo-autonomous.service).
#
# One long-lived, serialized worker. Every loop iteration either runs one
# orchestrator session or pauses:
#   1. a pending safe-wrapper marker is recovered through the wrapper first
#      (new work must never start on top of an unresolved cycle),
#   2. the local wrapper state is checked, so GitHub is not polled while a
#      session is active or the canonical checkout is not ready,
#   3. scripts/autonomy-work-selection.py decides the next work item
#      (read-only GitHub access happens only here),
#   4. a non-idle decision runs exactly one `opencode run` session, and the
#      controller waits until that session's whole process group is gone
#      before the next GitHub poll.
#
# It never exits because of a completion marker (.autonomous/*_COMPLETE,
# V4_COMPLETE, V5_COMPLETE, future markers). Only a signal, a configuration
# error, a busy lock or AUTONOMY_MAX_LOOPS (test-only) stops it. Once a
# shutdown signal has started the cycle cleanup, later HUP/INT/TERM signals are
# ignored: the process exits only after the cycle's process group is gone, so
# the locks are never released while a child could still be running.
#
# Fail-closed cleanup: zombies do not count as live group members, so a group
# whose remaining members are already dead is treated as gone. If live members
# survive SIGKILL past CYCLE_DRAIN_TIMEOUT the controller never resumes polling
# (or waits on them forever): it logs an error and exits 70. Under systemd the
# released locks are safe to hand over, because TimeoutStopSec/KillMode=
# control-group stops the whole cgroup on unit stop or restart (killing the
# remnants) while Restart=on-failure starts one clean controller.
#
# Locks: both locks are held for the whole process lifetime and are never
# inherited by children (8>&- 9>&-). The singleton lock prevents a second
# controller; the legacy maintenance lock keeps the host maintenance
# controller out while an autonomy session may be working.
#
# Logging: the controller's own `[autonomy]` lines are the only journal
# output. Each session's stdout and stderr go by plain redirection (no pipe,
# so `timeout` stays the process-group leader and its exit status is kept)
# into private per-cycle files below CYCLE_LOG_DIR (directories 0700, files
# 0600): <UTC start>-c<cycle>-p<controller pid>/{stdout.log,stderr.log,status}.
# The journal gets only a short summary with the exit code, duration and the
# diagnostics path, never the session's raw output. A session never starts
# when its diagnostics cannot be created. Old cycle directories are pruned by
# count, age and total size; the newest one is always kept.

ROOT="${TENDO_ROOT:-/home/tomas/repositories/tendo}"
IDLE_POLL_SECONDS="${IDLE_POLL_SECONDS:-300}"
POST_CYCLE_PAUSE="${POST_CYCLE_PAUSE:-30}"
FAIL_SLEEP_BASE="${FAIL_SLEEP_BASE:-120}"
FAIL_SLEEP_MAX="${FAIL_SLEEP_MAX:-1800}"
BUSY_SLEEP="${BUSY_SLEEP:-60}"
RATE_LIMIT_MIN_SLEEP="${RATE_LIMIT_MIN_SLEEP:-60}"
RATE_LIMIT_MAX_SLEEP="${RATE_LIMIT_MAX_SLEEP:-3600}"
CYCLE_TIMEOUT="${CYCLE_TIMEOUT:-4h}"
CYCLE_KILL_AFTER="${CYCLE_KILL_AFTER:-60s}"
CYCLE_STOP_GRACE="${CYCLE_STOP_GRACE:-30}"
CYCLE_DRAIN_TIMEOUT="${CYCLE_DRAIN_TIMEOUT:-600}"
SELECTION_TIMEOUT="${SELECTION_TIMEOUT:-300s}"
GUARD_CHECK_TIMEOUT="${GUARD_CHECK_TIMEOUT:-120}"
# Test-only bound: 0 (default) means unlimited.
AUTONOMY_MAX_LOOPS="${AUTONOMY_MAX_LOOPS:-0}"
GUARD_CHECK="${GUARD_CHECK:-auto}"
CYCLE_LOG_DIR="${CYCLE_LOG_DIR:-${XDG_STATE_HOME:-${HOME}/.local/state}/tendo-autonomy/cycles}"
CYCLE_LOG_KEEP_COUNT="${CYCLE_LOG_KEEP_COUNT:-200}"
CYCLE_LOG_MAX_AGE_DAYS="${CYCLE_LOG_MAX_AGE_DAYS:-30}"
CYCLE_LOG_MAX_BYTES="${CYCLE_LOG_MAX_BYTES:-2147483648}"

RUNTIME="${XDG_RUNTIME_DIR:-/tmp}"
LOCK_FILE="${RUNTIME}/tendo-autonomous-${UID}.lock"
MAINTENANCE_LOCK_FILE="${RUNTIME}/tendo-maintenance-${UID}.lock"

OPENCODE_BIN="${OPENCODE_BIN:-/home/tomas/.local/bin/opencode-tendo-safe}"
PENDING_MARKER="${PENDING_MARKER:-${HOME}/.local/state/tendo-cycle.pending}"
PROMPT_FILE="${PROMPT_FILE:-${ROOT}/.autonomous/AUTONOMY_PROMPT.md}"
SELECTION_SCRIPT="${SELECTION_SCRIPT:-${ROOT}/scripts/autonomy-work-selection.py}"

RECOVERY_DECISION='DECISION priority=3 kind=wrapper-recovery target=- (pending cycle marker present; the safe wrapper resumes the recorded session)'

BUSY_EXIT=75
# A live cycle process survived SIGKILL: never resume work next to it.
FAIL_CLOSED_EXIT=70

log() { printf '[autonomy] %s\n' "$*"; }
err() { printf '[autonomy] %s\n' "$*" >&2; }

# Fail closed: a live cycle process could still touch the repository. Stay
# down and let the process manager stop the unit's whole cgroup and start a
# fresh instance (Restart=on-failure) instead of polling/starting new work.
fail_closed() {
  err "fail-closed: ${1}; exiting ${FAIL_CLOSED_EXIT} so the service manager cleans up the whole cgroup"
  cycle_interrupted "fail-closed" "${FAIL_CLOSED_EXIT}"
  exit "${FAIL_CLOSED_EXIT}"
}

# cycle_interrupted <reason> <controller exit code>: the controller is leaving
# while a cycle is in flight; keep the evidence and name it in the journal.
# Defined before use; the per-cycle state it reads is initialised later.
cycle_interrupted() {
  [[ "${cycle_running:-0}" -eq 1 && -n "${cycle_log_dir:-}" ]] || return 0
  cycle_running=0
  printf '%s\n' "finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)" "interrupted=${1}" \
    "controller_exit_code=${2}" >>"${cycle_log_dir}/status" 2>/dev/null || true
  err "cycle ${cycle} interrupted (${1}); controller exit ${2}; diagnostics=${cycle_log_dir}"
}

require_int() {
  local name="$1" value="$2" minimum="$3"
  if [[ ! "${value}" =~ ^[0-9]+$ ]]; then
    err "invalid ${name}='${value}' (expected a non-negative integer)"
    exit 2
  fi
  if ((value < minimum)); then
    err "invalid ${name}='${value}' (minimum ${minimum})"
    exit 2
  fi
}

resolve_opencode() {
  local candidate
  if [[ -x "${OPENCODE_BIN}" ]]; then
    return 0
  fi
  candidate="/home/tomas/.opencode/bin/opencode"
  if [[ -x "${candidate}" ]]; then
    OPENCODE_BIN="${candidate}"
    return 0
  fi
  candidate="$(command -v opencode 2>/dev/null || true)"
  if [[ -n "${candidate}" && -x "${candidate}" ]]; then
    OPENCODE_BIN="${candidate}"
    return 0
  fi
  return 1
}

cd "${ROOT}" 2>/dev/null || {
  err "cannot cd to ${ROOT}"
  exit 2
}

for tool in flock timeout git python3 du realpath; do
  command -v "${tool}" >/dev/null 2>&1 || {
    err "required tool not found: ${tool}"
    exit 2
  }
done

resolve_opencode || {
  err "opencode not found (tried ${OPENCODE_BIN}, /home/tomas/.opencode/bin/opencode and PATH)"
  exit 2
}

for file in "${PROMPT_FILE}" "${SELECTION_SCRIPT}"; do
  [[ -f "${file}" ]] || {
    err "missing ${file}"
    exit 2
  }
done

require_int IDLE_POLL_SECONDS "${IDLE_POLL_SECONDS}" 1
require_int POST_CYCLE_PAUSE "${POST_CYCLE_PAUSE}" 1
require_int FAIL_SLEEP_BASE "${FAIL_SLEEP_BASE}" 1
require_int FAIL_SLEEP_MAX "${FAIL_SLEEP_MAX}" 1
require_int BUSY_SLEEP "${BUSY_SLEEP}" 1
require_int RATE_LIMIT_MIN_SLEEP "${RATE_LIMIT_MIN_SLEEP}" 1
require_int RATE_LIMIT_MAX_SLEEP "${RATE_LIMIT_MAX_SLEEP}" 1
require_int CYCLE_STOP_GRACE "${CYCLE_STOP_GRACE}" 1
require_int CYCLE_DRAIN_TIMEOUT "${CYCLE_DRAIN_TIMEOUT}" 1
require_int GUARD_CHECK_TIMEOUT "${GUARD_CHECK_TIMEOUT}" 1
require_int AUTONOMY_MAX_LOOPS "${AUTONOMY_MAX_LOOPS}" 0
require_int CYCLE_LOG_KEEP_COUNT "${CYCLE_LOG_KEEP_COUNT}" 1
require_int CYCLE_LOG_MAX_AGE_DAYS "${CYCLE_LOG_MAX_AGE_DAYS}" 1
require_int CYCLE_LOG_MAX_BYTES "${CYCLE_LOG_MAX_BYTES}" 1

# Cycle logs must be persistent and private: an absolute path outside the
# repository checkout (never committed or published) and outside the volatile
# runtime directory.
if [[ "${CYCLE_LOG_DIR}" != /* ]]; then
  err "invalid CYCLE_LOG_DIR='${CYCLE_LOG_DIR}' (expected an absolute path)"
  exit 2
fi
log_dir_real="$(realpath -m -- "${CYCLE_LOG_DIR}")"
root_real="$(realpath -m -- "${ROOT}")"
runtime_real="$(realpath -m -- "${XDG_RUNTIME_DIR:-/tmp}")"
tmp_real="$(realpath -m -- "${TMPDIR:-/tmp}")"
for forbidden in "${root_real}" "${runtime_real}" "${tmp_real}"; do
  if [[ "${log_dir_real}" == "${forbidden}" || "${log_dir_real}" == "${forbidden}"/* ]]; then
    err "invalid CYCLE_LOG_DIR='${CYCLE_LOG_DIR}' (must be outside ${forbidden})"
    exit 2
  fi
done

case "${GUARD_CHECK}" in
  1 | on | true) guard=1 ;;
  0 | off | false) guard=0 ;;
  auto)
    if [[ "$(basename "${OPENCODE_BIN}")" == "opencode-tendo-safe" ]]; then
      guard=1
    else
      guard=0
    fi
    ;;
  *)
    err "invalid GUARD_CHECK='${GUARD_CHECK}' (expected auto, 1 or 0)"
    exit 2
    ;;
esac

mkdir -p "${RUNTIME}" 2>/dev/null || {
  err "cannot create runtime directory ${RUNTIME}"
  exit 2
}

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/tendo-autonomy.XXXXXX")" || {
  err "cannot create a temporary directory"
  exit 2
}
cleanup() { rm -rf "${WORK_DIR}"; }
trap cleanup EXIT

# Singleton lock first, then the shared legacy maintenance lock. Either
# conflict exits 75 so the service manager retries after a delay; exiting
# closes fd 9 and therefore releases the singleton lock before the
# maintenance check fails.
exec 9>"${LOCK_FILE}"
if ! flock -n 9; then
  log "another controller holds ${LOCK_FILE}; exiting"
  exit "${BUSY_EXIT}"
fi

exec 8>"${MAINTENANCE_LOCK_FILE}"
if ! flock -n 8; then
  err "legacy maintenance run holds ${MAINTENANCE_LOCK_FILE}; refusing to start"
  exit "${BUSY_EXIT}"
fi

if [[ "${guard}" -eq 1 ]]; then
  guard_label=on
else
  guard_label=off
fi
log "controller started idle_poll=${IDLE_POLL_SECONDS}s opencode=$(basename "${OPENCODE_BIN}") guard=${guard_label} cycle_logs=${CYCLE_LOG_DIR}"

child_pid=""
child_group=0
last_pid=""
stopping=0
cycle=0
loops=0
failure_sleep="${FAIL_SLEEP_BASE}"
selection_sleep="${FAIL_SLEEP_BASE}"

# group_alive <pgid>: true while the process group has a member that can still
# run. Zombies (and dead entries) are ignored on purpose: they hold no
# resources and cannot touch the repository, so a group of only zombies counts
# as gone. Without this, a normally reaped leader's group could look alive for
# a moment and every drain would see a phantom leftover.
#
# AUTONOMY_TEST_GROUP_ALIVE_FILE is a test-only knob (never set in service): as
# long as that file exists every group is reported alive, which lets the
# behavior tests simulate a member that never disappears.
group_alive() {
  local pgid="$1" stat line state rest pgrp
  if [[ -n "${AUTONOMY_TEST_GROUP_ALIVE_FILE:-}" ]]; then
    # Explicit statuses: a bare `return` inside a trap handler inherits the
    # interrupted wait status in bash and would report the wrong answer.
    if [[ -e "${AUTONOMY_TEST_GROUP_ALIVE_FILE}" ]]; then
      return 0
    fi
    return 1
  fi
  for stat in /proc/[0-9]*/stat; do
    [[ -r "${stat}" ]] || continue
    # stderr first: the process may vanish between the glob and the read.
    read -r line 2>/dev/null <"${stat}" || continue
    # /proc/<pid>/stat: "pid (comm) state ppid pgrp ..."; comm may contain
    # spaces and parentheses, so cut through the last ") ".
    line="${line##*) }"
    state="${line%% *}"
    case "${state}" in
      Z | X) continue ;;
    esac
    rest="${line#* }" # ppid pgrp ...
    rest="${rest#* }" # pgrp ...
    pgrp="${rest%% *}"
    [[ "${pgrp}" == "${pgid}" ]] && return 0
  done
  return 1
}

# wait_group_empty <pgid> <grace-seconds>: 0 when the group disappeared.
# Poll ticks are tagged so behavior tests can tell internal polling apart from
# scheduling pauses (POST_CYCLE_PAUSE, backoff, idle poll).
wait_group_empty() {
  local pgid="$1" grace="$2" tick
  for ((tick = 0; tick < grace * 10; tick++)); do
    group_alive "${pgid}" || return 0
    AUTONOMY_POLL_TICK=1 sleep 0.1
  done
  return 1
}

# Terminate the current child (and its process group when it leads one) and
# wait until nothing is left. Safe to call when no child is running. Returns 1
# when live group members survived SIGKILL past the bound: the caller must then
# fail closed and must not wait on the direct child again.
stop_child() {
  local pid="${child_pid}" was_group="${child_group}" leftover=0
  [[ -n "${pid}" ]] || return 0
  child_pid=""
  kill -TERM "${pid}" 2>/dev/null || true
  if [[ "${was_group}" -eq 1 ]]; then
    kill -TERM -- "-${pid}" 2>/dev/null || true
    if ! wait_group_empty "${pid}" "${CYCLE_STOP_GRACE}"; then
      err "child processes ignored SIGTERM for ${CYCLE_STOP_GRACE}s; sending SIGKILL"
      kill -KILL -- "-${pid}" 2>/dev/null || true
      if ! wait_group_empty "${pid}" "${CYCLE_DRAIN_TIMEOUT}"; then
        err "child process group ${pid} still has live members after SIGKILL and ${CYCLE_DRAIN_TIMEOUT}s"
        leftover=1
      fi
    fi
  fi
  # Never block on `wait` once the deadline expired: a process in
  # uninterruptible state would hang the shutdown forever.
  if [[ "${leftover}" -eq 0 ]]; then
    wait "${pid}" 2>/dev/null || true
  fi
  child_group=0
  [[ "${leftover}" -eq 0 ]]
}

on_signal() {
  local name="$1" number="$2"
  if [[ "${stopping}" -eq 1 ]]; then
    # Cleanup is already running. Exit only from the first handler, after the
    # cycle's process group is gone (TERM, grace, KILL, drain); a later signal
    # must never release the locks while children may still be alive.
    err "received SIG${name} while already stopping; deferring to the running cleanup"
    return 0
  fi
  stopping=1
  # Ignore any further shutdown signal for the rest of the cleanup. The cycle's
  # process group is already running with default dispositions, so this only
  # affects the controller itself.
  trap '' HUP INT TERM
  err "received SIG${name}; stopping the running cycle before releasing locks"
  if ! stop_child; then
    # No unbounded wait and no fail-closed exit 70 here: this is a requested
    # stop, so exit with the signal status and let the unit's cgroup kill reap
    # the remnant (KillMode=control-group) before any restart.
    err "shutdown: live cycle group members survived SIGKILL; leaving them to the service manager's cgroup cleanup"
  fi
  cycle_interrupted "SIG${name}" $((number + 128))
  exit $((${number} + 128))
}
trap 'on_signal HUP 1' HUP
trap 'on_signal INT 2' INT
trap 'on_signal TERM 15' TERM

# run_child <cmd...>: plain background child (sleep); killed by pid.
run_child() {
  local rc
  "$@" 8>&- 9>&- </dev/null &
  child_pid=$!
  child_group=0
  last_pid="${child_pid}"
  wait "${child_pid}"
  rc=$?
  child_pid=""
  return "${rc}"
}

# run_child_group <cmd...>: the command leads its own process group (anything
# wrapped in `timeout`, which calls setpgid(0,0)); killed as a group.
run_child_group() {
  local rc
  "$@" 8>&- 9>&- </dev/null &
  child_pid=$!
  child_group=1
  last_pid="${child_pid}"
  wait "${child_pid}"
  rc=$?
  child_pid=""
  child_group=0
  return "${rc}"
}

# run_capture_group <stdout-file> <stderr-file> <cmd...>
run_capture_group() {
  local out="$1" errf="$2"
  shift 2
  local rc
  "$@" >"${out}" 2>"${errf}" 8>&- 9>&- </dev/null &
  child_pid=$!
  child_group=1
  last_pid="${child_pid}"
  wait "${child_pid}"
  rc=$?
  child_pid=""
  child_group=0
  return "${rc}"
}

pause_for() { run_child sleep "$1"; }

# Wait until every process of the finished cycle's group is gone, so the next
# GitHub poll can never overlap with a leftover child of the session.
drain_group() {
  local pgid="$1" waited=0
  [[ -n "${pgid}" ]] || return 0
  group_alive "${pgid}" || return 0
  err "cycle ${cycle} left background processes; waiting up to ${CYCLE_DRAIN_TIMEOUT}s for them to finish"
  child_pid="${pgid}"
  child_group=1
  while group_alive "${pgid}"; do
    if ((waited >= CYCLE_DRAIN_TIMEOUT)); then
      err "cycle ${cycle} drain timeout; terminating the remaining process group"
      kill -TERM -- "-${pgid}" 2>/dev/null || true
      if ! wait_group_empty "${pgid}" "${CYCLE_STOP_GRACE}"; then
        err "cycle ${cycle} background processes ignored SIGTERM; sending SIGKILL"
        kill -KILL -- "-${pgid}" 2>/dev/null || true
        if ! wait_group_empty "${pgid}" "${CYCLE_DRAIN_TIMEOUT}"; then
          fail_closed "cycle ${cycle} process group ${pgid} still has live members after SIGKILL and ${CYCLE_DRAIN_TIMEOUT}s"
        fi
      fi
      break
    fi
    # Tagged: internal drain polls are not scheduling pauses.
    AUTONOMY_POLL_TICK=1 sleep 1
    waited=$((waited + 1))
  done
  child_pid=""
  child_group=0
}

# --- private per-cycle diagnostics -------------------------------------------

# private_dir <path>: create (or tighten) a directory only the controller user
# can read. Refuses symlinks and directories owned by someone else.
private_dir() {
  local dir="$1"
  (umask 077 && mkdir -p -- "${dir}") 2>/dev/null || return 1
  [[ -d "${dir}" && ! -L "${dir}" && -O "${dir}" ]] || return 1
  chmod 0700 -- "${dir}" 2>/dev/null
}

# file_bytes <file>: size in bytes, 0 when unreadable.
file_bytes() {
  local size
  size="$(stat -c %s -- "$1" 2>/dev/null)" || size=0
  printf '%s' "${size:-0}"
}

# prune_cycle_logs: bounded retention. Cycle directories are ordered newest
# first by their UTC-stamped name; the newest is always kept, older ones are
# removed beyond CYCLE_LOG_KEEP_COUNT, CYCLE_LOG_MAX_AGE_DAYS or a cumulative
# CYCLE_LOG_MAX_BYTES. Only controller-named, non-symlink directories directly
# inside CYCLE_LOG_DIR are ever removed. Failure is logged, never fatal.
prune_cycle_logs() {
  local entries=() dir index=0 total=0 bytes mtime now cutoff removed=0
  [[ -d "${CYCLE_LOG_DIR}" && ! -L "${CYCLE_LOG_DIR}" ]] || return 0
  for dir in "${CYCLE_LOG_DIR}"/[0-9]*T[0-9]*Z-c[0-9]*-p[0-9]*; do
    [[ -d "${dir}" && ! -L "${dir}" ]] || continue
    entries+=("${dir}")
  done
  ((${#entries[@]} > 0)) || return 0
  now="$(date +%s)"
  cutoff=$((now - CYCLE_LOG_MAX_AGE_DAYS * 86400))
  while IFS= read -r dir; do
    bytes="$(du -sb -- "${dir}" 2>/dev/null)" || bytes=0
    bytes="${bytes%%[[:space:]]*}"
    [[ "${bytes}" =~ ^[0-9]+$ ]] || bytes=0
    total=$((total + bytes))
    mtime="$(stat -c %Y -- "${dir}" 2>/dev/null)" || mtime="${now}"
    if ((index > 0)) && ((index >= CYCLE_LOG_KEEP_COUNT || mtime < cutoff || total > CYCLE_LOG_MAX_BYTES)); then
      if rm -rf -- "${dir}" 2>/dev/null; then
        removed=$((removed + 1))
      else
        err "cycle log retention: cannot remove ${dir}"
      fi
      total=$((total - bytes))
    fi
    index=$((index + 1))
  done < <(printf '%s\n' "${entries[@]}" | sort -r)
  if ((removed > 0)); then
    log "cycle log retention: removed ${removed} old cycle log director$( ((removed == 1)) && echo y || echo ies)"
  fi
  return 0
}

# prepare_cycle_logs: creates this cycle's private directory and empty 0600
# files; sets cycle_log_dir. Returns 1 (nothing started) on any failure.
prepare_cycle_logs() {
  local dir file
  cycle_log_dir=""
  private_dir "${CYCLE_LOG_DIR}" || return 1
  dir="${CYCLE_LOG_DIR}/$(date -u +%Y%m%dT%H%M%SZ)-c${cycle}-p$$"
  (umask 077 && mkdir -m 0700 -- "${dir}") 2>/dev/null || return 1
  for file in stdout.log stderr.log status; do
    (umask 077 && : >"${dir}/${file}") 2>/dev/null || return 1
    [[ -f "${dir}/${file}" && ! -L "${dir}/${file}" ]] || return 1
    chmod 0600 -- "${dir}/${file}" 2>/dev/null || return 1
  done
  cycle_log_dir="${dir}"
}

# record_cycle_status <key=value>...: append to this cycle's status file.
record_cycle_status() {
  [[ -n "${cycle_log_dir}" ]] || return 1
  printf '%s\n' "$@" >>"${cycle_log_dir}/status" 2>/dev/null
}

cycle_log_dir=""
cycle_started=0
cycle_running=0

run_cycle() { # <prompt> <decision line>
  local prompt="$1" decision="$2" rc started finished duration
  cycle=$((cycle + 1))
  cycle_started=0
  if ! prepare_cycle_logs ||
    ! record_cycle_status "cycle=${cycle}" "controller_pid=$$" \
      "decision=${decision}" "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"; then
    # Never run a session whose output could not be preserved.
    err "cycle ${cycle} not started: cannot create private diagnostics under ${CYCLE_LOG_DIR}"
    return 1
  fi
  cycle_started=1
  cycle_running=1
  log "cycle ${cycle} start: ${decision}"
  log "cycle ${cycle} diagnostics: ${cycle_log_dir}"
  started="$(date +%s)"
  # Plain redirection into the private files: `timeout` stays the background
  # job and process-group leader, and its exit status is the cycle status.
  run_capture_group "${cycle_log_dir}/stdout.log" "${cycle_log_dir}/stderr.log" \
    timeout --signal=TERM --kill-after="${CYCLE_KILL_AFTER}" "${CYCLE_TIMEOUT}" \
    "${OPENCODE_BIN}" run --auto --agent orchestrator \
    --title "Tendo autonomy cycle ${cycle}" \
    "${prompt}"
  rc=$?
  drain_group "${last_pid}"
  cycle_running=0
  finished="$(date +%s)"
  duration=$((finished - started))
  if ! record_cycle_status "finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "duration_seconds=${duration}" "exit_code=${rc}"; then
    err "cycle ${cycle} could not record its exit status in ${cycle_log_dir}/status"
  fi
  local summary
  summary="cycle ${cycle} exit rc=${rc} duration=${duration}s"
  summary+=" stdout=$(file_bytes "${cycle_log_dir}/stdout.log")B"
  summary+=" stderr=$(file_bytes "${cycle_log_dir}/stderr.log")B diagnostics=${cycle_log_dir}"
  if [[ "${rc}" -eq 0 ]]; then
    log "${summary}"
  else
    err "${summary}"
  fi
  prune_cycle_logs
  return "${rc}"
}

cycle_failed() {
  local next
  err "retrying after ${failure_sleep}s"
  pause_for "${failure_sleep}"
  next=$((failure_sleep * 2))
  if ((next > FAIL_SLEEP_MAX)); then
    next="${FAIL_SLEEP_MAX}"
  fi
  failure_sleep="${next}"
}

selection_failed() { # <rc> <stderr-file>
  local rc="$1" errf="$2" line next
  err "selection failed rc=${rc}; backing off ${selection_sleep}s (not treated as idle)"
  if [[ -s "${errf}" ]]; then
    while IFS= read -r line; do
      err "selection stderr: ${line}"
    done < <(tail -n 5 "${errf}")
  fi
  pause_for "${selection_sleep}"
  next=$((selection_sleep * 2))
  if ((next > FAIL_SLEEP_MAX)); then
    next="${FAIL_SLEEP_MAX}"
  fi
  selection_sleep="${next}"
}

CHECK_OUT="${WORK_DIR}/guard-check.out"
CHECK_ERR="${WORK_DIR}/guard-check.err"
SELECT_OUT="${WORK_DIR}/selection.out"
SELECT_ERR="${WORK_DIR}/selection.err"

while :; do
  if ((AUTONOMY_MAX_LOOPS > 0 && loops >= AUTONOMY_MAX_LOOPS)); then
    log "max loops reached"
    exit 0
  fi
  loops=$((loops + 1))

  # 1. A pending cycle marker means the safe wrapper recorded an interrupted
  #    session. Only the wrapper can finish it, so no new work is selected.
  if [[ -e "${PENDING_MARKER}" ]]; then
    if [[ "${guard}" -eq 0 ]]; then
      err "pending marker ${PENDING_MARKER} present but no safe wrapper; refusing to start new work"
      pause_for "${FAIL_SLEEP_MAX}"
      continue
    fi
    prompt="$(cat "${PROMPT_FILE}")"
    prompt+=$'\n\n'"## Controller work selection"
    prompt+=$'\n'"${RECOVERY_DECISION}"
    run_cycle "${prompt}" "${RECOVERY_DECISION}"
    rc=$?
    if [[ "${cycle_started}" -eq 0 ]]; then
      cycle_failed
      continue
    fi
    case "${rc}" in
      0)
        log "cycle ${cycle} finished (wrapper recovery)"
        failure_sleep="${FAIL_SLEEP_BASE}"
        pause_for "${POST_CYCLE_PAUSE}"
        ;;
      78)
        err "RECOVERY BLOCKED: the safe wrapper cannot resolve the pending cycle; inspect ${PENDING_MARKER} (opencode-tendo-safe --checkpoint) — no new work starts until it is resolved"
        pause_for "${FAIL_SLEEP_MAX}"
        ;;
      *)
        err "cycle ${cycle} recovery failed rc=${rc}"
        cycle_failed
        ;;
    esac
    continue
  fi

  # 2. Local busy check: no GitHub traffic while a Tendo session is active or
  #    the canonical checkout cannot take a new session.
  if [[ "${guard}" -eq 1 ]]; then
    run_capture_group "${CHECK_OUT}" "${CHECK_ERR}" \
      timeout "${GUARD_CHECK_TIMEOUT}" "${OPENCODE_BIN}" --check
    check_rc=$?
    check_text="$(cat "${CHECK_OUT}" 2>/dev/null || true)"
    if [[ -s "${CHECK_ERR}" ]]; then
      check_text+=$'\n'"$(cat "${CHECK_ERR}" 2>/dev/null || true)"
    fi
    sessions="$(printf '%s\n' "${check_text}" |
      sed -n 's/^Active Tendo sessions:[[:space:]]*\([0-9][0-9]*\).*$/\1/p' | head -n 1)"
    if [[ "${check_rc}" -eq 0 ]]; then
      : # ready
    elif [[ -n "${sessions}" ]] && ((sessions > 0)); then
      err "busy: ${sessions} active Tendo session(s); not polling GitHub"
      pause_for "${BUSY_SLEEP}"
      continue
    elif printf '%s\n' "${check_text}" | grep -q '^Pending marker: True'; then
      log "wrapper reports a pending cycle marker; re-checking before selecting work"
      pause_for 1
      continue
    elif printf '%s\n' "${check_text}" | grep -q '^Repository ready: False'; then
      err "canonical checkout not ready (detached/merge in progress); waiting"
      pause_for "${BUSY_SLEEP}"
      continue
    else
      err "guard check failed rc=${check_rc}; not polling GitHub"
      if [[ -s "${CHECK_ERR}" ]]; then
        while IFS= read -r line; do
          err "guard check: ${line}"
        done < <(tail -n 5 "${CHECK_ERR}")
      fi
      cycle_failed
      continue
    fi
  fi

  # 3. Selection: the only place GitHub is read.
  run_capture_group "${SELECT_OUT}" "${SELECT_ERR}" \
    timeout "${SELECTION_TIMEOUT}" python3 "${SELECTION_SCRIPT}"
  selection_rc=$?
  selection="$(cat "${SELECT_OUT}" 2>/dev/null || true)"
  first_line="$(printf '%s\n' "${selection}" | head -n 1)"

  if [[ "${selection_rc}" -eq 0 && "${first_line}" == DECISION\ * ]]; then
    priority="$(printf '%s' "${first_line}" | sed -n 's/^DECISION priority=\([^ ]*\).*$/\1/p')"
    if [[ -z "${priority}" ]]; then
      selection_failed 0 "${SELECT_ERR}"
      continue
    fi
    if [[ "${priority}" == "idle" ]]; then
      reason="$(printf '%s\n' "${selection}" | grep '^REASON ' | head -n 1)"
      [[ -n "${reason}" ]] || reason="REASON (none given)"
      selection_sleep="${FAIL_SLEEP_BASE}"
      log "idle: ${reason} ; next poll in ${IDLE_POLL_SECONDS}s"
      pause_for "${IDLE_POLL_SECONDS}"
      continue
    fi
    if [[ ! "${priority}" =~ ^[1-8]$ ]]; then
      selection_failed 0 "${SELECT_ERR}"
      continue
    fi
    selection_sleep="${FAIL_SLEEP_BASE}"
  elif [[ "${selection_rc}" -eq 4 ]]; then
    reset_epoch="$(sed -n 's/.*RATE_LIMITED reset=\([0-9][0-9]*\).*/\1/p' "${SELECT_ERR}" 2>/dev/null | tail -n 1)"
    if [[ -n "${reset_epoch}" ]]; then
      now_epoch="$(date +%s)"
      rate_sleep=$((reset_epoch - now_epoch + 5))
      if ((rate_sleep < RATE_LIMIT_MIN_SLEEP)); then
        rate_sleep="${RATE_LIMIT_MIN_SLEEP}"
      elif ((rate_sleep > RATE_LIMIT_MAX_SLEEP)); then
        rate_sleep="${RATE_LIMIT_MAX_SLEEP}"
      fi
    else
      rate_sleep="${selection_sleep}"
    fi
    log "GitHub rate limited; sleeping ${rate_sleep}s"
    pause_for "${rate_sleep}"
    continue
  else
    selection_failed "${selection_rc}" "${SELECT_ERR}"
    continue
  fi

  # 4. One session per non-idle decision.
  prompt="$(cat "${PROMPT_FILE}")"
  prompt+=$'\n\n'"## Controller work selection for this cycle"
  prompt+=$'\n'"Advisory snapshot taken before this session by scripts/autonomy-work-selection.py; re-verify live state, then follow it unless reality differs."
  prompt+=$'\n\n'"${selection}"

  run_cycle "${prompt}" "${first_line}"
  rc=$?
  if [[ "${cycle_started}" -eq 0 ]]; then
    cycle_failed
    continue
  fi
  case "${rc}" in
    0)
      log "cycle ${cycle} finished"
      failure_sleep="${FAIL_SLEEP_BASE}"
      pause_for "${POST_CYCLE_PAUSE}"
      ;;
    124 | 137)
      err "cycle ${cycle} timed out after ${CYCLE_TIMEOUT}"
      cycle_failed
      ;;
    78)
      err "cycle ${cycle} blocked by the safe wrapper (rc=78); pending work preserved"
      pause_for "${FAIL_SLEEP_MAX}"
      ;;
    *)
      err "cycle ${cycle} failed rc=${rc}"
      cycle_failed
      ;;
  esac
done
