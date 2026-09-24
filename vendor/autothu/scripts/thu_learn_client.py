#!/usr/bin/env python3
"""
清华网络学堂 HTTP 客户端（基于已验证的 thulearn2018 API 端点）。

登录说明（2025+）：
- 旧端点 id.tsinghua POST .../login/post/... 已返回「该应用不允许调用登录接口」
- 推荐：浏览器登录后导入 session.json，或使用 learn reset + 信任浏览器
"""
from __future__ import annotations

import json
import os
import tempfile
from pathlib import Path
from typing import Any

import requests

# 与 thulearn2018/settings.py 一致（源码验证）
BASE = "https://learn.tsinghua.edu.cn/"
LOGIN_ID_LEGACY = (
    "https://id.tsinghua.edu.cn/do/off/ui/auth/login/post/"
    "bb5df85216504820be7bba2b0ae1535b/0?/login.do"
)
LOGIN_FORM = (
    "https://id.tsinghua.edu.cn/do/off/ui/auth/login/form/"
    "bb5df85216504820be7bba2b0ae1535b/0"
)
LOGIN_ROAMING = BASE + "b/j_spring_security_thauth_roaming_entry"
SEMESTER_URL = BASE + "b/kc/zhjw_v_code_xnxq/getCurrentAndNextSemester"

DEFAULT_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
    ),
    "Accept": "*/*",
    "Connection": "keep-alive",
}


class SessionExpired(RuntimeError):
    """The server no longer accepts the saved login state."""


def school_domain(domain: str) -> bool:
    domain = domain.lstrip('.').lower()
    return domain == 'tsinghua.edu.cn' or domain.endswith('.tsinghua.edu.cn')


def lessons_url(semester_id: str) -> str:
    return (
        BASE
        + "b/wlxt/kc/v_wlkc_xs_xkb_kcb_extend/student/loadCourseBySemesterId/"
        + semester_id
        + "/zh"
    )


def homeworks_url(lesson_id: str) -> list[str]:
    form = f'aoData=[{{"name":"wlkcid","value":"{lesson_id}"}}]'
    types = ["Yjwg", "Wj", "Ypg"]
    return [BASE + f"b/wlxt/kczy/zy/student/zyList{x}?{form}" for x in types]


class ThuLearnClient:
    """带 XSRF 的 requests 会话封装。"""

    def __init__(self, session: requests.Session | None = None) -> None:
        self.session = session or requests.Session()
        self.session.headers.update(DEFAULT_HEADERS)
        self.session.verify = True
        self.semester_id: str | None = None
        self.session_path: Path | None = None
        self.loaded_bytes: bytes | None = None

    @classmethod
    def from_session_file(cls, path: str | Path) -> "ThuLearnClient":
        path = Path(path).expanduser().resolve()
        source = path.read_bytes()
        data = json.loads(source)
        client = cls()
        client.session_path, client.loaded_bytes = path, source
        xsrf = None

        if data.get("all_cookies"):
            for c in data["all_cookies"]:
                dom = c.get("domain") or ""
                if not school_domain(dom):
                    continue
                client.session.cookies.set(
                    c["name"],
                    c["value"],
                    domain=dom,
                    path=c.get("path", "/"),
                )
                if c["name"] == "XSRF-TOKEN" and "learn" in dom:
                    xsrf = c["value"]
        else:
            cookies = data.get("cookies") or data
            for name, value in cookies.items():
                if isinstance(value, dict):
                    continue
                client.session.cookies.set(
                    name, value, domain="learn.tsinghua.edu.cn", path="/"
                )
                if name == "XSRF-TOKEN":
                    xsrf = value

        if xsrf:
            client.session.headers["X-XSRF-TOKEN"] = xsrf
        return client

    def _csrf_params(self) -> dict[str, str]:
        token = next((c.value for c in self.session.cookies
                      if c.name.upper() == 'XSRF-TOKEN' and c.domain.lstrip('.') == 'learn.tsinghua.edu.cn'), None)
        if not token:
            raise SessionExpired(
                "缺少 XSRF-TOKEN：请先访问 learn 首页或完成 SSO 登录后再调用 API"
            )
        self.session.headers['X-XSRF-TOKEN'] = token
        return {"_csrf": token}

    @staticmethod
    def _json(response):
        if response.status_code in (401, 403):
            raise SessionExpired('网络学堂会话失效，请运行 thu-learn login')
        response.raise_for_status()
        try:
            return response.json()
        except ValueError as exc:
            raise SessionExpired('网络学堂返回登录页面，请运行 thu-learn login') from exc

    def get_json(self, url: str, params: dict | None = None) -> Any:
        p = dict(params or {})
        p.update(self._csrf_params())
        r = self.session.get(url, params=p, timeout=60)
        return self._json(r)

    def post_json(self, url: str, data: dict | None = None) -> Any:
        r = self.session.post(
            url, data=data or {}, params=self._csrf_params(), timeout=60
        )
        return self._json(r)

    def warmup(self) -> None:
        """获取 JSESSIONID 与 XSRF-TOKEN。"""
        self.session.get(BASE, timeout=30)

    def get_current_semester(self) -> str:
        data = self.get_json(SEMESTER_URL)
        try:
            self.semester_id = data['result']['id']
            if not isinstance(self.semester_id, str) or not self.semester_id:
                raise ValueError('missing semester')
        except (KeyError, TypeError, ValueError) as exc:
            raise SessionExpired('无法验证网络学堂登录态，请运行 thu-learn login') from exc
        return self.semester_id

    def list_courses(self, semester_id: str | None = None) -> list[dict]:
        sid = semester_id or self.semester_id or self.get_current_semester()
        # thulearn2018 使用 POST（非 GET）
        data = self.post_json(lessons_url(sid), data={})
        if not isinstance(data, dict) or not isinstance(data.get('resultList'), list):
            raise SessionExpired('无法读取课程列表，请运行 thu-learn login')
        return data['resultList']

    def persist(self) -> bool:
        """Save rotated cookies only if another process has not refreshed the file."""
        if self.session_path is None or self.loaded_bytes is None:
            return False
        # macOS/Linux callers share this lock; replacement prevents partial JSON reads.
        import fcntl
        path = self.session_path
        with path.with_suffix(path.suffix + '.lock').open('a') as guard:
            fcntl.flock(guard, fcntl.LOCK_EX)
            if path.read_bytes() != self.loaded_bytes:
                return False
            data = json.loads(self.loaded_bytes)
            data.pop('cookies', None)
            data['all_cookies'] = [{'name': c.name, 'value': c.value, 'domain': c.domain,
                                    'path': c.path or '/'} for c in self.session.cookies if school_domain(c.domain)]
            payload = (json.dumps(data, ensure_ascii=False, indent=2) + '\n').encode()
            fd, tmp = tempfile.mkstemp(prefix='.session-', dir=path.parent)
            try:
                with os.fdopen(fd, 'wb') as f:
                    f.write(payload)
                    f.flush()
                    os.fsync(f.fileno())
                os.replace(tmp, path)
                self.loaded_bytes = payload
            finally:
                if os.path.exists(tmp):
                    os.unlink(tmp)
        return True

    def ping(self) -> bool:
        try:
            self.get_current_semester()
            return True
        except Exception:
            return False


def save_session_template(path: Path) -> None:
    """写入 session 文件格式说明模板。"""
    template = {
        "username": "学号",
        "cookies": {
            "JSESSIONID": "从浏览器导出",
            "XSRF-TOKEN": "从浏览器导出",
        },
        "note": "在 learn.tsinghua.edu.cn 登录后，用浏览器开发者工具复制 Cookie",
    }
    path.write_text(json.dumps(template, ensure_ascii=False, indent=2), encoding="utf-8")
