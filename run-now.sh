#!/bin/bash
set -euo pipefail
RUNTIME="$HOME/.local/share/avatarthu"
cd "$RUNTIME"
exec "$RUNTIME/.venv/bin/python" -u -m loop.daily "$@"
