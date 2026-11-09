#!/usr/bin/env bash
# Behavior tests for scripts/autonomous-loop.sh (the V4 feature controller).
# Each case runs the real script from a disposable git checkout with fake
# `systemctl` and `opencode` executables, and asserts its exit status and
# whether, how often and how an OpenCode cycle was started.
set -euo pipefail

SOURCE_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "${WORK}"' EXIT

failures=0
pass() { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2" >&2; failures=$((failures + 1)); }

make_fakes() {
  local bin="$1"
  mkdir -p "${bin}"
  # systemctl --user is-enabled|is-active <unit>: answers from files in
  # $FAKE_STATE/<verb>.<unit>; an absent file prints nothing and fails.
  cat >"${bin}/systemctl" <<'EOF'
#!/usr/bin/env bash
[[ "$1" == "--user" ]] || exit 9
file="${FAKE_STATE}/$2.$3"
[[ -f "${file}" ]] || exit 4
cat "${file}"
EOF
  # opencode run ...: records each invocation and whether the maintenance
  # flock was free, then runs an optional per-test hook.
  cat >"${bin}/opencode" <<'EOF'
#!/usr/bin/env bash
count=$(( $(cat "${FAKE_STATE}/calls" 2>/dev/null || echo 0) + 1 ))
echo "${count}" >"${FAKE_STATE}/calls"
printf '%s\n' "$@" >"${FAKE_STATE}/args.${count}"
if flock -n "${XDG_RUNTIME_DIR}/tendo-maintenance-${UID}.lock" true; then
  echo free >"${FAKE_STATE}/maintenance-lock.${count}"
else
  echo held >"${FAKE_STATE}/maintenance-lock.${count}"
fi
if [[ -x "${FAKE_STATE}/hook" ]]; then "${FAKE_STATE}/hook" "${count}"; fi
exit "$(cat "${FAKE_STATE}/rc.${count}" 2>/dev/null || echo 0)"
EOF
  chmod +x "${bin}/systemctl" "${bin}/opencode"
}

# new_case <name>: fresh checkout on clean main with V3_COMPLETE tracked,
# V4 governance files present and the maintenance controller fully off.
new_case() {
  CASE="${WORK}/$1"
  REPO="${CASE}/repo"
  STATE="${CASE}/state"
  RUNTIME="${CASE}/run"
  mkdir -p "${REPO}/scripts" "${REPO}/.autonomous" "${STATE}" "${RUNTIME}"
  make_fakes "${CASE}/bin"
  cp "${SOURCE_ROOT}/scripts/autonomous-loop.sh" "${REPO}/scripts/"
  echo "V3 done" >"${REPO}/.autonomous/V3_COMPLETE"
  echo "V4 PROMPT MARKER" >"${REPO}/.autonomous/V4_CYCLE_PROMPT.md"
  echo "workflow" >"${REPO}/.autonomous/V4_ISSUES_WORKFLOW.md"
  git -C "${REPO}" init -q -b main
  git -C "${REPO}" add -A
  git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m init
  maintenance timer disabled inactive
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

COMMIT_V4_COMPLETE='echo done >.autonomous/V4_COMPLETE; git add -A; git -c user.name=t -c user.email=t@t commit -q -m complete'

run_loop() {
  set +e
  env -i HOME="${CASE}" PATH="${CASE}/bin:/usr/bin:/bin" UID="${UID}" \
    XDG_RUNTIME_DIR="${RUNTIME}" FAKE_STATE="${STATE}" \
    OPENCODE_BIN="${CASE}/bin/opencode" SUCCESS_SLEEP=0 FAIL_SLEEP_BASE=0 \
    FAIL_SLEEP_MAX=0 CYCLE_TIMEOUT=30s \
    timeout 15 bash "${REPO}/scripts/autonomous-loop.sh" \
    >"${CASE}/out" 2>&1
  RC=$?
  set -e
  CALLS=$(cat "${STATE}/calls" 2>/dev/null || echo 0)
}

expect() {
  local name="$1" rc="$2" calls="$3"
  if [[ "${RC}" != "${rc}" || "${CALLS}" != "${calls}" ]]; then
    fail "${name}" "rc=${RC} calls=${CALLS}, want rc=${rc} calls=${calls}; output: $(cat "${CASE}/out")"
    return 1
  fi
}

name="runs one V4 cycle and exits when V4_COMPLETE is merged on main"
new_case run-v4; hook "${COMMIT_V4_COMPLETE}"; run_loop
if expect "${name}" 0 1; then
  args="${STATE}/args.1"
  if grep -qx 'V4 PROMPT MARKER' "${args}" &&
    grep -qx 'Tendo V4 autonomous cycle 1' "${args}" &&
    grep -qx -- '--agent' "${args}" &&
    grep -qx orchestrator "${args}"; then pass "${name}"
  else fail "${name}" "unexpected opencode arguments: $(tr '\n' ' ' <"${args}")"; fi
fi

name="holds the maintenance lock while a V4 cycle runs"
if [[ "$(cat "${WORK}/run-v4/state/maintenance-lock.1" 2>/dev/null)" == held ]]; then pass "${name}"
else fail "${name}" "maintenance flock was acquirable during the cycle"; fi

name="tracked V3_COMPLETE alone does not stop the V4 loop"
new_case v3-only; hook "${COMMIT_V4_COMPLETE}"; run_loop
expect "${name}" 0 1 && pass "${name}"

name="exits without a cycle when V4_COMPLETE is tracked on clean main"
new_case v4-done
echo done >"${REPO}/.autonomous/V4_COMPLETE"
git -C "${REPO}" add -A; git -C "${REPO}" -c user.name=t -c user.email=t@t commit -q -m done
run_loop
expect "${name}" 0 0 && pass "${name}"

name="untracked V4_COMPLETE does not count as completion"
new_case v4-untracked; echo x >"${REPO}/.autonomous/V4_COMPLETE"
hook 'rm -f .autonomous/V4_COMPLETE; '"${COMMIT_V4_COMPLETE}"; run_loop
expect "${name}" 0 1 && pass "${name}"

for state in "timer enabled inactive" "timer enabled active" "timer disabled active" \
  "timer enabled-runtime inactive" "service static activating" "service static active"; do
  name="refuses while maintenance ${state}"
  new_case "refuse-${state// /-}"; maintenance ${state}; run_loop
  expect "${name}" 3 0 && pass "${name}"
done

name="refuses when maintenance unit state is unreadable"
new_case unreadable; rm "${STATE}/is-enabled.tendo-maintenance.timer"; run_loop
expect "${name}" 3 0 && pass "${name}"

name="accepts masked and not-found maintenance units"
new_case masked; maintenance timer masked inactive; maintenance service not-found inactive
hook "${COMMIT_V4_COMPLETE}"; run_loop
expect "${name}" 0 1 && pass "${name}"

name="refuses while a maintenance run holds its lock"
new_case maint-lock
flock "${RUNTIME}/tendo-maintenance-${UID}.lock" sleep 30 &
holder=$!
for _ in $(seq 50); do
  flock -n "${RUNTIME}/tendo-maintenance-${UID}.lock" true || break
  sleep 0.1
done
run_loop
kill "${holder}" 2>/dev/null || true; wait "${holder}" 2>/dev/null || true
expect "${name}" 3 0 && pass "${name}"

name="stops before the next cycle if maintenance is re-enabled"
new_case reenabled
hook "echo enabled >${STATE}/is-enabled.tendo-maintenance.timer"; run_loop
expect "${name}" 3 1 && pass "${name}"

name="retries a failed cycle, then finishes"
new_case retry
hook 'if [[ $1 == 2 ]]; then '"${COMMIT_V4_COMPLETE}"'; fi'
echo 1 >"${STATE}/rc.1"; run_loop
expect "${name}" 0 2 && pass "${name}"

name="refuses without V4 governance files"
new_case no-prompt; rm "${REPO}/.autonomous/V4_CYCLE_PROMPT.md"; run_loop
expect "${name}" 2 0 && pass "${name}"

name="does not run while another autonomous loop holds its lock"
new_case loop-lock
flock "${RUNTIME}/tendo-autonomous-${UID}.lock" sleep 30 &
holder=$!
for _ in $(seq 50); do
  flock -n "${RUNTIME}/tendo-autonomous-${UID}.lock" true || break
  sleep 0.1
done
run_loop
kill "${holder}" 2>/dev/null || true; wait "${holder}" 2>/dev/null || true
expect "${name}" 0 0 && pass "${name}"

if ((failures > 0)); then
  echo "${failures} autonomous-loop test(s) failed" >&2
  exit 1
fi
echo "all autonomous-loop tests passed"
