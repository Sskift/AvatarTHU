import copy
import importlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch, Mock

class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name).resolve()
        self.old_home = os.environ.get('AVATARTHU_HOME')
        os.environ['AVATARTHU_HOME'] = str(self.root)
        from loop import common, learn, cards, delivery, workflow, actions, messages, daily
        self.c = importlib.reload(common)
        self.learn = importlib.reload(learn)
        self.delivery = importlib.reload(delivery)
        publisher = patch.object(self.delivery, 'publish', return_value={'verified': True})
        publisher.start()
        self.addCleanup(publisher.stop)
        self.workflow = importlib.reload(workflow)
        self.actions = importlib.reload(actions)
        self.messages = importlib.reload(messages)
        self.daily = importlib.reload(daily)
        self.c.write_json(self.root / 'config.json', {'lark_user_id': 'ou_owner', 'lark_cli': '/fake/lark', 'claude_cli': '/fake/claude'})
        tid = '0123456789abcdef'
        folder = self.root / 'outbox' / tid / 'r1'
        folder.mkdir(parents=True)
        answer = folder / 'answer.txt'
        answer.write_text('Reviewed content')
        self.st = {'task_id': tid, 'revision': 1, 'status': 'awaiting', 'ready': True, 'blockers': [],
                   'title': 'Assignment', 'course': 'Course', 'deadline': '2099-01-01 12:00', 'summary': 'Reviewed',
                   'nonce': 'secret-version', 'card_message_id': 'om_card', 'chat_id': 'oc_chat',
                   'submission': str(answer), 'sha256': self.c.digest(answer), 'course_id': 'course-id', 'xszyid': 'student-homework-id',
                   'artifacts': [{'path': str(answer), 'sha256': self.c.digest(answer)}]}
        self.c.save_task(self.st)
        self.event = {'event_id': 'event-1', 'operator_id': 'ou_owner', 'message_id': 'om_card',
                      'action_value': json.dumps({'task_id': tid, 'revision': 1, 'nonce': 'secret-version', 'action': 'submit'})}

    def tearDown(self):
        if self.old_home is None:
            os.environ.pop('AVATARTHU_HOME', None)
        else:
            os.environ['AVATARTHU_HOME'] = self.old_home
        self.temp.cleanup()

    def test_submit_only_current_owner_once(self):
        with patch.object(self.learn, 'verify_open', return_value=object()), patch.object(self.learn, 'upload', return_value={'result': 'success'}) as upload, patch.object(self.actions, 'send', return_value={'message_id': 'receipt'}):
            self.assertTrue(self.actions.act(self.event))
            self.assertFalse(self.actions.act(self.event))
            upload.assert_called_once()
            self.assertEqual(self.c.read_json(self.c.task_path(self.st['task_id']))['status'], 'submitted')

    def test_stale_forged_and_blocked_cards_never_submit(self):
        cases = [dict(self.event, operator_id='ou_stranger'), dict(self.event, message_id='om_old'),
                 dict(self.event, action_value=json.dumps({'task_id': '../config', 'action': 'submit'})),
                 dict(self.event, action_value=json.dumps({'task_id': self.st['task_id'], 'revision': 0, 'nonce': 'secret-version', 'action': 'submit'})),
                 dict(self.event, action_value=json.dumps({'task_id': self.st['task_id'], 'revision': 1, 'nonce': 'wrong', 'action': 'submit'}))]
        with patch.object(self.learn, 'upload') as upload:
            for event in cases:
                self.assertFalse(self.actions.act(event))
            blocked = dict(self.st, status='needs_student', ready=False)
            self.c.save_task(blocked)
            self.assertFalse(self.actions.act(self.event))
            upload.assert_not_called()

    def test_modified_artifact_requires_new_review(self):
        Path(self.st['submission']).write_text('Changed after card')
        with patch.object(self.actions, 'send'), patch.object(self.learn, 'upload') as upload:
            self.assertFalse(self.actions.act(self.event))
            upload.assert_not_called()
            self.assertEqual(self.c.read_json(self.c.task_path(self.st['task_id']))['status'], 'approval_invalid')

    def test_uncertain_upload_does_not_retry_or_claim_success(self):
        with patch.object(self.learn, 'verify_open', return_value=object()), patch.object(self.learn, 'upload', side_effect=TimeoutError('response lost')) as upload, patch.object(self.actions, 'send'):
            self.assertFalse(self.actions.act(self.event))
            self.assertFalse(self.actions.act(self.event))
            upload.assert_called_once()
            self.assertEqual(self.c.read_json(self.c.task_path(self.st['task_id']))['status'], 'submission_unknown')

    def test_form_feedback_invalidates_approval_immediately(self):
        event = dict(self.event, action_name='revise', action_value='', form_value=json.dumps({'feedback': 'Fix the derivation'}))
        with patch.object(self.actions, 'send'):
            self.assertTrue(self.actions.act(event))
        st = self.c.read_json(self.c.task_path(self.st['task_id']))
        self.assertEqual(st['status'], 'revision_ready')
        self.assertEqual(st['feedback'], 'Fix the derivation')
        with patch.object(self.learn, 'upload') as upload:
            self.assertFalse(self.actions.act(self.event))
            upload.assert_not_called()

    def test_failed_notice_delivery_not_acknowledged(self):
        note = {'id': 'notice', 'course_id': 'cid', 'unread': True, 'title': 'Notice', 'course': 'Course', 'date': 'today', 'body': 'details'}
        mark = Mock()
        with patch.object(self.delivery, 'send', side_effect=RuntimeError('offline')):
            with self.assertRaises(RuntimeError):
                self.delivery.send_notices([note], mark)
        mark.assert_not_called()
        self.assertEqual(self.c.read_json(self.c.DATA / 'notices.json'), {})
        with patch.object(self.delivery, 'send', return_value={'message_id': 'notice'}) as send:
            self.assertEqual(self.delivery.send_notices([note], mark), 1)
            self.assertEqual(self.delivery.send_notices([note], mark), 0)
            self.assertEqual(send.call_count, 1)

    def test_mark_read_failure_retries_without_resending(self):
        note = {'id': 'notice', 'course_id': 'cid', 'unread': True, 'title': 'Notice', 'course': 'Course', 'date': 'today', 'body': 'details'}
        with patch.object(self.delivery, 'send', return_value={'message_id': 'notice'}) as send:
            with self.assertRaises(TimeoutError):
                self.delivery.send_notices([note], Mock(side_effect=TimeoutError()))
            mark = Mock()
            self.delivery.send_notices([note], mark)
            send.assert_called_once()
            mark.assert_called_once_with(note)

    def test_read_announcements_not_sent(self):
        with patch.object(self.delivery, 'send') as send:
            self.delivery.send_notices([{'unread': False}], Mock())
            send.assert_not_called()

    def test_card_delivery_retry_never_sends_files_to_chat(self):
        report = Path(self.st['submission']).parent / 'review.md'
        report.write_text('Independent review')
        st = dict(self.st, status='delivery_pending', deliveries={}, report=str(report))
        self.c.save_task(st)
        with patch.object(self.delivery, 'send', side_effect=RuntimeError('card failed')) as send:
            with self.assertRaises(RuntimeError):
                self.delivery.deliver(st)
            self.assertNotIn('file', send.call_args.kwargs)
        saved = self.c.read_json(self.c.task_path(st['task_id']))
        self.assertEqual(saved['status'], 'delivery_pending')
        with patch.object(self.delivery, 'send', return_value={'message_id': 'new-card'}) as send:
            self.delivery.deliver(saved)
            send.assert_called_once()
            self.assertIn('content', send.call_args.kwargs)
        self.assertEqual(set(saved['deliveries']), {'card'})

    def test_document_failure_sends_no_chat_card_or_attachment(self):
        st = dict(self.st, status='delivery_pending', deliveries={})
        with patch.object(self.delivery, 'publish', side_effect=RuntimeError('missing document attachment')), patch.object(self.delivery, 'send') as send:
            with self.assertRaises(RuntimeError):
                self.delivery.deliver(st)
            send.assert_not_called()
        self.assertEqual(st['status'], 'delivery_pending')

    def test_failed_claude_output_not_reused(self):
        job = self.root / 'job'
        (job / 'draft').mkdir(parents=True)
        (job / 'draft/answer.md').write_text('stale output')
        self.c.write_json(job / 'result.json', {'ready': True, 'summary': 'stale', 'blockers': [], 'files': ['draft/answer.md']})
        fake = Mock(returncode=1, pid=123)
        with patch('loop.workflow.subprocess.Popen', return_value=fake):
            with self.assertRaises(RuntimeError):
                self.workflow.run_stage(job, 'solver', 'prompt')
        self.assertFalse((job / 'complete.json').exists())

    def test_artifact_path_and_symlink_escape_rejected(self):
        with self.assertRaises(ValueError):
            self.c.inside(self.root, '../escape')
        file = self.root / 'target'
        file.write_text('x')
        (self.root / 'link').symlink_to(file)
        with self.assertRaises(ValueError):
            self.c.inside(self.root, 'link')

    def test_remote_failure_is_not_upload_success(self):
        response = Mock()
        response.json.return_value = {'result': 'failure', 'msg': 'closed'}
        c = Mock()
        c.session.post.return_value = response
        with self.assertRaises(RuntimeError):
            self.learn.upload(c, self.st, Path(self.st['submission']))

    def test_card_native_v2_and_no_submit_when_incomplete(self):
        from loop.cards import assignment, notice_digest
        card = assignment(dict(self.st, ready=False, blockers=['Missing experiment']))
        text = json.dumps(card)
        self.assertNotIn('"action": "submit"', text)
        self.assertEqual(card['schema'], '2.0')
        self.assertTrue(any(x['tag'] == 'form' for x in card['body']['elements']))
        self.assertNotIn('"tag": "action"', text)
        notice = notice_digest([{'course': 'C', 'title': '<title>', 'date': 'date', 'body': '*text*'}], 'today')
        self.assertIn('collapsible_panel', json.dumps(notice))

    def test_receipt_retry_does_not_upload_twice(self):
        with patch.object(self.learn, 'verify_open', return_value=object()), patch.object(self.learn, 'upload', return_value={'result': 'success'}) as upload, patch.object(self.actions, 'send', side_effect=RuntimeError('offline')):
            with self.assertRaises(RuntimeError):
                self.actions.act(self.event)
            upload.assert_called_once()
        saved = self.c.read_json(self.c.task_path(self.st['task_id']))
        self.assertEqual(saved['status'], 'submitted')
        with patch.object(self.actions, 'send', return_value={'message_id': 'receipt'}), patch.object(self.learn, 'upload') as upload:
            self.actions.send_submission_receipt(saved)
            upload.assert_not_called()
            self.assertIn('receipt_message', saved)

    def test_prepare_uses_one_claude_and_never_submits(self):
        folder = self.root / 'input-files'
        folder.mkdir()
        (folder / 'question.txt').write_text('Compute 1 + 1')
        course = self.root / 'courseware'
        course.mkdir()
        st = dict(self.st, status='queued', folder=str(folder), courseware=str(course), description='Compute 1+1')
        stages = []
        def stage(job, name, prompt):
            stages.append(name)
            target = 'final'
            (job / target).mkdir()
            (job / target / 'answer.txt').write_text('2')
            if name == 'claude':
                (job / 'review.md').write_text('Independently checked: 1+1=2')
            return {'ready': True, 'summary': 'Checked', 'blockers': [], 'files': [target + '/answer.txt']}
        with patch.object(self.workflow, 'run_stage', side_effect=stage), patch.object(self.delivery, 'send', return_value={'message_id': 'sent'}), patch.object(self.learn, 'upload') as upload:
            self.workflow.process(st)
            upload.assert_not_called()
        self.assertEqual(stages, ['claude'])
        self.assertEqual(st['status'], 'awaiting')
        self.assertEqual(Path(st['submission']).read_text(), '2')

    def test_reviewer_cannot_return_unreviewed_draft(self):
        result = {'ready': True, 'summary': 'checked', 'blockers': [], 'files': ['draft/answer.txt']}
        with self.assertRaises(ValueError):
            self.workflow.validate_result(result, self.root, 'reviewer')

    def test_blocked_work_delivers_a_report_without_submission(self):
        job = self.root / 'blocked-job'
        job.mkdir()
        (job / 'review.md').write_text('Missing physical measurements')
        result = {'ready': False, 'summary': 'Need data', 'blockers': ['Missing measurements'], 'files': []}
        self.workflow.validate_result(result, job)
        st = dict(self.st)
        self.delivery.snapshot(st, result, job)
        self.assertIsNone(st['submission'])
        with patch.object(self.delivery, 'send', return_value={'message_id': 'sent'}), patch.object(self.delivery, 'lark', side_effect=RuntimeError('lookup unavailable')):
            self.delivery.deliver(st)
        self.assertEqual(st['status'], 'needs_student')

    def test_reply_message_replay_does_not_create_another_revision(self):
        event = {'sender_id': 'ou_owner', 'message_id': 'om_feedback', 'chat_id': 'oc_chat',
                 'content': '意见 ' + self.st['task_id'] + ' fix the answer'}
        with patch.object(self.messages, 'send'):
            self.assertTrue(self.messages.act(event))
            saved = self.c.read_json(self.c.task_path(self.st['task_id']))
            saved.update(status='awaiting', revision=2)
            self.c.save_task(saved)
            self.assertFalse(self.messages.act(event))

    def test_complete_claude_checkpoint_requires_unchanged_artifacts(self):
        job = self.root / 'checked'
        (job / 'final').mkdir(parents=True)
        (job / 'final/answer.txt').write_text('2')
        (job / 'review.md').write_text('Checked')
        result = {'ready': True, 'summary': 'done', 'blockers': [], 'files': ['final/answer.txt']}
        self.c.write_json(job / 'result.json', result)
        self.c.write_json(job / 'complete.json', {'hashes': {p: self.c.digest(job / p) for p in ['final/answer.txt', 'review.md']}})
        with patch('loop.workflow.subprocess.Popen') as popen:
            self.assertEqual(self.workflow.run_stage(job, 'claude', 'ignored'), result)
            popen.assert_not_called()
            (job / 'final/answer.txt').write_text('changed')
            with self.assertRaises(RuntimeError):
                self.workflow.run_stage(job, 'claude', 'ignored')

    def test_error_redacts_csrf(self):
        self.assertNotIn('private-value', self.c.safe_error('failed https://learn.tsinghua.edu.cn/x?_csrf=private-value&x=y'))

    def test_card_links_retry_never_resends_or_changes_approval(self):
        st = dict(self.st, review_doc={'verified': True, 'url': 'https://example.feishu.cn/docx/doc', 'image_key': 'old-cover'},
                  deliveries={'bundle': {'message_id': 'om_file', 'message_app_link': 'https://applink.feishu.cn/file'},
                              'review': {'message_id': 'om_review'}, 'card': {'message_id': 'om_card'}})
        with patch.object(self.delivery, 'lark', return_value={}) as lark, patch.object(self.delivery, 'send') as send:
            self.delivery.refresh_card_links(st)
            self.delivery.refresh_card_links(st)
            send.assert_not_called()
        self.assertEqual(lark.call_count, 1)
        self.assertTrue(st['links_synced'])
        self.assertEqual(st['nonce'], 'secret-version')
        self.assertEqual(st['status'], 'awaiting')
        self.assertIn('https://example.feishu.cn/docx/doc', lark.call_args.args[-1])
        self.assertNotIn('https://applink.feishu.cn/file', lark.call_args.args[-1])
        self.assertNotIn('old-cover', lark.call_args.args[-1])

    def test_presentation_metadata_requires_frozen_delivered_evidence(self):
        job = self.root / 'job'
        (job / 'final').mkdir(parents=True)
        (job / 'final/answer.txt').write_text('Answer')
        (job / 'review.md').write_text('Self check')
        result = {'ready': True, 'summary': 'Done', 'blockers': [], 'files': ['final/answer.txt'],
                  'presentation': {'assignment': 'Question', 'checks': ['Checked'], 'highlights': []}}
        self.assertEqual(self.workflow.validate_result(result, job), result)
        self.delivery.snapshot(self.st, result, job)
        self.assertEqual(self.st['presentation'], result['presentation'])
        self.assertEqual(self.st['report_sha256'], self.c.digest(Path(self.st['report'])))
        result['presentation']['highlights'] = [{'title': 'Evidence', 'detail': 'Look here',
                                                'artifact': '../secret.png', 'member': ''}]
        with self.assertRaises(ValueError):
            self.workflow.validate_result(result, job)

    def test_lark_uses_structured_stderr_errors(self):
        fake = Mock(returncode=1, stdout='', stderr=json.dumps({'ok': False, 'error': {'message': 'missing permission'}}))
        with patch('loop.common.subprocess.run', return_value=fake):
            with self.assertRaisesRegex(RuntimeError, 'missing permission'):
                self.c.lark('im')

    def test_document_feedback_checks_owner_version_and_invalidates_submit(self):
        from loop import review
        st = dict(self.st, review_doc={'verified': True, 'url': 'https://example.feishu.cn/docx/doc'})
        self.c.save_task(st)
        event = dict(self.event, action_value=json.dumps({'task_id': st['task_id'], 'revision': 1, 'nonce': st['nonce'], 'action': 'revise_comments'}))
        with patch.object(review, 'comments', return_value=[{'quote': 'Equation 1', 'text': 'Show the derivation'}]) as comments, patch.object(self.actions, 'send'):
            self.assertFalse(self.actions.act(dict(event, operator_id='stranger')))
            self.assertFalse(self.actions.act(dict(event, message_id='old-card')))
            comments.assert_not_called()
            self.assertTrue(self.actions.act(event))
            self.assertFalse(self.actions.act(event))
            comments.assert_called_once()
        saved = self.c.read_json(self.c.task_path(st['task_id']))
        self.assertEqual(saved['status'], 'revision_ready')
        self.assertIn('Equation 1', saved['feedback'])
        with patch.object(self.learn, 'upload') as upload:
            self.assertFalse(self.actions.act(self.event))
            upload.assert_not_called()

if __name__ == '__main__':
    unittest.main()
