"""Interactive account setup. Feishu is an explicit, optional integration."""
import json
import shutil
import subprocess
from pathlib import Path
from . import common as c


def session_path():
    return Path(c.config().get('session', c.ROOT / 'session.json'))


def ensure_thu(*, skip_login=False):
    from .thulearn.keepalive import once
    result = once(session_path())
    if result['state'] == 'expired' and not skip_login:
        login_thu()
    else:
        print('网络学堂:', result['message'], flush=True)


def login_thu(*, import_only=False):
    from .thulearn.login import login
    from .thulearn.keepalive import once
    login(session_path(), import_only=import_only)
    result = once(session_path())
    if result['state'] != 'valid':
        raise RuntimeError(result['message'])


def find_lark():
    candidates = [c.config().get('lark_cli'), shutil.which('lark-cli'),
                  str(c.ROOT / 'tools/lark/bin/lark-cli')]
    for item in candidates:
        if item and Path(item).is_file():
            return item
    return None


def install_lark():
    npm = shutil.which('npm')
    if not npm:
        raise RuntimeError('启用飞书需要 Node.js/npm。安装后再运行 ./avatarthu login lark；纯本地运行不需要它。')
    print('正在安装官方 Lark CLI 到 AvatarTHU 运行目录……', flush=True)
    prefix = c.ROOT / 'tools/lark'
    subprocess.run([npm, 'install', '--global', '--prefix', str(prefix), '@larksuite/cli@1.0.96'], check=True)
    return str(prefix / 'bin/lark-cli')


class LarkError(RuntimeError):
    def __init__(self, value):
        self.error = value.get('error') or {}
        super().__init__('Lark CLI 检查失败；请检查网络、应用权限或重新登录。')


def cli_json(executable, *args):
    result = subprocess.run([executable, *args], capture_output=True, text=True, timeout=180)
    try:
        value = json.loads(result.stdout or result.stderr)
    except ValueError:
        raise RuntimeError('Lark CLI 未返回可识别的结果；请检查 CLI 安装和网络。') from None
    if result.returncode or value.get('ok') is False:
        # Do not echo auth responses: some flows can include credentials.
        raise LarkError(value)
    return value.get('data', value)


def login_lark(*, start=True):
    executable = find_lark() or install_lark()
    try:
        status = cli_json(executable, 'auth', 'status', '--json', '--verify')
    except LarkError as exc:
        if exc.error.get('type') == 'config' and exc.error.get('subtype') == 'not_configured':
            status = {}
        else:
            raise
    if not status.get('appId'):
        print('尚未配置飞书应用，请按 Lark CLI 的浏览器提示完成创建。', flush=True)
        subprocess.run([executable, 'config', 'init', '--new'], check=True)
        status = cli_json(executable, 'auth', 'status', '--json', '--verify')
    identity = status.get('identities', {}).get('user', {})
    if not identity.get('available'):
        print('尚无有效飞书登录态，请按下面的链接完成授权。', flush=True)
        subprocess.run([executable, 'auth', 'login', '--domain', 'docs,drive,im,event'], check=True)
    else:
        print('已检测到飞书登录态，正在验证并刷新授权……', flush=True)
    # whoami verifies/refreshes the saved identity; never assume local metadata is enough.
    try:
        who = cli_json(executable, 'whoami', '--as', 'user', '--json')
    except LarkError as exc:
        if exc.error.get('type') != 'auth' or not identity.get('available'):
            raise
        print('已保存的飞书授权已失效，请重新完成浏览器授权。', flush=True)
        subprocess.run([executable, 'auth', 'login', '--domain', 'docs,drive,im,event'], check=True)
        who = cli_json(executable, 'whoami', '--as', 'user', '--json')
    owner = (who.get('onBehalfOf') or {}).get('openId')
    if not owner:
        raise RuntimeError('飞书未返回本人身份，推送设置未更改。')
    for key in ('card.action.trigger', 'im.message.receive_v1'):
        cli_json(executable, 'event', 'consume', key, '--as', 'bot', '--dry-run')
    with c.lock('settings'):
        cfg = c.config()
        previous = cfg.get('lark_user_id')
        if previous and previous != owner and c.tasks():
            raise RuntimeError('当前飞书账号与已有作业的审阅人不同；请切回原账号，避免混用提交权限。')
        cfg.update(lark_enabled=True, lark_cli=executable, lark_user_id=owner)
        c.write_json(c.CONFIG, cfg)
    if start:
        from .services import reconcile
        reconcile(names=('actions', 'messages'))
    print('飞书已启用，后续审阅产物会通过云文档和卡片发送给本人。', flush=True)


def disable_lark():
    with c.lock('settings'):
        cfg = c.config()
        cfg['lark_enabled'] = False
        c.write_json(c.CONFIG, cfg)
    from .services import reconcile
    reconcile(names=('actions', 'messages'))
    print('已关闭飞书推送；保留账号配置，继续在本地归档和生成审阅产物。')
