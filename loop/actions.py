"""Only this module may upload; an authenticated, current card click is required."""
import hmac
import json
from pathlib import Path
from .common import DATA, config, digest, inside, lock, now, read_json, safe_error, save_task, send, task_path, tasks, write_json
from . import learn
from .cards import receipt


def verified_files(st):
    from .common import OUT
    base = OUT / st['task_id'] / f'r{st["revision"]}'
    for item in [*st['artifacts'], {'path': st['submission'], 'sha256': st['sha256']}]:
        raw = Path(item['path'])
        file = inside(base, str(raw.relative_to(base)))
        if not hmac.compare_digest(digest(file), item['sha256']):
            raise ValueError('产物已发生变化，请重新检查后使用新卡片。')
    return Path(st['submission'])


def send_submission_receipt(st):
    if st.get('status') != 'submitted' or st.get('receipt_message'):
        return
    st['receipt_message'] = send(content=receipt('作业提交成功', st['title'], '网络学堂已返回成功回执。'),
                                 idem='submitted:' + st['task_id'] + ':' + str(st['revision']))
    save_task(st)


def act(event):
    if event.get('operator_id') != config()['lark_user_id'] or not event.get('event_id'):
        return False
    value = event.get('action_value') or '{}'
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except ValueError:
            return False
    if not isinstance(value, dict):
        return False
    if value.get('action') == 'diagnostic':
        with lock('diagnostic'):
            path = DATA / 'diagnostic.json'
            state = read_json(path)
            if (not state.get('message_id') or event.get('message_id') != state['message_id']
                    or value.get('nonce') != state.get('nonce') or state.get('confirmed_at')):
                return False
            state['confirmed_at'] = now().isoformat()
            state['event_id'] = event['event_id']
            write_json(path, state)
            send(content=receipt('按钮连接正常', '飞书已连接到本机', '你的按钮操作已收到。作业产物准备好后，会在对应卡片上等待你的决定。'))
            return True
    # Card 2.0 form callbacks carry the button name and form fields, not a value.
    if event.get('action_name') == 'revise':
        matches = [s for s in tasks() if s.get('card_message_id') == event.get('message_id')]
        if len(matches) != 1:
            return False
        tid, action = matches[0]['task_id'], 'revise'
    else:
        tid, action = value.get('task_id'), value.get('action')
    if action not in {'submit', 'revise', 'revise_comments'}:
        return False
    try:
        path = task_path(tid)
    except ValueError:
        return False
    with lock(tid):
        st = read_json(path)
        if st.get('status') not in {'awaiting', 'needs_student', 'approval_invalid'}:
            return False
        if not event.get('message_id') or event['message_id'] != st.get('card_message_id'):
            return False
        if action == 'revise_comments':
            if (value.get('revision') != st['revision'] or not hmac.compare_digest(str(value.get('nonce', '')), st['nonce'])
                    or not st.get('review_doc', {}).get('verified')):
                return False
            from .review import comments
            entries = comments(st)
            if not entries:
                send(content=receipt('尚未发现可处理的批注', st['title'], '请先在审阅文档中添加文字批注，再点“按文档批注修改”。也可以在卡片内直接填写意见。'),
                     idem='no-comments:' + event['event_id'])
                return False
            feedback = '\n\n'.join(f'位置与上下文：{e.get("context") or "以引用原文和意见定位"}\n引用：{e["quote"]}\n修改意见：{e["text"]}' for e in entries)
            st.update(status='revision_ready', feedback=feedback, approval_event=None, review_comments=entries)
            save_task(st)
            send(content=receipt('已收到文档批注', st['title'], f'已收集 {len(entries)} 条意见，下一版会逐项处理。'), idem='comments:' + event['event_id'])
            return True
        if action == 'revise':
            form = event.get('form_value') or '{}'
            if isinstance(form, str):
                form = json.loads(form)
            feedback = form.get('feedback', '').strip()
            if not feedback:
                return False
            st.update(status='revision_ready', feedback=feedback, approval_event=None)
            save_task(st)
            send(content=receipt('已收到修改意见', st['title'], '已排入本地处理队列，完成后发送新版审阅卡片；产物和自查记录均在云文档中。'),
                 idem='feedback:' + event['event_id'])
            return True
        if (not st.get('ready') or st['status'] != 'awaiting' or st.get('blockers')
                or value.get('revision') != st['revision']
                or not hmac.compare_digest(str(value.get('nonce', '')), st['nonce'])):
            return False
        try:
            file = verified_files(st)
            c = learn.verify_open(st)
        except Exception as exc:
            st['status'] = 'approval_invalid'
            st['error'] = safe_error(exc)
            save_task(st)
            send(content=receipt('未提交作业', st['title'], safe_error(exc), success=False))
            return False
        # Persist the claim before touching the remote submission endpoint.
        # A crash/timeout after this point requires reconciliation, never an automatic retry.
        st.update(status='submitting', approval_event=event['event_id'], approved_at=now().isoformat())
        save_task(st)
        try:
            result = learn.upload(c, st, file)
        except Exception as exc:
            st.update(status='submission_unknown', error=safe_error(exc))
            save_task(st)
            send(content=receipt('提交结果待核对', st['title'], '未得到成功回执，请在网络学堂核对。系统不会自动重试。', success=False))
            return False
        st.update(status='submitted', submission_receipt=result, submitted_at=now().isoformat())
        save_task(st)
        send_submission_receipt(st)
        return True


def main():
    from .events import consume
    consume('card.action.trigger', act, 'actions')

if __name__ == '__main__':
    main()
