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
  AI_SDK_DEVTOOLS_VERSION     @ai-sdk/devtools version (default: 0.0.15)

Notes:
  - This viewer reads .devtools/generations.json under the repo root.
  - Only use for local development (data is stored in plain text).
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if command -v npx >/dev/null 2>&1; then
  :
else
  echo "Error: npx not found. Please install Node.js (which provides npx)." >&2
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
DEVTOOLS_VERSION="${AI_SDK_DEVTOOLS_VERSION:-0.0.15}"

export AI_SDK_DEVTOOLS_PORT="${DEVTOOLS_PORT}"

echo "Starting AI SDK DevTools viewer..."
echo "  - port:    ${DEVTOOLS_PORT}"
echo "  - version: ${DEVTOOLS_VERSION}"
echo "  - url:     http://localhost:${DEVTOOLS_PORT}"

npx --yes "@ai-sdk/devtools@${DEVTOOLS_VERSION}"

