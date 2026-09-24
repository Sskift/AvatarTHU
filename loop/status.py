import argparse
import json
import os
import subprocess
from .common import DATA, ROOT, config, read_json, tasks


def main():
    print('运行目录:', ROOT)
    print('每日运行:', config().get('daily_time', '08:00'), 'Asia/Shanghai；休眠后唤醒补跑')
    from pathlib import Path
    keepalive = read_json(Path(config()['session']).with_name('keepalive-status.json'))
    print('登录保活:', keepalive.get('state', '未安装'), keepalive.get('message', ''))
    for name in ('daily', 'actions', 'messages'):
        r = subprocess.run(['launchctl', 'print', f'gui/{os.getuid()}/com.local.avatarthu.{name}'],capture_output=True,text=True)
        print(name + ':', '已加载' if r.returncode == 0 else '未加载', read_json(DATA / (name + '-health.json')).get('state', ''))
    print('最近同步:', json.dumps(read_json(DATA / 'sync-result.json'), ensure_ascii=False))
    for st in tasks():
        print(f'{st["task_id"]}  {st["title"]}  r{st.get("revision", 1)}  {st["status"]}')
        if st.get('error'):
            print('  ' + st['error'])

if __name__ == '__main__':
    main()
