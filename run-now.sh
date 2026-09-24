#!/bin/bash
set -euo pipefail
RUNTIME="${AVATARTHU_HOME:-$HOME/.local/share/avatarthu}"
export AVATARTHU_HOME="$RUNTIME"
cd "$RUNTIME"
exec "$RUNTIME/.venv/bin/python" -u -m loop.daily "$@"
