#!/usr/bin/env bash
#
# Runs the QA audit suite, one area per server and per database.
#
# Each area gets its own port, so each gets its own `go run ./cmd/server --seed`
# and its own SQLite file. That isolation is not a nicety: the audit builds a
# world to interrogate it — custom roles, dozens of users, 214 requests — so two
# areas sharing a database contaminate each other's counts. Every figure in
# docs/qa/ was measured this way.
#
# Areas run in parallel by default (7 servers, 7 browsers). Set FERVID_AUDIT_JOBS=1
# for a sequential run on a smaller machine.
#
# Usage:
#   scripts/run-audit.sh                 # every area
#   scripts/run-audit.sh a d g           # only those areas
#   FERVID_AUDIT_JOBS=1 scripts/run-audit.sh
#   FERVID_AUDIT_PROJECT=mobile-chrome scripts/run-audit.sh d
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

PROJECT="${FERVID_AUDIT_PROJECT:-chromium}"
JOBS="${FERVID_AUDIT_JOBS:-8}"
LOGDIR="output/playwright/audit-logs"
mkdir -p "$LOGDIR"

# area:port — the ports the audit was originally measured on.
AREAS=(
  "a:4301:audit-a-rbac-permissions"
  "b:4302:audit-b-request-lifecycle"
  "c:4303:audit-c-approvals-cancellation"
  "d:4304:audit-d-linking-settlement"
  "e:4305:audit-e-recoverables"
  "f:4306:audit-f-notifications"
  "g:4307:audit-g-information-flow"
  "h:4310:audit-h-scale"
  "smoke:4308:audit-smoke"
)

wanted=("$@")
selected=()
for entry in "${AREAS[@]}"; do
  key="${entry%%:*}"
  if [ ${#wanted[@]} -eq 0 ]; then
    selected+=("$entry")
  else
    for w in "${wanted[@]}"; do
      [ "$w" = "$key" ] && selected+=("$entry")
    done
  fi
done

if [ ${#selected[@]} -eq 0 ]; then
  echo "no matching areas. known: a b c d e f g h smoke" >&2
  exit 2
fi

run_area() {
  local key="$1" port="$2" spec="$3"
  local log="$LOGDIR/$key.log"
  FERVID_E2E_PORT="$port" npx playwright test \
    -c playwright.audit.config.ts \
    "tests/e2e/$spec.spec.ts" \
    --project="$PROJECT" \
    --reporter=line >"$log" 2>&1
  echo $? >"$LOGDIR/$key.status"
}

echo "audit suite — project=$PROJECT jobs=$JOBS areas=${#selected[@]}"
echo

running=0
for entry in "${selected[@]}"; do
  IFS=':' read -r key port spec <<<"$entry"
  rm -f "$LOGDIR/$key.status"
  run_area "$key" "$port" "$spec" &
  running=$((running + 1))
  if [ "$running" -ge "$JOBS" ]; then
    wait -n 2>/dev/null || wait
    running=$((running - 1))
  fi
done
wait

fail=0
printf '%-7s %-6s %s\n' AREA PORT RESULT
printf '%-7s %-6s %s\n' ------- ------ ------
for entry in "${selected[@]}"; do
  IFS=':' read -r key port spec <<<"$entry"
  status="$(cat "$LOGDIR/$key.status" 2>/dev/null || echo '?')"
  # The counts line is the last one mentioning passed/failed/skipped.
  summary="$(grep -aE '[0-9]+ (passed|failed|skipped|flaky)' "$LOGDIR/$key.log" 2>/dev/null | tail -1 | sed 's/^ *//')"
  [ -z "$summary" ] && summary='(no summary — see log)'
  printf '%-7s %-6s %s\n' "$key" "$port" "$summary"
  [ "$status" != "0" ] && fail=1
done

echo
if [ "$fail" -eq 0 ]; then
  echo "audit suite: all areas green. logs in $LOGDIR/"
else
  echo "audit suite: FAILURES above. full output in $LOGDIR/<area>.log" >&2
fi
exit "$fail"
