#!/usr/bin/env bash
# Behavior tests for scripts/autonomous-loop.sh (the V5 feature controller).
# Each case runs the real script from a disposable git checkout with fake
# `systemctl`, `opencode` and work-selection executables, and asserts its exit
# status and whether, how often and how an OpenCode cycle was started. A
# stand-in for ~/.local/bin/tendo-maintenance.sh uses the same flock protocol
# (`flock -n` on tendo-maintenance-$UID.lock, "already running" on conflict)
# to prove the maintenance handoff.
set -euo pipefail

SOURCE_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
holders=()
cleanup() {
  local pid
  for pid in "${holders[@]}"; do kill "${pid}" 2>/dev/null || true; done
  # Reap anything a failed case left running from this private work dir.
  pkill -KILL -f "${WORK}/" 2>/dev/null || true
  rm -rf "${WORK}"
}
trap cleanup EXIT

failures=0
pass() { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2" >&2; failures=$((failures + 1)); }

make_fakes() {
  local bin="$1"
  mkdir -p "${bin}"
  # systemctl: the V5 loop must not consult unit state at all; any call is
  # recorded so tests can assert it never happened. Answers mimic the host.
  cat >"${bin}/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"${FAKE_STATE}/systemctl-calls"
file="${FAKE_STATE}/$2.$3"
[[ -f "${file}" ]] || exit 4
cat "${file}"
EOF
  # opencode run ...: records each invocation, its prompt (last argument),
  # inherited lock descriptors and whether the maintenance flock was free,
  # then runs an optional per-test hook.
  cat >"${bin}/opencode" <<'EOF'
#!/usr/bin/env bash
count=$(( $(cat "${FAKE_STATE}/calls" 2>/dev/null || echo 0) + 1 ))
echo "${count}" >"${FAKE_STATE}/calls"
printf '%s\n' "$@" >"${FAKE_STATE}/args.${count}"
printf '%s' "${@: -1}" >"${FAKE_STATE}/prompt.${count}"
for fd in 8 9; do
  if [[ -e "/proc/$$/fd/${fd}" ]]; then echo open; else echo closed; fi >"${FAKE_STATE}/fd${fd}.${count}"
done
if flock -n "${XDG_RUNTIME_DIR}/tendo-maintenance-${UID}.lock" true; then
  echo free >"${FAKE_STATE}/maintenance-lock.${count}"
else
  echo held >"${FAKE_STATE}/maintenance-lock.${count}"
fi
if [[ -x "${FAKE_STATE}/hook" ]]; then "${FAKE_STATE}/hook" "${count}"; fi
exit "$(cat "${FAKE_STATE}/rc.${count}" 2>/dev/null || echo 0)"
EOF
  # Same lock protocol as ~/.local/bin/tendo-maintenance.sh; "work" is a
  # file. With MAINTENANCE_BARRIER set it keeps working (holding the lock)
  # until that file exists, so races are ordered by barriers, not by sleeps.
  cat >"${bin}/fake-maintenance" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exec 9>"${XDG_RUNTIME_DIR:-/tmp}/tendo-maintenance-${UID}.lock"
flock -n 9 || { echo "[tendo-maintenance] already running"; exit 0; }
echo worked >>"${FAKE_STATE}/maintenance-worked"
if [[ -n "${MAINTENANCE_BARRIER:-}" ]]; then
  for _ in $(seq 300); do [[ -e "${MAINTENANCE_BARRIER}" ]] && exit 0; sleep 0.1; done
  echo "barrier timeout" >&2; exit 1
fi
EOF
  chmod +x "${bin}/systemctl" "${bin}/opencode" "${bin}/fake-maintenance"
}

# new_case <name>: fresh checkout on clean main with historical V3_COMPLETE
# and V4_COMPLETE tracked, the real V5 cycle prompt, a stub workflow and a
# stub work-selection script; maintenance timer enabled (the V5 default).
new_case() {
  CASE="${WORK}/$1"
  REPO="${CASE}/repo"
  STATE="${CASE}/state"
  RUNTIME="${CASE}/run"
  mkdir -p "${REPO}/scripts" "${REPO}/.autonomous" "${STATE}" "${RUNTIME}"
  make_fakes "${CASE}/bin"
  cp "${SOURCE_ROOT}/scripts/autonomous-loop.sh" "${REPO}/scripts/"
  cp "${SOURCE_ROOT}/.autonomous/V5_CYCLE_PROMPT.md" "${REPO}/.autonomous/"
  echo "V3 done" >"${REPO}/.autonomous/V3_COMPLETE"
  echo "V4 done" >"${REPO}/.autonomous/V4_COMPLETE"
  echo "V4 PROMPT MARKER" >"${REPO}/.autonomous/V4_CYCLE_PROMPT.md"
  echo "workflow" >"${REPO}/.autonomous/V5_ISSUES_WORKFLOW.md"
  printf '#!/usr/bin/env python3\nprint("DECISION priority=d kind=triage-aged-renovate target=pr #404 SELECTION MARKER")\n' \
    >"${REPO}/scripts/v5_work_selection.py"
  git -C "${REPO}" init -q -b main
  git -C "${REPO}" add -A
  git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m init
  maintenance timer enabled active
  maintenance service static inactive
}

maintenance() {
  echo "$2" >"${STATE}/is-enabled.tendo-maintenance.$1"
  echo "$3" >"${STATE}/is-active.tendo-maintenance.$1"
}

hook() {
  printf '#!/usr/bin/env bash\nset -e\ncd %q\n%s\n' "${REPO}" "$1" >"${STATE}/hook"
  chmod +x "${STATE}/hook"
}

COMMIT_V5_COMPLETE='echo done >.autonomous/V5_COMPLETE; git add -A; git -c user.name=t -c user.email=t@t commit -q -m complete'
RUN_MAINTENANCE='"${FAKE_STATE}/../bin/fake-maintenance" >"${FAKE_STATE}/maintenance-during-cycle.$1"'

loop_env() {
  env -i HOME="${CASE}" PATH="${CASE}/bin:/usr/bin:/bin" UID="${UID}" TENDO_ROOT="${REPO}" \
    XDG_RUNTIME_DIR="${RUNTIME}" FAKE_STATE="${STATE}" \
    OPENCODE_BIN="${CASE}/bin/opencode" SUCCESS_SLEEP=0 FAIL_SLEEP_BASE=0 \
    FAIL_SLEEP_MAX=0 CYCLE_TIMEOUT=30s "$@"
}

run_loop() {
  set +e
  loop_env timeout 15 bash "${REPO}/scripts/autonomous-loop.sh" >"${CASE}/out" 2>&1
  RC=$?
  set -e
  CALLS=$(cat "${STATE}/calls" 2>/dev/null || echo 0)
}

run_maintenance() {
  loop_env "${CASE}/bin/fake-maintenance"
}

# wait_for <file> [tenths]: barrier on a file another process creates.
wait_for() {
  local _
  for _ in $(seq "${2:-150}"); do [[ -e "$1" ]] && return 0; sleep 0.1; done
  return 1
}

wait_for_text() {
  local _
  for _ in $(seq 150); do grep -q "$1" "$2" 2>/dev/null && return 0; sleep 0.1; done
  return 1
}

pid_alive() { [[ -n "$1" ]] && kill -0 "$1" 2>/dev/null; }

# hold_lock <file>: a background holder; waits until the lock is really held.
hold_lock() {
  flock "$1" sleep 30 &
  HOLDER=$!
  holders+=("${HOLDER}")
  for _ in $(seq 50); do
    flock -n "$1" true || return 0
    sleep 0.1
  done
  return 1
}

release_lock() {
  kill "${HOLDER}" 2>/dev/null || true
  wait "${HOLDER}" 2>/dev/null || true
}

expect() {
  local name="$1" rc="$2" calls="$3"
  if [[ "${RC}" != "${rc}" || "${CALLS}" != "${calls}" ]]; then
    fail "${name}" "rc=${RC} calls=${CALLS}, want rc=${rc} calls=${calls}; output: $(cat "${CASE}/out")"
    return 1
  fi
}

maintenance_lock_free() {
  flock -n "${RUNTIME}/tendo-maintenance-${UID}.lock" true
}

# --- V5 cycle and completion -------------------------------------------------

name="runs one fresh V5 cycle and exits when V5_COMPLETE is merged on main"
new_case run-v5; hook "${COMMIT_V5_COMPLETE}"; run_loop
if expect "${name}" 0 1; then
  args="${STATE}/args.1"
  if grep -qx 'Tendo V5 autonomous cycle 1' "${args}" &&
    grep -qx -- '--agent' "${args}" && grep -qx orchestrator "${args}" &&
    grep -qx run "${args}" && ! grep -qx -- '--continue' "${args}" &&
    ! grep -q -- '--session' "${args}" && ! grep -q 'V4 PROMPT MARKER' "${args}"; then pass "${name}"
  else fail "${name}" "unexpected opencode arguments: $(tr '\n' ' ' <"${args}")"; fi
fi

name="the session prompt starts with the byte-exact V5_CYCLE_PROMPT"
prompt_size=$(stat -c %s "${SOURCE_ROOT}/.autonomous/V5_CYCLE_PROMPT.md")
if cmp -s -n "${prompt_size}" "${SOURCE_ROOT}/.autonomous/V5_CYCLE_PROMPT.md" "${WORK}/run-v5/state/prompt.1"; then pass "${name}"
else fail "${name}" "prompt prefix differs from .autonomous/V5_CYCLE_PROMPT.md"; fi

name="the session prompt carries the controller work selection"
if grep -q 'SELECTION MARKER' "${WORK}/run-v5/state/prompt.1" &&
  grep -q 'Controller work selection for this cycle' "${WORK}/run-v5/state/prompt.1"; then pass "${name}"
else fail "${name}" "selection output missing from the prompt"; fi

name="holds the maintenance lock while a V5 cycle runs"
if [[ "$(cat "${WORK}/run-v5/state/maintenance-lock.1" 2>/dev/null)" == held ]]; then pass "${name}"
else fail "${name}" "maintenance flock was acquirable during the cycle"; fi

name="the cycle does not inherit the controller lock descriptors"
if [[ "$(cat "${WORK}/run-v5/state/fd8.1" 2>/dev/null)" == closed && "$(cat "${WORK}/run-v5/state/fd9.1" 2>/dev/null)" == closed ]]; then pass "${name}"
else fail "${name}" "fd8=$(cat "${WORK}/run-v5/state/fd8.1" 2>/dev/null) fd9=$(cat "${WORK}/run-v5/state/fd9.1" 2>/dev/null)"; fi

name="tracked historical V4_COMPLETE (and V3_COMPLETE) never stops the V5 loop"
new_case v4-only; hook 'if [[ $1 == 3 ]]; then '"${COMMIT_V5_COMPLETE}"'; fi'; run_loop
expect "${name}" 0 3 && pass "${name}"

name="exits without a cycle when V5_COMPLETE is tracked on clean main"
new_case v5-done
echo done >"${REPO}/.autonomous/V5_COMPLETE"
git -C "${REPO}" add -A; git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m done
run_loop
expect "${name}" 0 0 && pass "${name}"

name="V5_COMPLETE tracked on a non-main branch does not count as completion"
new_case v5-branch
git -C "${REPO}" switch -q -c feature
echo done >"${REPO}/.autonomous/V5_COMPLETE"
git -C "${REPO}" add -A; git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m done
hook 'git switch -q main; git merge -q feature'; run_loop
expect "${name}" 0 1 && pass "${name}"

for dirt in unstaged staged; do
  name="V5_COMPLETE on main with ${dirt} changes does not count as completion"
  new_case "v5-dirty-${dirt}"
  echo done >"${REPO}/.autonomous/V5_COMPLETE"
  git -C "${REPO}" add -A; git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m done
  echo changed >>"${REPO}/.autonomous/V5_ISSUES_WORKFLOW.md"
  [[ "${dirt}" == staged ]] && git -C "${REPO}" add -A
  hook 'git reset -q --hard'; run_loop
  expect "${name}" 0 1 && pass "${name}"
done

name="untracked V5_COMPLETE does not count as completion"
new_case v5-untracked; echo x >"${REPO}/.autonomous/V5_COMPLETE"
hook 'rm -f .autonomous/V5_COMPLETE; '"${COMMIT_V5_COMPLETE}"; run_loop
expect "${name}" 0 1 && pass "${name}"

# --- maintenance timer/lock handoff -----------------------------------------

for state in "enabled active" "enabled inactive" "disabled inactive" "masked inactive" "not-found inactive"; do
  name="runs V5 cycles with maintenance timer ${state} and never queries systemd"
  new_case "timer-${state// /-}"; maintenance timer ${state}; hook "${COMMIT_V5_COMPLETE}"; run_loop
  if expect "${name}" 0 1; then
    if [[ ! -e "${STATE}/systemctl-calls" ]]; then pass "${name}"
    else fail "${name}" "systemctl was called: $(cat "${STATE}/systemctl-calls")"; fi
  fi
done

name="unreadable maintenance unit state does not block V5 (lock is the truth)"
new_case unreadable; rm "${STATE}/is-enabled.tendo-maintenance.timer"; hook "${COMMIT_V5_COMPLETE}"; run_loop
expect "${name}" 0 1 && pass "${name}"

name="a scheduled maintenance run during a V5 cycle skips without working"
new_case maint-skip; hook "${RUN_MAINTENANCE}; ${COMMIT_V5_COMPLETE}"; run_loop
if expect "${name}" 0 1; then
  if grep -q 'already running' "${STATE}/maintenance-during-cycle.1" && [[ ! -e "${STATE}/maintenance-worked" ]]; then pass "${name}"
  else fail "${name}" "maintenance output: $(cat "${STATE}/maintenance-during-cycle.1" 2>/dev/null)"; fi
fi

name="a maintenance run between failed cycles still skips (lock held across cycles)"
new_case maint-between; echo 1 >"${STATE}/rc.1"
hook 'if [[ $1 == 2 ]]; then '"${RUN_MAINTENANCE}; ${COMMIT_V5_COMPLETE}"'; fi'; run_loop
if expect "${name}" 0 2; then
  if grep -q 'already running' "${STATE}/maintenance-during-cycle.2" && [[ ! -e "${STATE}/maintenance-worked" ]]; then pass "${name}"
  else fail "${name}" "maintenance worked during V5"; fi
fi

name="after V5_COMPLETE the next maintenance run resumes work"
# maint-skip has exited cleanly on V5_COMPLETE; its lock must be released.
CASE="${WORK}/maint-skip"; STATE="${CASE}/state"; RUNTIME="${CASE}/run"; REPO="${CASE}/repo"
run_maintenance >"${CASE}/maint-after" 2>&1
if [[ "$(cat "${STATE}/maintenance-worked" 2>/dev/null)" == worked ]]; then pass "${name}"
else fail "${name}" "maintenance after completion: $(cat "${CASE}/maint-after")"; fi

name="refuses (exit 3, no session) while a maintenance run holds the shared lock"
new_case maint-lock
hold_lock "${RUNTIME}/tendo-maintenance-${UID}.lock"; run_loop; release_lock
if expect "${name}" 3 0; then
  if [[ ! -e "${STATE}/systemctl-calls" ]]; then pass "${name}"; else fail "${name}" "systemctl was called"; fi
fi

name="start race with a maintenance run: exactly one of them works"
new_case race; hook "${COMMIT_V5_COMPLETE}"
loop_env MAINTENANCE_BARRIER="${STATE}/release-maintenance" "${CASE}/bin/fake-maintenance" >"${CASE}/maint-race" 2>&1 &
maint_pid=$!
# Barrier: maintenance holds the lock and is working before the loop starts.
wait_for "${STATE}/maintenance-worked" || fail "${name}" "maintenance never started working"
run_loop
touch "${STATE}/release-maintenance"; wait "${maint_pid}" || true
if expect "${name}" 3 0; then
  if [[ "$(cat "${STATE}/maintenance-worked" 2>/dev/null)" == worked ]]; then pass "${name}"
  else fail "${name}" "maintenance did not work either"; fi
fi

name="concurrent maintenance and V5 start: never both working"
both=0
for attempt in 1 2 3 4 5; do
  # Started together, ordered only by barriers: whichever side wins the
  # flock keeps working until the other side has finished deciding, so a
  # sequential pass can never hide a double run.
  #  - maintenance won: it works until the test releases it after the loop
  #    has returned (which must be a refusal, exit 3, no cycle);
  #  - the loop won: its cycle works until maintenance has exited (which
  #    must be "already running" without work).
  new_case "race-concurrent-${attempt}"
  hook 'for _ in $(seq 150); do [[ -e "${FAKE_STATE}/maintenance-exited" ]] && break; sleep 0.1; done; '"${COMMIT_V5_COMPLETE}"
  (loop_env MAINTENANCE_BARRIER="${STATE}/release-maintenance" "${CASE}/bin/fake-maintenance" >"${CASE}/maint-race" 2>&1
   touch "${STATE}/maintenance-exited") &
  maint_pid=$!
  run_loop
  touch "${STATE}/release-maintenance"; wait "${maint_pid}" || true
  worked=$([[ -e "${STATE}/maintenance-worked" ]] && echo 1 || echo 0)
  if (( worked + CALLS != 1 )) || { [[ "${worked}" == 1 ]] && [[ "${RC}" != 3 ]]; } ||
    { [[ "${worked}" == 0 ]] && ! grep -q 'already running' "${CASE}/maint-race"; }; then
    both=1; fail "${name}" "attempt ${attempt}: maintenance worked=${worked} loop rc=${RC} calls=${CALLS}"
  fi
done
[[ "${both}" == 0 ]] && pass "${name}"

# --- failure paths -----------------------------------------------------------

name="retries a failed cycle with a fresh session, then finishes"
new_case retry
hook 'if [[ $1 == 2 ]]; then '"${COMMIT_V5_COMPLETE}"'; fi'
echo 1 >"${STATE}/rc.1"; run_loop
if expect "${name}" 0 2; then
  if grep -qx 'Tendo V5 autonomous cycle 2' "${STATE}/args.2"; then pass "${name}"
  else fail "${name}" "second cycle title: $(tr '\n' ' ' <"${STATE}/args.2")"; fi
fi

name="a failing work selection still runs the cycle with a manual-priority note"
new_case selection-fails
printf '#!/usr/bin/env python3\nimport sys\nprint("gh: network down")\nsys.exit(1)\n' >"${REPO}/scripts/v5_work_selection.py"
git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -am failing-selection
hook "${COMMIT_V5_COMPLETE}"; run_loop
if expect "${name}" 0 1; then
  if grep -q 'SELECTION UNAVAILABLE rc=1' "${STATE}/prompt.1" && grep -q 'network down' "${STATE}/prompt.1"; then pass "${name}"
  else fail "${name}" "prompt lacks the selection failure note"; fi
fi

name="refuses without V5 governance files and releases the maintenance lock"
new_case no-prompt; rm "${REPO}/.autonomous/V5_CYCLE_PROMPT.md"; run_loop
if expect "${name}" 2 0; then
  if maintenance_lock_free; then pass "${name}"; else fail "${name}" "maintenance lock still held after exit"; fi
fi

name="refuses with only V4 governance files present"
new_case v4-governance-only; rm "${REPO}/.autonomous/V5_ISSUES_WORKFLOW.md"; run_loop
expect "${name}" 2 0 && pass "${name}"

# start_loop_bg: the real controller in the background; $! is its own bash
# process (exec), so tests can signal it like systemd would.
start_loop_bg() {
  (exec env -i HOME="${CASE}" PATH="${CASE}/bin:/usr/bin:/bin" UID="${UID}" TENDO_ROOT="${REPO}" \
    XDG_RUNTIME_DIR="${RUNTIME}" FAKE_STATE="${STATE}" OPENCODE_BIN="${CASE}/bin/opencode" \
    CYCLE_TIMEOUT=60s CYCLE_STOP_GRACE="${1:-5}" bash "${REPO}/scripts/autonomous-loop.sh" >"${CASE}/out" 2>&1) &
  loop_pid=$!
}

# The cycle (fake opencode hook) records its pid, signals "started", then
# works until terminated. On TERM it records whether the maintenance lock
# was still held at that moment, i.e. before the controller exited.
CYCLE_UNTIL_TERM='echo $$ >"${FAKE_STATE}/cycle-pid"
lock="${XDG_RUNTIME_DIR}/tendo-maintenance-${UID}.lock"
trap '"'"'if flock -n "${lock}" true; then echo free; else echo held; fi >"${FAKE_STATE}/lock-at-cycle-term"; exit 0'"'"' TERM
touch "${FAKE_STATE}/cycle-started"
while :; do sleep 0.1; done'

name="SIGTERM stops the running cycle before the maintenance lock is released"
new_case killed; hook "${CYCLE_UNTIL_TERM}"; start_loop_bg
if wait_for "${STATE}/cycle-started"; then
  held_during=$(maintenance_lock_free && echo free || echo held)
  cycle_pid=$(cat "${STATE}/cycle-pid")
  kill -TERM "${loop_pid}"; RC=0; wait "${loop_pid}" || RC=$?
  if [[ "${held_during}" == held && "${RC}" == 143 ]] && ! pid_alive "${cycle_pid}" &&
    [[ "$(cat "${STATE}/lock-at-cycle-term" 2>/dev/null)" == held ]] && maintenance_lock_free &&
    [[ "$(cat "${STATE}/calls")" == 1 ]]; then pass "${name}"
  else fail "${name}" "during=${held_during} rc=${RC} cycle_alive=$(pid_alive "${cycle_pid}" && echo yes || echo no) lock_at_cycle_term=$(cat "${STATE}/lock-at-cycle-term" 2>/dev/null) free_after=$(maintenance_lock_free && echo yes || echo no); output: $(cat "${CASE}/out")"; fi
else
  kill "${loop_pid}" 2>/dev/null; wait "${loop_pid}" 2>/dev/null
  fail "${name}" "cycle never started; output: $(cat "${CASE}/out")"
fi

name="a cycle that ignores SIGTERM is killed before the controller exits"
new_case killed-stubborn
hook 'echo $$ >"${FAKE_STATE}/cycle-pid"; trap "" TERM; touch "${FAKE_STATE}/cycle-started"; while :; do sleep 0.1; done'
start_loop_bg 1
if wait_for "${STATE}/cycle-started"; then
  cycle_pid=$(cat "${STATE}/cycle-pid")
  kill -TERM "${loop_pid}"; RC=0; wait "${loop_pid}" || RC=$?
  if [[ "${RC}" == 143 ]] && ! pid_alive "${cycle_pid}" && maintenance_lock_free &&
    grep -q 'sending SIGKILL' "${CASE}/out"; then pass "${name}"
  else fail "${name}" "rc=${RC} cycle_alive=$(pid_alive "${cycle_pid}" && echo yes || echo no) free_after=$(maintenance_lock_free && echo yes || echo no); output: $(cat "${CASE}/out")"; fi
else
  kill "${loop_pid}" 2>/dev/null; wait "${loop_pid}" 2>/dev/null
  fail "${name}" "cycle never started"
fi

name="SIGTERM between cycles exits promptly and releases the lock"
new_case killed-sleeping; echo 1 >"${STATE}/rc.1"; hook 'touch "${FAKE_STATE}/cycle-done"'
(exec env -i HOME="${CASE}" PATH="${CASE}/bin:/usr/bin:/bin" UID="${UID}" TENDO_ROOT="${REPO}" \
  XDG_RUNTIME_DIR="${RUNTIME}" FAKE_STATE="${STATE}" OPENCODE_BIN="${CASE}/bin/opencode" \
  FAIL_SLEEP_BASE=600 bash "${REPO}/scripts/autonomous-loop.sh" >"${CASE}/out" 2>&1) &
loop_pid=$!
if wait_for "${STATE}/cycle-done" && wait_for_text "retrying after 600s" "${CASE}/out"; then
  kill -TERM "${loop_pid}"
  # A 600 s failure back-off must not delay the stop: it is interruptible.
  for _ in $(seq 100); do pid_alive "${loop_pid}" || break; sleep 0.1; done
  if pid_alive "${loop_pid}"; then
    kill -KILL "${loop_pid}"; wait "${loop_pid}" 2>/dev/null
    fail "${name}" "controller still sleeping 10 s after SIGTERM"
  else
    RC=0; wait "${loop_pid}" || RC=$?
    if [[ "${RC}" == 143 ]] && maintenance_lock_free; then pass "${name}"
    else fail "${name}" "rc=${RC} free=$(maintenance_lock_free && echo yes || echo no)"; fi
  fi
else
  kill "${loop_pid}" 2>/dev/null; wait "${loop_pid}" 2>/dev/null
  fail "${name}" "first cycle never finished; output: $(cat "${CASE}/out")"
fi

name="does not run while another autonomous loop holds its lock"
new_case loop-lock
hold_lock "${RUNTIME}/tendo-autonomous-${UID}.lock"; run_loop; release_lock
expect "${name}" 0 0 && pass "${name}"

name="V5 work-selection policy (scripts/test_v5_work_selection.py)"
if python3 "${SOURCE_ROOT}/scripts/test_v5_work_selection.py" >"${WORK}/selection-tests.out" 2>&1; then pass "${name}"
else fail "${name}" "$(tail -n 40 "${WORK}/selection-tests.out")"; fi

if ((failures > 0)); then
  echo "${failures} autonomous-loop test(s) failed" >&2
  exit 1
fi
echo "all autonomous-loop tests passed"
