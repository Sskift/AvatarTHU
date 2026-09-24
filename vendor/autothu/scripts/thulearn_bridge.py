"""
将 ~/.config/autothu/session.json 注入 thulearn2018.browser.Learn，绕过失效的 login()。
"""
from __future__ import annotations

import os
from pathlib import Path

import requests
from requests.packages.urllib3.exceptions import InsecureRequestWarning

from thulearn2018 import browser, settings
from thulearn2018.browser import Learn

from thu_learn_client import ThuLearnClient, DEFAULT_HEADERS

requests.packages.urllib3.disable_warnings(InsecureRequestWarning)

DEFAULT_SESSION = Path(
    os.environ.get("AUTOTHU_SESSION", Path.home() / ".config/autothu/session.json")
)
DEFAULT_WORK = Path(
    os.environ.get("AUTOTHU_WORK", Path.home() / "autoTHU" / "test")
)


def _dedupe_tsinghua_cookies(session: requests.Session) -> None:
    """只保留 learn / id 域各一份，避免 requests CookieConflictError。"""
    jar = requests.cookies.RequestsCookieJar()
    seen: set[tuple[str, str]] = set()
    for c in session.cookies:
        if "tsinghua.edu.cn" not in (c.domain or ""):
            continue
        key = (c.name, c.domain or "")
        if key in seen:
            continue
        seen.add(key)
        jar.set(c.name, c.value, domain=c.domain, path=c.path or "/")
    session.cookies = jar


def load_learn_from_session(
    session_path: str | Path | None = None,
    work_path: str | Path | None = None,
) -> Learn:
    path = Path(session_path or DEFAULT_SESSION)
    if not path.exists():
        raise FileNotFoundError(
            f"未找到 session: {path}\n"
            "请先运行: powershell.exe ... edge_login_winhost.py\n"
            "或: thu-learn login"
        )

    client = ThuLearnClient.from_session_file(path)
    _dedupe_tsinghua_cookies(client.session)
    client.session.headers.update(settings.headers)
    token = client.session.cookies.get("XSRF-TOKEN")
    if token:
        client.session.headers["X-XSRF-TOKEN"] = token

    learn = Learn()
    learn.session = client.session
    learn.login = lambda: None  # noqa: E731 — 旧 SSO 已禁用

    if work_path:
        learn.path = str(Path(work_path).expanduser().resolve())
    elif learn.path:
        learn.path = str(Path(learn.path).expanduser().resolve())
    else:
        learn.path = str(DEFAULT_WORK.resolve())
        os.makedirs(learn.path, exist_ok=True)

    # 从 session 恢复用户名（可选）
    import json

    data = json.loads(path.read_text(encoding="utf-8"))
    if data.get("username"):
        learn.username = data["username"]

    return learn


def ensure_semester(learn: Learn, semester: str = "") -> str:
    learn.set_semester(semester)
    return learn.semester
