#!/usr/bin/env bash

set -euo pipefail

REPO_DIR="/Users/super/Documents/coinw/dex/nitro-erigon/erigon-nitro-deriw"
COMPOSE_DIR="/Users/super/Documents/coinw/dex/localchain-test"
COMPOSE_FILE="${COMPOSE_DIR}/docker-compose.yaml"
IMAGE_TAG="quanquanah/nitro-node:erigon-mac"
LOG_FILE="${REPO_DIR}/docker.migrated.log"
DATA_DEST="${COMPOSE_DIR}/config-v1/My Arbitrum L3 Chain/nitro"
WAIT_SECS=30

SOURCE_DATA=""
SKIP_BUILD=0
SKIP_COPY=0
SKIP_UP=0

usage() {
  cat <<'EOF'
Usage:
  scripts/rebuild_rerun_localchain.sh [options]

Options:
  --source-data PATH   Source migrated nitro data directory to copy into config-v1.
  --log-file PATH      Output log file path (default: repo/docker.migrated.log).
  --wait-secs N        Seconds to wait after compose up before log capture (default: 30).
  --skip-build         Skip docker image build.
  --skip-copy          Skip copy to config-v1 even if --source-data is set.
  --skip-up            Skip docker compose up/down and log capture.
  -h, --help           Show help.

Example:
  scripts/rebuild_rerun_localchain.sh \
    --source-data /tmp/mdbx-debug10 \
    --wait-secs 45
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-data)
      SOURCE_DATA="${2:-}"
      shift 2
      ;;
    --log-file)
      LOG_FILE="${2:-}"
      shift 2
      ;;
    --wait-secs)
      WAIT_SECS="${2:-}"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --skip-copy)
      SKIP_COPY=1
      shift
      ;;
    --skip-up)
      SKIP_UP=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown arg: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if ! [[ "${WAIT_SECS}" =~ ^[0-9]+$ ]]; then
  echo "--wait-secs must be a non-negative integer" >&2
  exit 1
fi

if [[ "${SKIP_COPY}" -eq 0 && -n "${SOURCE_DATA}" && ! -d "${SOURCE_DATA}" ]]; then
  echo "Source data dir does not exist: ${SOURCE_DATA}" >&2
  exit 1
fi

if [[ "${SKIP_BUILD}" -eq 0 ]]; then
  echo "[rebuild] docker build -> ${IMAGE_TAG}"
  docker build --no-cache "${REPO_DIR}" \
    --target nitro-node-dev \
    --tag "${IMAGE_TAG}" \
    --build-arg GO_BUILD_TAGS=erigon \
    --build-arg SKIP_FORGE_YUL=1
fi

if [[ "${SKIP_COPY}" -eq 0 && -n "${SOURCE_DATA}" ]]; then
  echo "[copy] ${SOURCE_DATA} -> ${DATA_DEST}"
  rm -rf "${DATA_DEST}"
  cp -R "${SOURCE_DATA}" "${DATA_DEST}"
fi

if [[ "${SKIP_UP}" -eq 0 ]]; then
  echo "[compose] down"
  docker compose -f "${COMPOSE_FILE}" down >/dev/null 2>&1 || true

  echo "[compose] up -d"
  docker compose -f "${COMPOSE_FILE}" up -d

  if [[ "${WAIT_SECS}" -gt 0 ]]; then
    echo "[wait] ${WAIT_SECS}s"
    sleep "${WAIT_SECS}"
  fi

  echo "[logs] -> ${LOG_FILE}"
  docker compose -f "${COMPOSE_FILE}" logs --no-color nitro > "${LOG_FILE}"
  echo "[done] captured logs at ${LOG_FILE}"
fi

