"""AvatarTHU owns its scheduler, session keepalive and optional Feishu listeners."""
import os
import plistlib
import shutil
import subprocess
import sys
import time
from pathlib import Path
from . import common as c

LABEL = 'com.local.avatarthu.'
NAMES = ('daily', 'keepalive', 'actions', 'messages')


def enabled_names(cfg=None):
    return NAMES if c.lark_enabled(cfg) else NAMES[:2]


def spec(name):
    module = 'thulearn.keepalive' if name == 'keepalive' else name
    args = [str(c.ROOT / '.venv/bin/python'), '-u', '-m', 'loop.' + module]
    if name == 'daily':
        args.append('--tick')
    if name == 'keepalive':
        args.append('--recover-chrome')
    value = {'Label': LABEL + name, 'ProgramArguments': args, 'WorkingDirectory': str(c.ROOT),
             'EnvironmentVariables': {'AVATARTHU_HOME': str(c.ROOT), 'PATH': os.environ.get('PATH', os.defpath)},
             'StandardOutPath': str(c.ROOT / 'logs' / (name + '.log')),
             'StandardErrorPath': str(c.ROOT / 'logs' / (name + '.log')),
             'RunAtLoad': True, 'ThrottleInterval': 60}
    if name in ('daily', 'keepalive'):
        value['StartInterval'] = 60 if name == 'daily' else 900
    else:
        value['KeepAlive'] = True
    return value


def bootout(label):
    subprocess.run(['launchctl', 'bootout', f'gui/{os.getuid()}/{label}'], capture_output=True)


def reconcile(*, names=NAMES):
    if sys.platform != 'darwin':
        raise RuntimeError('后台服务安装目前支持 macOS。')
    dest = Path.home() / 'Library/LaunchAgents'
    dest.mkdir(parents=True, exist_ok=True)
    active = enabled_names()
    for name in names:
        bootout(LABEL + name)
        plist = dest / (LABEL + name + '.plist')
        if name not in active:
            plist.unlink(missing_ok=True)
            continue
        plist.write_bytes(plistlib.dumps(spec(name)))
        for attempt in range(10):
            result = subprocess.run(['launchctl', 'bootstrap', f'gui/{os.getuid()}', str(plist)], capture_output=True, text=True)
            if result.returncode == 0:
                break
            if attempt == 9:
                raise RuntimeError(f'无法加载 {LABEL + name}: {result.stderr.strip()}')
            time.sleep(1)


def retire_legacy():
    """Retire prior services installed by this project, preserving their files."""
    labels = ['com.local.thu-learn-loop.' + n for n in ('daily', 'actions', 'messages')]
    # Only take over the old AutoThu service when our config proves we used it.
    if c.config().get('migrated_autothu_session'):
        labels.append('com.local.autothu.keepalive')
    for label in labels:
        plist = Path.home() / 'Library/LaunchAgents' / (label + '.plist')
        if not plist.exists():
            continue
        bootout(label)
        backup = c.ROOT / 'legacy-launchagents'
        backup.mkdir(exist_ok=True)
        target = backup / plist.name
        if not target.exists():
            shutil.copy2(plist, target)
        plist.unlink()


def uninstall():
    for name in NAMES:
        bootout(LABEL + name)
        (Path.home() / 'Library/LaunchAgents' / (LABEL + name + '.plist')).unlink(missing_ok=True)
    print('已停用 AvatarTHU 后台任务和保活；运行目录与产物保留。')


if __name__ == '__main__':
    retire_legacy()
    reconcile()
