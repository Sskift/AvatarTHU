"""Reusable Card 2.0 layouts: course digest, reviewed work, and receipts.

Design references: lark-cli's card-2.0/style/component skills and
https://github.com/TWe1v3/Feishu-card-strong (newsletter pattern).
"""
import html
import re
from pathlib import Path


def plain(text):
    return {'tag': 'plain_text', 'content': str(text)}


def escape(text):
    text = html.escape(str(text), quote=False)
    return re.sub(r'([*`\[\]_~])', lambda m: f'&#{ord(m[0])};', text)


def md(text):
    return {'tag': 'markdown', 'content': text}


def columns(items, background='grey-50'):
    return {'tag': 'column_set', 'flex_mode': 'none', 'horizontal_spacing': '12px',
            'columns': [{'tag': 'column', 'width': 'weighted', 'weight': 1,
                         'padding': '12px', 'background_style': background,
                         'elements': [md(text)]} for text in items]}


def panel(title, text, expanded=False):
    return {'tag': 'collapsible_panel', 'expanded': expanded, 'header': {'title': plain(title)},
            'padding': '12px', 'border': {'color': 'grey-200', 'corner_radius': '8px'},
            'elements': [md(text)]}


def frame(title, subtitle, color, elements, tag=None):
    header = {'title': plain(title), 'subtitle': plain(subtitle), 'template': color}
    if tag:
        header['text_tag_list'] = [{'tag': 'text_tag', 'text': plain(tag), 'color': color}]
    return {'schema': '2.0', 'config': {'update_multi': True, 'width_mode': 'default', 'enable_forward': False,
                                       'summary': {'content': title + ' · ' + subtitle}},
            'header': header, 'body': {'direction': 'vertical', 'vertical_spacing': '12px',
                                      'padding': '12px', 'elements': elements}}


def assignment(st):
    ready = st['ready']
    status = '已完成自查 · 待你决定' if ready else '已完成自查 · 需要补充'
    color = 'indigo' if ready else 'orange'
    info = columns([f'**{status}**\n{escape(st["summary"][:260])}',
                    f'截止时间（北京）\n**{escape(st["deadline"])}**\n第 {st["revision"]} 版'],
                   'blue-50' if ready else 'orange-50')
    links = []
    for i, artifact in enumerate(st['artifacts']):
        label = escape(Path(artifact['path']).name)
        url = st.get('deliveries', {}).get('artifact-' + str(i), {}).get('message_app_link')
        links.append('• ' + (f'[{label}]({url})' if url and url.startswith('https://') else label))
    detail = '**本次产物**\n' + ('\n'.join(links) or '尚无可交付文件')
    report_url = st.get('deliveries', {}).get('review', {}).get('message_app_link')
    detail += '\n• ' + (f'[执行自查报告]({report_url})' if report_url and report_url.startswith('https://') else '执行自查报告')
    detail += '\n\n点击文件名查看，附件也已发送到本对话。'
    if st.get('source_cached'):
        detail += '\n\n基于已下载资料；当前登录待恢复，尚未重新同步。'
    if st.get('blockers'):
        detail += '\n\n**待补充项**\n' + '\n'.join('• ' + escape(b) for b in st['blockers'])
    elements = [info, panel('产物与检查详情', detail, expanded=not ready)]
    if ready:
        value = {'task_id': st['task_id'], 'revision': st['revision'], 'nonce': st['nonce'], 'action': 'submit'}
        elements.append({'tag': 'button', 'text': plain('确认，提交本版产物'), 'type': 'primary_filled', 'width': 'fill',
                         'behaviors': [{'type': 'callback', 'value': value}]})
    elements.append({'tag': 'form', 'name': 'feedback_form', 'elements': [
        {'tag': 'input', 'name': 'feedback', 'input_type': 'multiline_text', 'rows': 2, 'required': True,
         'label': plain('还需要调整？'), 'placeholder': plain('例如：补充第二题推导；我已放入自己的程序，请重新检查')},
        {'tag': 'button', 'name': 'revise', 'form_action_type': 'submit', 'text': plain('按意见修改'), 'type': 'default'}]})
    # The form footer is folded into the same block to stay within five visual groups.
    elements[-1]['elements'].append(md('<font color="grey">修改后会发送新版本；上版提交按钮随即失效。</font>'))
    return frame(st['title'], st['course'], color, elements, '待确认' if ready else '待补充')


def notice_digest(notes, date_text):
    courses = len({n['course'] for n in notes})
    elements = [columns([f'**{len(notes)} 条未读公告**\n送达后自动标为已读', f'{courses} 门课程\n{escape(date_text)}'], 'turquoise-50')]
    for note in notes:
        # Keep source paragraphs and line breaks; never try to render arbitrary source HTML/Markdown.
        body = note['body'][:3500]
        suffix = '\n\n原文较长，完整内容见网络学堂。' if len(note['body']) > len(body) else ''
        elements.append(panel(f'{note["course"]} · {note["title"]}',
                              f'<font color="grey">{escape(note["date"])}</font>\n\n' + escape(body) + suffix,
                              expanded=len(notes) == 1))
    elements.append({'tag': 'button', 'text': plain('打开网络学堂'), 'type': 'default',
                     'behaviors': [{'type': 'open_url', 'default_url': 'https://learn.tsinghua.edu.cn/'}]})
    return frame('网络学堂 · 今日更新', '课程公告', 'turquoise', elements)


def receipt(title, summary, details='', success=True):
    color = 'green' if success else 'orange'
    elements = [columns([f'**{escape(summary)}**'], 'green-50' if success else 'orange-50')]
    if details:
        elements.append(md(escape(details)))
    return frame(title, 'AvatarTHU · 课程助手', color, elements)
