#!/usr/bin/env python3
"""
从 WSL 启动 Windows Edge（远程调试），登录清华网络学堂并导出 session.json。

用法:
  conda activate autothu
  export THU_USER=学号 THU_PASS=密码   # 或依赖 ~/.config/thulearn2018/user.txt
  python scripts/edge_login_windows.py

说明:
  - 会打开 Windows 上的 Edge 窗口（独立用户数据目录，不影响日常浏览器）
  - 若需双因素/验证码，请在 Edge 窗口内手动完成，脚本会等待跳转
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path

import requests

ROOT = Path(__file__).resolve().parents[1]
SESSION_PATH = Path(
    os.environ.get("AUTOTHU_SESSION", Path.home() / ".config/autothu/session.json")
)
EDGE_EXE = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
EDGE_PROFILE = os.path.expandvars(r"%TEMP%\autothu-edge-cdp")
CDP_URL = "http://127.0.0.1:9222"
LOGIN_URL = "https://learn.tsinghua.edu.cn/f/login"
SUCCESS_HINTS = ("wlxt", "loadCourse", "index.html", "student")


def load_credentials() -> tuple[str, str]:
    user = os.environ.get("THU_USER", "").strip()
    passwd = os.environ.get("THU_PASS", "").strip()
    if user and passwd:
        return user, passwd
    cfg = Path.home() / ".config/thulearn2018/user.txt"
    if cfg.exists():
        lines = cfg.read_text(encoding="utf-8").splitlines()
        if len(lines) >= 2:
            return lines[0].strip(), lines[1].strip()
    raise SystemExit("请设置 THU_USER/THU_PASS 或运行 learn reset 配置 user.txt")


def cdp_ready() -> bool:
    try:
        r = requests.get(f"{CDP_URL}/json/version", timeout=2)
        return r.status_code == 200
    except Exception:
        return False


def launch_edge_windows(login_url: str) -> None:
    ps = f"""
$edge = '{EDGE_EXE}'
$profile = '{EDGE_PROFILE}'
New-Item -ItemType Directory -Force -Path $profile | Out-Null
$argList = @(
  '--remote-debugging-port=9222',
  "--user-data-dir=$profile",
  '--no-first-run',
  '--no-default-browser-check',
  '{login_url}'
)
Start-Process -FilePath $edge -ArgumentList $argList
"""
    subprocess.run(
        ["powershell.exe", "-NoProfile", "-Command", ps],
        check=True,
        cwd=str(ROOT),
    )


def save_session_from_context(context, username: str) -> Path:
    cookies = {}
    for c in context.cookies():
        if c.get("domain") and "tsinghua" in c["domain"]:
            cookies[c["name"]] = c["value"]
    # 也收集 learn 主域
    for c in context.cookies():
        if "learn.tsinghua" in (c.get("domain") or ""):
            cookies[c["name"]] = c["value"]

    if "JSESSIONID" not in cookies and "XSRF-TOKEN" not in cookies:
        # playwright 可能用不同 domain 格式，再扫一遍
        for c in context.cookies():
            cookies[c["name"]] = c["value"]

    data = {"username": username, "cookies": cookies, "exported_at": time.time()}
    SESSION_PATH.parent.mkdir(parents=True, exist_ok=True)
    SESSION_PATH.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    return SESSION_PATH


def try_fill_login(page, user: str, passwd: str) -> None:
    page.goto(LOGIN_URL, wait_until="domcontentloaded", timeout=60000)
    time.sleep(2)
    # 点击「登录」进入 SSO（若仍在门户页）
    for sel in ["#loginButtonId", "input[value='登录']", "text=登录"]:
        try:
            loc = page.locator(sel).first
            if loc.count() and loc.is_visible(timeout=2000):
                loc.click(timeout=5000)
                time.sleep(2)
                break
        except Exception:
            pass

    # 统一认证页
    for uid_sel in ["#i_user", "input[name='i_user']"]:
        try:
            el = page.locator(uid_sel).first
            if el.count():
                el.fill(user, timeout=5000)
                break
        except Exception:
            pass

    for pwd_sel in ["#i_pass", "input[type='password']"]:
        try:
            el = page.locator(pwd_sel).first
            if el.count():
                el.fill(passwd, timeout=5000)
                break
        except Exception:
            pass

    for btn in ["button:has-text('登录')", "input[type='submit']", "#login_submit"]:
        try:
            page.locator(btn).first.click(timeout=5000)
            break
        except Exception:
            pass


def wait_learn_success(page, timeout_sec: int = 300) -> bool:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        url = page.url
        if "learn.tsinghua.edu.cn" in url and "login" not in url.lower():
            return True
        if any(h in url for h in SUCCESS_HINTS):
            return True
        try:
            if page.locator("text=课程").first.is_visible(timeout=1000):
                return True
        except Exception:
            pass
        time.sleep(2)
    return False


def main() -> int:
    user, passwd = load_credentials()
    print(f"学号: {user}")
    print("启动 Windows Edge (CDP 9222)...")

    if not cdp_ready():
        launch_edge_windows(LOGIN_URL)
        for i in range(30):
            if cdp_ready():
                print("CDP 已就绪")
                break
            time.sleep(1)
        else:
            print("ERROR: 9222 端口未开放。请检查 Windows 防火墙或手动运行 Edge:")
            print(f'  "{EDGE_EXE}" --remote-debugging-port=9222 --user-data-dir={EDGE_PROFILE}')
            return 1

    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.connect_over_cdp(CDP_URL)
        if not browser.contexts:
            print("ERROR: 无浏览器上下文")
            return 1
        context = browser.contexts[0]
        page = context.pages[0] if context.pages else context.new_page()

        print("尝试自动填写 SSO（若需验证码/双因素，请在 Edge 窗口手动完成）...")
        try_fill_login(page, user, passwd)

        print("等待进入网络学堂（最多 5 分钟）...")
        if not wait_learn_success(page):
            print("WARN: 未检测到学堂首页，仍将尝试导出当前 Cookie")

        # 访问 API 页以刷新 token
        try:
            page.goto("https://learn.tsinghua.edu.cn/b/wlxt/index.html", timeout=30000)
            time.sleep(2)
        except Exception:
            pass

        out = save_session_from_context(context, user)
        names = list(json.loads(out.read_text())["cookies"].keys())
        print(f"已保存: {out}")
        print(f"Cookie 键: {names}")

        if "XSRF-TOKEN" in names or "JSESSIONID" in names:
            print("OK: 含关键 Cookie，可运行 verify_learn.py --session ...")
            return 0
        print("WARN: 可能未完全登录，请检查 Edge 窗口后重试")
        return 2


if __name__ == "__main__":
    sys.exit(main())
