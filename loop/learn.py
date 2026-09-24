"""Built-in authentication plus checked, incremental Web Learning downloads."""
from __future__ import annotations
import base64
import json
import os
import re
import tempfile
from datetime import datetime
from pathlib import Path
from urllib.parse import urlencode, urljoin, urlsplit, parse_qsl, urlunsplit
from bs4 import BeautifulSoup
from .common import DATA, ROOT, TZ, config, now, digest, fingerprint, read_json, write_json

BASE = 'https://learn.tsinghua.edu.cn'


def client():
    from .thulearn.client import ThuLearnClient
    c = ThuLearnClient.from_session_file(config()['session'])
    c.warmup()
    c.get_current_semester()
    c.persist()
    return c


def safe_name(value):
    return re.sub(r'[\\/\x00-\x1f:*?"<>|]', '_', str(value)).strip(' .')[:100] or 'untitled'


def course_dir(c, course):
    folder = DATA / 'courses' / safe_name(c.semester_id) / (safe_name(course['kcm']) + '--' + fingerprint(course['wlkcid'])[:8])
    folder.mkdir(parents=True, exist_ok=True)
    write_json(folder / 'course.json', course)
    (folder / 'README.md').write_text(f'# {course["kcm"]}\n\n学期：{c.semester_id}\n\n- notices/：公告\n- courseware/：课件\n- homework/：作业题目、运行记录和历次产物\n', encoding='utf-8')
    return folder


def rows(payload):
    obj = payload.get('object')
    if payload.get('result') != 'success' or not isinstance(obj, dict) or not isinstance(obj.get('aaData'), list):
        raise RuntimeError('网络学堂返回了无效列表；请检查登录态。')
    return obj['aaData']


def paged(c, path, cid, post=False):
    result, offset, seen = [], 0, set()
    while True:
        params = {'aoData': json.dumps([{'name': 'wlkcid', 'value': cid}, {'name': 'sEcho', 'value': 1},
                  {'name': 'iDisplayStart', 'value': offset}, {'name': 'iDisplayLength', 'value': 100}])}
        data = c.post_json(BASE + path, params) if post else c.get_json(BASE + path, params)
        batch = rows(data)
        marker = fingerprint(batch)
        if batch and marker in seen:
            raise RuntimeError('网络学堂分页未前进，暂不关闭任何本地作业。')
        seen.add(marker)
        result.extend(batch)
        if len(batch) < 100:
            return result
        offset += len(batch)


def homework_rows(c, course_id, status='Wj'):
    return paged(c, '/b/wlxt/kczy/zy/student/zyList' + status, course_id)


def eligible(hw):
    try:
        deadline = datetime.fromisoformat(hw.get('jzsjStr') or '')
        return deadline.replace(tzinfo=TZ) > now() if deadline.tzinfo is None else deadline > now()
    except ValueError:
        return False


def clean_url(url):
    parts = urlsplit(urljoin(BASE, url))
    if parts.scheme != 'https' or parts.hostname != 'learn.tsinghua.edu.cn':
        raise ValueError('附件链接不是网络学堂 HTTPS 地址。')
    return urlunsplit((parts.scheme, parts.netloc, parts.path,
                      urlencode([(k, v) for k, v in parse_qsl(parts.query) if k != '_csrf']), ''))


def page(c, url, selector):
    response = c.session.get(clean_url(url), params=c._csrf_params(), timeout=(20, 90))
    response.raise_for_status()
    soup = BeautifulSoup(response.content, 'html.parser')
    if not soup.select_one(selector):
        raise RuntimeError('页面缺少预期内容，可能登录过期或网络学堂结构已改变。')
    return soup


def download_file(c, url, target, version):
    """Never checkpoint partial downloads or replace a valid file with a login page."""
    target = Path(target)
    receipt = target.with_name('.' + target.name + '.download.json')
    previous = read_json(receipt)
    if target.is_file() and previous.get('version') == version and previous.get('sha256') == digest(target):
        return
    target.parent.mkdir(parents=True, exist_ok=True)
    with c.session.get(clean_url(url), params=c._csrf_params(), stream=True, timeout=(20, 120)) as response:
        response.raise_for_status()
        if 'text/html' in response.headers.get('Content-Type', '').lower() and target.suffix.lower() not in {'.html', '.htm'}:
            raise RuntimeError('附件接口返回 HTML，未覆盖本地文件。')
        if urlsplit(response.url).hostname != 'learn.tsinghua.edu.cn':
            raise RuntimeError('附件请求跳转到登录或外部站点。')
        fd, temp = tempfile.mkstemp(prefix='.download-', dir=target.parent)
        try:
            size = 0
            with os.fdopen(fd, 'wb') as f:
                for chunk in response.iter_content(1024 * 1024):
                    size += len(chunk)
                    f.write(chunk)
                f.flush()
                os.fsync(f.fileno())
            expected = response.headers.get('Content-Length')
            if not size or (expected and not response.headers.get('Content-Encoding') and size != int(expected)):
                raise RuntimeError('附件下载不完整，将在下次重试。')
            os.replace(temp, target)
        finally:
            Path(temp).unlink(missing_ok=True)
    write_json(receipt, {'version': version, 'sha256': digest(target)})


def courseware(c, cid, folder):
    payload = c.get_json(BASE + '/b/wlxt/kj/wlkc_kjxxb/student/kjxxbByWlkcidAndSizeForStudent', {'wlkcid': cid, 'size': 10000})
    files = payload.get('object')
    if payload.get('result') != 'success' or not isinstance(files, list):
        raise RuntimeError('网络学堂返回了无效课件列表。')
    if len(files) >= 10000:
        raise RuntimeError('课件列表达到上限，需要扩展分页。')
    folder.mkdir(parents=True, exist_ok=True)
    index = []
    for item in files:
        title = safe_name(item['bt'])
        extension = str(item.get('wjlx') or '').lstrip('.')
        if extension and not title.lower().endswith('.' + extension.lower()):
            title += '.' + safe_name(extension)
        relative = fingerprint(item['wjid'])[:12] + '/' + title
        download_file(c, '/b/wlxt/kj/wlkc_kjxxb/student/downloadFile?' + urlencode({'sfgk': 0, 'wjid': item['wjid']}),
                      folder / relative, fingerprint([item['wjid'], item.get('scsj'), item.get('wjdx')]))
        index.append({'path': relative, 'title': item['bt'], 'description': item.get('ms'), 'sha256': digest(folder / relative)})
    write_json(folder / 'index.json', sorted(index, key=lambda x: x['path']))


def homework_detail(c, cid, hw):
    url = '/f/wlxt/kczy/zy/student/viewTj?' + urlencode({'wlkcid': cid, 'sfgq': 0, 'zyid': hw['zyid'], 'xszyid': hw['xszyid']})
    soup = page(c, url, 'div.boxbox')
    box = soup.select_one('div.boxbox')
    description = box.get_text('\n', strip=True) + '\n截止日期：' + str(hw.get('jzsjStr'))
    attachments = []
    annex = box.select_one('div.list.fujian.clearfix')
    if annex:
        for link in annex.select('a[href]'):
            href = link['href']
            if urlsplit(href).path.startswith('/b/') and ('download' in href.lower() or 'xz' in href.lower()):
                label = next((a.get_text(strip=True) for a in annex.select('a') if a.get_text(strip=True) not in {'下载', 'Download', '预览', 'Preview'}), 'attachment')
                attachments.append((safe_name(label), clean_url(href)))
    for i, image in enumerate(box.select('div.calendar img[src]')):
        url = clean_url(image['src'])
        suffix = Path(urlsplit(url).path).suffix or '.png'
        attachments.append((f'figure-{i+1}{suffix}', url))
    if hw.get('zyfjid') and not attachments:
        raise RuntimeError('发现作业附件标识但未解析到下载链接。')
    return hw['bt'], description, attachments


def active_assignments(c, download=True):
    found = []
    for course in c.list_courses():
        cid = course['wlkcid']
        folder = course_dir(c, course)
        if download:
            courseware(c, cid, folder / 'courseware')
        for hw in homework_rows(c, cid):
            if hw.get('jzsjStr') and not eligible(hw):
                continue
            title, description, attachments = homework_detail(c, cid, hw)
            assignment = folder / 'homework' / (safe_name(title) + '--' + fingerprint(hw['xszyid'])[:8])
            source = assignment / 'source'
            source.mkdir(parents=True, exist_ok=True)
            (source / 'README.md').write_text(description, encoding='utf-8')
            if download:
                for name, url in attachments:
                    download_file(c, url, source / name, fingerprint([url, hw.get('scsj'), description]))
            meta = {'course_id': cid, 'course': course['kcm'], 'semester': c.semester_id,
                    'xszyid': hw['xszyid'], 'zyid': hw['zyid'], 'title': title, 'deadline': hw.get('jzsjStr') or '未提供',
                    'description': description, 'folder': str(source), 'assignment_dir': str(assignment),
                    'courseware': str(folder / 'courseware'),
                    'requirements_hash': fingerprint([description, attachments, hw.get('zyfjid'), hw.get('scsj')])}
            write_json(assignment / 'assignment.json', meta)
            found.append(meta)
    c.persist()
    return found


def announcement_rows(c, cid, kind):
    return paged(c, '/b/wlxt/kcgg/wlkc_ggb/student/pageListXsby' + kind, cid, post=True)


def announcements(c):
    result = []
    for course in c.list_courses():
        folder = course_dir(c, course) / 'notices'
        for kind in ('Wgq', 'Ygq'):
            for r in announcement_rows(c, course['wlkcid'], kind):
                body = r.get('ggnrStr')
                if r.get('ggnr'):
                    body = base64.b64decode(r['ggnr']).decode('utf-8')
                note = {'id': str(r['ggid']), 'course_id': course['wlkcid'], 'course': course['kcm'],
                        'title': r.get('bt', ''), 'date': r.get('fbsjStr', ''), 'kind': kind,
                        'body': BeautifulSoup(body or '', 'html.parser').get_text('\n', strip=True),
                        'unread': r.get('sfyd') in ('否', 0, '0', False), 'attachment_name': r.get('fjmc')}
                write_json(folder / (safe_name(note['id']) + '.json'), note)
                (folder / (safe_name(note['id']) + '.md')).write_text(f'# {note["title"]}\n\n{note["date"]}\n\n{note["body"]}', encoding='utf-8')
                result.append(note)
    return result


def mark_notice_read(c, note):
    # Viewing the detail is the website's read action, only after confirmed delivery.
    url = '/f/wlxt/kcgg/wlkc_ggb/student/beforeViewXs?' + urlencode({'wlkcid': note['course_id'], 'id': note['id']})
    page(c, url, '#editFormId')
    current = next((r for r in announcement_rows(c, note['course_id'], note['kind']) if str(r['ggid']) == note['id']), None)
    if current is None or current.get('sfyd') != '是':
        raise RuntimeError('公告已送达飞书，但网络学堂尚未确认已读；下次只重试标记。')
    c.persist()


def verify_open(st):
    c = client()
    match = next((h for h in homework_rows(c, st['course_id']) if h['xszyid'] == st['xszyid']), None)
    if not match or not eligible(match):
        raise ValueError('作业已提交、已截止或已不在待交列表，未执行上传。')
    title, description, attachments = homework_detail(c, st['course_id'], match)
    if description != st['description'] or match['jzsjStr'] != st['deadline'] or title != st['title']:
        raise ValueError('作业要求或截止时间已改变，请先同步并重新处理。')
    version = fingerprint([description, attachments, match.get('zyfjid'), match.get('scsj')])
    if st.get('requirements_hash') and st['requirements_hash'] != version:
        raise ValueError('作业附件已更新，请先同步并重新处理。')
    return c


def upload(c, st, file):
    from requests_toolbelt.multipart.encoder import MultipartEncoder
    with Path(file).open('rb') as f:
        form = MultipartEncoder(fields={'fileupload': (Path(file).name, f, 'application/octet-stream'),
                                        'xszyid': st['xszyid'], 'isDeleted': '0', 'zynr': '经本人飞书确认提交'})
        response = c.session.post(BASE + '/b/wlxt/kczy/zy/student/tjzy', params=c._csrf_params(), data=form,
                                  headers={'Content-Type': form.content_type}, timeout=(20, 120))
        response.raise_for_status()
        if response.json().get('result') != 'success':
            raise RuntimeError('网络学堂未确认提交成功，请在网页核对。')
    return {'result': 'success', 'received_at': now().isoformat()}
