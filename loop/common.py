"""Local paths, atomic state, locks, and the authenticated Lark CLI."""
from __future__ import annotations
import contextlib
import fcntl
import hashlib
import json
import os
import re
import subprocess
import tempfile
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

ROOT = Path(os.environ.get('AVATARTHU_HOME', Path(__file__).resolve().parents[1])).resolve()
CONFIG = ROOT / 'config.json'
DATA = ROOT / 'data'
OUT = ROOT / 'outbox'
TZ = ZoneInfo('Asia/Shanghai')

def now():
    return datetime.now(TZ)

def config():
    return read_json(CONFIG)

def lark_enabled(cfg=None):
    cfg = config() if cfg is None else cfg
    # Preserve installations from before notifications became optional.
    return bool(cfg.get('lark_enabled', bool(cfg.get('lark_user_id'))))

def read_json(path, default=None):
    if not Path(path).exists():
        return {} if default is None else default
    return json.loads(Path(path).read_text(encoding='utf-8'))

def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix='.' + path.name, dir=path.parent)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as f:
            json.dump(value, f, ensure_ascii=False, indent=2)
            f.write('\n')
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, path)
    finally:
        if Path(tmp).exists():
            Path(tmp).unlink()

@contextlib.contextmanager
def lock(name, blocking=True):
    path = DATA / 'locks' / (name + '.lock')
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('a') as f:
        fcntl.flock(f, fcntl.LOCK_EX | (0 if blocking else fcntl.LOCK_NB))
        yield f

def task_path(tid):
    if not re.fullmatch(r'[a-f0-9]{16}', tid or ''):
        raise ValueError('Invalid task id')
    return DATA / 'tasks' / f'{tid}.json'

def save_task(st):
    st['updated_at'] = now().isoformat()
    write_json(task_path(st['task_id']), st)
    if st.get('assignment_dir'):
        write_json(Path(st['assignment_dir']) / 'state.json', st)

def tasks():
    return [read_json(p) for p in sorted((DATA / 'tasks').glob('*.json'))]

def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for b in iter(lambda: f.read(1024 * 1024), b''):
            h.update(b)
    return h.hexdigest()

def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()

def safe_error(value):
    """Requests exceptions can include CSRF query parameters; never forward these."""
    return re.sub(r'(?i)((?:_csrf|access_token|refresh_token|app_secret)=)[^&\s\'"<>]+', r'\1[redacted]', str(value))

def inside(base, relative):
    base = Path(base).resolve()
    raw = Path(relative)
    if raw.is_absolute() or '..' in raw.parts or any(p.startswith('.') for p in raw.parts):
        raise ValueError('Artifact path must be a visible relative path')
    path = base / raw
    if any(p.is_symlink() for p in [path, *path.parents] if p != base and p.is_relative_to(base)):
        raise ValueError('Symlink artifact is not allowed')
    if not path.resolve().is_relative_to(base) or not path.is_file() or path.stat().st_size == 0:
        raise ValueError(f'Missing or empty artifact: {relative}')
    return path

def lark(*args, timeout=180):
    if not lark_enabled():
        raise RuntimeError('飞书推送未启用；运行 ./avatarthu login lark 可启用。')
    command = [config()['lark_cli'], *args]
    r = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, timeout=timeout)
    try:
        result = json.loads(r.stderr if r.returncode else r.stdout)
    except ValueError:
        raise RuntimeError(f'Lark returned non-JSON output (exit {r.returncode}); check logs')
    if r.returncode or result.get('ok') is not True:
        err = result.get('error') or result
        raise RuntimeError('Lark: ' + safe_error(err.get('message') or err.get('msg') or err))
    return result['data']

def send(*, text=None, content=None, file=None, idem=None):
    args = ['im', '+messages-send', '--as', 'bot', '--user-id', config()['lark_user_id']]
    if text is not None:
        args += ['--text', text]
    elif content is not None:
        args += ['--msg-type', 'interactive', '--content', json.dumps(content, ensure_ascii=False)]
    elif file is not None:
        path = Path(file).resolve()
        args += ['--file', str(path.relative_to(ROOT))]
    else:
        raise ValueError('Missing message content')
    if idem:
        args += ['--idempotency-key', fingerprint(idem)[:48]]
    result = lark(*args)
    if not result.get('message_id'):
        raise RuntimeError('Lark did not return a message_id')
    return result

def notify_once(key, text):
    path = DATA / 'notifications.json'
    with lock('notifications'):
        sent = read_json(path)
        if key in sent:
            return
        if not lark_enabled():
            sent[key] = {'text': text, 'recorded_at': now().isoformat(), 'channel': 'local'}
            write_json(path, sent)
            print(text, flush=True)
            return
        from .cards import receipt
        title, _, detail = text.partition('\n')
        sent[key] = send(content=receipt('网络学堂 · 需要处理', title, detail, success=False), idem=key)
        write_json(path, sent)
