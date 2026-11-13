#!/usr/bin/env bash
# Behavior tests for scripts/autonomy-controller.sh (Tendo evergreen autonomy).
#
# Everything is offline: each case runs the real controller under `env -i` in a
# disposable directory tree with fake `opencode`, `sleep`, `gh` and `systemctl`
# executables on PATH plus a scripted selection stub. Nothing can reach GitHub,
# the real user systemd or a real OpenCode session:
#   - `opencode` records every invocation and answers --check from state files,
#   - `sleep` records the requested duration and returns immediately (cases
#     that need a genuinely blocking pause opt in via FAKE_SLEEP_BLOCK=300);
#     the controller's internal poll ticks (AUTONOMY_POLL_TICK) are logged
#     separately, so scheduling-pause assertions never race with a drain poll,
#   - `gh` / `systemctl` only record that they were (wrongly) called.
set -uo pipefail

SOURCE_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
failures=0
holders=()
HOLDER=""

cleanup() {
  local pid
  if ((${#holders[@]} > 0)); then
    for pid in "${holders[@]}"; do kill "${pid}" 2>/dev/null || true; done
  fi
  # A blocking fake sleep outlives `kill -9` of the controller; kill the ones
  # the fake recorded before it exec'd the real sleep.
  # shellcheck disable=SC2046 # word splitting over the recorded pids is intended
  for pid in $(cat "${WORK}"/*/state/sleep-pids 2>/dev/null || true); do
    kill -KILL "${pid}" 2>/dev/null || true
  done
  pkill -KILL -f "${WORK}/" 2>/dev/null || true
  rm -rf "${WORK}"
}
trap cleanup EXIT

pass() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s: %s\n' "$1" "$2" >&2
  failures=$((failures + 1))
}

# --- fakes -------------------------------------------------------------------

make_fakes() {
  local bin="$1"
  mkdir -p "${bin}"

  # The wrapper and the raw binary share one implementation; the basename
  # decides GUARD_CHECK=auto, exactly like on the host.
  cat >"${bin}/opencode" <<'EOF'
#!/usr/bin/env bash
set -u
state="${FAKE_STATE}"
if [[ "${1:-}" == "--check" ]]; then
  count=$(( $(cat "${state}/check.calls" 2>/dev/null || echo 0) + 1 ))
  echo "${count}" >"${state}/check.calls"
  printf '%s\n' "$@" >"${state}/check-args.${count}"
  echo check >>"${state}/all.calls"
  for fd in 8 9; do
    if [[ -e "/proc/$$/fd/${fd}" ]]; then
      echo open >"${state}/check-fd${fd}.${count}"
    else
      echo closed >"${state}/check-fd${fd}.${count}"
    fi
  done
  if [[ -f "${state}/check.out.${count}" ]]; then
    cat "${state}/check.out.${count}"
  elif [[ -f "${state}/check.out" ]]; then
    cat "${state}/check.out"
  else
    printf 'Repository ready: True\nPending marker: False\nActive Tendo sessions: 0\nGUARD CHECK PASSED\n'
  fi
  if [[ -f "${state}/checkrc.${count}" ]]; then
    exit "$(cat "${state}/checkrc.${count}")"
  fi
  if [[ -f "${state}/checkrc" ]]; then
    exit "$(cat "${state}/checkrc")"
  fi
  exit 0
fi

count=$(( $(cat "${state}/run.calls" 2>/dev/null || echo 0) + 1 ))
echo "${count}" >"${state}/run.calls"
echo run >>"${state}/all.calls"
printf '%s\n' "$@" >"${state}/args.${count}"
printf '%s' "${@: -1}" >"${state}/prompt.${count}"
for fd in 8 9; do
  if [[ -e "/proc/$$/fd/${fd}" ]]; then
    echo open >"${state}/fd${fd}.${count}"
  else
    echo closed >"${state}/fd${fd}.${count}"
  fi
done
if flock -n "${XDG_RUNTIME_DIR}/tendo-autonomous-${UID}.lock" true; then
  echo free >"${state}/controller-lock.${count}"
else
  echo held >"${state}/controller-lock.${count}"
fi
if flock -n "${XDG_RUNTIME_DIR}/tendo-maintenance-${UID}.lock" true; then
  echo free >"${state}/maintenance-lock.${count}"
else
  echo held >"${state}/maintenance-lock.${count}"
fi
if [[ -x "${state}/hook" ]]; then
  "${state}/hook" "${count}"
fi
if [[ -f "${state}/rc.${count}" ]]; then
  exit "$(cat "${state}/rc.${count}")"
fi
exit 0
EOF

  # Sleeps are observed, not waited for. FAKE_SLEEP_BLOCK lists durations that
  # must really elapse (space separated), so signal handling and the drain poll
  # can be tested against a pause that is genuinely in progress.
  # The controller tags its internal poll ticks (grace/drain polling) with
  # AUTONOMY_POLL_TICK; they go to their own log so an assertion on the
  # scheduling pauses can never race with a timing-dependent drain tick.
  cat >"${bin}/sleep" <<'EOF'
#!/usr/bin/env bash
state="${FAKE_STATE}"
if [[ -n "${AUTONOMY_POLL_TICK:-}" ]]; then
  printf '%s\n' "${1:-0}" >>"${state}/poll-sleeps"
else
  printf '%s\n' "${1:-0}" >>"${state}/sleeps"
fi
# shellcheck disable=SC2086 # a space separated duration list is intended
for duration in ${FAKE_SLEEP_BLOCK:-}; do
  if [[ "${duration}" == "${1:-0}" ]]; then
    echo "$$" >>"${state}/sleep-pids"
    exec /bin/sleep "$1"
  fi
done
exit 0
EOF

  cat >"${bin}/gh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FAKE_STATE}/gh-calls"
exit 1
EOF

  cat >"${bin}/systemctl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FAKE_STATE}/systemctl-calls"
exit 1
EOF

  chmod +x "${bin}/opencode" "${bin}/gh" "${bin}/systemctl" "${bin}/sleep"
  cp "${bin}/opencode" "${bin}/opencode-tendo-safe"
  chmod +x "${bin}/opencode-tendo-safe"
}

# Scripted selection stub: per-call stdout / stderr / exit code from
# $FAKE_STATE/selout.N, selerr.N, selrc.N (or the unsuffixed defaults).
# It also records whether the run marker and the drain marker existed at the
# moment it was called, which is how ordering is asserted.
write_selection_stub() {
  cat >"$1" <<'EOF'
#!/usr/bin/env python3
"""Scripted work-selection stub for the autonomy controller tests."""
import os
import pathlib
import sys

state = pathlib.Path(os.environ["FAKE_STATE"])
count_file = state / "sel.calls"
count = int(count_file.read_text().strip() or "0") if count_file.exists() else 0
count += 1
count_file.write_text("%d\n" % count)
for marker in ("run-finished", "drain-marker"):
    record = state / ("sel.%s.%d" % (marker, count))
    record.write_text("yes\n" if (state / marker).exists() else "no\n")


def pick(prefix):
    for name in ("%s.%d" % (prefix, count), prefix):
        path = state / name
        if path.exists():
            return path
    return None


stderr_file = pick("selerr")
if stderr_file is not None:
    sys.stderr.write(stderr_file.read_text())
stdout_file = pick("selout")
if stdout_file is not None:
    sys.stdout.write(stdout_file.read_text())
else:
    sys.stdout.write("DECISION priority=idle kind=none target=-\nREASON stub idle\n")
rc_file = pick("selrc")
sys.exit(int(rc_file.read_text().strip()) if rc_file is not None else 0)
EOF
}

# --- case scaffolding --------------------------------------------------------

new_case() {
  CASE="${WORK}/$1"
  REPO="${CASE}/repo"
  STATE="${CASE}/state"
  RUNTIME="${CASE}/run"
  TMP="${CASE}/tmp"
  OUT_FILE="${CASE}/out"
  mkdir -p "${REPO}/scripts" "${REPO}/.autonomous" "${STATE}" "${RUNTIME}" "${TMP}"
  make_fakes "${CASE}/bin"
  cp "${SOURCE_ROOT}/scripts/autonomy-controller.sh" "${REPO}/scripts/"
  printf 'STUB AUTONOMY PROMPT line one\nsecond prompt line\n' \
    >"${REPO}/.autonomous/AUTONOMY_PROMPT.md"
  write_selection_stub "${REPO}/scripts/autonomy-work-selection.py"
  echo "V4 done" >"${REPO}/.autonomous/V4_COMPLETE"
  git -C "${REPO}" init -q -b main
  git -C "${REPO}" add -A
  git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m init
  env_args=(
    HOME="${CASE}"
    PATH="${CASE}/bin:/usr/local/bin:/usr/bin:/bin"
    UID="${UID}"
    TENDO_ROOT="${REPO}"
    XDG_RUNTIME_DIR="${RUNTIME}"
    TMPDIR="${TMP}"
    FAKE_STATE="${STATE}"
    OPENCODE_BIN="${CASE}/bin/opencode"
    PROMPT_FILE="${REPO}/.autonomous/AUTONOMY_PROMPT.md"
    SELECTION_SCRIPT="${REPO}/scripts/autonomy-work-selection.py"
    PENDING_MARKER="${STATE}/pending.marker"
  )
}

hook() {
  printf '#!/usr/bin/env bash\nset -u\ncd %q\n%s\n' "${REPO}" "$1" >"${STATE}/hook"
  chmod +x "${STATE}/hook"
}

check_hook() {
  printf '#!/usr/bin/env bash\nset -u\n%s\n' "$1" >"${STATE}/check-hook"
  chmod +x "${STATE}/check-hook"
}

run_controller() {
  env -i "${env_args[@]}" "$@" timeout 20 bash "${REPO}/scripts/autonomy-controller.sh" \
    >"${OUT_FILE}" 2>&1
  RC=$?
}

start_controller_bg() {
  ( exec env -i "${env_args[@]}" "$@" bash "${REPO}/scripts/autonomy-controller.sh" \
    >"${OUT_FILE}" 2>&1 ) &
  CTRL_PID=$!
}

# --- assertions --------------------------------------------------------------

expect_eq() { # <name> <want> <got>
  if [[ "$2" == "$3" ]]; then
    pass "$1"
  else
    fail "$1" "want [$2] got [$3]; output: $(tail -n 8 "${OUT_FILE}" 2>/dev/null)"
  fi
}

expect_grep() { # <name> <pattern> <file>
  if grep -q -- "$2" "$3" 2>/dev/null; then
    pass "$1"
  else
    fail "$1" "missing [$2] in $(basename "$3")"
  fi
}

expect_absent_grep() { # <name> <pattern> <file>
  if grep -q -- "$2" "$3" 2>/dev/null; then
    fail "$1" "unexpected [$2] in $(basename "$3")"
  else
    pass "$1"
  fi
}

sleep_log() {
  if [[ -f "${STATE}/sleeps" ]]; then
    tr '\n' ' ' <"${STATE}/sleeps" | sed 's/ *$//'
  fi
}

# Internal poll ticks (controller grace/drain polling) are recorded separately
# by the fake sleep; they are not scheduling decisions.
poll_log() {
  if [[ -f "${STATE}/poll-sleeps" ]]; then
    tr '\n' ' ' <"${STATE}/poll-sleeps" | sed 's/ *$//'
  fi
}

sel_calls() { cat "${STATE}/sel.calls" 2>/dev/null || echo 0; }
run_calls() { cat "${STATE}/run.calls" 2>/dev/null || echo 0; }
check_calls() { cat "${STATE}/check.calls" 2>/dev/null || echo 0; }

expect_prompt_prefix() { # <name> <prompt-file>
  local expected="${CASE}/expected-prefix" size
  printf '%s' "$(cat "${REPO}/.autonomous/AUTONOMY_PROMPT.md")" >"${expected}"
  size=$(stat -c %s "${expected}")
  if cmp -s -n "${size}" "${expected}" "$2"; then
    pass "$1"
  else
    fail "$1" "session prompt does not start with the byte-exact AUTONOMY_PROMPT"
  fi
}

expect_arg_line() { # <name> <args-file> <exact line>
  if grep -qx -- "$3" "$2"; then
    pass "$1"
  else
    fail "$1" "argument [$3] missing from $(tr '\n' ' ' <"$2")"
  fi
}

wait_for() { # <file> [tenths]
  local _
  for _ in $(seq "${2:-100}"); do
    [[ -e "$1" ]] && return 0
    sleep 0.1
  done
  return 1
}

wait_for_text() { # <text> <file> [tenths]
  local _
  for _ in $(seq "${3:-150}"); do
    grep -q -- "$1" "$2" 2>/dev/null && return 0
    sleep 0.1
  done
  return 1
}

pid_alive() { [[ -n "${1:-}" ]] && kill -0 "$1" 2>/dev/null; }

wait_gone() { # <pid> [tenths]
  local _
  for _ in $(seq "${2:-30}"); do
    pid_alive "$1" || return 0
    sleep 0.1
  done
  return 1
}

lock_free() { # <lock file>
  flock -n "$1" true
}

hold_lock() { # <lock file>
  flock "$1" sleep 30 &
  HOLDER=$!
  holders+=("${HOLDER}")
  local _
  for _ in $(seq 50); do
    lock_free "$1" || return 0
    sleep 0.1
  done
  return 1
}

release_lock() {
  kill "${HOLDER}" 2>/dev/null || true
  wait "${HOLDER}" 2>/dev/null || true
}

# --- 1. idle never starts a session -----------------------------------------

new_case idle
run_controller AUTONOMY_MAX_LOOPS=3
name="idle: three loops, no session, one default poll per loop"
expect_eq "${name}" 0 "${RC}"
expect_eq "idle: no opencode run call" 0 "$(run_calls)"
expect_eq "idle: selection runs once per loop" 3 "$(sel_calls)"
expect_eq "idle: IDLE_POLL_SECONDS=300 is requested each loop" "300 300 300" "$(sleep_log)"
expect_grep "idle: idle decision is logged with its REASON" 'idle: REASON stub idle ; next poll in 300s' "${OUT_FILE}"
if [[ -e "${STATE}/check.calls" ]]; then
  fail "idle: guard check skipped without a safe wrapper" "wrapper --check was called"
else
  pass "idle: guard check skipped without a safe wrapper"
fi
if [[ -e "${STATE}/gh-calls" || -e "${STATE}/systemctl-calls" ]]; then
  fail "idle: the controller never calls gh or systemctl" "$(cat "${STATE}/gh-calls" "${STATE}/systemctl-calls" 2>/dev/null)"
else
  pass "idle: the controller never calls gh or systemctl"
fi

new_case idle-17
run_controller AUTONOMY_MAX_LOOPS=3 IDLE_POLL_SECONDS=17
expect_eq "idle: IDLE_POLL_SECONDS=17 is honoured" "17 17 17" "$(sleep_log)"

# --- 2./4. one session for one decision, locks not inherited ------------------

new_case work
printf 'DECISION priority=6 kind=claim-issue target=issue #900 Example work\nREASON highest priority claimable issue\nNOTE advisory only\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook 'touch "${FAKE_STATE}/run-finished"'
run_controller AUTONOMY_MAX_LOOPS=2

name="work: exactly one opencode run for one claim decision"
expect_eq "${name}" 0 "${RC}"
expect_eq "work: one session started" 1 "$(run_calls)"
expect_eq "work: selection runs again only for the next loop" 2 "$(sel_calls)"
expect_arg_line "work: run subcommand" "${STATE}/args.1" "run"
expect_arg_line "work: --auto" "${STATE}/args.1" "--auto"
expect_arg_line "work: orchestrator agent" "${STATE}/args.1" "orchestrator"
expect_arg_line "work: no --continue on a fresh session" "${STATE}/args.1" "--title"
if grep -qx -- '--continue' "${STATE}/args.1" || grep -q -- '--session' "${STATE}/args.1"; then
  fail "work: never resumes an existing session" "$(tr '\n' ' ' <"${STATE}/args.1")"
else
  pass "work: never resumes an existing session"
fi
expect_grep "work: cycle title carries the cycle number" '^Tendo autonomy cycle 1$' "${STATE}/args.1"
expect_prompt_prefix "work: prompt starts with the byte-exact AUTONOMY_PROMPT" "${STATE}/prompt.1"
expect_grep "work: prompt carries the selection decision" 'DECISION priority=6 kind=claim-issue target=issue #900' "${STATE}/prompt.1"
expect_grep "work: prompt carries the selection advisory header" 'Controller work selection for this cycle' "${STATE}/prompt.1"
expect_grep "work: prompt carries the full selection output" 'NOTE advisory only' "${STATE}/prompt.1"
expect_grep "work: cycle start is logged with the decision" 'cycle 1 start: DECISION priority=6' "${OUT_FILE}"
expect_eq "work: POST_CYCLE_PAUSE=30 then the idle poll" "30 300" "$(sleep_log)"
expect_eq "work: cycle lock is held during the run" held "$(cat "${STATE}/controller-lock.1" 2>/dev/null)"
expect_eq "work: maintenance lock is held during the run" held "$(cat "${STATE}/maintenance-lock.1" 2>/dev/null)"
expect_eq "work: fd 8 is not inherited by the session" closed "$(cat "${STATE}/fd8.1" 2>/dev/null)"
expect_eq "work: fd 9 is not inherited by the session" closed "$(cat "${STATE}/fd9.1" 2>/dev/null)"
expect_eq "work: selection after the cycle sees it finished" yes "$(cat "${STATE}/sel.run-finished.2" 2>/dev/null)"
expect_eq "work: selection before the cycle does not" no "$(cat "${STATE}/sel.run-finished.1" 2>/dev/null)"
if [[ -e "${STATE}/gh-calls" || -e "${STATE}/systemctl-calls" ]]; then
  fail "work: the controller never calls gh or systemctl" "$(cat "${STATE}/gh-calls" "${STATE}/systemctl-calls" 2>/dev/null)"
else
  pass "work: the controller never calls gh or systemctl"
fi

# --- 3. no GitHub poll while a session is running ----------------------------

new_case no-poll
printf 'DECISION priority=6 kind=claim-issue target=issue #903 Blocking work\nREASON blocking\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook 'touch "${FAKE_STATE}/job-started"
for _ in $(seq 400); do
  [[ -e "${FAKE_STATE}/release-job" ]] && break
  /bin/sleep 0.05
done'
start_controller_bg AUTONOMY_MAX_LOOPS=2
name="no-poll: no selection call while the session runs"
if wait_for "${STATE}/job-started"; then
  during_before=$(sel_calls)
  sleep 0.6
  during_after=$(sel_calls)
  touch "${STATE}/release-job"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  if [[ "${during_before}" == 1 && "${during_after}" == 1 ]]; then
    pass "${name}"
  else
    fail "${name}" "selection ran during the job (${during_before} -> ${during_after})"
  fi
  expect_eq "no-poll: the second selection happens after the release" 2 "$(sel_calls)"
  expect_eq "no-poll: controller exits 0 at the loop bound" 0 "${RC}"
  if [[ -e "${STATE}/gh-calls" ]]; then
    fail "no-poll: the controller never calls gh directly" "$(cat "${STATE}/gh-calls")"
  else
    pass "no-poll: the controller never calls gh directly"
  fi
else
  kill "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the session never started"
fi

# --- 5. completion markers never stop the controller ------------------------

new_case markers
echo "V5 done" >"${REPO}/.autonomous/V5_COMPLETE"
echo "V6 done" >"${REPO}/.autonomous/V6_COMPLETE"
git -C "${REPO}" add -A
git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m "markers"
run_controller AUTONOMY_MAX_LOOPS=3
expect_eq "markers: tracked completion markers never stop the controller" 0 "${RC}"
expect_eq "markers: three loops still select work" 3 "$(sel_calls)"
expect_eq "markers: no session for an idle backlog" 0 "$(run_calls)"
expect_grep "markers: the loop stops only at its test bound" 'max loops reached' "${OUT_FILE}"

# A completed release still permits a new unit of maintenance/recovery work.
new_case post-release-work
echo "complete" >"${REPO}/.autonomous/V99_COMPLETE"
printf 'DECISION priority=6 kind=review-owned-dependencies target=issue #900 existing claimant only\nNOTE Never authorises takeover\n' >"${STATE}/selout.1"
printf 'DECISION priority=7 kind=dependency-pr target=pr #901 maintenance after release\n' >"${STATE}/selout.2"
printf 'DECISION priority=idle kind=none target=-\n' >"${STATE}/selout.3"
run_controller AUTONOMY_MAX_LOOPS=3
expect_eq "post-release: controller remains operational" 0 "${RC}"
expect_eq "post-release: recovery and maintenance sessions both run" 2 "$(run_calls)"
expect_eq "post-release: idle polling continues afterward" 3 "$(sel_calls)"
expect_grep "post-release: advisory kind passed unchanged" 'kind=review-owned-dependencies' "${STATE}/prompt.1"
expect_grep "post-release: no automatic claim permission added" 'Never authorises takeover' "${STATE}/prompt.1"
expect_grep "post-release: dependency maintenance remains available" 'kind=dependency-pr' "${STATE}/prompt.2"

# --- 6. selection failures back off and are never idle ----------------------

new_case selection-error
for n in 1 2 3; do
  echo 2 >"${STATE}/selrc.${n}"
  echo "SELECTION ERROR gh: network down ${n}" >"${STATE}/selerr.${n}"
done
run_controller AUTONOMY_MAX_LOOPS=3
expect_eq "selection-error: controller keeps running" 0 "${RC}"
expect_eq "selection-error: no session is ever started" 0 "$(run_calls)"
expect_eq "selection-error: one selection attempt per loop" 3 "$(sel_calls)"
expect_eq "selection-error: backoff doubles 120, 240, 480" "120 240 480" "$(sleep_log)"
expect_grep "selection-error: the failure is logged with its rc" 'selection failed rc=2' "${OUT_FILE}"
expect_grep "selection-error: it is not treated as idle" 'not treated as idle' "${OUT_FILE}"
expect_grep "selection-error: the stderr tail is logged" 'network down 1' "${OUT_FILE}"
expect_absent_grep "selection-error: never logged as idle" 'idle: REASON' "${OUT_FILE}"

new_case selection-garbage
echo "this is not a decision" >"${STATE}/selout.1"
run_controller AUTONOMY_MAX_LOOPS=1
expect_eq "garbage: rc 0 without a DECISION line is a failure" 0 "$(run_calls)"
expect_eq "garbage: it backs off like a failure" "120" "$(sleep_log)"
expect_grep "garbage: logged as a selection failure" 'selection failed rc=0' "${OUT_FILE}"

new_case selection-recover
echo 2 >"${STATE}/selrc.1"
echo 2 >"${STATE}/selrc.2"
printf 'DECISION priority=idle kind=none target=-\nREASON recovered\n' >"${STATE}/selout.3"
printf 'DECISION priority=idle kind=none target=-\nREASON recovered\n' >"${STATE}/selout.4"
run_controller AUTONOMY_MAX_LOOPS=4
expect_eq "selection-recover: success resets the failure backoff" "120 240 300 300" "$(sleep_log)"

new_case selection-cap
for n in 1 2 3; do echo 2 >"${STATE}/selrc.${n}"; done
run_controller AUTONOMY_MAX_LOOPS=3 FAIL_SLEEP_BASE=500 FAIL_SLEEP_MAX=900
expect_eq "selection-cap: the failure backoff is capped at FAIL_SLEEP_MAX" "500 900 900" "$(sleep_log)"

# --- 7. GitHub rate limiting ------------------------------------------------

now=$(date +%s)
new_case rate-limit
echo 4 >"${STATE}/selrc.1"
printf 'RATE_LIMITED reset=%s\n' "$((now + 100))" >"${STATE}/selerr.1"
run_controller AUTONOMY_MAX_LOOPS=1
rl_sleep=$(sleep_log)
expect_eq "rate-limit: no session is started" 0 "$(run_calls)"
expect_grep "rate-limit: the wait is logged" 'GitHub rate limited; sleeping' "${OUT_FILE}"
if [[ "${rl_sleep}" =~ ^[0-9]+$ ]] && ((rl_sleep >= 100 && rl_sleep <= 110)); then
  pass "rate-limit: sleeps until the reset plus a small margin"
else
  fail "rate-limit: sleeps until the reset plus a small margin" "slept [${rl_sleep}]"
fi

new_case rate-limit-past
echo 4 >"${STATE}/selrc.1"
printf 'RATE_LIMITED reset=%s\n' "$((now - 1000))" >"${STATE}/selerr.1"
run_controller AUTONOMY_MAX_LOOPS=1
expect_eq "rate-limit-past: reset in the past clamps to the minimum" "60" "$(sleep_log)"

new_case rate-limit-huge
echo 4 >"${STATE}/selrc.1"
printf 'RATE_LIMITED reset=%s\n' "$((now + 100000))" >"${STATE}/selerr.1"
run_controller AUTONOMY_MAX_LOOPS=1
expect_eq "rate-limit-huge: capped at the maximum" "3600" "$(sleep_log)"

new_case rate-limit-no-reset
echo 4 >"${STATE}/selrc.1"
echo "RATE_LIMITED without a reset value" >"${STATE}/selerr.1"
run_controller AUTONOMY_MAX_LOOPS=1
expect_eq "rate-limit-no-reset: falls back to the failure backoff" "120" "$(sleep_log)"
expect_eq "rate-limit-no-reset: still no session" 0 "$(run_calls)"

# --- 8. pending wrapper marker takes priority -------------------------------

new_case pending-blocked
touch "${STATE}/pending.marker"
echo 78 >"${STATE}/rc.1"
echo 78 >"${STATE}/rc.2"
run_controller AUTONOMY_MAX_LOOPS=2 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "pending-blocked: two recovery attempts, one per loop" 2 "$(run_calls)"
expect_eq "pending-blocked: the selector is never consulted" 0 "$(sel_calls)"
expect_eq "pending-blocked: each block pauses FAIL_SLEEP_MAX" "1800 1800" "$(sleep_log)"
expect_grep "pending-blocked: RECOVERY BLOCKED is logged" 'RECOVERY BLOCKED' "${OUT_FILE}"
expect_grep "pending-blocked: the marker is named for inspection" "${STATE}/pending.marker" "${OUT_FILE}"
expect_grep "pending-blocked: the recovery decision is in the prompt" 'kind=wrapper-recovery' "${STATE}/prompt.1"
expect_grep "pending-blocked: recovery prompt uses its own header" '## Controller work selection' "${STATE}/prompt.1"
expect_prompt_prefix "pending-blocked: recovery prompt keeps the byte-exact base prompt" "${STATE}/prompt.1"
expect_arg_line "pending-blocked: the second loop is a second cycle" "${STATE}/args.2" "Tendo autonomy cycle 2"

new_case pending-recovery
touch "${STATE}/pending.marker"
run_controller AUTONOMY_MAX_LOOPS=1 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "pending-recovery: one recovery session" 1 "$(run_calls)"
expect_eq "pending-recovery: no selector call" 0 "$(sel_calls)"
expect_eq "pending-recovery: POST_CYCLE_PAUSE after a successful recovery" "30" "$(sleep_log)"
expect_grep "pending-recovery: the cycle is logged as a recovery" 'finished (wrapper recovery)' "${OUT_FILE}"

new_case pending-no-wrapper
touch "${STATE}/pending.marker"
run_controller AUTONOMY_MAX_LOOPS=2
expect_eq "pending-no-wrapper: no opencode call at all" 0 "$(run_calls)"
if [[ -e "${STATE}/all.calls" ]]; then
  fail "pending-no-wrapper: the raw binary is never invoked" "$(cat "${STATE}/all.calls")"
else
  pass "pending-no-wrapper: the raw binary is never invoked"
fi
expect_eq "pending-no-wrapper: refuses and backs off" "1800 1800" "$(sleep_log)"
expect_grep "pending-no-wrapper: the refusal is logged" 'present but no safe wrapper; refusing to start new work' "${OUT_FILE}"

# --- 9. wrapper busy / not ready / failing ----------------------------------

new_case guard-busy
printf 'Repository ready: True\nPending marker: False\nActive Tendo sessions: 2\n  ses_1\n  ses_2\n' \
  >"${STATE}/check.out.1"
echo 2 >"${STATE}/checkrc.1"
printf 'Repository ready: True\nPending marker: False\nActive Tendo sessions: 0\nGUARD CHECK PASSED\n' \
  >"${STATE}/check.out.2"
run_controller AUTONOMY_MAX_LOOPS=2 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "guard-busy: no selection while sessions are active" 1 "$(sel_calls)"
expect_eq "guard-busy: no session started" 0 "$(run_calls)"
expect_eq "guard-busy: --check runs once per loop" 2 "$(check_calls)"
expect_eq "guard-busy: BUSY_SLEEP then the idle poll" "60 300" "$(sleep_log)"
expect_grep "guard-busy: the busy state is logged" 'busy: 2 active Tendo session(s); not polling GitHub' "${OUT_FILE}"
expect_eq "guard-busy: check does not inherit lock fd 9" closed "$(cat "${STATE}/check-fd9.1" 2>/dev/null)"

new_case guard-pending
printf 'Repository ready: True\nPending marker: True\nActive Tendo sessions: 0\n' >"${STATE}/check.out"
echo 2 >"${STATE}/checkrc"
run_controller AUTONOMY_MAX_LOOPS=2 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "guard-pending: no selection while the wrapper reports a marker" 0 "$(sel_calls)"
expect_eq "guard-pending: no session started" 0 "$(run_calls)"
expect_eq "guard-pending: short re-check pause, never a busy loop" "1 1" "$(sleep_log)"

new_case guard-not-ready
printf 'Repository ready: False\nPending marker: False\nActive Tendo sessions: 0\n' >"${STATE}/check.out"
echo 2 >"${STATE}/checkrc"
run_controller AUTONOMY_MAX_LOOPS=1 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "guard-not-ready: selection is skipped" 0 "$(sel_calls)"
expect_eq "guard-not-ready: waits BUSY_SLEEP" "60" "$(sleep_log)"
expect_grep "guard-not-ready: the reason is logged" 'canonical checkout not ready' "${OUT_FILE}"

new_case guard-failed
echo "GUARD CHECK FAILED: internal error" >"${STATE}/check.out"
echo 2 >"${STATE}/checkrc"
run_controller AUTONOMY_MAX_LOOPS=2 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "guard-failed: no GitHub poll after a guard failure" 0 "$(sel_calls)"
expect_eq "guard-failed: failure backoff doubles" "120 240" "$(sleep_log)"
expect_grep "guard-failed: the failure is logged" 'guard check failed rc=2' "${OUT_FILE}"

# --- 10. one controller, one legacy maintenance run -------------------------

new_case two-controllers
printf 'DECISION priority=6 kind=claim-issue target=issue #904 Held work\nREASON held\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook 'touch "${FAKE_STATE}/job-started"
for _ in $(seq 400); do
  [[ -e "${FAKE_STATE}/release-job" ]] && break
  /bin/sleep 0.05
done'
start_controller_bg AUTONOMY_MAX_LOOPS=1
name="two-controllers: the second controller exits 75 without any call"
if wait_for "${STATE}/job-started"; then
  mkdir -p "${CASE}/state-b"
  OUT_FILE="${CASE}/out-b"
  started=$(date +%s)
  run_controller AUTONOMY_MAX_LOOPS=1 FAKE_STATE="${CASE}/state-b"
  elapsed=$(( $(date +%s) - started ))
  OUT_FILE="${CASE}/out"
  if [[ "${RC}" == 75 && "${elapsed}" -le 10 ]]; then
    pass "${name}"
  else
    fail "${name}" "rc=${RC} elapsed=${elapsed}s output: $(cat "${CASE}/out-b")"
  fi
  expect_grep "two-controllers: the second controller explains itself" 'another controller holds' "${CASE}/out-b"
  if [[ -e "${CASE}/state-b/sel.calls" || -e "${CASE}/state-b/all.calls" ]]; then
    fail "two-controllers: the second controller makes no calls" "state-b is not empty"
  else
    pass "two-controllers: the second controller makes no calls"
  fi
  touch "${STATE}/release-job"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  expect_eq "two-controllers: the first controller finishes normally" 0 "${RC}"
else
  kill "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the first controller never started a session"
fi

new_case maintenance-lock
hold_lock "${RUNTIME}/tendo-maintenance-${UID}.lock"
run_controller AUTONOMY_MAX_LOOPS=1
release_lock
expect_eq "maintenance-lock: refuses with exit 75" 75 "${RC}"
expect_grep "maintenance-lock: the legacy run is named" 'legacy maintenance run holds' "${OUT_FILE}"
expect_eq "maintenance-lock: no selection call" 0 "$(sel_calls)"
expect_eq "maintenance-lock: no opencode call" 0 "$(run_calls)"
if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock"; then
  pass "maintenance-lock: the singleton lock is released on exit"
else
  fail "maintenance-lock: the singleton lock is released on exit" "still held"
fi

# --- 11. signals ------------------------------------------------------------

cat >"${WORK}/stubborn" <<'EOF'
#!/usr/bin/env bash
trap '' TERM
while :; do /bin/sleep 1; done
EOF
chmod +x "${WORK}/stubborn"

new_case sigterm-cycle
cp "${WORK}/stubborn" "${CASE}/stubborn"
printf 'DECISION priority=6 kind=claim-issue target=issue #905 Long work\nREASON long\n' \
  >"${STATE}/selout.1"
hook '"${FAKE_STATE}/../stubborn" &
echo $! >"${FAKE_STATE}/grandchild-pid"
touch "${FAKE_STATE}/cycle-started"
while :; do /bin/sleep 0.1; done'
start_controller_bg CYCLE_STOP_GRACE=1 CYCLE_TIMEOUT=300
name="sigterm-cycle: TERM stops the cycle and exits 143"
if wait_for "${STATE}/cycle-started"; then
  grandchild=$(cat "${STATE}/grandchild-pid")
  started=$(date +%s)
  kill -TERM "${CTRL_PID}"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  elapsed=$(( $(date +%s) - started ))
  expect_eq "${name}" 143 "${RC}"
  if ((elapsed <= 6)); then
    pass "sigterm-cycle: exits within CYCLE_STOP_GRACE+5s"
  else
    fail "sigterm-cycle: exits within CYCLE_STOP_GRACE+5s" "took ${elapsed}s"
  fi
  if wait_gone "${grandchild}"; then
    pass "sigterm-cycle: the TERM-ignoring grandchild is gone"
  else
    fail "sigterm-cycle: the TERM-ignoring grandchild is gone" "pid ${grandchild} alive"
  fi
  expect_grep "sigterm-cycle: the stop is logged" 'received SIGTERM; stopping the running cycle before releasing locks' "${OUT_FILE}"
  expect_grep "sigterm-cycle: the stubborn group is killed" 'sending SIGKILL' "${OUT_FILE}"
  if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock" &&
    lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
    pass "sigterm-cycle: both locks are free after exit"
  else
    fail "sigterm-cycle: both locks are free after exit" "a lock is still held"
  fi
else
  kill "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the session never started"
fi

new_case sigterm-idle
start_controller_bg FAKE_SLEEP_BLOCK=300 IDLE_POLL_SECONDS=300
name="sigterm-idle: TERM interrupts the idle pause"
if wait_for_text '^300$' "${STATE}/sleeps"; then
  started=$(date +%s)
  kill -TERM "${CTRL_PID}"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  elapsed=$(( $(date +%s) - started ))
  expect_eq "${name}" 143 "${RC}"
  if ((elapsed <= 5)); then
    pass "sigterm-idle: exits promptly instead of sleeping out the pause"
  else
    fail "sigterm-idle: exits promptly instead of sleeping out the pause" "took ${elapsed}s"
  fi
  if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock"; then
    pass "sigterm-idle: the singleton lock is free afterwards"
  else
    fail "sigterm-idle: the singleton lock is free afterwards" "still held"
  fi
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the idle pause never started"
fi

# --- 12. a second shutdown signal never releases the locks early --------------

new_case double-sigterm
cp "${WORK}/stubborn" "${CASE}/stubborn"
printf 'DECISION priority=6 kind=claim-issue target=issue #907 Long work\nREASON long\n' \
  >"${STATE}/selout.1"
hook '"${FAKE_STATE}/../stubborn" &
echo $! >"${FAKE_STATE}/grandchild-pid"
touch "${FAKE_STATE}/cycle-started"
while :; do /bin/sleep 0.1; done'
# FAKE_SLEEP_BLOCK makes the grace ticks really elapse, so the second TERM
# lands while the controller is still cleaning up (no fragile sleep needed:
# the first stop is detected through its log line).
start_controller_bg CYCLE_STOP_GRACE=2 CYCLE_TIMEOUT=300 FAKE_SLEEP_BLOCK=0.1
name="double-sigterm: both locks stay held after a second TERM until the cycle group is gone"
if wait_for "${STATE}/cycle-started"; then
  grandchild=$(cat "${STATE}/grandchild-pid")
  kill -TERM "${CTRL_PID}"
  # Barrier: the first stop is visible in the log before its cleanup starts, so
  # the second TERM is delivered while the controller is killing the group.
  if wait_for_text 'received SIGTERM; stopping the running cycle before releasing locks' "${OUT_FILE}"; then
    kill -TERM "${CTRL_PID}"
    # Sample until the TERM-ignoring grandchild is gone: the controller must
    # stay alive and keep both locks exclusively held for that whole window.
    early_exit=""
    early_release=""
    for _ in $(seq 120); do
      pid_alive "${grandchild}" || break
      if ! pid_alive "${CTRL_PID}"; then
        early_exit=1
        break
      fi
      if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock"; then
        early_release=singleton
        break
      fi
      if lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
        early_release=maintenance
        break
      fi
      sleep 0.05
    done
    if [[ -n "${early_exit}" ]]; then
      fail "${name}" "the controller exited while the TERM-ignoring grandchild was still alive"
    elif [[ -n "${early_release}" ]]; then
      fail "${name}" "the ${early_release} lock was free while the grandchild was still alive"
    elif pid_alive "${grandchild}"; then
      fail "${name}" "the grandchild was still alive after the sampling window"
    else
      pass "${name}"
    fi
    RC=0
    wait "${CTRL_PID}" || RC=$?
    expect_eq "double-sigterm: exits 143 after the cleanup" 143 "${RC}"
    if wait_gone "${grandchild}"; then
      pass "double-sigterm: the TERM-ignoring grandchild is KILLed"
    else
      fail "double-sigterm: the TERM-ignoring grandchild is KILLed" "pid ${grandchild} alive"
    fi
    expect_grep "double-sigterm: the stubborn group is killed" 'sending SIGKILL' "${OUT_FILE}"
    if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock" &&
      lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
      pass "double-sigterm: both locks are free after exit"
    else
      fail "double-sigterm: both locks are free after exit" "a lock is still held"
    fi
  else
    kill -KILL "${CTRL_PID}" 2>/dev/null || true
    wait "${CTRL_PID}" 2>/dev/null || true
    fail "${name}" "the stop was never logged"
  fi
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the session never started"
fi

# A signal that arrives while the controller is draining a leftover cycle group
# must still stop the process; the drain then ends at the grace/KILL bound
# instead of running out CYCLE_DRAIN_TIMEOUT.
new_case sigterm-drain
cp "${WORK}/stubborn" "${CASE}/stubborn"
printf 'DECISION priority=6 kind=claim-issue target=issue #908 Leftover work\nREASON leftover\n' \
  >"${STATE}/selout.1"
hook '"${FAKE_STATE}/../stubborn" &
echo $! >"${FAKE_STATE}/grandchild-pid"'
start_controller_bg CYCLE_STOP_GRACE=1 CYCLE_DRAIN_TIMEOUT=300 FAKE_SLEEP_BLOCK=1
name="sigterm-drain: TERM during the drain stops it at the grace bound, not CYCLE_DRAIN_TIMEOUT"
if wait_for "${STATE}/grandchild-pid" &&
  wait_for_text 'left background processes' "${OUT_FILE}"; then
  grandchild=$(cat "${STATE}/grandchild-pid")
  started=$(date +%s)
  kill -TERM "${CTRL_PID}"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  elapsed=$(( $(date +%s) - started ))
  expect_eq "${name}" 143 "${RC}"
  if ((elapsed <= 6)); then
    pass "sigterm-drain: shutdown during the drain stays within the grace bound"
  else
    fail "sigterm-drain: shutdown during the drain stays within the grace bound" "took ${elapsed}s"
  fi
  expect_absent_grep "sigterm-drain: the 300s drain timeout never has to fire" 'drain timeout' "${OUT_FILE}"
  expect_grep "sigterm-drain: the stubborn group is killed" 'sending SIGKILL' "${OUT_FILE}"
  if wait_gone "${grandchild}"; then
    pass "sigterm-drain: the leftover TERM-ignoring child is gone"
  else
    fail "sigterm-drain: the leftover TERM-ignoring child is gone" "pid ${grandchild} alive"
  fi
  if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock" &&
    lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
    pass "sigterm-drain: both locks are free after exit"
  else
    fail "sigterm-drain: both locks are free after exit" "a lock is still held"
  fi
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the controller never entered the drain"
fi

# A shutdown must stay bounded when the group never disappears: no `wait` on a
# child that survived SIGKILL, and no fail-closed hang.
new_case fail-closed-shutdown
printf 'DECISION priority=6 kind=claim-issue target=issue #913 Stuck work\nREASON stuck\n' \
  >"${STATE}/selout.1"
hook 'touch "${FAKE_STATE}/cycle-started"
while :; do /bin/sleep 0.1; done'
touch "${STATE}/group-never-gone"
start_controller_bg CYCLE_STOP_GRACE=1 CYCLE_DRAIN_TIMEOUT=1 \
  AUTONOMY_TEST_GROUP_ALIVE_FILE="${STATE}/group-never-gone"
name="fail-closed-shutdown: shutdown stays bounded when the group never disappears"
if wait_for "${STATE}/cycle-started"; then
  started=$(date +%s)
  kill -TERM "${CTRL_PID}"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  elapsed=$(( $(date +%s) - started ))
  expect_eq "${name}" 143 "${RC}"
  if ((elapsed <= 6)); then
    pass "fail-closed-shutdown: no unbounded wait after the expired deadline"
  else
    fail "fail-closed-shutdown: no unbounded wait after the expired deadline" "took ${elapsed}s"
  fi
  expect_grep "fail-closed-shutdown: the surviving group is named" \
    'still has live members after SIGKILL' "${OUT_FILE}"
  expect_grep "fail-closed-shutdown: the shutdown leftovers are logged" \
    'live cycle group members survived SIGKILL' "${OUT_FILE}"
  expect_grep "fail-closed-shutdown: the stubborn group is killed" 'sending SIGKILL' "${OUT_FILE}"
  if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock" &&
    lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
    pass "fail-closed-shutdown: both locks are free after exit"
  else
    fail "fail-closed-shutdown: both locks are free after exit" "a lock is still held"
  fi
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the session never started"
fi

# --- 13. cycle timeout ------------------------------------------------------

new_case cycle-timeout
hook 'echo $$ >"${FAKE_STATE}/cycle-pid"
/bin/sleep 30'
printf 'DECISION priority=6 kind=claim-issue target=issue #901 Slow work\nREASON slow\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
run_controller AUTONOMY_MAX_LOOPS=2 CYCLE_TIMEOUT=2s CYCLE_KILL_AFTER=1s
expect_eq "cycle-timeout: the loop continues after the timeout" 0 "${RC}"
expect_eq "cycle-timeout: the second loop selects again" 2 "$(sel_calls)"
expect_grep "cycle-timeout: the timeout is logged" 'cycle 1 timed out after 2s' "${OUT_FILE}"
expect_eq "cycle-timeout: failure backoff follows the timeout" "120 300" "$(sleep_log)"
# A leftover member of the timed-out group can trigger a drain poll before the
# retry; those tagged ticks must never be mistaken for the backoff sequence.
if [[ -f "${STATE}/poll-sleeps" ]] && grep -qvxE '0\.1|1' "${STATE}/poll-sleeps"; then
  fail "cycle-timeout: drain polls are tagged apart from the backoff" "$(poll_log)"
else
  pass "cycle-timeout: drain polls are tagged apart from the backoff"
fi
if pid_alive "$(cat "${STATE}/cycle-pid")"; then
  fail "cycle-timeout: the timed-out process group is gone" "the session pid is still alive"
else
  pass "cycle-timeout: the timed-out process group is gone"
fi

# --- 14. drain of leftover session children ---------------------------------

new_case drain
printf 'DECISION priority=6 kind=claim-issue target=issue #902 Slow child\nREASON slow child\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook '( /bin/sleep 2; : >"${FAKE_STATE}/drain-marker" ) &
touch "${FAKE_STATE}/job-started"'
run_controller AUTONOMY_MAX_LOOPS=2 FAKE_SLEEP_BLOCK=1
expect_eq "drain: the loop survives the leftover child" 0 "${RC}"
expect_eq "drain: the second selection still happens" 2 "$(sel_calls)"
expect_eq "drain: the first selection runs before the child exists" no "$(cat "${STATE}/sel.drain-marker.1" 2>/dev/null)"
expect_eq "drain: no selection before the leftover child ended" yes "$(cat "${STATE}/sel.drain-marker.2" 2>/dev/null)"
expect_grep "drain: waiting for background processes is logged" 'left background processes' "${OUT_FILE}"

# The drain itself is bounded: after CYCLE_DRAIN_TIMEOUT the leftover group is
# terminated instead of waiting for it forever.
new_case drain-timeout
printf 'DECISION priority=6 kind=claim-issue target=issue #909 Leftover child\nREASON leftover child\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook '( /bin/sleep 30 ) &
echo $! >"${FAKE_STATE}/leftover-pid"
touch "${FAKE_STATE}/job-started"'
run_controller AUTONOMY_MAX_LOOPS=2 CYCLE_DRAIN_TIMEOUT=1 CYCLE_STOP_GRACE=1 FAKE_SLEEP_BLOCK=1
expect_eq "drain-timeout: the controller keeps running after the bound" 0 "${RC}"
expect_eq "drain-timeout: the next loop still selects" 2 "$(sel_calls)"
expect_grep "drain-timeout: the wait is logged" 'left background processes' "${OUT_FILE}"
expect_grep "drain-timeout: the bound is logged" 'drain timeout; terminating the remaining process group' "${OUT_FILE}"
if wait_gone "$(cat "${STATE}/leftover-pid")"; then
  pass "drain-timeout: the leftover child is terminated after the bound"
else
  fail "drain-timeout: the leftover child is terminated after the bound" "pid $(cat "${STATE}/leftover-pid") alive"
fi
expect_eq "drain-timeout: the drain poll is not a scheduling pause" "30 300" "$(sleep_log)"
if grep -qx '1' "${STATE}/poll-sleeps" 2>/dev/null &&
  ! grep -qvxE '0\.1|1' "${STATE}/poll-sleeps"; then
  pass "drain-timeout: drain polls are recorded separately"
else
  fail "drain-timeout: drain polls are recorded separately" "poll ticks: $(poll_log)"
fi

# A live member that survives SIGKILL past the drain deadline must never be
# followed by another poll or a new cycle: the controller fails closed.
new_case fail-closed-drain
printf 'DECISION priority=6 kind=claim-issue target=issue #911 Stuck work\nREASON stuck\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook 'touch "${FAKE_STATE}/job-started"'
touch "${STATE}/group-never-gone"
run_controller AUTONOMY_MAX_LOOPS=2 CYCLE_DRAIN_TIMEOUT=1 CYCLE_STOP_GRACE=1 \
  AUTONOMY_TEST_GROUP_ALIVE_FILE="${STATE}/group-never-gone"
expect_eq "fail-closed-drain: a live leftover after SIGKILL exits 70" 70 "${RC}"
expect_eq "fail-closed-drain: no second selection after the failure" 1 "$(sel_calls)"
expect_eq "fail-closed-drain: only the one cycle ran" 1 "$(run_calls)"
expect_grep "fail-closed-drain: the drain bound is logged" \
  'drain timeout; terminating the remaining process group' "${OUT_FILE}"
expect_grep "fail-closed-drain: the surviving group is named" \
  'still has live members after SIGKILL' "${OUT_FILE}"
expect_grep "fail-closed-drain: the fail-closed exit is logged" 'fail-closed:' "${OUT_FILE}"
if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock" &&
  lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
  pass "fail-closed-drain: both locks are free after the fail-closed exit"
else
  fail "fail-closed-drain: both locks are free after the fail-closed exit" "a lock is still held"
fi

# Zombies are not live members: a group whose only remaining member is a zombie
# must not look like a leftover (otherwise fail-closed would fire on ordinary
# cycles). The zombie is created by a helper outside the cycle group: its
# exited child moved itself into the cycle's group and stays unreaped while the
# helper sleeps.
new_case zombie-only
printf 'DECISION priority=6 kind=claim-issue target=issue #912 Zombie work\nREASON zombie\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook 'ps -o pgid= -p $$ | tr -d " " >"${FAKE_STATE}/cycle-pgid.tmp" &&
mv "${FAKE_STATE}/cycle-pgid.tmp" "${FAKE_STATE}/cycle-pgid"
for _ in $(seq 200); do
  [[ -e "${FAKE_STATE}/zombie-ready" ]] && break
  /bin/sleep 0.05
done'
cat >"${CASE}/zombie-helper.py" <<'EOF'
import os
import sys
import time

pgid_file, ready_file, pid_file = sys.argv[1:4]
pgid = 0
for _ in range(1000):
    text = ""
    try:
        text = open(pgid_file).read().strip()
    except OSError:
        text = ""
    if text:
        pgid = int(text)
        break
    time.sleep(0.02)
if pgid == 0:
    open(pid_file, "w").write("FAIL no cycle pgid")
    open(ready_file, "w").close()
    sys.exit(1)
pid = os.fork()
if pid == 0:
    try:
        os.setpgid(0, pgid)
        with open(pid_file, "w") as fh:
            fh.write("%d %d" % (os.getpid(), os.getpgid(0)))
    except OSError as exc:
        with open(pid_file, "w") as fh:
            fh.write("FAIL %s" % exc)
    os._exit(0)
for _ in range(500):
    state = ""
    try:
        with open("/proc/%d/stat" % pid) as fh:
            state = fh.read().split(") ", 1)[1][:1]
    except OSError:
        state = ""
    if state == "Z":
        break
    time.sleep(0.01)
open(ready_file, "w").close()
time.sleep(30)
EOF
python3 "${CASE}/zombie-helper.py" "${STATE}/cycle-pgid" "${STATE}/zombie-ready" \
  "${STATE}/zombie-pid" &
ZOMBIE_HELPER=$!
holders+=("${ZOMBIE_HELPER}")
run_controller AUTONOMY_MAX_LOOPS=2
zombie_pid=$(cut -d' ' -f1 "${STATE}/zombie-pid" 2>/dev/null)
zombie_pgid=$(cut -d' ' -f2 "${STATE}/zombie-pid" 2>/dev/null)
zombie_state=$(ps -o stat= -p "${zombie_pid:-0}" 2>/dev/null | tr -d ' ')
kill "${ZOMBIE_HELPER}" 2>/dev/null || true
wait "${ZOMBIE_HELPER}" 2>/dev/null || true
expect_eq "zombie-only: the zombie really was in the cycle's process group" \
  "$(cat "${STATE}/cycle-pgid" 2>/dev/null)" "${zombie_pgid}"
expect_eq "zombie-only: the remaining member really was a zombie" Z "${zombie_state:0:1}"
expect_eq "zombie-only: a zombie-only group never trips fail-closed" 0 "${RC}"
expect_absent_grep "zombie-only: no drain wait is logged" 'left background processes' "${OUT_FILE}"
expect_eq "zombie-only: both loops still select" 2 "$(sel_calls)"

# --- 15. crash recovery and restart -----------------------------------------

new_case cycle-crash
printf 'DECISION priority=6 kind=claim-issue target=issue #906 Crashing work\nREASON crashing\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
echo 1 >"${STATE}/rc.1"
run_controller AUTONOMY_MAX_LOOPS=2
expect_eq "cycle-crash: one failed session, then a fresh selection" 1 "$(run_calls)"
expect_eq "cycle-crash: the next loop selects before any new session" 2 "$(sel_calls)"
expect_grep "cycle-crash: the failure is logged with its rc" 'cycle 1 failed rc=1' "${OUT_FILE}"
expect_eq "cycle-crash: backoff 120 then idle poll" "120 300" "$(sleep_log)"

new_case restart-after-kill
start_controller_bg FAKE_SLEEP_BLOCK=300 IDLE_POLL_SECONDS=300
name="restart-after-kill: a new controller starts after kill -9"
if wait_for_text '^300$' "${STATE}/sleeps"; then
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock"; then
    pass "${name}: the kernel released the singleton lock"
  else
    fail "${name}: the kernel released the singleton lock" "still held"
  fi
  OUT_FILE="${CASE}/out-2"
  before=$(sel_calls)
  run_controller AUTONOMY_MAX_LOOPS=1
  expect_eq "${name}" 0 "${RC}"
  expect_grep "restart-after-kill: the new controller is running" 'controller started' "${CASE}/out-2"
  expect_eq "restart-after-kill: the new controller does its own selection" \
    "$((before + 1))" "$(sel_calls)"
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  fail "${name}" "the idle pause never started"
fi

# --- 16. the wrapper check is the only per-loop opencode call ---------------

new_case idle-guard
run_controller AUTONOMY_MAX_LOOPS=5 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
expect_eq "idle-guard: five idle loops start no session" 0 "$(run_calls)"
expect_eq "idle-guard: --check runs exactly once per loop" 5 "$(check_calls)"
expect_eq "idle-guard: selection runs once per loop" 5 "$(sel_calls)"
expect_eq "idle-guard: every loop polls at IDLE_POLL_SECONDS" "300 300 300 300 300" "$(sleep_log)"
if [[ "$(grep -c '^check$' "${STATE}/all.calls" 2>/dev/null)" == 5 ]]; then
  pass "idle-guard: the only opencode invocations are checks"
else
  fail "idle-guard: the only opencode invocations are checks" "$(cat "${STATE}/all.calls" 2>/dev/null)"
fi

# --- startup validation -----------------------------------------------------

new_case invalid-env
run_controller AUTONOMY_MAX_LOOPS=1 IDLE_POLL_SECONDS=abc
expect_eq "invalid-env: a non-numeric poll interval is refused" 2 "${RC}"
expect_eq "invalid-env: no selection call before the refusal" 0 "$(sel_calls)"
if [[ -e "${STATE}/all.calls" ]]; then
  fail "invalid-env: no opencode call before the refusal" "$(cat "${STATE}/all.calls")"
else
  pass "invalid-env: no opencode call before the refusal"
fi

new_case missing-prompt
rm "${REPO}/.autonomous/AUTONOMY_PROMPT.md"
run_controller AUTONOMY_MAX_LOOPS=1
expect_eq "missing-prompt: a missing prompt file is refused" 2 "${RC}"
expect_eq "missing-prompt: no selection call" 0 "$(sel_calls)"

# --- 17. concise journal, private per-cycle diagnostics ----------------------

# Default location: $HOME/.local/state/tendo-autonomy/cycles (HOME=${CASE}).
cycle_logs_root() { printf '%s' "${CYCLE_LOGS:-${CASE}/.local/state/tendo-autonomy/cycles}"; }
cycle_dirs() {
  local root
  root=$(cycle_logs_root)
  find "${root}" -mindepth 1 -maxdepth 1 -type d -name '*T*Z-c*-p*' 2>/dev/null | sort
}
cycle_dir_count() { cycle_dirs | grep -c . || true; }
mode_of() { stat -c %a -- "$1" 2>/dev/null; }

# Noisy session output: never in the journal, byte-exact in private files.
new_case logs-noisy
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #920 Noisy work\nREASON noisy\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
python3 - "${STATE}/expected-stdout" "${STATE}/expected-stderr" <<'EOF'
import sys
out = b"".join(b"NOISY-STDOUT diff line %d +added -removed\n" % i for i in range(3000))
out += b"token-stream progress \r\x1b[2K\xff\xfe binary tail without newline"
err = b"".join(b"NOISY-STDERR tool output %d ghp_FAKEsecretTOKEN\n" % i for i in range(500))
open(sys.argv[1], "wb").write(out)
open(sys.argv[2], "wb").write(err)
EOF
hook 'cat "${FAKE_STATE}/expected-stdout"
cat "${FAKE_STATE}/expected-stderr" >&2
readlink "/proc/$$/fd/1" >"${FAKE_STATE}/session-fd1"
readlink "/proc/$$/fd/2" >"${FAKE_STATE}/session-fd2"
pgid=$(ps -o pgid= -p $$ | tr -d " ")
ps -o comm= -p "${pgid}" >"${FAKE_STATE}/session-leader-comm"
ps -o pgid= -p "${pgid}" | tr -d " " >"${FAKE_STATE}/session-leader-pgid"
echo "${pgid}" >"${FAKE_STATE}/session-pgid"'
run_controller AUTONOMY_MAX_LOOPS=2
expect_eq "logs-noisy: controller runs normally" 0 "${RC}"
expect_eq "logs-noisy: exactly one cycle directory" 1 "$(cycle_dir_count)"
cdir=$(cycle_dirs | head -n 1)
expect_absent_grep "logs-noisy: session stdout never reaches the journal" 'NOISY-STDOUT' "${OUT_FILE}"
expect_absent_grep "logs-noisy: session stderr never reaches the journal" 'NOISY-STDERR' "${OUT_FILE}"
expect_absent_grep "logs-noisy: secrets in session output never reach the journal" 'ghp_FAKE' "${OUT_FILE}"
if cmp -s "${STATE}/expected-stdout" "${cdir}/stdout.log"; then
  pass "logs-noisy: stdout is preserved byte-for-byte"
else
  fail "logs-noisy: stdout is preserved byte-for-byte" "stdout.log differs ($(stat -c %s "${cdir}/stdout.log" 2>/dev/null) bytes)"
fi
if cmp -s "${STATE}/expected-stderr" "${cdir}/stderr.log"; then
  pass "logs-noisy: stderr is preserved byte-for-byte"
else
  fail "logs-noisy: stderr is preserved byte-for-byte" "stderr.log differs"
fi
expect_eq "logs-noisy: session stdout is the private file, not a pipe" "${cdir}/stdout.log" "$(cat "${STATE}/session-fd1" 2>/dev/null)"
expect_eq "logs-noisy: session stderr is the private file, not a pipe" "${cdir}/stderr.log" "$(cat "${STATE}/session-fd2" 2>/dev/null)"
expect_eq "logs-noisy: the session's process group has a live leader" \
  "$(cat "${STATE}/session-pgid" 2>/dev/null)" "$(cat "${STATE}/session-leader-pgid" 2>/dev/null)"
expect_eq "logs-noisy: timeout itself leads the session's process group" timeout "$(cat "${STATE}/session-leader-comm" 2>/dev/null)"
expect_eq "logs-noisy: state tree directory is private" 700 "$(mode_of "${CASE}/.local/state/tendo-autonomy")"
expect_eq "logs-noisy: cycles directory is private" 700 "$(mode_of "$(cycle_logs_root)")"
expect_eq "logs-noisy: cycle directory is private" 700 "$(mode_of "${cdir}")"
for f in stdout.log stderr.log status; do
  expect_eq "logs-noisy: ${f} is 0600" 600 "$(mode_of "${cdir}/${f}")"
done
if [[ "$(basename "${cdir}")" =~ ^[0-9]{8}T[0-9]{6}Z-c1-p[0-9]+$ ]]; then
  pass "logs-noisy: cycle directory name carries UTC start, cycle and controller pid"
else
  fail "logs-noisy: cycle directory name carries UTC start, cycle and controller pid" "$(basename "${cdir}")"
fi
expect_grep "logs-noisy: status records the cycle number" '^cycle=1$' "${cdir}/status"
expect_grep "logs-noisy: status records the decision" '^decision=DECISION priority=6 kind=claim-issue target=issue #920' "${cdir}/status"
expect_grep "logs-noisy: status records the start time" '^started=[0-9-]*T[0-9:]*Z$' "${cdir}/status"
expect_grep "logs-noisy: status records the finish time" '^finished=[0-9-]*T[0-9:]*Z$' "${cdir}/status"
expect_grep "logs-noisy: status records the exit code" '^exit_code=0$' "${cdir}/status"
expect_grep "logs-noisy: status records the duration" '^duration_seconds=[0-9][0-9]*$' "${cdir}/status"
expect_grep "logs-noisy: journal keeps the cycle start line" 'cycle 1 start: DECISION priority=6' "${OUT_FILE}"
expect_grep "logs-noisy: journal names the diagnostics path at start" "cycle 1 diagnostics: ${cdir}" "${OUT_FILE}"
expect_grep "logs-noisy: journal summary has rc, duration and path" \
  "cycle 1 exit rc=0 duration=[0-9]*s stdout=$(stat -c %s "${STATE}/expected-stdout")B stderr=$(stat -c %s "${STATE}/expected-stderr")B diagnostics=${cdir}" "${OUT_FILE}"
expect_grep "logs-noisy: journal keeps the finish line" 'cycle 1 finished' "${OUT_FILE}"
expect_grep "logs-noisy: journal keeps the idle line" 'idle: REASON nothing left ; next poll in 300s' "${OUT_FILE}"
journal_lines=$(wc -l <"${OUT_FILE}")
if ((journal_lines <= 10)); then
  pass "logs-noisy: the journal stays concise (${journal_lines} lines)"
else
  fail "logs-noisy: the journal stays concise" "${journal_lines} lines"
fi
if git -C "${REPO}" status --porcelain --ignored | grep -q .; then
  fail "logs-noisy: nothing is written into the checkout" "$(git -C "${REPO}" status --porcelain --ignored)"
else
  pass "logs-noisy: nothing is written into the checkout"
fi

# A failing session keeps its exit code; the journal gets an actionable
# summary without the raw output.
new_case logs-failure
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #921 Failing work\nREASON failing\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
echo 3 >"${STATE}/rc.1"
hook 'echo "RAW-FAILURE-STDOUT tool output"
echo "RAW-FAILURE-STDERR Error: provider exploded" >&2'
run_controller AUTONOMY_MAX_LOOPS=2
cdir=$(cycle_dirs | head -n 1)
expect_eq "logs-failure: controller continues" 0 "${RC}"
expect_grep "logs-failure: journal summary keeps rc and path" "cycle 1 exit rc=3 duration=[0-9]*s .*diagnostics=${cdir}" "${OUT_FILE}"
expect_grep "logs-failure: existing failure line is kept" 'cycle 1 failed rc=3' "${OUT_FILE}"
expect_grep "logs-failure: retry backoff line is kept" 'retrying after 120s' "${OUT_FILE}"
expect_absent_grep "logs-failure: raw stdout is not journalled" 'RAW-FAILURE-STDOUT' "${OUT_FILE}"
expect_absent_grep "logs-failure: raw stderr is not journalled" 'RAW-FAILURE-STDERR' "${OUT_FILE}"
expect_grep "logs-failure: stderr is preserved in the file" 'RAW-FAILURE-STDERR Error: provider exploded' "${cdir}/stderr.log"
expect_grep "logs-failure: status records the exit code" '^exit_code=3$' "${cdir}/status"
expect_eq "logs-failure: failure backoff unchanged" "120 300" "$(sleep_log)"

# Timeout: the timeout status and the partial output are preserved.
new_case logs-timeout
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #922 Slow work\nREASON slow\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
hook 'echo "PARTIAL-BEFORE-TIMEOUT"
/bin/sleep 30'
run_controller AUTONOMY_MAX_LOOPS=2 CYCLE_TIMEOUT=2s CYCLE_KILL_AFTER=1s
cdir=$(cycle_dirs | head -n 1)
expect_grep "logs-timeout: exit 124 is kept in the summary" "cycle 1 exit rc=124 .*diagnostics=${cdir}" "${OUT_FILE}"
expect_grep "logs-timeout: the timeout line is kept" 'cycle 1 timed out after 2s' "${OUT_FILE}"
expect_grep "logs-timeout: status records the timeout code" '^exit_code=124$' "${cdir}/status"
expect_grep "logs-timeout: output before the timeout is preserved" 'PARTIAL-BEFORE-TIMEOUT' "${cdir}/stdout.log"
expect_absent_grep "logs-timeout: partial output is not journalled" 'PARTIAL-BEFORE-TIMEOUT' "${OUT_FILE}"

# Wrapper recovery sessions are logged the same way.
new_case logs-recovery
CYCLE_LOGS=""
touch "${STATE}/pending.marker"
echo 78 >"${STATE}/rc.1"
hook 'echo "RECOVERY-STDOUT resumed session"
echo "RECOVERY-STDERR wrapper detail" >&2'
run_controller AUTONOMY_MAX_LOOPS=1 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe"
cdir=$(cycle_dirs | head -n 1)
expect_eq "logs-recovery: one recovery cycle directory" 1 "$(cycle_dir_count)"
expect_grep "logs-recovery: status names the recovery decision" '^decision=DECISION priority=3 kind=wrapper-recovery' "${cdir}/status"
expect_grep "logs-recovery: status records rc 78" '^exit_code=78$' "${cdir}/status"
expect_grep "logs-recovery: recovery stdout preserved" 'RECOVERY-STDOUT resumed session' "${cdir}/stdout.log"
expect_grep "logs-recovery: recovery stderr preserved" 'RECOVERY-STDERR wrapper detail' "${cdir}/stderr.log"
expect_absent_grep "logs-recovery: recovery output not journalled" 'RECOVERY-STD' "${OUT_FILE}"
expect_grep "logs-recovery: RECOVERY BLOCKED is still journalled" 'RECOVERY BLOCKED' "${OUT_FILE}"
expect_grep "logs-recovery: summary names rc 78 and the path" "cycle 1 exit rc=78 .*diagnostics=${cdir}" "${OUT_FILE}"

# Diagnostics that cannot be created: the session is never started.
new_case logs-unwritable
CYCLE_LOGS="${CASE}/not-a-dir/cycles"
printf 'regular file\n' >"${CASE}/not-a-dir"
printf 'DECISION priority=6 kind=claim-issue target=issue #923 Unloggable work\nREASON unloggable\n' \
  >"${STATE}/selout.1"
printf 'DECISION priority=idle kind=none target=-\nREASON nothing left\n' >"${STATE}/selout.2"
run_controller AUTONOMY_MAX_LOOPS=2 CYCLE_LOG_DIR="${CYCLE_LOGS}"
expect_eq "logs-unwritable: controller keeps running" 0 "${RC}"
expect_eq "logs-unwritable: no session starts without diagnostics" 0 "$(run_calls)"
expect_grep "logs-unwritable: the refusal is journalled" "cycle 1 not started: cannot create private diagnostics under ${CYCLE_LOGS}" "${OUT_FILE}"
expect_absent_grep "logs-unwritable: no start line for an unstarted cycle" 'cycle 1 start:' "${OUT_FILE}"
expect_eq "logs-unwritable: it backs off like a failure, then polls" "120 300" "$(sleep_log)"

new_case logs-unwritable-recovery
CYCLE_LOGS="${CASE}/not-a-dir/cycles"
printf 'regular file\n' >"${CASE}/not-a-dir"
touch "${STATE}/pending.marker"
run_controller AUTONOMY_MAX_LOOPS=1 OPENCODE_BIN="${CASE}/bin/opencode-tendo-safe" CYCLE_LOG_DIR="${CYCLE_LOGS}"
expect_eq "logs-unwritable-recovery: no recovery session without diagnostics" 0 "$(run_calls)"
expect_eq "logs-unwritable-recovery: failure backoff" "120" "$(sleep_log)"

# A symlinked log directory (possible redirection) is refused.
new_case logs-symlink
mkdir -p "${CASE}/elsewhere"
CYCLE_LOGS="${CASE}/linked-cycles"
ln -s "${CASE}/elsewhere" "${CYCLE_LOGS}"
printf 'DECISION priority=6 kind=claim-issue target=issue #924 Linked work\nREASON linked\n' \
  >"${STATE}/selout.1"
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_DIR="${CYCLE_LOGS}"
expect_eq "logs-symlink: no session through a symlinked log directory" 0 "$(run_calls)"
expect_eq "logs-symlink: nothing written through the link" "" "$(ls -A "${CASE}/elsewhere")"
expect_grep "logs-symlink: the refusal is journalled" "cycle 1 not started: cannot create private diagnostics" "${OUT_FILE}"

# Invalid locations are refused at startup.
new_case logs-config
for bad in "relative/cycles" "${REPO}/.cycle-logs" "${RUNTIME}/cycles" "${TMP}/cycles"; do
  run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_DIR="${bad}"
  expect_eq "logs-config: CYCLE_LOG_DIR=${bad#"${CASE}"/} is refused" 2 "${RC}"
  expect_grep "logs-config: CYCLE_LOG_DIR=${bad#"${CASE}"/} refusal is explained" 'invalid CYCLE_LOG_DIR=' "${OUT_FILE}"
done
for knob in CYCLE_LOG_KEEP_COUNT CYCLE_LOG_MAX_AGE_DAYS CYCLE_LOG_MAX_BYTES; do
  run_controller AUTONOMY_MAX_LOOPS=1 "${knob}=0"
  expect_eq "logs-config: ${knob}=0 is refused" 2 "${RC}"
  expect_grep "logs-config: ${knob}=0 refusal is explained" "invalid ${knob}='0' (minimum 1)" "${OUT_FILE}"
done
expect_eq "logs-config: no selection before any refusal" 0 "$(sel_calls)"

# Bounded retention by count, age and bytes; the newest cycle always stays and
# unrelated entries are never touched.
make_old_cycle() { # <name> <bytes> [touch date]
  local dir
  dir="$(cycle_logs_root)/$1"
  mkdir -p "${dir}"
  head -c "$2" /dev/zero >"${dir}/stdout.log"
  : >"${dir}/stderr.log"
  : >"${dir}/status"
  if [[ -n "${3:-}" ]]; then touch -d "$3" "${dir}" "${dir}"/*; fi
}

new_case logs-retention-count
CYCLE_LOGS=""
for n in 1 2 3 4 5; do make_old_cycle "2026010${n}T000000Z-c${n}-p1" 10; done
mkdir -p "$(cycle_logs_root)/unrelated-dir"
ln -s "${CASE}" "$(cycle_logs_root)/20260109T000000Z-c9-p1"
printf 'DECISION priority=6 kind=claim-issue target=issue #925 Work\nREASON work\n' >"${STATE}/selout.1"
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_KEEP_COUNT=3
expect_eq "logs-retention-count: keeps CYCLE_LOG_KEEP_COUNT cycle dirs" 3 "$(cycle_dir_count)"
if [[ "$(cycle_dirs | grep -c -- '-c1-p[0-9]*$' || true)" == 1 &&
  -d "$(cycle_logs_root)/20260105T000000Z-c5-p1" && -d "$(cycle_logs_root)/20260104T000000Z-c4-p1" ]]; then
  pass "logs-retention-count: the newest cycles survive"
else
  fail "logs-retention-count: the newest cycles survive" "$(cycle_dirs | xargs -n1 basename | tr '\n' ' ')"
fi
expect_eq "logs-retention-count: oldest is removed" no "$([[ -d "$(cycle_logs_root)/20260101T000000Z-c1-p1" ]] && echo yes || echo no)"
expect_eq "logs-retention-count: unrelated entries are untouched" yes "$([[ -d "$(cycle_logs_root)/unrelated-dir" ]] && echo yes || echo no)"
expect_eq "logs-retention-count: symlinks are never followed or removed" yes "$([[ -L "$(cycle_logs_root)/20260109T000000Z-c9-p1" && -d "${REPO}" ]] && echo yes || echo no)"
expect_grep "logs-retention-count: pruning is journalled" 'cycle log retention: removed 3 old cycle log directories' "${OUT_FILE}"

new_case logs-retention-age
CYCLE_LOGS=""
make_old_cycle "20200101T000000Z-c1-p1" 10 "2020-01-01"
make_old_cycle "20200102T000000Z-c2-p1" 10 "2020-01-02"
make_old_cycle "29990101T000000Z-c3-p1" 10
printf 'DECISION priority=6 kind=claim-issue target=issue #926 Work\nREASON work\n' >"${STATE}/selout.1"
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_MAX_AGE_DAYS=30
expect_eq "logs-retention-age: aged cycle dirs are removed" 2 "$(cycle_dir_count)"
expect_eq "logs-retention-age: recent cycle dirs stay" yes "$([[ -d "$(cycle_logs_root)/29990101T000000Z-c3-p1" ]] && echo yes || echo no)"

new_case logs-retention-bytes
CYCLE_LOGS=""
make_old_cycle "20260101T000000Z-c1-p1" 40000
make_old_cycle "20260102T000000Z-c2-p1" 40000
make_old_cycle "20260103T000000Z-c3-p1" 40000
printf 'DECISION priority=6 kind=claim-issue target=issue #927 Work\nREASON work\n' >"${STATE}/selout.1"
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_MAX_BYTES=100000
expect_eq "logs-retention-bytes: total stays under CYCLE_LOG_MAX_BYTES" 3 "$(cycle_dir_count)"
expect_eq "logs-retention-bytes: oldest removed first" no "$([[ -d "$(cycle_logs_root)/20260101T000000Z-c1-p1" ]] && echo yes || echo no)"
total=$(du -sb "$(cycle_logs_root)" | cut -f1)
if ((total <= 100000 + 8192)); then
  pass "logs-retention-bytes: remaining bytes are bounded (${total})"
else
  fail "logs-retention-bytes: remaining bytes are bounded" "${total}"
fi

new_case logs-retention-newest
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #928 Big work\nREASON big\n' >"${STATE}/selout.1"
hook 'head -c 50000 /dev/zero'
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_MAX_BYTES=1000
expect_eq "logs-retention-newest: the newest cycle survives even above the byte cap" 1 "$(cycle_dir_count)"
expect_eq "logs-retention-newest: its output is complete" 50000 "$(stat -c %s "$(cycle_dirs | head -n 1)/stdout.log" 2>/dev/null)"

# A crafted name with newlines must never split into a relative path that
# pruning would resolve against the checkout (the controller's cwd).
new_case logs-retention-newline
CYCLE_LOGS=""
mkdir -p "${REPO}/victim" "$(cycle_logs_root)"
touch "${REPO}/victim/sentinel"
# Split on newlines, "zz" would sort first (kept as "newest") and the relative
# "victim" second, so a line-based prune would remove ${REPO}/victim.
crafted="$(cycle_logs_root)/20200101T000000Z-c0-p0"$'\n'"zz"$'\n'"victim"$'\n'"20200101T000000Z-c0-p0"
mkdir -p "${crafted}"
touch -d 2020-01-01 "${crafted}"
make_old_cycle "20200101T000000Z-c1-p1" 10 "2020-01-01"
printf 'DECISION priority=6 kind=claim-issue target=issue #932 Work\nREASON work\n' >"${STATE}/selout.1"
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_KEEP_COUNT=1
expect_eq "logs-retention-newline: nothing outside CYCLE_LOG_DIR is removed" yes \
  "$([[ -f "${REPO}/victim/sentinel" ]] && echo yes || echo no)"
expect_eq "logs-retention-newline: a non-matching crafted name is left alone" yes \
  "$([[ -d "${crafted}" ]] && echo yes || echo no)"
expect_eq "logs-retention-newline: genuine aged cycles are still pruned" no \
  "$([[ -d "$(cycle_logs_root)/20200101T000000Z-c1-p1" ]] && echo yes || echo no)"

# The current cycle's evidence survives even when a future-stamped directory
# sorts above it and the byte cap is exceeded.
new_case logs-retention-current
CYCLE_LOGS=""
make_old_cycle "29990101T000000Z-c9-p1" 40000
printf 'DECISION priority=6 kind=claim-issue target=issue #933 Work\nREASON work\n' >"${STATE}/selout.1"
hook 'head -c 20000 /dev/zero'
run_controller AUTONOMY_MAX_LOOPS=1 CYCLE_LOG_MAX_BYTES=30000
current=$(cycle_dirs | grep -- '-c1-p[0-9]*$' | head -n 1)
expect_eq "logs-retention-current: the just-summarised cycle survives" 20000 \
  "$(stat -c %s "${current}/stdout.log" 2>/dev/null)"
expect_grep "logs-retention-current: its journal path stays valid" "diagnostics=${current}" "${OUT_FILE}"

# Signals: the in-flight cycle's evidence names the interruption.
new_case logs-sigterm
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #929 Long work\nREASON long\n' >"${STATE}/selout.1"
hook 'echo "BEFORE-SIGNAL output"
touch "${FAKE_STATE}/cycle-started"
while :; do /bin/sleep 0.1; done'
start_controller_bg CYCLE_STOP_GRACE=1 CYCLE_TIMEOUT=300
if wait_for "${STATE}/cycle-started"; then
  kill -TERM "${CTRL_PID}"
  RC=0
  wait "${CTRL_PID}" || RC=$?
  cdir=$(cycle_dirs | head -n 1)
  expect_eq "logs-sigterm: exit status unchanged" 143 "${RC}"
  expect_grep "logs-sigterm: status records the interruption" '^interrupted=SIGTERM$' "${cdir}/status"
  expect_grep "logs-sigterm: status records the controller exit" '^controller_exit_code=143$' "${cdir}/status"
  expect_grep "logs-sigterm: journal names the interrupted cycle and path" "cycle 1 interrupted (SIGTERM); controller exit 143; diagnostics=${cdir}" "${OUT_FILE}"
  expect_grep "logs-sigterm: output before the signal is preserved" 'BEFORE-SIGNAL output' "${cdir}/stdout.log"
  expect_absent_grep "logs-sigterm: output is not journalled" 'BEFORE-SIGNAL' "${OUT_FILE}"
  if lock_free "${RUNTIME}/tendo-autonomous-${UID}.lock" && lock_free "${RUNTIME}/tendo-maintenance-${UID}.lock"; then
    pass "logs-sigterm: locks are released only at exit"
  else
    fail "logs-sigterm: locks are released only at exit" "a lock is still held"
  fi
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "logs-sigterm: TERM during a logged cycle" "the session never started"
fi

new_case logs-fail-closed
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #930 Stuck work\nREASON stuck\n' >"${STATE}/selout.1"
hook 'touch "${FAKE_STATE}/job-started"'
touch "${STATE}/group-never-gone"
run_controller AUTONOMY_MAX_LOOPS=2 CYCLE_DRAIN_TIMEOUT=1 CYCLE_STOP_GRACE=1 \
  AUTONOMY_TEST_GROUP_ALIVE_FILE="${STATE}/group-never-gone"
cdir=$(cycle_dirs | head -n 1)
expect_eq "logs-fail-closed: still exits 70" 70 "${RC}"
expect_grep "logs-fail-closed: the session exit code is kept before the drain" '^exit_code=0$' "${cdir}/status"
expect_grep "logs-fail-closed: status records the fail-closed exit" '^interrupted=fail-closed$' "${cdir}/status"
expect_grep "logs-fail-closed: journal names the path" "cycle 1 interrupted (fail-closed); controller exit 70; diagnostics=${cdir}" "${OUT_FILE}"

# A second controller never creates cycle logs.
new_case logs-concurrent
CYCLE_LOGS=""
printf 'DECISION priority=6 kind=claim-issue target=issue #931 Held work\nREASON held\n' >"${STATE}/selout.1"
hook 'touch "${FAKE_STATE}/job-started"
for _ in $(seq 400); do
  [[ -e "${FAKE_STATE}/release-job" ]] && break
  /bin/sleep 0.05
done'
start_controller_bg AUTONOMY_MAX_LOOPS=1
if wait_for "${STATE}/job-started"; then
  mkdir -p "${CASE}/state-b"
  OUT_FILE="${CASE}/out-b"
  run_controller AUTONOMY_MAX_LOOPS=1 FAKE_STATE="${CASE}/state-b"
  OUT_FILE="${CASE}/out"
  expect_eq "logs-concurrent: the second controller exits 75" 75 "${RC}"
  expect_eq "logs-concurrent: only the running cycle has a log directory" 1 "$(cycle_dir_count)"
  touch "${STATE}/release-job"
  wait "${CTRL_PID}" 2>/dev/null || true
else
  kill -KILL "${CTRL_PID}" 2>/dev/null || true
  wait "${CTRL_PID}" 2>/dev/null || true
  fail "logs-concurrent: second controller during a logged cycle" "the session never started"
fi

# --- summary ----------------------------------------------------------------

if ((failures > 0)); then
  echo "${failures} autonomy-controller test(s) failed" >&2
  exit 1
fi
echo "all autonomy-controller tests passed"
