"""Feishu artifacts and version-bound approval cards (Card 2.0)."""
import json
import secrets
import shutil
import zipfile
from datetime import datetime, timedelta
from pathlib import Path
from .common import DATA, OUT, digest, fingerprint, inside, lark, lark_enabled, lock, now, read_json, save_task, send, write_json


from .cards import assignment as card, notice_digest
from .review import publish


def snapshot(st, result, job):
    # A new output folder per revision prevents failed runs from reusing stale answers.
    target = OUT / st['task_id'] / f'r{st["revision"]}'
    target.mkdir(parents=True, exist_ok=True)
    artifacts = []
    names = set()
    for relative in result['files']:
        src = inside(job, relative)
        if src.name in {'review.md', 'submission.zip'}:
            raise ValueError('Artifact filename is reserved for review/packaging')
        if src.name in names:
            raise ValueError('Artifact basenames collide')
        names.add(src.name)
        dst = target / src.name
        shutil.copy2(src, dst)
        artifacts.append({'path': str(dst), 'sha256': digest(dst)})
    if not artifacts and result['ready']:
        raise ValueError('No reviewed artifacts')
    report = inside(job, 'review.md')
    shutil.copy2(report, target / 'review.md')
    files = [Path(a['path']) for a in artifacts]
    if not files:
        submit_file = None
    elif len(files) == 1:
        submit_file = files[0]
    else:
        submit_file = target / 'submission.zip'
        with zipfile.ZipFile(submit_file, 'w', zipfile.ZIP_DEFLATED) as z:
            for p in files:
                z.write(p, p.name)
    st.update(ready=result['ready'], summary=result['summary'], blockers=result['blockers'],
              artifacts=artifacts, report=str(target / 'review.md'),
              report_sha256=digest(target / 'review.md'), presentation=result.get('presentation', {}),
              submission=str(submit_file) if submit_file else None,
              sha256=digest(submit_file) if submit_file else None, nonce=secrets.token_hex(16),
              deliveries={}, review_doc={}, links_synced=False, links_retry_at=None, status='delivery_pending')
    for key in ('card_message_id', 'chat_id', 'local_review', 'delivery_mode'):
        st.pop(key, None)
    save_task(st)


def deliver(st):
    if not lark_enabled():
        from .local_review import publish as local_publish
        local_publish(st)
        st.update(status='awaiting' if st['ready'] else 'needs_student', delivery_mode='local')
        save_task(st)
        print('本地审阅：' + st['local_review'], flush=True)
        return
    # The document owns all artifacts; only the approval card goes to the chat.
    publish(st)
    if 'card' not in st['deliveries']:
        response = send(content=card(st), idem=f'{st["task_id"]}:{st["revision"]}:card')
        st['deliveries']['card'] = response
        st['card_message_id'] = response['message_id']
        st['chat_id'] = response.get('chat_id')
    st['status'] = 'awaiting' if st['ready'] else 'needs_student'
    st['delivery_mode'] = 'lark'
    save_task(st)


def refresh_card_links(st):
    """Finish document delivery and refresh the existing approval card in place."""
    if not lark_enabled():
        if not st.get('local_review') or not Path(st['local_review']).is_file():
            from .local_review import publish as local_publish
            local_publish(st)
        return
    if st.get('status') not in {'awaiting', 'needs_student'} or (st.get('links_synced') and st.get('review_doc', {}).get('verified')):
        return
    if st.get('links_retry_at') and now() < datetime.fromisoformat(st['links_retry_at']):
        return
    st['links_retry_at'] = (now() + timedelta(minutes=15)).isoformat()
    save_task(st)
    if not st.get('card_message_id'):
        deliver(st)
        return
    publish(st)
    lark('im', 'messages', 'patch', '--as', 'bot', '--message-id', st['card_message_id'],
         '--data', json.dumps({'content': json.dumps(card(st), ensure_ascii=False)}, ensure_ascii=False))
    st['links_synced'] = True
    save_task(st)


def send_notices(notices, mark_read):
    """Commit the delivery receipt before marking read; resume only the failed step."""
    if not lark_enabled():
        # learn.announcements has already archived the original notice locally.
        # No Feishu receipt means no permission to mark it read, even on retries.
        return 0
    state_path = DATA / 'notices.json'
    acknowledged = read_json(state_path)
    count = 0
    for note in notices:
        if not note.get('unread'):
            continue
        key = note['course_id'] + ':' + note['id']
        version = fingerprint({k: note.get(k) for k in ('course_id', 'id', 'title', 'body', 'date', 'attachment_name')})
        previous = acknowledged.get(key, {})
        if previous.get('version') != version or not previous.get('message'):
            response = send(content=notice_digest([note], now().strftime('%m 月 %d 日')), idem='notice:' + version)
            previous = {'version': version, 'message': response, 'delivered_at': now().isoformat()}
            acknowledged[key] = previous
            write_json(state_path, acknowledged)
            count += 1
        mark_read(note)
        previous['read_at'] = now().isoformat()
        write_json(state_path, acknowledged)
    return count
