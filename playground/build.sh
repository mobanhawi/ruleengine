#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/playground/dist"
PUBLIC_DIR="${ROOT_DIR}/playground/public"

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

cp -R "${PUBLIC_DIR}/." "${DIST_DIR}/"

GOROOT_PATH="$(go env GOROOT)"
if [[ -f "${GOROOT_PATH}/lib/wasm/wasm_exec.js" ]]; then
  cp "${GOROOT_PATH}/lib/wasm/wasm_exec.js" "${DIST_DIR}/wasm_exec.js"
elif [[ -f "${GOROOT_PATH}/misc/wasm/wasm_exec.js" ]]; then
  cp "${GOROOT_PATH}/misc/wasm/wasm_exec.js" "${DIST_DIR}/wasm_exec.js"
else
  echo "error: wasm_exec.js not found in GOROOT (${GOROOT_PATH})" >&2
  exit 1
fi

GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" \
  -o "${DIST_DIR}/ruleengine.wasm" \
  "${ROOT_DIR}/cmd/ruleengine-wasm"

echo "Built Cloudflare Pages bundle in ${DIST_DIR}"
ls -lh "${DIST_DIR}"
