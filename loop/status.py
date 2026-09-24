import argparse
import json
import os
import subprocess
from .common import DATA, ROOT, config, lark_enabled, read_json, tasks


def main():
    print('运行目录:', ROOT)
    print('飞书推送:', '已启用' if lark_enabled() else '未启用；产物在本地审阅，可用 avatarthu login lark 开启')
    print('每日运行:', config().get('daily_time', '08:00'), 'Asia/Shanghai；休眠后唤醒补跑')
    from pathlib import Path
    keepalive = read_json(Path(config().get('session', ROOT / 'session.json')).with_name('keepalive-status.json'))
    print('登录保活:', keepalive.get('state', '未安装'), keepalive.get('message', ''))
    for name in ('daily', 'keepalive', 'actions', 'messages'):
        if name in ('actions', 'messages') and not lark_enabled():
            print(name + ': 已停用（飞书推送关闭）')
            continue
        r = subprocess.run(['launchctl', 'print', f'gui/{os.getuid()}/com.local.avatarthu.{name}'],capture_output=True,text=True)
        print(name + ':', '已加载' if r.returncode == 0 else '未加载', read_json(DATA / (name + '-health.json')).get('state', ''))
    print('最近同步:', json.dumps(read_json(DATA / 'sync-result.json'), ensure_ascii=False))
    for st in tasks():
        print(f'{st["task_id"]}  {st["title"]}  r{st.get("revision", 1)}  {st["status"]}')
        if st.get('error'):
            print('  ' + st['error'])
        if st.get('local_review'):
            print('  本地审阅:', st['local_review'])
        if st.get('review_doc', {}).get('url'):
            print('  飞书审阅:', st['review_doc']['url'])

if __name__ == '__main__':
    main()
