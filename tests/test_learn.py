import base64
import importlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


class LearnTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.old_home = os.environ.get('AVATARTHU_HOME')
        os.environ['AVATARTHU_HOME'] = self.temp.name
        from loop import common, learn, workflow
        self.common = importlib.reload(common)
        self.learn = importlib.reload(learn)
        self.workflow = importlib.reload(workflow)
        self.c = Mock(semester_id='2026-2027-1')

    def tearDown(self):
        if self.old_home is None:
            os.environ.pop('AVATARTHU_HOME', None)
        else:
            os.environ['AVATARTHU_HOME'] = self.old_home
        self.temp.cleanup()

    def test_base64_notice_body_and_read_flags(self):
        self.c.list_courses.return_value = [{'wlkcid': 'c1', 'kcm': '课程'}]
        row = {'ggid': 'g1', 'bt': '公告', 'sfyd': '否', 'ggnrStr': 'truncated',
               'ggnr': base64.b64encode('<p>完整公告 &amp; 正文</p>'.encode()).decode()}
        with patch.object(self.learn, 'announcement_rows', side_effect=[[row], []]):
            result = self.learn.announcements(self.c)
        self.assertTrue(result[0]['unread'])
        self.assertEqual(result[0]['body'], '完整公告 & 正文')
        self.assertEqual(len(list(Path(self.temp.name).glob('data/courses/*/*/notices/*.md'))), 1)

    def test_course_names_cannot_escape_or_collide(self):
        a = self.learn.course_dir(self.c, {'wlkcid': 'a', 'kcm': '../同名课'})
        b = self.learn.course_dir(self.c, {'wlkcid': 'b', 'kcm': '../同名课'})
        self.assertNotEqual(a, b)
        self.assertTrue(a.resolve().is_relative_to(self.common.DATA))

    def test_pagination_no_silent_truncation(self):
        first = [{'xszyid': str(i)} for i in range(100)]
        self.c.get_json.side_effect = [dict(result='success', object={'aaData': first}),
                                       dict(result='success', object={'aaData': [{'xszyid': '100'}]})]
        self.assertEqual(len(self.learn.homework_rows(self.c, 'c')), 101)
        self.c.get_json.side_effect = None
        self.c.get_json.return_value = dict(result='success', object={'aaData': first})
        with self.assertRaisesRegex(RuntimeError, '分页'):
            self.learn.homework_rows(self.c, 'c')

    def test_download_truncation_preserves_previous_file(self):
        dest = Path(self.temp.name) / 'file.pdf'
        dest.write_bytes(b'old-content')
        r = Mock(url='https://learn.tsinghua.edu.cn/download', headers={'Content-Type': 'application/pdf', 'Content-Length': '99'})
        r.iter_content.return_value = [b'%PDF-partial']
        self.c.session.get.return_value.__enter__ = Mock(return_value=r)
        self.c.session.get.return_value.__exit__ = Mock(return_value=False)
        with self.assertRaisesRegex(RuntimeError, '不完整'):
            self.learn.download_file(self.c, '/download', dest, 'v2')
        self.assertEqual(dest.read_bytes(), b'old-content')
        self.assertFalse(dest.with_name('.file.pdf.download.json').exists())

    def test_download_cache_repairs_tampered_file(self):
        dest = Path(self.temp.name) / 'file.pdf'
        r = Mock(url='https://learn.tsinghua.edu.cn/download', headers={'Content-Type': 'application/pdf', 'Content-Length': '6'})
        r.iter_content.return_value = [b'%PDF-x']
        self.c.session.get.return_value.__enter__ = Mock(return_value=r)
        self.c.session.get.return_value.__exit__ = Mock(return_value=False)
        self.learn.download_file(self.c, '/download', dest, 'v1')
        self.learn.download_file(self.c, '/download', dest, 'v1')
        self.assertEqual(self.c.session.get.call_count, 1)
        dest.write_text('tampered')
        self.learn.download_file(self.c, '/download', dest, 'v1')
        self.assertEqual(self.c.session.get.call_count, 2)

    def test_html_login_is_not_saved_as_pdf(self):
        dest = Path(self.temp.name) / 'file.pdf'
        r = Mock(url='https://learn.tsinghua.edu.cn/login', headers={'Content-Type': 'text/html'})
        self.c.session.get.return_value.__enter__ = Mock(return_value=r)
        self.c.session.get.return_value.__exit__ = Mock(return_value=False)
        with self.assertRaises(RuntimeError):
            self.learn.download_file(self.c, '/download', dest, 'v1')
        self.assertFalse(dest.exists())

    def test_homework_preview_link_is_not_downloaded(self):
        from bs4 import BeautifulSoup
        html = '''<div class="boxbox"><div class="list fujian clearfix">
        <a href="/f/preview?downloadUrl=/b/download">question.pdf</a>
        <a href="/b/download?_csrf=secret&amp;id=attachment">下载</a>
        </div></div>'''
        with patch.object(self.learn, 'page', return_value=BeautifulSoup(html, 'html.parser')):
            _, _, attachments = self.learn.homework_detail(self.c, 'c', {'bt': 'Title', 'zyid': 'z', 'xszyid': 'x', 'zyfjid': 'a'})
        self.assertEqual(attachments, [('question.pdf', 'https://learn.tsinghua.edu.cn/b/download?id=attachment')])

    def test_mark_read_requires_remote_confirmation(self):
        note = {'course_id': 'c', 'id': 'n', 'kind': 'Wgq'}
        with patch.object(self.learn, 'page'), patch.object(self.learn, 'announcement_rows', return_value=[{'ggid': 'n', 'sfyd': '否'}]):
            with self.assertRaises(RuntimeError):
                self.learn.mark_notice_read(self.c, note)
        with patch.object(self.learn, 'page'), patch.object(self.learn, 'announcement_rows', return_value=[{'ggid': 'n', 'sfyd': '是'}]):
            self.learn.mark_notice_read(self.c, note)
        self.c.persist.assert_called_once()

    def test_parse_failed_response_is_not_empty_success(self):
        with self.assertRaises(RuntimeError):
            self.learn.rows({'result': 'failure', 'object': {'aaData': []}})


if __name__ == '__main__':
    unittest.main()
