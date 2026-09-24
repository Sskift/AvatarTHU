"""Standalone review pages from the same frozen content as Feishu documents."""
import html
import xml.etree.ElementTree as ET
from pathlib import Path
from . import common as c
from .review import build


def publish(st):
    folder = c.DATA / 'reviews' / st['task_id'] / f'r{st["revision"]}'
    folder.mkdir(parents=True, exist_ok=True)
    draft = folder / 'content.xml'
    build(st, draft, local=True)
    root = ET.fromstring('<review>' + draft.read_text() + '</review>')

    def file_url(node):
        raw = node.attrib['path']
        if not raw.startswith('@./'):
            raise ValueError('本地审阅资源必须来自本版文件。')
        return c.inside(c.ROOT, raw[3:]).as_uri()

    def render(node):
        if node.tag == 'figure':
            source = node.find('source')
            if source is None:
                return ''
            return '<p class="attachment"><a href="' + html.escape(file_url(source), quote=True) + '">' + html.escape(source.get('name', '附件')) + '</a></p>'
        if node.tag == 'img':
            return '<img alt="本版产物预览" src="' + html.escape(file_url(node), quote=True) + '"/>'
        inside = html.escape(node.text or '') + ''.join(render(child) + html.escape(child.tail or '') for child in node)
        if node.tag == 'review':
            return inside
        tag = {'title': 'h1', 'callout': 'aside', 'latex': 'code', 'h1': 'h2', 'h2': 'h3', 'h3': 'h4'}.get(node.tag, node.tag)
        if tag == 'br':
            return '<br/>'
        attrs = ''
        if tag == 'a':
            href = node.get('href', '')
            if href.startswith(('https://', 'http://')):
                attrs = ' href="' + html.escape(href, quote=True) + '" rel="noreferrer"'
            else:
                tag = 'span'
        if tag not in {'h1','h2','h3','h4','h5','h6','p','b','em','del','span','a','aside','code','pre','ul','ol','li','table','thead','tbody','tr','th','td','blockquote'}:
            tag = 'div'
        return '<' + tag + attrs + '>' + inside + '</' + tag + '>'

    page = folder / 'index.html'
    page.write_text('''<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src file:; style-src 'unsafe-inline'">
<title>''' + html.escape(st['title']) + ''' · AvatarTHU 审阅</title>
<style>body{max-width:900px;margin:40px auto;padding:0 24px 60px;font:16px/1.85 -apple-system,BlinkMacSystemFont,'PingFang SC',sans-serif;color:#243248;background:#f9fafc}h1{font-size:30px;line-height:1.4}h2{margin-top:44px;padding-top:20px;border-top:1px solid #dce2ec}h3{font-size:18px}aside,.attachment{background:#edf3fb;padding:12px 18px;border-radius:10px}a{color:#2358ad;overflow-wrap:anywhere}img{max-width:100%;height:auto;border:1px solid #dce2ec;border-radius:8px}pre{overflow:auto;padding:16px;background:#edf0f5}table{border-collapse:collapse;width:100%}td,th{border:1px solid #dce2ec;padding:8px}li{margin:8px 0}p{overflow-wrap:anywhere}</style><body>'''
                    + render(root) + '</body></html>', encoding='utf-8')
    st['local_review'] = str(page)
    c.save_task(st)
    return page


def revise(task_id, feedback):
    if not feedback.strip():
        raise ValueError('修改意见不能为空。')
    path = c.task_path(task_id)
    with c.lock(task_id):
        st = c.read_json(path)
        if st.get('status') not in {'awaiting', 'needs_student', 'approval_invalid'}:
            raise RuntimeError('该作业当前不在可修改的审阅状态。')
        st.update(status='revision_ready', feedback=feedback.strip(), approval_event=None)
        c.save_task(st)
    print('修改意见已保存，下一次调度会生成新版；旧版提交按钮已失效。')
