#!/usr/bin/env python3
"""Install this one-machine workflow outside macOS's protected Documents folder."""
import argparse
import json
import os
import plistlib
import shutil
import subprocess
import sys
import time
from pathlib import Path

SOURCE = Path(__file__).resolve().parents[1]
RUNTIME = Path.home() / '.local/share/avatarthu'
LABEL = 'com.local.avatarthu.'


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    path.chmod(0o600)


def agents(start=True, names=('daily', 'actions', 'messages')):
    dest = Path.home() / 'Library/LaunchAgents'
    dest.mkdir(parents=True, exist_ok=True)
    for name in names:
        label = LABEL + name
        subprocess.run(['launchctl', 'bootout', f'gui/{os.getuid()}/{label}'], capture_output=True)
        plist = dest / (label + '.plist')
        arguments = [str(RUNTIME / '.venv/bin/python'), '-u', '-m', 'loop.' + name]
        if name == 'daily':
            arguments.append('--tick')
        content = {'Label': label, 'ProgramArguments': arguments, 'WorkingDirectory': str(RUNTIME),
                   'EnvironmentVariables': {'AVATARTHU_HOME': str(RUNTIME), 'PATH': '/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:' + str(Path.home() / '.local/bin')},
                   'StandardOutPath': str(RUNTIME / 'logs' / (name + '.log')),
                   'StandardErrorPath': str(RUNTIME / 'logs' / (name + '.log')),
                   'RunAtLoad': True, 'ThrottleInterval': 60}
        if name == 'daily':
            content['StartInterval'] = 60
        else:
            content['KeepAlive'] = True
        with plist.open('wb') as f:
            plistlib.dump(content, f)
        if start:
            # bootout can return before launchd has finished releasing the label.
            for attempt in range(10):
                result = subprocess.run(['launchctl', 'bootstrap', f'gui/{os.getuid()}', str(plist)], capture_output=True, text=True)
                if result.returncode == 0:
                    break
                if attempt == 9:
                    raise RuntimeError(f'无法加载 {label}: {result.stderr.strip()}')
                time.sleep(1)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--autothu', type=Path, default=SOURCE.parent / 'AutoThu')
    parser.add_argument('--no-start', action='store_true')
    parser.add_argument('--agents-only', action='store_true')
    args = parser.parse_args()
    if args.agents_only:
        agents()
        return
    home = args.autothu.expanduser().resolve()
    canonical = Path.home() / '.config/autothu/session.json'
    source_python = home / '.venv/bin/python'
    if not source_python.exists():
        source_python = Path(sys.executable)
    lark, claude = shutil.which('lark-cli'), shutil.which('claude')
    if not lark or not claude:
        raise SystemExit('请先配置 lark-cli 和 Claude Code CLI')
    who = json.loads(subprocess.check_output([lark, 'whoami', '--as', 'user', '--json'], text=True))
    user_id = who['onBehalfOf']['openId']
    for key in ('card.action.trigger', 'im.message.receive_v1'):
        r = json.loads(subprocess.check_output([lark, 'event', 'consume', key, '--as', 'bot', '--dry-run'], text=True))
        if r.get('ok') is not True:
            raise SystemExit(f'飞书事件 {key} 未就绪')
    RUNTIME.mkdir(parents=True, exist_ok=True)
    RUNTIME.chmod(0o700)
    for path in ('logs', 'data', 'outbox'):
        (RUNTIME / path).mkdir(parents=True, exist_ok=True)
    python = RUNTIME / '.venv/bin/python'
    if not python.exists():
        base = subprocess.check_output([str(source_python), '-c', 'import sys; print(sys._base_executable)'], text=True).strip()
        subprocess.run([base, '-m', 'venv', str(RUNTIME / '.venv')], check=True)
    req = (SOURCE / 'requirements.txt').read_bytes()
    receipt = RUNTIME / 'requirements-installed.txt'
    if not receipt.exists() or receipt.read_bytes() != req:
        subprocess.run([str(python), '-m', 'pip', 'install', '-r', str(SOURCE / 'requirements.txt')], check=True)
        receipt.write_bytes(req)
    shutil.copytree(SOURCE / 'loop', RUNTIME / 'loop', dirs_exist_ok=True, ignore=shutil.ignore_patterns('__pycache__'))
    shutil.copytree(SOURCE / 'vendor/autothu', RUNTIME / 'autothu', dirs_exist_ok=True, ignore=shutil.ignore_patterns('__pycache__'))
    canonical.parent.mkdir(parents=True, exist_ok=True)
    if not canonical.exists():
        old_session = home / 'local/session.json'
        if old_session.exists():
            shutil.copy2(old_session, canonical)
        else:
            subprocess.run([str(python), str(RUNTIME / 'autothu/scripts/thu_learn_cli.py'), '--session', str(canonical), 'login'], check=True)
    canonical.chmod(0o600)
    old = json.loads((RUNTIME / 'config.json').read_text()) if (RUNTIME / 'config.json').exists() else {}
    old.update(lark_cli=lark, claude_cli=claude, lark_user_id=user_id,
               session=str(canonical), source_repo=str(SOURCE))
    old.setdefault('daily_time', '08:00')
    old.setdefault('stage_timeout', 7200)
    dump(RUNTIME / 'config.json', old)
    if not (SOURCE / 'courses').exists():
        (RUNTIME / 'data/courses').mkdir(parents=True, exist_ok=True)
        (SOURCE / 'courses').symlink_to(RUNTIME / 'data/courses', target_is_directory=True)
    # Do not run two generations of this same personal workflow simultaneously.
    if not args.no_start:
        for name in ('daily', 'actions', 'messages'):
            label = 'com.local.thu-learn-loop.' + name
            old_plist = Path.home() / 'Library/LaunchAgents' / (label + '.plist')
            subprocess.run(['launchctl', 'bootout', f'gui/{os.getuid()}/{label}'], capture_output=True)
            if old_plist.exists():
                backup = RUNTIME / 'legacy-launchagents'
                backup.mkdir(exist_ok=True)
                shutil.move(str(old_plist), backup / old_plist.name)
        subprocess.run([str(python), str(RUNTIME / 'autothu/scripts/thu_learn_cli.py'), '--session', str(canonical),
                        'keepalive', '--install', '--recover-chrome'], check=True)
        agents()
    print('安装完成:', RUNTIME)
    print('立即运行：./run-now.sh；查看状态：./status.sh；更新登录：./login.sh')

if __name__ == '__main__':
    main()
