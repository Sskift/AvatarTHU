"""Reply-to-card feedback fallback; the card form is the normal interface."""
import json
import re
from .common import config, lock, read_json, save_task, send, task_path, tasks
from .cards import receipt


def act(event):
    from .common import lark_enabled
    if not lark_enabled() or event.get('sender_id') != config().get('lark_user_id') or not event.get('message_id'):
        return False
    content = event.get('content', '')
    try:
        parsed = json.loads(content)
        if isinstance(parsed, dict):
            content = parsed.get('text', '')
    except ValueError:
        pass
    reply = event.get('reply_to') or event.get('root_id')
    matched = [s for s in tasks() if reply and s.get('card_message_id') == reply]
    command = re.fullmatch(r'意见\s+([a-f0-9]{16})\s+(.+)', content.strip(), re.S)
    if command:
        tid, feedback = command.groups()
    elif len(matched) == 1:
        tid, feedback = matched[0]['task_id'], content.strip()
    else:
        return False
    if not feedback:
        return False
    with lock(tid):
        st = read_json(task_path(tid))
        if event['message_id'] in st.get('feedback_messages', []):
            return False
        if st.get('status') not in {'awaiting', 'needs_student', 'revision_requested', 'approval_invalid'}:
            return False
        if st.get('chat_id') and event.get('chat_id') != st['chat_id']:
            return False
        st.setdefault('feedback_messages', []).append(event['message_id'])
        st.update(status='revision_ready', feedback=feedback)
        save_task(st)
    send(content=receipt('修改已排队', st['title'], '会自动重新处理并检查，完成后发给你。'),
         idem='feedback:' + str(event.get('message_id')))
    return True


def main():
    from .events import consume
    consume('im.message.receive_v1', act, 'messages')

if __name__ == '__main__':
    main()
