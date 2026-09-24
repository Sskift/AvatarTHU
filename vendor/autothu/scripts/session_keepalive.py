#!/usr/bin/env python3
"""Low-frequency authenticated probes; never stores passwords or bypasses SSO."""
from __future__ import annotations
import argparse
import json
import os
import plistlib
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from thu_learn_client import ThuLearnClient, SessionExpired

LABEL = 'com.local.autothu.keepalive'
RUNTIME = Path.home() / '.local/share/autothu'


def state_path(session):
    return Path(session).with_name('keepalive-status.json')


def write_state(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp = tempfile.mkstemp(prefix='.keepalive-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
            f.write('\n')
        os.replace(temp, path)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


def once(session, recover_chrome=False):
    import fcntl
    session = Path(session).expanduser().resolve()
    session.parent.mkdir(parents=True, exist_ok=True)
    health = state_path(session)
    with session.with_name('keepalive.lock').open('a') as guard:
        fcntl.flock(guard, fcntl.LOCK_EX)
        previous = json.loads(health.read_text()) if health.exists() else {}
        current = datetime.now(timezone.utc)
        result = dict(previous, checked_at=current.isoformat())
        try:
            client = ThuLearnClient.from_session_file(session)
            client.get_current_semester()
            client.list_courses()
            client.persist()
            result.update(state='valid', last_success=current.isoformat(), recovered=False,
                          message='登录有效，已完成保活并保存更新后的会话。')
        except (SessionExpired, FileNotFoundError):
            recovered = False
            # Do not repeatedly ask the macOS keychain or open a browser in the background.
            last = previous.get('chrome_attempt_at')
            due = not last or (current - datetime.fromisoformat(last)).total_seconds() >= 3600
            if recover_chrome and sys.platform == 'darwin' and due:
                result['chrome_attempt_at'] = current.isoformat()
                try:
                    from mac_login import import_chrome_cookies, save_session
                    save_session(session, import_chrome_cookies())
                    recovered = True
                except Exception:
                    pass
            if recovered:
                result.update(state='valid', recovered=True, last_success=current.isoformat(),
                              message='已从 Chrome 恢复有效登录态。')
            else:
                result.update(state='expired', recovered=False,
                              message='网络学堂登录已过期；请在 Chrome 登录后运行 thu-learn login --import-only。')
        except Exception as exc:
            # Never log requests' raw URLs: they may contain the CSRF token.
            result.update(state='unavailable', recovered=False,
                          message=f'暂时无法验证登录（{type(exc).__name__}），保留原会话，下次重试。')
        write_state(health, result)
        return result


def plist_path():
    return Path.home() / 'Library/LaunchAgents' / (LABEL + '.plist')


def remove_service():
    subprocess.run(['launchctl', 'bootout', f'gui/{os.getuid()}/{LABEL}'], capture_output=True)
    plist_path().unlink(missing_ok=True)
    print('保活服务已停用；会话和运行记录已保留。')


def show_status(session):
    path = state_path(Path(session).expanduser().resolve())
    print(path.read_text() if path.exists() else '尚未运行保活检查。')


def install(session, interval=900, recover_chrome=False):
    if sys.platform != 'darwin':
        raise RuntimeError('自动安装仅支持 macOS；其他系统可定时运行 thu-learn keepalive。')
    session = Path(session).expanduser().resolve()
    if not session.exists():
        raise RuntimeError('请先运行 thu-learn login。')
    if session.is_relative_to(Path.home() / 'Documents'):
        raise RuntimeError('后台服务请使用 ~/.config/autothu/session.json，避免 macOS Documents 权限限制。')
    RUNTIME.mkdir(parents=True, exist_ok=True)
    RUNTIME.chmod(0o700)
    python = RUNTIME / '.venv/bin/python'
    if not python.exists():
        subprocess.run([sys._base_executable, '-m', 'venv', str(RUNTIME / '.venv')], check=True)
    requirements = ['requests>=2.31,<3', 'cryptography>=36']
    marker = RUNTIME / 'dependencies.json'
    if not marker.exists() or json.loads(marker.read_text()) != requirements:
        subprocess.run([str(python), '-m', 'pip', 'install', *requirements], check=True)
        write_state(marker, requirements)
    source = Path(__file__).resolve().parent
    for name in ('session_keepalive.py', 'thu_learn_client.py', 'mac_login.py'):
        target = RUNTIME / name
        if (source / name) != target:
            shutil.copy2(source / name, target)
    plist = plist_path()
    plist.parent.mkdir(parents=True, exist_ok=True)
    args = [str(python), '-u', str(RUNTIME / 'session_keepalive.py'), '--session', str(session)]
    if recover_chrome:
        args.append('--recover-chrome')
    spec = {'Label': LABEL, 'ProgramArguments': args, 'WorkingDirectory': str(RUNTIME),
            'RunAtLoad': True, 'StartInterval': interval, 'ThrottleInterval': 60,
            'StandardOutPath': str(RUNTIME / 'keepalive.log'), 'StandardErrorPath': str(RUNTIME / 'keepalive.log')}
    subprocess.run(['launchctl', 'bootout', f'gui/{os.getuid()}/{LABEL}'], capture_output=True)
    with plist.open('wb') as f:
        plistlib.dump(spec, f)
    subprocess.run(['launchctl', 'bootstrap', f'gui/{os.getuid()}', str(plist)], check=True)
    print(f'已安装保活服务：每 {interval // 60} 分钟验证一次；状态：thu-learn keepalive --status')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--session', type=Path, required=True)
    parser.add_argument('--recover-chrome', action='store_true')
    args = parser.parse_args()
    result = once(args.session, args.recover_chrome)
    print(result['checked_at'], result['state'], result['message'], flush=True)
    raise SystemExit(0 if result['state'] == 'valid' else 2)
