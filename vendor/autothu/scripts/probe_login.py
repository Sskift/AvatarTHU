#!/usr/bin/env python3
"""探测清华统一认证 → 网络学堂 完整登录链。"""
import os
import re
import sys

import requests
from bs4 import BeautifulSoup

requests.packages.urllib3.disable_warnings()

BASE = "https://learn.tsinghua.edu.cn/"
LOGIN_ID = (
    "https://id.tsinghua.edu.cn/do/off/ui/auth/login/post/"
    "bb5df85216504820be7bba2b0ae1535b/0?/login.do"
)
LOGIN_ROAMING = BASE + "b/j_spring_security_thauth_roaming_entry"

user = os.environ.get("THU_USER") or open(os.path.expanduser("~/.config/thulearn2018/user.txt")).readline().strip()
passwd = os.environ.get("THU_PASS") or open(os.path.expanduser("~/.config/thulearn2018/user.txt")).readlines()[1].strip()

s = requests.Session()
s.headers.update({
    "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
    "Accept": "*/*",
})

print("Step 0: GET learn homepage")
r0 = s.get(BASE, verify=False, timeout=30)
print(f"  status={r0.status_code} cookies={list(s.cookies.keys())}")

print("Step 1: POST id login")
r1 = s.post(LOGIN_ID, data={"i_user": user, "i_pass": passwd}, verify=False, timeout=30)
print(f"  status={r1.status_code} len={len(r1.content)}")
soup = BeautifulSoup(r1.content, "html.parser")
ticket = ""
for a in soup.find_all("a"):
    h = a.get("href") or ""
    if h.startswith("http"):
        ticket = h[-59:]
print(f"  ticket_len={len(ticket)} ticket_tail={ticket[-20:] if ticket else 'NONE'}")

print("Step 2: POST roaming entry")
if ticket:
    r2 = s.post(LOGIN_ROAMING + ticket, verify=False, timeout=30)
    print(f"  status={r2.status_code} url={r2.url}")
    print(f"  cookies={s.cookies.get_dict()}")

print("Step 3: GET learn again for XSRF")
r3 = s.get(BASE + "b/wlxt/index.html", verify=False, timeout=30)
print(f"  status={r3.status_code} cookies={s.cookies.get_dict()}")

# try semester without csrf
sem_url = BASE + "b/kc/zhjw_v_code_xnxq/getCurrentAndNextSemester"
r4 = s.get(sem_url, verify=False, timeout=30)
print(f"Step 4: GET semester raw status={r4.status_code}")
text = r4.text[:500]
print(text)
