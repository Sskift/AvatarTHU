#!/usr/bin/env python3
"""
AutoThu 网络学堂 CLI（session 驱动，替代失效的 learn login）。

用法:
  conda activate autothu
  thu-learn --help

  thu-learn login          # 调 Windows Edge 登录并导出 session
  thu-learn verify         # 验证 session
  thu-learn courses      # 列出当前学期课程
  thu-learn ddl          # 作业截止（会下载 homework 元数据）
  thu-learn download     # 下载课件与作业
  thu-learn submit FILE  # 在含 .xszyid 的作业目录下提交
"""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import click

SCRIPT_DIR = Path(__file__).resolve().parent
ROOT = SCRIPT_DIR.parent
sys.path.insert(0, str(SCRIPT_DIR))

from thulearn_bridge import DEFAULT_SESSION, DEFAULT_WORK, ensure_semester, load_learn_from_session
from thu_learn_client import ThuLearnClient, SessionExpired


def _align(string: str, length: int = 0) -> str:
    len_en = len(string)
    len_utf8 = len(string.encode("utf-8"))
    lent = len_en + (len_utf8 - len_en) // 2
    return string + " " * (length - lent)


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
@click.option(
    "--session",
    type=click.Path(path_type=Path),
    default=DEFAULT_SESSION,
    help="session.json 路径",
    show_default=True,
)
@click.option(
    "--work-dir",
    "-o",
    type=click.Path(path_type=Path),
    default=DEFAULT_WORK,
    help="下载/作业根目录",
    show_default=True,
)
@click.pass_context
def cli(ctx: click.Context, session: Path, work_dir: Path) -> None:
    """AutoThu 清华网络学堂命令行（基于 session，不调用旧版 SSO login）。"""
    ctx.ensure_object(dict)
    ctx.obj["session"] = session.expanduser()
    ctx.obj["work_dir"] = work_dir.expanduser()
    os.makedirs(ctx.obj["work_dir"], exist_ok=True)


@cli.command(help="macOS 导入 Chrome 登录态；Windows/WSL 使用 Edge 登录")
@click.option(
    "--ps1",
    type=click.Path(exists=True, path_type=Path),
    default=None,
    help="run_edge_login.ps1 路径",
)
@click.option('--import-only', is_flag=True, help='macOS 只导入已有 Chrome 会话，不打开登录窗口')
@click.pass_context
def login(ctx: click.Context, ps1: Path | None, import_only: bool) -> None:
    if sys.platform == "darwin":
        env = os.environ.copy()
        env['AUTOTHU_SESSION'] = str(ctx.obj['session'])
        result = subprocess.run(
            [sys.executable, str(ROOT / "scripts" / "mac_login.py"), *(['--import-only'] if import_only else [])], cwd=str(ROOT), env=env
        )
        sys.exit(result.returncode)

    candidates = [
        ps1,
        Path("/mnt/c/Users/lenovo/autoTHU/run_edge_login.ps1"),
        ROOT / "scripts" / "run_edge_login.ps1",
    ]
    script = next((p for p in candidates if p and p.exists()), None)
    if script is None:
        win_py = (
            "powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "
            "\"& 'C:/Users/lenovo/AppData/Local/Programs/Python/Python313/python.exe' "
            "'//wsl.localhost/Ubuntu/home/lenovo/autoTHU/AutoThu/scripts/edge_login_winhost.py'\""
        )
        click.echo("未找到 run_edge_login.ps1，执行:")
        click.echo(win_py)
        subprocess.run(win_py, shell=True, check=False)
        return

    click.echo(f"运行: powershell.exe -File {script}")
    r = subprocess.run(
        ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(script)],
        cwd=str(ROOT),
    )
    sys.exit(r.returncode)


@cli.command(help="验证 session 是否有效")
@click.pass_context
def verify(ctx: click.Context) -> None:
    session: Path = ctx.obj["session"]
    if not session.exists():
        click.echo(f"FAIL: 不存在 {session}", err=True)
        sys.exit(1)
    client = ThuLearnClient.from_session_file(session)
    try:
        sem = client.get_current_semester()
        courses = client.list_courses(sem)
    except SessionExpired as exc:
        click.echo(f'FAIL: {exc}', err=True)
        sys.exit(2)
    except Exception as exc:
        click.echo(f'FAIL: 暂时无法验证（{type(exc).__name__}），原会话已保留。', err=True)
        sys.exit(1)
    if os.name == 'posix':
        client.persist()
    click.echo(f"OK 学期={sem} 课程数={len(courses)}")
    for c in courses:
        click.echo(f"  - {c.get('kcm')} ({c.get('jsm', '')})")


@cli.command(help='验证并持久化会话；macOS 可安装每 15 分钟的保活任务')
@click.option('--install', 'install_service', is_flag=True, help='安装 macOS 保活 LaunchAgent')
@click.option('--remove', is_flag=True, help='停用保活任务，保留会话')
@click.option('--status', is_flag=True, help='显示最近验证结果')
@click.option('--recover-chrome', is_flag=True, help='会话过期时尝试导入 Chrome；不弹登录窗口')
@click.option('--interval', type=click.IntRange(300, 3600), default=900, show_default=True)
@click.pass_context
def keepalive(ctx, install_service, remove, status, recover_chrome, interval):
    from session_keepalive import install, remove_service, show_status, once
    if sum((install_service, remove, status)) > 1:
        raise click.UsageError('--install、--remove、--status 只能选择一个')
    session = ctx.obj['session']
    if install_service:
        install(session, interval, recover_chrome)
    elif remove:
        remove_service()
    elif status:
        show_status(session)
    else:
        result = once(session, recover_chrome)
        click.echo(result['message'])
        if result['state'] != 'valid':
            raise SystemExit(2)


@cli.command("courses", help="列出当前学期课程")
@click.option("-s", "--semester", default="", help="学期 ID，如 2025-2026-2")
@click.pass_context
def courses_cmd(ctx: click.Context, semester: str) -> None:
    learn = load_learn_from_session(ctx.obj["session"], ctx.obj["work_dir"])
    ensure_semester(learn, semester)
    for row in learn.get_lessons():
        click.echo(f"{row[4]}\twlkcid={row[0][:20]}...")


@cli.command(help="列出未过期作业截止（并写入 homework 目录）")
@click.option("-e", "--exclude", default="", help="排除课程，逗号分隔无空格")
@click.option("-i", "--include", default="", help="仅包含课程")
@click.option("-s", "--semester", default="")
@click.option(
    "--download-submission/--no-download-submission",
    default=False,
    help="是否下载已提交的作业副本",
)
@click.pass_context
def ddl(
    ctx: click.Context,
    exclude: str,
    include: str,
    semester: str,
    download_submission: bool,
) -> None:
    learn = load_learn_from_session(ctx.obj["session"], ctx.obj["work_dir"])
    ensure_semester(learn, semester)
    ex = [x for x in exclude.split(",") if x]
    inc = [x for x in include.split(",") if x]
    ddls = learn.get_ddl(
        learn.init_lessons(exclude=ex, include=inc),
        download_submission=download_submission,
    )
    click.echo(f"Total {len(ddls)} ddl(s)")
    for row in ddls:
        click.echo(
            _align(row[0][:8], 25),
            _align(row[1][:20], 30) + _align(row[3][:20], 30),
            row[4],
        )


@cli.command(help="下载课件与作业附件")
@click.option("-e", "--exclude", default="")
@click.option("-i", "--include", default="")
@click.option("-s", "--semester", default="")
@click.option("--download-submission/--no-download-submission", default=False)
@click.pass_context
def download(
    ctx: click.Context,
    exclude: str,
    include: str,
    semester: str,
    download_submission: bool,
) -> None:
    learn = load_learn_from_session(ctx.obj["session"], ctx.obj["work_dir"])
    ensure_semester(learn, semester)
    ex = [x for x in exclude.split(",") if x]
    inc = [x for x in include.split(",") if x]
    lessons = learn.init_lessons(exclude=ex, include=inc)
    for lesson in lessons:
        click.echo(f"Check {lesson[4]}")
        for group in learn.get_files_id(lesson[0]):
            learn.download_files(lesson[0], lesson[4], group)
        learn.download_homework(lesson[0], lesson[4], download_submission)
    click.echo(f"完成，目录: {learn.path}")


@cli.command(help="提交作业（须在含 .xszyid 的作业目录下执行）")
@click.argument("file", default="")
@click.option("-m", "--message", default="", help="提交说明")
@click.option("-s", "--semester", default="")
@click.pass_context
def submit(ctx: click.Context, file: str, message: str, semester: str) -> None:
    id_path = Path(".xszyid")
    if not id_path.exists():
        click.echo("错误: 当前目录无 .xszyid，请 cd 到 homework/某作业 目录", err=True)
        sys.exit(1)
    if file and not Path(file).exists():
        click.echo(f"错误: 文件不存在 {file}", err=True)
        sys.exit(1)

    learn = load_learn_from_session(ctx.obj["session"], ctx.obj["work_dir"])
    ensure_semester(learn, semester)
    xszyid = id_path.read_text(encoding="utf-8").strip()
    learn.upload(xszyid, file, message)


@cli.command(help="显示配置路径")
@click.pass_context
def config(ctx: click.Context) -> None:
    click.echo(f"session: {ctx.obj['session']}")
    click.echo(f"work-dir: {ctx.obj['work_dir']}")
    if ctx.obj["session"].exists():
        import json

        data = json.loads(ctx.obj["session"].read_text(encoding="utf-8"))
        click.echo(f"username: {data.get('username', '?')}")
    path_cfg = Path.home() / ".config/thulearn2018/path.txt"
    if path_cfg.exists():
        click.echo(f"thulearn2018 path.txt: {path_cfg.read_text().strip()}")


def main() -> None:
    cli(obj={})


if __name__ == "__main__":
    main()
