#!/usr/bin/env bash
# Create the project-local Python environment used by macOS.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
PYTHON_BIN="${AUTOTHU_PYTHON:-python3}"

if ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  echo "ERROR: Python 3.11+ not found. Set AUTOTHU_PYTHON to its path."
  exit 1
fi
if ! "$PYTHON_BIN" -c 'import sys; raise SystemExit(sys.version_info < (3, 11))'; then
  echo "ERROR: AutoThu requires Python 3.11+."
  exit 1
fi

if [[ ! -x "$ROOT/.venv/bin/python" ]]; then
  "$PYTHON_BIN" -m venv "$ROOT/.venv"
fi
"$ROOT/.venv/bin/python" -m pip install --upgrade pip
"$ROOT/.venv/bin/python" -m pip install \
  'thulearn2018>=2.4.9' \
  'pdfplumber>=0.11.0' \
  'pymupdf>=1.24.0' \
  'markdown>=3.5' \
  'requests>=2.31.0' \
  'beautifulsoup4>=4.12.0' \
  'gmssl>=3.2.0' \
  'cryptography>=36.0.0' \
  'selenium>=4.15.0'
mkdir -p "$HOME/.config/autothu"
echo "Setup complete. Add this to PATH:"
echo "  export PATH=\"$ROOT/bin:\$PATH\""
echo "Then run: thu-learn login"
