"""Keep event-consumer stdin open and report actual ready markers."""
import json
import signal
import subprocess
import sys
import threading
from .common import DATA, ROOT, config, now, safe_error, write_json


def consume(key, handler, name):
    proc = subprocess.Popen([config()['lark_cli'], 'event', 'consume', key, '--as', 'bot'],
                            cwd=ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, bufsize=1)
    health = DATA / (name + '-health.json')
    write_json(health, {'state': 'starting', 'pid': proc.pid, 'time': now().isoformat()})
    def stop(*_):
        proc.terminate()
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    def stderr():
        for line in proc.stderr:
            print(line.rstrip(), file=sys.stderr, flush=True)
            if '[event] ready ' in line:
                write_json(health, {'state': 'ready', 'pid': proc.pid, 'time': now().isoformat()})
    worker = threading.Thread(target=stderr, daemon=True)
    worker.start()
    try:
        for line in proc.stdout:
            try:
                event = json.loads(line)
                handled = handler(event)
                if handled:
                    write_json(DATA / (name + '-last-event.json'), {'event_id': event.get('event_id'),
                               'message_id': event.get('message_id'), 'type': event.get('type'), 'time': now().isoformat()})
            except Exception as exc:
                print('Event processing failed: ' + safe_error(exc), file=sys.stderr, flush=True)
    finally:
        proc.terminate()
        proc.wait(timeout=15)
        worker.join(timeout=2)
        write_json(health, {'state': 'stopped', 'exit_code': proc.returncode, 'time': now().isoformat()})
    raise SystemExit(proc.returncode or 1)
