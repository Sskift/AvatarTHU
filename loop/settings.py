"""User-facing executor pairing and course polling settings."""
import re
import shutil
from datetime import datetime, timedelta
from . import common as c

MODES = {'claude-codex': ('claude', 'codex'), 'codex-claude': ('codex', 'claude')}


def duration(value):
    match = re.fullmatch(r'\s*(\d+(?:\.\d+)?)\s*(s|m|h|d)?\s*', str(value).lower())
    if not match:
        raise ValueError('轮询间隔请写成 30m、12h 或 1d。')
    seconds = float(match[1]) * {'s': 1, 'm': 60, 'h': 3600, 'd': 86400}[match[2] or 'h']
    if seconds < 60 or seconds > 365 * 86400:
        raise ValueError('轮询间隔应在 1 分钟至 365 天之间。')
    return int(seconds)


def interval(cfg=None):
    return int((c.config() if cfg is None else cfg).get('poll_interval_seconds', 12 * 3600))


def next_scan(schedule=None, cfg=None, current=None):
    schedule = c.read_json(c.DATA / 'schedule.json') if schedule is None else schedule
    current = c.now() if current is None else current
    if schedule.get('next_sync_retry_at'):
        return datetime.fromisoformat(schedule['next_sync_retry_at'])
    last = schedule.get('last_sync_at')
    return datetime.fromisoformat(last) + timedelta(seconds=interval(cfg)) if last else current


def pairing(cfg=None):
    cfg = c.config() if cfg is None else cfg
    mode = cfg.get('review_mode', 'claude-codex')
    if mode not in MODES:
        raise ValueError('复审模式只能是 claude-codex 或 codex-claude。')
    writer, reviewer = MODES[mode]
    return {'mode': mode, 'writer': writer, 'reviewer': reviewer,
            'max_review_rounds': int(cfg.get('max_review_rounds', 3)),
            'claude_cli': cfg.get('claude_cli') or shutil.which('claude'),
            'codex_cli': cfg.get('codex_cli') or shutil.which('codex'),
            'stage_timeout': cfg.get('stage_timeout', 7200)}


def show():
    cfg = c.config()
    selected = pairing(cfg)
    print(f'主写：{selected["writer"]}；独立复审：{selected["reviewer"]}')
    print('模型：分别使用 Claude 与 Codex CLI 的默认配置')
    print(f'课程轮询：每 {interval(cfg) / 3600:g} 小时；下次：{next_scan(cfg=cfg).isoformat()}')
    maximum = selected['max_review_rounds']
    print('每版自动复审轮数：' + (str(maximum) if maximum else '不限'))


def configure(args):
    changes = {}
    for flag, key in [('mode', 'review_mode'), ('max_review_rounds', 'max_review_rounds')]:
        value = getattr(args, flag, None)
        if value is not None:
            changes[key] = value
    if args.poll_interval is not None:
        changes['poll_interval_seconds'] = duration(args.poll_interval)
    if not changes:
        import sys
        if not sys.stdin.isatty():
            show()
            return
        cfg = c.config()
        print('1. Claude 主写 / Codex 复审\n2. Codex 主写 / Claude 复审')
        choice = input('选择分工 [保留当前]：').strip()
        if choice:
            if choice not in ('1', '2'):
                raise ValueError('请选择 1 或 2。')
            changes['review_mode'] = 'claude-codex' if choice == '1' else 'codex-claude'
        poll = input(f'课程轮询间隔 [{interval(cfg) / 3600:g}h]：').strip()
        if poll:
            changes['poll_interval_seconds'] = duration(poll)
        maximum = input(f'每版最多复审轮数，0 为不限 [{cfg.get("max_review_rounds", 3)}]：').strip()
        if maximum:
            changes['max_review_rounds'] = int(maximum)
    if changes.get('max_review_rounds', 0) < 0:
        raise ValueError('复审轮数不能为负数；0 表示不限。')
    with c.lock('settings'):
        cfg = c.config()
        cfg.update(changes)
        for tool in ('codex', 'claude'):
            if shutil.which(tool):
                cfg[tool + '_cli'] = shutil.which(tool)
        pairing(cfg)
        c.write_json(c.CONFIG, cfg)
    show()
    print('设置已保存。轮询设置下一次调度即生效；写作与复审分工从下一版作业开始使用。')
