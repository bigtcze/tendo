#!/usr/bin/env bash
set -u -o pipefail

ROOT="/home/tomas/repositories/tendo"
PROMPT_FILE="${ROOT}/.autonomous/CYCLE_PROMPT.md"
COMPLETE_FILE=".autonomous/PROJECT_COMPLETE"

OPENCODE_BIN="${OPENCODE_BIN:-/home/tomas/.opencode/bin/opencode}"
SUCCESS_SLEEP="${SUCCESS_SLEEP:-45}"
FAIL_SLEEP_BASE="${FAIL_SLEEP_BASE:-120}"
FAIL_SLEEP_MAX="${FAIL_SLEEP_MAX:-900}"
CYCLE_TIMEOUT="${CYCLE_TIMEOUT:-4h}"
LOCK_FILE="${XDG_RUNTIME_DIR:-/tmp}/tendo-autonomous-${UID}.lock"

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

command -v flock >/dev/null 2>&1 || {
  echo "[tendo] flock not found" >&2
  exit 2
}
command -v timeout >/dev/null 2>&1 || {
  echo "[tendo] timeout not found" >&2
  exit 2
}

exec 9>"${LOCK_FILE}"
if ! flock -n 9; then
  echo "[tendo] another autonomous loop already holds ${LOCK_FILE}; exiting."
  exit 0
fi

is_complete_on_main() {
  [[ "$(git branch --show-current 2>/dev/null)" == "main" ]] || return 1
  git diff --quiet || return 1
  git diff --cached --quiet || return 1
  git cat-file -e "HEAD:${COMPLETE_FILE}" 2>/dev/null
}

if is_complete_on_main; then
  echo "[tendo] PROJECT_COMPLETE is tracked on clean main; exiting successfully."
  exit 0
fi

cycle=0
failure_sleep="${FAIL_SLEEP_BASE}"

while true; do
  if is_complete_on_main; then
    echo "[tendo] V1 complete on protected main; exiting successfully."
    exit 0
  fi

  [[ -f "${PROMPT_FILE}" ]] || {
    echo "[tendo] missing ${PROMPT_FILE}" >&2
    exit 2
  }

  cycle=$((cycle + 1))
  PROMPT="$(cat "${PROMPT_FILE}")"

  echo "[tendo] starting autonomous cycle ${cycle}"

  if timeout --signal=TERM --kill-after=60s "${CYCLE_TIMEOUT}"       "${OPENCODE_BIN}" run --auto --agent orchestrator       --title "Tendo autonomous cycle ${cycle}"       "${PROMPT}"; then
    rc=0
  else
    rc=$?
  fi

  if is_complete_on_main; then
    echo "[tendo] V1 completion merged and verified on main; exiting successfully."
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
