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
        self.c.write_json(Path(self.temp.name)/'config.json', {'daily_time': '08:00'})

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


if __name__ == '__main__':
    unittest.main()
