import importlib
import os
import signal
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch, Mock


class CrossReviewTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.old_home = os.environ.get('AVATARTHU_HOME')
        os.environ['AVATARTHU_HOME'] = self.temp.name
        from loop import common, review, local_review, delivery, workflow, cross_review, engines
        self.c = importlib.reload(common)
        importlib.reload(review)
        self.local = importlib.reload(local_review)
        importlib.reload(delivery)
        self.w = importlib.reload(workflow)
        self.r = cross_review
        self.engines = engines
        self.cfg = {'lark_enabled': False, 'claude_cli': '/fake/claude', 'codex_cli': '/fake/codex',
                    'review_mode': 'claude-codex', 'max_review_rounds': 3}
        self.c.write_json(self.c.CONFIG, self.cfg)
        folder = self.c.DATA / 'homework'
        (folder / 'source').mkdir(parents=True)
        (folder / 'courseware').mkdir()
        (folder / 'source/question.txt').write_text('What is 17 + 25?')
        (folder / 'courseware/chapter.txt').write_text('Add integers.')
        self.st = {'task_id': '0123456789abcdef', 'status': 'queued', 'revision': 1,
                   'course': 'Arithmetic', 'title': 'Addition', 'description': 'Compute 17 + 25.',
                   'deadline': '2027-01-01 12:00:00', 'assignment_dir': str(folder),
                   'folder': str(folder / 'source'), 'courseware': str(folder / 'courseware')}
        self.calls = []

    def tearDown(self):
        if self.old_home is None:
            os.environ.pop('AVATARTHU_HOME', None)
        else:
            os.environ['AVATARTHU_HOME'] = self.old_home
        self.temp.cleanup()

    def engine(self, executor, job, prompt, schema, role, plan):
        self.calls.append((executor, role, job, prompt))
        if role == 'writer':
            (job / 'final').mkdir()
            (job / 'final/answer.txt').write_text('42' if 'Correct the addition' in prompt else '41')
            (job / 'review.md').write_text('WRITER_SELF_ASSESSMENT_DO_NOT_SHARE')
            return {'ready': True, 'summary': 'Calculated answer', 'files': ['final/answer.txt'], 'blockers': []}
        visible = {str(p.relative_to(job)) for p in job.rglob('*') if p.is_file()}
        self.assertEqual(visible, {'input/assignment.md', 'input/attachments/question.txt',
                                   'input/courseware/chapter.txt', 'candidate/answer.txt'})
        self.assertNotIn('WRITER_SELF_ASSESSMENT_DO_NOT_SHARE', prompt)
        self.assertNotIn('Correct the addition', prompt)
        self.assertNotIn('PREVIOUS_VERDICT', prompt)
        approved = (job / 'candidate/answer.txt').read_text() == '42'
        return {'approved': approved, 'summary': 'Correct' if approved else 'Incorrect addition',
                'comments': [] if approved else [{'location': 'answer.txt', 'comment': '17 + 25 is 42',
                                                  'suggestion': 'Correct the addition'}],
                'checks': ['Recomputed 17 + 25'], 'limitations': []}

    def test_both_pairings_rewrite_then_independent_review_and_local_document(self):
        for mode, writer, reviewer in [('claude-codex', 'claude', 'codex'), ('codex-claude', 'codex', 'claude')]:
            with self.subTest(mode=mode):
                self.calls.clear()
                self.cfg['review_mode'] = mode
                self.c.write_json(self.c.CONFIG, self.cfg)
                st = dict(self.st, task_id='a' * 16 if writer == 'claude' else 'b' * 16)
                with patch.object(self.engines, 'run', side_effect=self.engine):
                    self.w.process(st)
                self.assertEqual([(e, r) for e, r, _, _ in self.calls],
                                 [(writer, 'writer'), (reviewer, 'reviewer')] * 2)
                self.assertEqual(len({p for _, _, p, _ in self.calls}), 4)
                self.assertEqual([r['approved'] for r in st['review_history']], [False, True])
                self.assertEqual(st['status'], 'awaiting')
                self.assertEqual(Path(st['submission']).read_text(), '42')
                second_writer = self.calls[2][2]
                self.assertEqual((second_writer / 'previous-final/answer.txt').read_text(), '41')
                page = Path(st['local_review']).read_text()
                self.assertIn('五、历次独立复审', page)
                self.assertIn('Incorrect addition', page)
                self.assertIn('Correct the addition', page)
                self.assertIn('Recomputed 17 + 25', page)

    def test_review_limit_delivers_all_comments_for_owner(self):
        self.cfg['max_review_rounds'] = 2
        self.c.write_json(self.c.CONFIG, self.cfg)
        def always_reject(*args):
            result = self.engine(*args)
            if args[4] == 'reviewer':
                result.update(approved=False, summary='Still incomplete')
            return result
        with patch.object(self.engines, 'run', side_effect=always_reject):
            self.w.process(self.st)
        self.assertEqual(len(self.calls), 4)
        self.assertEqual(len(self.st['review_history']), 2)
        self.assertEqual(self.st['status'], 'needs_student')
        self.assertFalse(self.st['ready'])
        self.assertIn('第 2 轮', self.st['blockers'][0])

    def test_review_crash_reuses_completed_writer_but_starts_fresh_reviewer(self):
        def interrupted(*args):
            if args[4] == 'reviewer':
                (args[1] / 'partial-context.txt').write_text('Do not resume this conversation')
                raise RuntimeError('review interrupted')
            return self.engine(*args)
        with patch.object(self.engines, 'run', side_effect=interrupted):
            with self.assertRaisesRegex(RuntimeError, 'review interrupted'):
                self.w.process(self.st)
        old_review = self.st['review_attempt']['job']
        with patch.object(self.engines, 'run', side_effect=self.engine):
            self.w.process(self.st)
        self.assertEqual([role for _, role, _, _ in self.calls], ['writer', 'reviewer', 'writer', 'reviewer'])
        self.assertNotEqual(str(self.calls[1][2]), old_review)
        self.assertEqual(self.st['status'], 'awaiting')

    def test_durable_review_survives_task_state_write_failure(self):
        original_save = self.c.save_task
        def fail_after_receipt(st):
            if st.get('review_history'):
                raise OSError('task state unavailable')
            original_save(st)
        with patch.object(self.engines, 'run', side_effect=self.engine), patch.object(self.c, 'save_task', side_effect=fail_after_receipt):
            with self.assertRaisesRegex(OSError, 'task state unavailable'):
                self.w.process(self.st)
        restored = self.c.read_json(self.c.task_path(self.st['task_id']))
        self.assertFalse(restored.get('review_history'))
        self.assertTrue(Path(restored['review_attempt']['job'], 'complete.json').exists())
        with patch.object(self.engines, 'run', side_effect=self.engine):
            self.w.process(restored)
        # The first completed review is recovered; only round two invokes the CLIs again.
        self.assertEqual([role for _, role, _, _ in self.calls], ['writer', 'reviewer', 'writer', 'reviewer'])
        self.assertEqual(len(restored['review_history']), 2)

    def test_new_revision_keeps_history_and_uses_new_pairing(self):
        with patch.object(self.engines, 'run', side_effect=self.engine):
            self.w.process(self.st)
            first_ids = [r['id'] for r in self.st['review_history']]
            self.local.revise(self.st['task_id'], 'Another independent check')
            self.cfg['review_mode'] = 'codex-claude'
            self.c.write_json(self.c.CONFIG, self.cfg)
            st = self.c.read_json(self.c.task_path(self.st['task_id']))
            self.w.process(st)
        self.assertEqual(st['revision'], 2)
        self.assertEqual([r['id'] for r in st['review_history'][:2]], first_ids)
        self.assertEqual(st['review_history'][-1]['writer'], 'codex')
        self.assertEqual(st['review_history'][-1]['reviewer'], 'claude')
        self.assertEqual(len(st['review_history']), 4)
        self.assertIn('五、历次独立复审', Path(st['local_review']).read_text())

    def test_cli_defaults_and_fresh_sessions_are_used_for_both_engines(self):
        workspace = self.c.DATA
        workspace.mkdir(exist_ok=True)
        for executor in ('claude', 'codex'):
            command = self.engines.command(executor, workspace, self.r.SCHEMA,
                                          dict(self.cfg, claude_model='ignored', codex_model='ignored'))
            self.assertNotIn('--model', command)
            self.assertNotIn('ignored', command)
            self.assertNotIn('--ignore-user-config', command)
            self.assertNotIn('--resume', command)
            self.assertIn('--no-session-persistence' if executor == 'claude' else '--ephemeral', command)

    def test_service_stop_terminates_active_model_process_group(self):
        workspace = self.c.DATA / 'interrupted'
        workspace.mkdir(parents=True)
        original = signal.getsignal(signal.SIGTERM)
        def stopped(*args, **kwargs):
            signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
        process = Mock(pid=123, communicate=Mock(side_effect=stopped))
        with patch.object(self.engines.subprocess, 'Popen', return_value=process), patch.object(self.engines.os, 'killpg') as kill:
            with self.assertRaises(SystemExit):
                self.engines.run('claude', workspace, 'prompt', self.r.SCHEMA, 'reviewer', self.cfg)
        kill.assert_called_once_with(123, signal.SIGTERM)
        self.assertIs(signal.getsignal(signal.SIGTERM), original)


if __name__ == '__main__':
    unittest.main()
