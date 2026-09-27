#!/usr/bin/env bash
# Deploy a reviewed, clean release. Configure the destination and verify its SSH
# host key in known_hosts before running. The remote account needs write access
# to /opt/fervid-budget and narrowly scoped sudo access for restart/backup.
set -euo pipefail
SERVER="${FERVID_DEPLOY_HOST:?Set FERVID_DEPLOY_HOST explicitly}"
PUBLIC_URL="${FERVID_PUBLIC_URL:?Set FERVID_PUBLIC_URL explicitly}"
[[ "$SERVER" == *@* && "$SERVER" != root@* && "$SERVER" != -* ]] || { echo "Use an explicit non-root deployment account"; exit 1; }
[[ "$PUBLIC_URL" == https://* ]] || { echo "FERVID_PUBLIC_URL must use HTTPS"; exit 1; }
APP_DIR=/opt/fervid-budget
SSH_OPTS=(-o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=20)
cd "$(dirname "$0")"
export GOTOOLCHAIN=go1.27.1
[[ "$(go env GOVERSION)" == go1.27.1 ]] || { echo "Go 1.27.1 is required"; exit 1; }
[[ -z "$(git status --porcelain --untracked-files=normal)" ]] || { echo "Commit the reviewed changes before deploying"; exit 1; }
release_dir="$(mktemp -d)"
trap 'rm -rf "$release_dir"' EXIT
GOBIN="$release_dir/tools" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
"$release_dir/tools/govulncheck" ./cmd/... ./internal/...
go test ./cmd/... ./internal/...
revision="$(git rev-parse HEAD)"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$revision" -o "$release_dir/server" ./cmd/server
mkdir "$release_dir/static"
for asset in fervid-ds.css fervid-app.js fervid-logo.svg budget-planner.css budget-ux.css budget-planner.js htmx.min.js; do
 cp "web/static/$asset" "$release_dir/static/$asset"
done
printf '%s\n' "$revision" > "$release_dir/revision.txt"
go version -m "$release_dir/server" > "$release_dir/build-info.txt"
(cd "$release_dir" && shasum -a 256 server static/* > SHA256SUMS)
# Stage a complete release; the remote verifies its bytes before stopping anything.
ssh "${SSH_OPTS[@]}" "$SERVER" 'mkdir -p /opt/fervid-budget/incoming'
scp "${SSH_OPTS[@]}" -r "$release_dir/server" "$release_dir/static" "$release_dir/revision.txt" "$release_dir/build-info.txt" "$release_dir/SHA256SUMS" "$SERVER:$APP_DIR/incoming/"
ssh "${SSH_OPTS[@]}" "$SERVER" bash -s <<'REMOTE'
set -euo pipefail
cd /opt/fervid-budget/incoming
sha256sum -c SHA256SUMS
# Operator installs this root-owned helper using the service's environment.
# It runs the currently installed binary with -backup as the service account.
sudo -n /usr/local/sbin/fervid-budget-backup
cd /opt/fervid-budget
sudo -n systemctl stop fervid-budget.service
rollback() {
 code=$?
 if [[ $code != 0 ]]; then
  [[ ! -f server.prev ]] || cp -p server.prev server
  if [[ -d web/static.prev ]]; then rm -rf web/static; mv web/static.prev web/static; fi
  sudo -n systemctl start fervid-budget.service || true
 fi
 exit "$code"
}
trap rollback EXIT
cp -p server server.prev
rm -rf web/static.prev
mv web/static web/static.prev
mv incoming/static web/static
install -m 0755 incoming/server server.next
mv server.next server
mv incoming/revision.txt incoming/build-info.txt incoming/SHA256SUMS .
sudo -n systemctl start fervid-budget.service
trap - EXIT
REMOTE
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 25 "$PUBLIC_URL/login" || true)
if [[ "$code" != 200 ]]; then
 echo "Health check failed ($code); restoring previous binary and assets"
 ssh "${SSH_OPTS[@]}" "$SERVER" bash -s <<'REMOTE'
set -euo pipefail
cd /opt/fervid-budget
sudo -n systemctl stop fervid-budget.service
mv server.prev server
rm -rf web/static
mv web/static.prev web/static
sudo -n systemctl start fervid-budget.service
REMOTE
 exit 1
fi
echo "Deployed $revision; previous binary/assets retained for rollback."
