#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PORT:-8080}"
ADDR=":${PORT}"
BIN="${ROOT_DIR}/server"

cd "${ROOT_DIR}"

echo "Checking for services on port ${PORT}..."
pids="$(lsof -tiTCP:"${PORT}" -sTCP:LISTEN || true)"

if [[ -n "${pids}" ]]; then
  echo "Stopping process(es): ${pids}"
  kill ${pids} || true

  for _ in {1..20}; do
    remaining="$(lsof -tiTCP:"${PORT}" -sTCP:LISTEN || true)"
    if [[ -z "${remaining}" ]]; then
      break
    fi
    sleep 0.25
  done

  remaining="$(lsof -tiTCP:"${PORT}" -sTCP:LISTEN || true)"
  if [[ -n "${remaining}" ]]; then
    echo "Port ${PORT} is still busy; force stopping process(es): ${remaining}"
    kill -9 ${remaining} || true
  fi
else
  echo "No service is listening on port ${PORT}."
fi

echo "Building latest code..."
go build -o "${BIN}" ./cmd/server

echo "Starting Fervid Budget on http://127.0.0.1:${PORT}"
echo "Press Ctrl+C to stop."
FERVID_ADDR="${ADDR}" FERVID_SECURE_COOKIES=false "${BIN}"
