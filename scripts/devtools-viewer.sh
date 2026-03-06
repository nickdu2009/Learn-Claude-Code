#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

usage() {
  cat <<'EOF'
Usage:
  scripts/devtools-viewer.sh

Env:
  AI_SDK_DEVTOOLS_PORT        Viewer port (default: 4983)
  AI_SDK_DEVTOOLS_DIR         Trace directory (default: .devtools)

Notes:
  - This viewer reads generations.json in the configured trace directory.
  - Only use for local development (data is stored in plain text).
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if command -v go >/dev/null 2>&1; then
  :
else
  echo "Error: go not found. Please install Go." >&2
  exit 1
fi

if command -v npm >/dev/null 2>&1; then
  :
else
  echo "Error: npm not found. Please install Node.js." >&2
  exit 1
fi

# Load .env if present (export all variables it defines).
if [[ -f ".env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source ".env"
  set +a
fi

DEVTOOLS_PORT="${AI_SDK_DEVTOOLS_PORT:-4983}"
export AI_SDK_DEVTOOLS_PORT="${DEVTOOLS_PORT}"
VIEWER_DIR="${REPO_ROOT}/pkg/devtools/viewer"

if [[ ! -d "${VIEWER_DIR}/node_modules" ]]; then
  echo "Installing viewer frontend dependencies..."
  npm install --prefix "${VIEWER_DIR}"
fi

echo "Building forked DevTools frontend..."
npm run build --prefix "${VIEWER_DIR}"

echo "Starting Trace V2 viewer..."
echo "  - port:    ${DEVTOOLS_PORT}"
echo "  - url:     http://localhost:${DEVTOOLS_PORT}"

go run ./cmd/devtools-viewer

