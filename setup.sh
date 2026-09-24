#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
AVATAR_RUNTIME="${AVATARTHU_HOME:-$HOME/.local/share/avatarthu}"
for candidate in "$AVATAR_RUNTIME/.venv/bin/python" python3 python3.14 python3.13 python3.12 python3.11 python3.10; do
  if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import sys; sys.exit(sys.version_info < (3, 10))' 2>/dev/null; then
    exec "$candidate" "$ROOT/scripts/install.py" "$@"
  fi
done
echo '需要 Python 3.10 或更新版本；安装后重新运行 ./setup.sh。' >&2
exit 1
