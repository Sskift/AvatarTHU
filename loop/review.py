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

TEXT_SUFFIXES = {'.py', '.js', '.ts', '.tsx', '.jsx', '.c', '.cpp', '.h', '.java', '.rs', '.go', '.sh', '.bat', '.md', '.txt', '.tex', '.json', '.html', '.css'}
IMAGE_SUFFIXES = {'.png', '.jpg', '.jpeg', '.webp'}
MAX_MEMBER = 4 * 1024 * 1024
MAX_ARCHIVE = 24 * 1024 * 1024


def esc(value):
    return html.escape(str(value), quote=True)


def paragraph(value):
    return '<p>' + esc(value).replace('\n', '<br/>') + '</p>'


def rich_text(value):
    text = esc(value)
    return re.sub(r'\*\*([^*]+)\*\*', r'<b>\1</b>', text)


def prose(value):
    """Preserve supplied text without interpreting arbitrary XML or remote media."""
    blocks = []
    code = None
    table = []
    def flush_table():
        if table:
            rows = []
            for i, row in enumerate(table):
                tag = 'th' if i == 0 else 'td'
                rows.append('<tr>' + ''.join(f'<{tag}><p>{rich_text(cell)}</p></{tag}>' for cell in row) + '</tr>')
            blocks.append('<table><thead>' + rows[0] + '</thead><tbody>' + ''.join(rows[1:]) + '</tbody></table>')
            table.clear()
    for line in value.splitlines():
        if code is None and line.strip().startswith('|') and line.strip().endswith('|'):
            cells = [cell.strip() for cell in line.strip().strip('|').split('|')]
            if not all(re.fullmatch(r':?-+:?', cell) for cell in cells):
                table.append(cells)
            continue
        flush_table()
        if line.startswith('```'):
            if code is None:
                code = []
            else:
                blocks.append('<pre><code>' + esc('\n'.join(code)) + '</code></pre>')
                code = None
        elif code is not None:
            code.append(line)
        elif line.strip():
            match = re.match(r'^#{1,6}\s+(.+)', line)
            blocks.append('<h2>' + esc(match[1]) + '</h2>' if match else '<p>' + rich_text(line) + '</p>')
    if code:
        blocks.append(paragraph('\n'.join(code)))
    flush_table()
    return ''.join(blocks)


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


def build(st, draft):
    """Build review XML and assets; all output content comes from this revision."""
    base = c.OUT / st['task_id'] / f'r{st["revision"]}'
    paths = []
    for item in st['artifacts']:
        p = c.inside(base, str(Path(item['path']).relative_to(base)))
        if c.digest(p) != item['sha256']:
            raise ValueError('审阅产物与已冻结版本不一致。')
        paths.append(p)
    if st.get('submission') and c.digest(st['submission']) != st['sha256']:
        raise ValueError('提交包已变化，不能生成本版审阅页。')
    assets = draft.parent / 'assets'
    assets.mkdir(parents=True, exist_ok=True)
    blocks = ['<title>' + esc(f'{st["course"]} · {st["title"]} · 第 {st["revision"]} 版审阅') + '</title>',
              '<callout emoji="📖" background-color="light-blue"><p>先看报告原页和产物，再决定提交。可圈选文字、图片或源码添加批注，写完后回到飞书卡片点“按文档批注修改”；也可直接在卡片填写意见。</p></callout>',
              paragraph(f'截止：{st["deadline"]}（北京时间） · 第 {st["revision"]} 版 · 尚待本人确认提交')]
    if st.get('blockers'):
        blocks += [paragraph('待补充：' + '；'.join(st['blockers']))]
    if st.get('feedback'):
        blocks += ['<h1>本版修改要求</h1>', paragraph(st['feedback'])]
    blocks += ['<h1>审阅顺序与下载</h1>',
               paragraph('1. 对照题目要求；2. 阅读报告原页和图；3. 查看源码与运行说明；4. 核对执行自查及未完成项。')]
    bundle_url = st.get('deliveries', {}).get('bundle', {}).get('message_app_link')
    if bundle_url:
        blocks += ['<p><a href="' + esc(bundle_url) + '">下载本版完整提交包</a></p>']
    if st.get('submission'):
        relative = Path(st['submission']).relative_to(c.ROOT)
        blocks += ['<source path="@./' + esc(relative) + '" name="' + esc(Path(st['submission']).name) + '"/>']
    blocks += ['<table><thead><tr><th><p>本版文件</p></th><th><p>大小</p></th></tr></thead><tbody>']
    for p in paths:
        blocks += ['<tr><td>' + paragraph(p.name) + '</td><td>' + paragraph(f'{p.stat().st_size / 1024:.1f} KB') + '</td></tr>']
    blocks += ['</tbody></table>', '<h1>题目要求</h1>', paragraph(st.get('description', ''))]
    # Use the worker's input snapshot, not subsequently refreshed course material.
    inputs = Path(st['job']) / 'input' / 'attachments' if st.get('job') else None
    if inputs and inputs.exists():
        for p in sorted(inputs.rglob('*.pdf.txt'))[:8]:
            text = p.read_text(errors='replace')
            blocks += ['<h2>' + esc(p.name.removesuffix('.txt')) + '</h2>', paragraph(text[:18000])]
            if len(text) > 18000:
                blocks += [paragraph('题面文本较长，仅展开前 18000 字符；原附件保存在课程文件夹。')]
    images = []
    page_count = 0
    blocks += ['<h1>报告原页</h1>']
    for p in paths:
        if p.suffix.lower() != '.pdf':
            continue
        import pymupdf
        with pymupdf.open(p) as doc:
            blocks += ['<h2>' + esc(p.name) + '</h2>', paragraph(f'共 {len(doc)} 页。以下是提交文件的直接渲染，可点击放大。')]
            for i, page in enumerate(doc):
                if page_count >= 30:
                    blocks += [paragraph('原页预览已达到 30 页上限，后续内容请打开原 PDF。')]
                    break
                dest = assets / f'page-{page_count + 1}.png'
                scale = min(1.65, 1400 / max(page.rect.width, 1), 2200 / max(page.rect.height, 1))
                page.get_pixmap(matrix=pymupdf.Matrix(scale, scale), alpha=False).save(dest)
                images.append(dest)
                blocks += [paragraph(f'{p.name} · 第 {i+1} / {len(doc)} 页'), image_xml(dest)]
                page_count += 1
    if not page_count:
        blocks += [paragraph('本版没有 PDF，正文、源码及附图见下文。')]
    contents = [(p.name, p.read_bytes()) for p in paths if p.suffix.lower() in TEXT_SUFFIXES | IMAGE_SUFFIXES and p.stat().st_size <= MAX_MEMBER]
    seen = {c.fingerprint([n, data.hex()]) for n, data in contents}
    for p in paths:
        if p.suffix.lower() == '.zip':
            for name, data in zip_previews(p):
                key = c.fingerprint([Path(name).name, data.hex()])
                if key not in seen:
                    contents.append((p.name + ' / ' + name, data))
                    seen.add(key)
    blocks += ['<h1>产物附图</h1>', paragraph('图片从本版文件或代码包直接读取；图的生成方法和验证范围以执行自查为准。')]
    picture_count = 0
    for name, data in contents:
        if Path(name).suffix.lower() not in IMAGE_SUFFIXES or picture_count >= 16:
            continue
        from PIL import Image
        import io
        with Image.open(io.BytesIO(data)) as source:
            if source.width * source.height > 30_000_000:
                continue
            source.thumbnail((1400, 1800))
            dest = assets / f'figure-{picture_count+1}.png'
            source.convert('RGB').save(dest)
        caption = name
        if 'gui_interface' in name or 'mock' in name.lower():
            caption += '（界面示意图，不作为真实运行截图）'
        blocks += [paragraph(caption), image_xml(dest)]
        images.append(dest)
        picture_count += 1
    if not picture_count:
        blocks += [paragraph('没有独立图片附件；报告中的图见上方原页。')]
    blocks += ['<h1>正文、运行说明与源码</h1>', paragraph('以下内容来自本版文件，可圈选批注。单个文件最多展开 18000 字符，总计 90000 字符；ZIP 只预览文本与图片，完整文件见下载包。')]
    remaining = 90000
    for name, data in sorted(contents, key=lambda item: (Path(item[0]).suffix.lower() not in {'.md', '.txt'}, item[0])):
        suffix = Path(name).suffix.lower()
        if suffix not in TEXT_SUFFIXES or remaining <= 0:
            continue
        try:
            full = data.decode('utf-8')
        except UnicodeDecodeError:
            continue
        text = full[:min(remaining, 18000)]
        remaining -= len(text)
        blocks += ['<h2>' + esc(name) + '</h2>']
        if suffix in {'.md', '.txt'}:
            blocks += [prose(text)]
        else:
            lang = {'.py': 'python', '.js': 'javascript', '.ts': 'typescript', '.sh': 'bash', '.html': 'html'}.get(suffix, 'plain text')
            lines = text.splitlines(keepends=True)
            for offset in range(0, len(lines), 80):
                blocks += [f'<pre lang="{lang}"><code>' + esc(''.join(lines[offset:offset+80])) + '</code></pre>']
        if len(full) > len(text):
            blocks += [paragraph('此文件预览已截断，完整内容见下载包。')]
    blocks += ['<h1>执行自查记录</h1>', paragraph('以下由完成作业的同一执行会话撰写。命令、结果与结论按原记录呈现，不代表额外的独立评审。')]
    report = Path(st['report']).read_text(errors='replace')
    blocks += [prose(report[:24000])]
    if len(report) > 24000:
        blocks += [paragraph('自查预览已截断，完整记录保存在课程文件夹。')]
    blocks += ['<h1>确认或修改</h1>', paragraph('在本页圈选并写批注后，回到当前版本卡片点“按文档批注修改”。系统只读取你本人未解决的批注，并为下一版保留原文引用。直接编辑这份审阅文档不会改变待提交文件。')]
    marker = f'版本标识：{st["task_id"]} / r{st["revision"]} / {st.get("sha256") or "无提交包"}'
    blocks += [paragraph(marker)]
    draft.write_text('\n'.join(blocks), encoding='utf-8')
    # A landscape contact sheet gives the message an immediate visual anchor.
    cover = assets / 'cover.png'
    if images:
        from PIL import Image, ImageOps
        sheet = Image.new('RGB', (1200, 675), '#eef1f7')
        candidates = images[page_count:page_count+2] or images[:2]
        for i, p in enumerate(candidates):
            with Image.open(p) as im:
                thumb = ImageOps.contain(im.convert('RGB'), (570, 635))
                sheet.paste(thumb, (15+i*600+(570-thumb.width)//2, (675-thumb.height)//2))
        sheet.save(cover)
    return {'image_count': len(images), 'has_bundle': bool(st.get('submission')), 'pages': page_count, 'pictures': picture_count,
            'cover': str(cover) if images else None, 'marker': marker, 'draft': str(draft)}


def image_xml(path):
    return '<img path="@./' + esc(path.relative_to(c.ROOT)) + '" width="820"/>'


def publish(st):
    review = st.setdefault('review_doc', {})
    if review.get('verified'):
        return review
    if not review.get('draft'):
        decision = {'audience': '作业提交者', 'reader_task': '对照题目审阅当前版本产物并决定提交或提出修改',
                    'genre_contract': None, 'adapter': None, 'presentation_mode': 'rich',
                    'visual_plan': {'reason': '直接展示报告原页、附图和源码',
                                    'blocks': [{'type': 'table', 'min_count': 1, 'purpose': '核对本版文件清单'}]}}
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
    if (review['marker'] not in content or len(re.findall(r'<img\b', content)) < review['image_count']
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
