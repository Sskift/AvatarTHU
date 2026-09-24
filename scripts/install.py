#!/usr/bin/env python3
"""Self-contained macOS installation; no AutoThu checkout or mandatory Lark CLI."""
import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

SOURCE = Path(__file__).resolve().parents[1]
RUNTIME = Path(os.environ.get('AVATARTHU_HOME', Path.home() / '.local/share/avatarthu')).expanduser().resolve()


def prepare_config(previous, *, lark=None, claude=None):
    """Migrate a session once; later upgrades never overwrite refreshed cookies."""
    cfg = dict(previous)
    session = RUNTIME / 'session.json'
    old_session = Path(cfg.get('session') or Path.home() / '.config/autothu/session.json').expanduser()
    if not session.exists() and old_session != session and old_session.is_file():
        # Create the copy with private permissions from its first byte.
        fd = os.open(session, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as dest:
            dest.write(old_session.read_bytes())
        if previous.get('session') and 'autothu' in str(old_session).lower():
            cfg['migrated_autothu_session'] = str(old_session)
    if session.exists():
        session.chmod(0o600)
    cfg.update(session=str(session), source_repo=str(SOURCE), claude_cli=claude or cfg.get('claude_cli'))
    cfg['lark_enabled'] = bool(previous.get('lark_enabled', bool(previous.get('lark_user_id')))) if lark is None else lark
    cfg.setdefault('daily_time', '08:00')
    cfg.setdefault('stage_timeout', 7200)
    return cfg


def main(argv=None):
    parser = argparse.ArgumentParser(description='安装或更新 AvatarTHU；首次安装默认纯本地运行')
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--lark', dest='lark', action='store_const', const=True, default=None, help='启用飞书，自动检测并引导登录')
    mode.add_argument('--no-lark', dest='lark', action='store_const', const=False, help='仅本地运行')
    parser.add_argument('--no-start', action='store_true', help='暂不启动或修改后台服务')
    parser.add_argument('--skip-login', action='store_true', help='安装时跳过交互登录，稍后用 avatarthu login 完成')
    parser.add_argument('--agents-only', action='store_true', help='按当前设置重新加载已安装的服务')
    args = parser.parse_args(argv)
    if sys.version_info < (3, 10):
        raise SystemExit('需要 Python 3.10 或更新版本。')
    python = RUNTIME / '.venv/bin/python'
    if args.agents_only:
        if not args.no_start:
            subprocess.run([str(python), '-m', 'loop.services'], cwd=RUNTIME, check=True)
        return
    previous = json.loads((RUNTIME / 'config.json').read_text()) if (RUNTIME / 'config.json').exists() else {}
    claude = shutil.which('claude') or previous.get('claude_cli')
    if not claude or not Path(claude).is_file():
        raise SystemExit('请先安装并登录 Claude Code CLI。飞书推送可选。')
    RUNTIME.mkdir(parents=True, exist_ok=True)
    RUNTIME.chmod(0o700)
    for name in ('logs', 'data', 'outbox', 'bin'):
        (RUNTIME / name).mkdir(parents=True, exist_ok=True)
    if not python.exists():
        subprocess.run([sys._base_executable, '-m', 'venv', str(RUNTIME / '.venv')], check=True)
    requirements = (SOURCE / 'requirements.txt').read_bytes()
    receipt = RUNTIME / 'requirements-installed.txt'
    if not receipt.exists() or receipt.read_bytes() != requirements:
        subprocess.run([str(python), '-m', 'pip', 'install', '-r', str(SOURCE / 'requirements.txt')], check=True)
        receipt.write_bytes(requirements)
    shutil.copytree(SOURCE / 'loop', RUNTIME / 'loop', dirs_exist_ok=True, ignore=shutil.ignore_patterns('__pycache__'))
    shutil.copytree(SOURCE / 'third_party', RUNTIME / 'third_party', dirs_exist_ok=True)
    shutil.copy2(SOURCE / 'THIRD_PARTY.md', RUNTIME / 'THIRD_PARTY.md')
    shutil.copy2(SOURCE / 'avatarthu', RUNTIME / 'bin/avatarthu')
    (RUNTIME / 'bin/avatarthu').chmod(0o755)
    # --lark enables only after auth succeeds; preserve the previous working mode on failure.
    cfg = prepare_config(previous, lark=False if args.lark is False else None, claude=claude)
    sys.path.insert(0, str(RUNTIME))
    os.environ['AVATARTHU_HOME'] = str(RUNTIME)
    from loop import common as c
    with c.lock('settings'):
        c.write_json(c.CONFIG, cfg)
    courses = SOURCE / 'courses'
    (RUNTIME / 'data/courses').mkdir(parents=True, exist_ok=True)
    if not courses.exists() and not courses.is_symlink():
        courses.symlink_to(RUNTIME / 'data/courses', target_is_directory=True)
    subprocess.run([str(python), '-c', 'from loop.auth import ensure_thu; ensure_thu(skip_login=' + str(args.skip_login) + ')'], cwd=RUNTIME, check=True)
    lark_error = False
    if args.lark is True or (c.lark_enabled() and not args.skip_login):
        # Explicit --lark still authorizes login even when school login is skipped.
        result = subprocess.run([str(python), '-m', 'loop.cli', 'login', 'lark', '--no-start'], cwd=RUNTIME)
        lark_error = result.returncode != 0
    if not args.no_start:
        subprocess.run([str(python), '-m', 'loop.services'], cwd=RUNTIME, check=True)
    try:
        revision = subprocess.check_output(['git', '-C', str(SOURCE), 'rev-parse', 'HEAD'], text=True).strip()
        c.write_json(c.DATA / 'deployed-revision.json', {'revision': revision, 'deployed_at': c.now().isoformat()})
    except (OSError, subprocess.CalledProcessError):
        pass
    print('安装完成:', RUNTIME)
    print('飞书推送:', '已启用' if c.lark_enabled() else '未启用（本地审阅）')
    print('查看状态：./avatarthu status；更新登录：./avatarthu login；启用飞书：./avatarthu login lark')
    if lark_error:
        print('核心流程已安装，飞书配置未完成；排查后运行 ./avatarthu login lark。', file=sys.stderr)
        raise SystemExit(2)


if __name__ == '__main__':
    main()
