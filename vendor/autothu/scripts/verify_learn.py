#!/usr/bin/env python3
"""验证清华网络学堂 API 可达性。

推荐在 conda 环境 autothu 中运行:

  conda activate autothu
  cd AutoThu

  # 方式 A: 已有 session.json（浏览器登录后导出）
  python scripts/verify_learn.py --session ~/.config/autothu/session.json

  # 方式 B: thulearn2018 配置（若旧版登录仍可用）
  learn reset   # 交互配置 ~/.config/thulearn2018/user.txt
  python scripts/verify_learn.py --use-learn-cli
"""
from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from thu_learn_client import (  # noqa: E402
    BASE,
    LOGIN_FORM,
    LOGIN_ID_LEGACY,
    LOGIN_ROAMING,
    SEMESTER_URL,
    ThuLearnClient,
)


def verify_endpoints_doc() -> None:
    print("\n已验证的 API 端点（来源: thulearn2018/settings.py + 实探测）:")
    for name, url in [
        ("learn 首页", BASE),
        ("SSO 登录表单", LOGIN_FORM),
        ("SSO 旧 POST（已禁用）", LOGIN_ID_LEGACY),
        ("漫游票据入口", LOGIN_ROAMING),
        ("当前学期", SEMESTER_URL),
    ]:
        print(f"  {name}: {url}")
    print("\n登录现状 (2025+):")
    print("  - 旧 POST login.do → 返回「该应用不允许调用登录接口」")
    print("  - 新流程: /f/login → SSO 表单 + SM2 密码 + 可选双因素/信任浏览器")
    print("  - 推荐: 浏览器登录后 --session 导入 Cookie")


def verify_session(session_path: Path) -> int:
    print(f"[session] 加载 {session_path}")
    client = ThuLearnClient.from_session_file(session_path)
    if not client.ping():
        print("FAIL: session 无效或已过期，请重新浏览器登录并导出 Cookie")
        return 2
    sem = client.semester_id
    courses = client.list_courses(sem)
    print(f"OK: 学期={sem}, 课程数={len(courses)}")
    for c in courses[:5]:
        print(f"  - {c.get('kcm')} ({c.get('jsm', '')})")
    if len(courses) > 5:
        print(f"  ... 另有 {len(courses) - 5} 门")
    return 0


def verify_learn_cli() -> int:
    try:
        from thulearn2018 import browser, settings
    except ImportError:
        print("ERROR: pip install thulearn2018（应在 conda env autothu 中）")
        return 1

    user = os.environ.get("THU_USER")
    passwd = os.environ.get("THU_PASS")
    if user and passwd:
        os.makedirs(settings.config_dir, exist_ok=True)
        with open(settings.user_file_path, "w", encoding="utf-8") as f:
            f.write(f"{user}\n{passwd}\n")

    learn = browser.Learn()
    username, _ = learn.get_user()
    if not username:
        print("未配置账号: learn reset 或 export THU_USER/THU_PASS")
        return 1

    print(f"[learn-cli] 尝试 thulearn2018 登录 (用户 {username})")
    try:
        learn.login()
        learn.set_semester()
        lessons = learn.get_lessons()
        print(f"OK: 学期={learn.semester}, 课程={len(lessons)}")
        return 0
    except KeyError as e:
        print(f"FAIL: {e} — 旧版 login() 可能已失效，请改用 --session")
        return 2
    except Exception as e:
        print(f"FAIL: {e}")
        return 2


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--session",
        type=Path,
        default=Path(os.environ.get("AUTOTHU_SESSION", "~/.config/autothu/session.json")),
        help="浏览器导出的 session.json",
    )
    parser.add_argument(
        "--use-learn-cli",
        action="store_true",
        help="使用 thulearn2018 learn 命令登录",
    )
    parser.add_argument("--doc-only", action="store_true", help="仅打印端点文档")
    args = parser.parse_args()

    verify_endpoints_doc()
    if args.doc_only:
        return 0

    session_path = args.session.expanduser()
    if session_path.exists():
        return verify_session(session_path)

    if args.use_learn_cli:
        return verify_learn_cli()

    print(f"\n未找到 session 文件: {session_path}")
    print("请先浏览器登录 learn.tsinghua.edu.cn，导出 Cookie 到该路径，或:")
    print("  python scripts/verify_learn.py --use-learn-cli")
    return 1


if __name__ == "__main__":
    sys.exit(main())
