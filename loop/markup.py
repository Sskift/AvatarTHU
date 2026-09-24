"""Convert Markdown to native document blocks without loading embedded content."""
import html
from markdown_it import MarkdownIt
from mdit_py_plugins.dollarmath import dollarmath_plugin


def escape(text):
    return html.escape(str(text), quote=True)


def markdown_xml(text, base_level=2):
    parser = MarkdownIt('commonmark', {'html': False}).enable(['table', 'strikethrough'])
    parser.use(dollarmath_plugin)
    tokens = parser.parse(text)
    minimum = min((int(t.tag[1]) for t in tokens if t.type == 'heading_open'), default=1)
    previous = base_level - 1
    headings = []

    def inline(items):
        result = []
        links = []
        for token in items or []:
            kind = token.type
            if kind == 'text':
                result.append(escape(token.content))
            elif kind in {'softbreak', 'hardbreak'}:
                result.append('<br/>')
            elif kind == 'code_inline':
                result.append('<span background-color="light-gray">' + escape(token.content) + '</span>')
            elif kind == 'math_inline':
                result.append('<latex>' + escape(token.content) + '</latex>')
            elif kind == 'link_open':
                href = token.attrGet('href') or ''
                allowed = href.startswith(('https://', 'http://'))
                links.append(allowed)
                if allowed:
                    result.append('<a href="' + escape(href) + '">')
            elif kind == 'link_close':
                if links.pop():
                    result.append('</a>')
            elif kind == 'image':
                result.append(escape(token.content))
            elif kind in {'strong_open', 'strong_close', 'em_open', 'em_close', 's_open', 's_close'}:
                tag = {'strong': 'b', 'em': 'em', 's': 'del'}[kind.rsplit('_', 1)[0]]
                result.append(('<' if token.nesting == 1 else '</') + tag + '>')
            else:
                result.append(escape(token.content))
        return ''.join(result)

    result = []
    for i, token in enumerate(tokens):
        kind = token.type
        if kind == 'inline':
            result.append(inline(token.children))
        elif kind in {'fence', 'code_block'}:
            lang = (token.info.strip().split() or ['plain text'])[0]
            lang = {'text': 'plain text', 'cmd': 'powershell', 'sh': 'bash'}.get(lang, lang)
            result.append('<pre lang="' + escape(lang) + '"><code>' + escape(token.content) + '</code></pre>')
        elif kind == 'math_block':
            result.append('<p><latex>' + escape(token.content.strip()) + '</latex></p>')
        elif kind == 'heading_open':
            level = min(6, previous + 1, base_level + int(token.tag[1]) - minimum)
            previous = level
            headings.append(level)
            result.append(f'<h{level}>')
        elif kind == 'heading_close':
            result.append(f'</h{headings.pop()}>')
        elif kind == 'hr':
            result.append('<hr/>')
        elif kind == 'table_open':
            columns = 0
            for cell in tokens[i+1:]:
                if cell.type == 'tr_close':
                    break
                columns += cell.type == 'th_open'
            result.append('<table><colgroup>' + f'<col width="{max(80, 820 // max(columns, 1))}"/>' * columns + '</colgroup>')
        elif kind in {'th_open', 'td_open'}:
            color = ' background-color="light-gray"' if token.tag == 'th' else ''
            result.append('<' + token.tag + color + '><p>')
        elif kind in {'th_close', 'td_close'}:
            result.append('</p></' + token.tag + '>')
        elif token.hidden:
            continue
        elif token.tag in {'p', 'ul', 'ol', 'li', 'blockquote', 'thead', 'tbody', 'tr', 'table'}:
            result.append(('<' + token.tag + '>') if token.nesting == 1 else '</' + token.tag + '>')
        else:
            result.append(escape(token.content))
    return ''.join(result)


def extracted_text_xml(text):
    """Keep PDF line order while separating pages and paragraphs for reading."""
    result = []
    paragraph = []
    def flush():
        if paragraph:
            result.append('<p>' + '<br/>'.join(escape(line) for line in paragraph) + '</p>')
            paragraph.clear()
    import re
    for line in text.splitlines():
        line = line.strip()
        if not line:
            flush()
        elif re.fullmatch(r'\[第 \d+ 页\]', line):
            flush()
            result.append('<h3>' + escape(line.strip('[]')) + '</h3>')
        else:
            if re.match(r'^(?:\d+[.、]|[•●])\s*', line) or len(''.join(paragraph)) > 250:
                flush()
            paragraph.append(line)
    flush()
    return ''.join(result)
