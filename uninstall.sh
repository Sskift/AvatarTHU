#!/bin/bash
set -euo pipefail
python3 - <<'PY'
import os,subprocess
from pathlib import Path
for name in ('daily','actions','messages'):
    label='com.local.avatarthu.'+name
    subprocess.run(['launchctl','bootout',f'gui/{os.getuid()}/{label}'],capture_output=True)
    (Path.home()/'Library/LaunchAgents'/f'{label}.plist').unlink(missing_ok=True)
print('已停用后台任务。运行目录和产物保留在 ~/.local/share/avatarthu。')
PY
