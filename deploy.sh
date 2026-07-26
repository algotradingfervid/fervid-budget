#!/usr/bin/env bash
#
# deploy.sh — build and deploy Fervid Budget to the Hetzner server.
#
# What it does:
#   1. Cross-compiles a fresh linux/amd64 static binary from the current code
#   2. Uploads the binary and web/static assets to the server
#   3. Atomically swaps the binary and restarts the systemd service
#   4. Runs a health check against the public HTTPS URL
#
# Your data (SQLite DB, attachments, backups under /opt/fervid-budget/data)
# and secrets (/etc/fervid-budget.env) are NEVER touched by this script.
#
# Requirements: Go toolchain locally, and SSH key access to the server
# (this machine's key was added at server creation).
#
# Usage:
#   ./deploy.sh
#   FERVID_DEPLOY_HOST=root@1.2.3.4 ./deploy.sh   # override target host
#
set -euo pipefail

SERVER="${FERVID_DEPLOY_HOST:-root@116.203.184.169}"
APP_DIR="/opt/fervid-budget"
PUBLIC_URL="https://fervidtools.optimussoftwares.com"
SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=20)

cd "$(dirname "$0")"

echo "==> Building linux/amd64 static binary..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o ./server-linux ./cmd/server

echo "==> Uploading binary..."
scp "${SSH_OPTS[@]}" ./server-linux "$SERVER:$APP_DIR/server.new"

echo "==> Uploading web/static assets..."
scp "${SSH_OPTS[@]}" -r ./web/static "$SERVER:$APP_DIR/web/"

echo "==> Swapping binary and restarting service..."
ssh "${SSH_OPTS[@]}" "$SERVER" bash -s <<EOF
set -euo pipefail
install -m 0755 -o fervid -g fervid "$APP_DIR/server.new" "$APP_DIR/server"
rm -f "$APP_DIR/server.new"
chown -R fervid:fervid "$APP_DIR/web"
systemctl restart fervid-budget.service
sleep 2
echo -n "service: "; systemctl is-active fervid-budget.service
EOF

echo "==> Health check..."
sleep 1
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 25 "$PUBLIC_URL/" || echo "000")
echo "$PUBLIC_URL/ -> HTTP $code"

rm -f ./server-linux

if [[ "$code" == "200" || "$code" == "303" ]]; then
  echo "==> Deploy complete ✅"
else
  echo "==> WARNING: unexpected status $code — check 'ssh $SERVER journalctl -u fervid-budget -n 50'"
  exit 1
fi
