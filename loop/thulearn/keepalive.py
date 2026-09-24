# Adapted from AutoThu (MIT), snapshot b5caba55ba2a53fcd1e08db1a5fab0d01231a39f.
# Copyright (c) 2026 AutoThu contributors. See third_party/AutoThu-LICENSE.
"""Low-frequency authenticated probes; never stores passwords or bypasses SSO."""
from __future__ import annotations
import argparse
import json
import os
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from .client import ThuLearnClient, SessionExpired



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
                    from .login import import_chrome_cookies, save_session
                    save_session(session, import_chrome_cookies())
                    recovered = True
                except Exception:
                    pass
            if recovered:
                result.update(state='valid', recovered=True, last_success=current.isoformat(),
                              message='已从 Chrome 恢复有效登录态。')
            else:
                result.update(state='expired', recovered=False,
                              message='网络学堂登录已过期；请在 Chrome 登录后运行 ./avatarthu login thu。')
        except Exception as exc:
            # Never log requests' raw URLs: they may contain the CSRF token.
            result.update(state='unavailable', recovered=False,
                          message=f'暂时无法验证登录（{type(exc).__name__}），保留原会话，下次重试。')
        write_state(health, result)
        return result


def main():
    from ..common import config
    parser = argparse.ArgumentParser()
    parser.add_argument('--recover-chrome', action='store_true')
    args = parser.parse_args()
    result = once(config()['session'], args.recover_chrome)
    print(result['checked_at'], result['state'], result['message'], flush=True)
    raise SystemExit(0 if result['state'] == 'valid' else 2)


if __name__ == '__main__':
    main()
