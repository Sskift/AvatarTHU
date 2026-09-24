"""Course polling (12 hours by default) and prompt processing of local revisions."""
import argparse
import sys
import traceback
from datetime import datetime, timedelta
from pathlib import Path
from .common import DATA, config, fingerprint, lark_enabled, lock, now, notify_once, read_json, safe_error, save_task, task_path, tasks, write_json
from . import learn
from .delivery import refresh_card_links, send_notices
from .workflow import materials_hash, process
from .settings import next_scan


def sync():
    c = learn.client()
    notices = send_notices(learn.announcements(c), lambda note: learn.mark_notice_read(c, note))
    pending = learn.active_assignments(c)
    active = set()
    for meta in pending:
        tid = fingerprint([meta['semester'], meta['course_id'], meta['xszyid']])[:16]
        cached = next((s for s in tasks() if s.get('source_cached') and s.get('xszyid') == meta['xszyid']), None)
        if cached:
            tid = cached['task_id']
        active.add(tid)
        with lock(tid):
            st = read_json(task_path(tid))
            source_hash = materials_hash(meta)
            if not st:
                st = dict(meta, task_id=tid, status='queued', revision=1, source_hash=source_hash)
            elif st['status'] not in {'submitted', 'submitting', 'submission_unknown'}:
                if st.get('source_hash') != source_hash or st.get('source_cached'):
                    st.update(status='revision_ready', feedback='课程材料或本地输入材料有更新，请重新核对。')
                st.update(meta, source_hash=source_hash, source_cached=False)
            save_task(st)
    for old in tasks():
        if old['task_id'] not in active and old.get('status') in {'queued', 'awaiting', 'needs_student', 'revision_ready', 'working', 'reviewing', 'rewriting', 'failed', 'approval_invalid', 'delivery_pending'}:
            with lock(old['task_id']):
                st = read_json(task_path(old['task_id']))
                st['status'] = 'closed_remote'
                save_task(st)
    stats = {'time': now().isoformat(), 'pending': len(pending), 'notices_sent': notices}
    write_json(DATA / 'sync-result.json', stats)
    print(f'Sync complete: {len(pending)} pending assignments, {notices} new/updated notices', flush=True)


def run(*, tick=False, sync_only=False, selected=None):
    DATA.mkdir(parents=True, exist_ok=True)
    try:
        with lock('daily', blocking=False):
            schedule = read_json(DATA / 'schedule.json')
            due = now() >= next_scan(schedule, config(), now())
            sync_failed = False
            if not tick or due:
                try:
                    sync()
                    write_json(DATA / 'schedule.json', {'last_sync_at': now().isoformat()})
                except Exception as exc:
                    sync_failed = True
                    record_sync_error(exc)
            if sync_only:
                return not sync_failed
            for old in tasks():
                if selected and old['task_id'] != selected:
                    continue
                if old.get('status') in {'awaiting', 'needs_student'} and ((lark_enabled() and (not old.get('links_synced') or not old.get('review_doc', {}).get('verified')))
                        or (not lark_enabled() and not old.get('local_review'))):
                    with lock(old['task_id']):
                        try:
                            refresh_card_links(read_json(task_path(old['task_id'])))
                        except Exception as exc:
                            print('附件链接稍后重试: ' + safe_error(exc), file=sys.stderr)
                    continue
                if lark_enabled() and old.get('status') == 'submitted' and not old.get('receipt_message'):
                    from .actions import send_submission_receipt
                    with lock(old['task_id']):
                        send_submission_receipt(read_json(task_path(old['task_id'])))
                    continue
                if old.get('status') not in {'queued', 'revision_ready', 'working', 'reviewing', 'rewriting', 'delivery_pending', 'failed'}:
                    continue
                if old.get('status') == 'failed' and tick and old.get('retry_at') and now() < datetime.fromisoformat(old['retry_at']):
                    continue
                with lock(old['task_id']):
                    st = read_json(task_path(old['task_id']))
                    try:
                        if st.get('status') == 'failed':
                            st['status'] = st.get('resume_status', 'queued')
                        process(st)
                    except Exception as exc:
                        st['resume_status'] = st.get('status', 'queued')
                        st.update(status='failed', error=safe_error(exc), retry_at=(now() + timedelta(minutes=15)).isoformat())
                        save_task(st)
                        print(safe_error(traceback.format_exc()), file=sys.stderr)
                        notify_once('task-error:' + st['task_id'] + ':' + str(st.get('revision', 1)) + ':' + fingerprint(str(exc)),
                                    f'本地作业处理需要检查：{st["title"]}\n{safe_error(exc)[:1000]}\n修复后运行 ./run-now.sh 会从上次成功阶段继续。')
            write_json(DATA / 'run-result.json', {'completed_at': now().isoformat(), 'tasks': len(tasks())})
            return not sync_failed
    except BlockingIOError:
        print('A run is already active; no duplicate run started.', flush=True)


def record_sync_error(exc):
    print(safe_error(traceback.format_exc()), file=sys.stderr)
    schedule = read_json(DATA / 'schedule.json')
    schedule['next_sync_retry_at'] = (now() + timedelta(minutes=15)).isoformat()
    write_json(DATA / 'schedule.json', schedule)
    try:
        notify_once('sync-error:' + now().date().isoformat() + ':' + fingerprint(str(exc)),
                    '网络学堂同步失败：' + safe_error(exc)[:1200] + '\n如登录过期，请运行 ./avatarthu login thu。已缓存的作业仍会继续处理。')
    except Exception:
        print(safe_error(traceback.format_exc()), file=sys.stderr)


def main(argv=None):
    p = argparse.ArgumentParser()
    p.add_argument('--tick', action='store_true')
    p.add_argument('--sync-only', action='store_true')
    p.add_argument('--task')
    args = p.parse_args(argv)
    try:
        success = run(tick=args.tick, sync_only=args.sync_only, selected=args.task)
    except Exception as exc:
        record_sync_error(exc)
        raise SystemExit(1)
    if success is False:
        raise SystemExit(1)

if __name__ == '__main__':
    main()
