#!/usr/bin/env bash
# Offline behavior tests for the evergreen autonomy controller stack:
# work selection, controller loop, GitHub label migration and the one-time
# host installer. Uses only fakes and disposable directories; never touches
# GitHub, the user systemd instance or host state. Needs Bash, Git, Python 3,
# flock, timeout and sha256sum.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

run() {
  echo "--> $*"
  "$@"
}

run python3 scripts/test_autonomy_work_selection.py
run python3 scripts/autonomy-work-selection.py --self-check
run bash scripts/autonomy-controller-test.sh
run python3 scripts/test_autonomy_migration.py
run bash scripts/install-autonomy-service-test.sh
run python3 scripts/test_governance_contracts.py
