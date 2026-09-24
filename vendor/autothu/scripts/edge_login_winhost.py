#!/usr/bin/env python3
"""
在 Windows 本机运行（非 WSL），通过 CDP 连接已启动的 Edge 并导出 session.json。

由 run_edge_login.ps1 或:
  python.exe scripts/edge_login_winhost.py
"""
from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path

CDP_URL = "http://127.0.0.1:9222"
LOGIN_URL = "https://learn.tsinghua.edu.cn/f/login"

# WSL 路径: Windows 写入 \\wsl.localhost\...\session.json
def default_session_path() -> Path:
    wsl = os.environ.get("WSL_DISTRO_NAME")
    if wsl:
        base = Path.home() / ".config" / "autothu"
    else:
        # Windows 侧默认写到用户目录，同时尝试 WSL 挂载
        win_home = Path(os.environ.get("USERPROFILE", "."))
        wsl_cfg = win_home / ".autothu_session.json"
        if wsl_cfg.parent.exists():
            return wsl_cfg
        base = Path(os.environ.get("AUTOTHU_SESSION", ""))
        if base:
            return Path(base)
        # \\wsl$\Ubuntu\home\lenovo\.config\autothu\session.json
        for distro in ("Ubuntu", "ubuntu", "Ubuntu-22.04"):
            p = Path(f"//wsl.localhost/{distro}/home/lenovo/.config/autothu/session.json")
            if p.parent.exists() or os.environ.get("USERNAME") == "lenovo":
                p.parent.mkdir(parents=True, exist_ok=True)
                return p
        base = win_home / ".config" / "autothu"
    base.mkdir(parents=True, exist_ok=True)
    return base / "session.json"


SESSION_PATH = Path(os.environ.get("AUTOTHU_SESSION", str(default_session_path())))


def load_credentials() -> tuple[str, str]:
    user = os.environ.get("THU_USER", "").strip()
    passwd = os.environ.get("THU_PASS", "").strip()
    if user and passwd:
        return user, passwd
    # WSL 配置
    for distro in ("Ubuntu", "ubuntu"):
        cfg = Path(f"//wsl.localhost/{distro}/home/lenovo/.config/thulearn2018/user.txt")
        if cfg.exists():
            lines = cfg.read_text(encoding="utf-8").splitlines()
            return lines[0].strip(), lines[1].strip()
    cfg2 = Path.home() / ".config" / "thulearn2018" / "user.txt"
    if cfg2.exists():
        lines = cfg2.read_text(encoding="utf-8").splitlines()
        return lines[0].strip(), lines[1].strip()
    raise SystemExit("需要 THU_USER/THU_PASS 或 WSL 下 ~/.config/thulearn2018/user.txt")


def save_session(context, username: str) -> Path:
    all_cookies = context.cookies()
    # 仅保留清华域；扁平 cookies 优先 learn 域（避免 JSESSIONID 被 id 域覆盖）
    cookies: dict[str, str] = {}
    for c in all_cookies:
        dom = c.get("domain") or ""
        if "tsinghua.edu.cn" not in dom:
            continue
        if c["name"] not in cookies or "learn.tsinghua" in dom:
            cookies[c["name"]] = c["value"]
    thu_only = [c for c in all_cookies if "tsinghua.edu.cn" in (c.get("domain") or "")]
    data = {
        "username": username,
        "cookies": cookies,
        "all_cookies": thu_only,
        "exported_at": time.time(),
    }
    SESSION_PATH.parent.mkdir(parents=True, exist_ok=True)
    SESSION_PATH.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    return SESSION_PATH


def automate(page, user: str, passwd: str) -> None:
    page.goto(LOGIN_URL, wait_until="domcontentloaded", timeout=90000)
    time.sleep(2)
    for sel in ["#loginButtonId", "input[value='登录']"]:
        try:
            loc = page.locator(sel).first
            if loc.count() and loc.is_visible(timeout=3000):
                loc.click(timeout=8000)
                time.sleep(2)
                break
        except Exception:
            pass
    for sel in ["#i_user", "input[name='i_user']"]:
        try:
            page.locator(sel).first.fill(user, timeout=8000)
            break
        except Exception:
            pass
    for sel in ["#i_pass", "input[type='password']"]:
        try:
            page.locator(sel).first.fill(passwd, timeout=8000)
            break
        except Exception:
            pass
    for sel in ["button:has-text('登录')", "#login_submit", "input[type='submit']"]:
        try:
            page.locator(sel).first.click(timeout=5000)
            break
        except Exception:
            pass


def wait_success(page, timeout: int = 300) -> bool:
    t0 = time.time()
    while time.time() - t0 < timeout:
        u = page.url
        if "learn.tsinghua.edu.cn" in u and "/f/login" not in u and "login" not in u.split("?")[0]:
            if "id.tsinghua" not in u:
                return True
        time.sleep(2)
    return False


def main() -> int:
    import requests

    try:
        requests.get(f"{CDP_URL}/json/version", timeout=3).raise_for_status()
    except Exception as e:
        print(f"CDP 不可用 ({CDP_URL}): {e}")
        print("请先运行 run_edge_login.ps1 启动 Edge")
        return 1

    user, passwd = load_credentials()
    print(f"用户: {user}")

    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.connect_over_cdp(CDP_URL)
        ctx = browser.contexts[0] if browser.contexts else browser.new_context()
        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        print("自动填写登录（验证码/双因素请在 Edge 窗口手动完成）...")
        automate(page, user, passwd)
        print("等待进入网络学堂...")
        ok = wait_success(page)
        if ok:
            try:
                page.goto("https://learn.tsinghua.edu.cn/b/wlxt/index.html", timeout=60000)
                time.sleep(2)
            except Exception:
                pass
        out = save_session(ctx, user)
        keys = list(json.loads(out.read_text(encoding="utf-8"))["cookies"].keys())
        print(f"已保存: {out}")
        print(f"Cookies: {keys}")
        if "JSESSIONID" in keys or "XSRF-TOKEN" in keys:
            return 0
        return 2 if not ok else 1


if __name__ == "__main__":
    sys.exit(main())
