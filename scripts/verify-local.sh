#!/usr/bin/env bash
# Complete mandatory native pre-PR gate. No skip flags; fail fast and report stage timing.
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
start=$(date +%s)
failed_stage=""
completed=()

print_summary() {
  local status=${1:-0} total
  total=$(($(date +%s) - start))
  printf '\nLocal verification summary (%ss total):\n' "$total"
  if ((${#completed[@]})); then printf '  %s\n' "${completed[@]}"; fi
  if [[ -n "$failed_stage" ]]; then printf '  downstream stages: NOT RUN (fail-fast at %s)\n' "$failed_stage"; fi
  trap - EXIT
  exit "$status"
}

on_exit() {
  local status=$?
  if (( status != 0 )); then print_summary "$status"; fi
}
trap on_exit EXIT

# Execute each named stage in a fresh strict shell. This avoids Bash's errexit
# suppression when functions run in conditional/OR-list contexts and preserves
# the exact stage status for the summary.
run_stage() {
  local name=$1
  shift
  local began ended elapsed status stage_file
  failed_stage=$name
  began=$(date +%s)
  printf '\n==> %s\n' "$name"
  stage_file=$(mktemp "${TMPDIR:-/tmp}/tendo-verify-stage.XXXXXX")
  printf '%s\n' 'set -Eeuo pipefail' 'cd "$VERIFY_ROOT"' "$@" > "$stage_file"
  status=0
  VERIFY_ROOT="$root" bash "$stage_file" || status=$?
  local cleanup_status=0
  rm -f "$stage_file" || cleanup_status=$?
  if (( status == 0 && cleanup_status != 0 )); then status=$cleanup_status; fi
  ended=$(date +%s)
  elapsed=$((ended - began))
  if (( status == 0 )); then
    completed+=("$name:PASS:${elapsed}s")
    failed_stage=""
    printf '<== %s PASS (%ss)\n' "$name" "$elapsed"
  else
    completed+=("$name:FAIL:${elapsed}s")
    printf '<== %s FAIL (%ss, exit %s)\n' "$name" "$elapsed" "$status" >&2
    print_summary "$status"
  fi
}

run_stage bootstrap 'for f in AGENTS.md .autonomous/GUARDRAILS.md .autonomous/CHARTER.md .autonomous/PRODUCT.md .autonomous/ROADMAP.md .autonomous/UX.md .autonomous/ARCHITECTURE.md .autonomous/API.md .autonomous/TESTING.md .autonomous/DOCUMENTATION.md .autonomous/DEFINITION_OF_DONE.md .autonomous/FLOW.md .autonomous/STATE.md .autonomous/LEARNINGS.md .autonomous/CYCLE_PROMPT.md; do [[ -f "$f" ]] || { printf "required repository context missing: %s\\n" "$f" >&2; exit 1; }; done' 'bash -n scripts/autonomous-loop.sh'
run_stage go-format 'files=$(gofmt -l $(find backend -name "*.go" -type f))' 'if [[ -n "$files" ]]; then printf "Go files need formatting:\\n%s\\n" "$files" >&2; exit 1; fi'
run_stage go-vet 'cd backend' 'go vet ./...'
run_stage go-unit-race 'cd backend' 'all_packages=$(go list ./...)' 'packages=$(printf "%s\\n" "$all_packages" | grep -v -e "/internal/identity/postgres$" -e "/internal/household/postgres$" -e "/internal/subject/postgres$" -e "/internal/item/postgres$")' '[[ -n "$packages" ]]' 'go test -race $packages'
run_stage postgres-integration 'bash scripts/setup-integration.sh'
run_stage sqlc-drift 'bash scripts/check-sqlc.sh'
run_stage go-build 'cd backend' 'go build ./...'
run_stage api-contract-audit 'cd api' 'npm ci' 'npm audit --audit-level=moderate' 'npm run check'
run_stage frontend-check 'cd frontend' 'npm ci' 'npm audit --audit-level=moderate' 'npm run check'
run_stage native-harness-tests 'python3 -m unittest scripts/test_postgres_test_service.py scripts/test_e2e_native_runner.py scripts/test_verify_local.py scripts/test_ci_workflow.py scripts/test_toolchain_manifest.py scripts/test_dev_toolchain.py' 'python3 scripts/test_postgres_live.py' 'python3 -m unittest scripts/test_generation_drift.py' 'python3 scripts/test_e2e_interrupt.py'
run_stage native-production-e2e 'bash scripts/e2e-smoke.sh'
print_summary 0
