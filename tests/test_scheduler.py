import importlib
import os
import tempfile
import unittest
from datetime import datetime
from unittest.mock import patch, Mock
from pathlib import Path


class SchedulerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.old = os.environ.get('AVATARTHU_HOME')
        os.environ['AVATARTHU_HOME'] = self.temp.name
        from loop import common, learn, delivery, workflow, daily
        self.c = importlib.reload(common)
        importlib.reload(learn)
        importlib.reload(delivery)
        importlib.reload(workflow)
        self.d = importlib.reload(daily)
        self.c.write_json(Path(self.temp.name)/'config.json', {'poll_interval_seconds': 12 * 3600})
        self.c.write_json(self.c.DATA / 'schedule.json', {'last_sync_at': '2026-09-24T00:00:00+08:00'})

    def tearDown(self):
        if self.old is None:
            os.environ.pop('AVATARTHU_HOME', None)
        else:
            os.environ['AVATARTHU_HOME'] = self.old
        self.temp.cleanup()

    def test_catch_up_once_after_scheduled_time(self):
        with patch.object(self.d, 'sync') as sync, patch.object(self.d, 'now', return_value=datetime(2026,9,24,7,59,tzinfo=self.c.TZ)):
            self.d.run(tick=True)
            sync.assert_not_called()
        with patch.object(self.d, 'sync') as sync, patch.object(self.d, 'now', return_value=datetime(2026,9,24,12,0,tzinfo=self.c.TZ)):
            self.d.run(tick=True)
            self.d.run(tick=True)
            sync.assert_called_once()

    def test_sync_failure_does_not_close_existing_tasks(self):
        st = {'task_id': '0123456789abcdef', 'status': 'awaiting', 'revision': 1}
        self.c.save_task(st)
        with patch.object(self.d.learn,'client'), patch.object(self.d.learn,'announcements',return_value=[]), patch.object(self.d.learn,'active_assignments',side_effect=RuntimeError('course failure')):
            with self.assertRaises(RuntimeError):
                self.d.sync()
        self.assertEqual(self.c.read_json(self.c.task_path(st['task_id']))['status'], 'awaiting')

    def test_revision_queue_runs_before_daily_sync_time(self):
        st = {'task_id': '0123456789abcdef', 'status': 'revision_ready', 'revision': 1}
        self.c.save_task(st)
        with patch.object(self.d,'sync') as sync, patch.object(self.d,'process') as process, patch.object(self.d,'now',return_value=datetime(2026,9,24,7,0,tzinfo=self.c.TZ)):
            self.d.run(tick=True)
            sync.assert_not_called()
            process.assert_called_once()

    def test_failed_extra_sync_retries_even_after_success_that_day(self):
        self.c.write_json(self.c.DATA / 'schedule.json', {'last_sync_at': '2026-09-24T08:00:00+08:00', 'next_sync_retry_at': '2026-09-24T09:15:00+08:00'})
        with patch.object(self.d, 'sync') as sync, patch.object(self.d, 'now', return_value=datetime(2026,9,24,9,16,tzinfo=self.c.TZ)):
            self.d.run(tick=True)
            self.d.run(tick=True)
            sync.assert_called_once()

    def test_wake_after_multiple_intervals_scans_once_and_uses_changed_interval(self):
        with patch.object(self.d, 'sync') as sync, patch.object(self.d, 'now', return_value=datetime(2026,9,27,9,0,tzinfo=self.c.TZ)):
            self.d.run(tick=True)
            self.d.run(tick=True)
            sync.assert_called_once()
        self.c.write_json(self.c.CONFIG, {'poll_interval_seconds': 1800})
        with patch.object(self.d, 'sync') as sync, patch.object(self.d, 'now', return_value=datetime(2026,9,27,9,30,tzinfo=self.c.TZ)):
            self.d.run(tick=True)
            sync.assert_called_once()

    def test_cached_rewrite_continues_when_school_login_expired(self):
        self.c.save_task({'task_id': '0123456789abcdef', 'status': 'rewriting', 'revision': 1})
        with patch.object(self.d, 'sync', side_effect=RuntimeError('login expired')), patch.object(self.d, 'process') as process, patch.object(self.d, 'notify_once'), patch.object(self.d, 'now', return_value=datetime(2026,9,24,12,0,tzinfo=self.c.TZ)):
            self.assertFalse(self.d.run(tick=True))
            process.assert_called_once()
        schedule = self.c.read_json(self.c.DATA / 'schedule.json')
        self.assertEqual(schedule['last_sync_at'], '2026-09-24T00:00:00+08:00')
        self.assertEqual(schedule['next_sync_retry_at'], '2026-09-24T12:15:00+08:00')

    def test_configure_persists_mode_interval_and_unlimited_rounds(self):
        from argparse import Namespace
        from loop import settings
        with patch.object(settings, 'show'), patch.object(settings.shutil, 'which', return_value=None):
            settings.configure(Namespace(mode='codex-claude', poll_interval='30m', max_review_rounds=0))
        cfg = self.c.config()
        self.assertEqual(cfg['poll_interval_seconds'], 1800)
        self.assertEqual(settings.pairing(cfg)['writer'], 'codex')
        self.assertEqual(settings.pairing(cfg)['max_review_rounds'], 0)
        for invalid in ('0', '-1h', 'word'):
            with self.assertRaises(ValueError):
                settings.duration(invalid)


if __name__ == '__main__':
    unittest.main()
