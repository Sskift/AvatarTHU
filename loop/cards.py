"""Reusable Card 2.0 layouts: course digest, reviewed work, and receipts.

Design references: lark-cli's card-2.0/style/component skills and
https://github.com/TWe1v3/Feishu-card-strong (newsletter pattern).
"""
import html
import re
from urllib.parse import urlencode


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
    review = st.get('review_doc', {})
    status = ('独立复审通过 · 待你确认' if st.get('review_outcome') == 'approved' else '产物待审阅') if ready else '需要修改或补充'
    color = 'indigo' if ready else 'orange'
    info = columns([f'**{status}**\n作业描述、关键结果和所有产物集中在云文档。',
                    f'截止时间（北京）\n**{escape(st["deadline"])}**\n第 {st["revision"]} 版'],
                   'blue-50' if ready else 'orange-50')
    detail = '在云文档中查看题目、预览报告、下载全部产物。\n\n文档中的批注和编辑不会直接更改提交文件。'
    if st.get('review_history'):
        detail += '\n\n历次独立复审的结论、意见和检查记录都在云文档第五部分。'
    if st.get('source_cached'):
        detail += '\n\n基于已下载资料；当前登录待恢复，尚未重新同步。'
    if st.get('blockers'):
        detail += '\n\n**待补充项**\n' + '\n'.join('• ' + escape(b) for b in st['blockers'])
    elements = [info]
    controls = []
    if review.get('verified') and review.get('url'):
        controls.append({'tag': 'button', 'text': plain('打开审阅文档'), 'type': 'primary_filled', 'width': 'fill',
                         'behaviors': [{'type': 'open_url', 'default_url': review['url']}]})
        controls.append({'tag': 'button', 'text': plain('按文档批注修改'), 'type': 'default', 'width': 'fill',
                         'behaviors': [{'type': 'callback', 'value': {'task_id': st['task_id'], 'revision': st['revision'], 'nonce': st['nonce'], 'action': 'revise_comments'}}]})
    if ready:
        value = {'task_id': st['task_id'], 'revision': st['revision'], 'nonce': st['nonce'], 'action': 'submit'}
        controls.append({'tag': 'button', 'text': plain('审阅完成，提交本版'), 'type': 'default', 'width': 'fill',
                         'behaviors': [{'type': 'callback', 'value': value}]})
    if controls:
        elements.append({'tag': 'column_set', 'flex_mode': 'none', 'horizontal_spacing': '8px',
                         'columns': [{'tag': 'column', 'width': 'weighted', 'weight': 1, 'elements': [button]} for button in controls]})
    elements.append(panel('审阅说明与待补充项', detail, expanded=not ready))
    elements.append({'tag': 'form', 'name': 'feedback_form', 'elements': [
        {'tag': 'input', 'name': 'feedback', 'input_type': 'multiline_text', 'rows': 2, 'required': True,
         'label': plain('直接写修改意见'), 'placeholder': plain('例如：报告第 3 页的推导补充中间步骤；案例 4 的输出有问题')},
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
        if note.get('attachment_name'):
            suffix += '\n\n公告附件：' + escape(note['attachment_name'])
        elements.append(panel(f'{note["course"]} · {note["title"]}',
                              f'<font color="grey">{escape(note["date"])}</font>\n\n' + escape(body) + suffix,
                              expanded=len(notes) == 1))
    url = 'https://learn.tsinghua.edu.cn/'
    if len(notes) == 1 and notes[0].get('course_id') and notes[0].get('id'):
        url += 'f/wlxt/kcgg/wlkc_ggb/student/beforeViewXs?' + urlencode({'wlkcid': notes[0]['course_id'], 'id': notes[0]['id']})
    elements.append({'tag': 'button', 'text': plain('查看公告原文与附件'), 'type': 'primary_filled', 'width': 'fill',
                     'behaviors': [{'type': 'open_url', 'default_url': url}]})
    return frame('网络学堂 · 今日更新', '课程公告', 'turquoise', elements)


def receipt(title, summary, details='', success=True):
    color = 'green' if success else 'orange'
    elements = [columns([f'**{escape(summary)}**'], 'green-50' if success else 'orange-50')]
    if details:
        elements.append(md(escape(details)))
    return frame(title, 'AvatarTHU · 课程助手', color, elements)
