#!/usr/bin/env bash
set -u -o pipefail

# V5 Refined & Reliable feature controller (tendo-autonomous.service).
# Exits successfully once .autonomous/V5_COMPLETE is tracked on clean main.
# Historical markers (V3_COMPLETE, V4_COMPLETE, ...) never stop it.
#
# Maintenance handoff: the separate post-release maintenance controller
# (tendo-maintenance.timer/.service, outside this repository) takes the flock
# ${XDG_RUNTIME_DIR}/tendo-maintenance-${UID}.lock non-blockingly and exits
# "already running" when it is held. This loop takes the SAME flock before
# its first cycle and holds it until it exits, so the timer may stay enabled:
# scheduled maintenance runs skip while V5 runs and resume on their next
# trigger after V5_COMPLETE releases the lock. The lock, not the unit's
# enabled state, is the truth: a maintenance run that holds it is actual
# competing work, and the loop then refuses to start (exit 3).

# Canonical checkout: the host wrapper (opencode-tendo-safe) always runs and
# recovers sessions there, so prompt and completion state must come from the
# same checkout. TENDO_ROOT exists only for the behavior tests.
ROOT="${TENDO_ROOT:-/home/tomas/repositories/tendo}"
PROMPT_FILE="${ROOT}/.autonomous/V5_CYCLE_PROMPT.md"
WORKFLOW_FILE="${ROOT}/.autonomous/V5_ISSUES_WORKFLOW.md"
SELECTION_SCRIPT="${ROOT}/scripts/v5_work_selection.py"
COMPLETE_FILE=".autonomous/V5_COMPLETE"

OPENCODE_BIN="${OPENCODE_BIN:-/home/tomas/.opencode/bin/opencode}"
SUCCESS_SLEEP="${SUCCESS_SLEEP:-45}"
FAIL_SLEEP_BASE="${FAIL_SLEEP_BASE:-120}"
FAIL_SLEEP_MAX="${FAIL_SLEEP_MAX:-900}"
CYCLE_TIMEOUT="${CYCLE_TIMEOUT:-4h}"
SELECTION_TIMEOUT="${SELECTION_TIMEOUT:-300s}"
RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp}"
LOCK_FILE="${RUNTIME_DIR}/tendo-autonomous-${UID}.lock"
# Same path as ~/.local/bin/tendo-maintenance.sh uses for its own flock.
MAINTENANCE_LOCK_FILE="${RUNTIME_DIR}/tendo-maintenance-${UID}.lock"
REFUSED=3

cd "${ROOT}" || {
  echo "[tendo] cannot cd to ${ROOT}" >&2
  exit 2
}

if [[ ! -x "${OPENCODE_BIN}" ]]; then
  fallback="$(command -v opencode 2>/dev/null || true)"
  if [[ -n "${fallback}" ]]; then
    OPENCODE_BIN="${fallback}"
  else
    echo "[tendo] opencode not found; expected ${OPENCODE_BIN}" >&2
    exit 2
  fi
fi

for tool in flock timeout git; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "[tendo] ${tool} not found" >&2
    exit 2
  }
done

exec 9>"${LOCK_FILE}"
if ! flock -n 9; then
  echo "[tendo] another autonomous loop already holds ${LOCK_FILE}; exiting."
  exit 0
fi

exec 8>"${MAINTENANCE_LOCK_FILE}"
if ! flock -n 8; then
  echo "[tendo] a maintenance run is active (holds ${MAINTENANCE_LOCK_FILE}); refusing to start V5 cycles." >&2
  exit "${REFUSED}"
fi
echo "[tendo] holding ${MAINTENANCE_LOCK_FILE}; scheduled maintenance runs skip until V5 completes."

is_complete_on_main() {
  [[ "$(git branch --show-current 2>/dev/null)" == "main" ]] || return 1
  git diff --quiet || return 1
  git diff --cached --quiet || return 1
  git cat-file -e "HEAD:${COMPLETE_FILE}" 2>/dev/null
}

# Advisory: the locked V5_ISSUES_WORKFLOW priority computed from live GitHub
# state. The session prompt stays the byte-exact V5_CYCLE_PROMPT; this is
# appended after it. A selection failure never blocks a cycle.
work_selection() {
  local output rc
  if command -v python3 >/dev/null 2>&1 && [[ -f "${SELECTION_SCRIPT}" ]]; then
    output="$(timeout "${SELECTION_TIMEOUT}" python3 "${SELECTION_SCRIPT}" 2>&1 8>&- 9>&-)"
    rc=$?
  else
    output="python3 or ${SELECTION_SCRIPT} missing"
    rc=127
  fi
  if [[ "${rc}" -eq 0 ]]; then
    printf '%s\n' "${output}"
  else
    printf 'SELECTION UNAVAILABLE rc=%s; apply the V5_ISSUES_WORKFLOW priority manually.\n%s\n' \
      "${rc}" "$(printf '%s\n' "${output}" | tail -n 20)"
  fi
}

if is_complete_on_main; then
  echo "[tendo] V5_COMPLETE is tracked on clean main; exiting successfully."
  exit 0
fi

cycle=0
failure_sleep="${FAIL_SLEEP_BASE}"

while true; do
  if is_complete_on_main; then
    echo "[tendo] V5 complete on protected main; exiting successfully."
    exit 0
  fi

  for file in "${PROMPT_FILE}" "${WORKFLOW_FILE}"; do
    [[ -f "${file}" ]] || {
      echo "[tendo] missing ${file}; V5 governance is not active in this checkout." >&2
      exit 2
    }
  done

  cycle=$((cycle + 1))
  selection="$(work_selection)"
  echo "[tendo] cycle ${cycle} work selection: $(printf '%s\n' "${selection}" | head -n 1)"
  PROMPT="$(cat "${PROMPT_FILE}")"
  PROMPT+=$'\n\n'"## Controller work selection for this cycle (scripts/v5_work_selection.py)"
  PROMPT+=$'\n'"Advisory snapshot taken before this session; re-verify live state, then follow it unless the live state differs."
  PROMPT+=$'\n\n'"${selection}"

  echo "[tendo] starting V5 autonomous cycle ${cycle}"

  if timeout --signal=TERM --kill-after=60s "${CYCLE_TIMEOUT}" \
    "${OPENCODE_BIN}" run --auto --agent orchestrator \
    --title "Tendo V5 autonomous cycle ${cycle}" \
    "${PROMPT}" 8>&- 9>&-; then
    rc=0
  else
    rc=$?
  fi

  if is_complete_on_main; then
    echo "[tendo] V5 completion merged and verified on main; releasing the maintenance lock and exiting successfully."
    exit 0
  fi

  if [[ "${rc}" -eq 0 ]]; then
    failure_sleep="${FAIL_SLEEP_BASE}"
    sleep "${SUCCESS_SLEEP}"
  else
    if [[ "${rc}" -eq 124 ]]; then
      echo "[tendo] cycle timed out after ${CYCLE_TIMEOUT}" >&2
    else
      echo "[tendo] cycle failed rc=${rc}" >&2
    fi

    echo "[tendo] retrying after ${failure_sleep}s" >&2
    sleep "${failure_sleep}"

    next_sleep=$((failure_sleep * 2))
    if (( next_sleep > FAIL_SLEEP_MAX )); then
      next_sleep="${FAIL_SLEEP_MAX}"
    fi
    failure_sleep="${next_sleep}"
  fi
done
