"""The installed AvatarTHU command; no external AutoThu checkout is required."""
import argparse
import sys
from . import common as c


def main():
    parser = argparse.ArgumentParser(prog='avatarthu', description='AvatarTHU 课程助手')
    sub = parser.add_subparsers(dest='command', required=True)
    login = sub.add_parser('login', help='登录网络学堂，或启用飞书')
    login.add_argument('provider', choices=('thu', 'lark'), nargs='?', default='thu')
    login.add_argument('--import-only', action='store_true', help='只导入 Chrome 中的网络学堂会话')
    login.add_argument('--no-start', action='store_true', help='登录飞书后暂不启用回调服务')
    notifications = sub.add_parser('notifications', help='开关可选的飞书推送')
    notifications.add_argument('mode', choices=('on', 'off'))
    sub.add_parser('status', help='查看服务、登录和本地审阅路径')
    run = sub.add_parser('run', help='立即扫描并处理作业')
    run.add_argument('--sync-only', action='store_true')
    run.add_argument('--task')
    revise = sub.add_parser('revise', help='为本地审阅中的作业提供修改意见')
    revise.add_argument('task_id')
    revise.add_argument('--feedback', required=True)
    sub.add_parser('uninstall', help='停用所有服务，保留课程和产物')
    args = parser.parse_args()
    try:
        if args.command == 'login':
            from . import auth
            if args.provider == 'thu':
                auth.login_thu(import_only=args.import_only)
            else:
                if args.import_only:
                    parser.error('--import-only 只适用于网络学堂')
                auth.login_lark(start=not args.no_start)
        elif args.command == 'notifications':
            from . import auth
            auth.login_lark() if args.mode == 'on' else auth.disable_lark()
        elif args.command == 'status':
            from .status import main as status
            status()
        elif args.command == 'revise':
            from .local_review import revise
            revise(args.task_id, args.feedback)
        elif args.command == 'run':
            from .daily import run
            run(sync_only=args.sync_only, selected=args.task)
        elif args.command == 'uninstall':
            from .services import uninstall
            uninstall()
    except (RuntimeError, OSError, ValueError) as exc:
        print(c.safe_error(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
