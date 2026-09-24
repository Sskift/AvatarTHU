import importlib
import os
import stat
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch


class ReviewTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.old = os.environ.get('AVATARTHU_HOME')
        os.environ['AVATARTHU_HOME'] = self.temp.name
        from loop import common, review
        self.c = importlib.reload(common)
        self.r = importlib.reload(review)
        self.root = self.c.ROOT
        self.base = self.c.OUT / '0123456789abcdef' / 'r1'
        self.base.mkdir(parents=True)
        self.answer = self.base / 'answer.txt'
        self.answer.write_text('A < B & C\nActual result')
        self.report = self.base / 'review.md'
        self.report.write_text('# Self check\nNo independent validation')
        self.st = {'task_id': '0123456789abcdef', 'revision': 1, 'title': 'Work', 'course': 'Course', 'deadline': 'tomorrow',
                   'description': 'Question', 'report': str(self.report), 'artifacts': [{'path': str(self.answer), 'sha256': self.c.digest(self.answer)}],
                   'submission': str(self.answer), 'sha256': self.c.digest(self.answer)}
        self.c.write_json(self.root / 'config.json', {'lark_user_id': 'owner'})

    def tearDown(self):
        if self.old is None:
            os.environ.pop('AVATARTHU_HOME', None)
        else:
            os.environ['AVATARTHU_HOME'] = self.old
        self.temp.cleanup()

    def test_sections_embed_every_frozen_file_and_limit_pdf_preview(self):
        import pymupdf
        p = self.base / 'answer.pdf'
        with pymupdf.open() as doc:
            doc.new_page().insert_text((72, 72), 'Actual submission')
            doc.new_page().insert_text((72, 72), 'Second page stays in the PDF')
            doc.save(p)
        self.st['artifacts'].append({'path': str(p), 'sha256': self.c.digest(p)})
        draft = self.root / 'draft.xml'
        result = self.r.build(self.st, draft)
        self.assertEqual(result['pages'], 1)
        self.assertEqual(result['attachment_names'], ['answer.txt', 'answer.pdf', 'review.md'])
        from xml.etree import ElementTree as E
        root = E.fromstring('<root>' + draft.read_text() + '</root>')
        self.assertEqual([x.text for x in root.findall('h1')],
                         ['一、作业描述', '二、完成情况与关键结果', '三、完整产物', '四、审阅与操作'])
        self.assertEqual(len(list(root.iter('source'))), 3)
        self.assertIn(self.st['sha256'], draft.read_text())
        p.write_bytes(b'changed')
        with self.assertRaises(ValueError):
            self.r.build(self.st, draft)

    def test_selected_evidence_comes_from_frozen_archive_and_original_inputs_are_embedded(self):
        import io
        from PIL import Image
        image = io.BytesIO()
        Image.new('RGB', (20, 20), 'blue').save(image, format='PNG')
        archive = self.base / 'code.zip'
        with zipfile.ZipFile(archive, 'w') as z:
            for i in range(5):
                z.writestr(f'images/result-{i}.png', image.getvalue())
        self.st['artifacts'].append({'path': str(archive), 'sha256': self.c.digest(archive)})
        self.st['presentation'] = {'assignment': '1. Input A < B & C', 'checks': ['One recorded check'],
                                  'highlights': [{'title': 'Inspect this output', 'detail': 'Look at the boundary',
                                                  'artifact': 'final/code.zip', 'member': 'images/result-4.png'}]}
        inputs = self.root / 'job/input/attachments'
        inputs.mkdir(parents=True)
        for name in ('question.pdf', 'question.pdf.txt', 'question.pdf-page-1.png'):
            (inputs / name).write_text('Source question')
        self.st['job'] = str(self.root / 'job')
        draft = self.root / 'draft.xml'
        result = self.r.build(self.st, draft)
        self.assertEqual(result['image_count'], 1)
        self.assertIn('question.pdf', result['attachment_names'])
        self.assertNotIn('question.pdf.txt', result['attachment_names'])
        self.assertNotIn('question.pdf-page-1.png', result['attachment_names'])
        self.assertIn('A &lt; B &amp; C', draft.read_text())
        self.assertIn('Look at the boundary', draft.read_text())
        self.assertEqual((self.root / 'assets/figure-1.png').read_bytes(), image.getvalue())
        self.st['presentation']['highlights'][0]['member'] = '../outside.png'
        with self.assertRaises(ValueError):
            self.r.build(self.st, draft)

    def test_zip_skips_paths_links_large_members_and_binaries(self):
        path = self.base / 'code.zip'
        with zipfile.ZipFile(path, 'w') as z:
            z.writestr('../bad.py', 'bad')
            z.writestr('/bad.py', 'bad')
            z.writestr('..\\bad.py', 'bad')
            z.writestr('.secret.txt', 'bad')
            z.writestr('binary.exe', 'bad')
            z.writestr('large.txt', b'x' * (self.r.MAX_MEMBER+1))
            info = zipfile.ZipInfo('link.py')
            info.external_attr = (stat.S_IFLNK | 0o777) << 16
            z.writestr(info, '/outside')
            z.writestr('src/main.py', 'print(1)')
        self.assertEqual(list(self.r.zip_previews(path)), [('src/main.py', b'print(1)')])

    def test_created_document_is_reused_after_fetch_failure(self):
        draft = self.root / 'draft.xml'
        self.st['review_doc'] = self.r.build(self.st, draft)
        def call(*args, **kwargs):
            if '+script' in args:
                return {'assessment': {'status': 'passed'}}
            if '+create' in args:
                return {'document': {'document_id': 'doc', 'url': 'https://example.feishu.cn/docx/doc'}}
            raise TimeoutError('fetch failed')
        with patch.object(self.c, 'lark', side_effect=call):
            with self.assertRaises(TimeoutError):
                self.r.publish(self.st)
        saved = self.c.read_json(self.c.task_path(self.st['task_id']))
        self.assertEqual(saved['review_doc']['document_id'], 'doc')
        content = saved['review_doc']['marker'] + '<source token="file" name="answer.txt"/><source token="report" name="review.md"/>'
        with patch.object(self.c, 'lark', return_value={'document': {'content': content}}) as lark:
            self.r.publish(saved)
        self.assertEqual(lark.call_count, 1)
        self.assertIn('+fetch', lark.call_args.args)
        self.assertTrue(saved['review_doc']['verified'])

    def test_uncertain_create_never_creates_duplicate(self):
        self.st['review_doc'] = {'draft': str(self.root/'draft.xml'), 'creating': True}
        with patch.object(self.c, 'lark') as lark:
            with self.assertRaisesRegex(RuntimeError, '待核对'):
                self.r.publish(self.st)
            lark.assert_not_called()

    def test_incomplete_document_cannot_be_marked_verified(self):
        self.st['review_doc'] = {'draft': str(self.root/'draft.xml'), 'document_id': 'existing', 'marker': 'version', 'image_count': 2}
        with patch.object(self.c, 'lark', return_value={'document': {'content': 'version<img src="one"/>'}}):
            with self.assertRaisesRegex(RuntimeError, '尚未完整发布'):
                self.r.publish(self.st)
        self.assertFalse(self.st['review_doc'].get('verified'))

    def test_missing_attachment_cannot_be_marked_verified(self):
        self.st['review_doc'] = {'draft': str(self.root/'draft.xml'), 'document_id': 'existing', 'marker': 'version',
                                'image_count': 0, 'attachment_names': ['answer.txt', 'review.md']}
        with patch.object(self.c, 'lark', return_value={'document': {'content': 'version<source token="file" name="answer.txt"/>'}}):
            with self.assertRaisesRegex(RuntimeError, '尚未完整发布'):
                self.r.publish(self.st)
        self.assertFalse(self.st['review_doc'].get('verified'))

    def test_image_comment_includes_report_page_context(self):
        import json
        self.st['review_doc'] = {'url': 'https://example.feishu.cn/docx/doc'}
        relation = {'relation': json.dumps({'22-doc': {'positionInfo': {'blockID': 'page'}}})}
        item = {'comment_id': '1', 'quote': '', 'relation': relation, 'reply_list': {'replies': [
            {'user_id': 'owner', 'content': {'elements': [{'type': 'text_run', 'text_run': {'text': 'Fix this chart'}}]}}]}}
        responses = [{'items': [item], 'has_more': False}, {'document': {'content': '<h2>report.pdf</h2><p>第 3 页</p><img id="page"/>'}}]
        with patch.object(self.c, 'lark', side_effect=responses):
            entry = self.r.comments(self.st)[0]
        self.assertIn('report.pdf', entry['context'])
        self.assertIn('第 3 页', entry['context'])

    def test_comments_collect_all_pages_owner_only(self):
        self.st['review_doc'] = {'url': 'https://example.feishu.cn/docx/doc'}
        def reply(user, text):
            return {'user_id': user, 'content': {'elements': [{'type': 'text_run', 'text_run': {'text': text}}]}}
        pages = [{'items': [{'comment_id': '1', 'quote': 'Original text', 'reply_list': {'replies': [reply('owner', 'Fix it'), reply('other', 'Ignore')]}}], 'has_more': True, 'page_token': 'next'},
                 {'items': [{'comment_id': '2', 'has_more': True}], 'has_more': False},
                 {'items': [reply('owner', 'Another fix')], 'has_more': False}]
        with patch.object(self.c, 'lark', side_effect=pages):
            got = self.r.comments(self.st)
        self.assertEqual([x['text'] for x in got], ['Fix it', 'Another fix'])
        self.assertEqual(got[0]['quote'], 'Original text')

    def test_attachment_comment_retains_filename(self):
        import json
        self.st['review_doc'] = {'url': 'https://example.feishu.cn/docx/doc'}
        relation = {'relation': json.dumps({'doc': {'positionInfo': {'blockID': 'file'}}})}
        item = {'comment_id': '1', 'relation': relation, 'reply_list': {'replies': [
            {'user_id': 'owner', 'content': {'elements': [{'type': 'text_run', 'text_run': {'text': 'Fix page 2'}}]}}]}}
        responses = [{'items': [item], 'has_more': False}, {'document': {'content': '<h2>报告</h2><figure id="file"><source name="report.pdf"/></figure>'}}]
        with patch.object(self.c, 'lark', side_effect=responses):
            entry = self.r.comments(self.st)[0]
        self.assertIn('report.pdf', entry['context'])


if __name__ == '__main__':
    unittest.main()
