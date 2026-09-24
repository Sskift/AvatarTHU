import importlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


class IntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        env = patch.dict(os.environ, AVATARTHU_HOME=self.temp.name)
        env.start()
        self.addCleanup(env.stop)
        from loop import common, auth, services, delivery, local_review, workflow, actions, messages, daily, learn
        self.c = importlib.reload(common)
        self.auth = importlib.reload(auth)
        self.services = importlib.reload(services)
        self.delivery = importlib.reload(delivery)
        self.local = importlib.reload(local_review)
        self.workflow = importlib.reload(workflow)
        self.actions = importlib.reload(actions)
        self.messages = importlib.reload(messages)
        self.daily = importlib.reload(daily)
        self.learn = importlib.reload(learn)
        self.root = Path(self.temp.name).resolve()
        self.c.write_json(self.c.CONFIG, {'lark_enabled': False, 'claude_cli': '/fake/claude'})

    def frozen(self, ready=True):
        job = self.root / 'job'
        (job / 'final').mkdir(parents=True)
        (job / 'final/答案.txt').write_text('1 + 1 = 2')
        (job / 'review.md').write_text('Checked by the same worker')
        st = {'task_id': '0123456789abcdef', 'revision': 1, 'title': '本地 <script>alert(1)</script>',
              'course': 'Course', 'deadline': '2099-01-01 12:00', 'description': 'Calculate 1+1'}
        result = {'ready': ready, 'summary': '1. 已计算\n2. 已自查', 'blockers': [] if ready else ['待测量'],
                  'files': ['final/答案.txt'] if ready else [],
                  'presentation': {'checks': ['1+1=2'], 'highlights': []}}
        self.delivery.snapshot(st, result, job)
        return st

    def test_local_attempt_needs_no_lark_and_never_uploads(self):
        source = self.root / 'source'
        source.mkdir()
        (source / 'question.txt').write_text('1+1?')
        courseware = self.root / 'courseware'
        courseware.mkdir()
        st = {'task_id': '0123456789abcdef', 'revision': 1, 'status': 'queued', 'title': '题目', 'course': 'Course',
              'deadline': '2099-01-01 12:00', 'folder': str(source), 'courseware': str(courseware), 'description': '1+1?'}
        def solve(job, stage, prompt):
            (job / 'final').mkdir()
            (job / 'final/answer.txt').write_text('2')
            (job / 'review.md').write_text('Checked 1+1=2')
            return {'ready': True, 'summary': 'Done', 'blockers': [], 'files': ['final/answer.txt']}
        with patch.object(self.workflow, 'run_stage', side_effect=solve) as stage, patch.object(self.delivery, 'publish') as cloud, patch.object(self.delivery, 'send') as send, patch.object(self.learn, 'upload') as upload:
            self.workflow.process(st)
            stage.assert_called_once()
            cloud.assert_not_called()
            send.assert_not_called()
            upload.assert_not_called()
        self.assertEqual(st['status'], 'awaiting')
        self.assertEqual(st['delivery_mode'], 'local')
        page = Path(st['local_review']).read_text()
        self.assertIn('一、作业描述', page)
        self.assertIn('三、完整产物', page)
        self.assertIn('answer.txt', page)
        self.assertIn('./avatarthu revise', page)

    def test_local_report_escapes_content_and_verifies_frozen_files(self):
        st = self.frozen()
        self.delivery.deliver(st)
        page = Path(st['local_review']).read_text()
        self.assertNotIn('<script>', page)
        self.assertIn('&lt;script&gt;', page)
        self.assertIn('Content-Security-Policy', page)
        self.assertIn('file://', page)
        Path(st['submission']).write_text('Changed')
        with self.assertRaisesRegex(ValueError, '冻结'):
            self.local.publish(st)

    def test_blocked_local_work_has_review_without_submit_file(self):
        st = self.frozen(ready=False)
        self.delivery.deliver(st)
        self.assertEqual(st['status'], 'needs_student')
        self.assertIsNone(st['submission'])
        self.assertIn('待测量', Path(st['local_review']).read_text())

    def test_disabled_notices_stay_unread_even_with_old_receipt(self):
        self.c.write_json(self.c.DATA / 'notices.json', {'cid:n': {'message': {'message_id': 'old'}}})
        mark = Mock()
        with patch.object(self.delivery, 'send') as send:
            self.assertEqual(self.delivery.send_notices([{'unread': True, 'course_id': 'cid', 'id': 'n'}], mark), 0)
            send.assert_not_called()
        mark.assert_not_called()

    def test_disabled_callbacks_and_local_errors_need_no_lark_keys(self):
        with patch.object(self.learn, 'upload') as upload, patch.object(self.c, 'send') as send:
            self.assertFalse(self.actions.act({'operator_id': 'owner', 'event_id': 'e'}))
            self.assertFalse(self.messages.act({'sender_id': 'owner', 'message_id': 'm'}))
            self.c.notify_once('error', 'Network unavailable')
            self.c.notify_once('error', 'Network unavailable')
            send.assert_not_called()
            upload.assert_not_called()
        self.assertEqual(self.c.read_json(self.c.DATA / 'notifications.json')['error']['channel'], 'local')

    def test_local_to_lark_delivers_same_revision_without_rerunning_worker(self):
        st = self.frozen()
        self.delivery.deliver(st)
        frozen = st['nonce'], st['sha256'], st['revision']
        self.c.write_json(self.c.CONFIG, {'lark_enabled': True, 'lark_user_id': 'owner'})
        with patch.object(self.delivery, 'publish') as cloud, patch.object(self.delivery, 'send', return_value={'message_id': 'new-card'}) as send, patch.object(self.workflow, 'run_stage') as stage:
            self.delivery.refresh_card_links(st)
            self.delivery.refresh_card_links(st)
            cloud.assert_called_once()
            send.assert_called_once()
            stage.assert_not_called()
        self.assertEqual((st['nonce'], st['sha256'], st['revision']), frozen)
        self.assertEqual(st['card_message_id'], 'new-card')

    def test_local_revision_invalidates_card_and_clears_it_for_new_output(self):
        st = self.frozen()
        self.delivery.deliver(st)
        st['card_message_id'] = 'old-card'
        self.c.save_task(st)
        self.local.revise(st['task_id'], '补充推导')
        current = self.c.read_json(self.c.task_path(st['task_id']))
        self.assertEqual(current['status'], 'revision_ready')
        self.assertEqual(current['feedback'], '补充推导')
        current['revision'] = 2
        self.delivery.snapshot(current, {'ready': True, 'summary': 'Done', 'blockers': [], 'files': ['final/答案.txt']}, self.root / 'job')
        self.assertNotIn('card_message_id', current)
        self.assertNotIn('local_review', current)

    def test_legacy_lark_enabled_and_local_services_are_independent(self):
        self.assertFalse(self.c.lark_enabled({}))
        self.assertTrue(self.c.lark_enabled({'lark_user_id': 'owner'}))
        self.assertFalse(self.c.lark_enabled({'lark_enabled': False, 'lark_user_id': 'owner'}))
        self.assertEqual(self.services.enabled_names({}), ('daily', 'keepalive'))
        for name in ('daily', 'keepalive'):
            self.assertNotIn('autothu', json.dumps(self.services.spec(name)['ProgramArguments']).lower())
        self.assertEqual(self.services.spec('keepalive')['StartInterval'], 900)

    def test_installer_migrates_session_once_and_preserves_mode(self):
        from scripts import install
        install = importlib.reload(install)
        old = self.root / 'legacy-autothu-session.json'
        old.write_text('{"cookies": {"test": "old"}}')
        cfg = install.prepare_config({'session': str(old), 'lark_user_id': 'owner'}, claude='/fake/claude')
        self.assertTrue(cfg['lark_enabled'])
        self.assertEqual(cfg['migrated_autothu_session'], str(old))
        target = Path(cfg['session'])
        self.assertEqual(target, self.root / 'session.json')
        self.assertEqual(target.stat().st_mode & 0o777, 0o600)
        target.write_text('new session')
        cfg = install.prepare_config(cfg, lark=False)
        self.assertEqual(target.read_text(), 'new session')
        self.assertFalse(cfg['lark_enabled'])
        self.assertFalse(install.prepare_config({})['lark_enabled'])

    def test_no_session_login_uses_built_in_browser_helper(self):
        from loop.thulearn import login
        target = self.root / 'session.json'
        with patch.object(login, 'import_chrome_cookies', side_effect=RuntimeError('expired')), patch.object(login, 'browser_login_cookies', return_value={'fake': 'session'}) as browser, patch.object(login, 'save_session') as save:
            login.login(target)
            browser.assert_called_once()
            save.assert_called_once_with(target, {'fake': 'session'})

    def test_local_installer_does_not_discover_or_invoke_lark(self):
        from scripts import install
        install = importlib.reload(install)
        source = self.root / 'source'
        source.mkdir()
        (source / 'loop').mkdir()
        (source / 'third_party').mkdir()
        for name in ('requirements.txt', 'THIRD_PARTY.md', 'avatarthu'):
            (source / name).write_text('test fixture')
        python = self.root / '.venv/bin/python'
        python.parent.mkdir(parents=True)
        python.touch()
        (self.root / 'requirements-installed.txt').write_text('test fixture')
        claude = self.root / 'claude'
        claude.touch()
        with patch.object(install, 'SOURCE', source), patch.object(install.shutil, 'which', return_value=str(claude)) as which, patch.object(install.subprocess, 'run') as run, patch.object(install.subprocess, 'check_output', return_value='revision'):
            install.main(['--no-lark', '--no-start', '--skip-login'])
        which.assert_called_once_with('claude')
        self.assertEqual(run.call_count, 1)
        self.assertIn('ensure_thu(skip_login=True)', run.call_args.args[0][-1])
        self.assertFalse(self.c.lark_enabled())
        self.assertTrue((self.root / 'bin/avatarthu').exists())

    def test_school_client_import_has_no_checkout_dependency(self):
        from loop.thulearn.client import ThuLearnClient
        self.c.write_json(self.c.CONFIG, {'session': str(self.root / 'session.json')})
        with patch.object(ThuLearnClient, 'from_session_file') as client:
            self.learn.client()
            client.assert_called_once_with(str(self.root / 'session.json'))

    def test_school_login_saves_rotated_cookies_and_retains_old_session_on_failure(self):
        from loop.thulearn import client, login
        target = self.root / 'session.json'
        target.write_text('old session')
        with patch.object(client.ThuLearnClient, 'ping', return_value=False):
            with self.assertRaises(RuntimeError):
                login.save_session(target, {'JSESSIONID': 'input', 'XSRF-TOKEN': 'csrf'})
        self.assertEqual(target.read_text(), 'old session')
        def accept(probe):
            probe.session.cookies.set('JSESSIONID', 'rotated', domain='learn.tsinghua.edu.cn', path='/')
            return True
        with patch.object(client.ThuLearnClient, 'ping', accept):
            login.save_session(target, {'JSESSIONID': 'input', 'XSRF-TOKEN': 'csrf'})
        loaded = client.ThuLearnClient.from_session_file(target)
        self.assertEqual(loaded.session.cookies.get('JSESSIONID'), 'rotated')
        self.assertEqual(target.stat().st_mode & 0o777, 0o600)

    def test_consumer_shutdown_signals_the_cli_process_group(self):
        import signal
        from loop import events
        events = importlib.reload(events)
        self.c.write_json(self.c.CONFIG, {'lark_enabled': True, 'lark_cli': '/fake/lark'})
        proc = Mock(pid=12345, stdout=[], stderr=[], returncode=0)
        with patch.object(events.subprocess, 'Popen', return_value=proc) as popen, patch.object(events.signal, 'signal'), patch.object(events.os, 'killpg') as kill, self.assertRaises(SystemExit):
            events.consume('card.action.trigger', Mock(), 'actions')
        self.assertTrue(popen.call_args.kwargs['start_new_session'])
        kill.assert_called_once_with(proc.pid, signal.SIGTERM)
        proc.wait.assert_called_once_with(timeout=15)

    def auth_flow(self, responses):
        finder = patch.object(self.auth, 'find_lark', return_value='/fake/lark')
        finder.start()
        self.addCleanup(finder.stop)
        sequence = patch.object(self.auth, 'cli_json', side_effect=responses)
        sequence.start()
        self.addCleanup(sequence.stop)

    def test_existing_lark_session_is_verified_without_browser_login(self):
        self.auth_flow([{'appId': 'app', 'identities': {'user': {'available': True}}}, {'onBehalfOf': {'openId': 'owner'}}, {}, {}])
        with patch.object(self.auth.subprocess, 'run') as run:
            self.auth.login_lark(start=False)
            run.assert_not_called()
        self.assertTrue(self.c.lark_enabled())

    def test_missing_lark_session_runs_login_only_after_opt_in(self):
        self.auth_flow([{'appId': 'app', 'identities': {'user': {'available': False}}}, {'onBehalfOf': {'openId': 'owner'}}, {}, {}])
        with patch.object(self.auth.subprocess, 'run') as run:
            self.auth.login_lark(start=False)
            run.assert_called_once_with(['/fake/lark', 'auth', 'login', '--domain', 'docs,drive,im,event'], check=True)
        self.assertEqual(self.c.config()['lark_user_id'], 'owner')

    def test_unconfigured_lark_creates_app_then_authenticates(self):
        missing = self.auth.LarkError({'error': {'type': 'config', 'subtype': 'not_configured'}})
        self.auth_flow([missing, {'appId': 'new-app'}, {'onBehalfOf': {'openId': 'owner'}}, {}, {}])
        with patch.object(self.auth.subprocess, 'run') as run:
            self.auth.login_lark(start=False)
        self.assertEqual(run.call_args_list[0].args[0], ['/fake/lark', 'config', 'init', '--new'])
        self.assertEqual(run.call_args_list[1].args[0][1:3], ['auth', 'login'])
        self.assertTrue(self.c.lark_enabled())

    def test_lark_network_or_permission_failure_does_not_enable_or_create_app(self):
        self.auth_flow([self.auth.LarkError({'error': {'type': 'network'}})])
        with patch.object(self.auth.subprocess, 'run') as run, self.assertRaises(RuntimeError):
            self.auth.login_lark(start=False)
        run.assert_not_called()
        self.assertFalse(self.c.lark_enabled())

    def test_expired_lark_auth_is_renewed_once_and_permissions_checked(self):
        expired = self.auth.LarkError({'error': {'type': 'auth', 'subtype': 'token_expired'}})
        self.auth_flow([{'appId': 'app', 'identities': {'user': {'available': True}}}, expired, {'onBehalfOf': {'openId': 'owner'}}, {}, self.auth.LarkError({'error': {'type': 'permission'}})])
        with patch.object(self.auth.subprocess, 'run') as run, self.assertRaises(RuntimeError):
            self.auth.login_lark(start=False)
        run.assert_called_once()
        self.assertFalse(self.c.lark_enabled())


if __name__ == '__main__':
    unittest.main()
