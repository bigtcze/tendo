#!/usr/bin/env bash
set -u -o pipefail

# V4 Modern Everyday feature controller (tendo-autonomous.service).
# Exits successfully once .autonomous/V4_COMPLETE is tracked on clean main.
# Refuses to run while the separate post-V3 maintenance controller
# (tendo-maintenance.timer/.service, outside this repository) is enabled or
# active, and holds that controller's own flock for its whole lifetime so a
# manually started maintenance run exits as "already running".

# Canonical checkout: the host wrapper (opencode-tendo-safe) always runs and
# recovers sessions there, so prompt and completion state must come from the
# same checkout. TENDO_ROOT exists only for the behavior tests.
ROOT="${TENDO_ROOT:-/home/tomas/repositories/tendo}"
PROMPT_FILE="${ROOT}/.autonomous/V4_CYCLE_PROMPT.md"
WORKFLOW_FILE="${ROOT}/.autonomous/V4_ISSUES_WORKFLOW.md"
COMPLETE_FILE=".autonomous/V4_COMPLETE"

OPENCODE_BIN="${OPENCODE_BIN:-/home/tomas/.opencode/bin/opencode}"
SUCCESS_SLEEP="${SUCCESS_SLEEP:-45}"
FAIL_SLEEP_BASE="${FAIL_SLEEP_BASE:-120}"
FAIL_SLEEP_MAX="${FAIL_SLEEP_MAX:-900}"
CYCLE_TIMEOUT="${CYCLE_TIMEOUT:-4h}"
RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp}"
LOCK_FILE="${RUNTIME_DIR}/tendo-autonomous-${UID}.lock"
# Same path as ~/.local/bin/tendo-maintenance.sh uses for its own flock.
MAINTENANCE_LOCK_FILE="${RUNTIME_DIR}/tendo-maintenance-${UID}.lock"
MAINTENANCE_UNITS=(tendo-maintenance.timer tendo-maintenance.service)
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

for tool in flock timeout git systemctl; do
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
  echo "[tendo] post-V3 maintenance run holds ${MAINTENANCE_LOCK_FILE}; refusing to start V4 cycles." >&2
  exit "${REFUSED}"
fi

# Fails closed: any unit state other than disabled/masked/not-found and
# inactive, or an unreadable state, refuses the V4 cycle.
maintenance_controller_off() {
  local unit enabled active
  for unit in "${MAINTENANCE_UNITS[@]}"; do
    enabled="$(systemctl --user is-enabled "${unit}" 2>/dev/null)"
    case "${enabled}" in
      disabled|masked|masked-runtime|not-found|static) ;;
      *)
        echo "[tendo] ${unit} is-enabled=${enabled:-unknown}; disable the maintenance controller first." >&2
        return 1 ;;
    esac
    active="$(systemctl --user is-active "${unit}" 2>/dev/null)"
    case "${active}" in
      inactive|failed) ;;
      *)
        echo "[tendo] ${unit} is-active=${active:-unknown}; wait for or stop the maintenance controller first." >&2
        return 1 ;;
    esac
  done
}

is_complete_on_main() {
  [[ "$(git branch --show-current 2>/dev/null)" == "main" ]] || return 1
  git diff --quiet || return 1
  git diff --cached --quiet || return 1
  git cat-file -e "HEAD:${COMPLETE_FILE}" 2>/dev/null
}

if is_complete_on_main; then
  echo "[tendo] V4_COMPLETE is tracked on clean main; exiting successfully."
  exit 0
fi

cycle=0
failure_sleep="${FAIL_SLEEP_BASE}"

while true; do
  if is_complete_on_main; then
    echo "[tendo] V4 complete on protected main; exiting successfully."
    exit 0
  fi

  maintenance_controller_off || exit "${REFUSED}"

  for file in "${PROMPT_FILE}" "${WORKFLOW_FILE}"; do
    [[ -f "${file}" ]] || {
      echo "[tendo] missing ${file}; V4 governance is not active in this checkout." >&2
      exit 2
    }
  done

  cycle=$((cycle + 1))
  PROMPT="$(cat "${PROMPT_FILE}")"

  echo "[tendo] starting V4 autonomous cycle ${cycle}"

  if timeout --signal=TERM --kill-after=60s "${CYCLE_TIMEOUT}" \
    "${OPENCODE_BIN}" run --auto --agent orchestrator \
    --title "Tendo V4 autonomous cycle ${cycle}" \
    "${PROMPT}" 8>&- 9>&-; then
    rc=0
  else
    rc=$?
  fi

  if is_complete_on_main; then
    echo "[tendo] V4 completion merged and verified on main; exiting successfully."
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
