# Log leak search shared by the deployment smokes (sourced, not executed).
# Secrets are random base64url values and may start with "-": without "--" grep reads one as options (a "-V..."
# value exits 0 as a false leak, an invalid one exits 2). Any grep error (rc 2, for example an unreadable log)
# fails the smoke instead of passing as "not found".

# log_contains <fixed string> <file>: 0 when present, 1 when absent; exits the smoke on a search error.
log_contains() {
  local rc=0
  grep -qF -- "$1" "$2" || rc=$?
  (( rc <= 1 )) || { echo "log leak search failed rc=$rc" >&2; exit 1; }
  return "$rc"
}

# log_matches <extended regex> <file>: like log_contains, for parameter-marker patterns.
log_matches() {
  local rc=0
  grep -qE -- "$1" "$2" || rc=$?
  (( rc <= 1 )) || { echo "log marker search failed rc=$rc" >&2; exit 1; }
  return "$rc"
}

# Self-check the searches on dash-leading values and on an unreadable log before trusting them with real secrets.
# The error cases run in a subshell, because a search error exits the caller.
log_search_self_check() {
  local canary_log missing
  canary_log=$(mktemp)
  missing="$canary_log.missing"
  printf 'request -Vcanary-leak -Xcanary-leak done\n' >"$canary_log"
  for canary in -Vcanary-leak -Xcanary-leak; do
    log_contains "$canary" "$canary_log" || { rm -f "$canary_log"; echo "log leak check missed dash-leading value $canary" >&2; exit 1; }
  done
  for absent in -Vcanary-absent -Xcanary-absent; do
    if log_contains "$absent" "$canary_log"; then rm -f "$canary_log"; echo "log leak check reported absent dash-leading value $absent" >&2; exit 1; fi
  done
  log_matches '-Vcanary-l[e]ak' "$canary_log" || { rm -f "$canary_log"; echo 'log marker check missed a dash-leading pattern' >&2; exit 1; }
  # A search that returns at all (found or "not found") on an unreadable log has swallowed the error.
  if [[ "$( (if log_contains -Vcanary-leak "$missing"; then :; fi; echo returned) 2>/dev/null)" == *returned* ]]; then
    rm -f "$canary_log"; echo 'log leak check did not fail on an unreadable log' >&2; exit 1
  fi
  if [[ "$( (if log_matches canary "$missing"; then :; fi; echo returned) 2>/dev/null)" == *returned* ]]; then
    rm -f "$canary_log"; echo 'log marker check did not fail on an unreadable log' >&2; exit 1
  fi
  rm -f "$canary_log"
}
