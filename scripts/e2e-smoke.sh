#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${TENDO_TEST_POSTGRES_ADMIN_URL:?Set TENDO_TEST_POSTGRES_ADMIN_URL to the dedicated PostgreSQL 18 service}"
: "${TENDO_TEST_CLOCK_ACK:=isolated-e2e-only}"
if [[ "$TENDO_TEST_CLOCK_ACK" != isolated-e2e-only ]]; then
  printf 'TENDO_TEST_CLOCK_ACK must be isolated-e2e-only\n' >&2
  exit 1
fi
if [[ ! -x "$root/frontend/node_modules/.bin/playwright" ]]; then
  printf 'frontend/node_modules is missing; run npm ci in frontend/ first\n' >&2
  exit 1
fi

# Fixed, gitignored location: overwritten each run and never left behind by exec below.
tmp="$root/frontend/test-results/e2e-bin"
mkdir -p "$tmp"
(cd "$root/frontend" && npm run build)
[[ -s "$root/backend/internal/platform/webui/dist/index.html" ]] || { printf 'frontend build did not produce embedded dist/index.html\n' >&2; exit 1; }
find "$root/backend/internal/platform/webui/dist/assets" -type f -print -quit | grep -q . || { printf 'frontend build did not produce hashed embedded assets\n' >&2; exit 1; }
(cd "$root/backend" && go build -trimpath -o "$tmp/tendo" ./cmd/tendo && go build -trimpath -o "$tmp/oidc-provider" ./test/oidc-provider)
[[ -s "$tmp/tendo" && -s "$tmp/oidc-provider" ]] || { printf 'native production binaries were not built\n' >&2; exit 1; }
runner_args=(--root "$root" --app "$tmp/tendo" --provider "$tmp/oidc-provider")
mode_count=0
for mode in TENDO_README_SCREENSHOT TENDO_UX_AUDIT_DIR TENDO_SR_AUDIT_DIR TENDO_VISUAL_MODE; do
  [[ -n "${!mode:-}" ]] && ((mode_count+=1))
done
if (( mode_count > 1 )); then
  printf 'TENDO_README_SCREENSHOT, TENDO_UX_AUDIT_DIR, TENDO_SR_AUDIT_DIR and TENDO_VISUAL_MODE are mutually exclusive\n' >&2
  exit 1
fi
if [[ -n "${TENDO_README_SCREENSHOT:-}" ]]; then
  screenshot_path=$(realpath -m -- "$TENDO_README_SCREENSHOT")
  mkdir -p "$(dirname "$screenshot_path")"
  runner_args+=(--screenshot "$screenshot_path")
elif [[ -n "${TENDO_UX_AUDIT_DIR:-}" ]]; then
  audit_path=$(realpath -m -- "$TENDO_UX_AUDIT_DIR")
  mkdir -p "$audit_path"
  runner_args+=(--audit-dir "$audit_path")
elif [[ -n "${TENDO_SR_AUDIT_DIR:-}" ]]; then
  audit_path=$(realpath -m -- "$TENDO_SR_AUDIT_DIR")
  mkdir -p "$audit_path"
  runner_args+=(--sr-audit-dir "$audit_path")
elif [[ -n "${TENDO_VISUAL_MODE:-}" ]]; then
  if [[ "${TENDO_VISUAL_MODE}" != 1 ]]; then printf 'TENDO_VISUAL_MODE must be 1\n' >&2; exit 1; fi
  if [[ "${TENDO_VISUAL_UPDATE:-0}" != 0 && "${TENDO_VISUAL_UPDATE}" != 1 ]]; then printf 'TENDO_VISUAL_UPDATE must be 0 or 1\n' >&2; exit 1; fi
  if [[ "${TENDO_VISUAL_UPDATE:-0}" == 1 && -n "${CI:-}" ]]; then printf 'visual snapshots are never updated in CI\n' >&2; exit 1; fi
  runner_args+=(--visual)
fi
exec python3 "$root/scripts/postgres-test-service.py" python3 "$root/scripts/e2e-native-runner.py" "${runner_args[@]}"
