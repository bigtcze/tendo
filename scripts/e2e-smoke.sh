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
installed=$(node -p "require('$root/frontend/node_modules/@playwright/test/package.json').version")
if [[ "$installed" != 1.64.0 ]]; then
  printf 'Playwright version mismatch: expected 1.64.0, found %s\n' "$installed" >&2
  exit 1
fi
command -v go >/dev/null 2>&1 || { printf 'go is required\n' >&2; exit 1; }
tmp=$(mktemp -d)
frontend_dist="$root/frontend/dist"
embedded_dist="$root/backend/internal/platform/webui/dist"
frontend_existed=0
embedded_existed=0
if [[ -e "$frontend_dist" ]]; then frontend_existed=1; cp -a "$frontend_dist" "$tmp/frontend-dist-before"; fi
if [[ -e "$embedded_dist" ]]; then embedded_existed=1; cp -a "$embedded_dist" "$tmp/embedded-dist-before"; fi
cleanup() {
  local result=$?
  trap - EXIT
  local cleanup_failed=0
  rm -rf "$frontend_dist" "$embedded_dist" || cleanup_failed=1
  if (( frontend_existed )); then cp -a "$tmp/frontend-dist-before" "$frontend_dist" || cleanup_failed=1; fi
  if (( embedded_existed )); then cp -a "$tmp/embedded-dist-before" "$embedded_dist" || cleanup_failed=1; fi
  rm -rf "$tmp" || cleanup_failed=1
  if (( result == 0 && cleanup_failed )); then result=1; fi
  if (( result != 0 && cleanup_failed )); then printf 'E2E cleanup also failed; primary test failure preserved\n' >&2; fi
  exit "$result"
}
trap cleanup EXIT
rm -rf "$frontend_dist" "$embedded_dist"
(cd "$root/frontend" && npm run build)
[[ -s "$root/backend/internal/platform/webui/dist/index.html" ]] || { printf 'frontend build did not produce embedded dist/index.html\n' >&2; exit 1; }
find "$root/backend/internal/platform/webui/dist/assets" -type f -print -quit | grep -q . || { printf 'frontend build did not produce hashed embedded assets\n' >&2; exit 1; }
(cd "$root/backend" && go build -trimpath -o "$tmp/tendo" ./cmd/tendo && go build -trimpath -o "$tmp/oidc-provider" ./test/oidc-provider)
[[ -s "$tmp/tendo" && -s "$tmp/oidc-provider" ]] || { printf 'native production binaries were not built\n' >&2; exit 1; }
export TENDO_E2E_BINARY="$tmp/tendo" TENDO_OIDC_PROVIDER_BINARY="$tmp/oidc-provider" TENDO_E2E_ROOT="$root"
wrapper_child=""
forward_signal() {
  local sig=$1 status=0
  trap - INT TERM
  if [[ -n "$wrapper_child" ]]; then
    if kill -0 "$wrapper_child" 2>/dev/null; then
      kill -s "$sig" "$wrapper_child" || status=$?
    fi
    # Normal signal cleanup budget in postgres-test-service is bounded; leave
    # headroom for this shell to restore frontend/embed dist snapshots.
    wait_for=35
    ( sleep "$wait_for"; kill -KILL "$wrapper_child" 2>/dev/null ) &
    watchdog=$!
    wait "$wrapper_child" || status=$?
    if kill -0 "$watchdog" 2>/dev/null; then kill "$watchdog" 2>/dev/null || :; fi
    wait "$watchdog" 2>/dev/null || :
    wrapper_child=""
  fi
  if (( status != 0 )); then printf 'failed forwarding %s to owned E2E wrapper (status %s)\n' "$sig" "$status" >&2; fi
  exit $((128 + $(kill -l "$sig")))
}
trap 'forward_signal INT' INT
trap 'forward_signal TERM' TERM
set +e
python3 "$root/scripts/postgres-test-service.py" python3 "$root/scripts/e2e-native-runner.py" --root "$root" --app "$tmp/tendo" --provider "$tmp/oidc-provider" &
wrapper_child=$!
wait "$wrapper_child"
result=$?
wrapper_child=""
set -e
exit "$result"
