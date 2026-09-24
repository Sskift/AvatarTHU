#!/bin/bash
set -euo pipefail
python3 - <<'PY'
import json,subprocess
from pathlib import Path
runtime=Path.home()/'.local/share/avatarthu'
cfg=json.loads((runtime/'config.json').read_text())
auto=runtime/'autothu'
subprocess.run([str(runtime/'.venv/bin/python'),str(auto/'scripts/thu_learn_cli.py'),'--session',cfg['session'],'login'],check=True)
subprocess.run([str(runtime/'.venv/bin/python'),'-c','from loop.learn import client; print("登录已更新：",client().semester_id)'],cwd=runtime,check=True)
PY
