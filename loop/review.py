"""Readable, version-bound Feishu dossiers built from frozen deliverables.

Rendering never executes submitted code. Document creation receipts are durable;
an uncertain create is reconciled manually instead of producing duplicate docs.
"""
import html
import json
import re
import stat
import zipfile
from pathlib import Path, PurePosixPath
from . import common as c
from .markup import markdown_xml

TEXT_SUFFIXES = {'.py', '.js', '.ts', '.tsx', '.jsx', '.c', '.cpp', '.h', '.java', '.rs', '.go', '.sh', '.bat', '.md', '.txt', '.tex', '.json', '.html', '.css'}
IMAGE_SUFFIXES = {'.png', '.jpg', '.jpeg', '.webp'}
MAX_MEMBER = 4 * 1024 * 1024
MAX_ARCHIVE = 24 * 1024 * 1024


def esc(value):
    return html.escape(str(value), quote=True)


def paragraph(value):
    return '<p>' + esc(value).replace('\n', '<br/>') + '</p>'


def prose(value, base_level=2):
    return markdown_xml(value, base_level=base_level)


def zip_previews(path):
    """Read bounded text/images only, never extract paths or executable members."""
    used = 0
    with zipfile.ZipFile(path) as archive:
        for info in sorted(archive.infolist(), key=lambda i: i.filename):
            name = PurePosixPath(info.filename)
            if (info.is_dir() or name.is_absolute() or '..' in name.parts or '\\' in info.filename
                    or any(p.startswith('.') for p in name.parts)
                    or stat.S_ISLNK(info.external_attr >> 16)):
                continue
            if name.suffix.lower() not in TEXT_SUFFIXES | IMAGE_SUFFIXES:
                continue
            if info.file_size > MAX_MEMBER or used + info.file_size > MAX_ARCHIVE:
                continue
            used += info.file_size
            yield info.filename, archive.read(info)


def attachment_xml(path, name=None):
    relative = path.relative_to(c.ROOT)
    return ('<figure view-type="Card"><source path="@./' + esc(relative)
            + '" name="' + esc(name or path.name) + '"/></figure>')


def input_attachments(st):
    """Original assignment inputs only; omit derived OCR and page previews."""
    base = Path(st['job']) / 'input' / 'attachments' if st.get('job') else None
    if not base or not base.exists():
        return []
    files = []
    for path in sorted(base.rglob('*')):
        if not path.is_file() or path.is_symlink() or any(p.startswith('.') for p in path.relative_to(base).parts):
            continue
        if path.suffix == '.txt' and path.with_suffix('').suffix.lower() in {'.pdf', '.docx', '.pptx'} and path.with_suffix('').exists():
            continue
        match = re.fullmatch(r'(.+\.(?:pdf|pptx|docx))-page-\d+\.png', path.name, re.I)
        if match and path.with_name(match[1]).exists():
            continue
        files.append(c.inside(base, str(path.relative_to(base))))
    return files


def build(st, draft):
    """Four review sections, selected evidence, and every frozen file in the doc."""
    base = c.OUT / st['task_id'] / f'r{st["revision"]}'
    paths = []
    for item in st['artifacts']:
        p = c.inside(base, str(Path(item['path']).relative_to(base)))
        if c.digest(p) != item['sha256']:
            raise ValueError('审阅产物与已冻结版本不一致。')
        paths.append(p)
    submission = c.inside(base, str(Path(st['submission']).relative_to(base))) if st.get('submission') else None
    if submission and c.digest(submission) != st['sha256']:
        raise ValueError('提交包已变化，不能生成本版审阅页。')
    report = c.inside(base, str(Path(st['report']).relative_to(base)))
    if st.get('report_sha256') and c.digest(report) != st['report_sha256']:
        raise ValueError('执行自查记录与已冻结版本不一致。')
    assets = draft.parent / 'assets'
    assets.mkdir(parents=True, exist_ok=True)
    presentation = st.get('presentation') or {}
    attachment_names = []

    def attach(path, name=None):
        attachment_names.append(name or path.name)
        return attachment_xml(path, name)

    blocks = ['<title>' + esc(f'{st["title"]} · 第 {st["revision"]} 版审阅') + '</title>',
              paragraph(f'{st["course"]} · 截止 {st["deadline"]}（北京时间）'),
              '<callout emoji="📦" background-color="light-blue">' + paragraph(
                  f'本版已生成 {len(paths)} 份交付文件，完整产物集中在第三部分。'
                  + ('待本人审阅确认。' if st.get('ready') else '仍有待补充事项，见第二部分。')) + '</callout>',
              '<h1>一、作业描述</h1>']
    assignment = presentation.get('assignment') or st.get('description') or '未提供文字描述，请查看下方原题附件。'
    blocks += [prose(assignment[:6000])]
    if len(assignment) > 6000:
        dest = assets / 'assignment-description.txt'
        dest.write_text(assignment, encoding='utf-8')
        blocks += [paragraph('文字描述较长，完整内容见附件。'), attach(dest, '作业描述全文.txt')]
    originals = input_attachments(st)
    if originals:
        blocks += ['<h2>原题与附件</h2>']
        blocks += [attach(p) for p in originals]

    blocks += ['<h1>二、完成情况与关键结果</h1>',
               paragraph('以下摘要和检查说明来自本次作业执行记录，可结合结果图与完整文件核对。')]
    summary = st.get('summary', '').strip()
    if summary:
        blocks += [prose(summary[:1800])]
        if len(summary) > 1800:
            dest = assets / 'execution-summary.txt'
            dest.write_text(summary, encoding='utf-8')
            blocks += [paragraph('摘要已节选，完整执行说明见附件。'), attach(dest, '执行说明全文.txt')]
    if st.get('feedback'):
        blocks += ['<h2>本版修改要求</h2>', paragraph(st['feedback'])]
    if st.get('blockers'):
        blocks += ['<h2>待补充事项</h2>', '<ol>' + ''.join('<li>' + esc(x) + '</li>' for x in st['blockers']) + '</ol>']

    # Resolve selected figures only from frozen files; never run submitted code.
    contents = {}
    for p in paths:
        if p.suffix.lower() in IMAGE_SUFFIXES and p.stat().st_size <= MAX_MEMBER:
            contents[(p.name, '')] = p.read_bytes()
        elif p.suffix.lower() == '.zip':
            for name, data in zip_previews(p):
                if Path(name).suffix.lower() in IMAGE_SUFFIXES:
                    contents[(p.name, name)] = data
    highlights = presentation.get('highlights', [])
    if not highlights:
        highlights = [{'title': Path(member or filename).name, 'detail': '本版文件中的结果图，请结合原题核对。',
                       'artifact': filename, 'member': member}
                      for filename, member in contents if not any(x in (member or filename).lower() for x in ('mock', 'gui_interface'))][:3]
    images = []
    for point in highlights[:3]:
        key = (Path(point['artifact']).name, point.get('member', ''))
        data = contents.get(key)
        if data is None:
            raise ValueError('关键结果引用的图片不在本版可预览产物中：' + str(key))
        from PIL import Image
        import io
        with Image.open(io.BytesIO(data)) as source:
            if source.width * source.height > 30_000_000:
                raise ValueError('关键结果图片尺寸过大。')
            source.verify()
        dest = assets / f'figure-{len(images)+1}{Path(key[1] or key[0]).suffix.lower()}'
        dest.write_bytes(data)
        label = point['detail']
        if any(x in (key[1] or key[0]).lower() for x in ('mock', 'gui_interface')):
            label += '（界面示意图，不作为真实运行截图）'
        blocks += ['<h2>' + esc(point['title']) + '</h2>', paragraph(label), image_xml(dest)]
        images.append(dest)
    page_count = 0
    if not images:
        pdf = next((p for p in paths if p.suffix.lower() == '.pdf'), None)
        if pdf:
            import pymupdf
            with pymupdf.open(pdf) as doc:
                if len(doc):
                    dest = assets / 'report-first-page.png'
                    page = doc[0]
                    scale = min(1.5, 1400 / max(page.rect.width, 1), 2200 / max(page.rect.height, 1))
                    page.get_pixmap(matrix=pymupdf.Matrix(scale, scale), alpha=False).save(dest)
                    blocks += ['<h2>报告首页预览</h2>', paragraph('完整 PDF 可在第三部分打开。'), image_xml(dest)]
                    images.append(dest)
                    page_count = 1
        else:
            # A textual answer remains readable without expanding source code.
            answer = next((p for p in paths if p.suffix.lower() in {'.md', '.txt'}), None)
            if answer:
                text = answer.read_text(errors='replace')
                blocks += ['<h2>答案节选</h2>', prose(text[:2400])]
                if len(text) > 2400:
                    blocks += [paragraph('完整答案见第三部分附件。')]
    if presentation.get('checks'):
        blocks += ['<h2>执行自查与待核对点</h2>', '<ol>' + ''.join('<li>' + esc(x) + '</li>' for x in presentation['checks']) + '</ol>']

    blocks += ['<h1>三、完整产物</h1>', paragraph('所有交付文件均嵌在本节，点击附件预览或下载；代码包保留完整目录结构。')]
    if submission:
        blocks += ['<h2>本版提交文件</h2>', attach(submission)]
    remaining = [p for p in paths if p != submission]
    documents = [p for p in remaining if p.suffix.lower() in {'.pdf', '.docx', '.pptx', '.xlsx', '.md', '.txt'}]
    for title, group in [('报告与说明', documents), ('代码、程序及其他产物', [p for p in remaining if p not in documents])]:
        if group:
            blocks += ['<h2>' + title + '</h2>']
            blocks += [attach(p) for p in group]
    blocks += ['<h2>执行自查记录</h2>', paragraph('由完成作业的同一会话撰写，包含运行命令、结果与未完成项。'), attach(report)]
    blocks += ['<h1>四、审阅与操作</h1>',
               '<ol><li><b>对照要求。</b>阅读第一部分的描述与原题，确认完成范围。</li>'
               '<li><b>检查产物。</b>查看关键结果，再打开第三部分的报告、源码或程序核对细节。</li>'
               '<li><b>提出修改。</b>在本文相关段落或图片添加批注，回到本版卡片点击“按文档批注修改”；也可直接在卡片填写意见。</li>'
               '<li><b>确认提交。</b>审阅完成后，由本人在本版卡片选择提交。</li></ol>',
               paragraph('直接编辑审阅文档不会改变待提交文件。')]
    marker = f'版本标识：{st["task_id"]} / r{st["revision"]} / {st.get("sha256") or "无提交包"}'
    blocks += [paragraph(marker)]
    draft.write_text('\n'.join(blocks), encoding='utf-8')
    return {'image_count': len(images), 'has_bundle': bool(submission), 'pages': page_count,
            'pictures': len(images) - page_count, 'attachment_names': attachment_names,
            'marker': marker, 'draft': str(draft)}


def image_xml(path):
    return '<img path="@./' + esc(path.relative_to(c.ROOT)) + '" width="820"/>'


def publish(st):
    review = st.setdefault('review_doc', {})
    if review.get('verified'):
        return review
    if not review.get('draft'):
        decision = {'audience': '作业提交者', 'reader_task': '对照题目审阅当前版本产物并决定提交或提出修改',
                    'genre_contract': None, 'adapter': None, 'presentation_mode': 'rich',
                    'visual_plan': {'reason': '按作业描述、关键结果、完整产物和审阅操作分层展示', 'blocks': []}}
        workspace = c.lark('docs', '+script', '--command', 'init-draft', '--presentation-decision', json.dumps(decision, ensure_ascii=False))
        draft = Path(workspace['draft_path'])
        if not draft.is_absolute():
            draft = Path(workspace['cwd']) / draft
        review.update(build(st, draft))
        c.save_task(st)
    draft_arg = '@' + str(Path(review['draft']).relative_to(c.ROOT))
    if not review.get('document_id'):
        if review.get('creating'):
            raise RuntimeError('审阅文档创建结果待核对；请查飞书最近文档并恢复 review_doc 回执，避免重复创建。')
        check = c.lark('docs', '+script', '--command', 'parse', '--content', draft_arg)
        if check.get('assessment', {}).get('status') != 'passed':
            raise RuntimeError('审阅文档预检未通过：' + json.dumps(check.get('assessment'), ensure_ascii=False))
        review['creating'] = True
        c.save_task(st)
        result = c.lark('docs', '+create', '--as', 'user', '--doc-format', 'xml', '--content', draft_arg, timeout=600)
        review.update(result['document'])
        review['warnings'] = result.get('warnings', [])
        c.save_task(st)
    fetched = c.lark('docs', '+fetch', '--as', 'user', '--doc', review['document_id'], '--detail', 'with-ids', timeout=300)
    c.write_json(Path(review['draft']).parent / 'published.json', fetched)
    content = fetched['document']['content']
    from collections import Counter
    from xml.etree import ElementTree
    document = ElementTree.fromstring('<root>' + content + '</root>')
    actual_files = Counter(node.get('name') for node in document.iter('source') if node.get('token'))
    missing_files = Counter(review.get('attachment_names', [])) - actual_files
    if (missing_files or review['marker'] not in content or len(re.findall(r'<img\b', content)) < review['image_count']
            or (review.get('has_bundle') and '<source' not in content) or review.get('warnings')):
        raise RuntimeError('审阅文档尚未完整发布，已保存现有文档回执，请修复后重试。')
    review['verified'] = True
    c.save_task(st)
    return review


def comments(st):
    """Called only after the owner explicitly requests revision from this card."""
    url = st['review_doc']['url']
    owner = c.config()['lark_user_id']
    entries = []
    for item in paged('drive', '+list-comments', '--as', 'user', '--url', url, '--solved-status', 'false', '--need-relation', '--page-size', '100'):
        replies = item.get('reply_list', {}).get('replies', [])
        if item.get('has_more'):
            replies = paged('drive', '+list-replies', '--as', 'user', '--url', url, '--comment-id', item['comment_id'], '--page-size', '100')
        for reply in replies:
            if reply.get('user_id') != owner:
                continue
            parts = []
            for element in reply.get('content', {}).get('elements', []):
                if element.get('type') == 'text_run':
                    parts.append(element.get('text_run', {}).get('text', ''))
                elif element.get('type') == 'text':
                    text = element.get('text', '')
                    parts.append(text.get('text', '') if isinstance(text, dict) else text)
            text = ''.join(parts).strip()
            if text:
                entries.append({'comment_id': item['comment_id'], 'reply_id': reply.get('reply_id'),
                                'quote': item.get('quote', ''), 'text': text, 'relation': item.get('relation')})
    if any(e.get('relation') for e in entries):
        from bs4 import BeautifulSoup
        fetched = c.lark('docs', '+fetch', '--as', 'user', '--doc', url, '--detail', 'with-ids')
        soup = BeautifulSoup(fetched['document']['content'], 'html.parser')
        for entry in entries:
            relation = entry.get('relation') or {}
            if relation.get('content_deleted'):
                entry['context'] = '批注引用内容已删除，请结合原文引用核对。'
                continue
            raw = relation.get('relation') or '{}'
            try:
                locations = json.loads(raw) if isinstance(raw, str) else raw
            except ValueError:
                continue
            if not isinstance(locations, dict):
                continue
            for location in locations.values():
                if not isinstance(location, dict):
                    continue
                block_id = location.get('positionInfo', {}).get('blockID')
                if not block_id:
                    continue
                block = soup.find(id=block_id)
                if block is None:
                    continue
                heading = block.find_previous(['h1', 'h2'])
                previous = block.find_previous('p') if block.name == 'img' else None
                entry['context'] = '\n'.join(x.get_text(' ', strip=True)[:3500] for x in [heading, previous, block] if x is not None)
                attachment = block if block.name == 'source' else block.find('source')
                if attachment is not None and attachment.get('name'):
                    entry['context'] += '\n附件：' + attachment['name']
                break
    return entries


def paged(*args):
    token = None
    seen = set()
    for _ in range(100):
        result = c.lark(*args, *(['--page-token', token] if token else []))
        yield from result.get('items', [])
        if not result.get('has_more'):
            return
        token = result.get('page_token')
        if not token or token in seen:
            raise RuntimeError('飞书批注分页不完整，未开始修改。')
        seen.add(token)
    raise RuntimeError('飞书批注数量超过处理上限，未开始修改。')
