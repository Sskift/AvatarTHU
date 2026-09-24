"""Fresh, non-interactive Codex/Claude sessions for writing and independent review."""
import copy
import json
import os
import signal
import subprocess
from pathlib import Path
from . import common as c


def strict_schema(schema):
    value = copy.deepcopy(schema)
    def walk(node):
        if not isinstance(node, dict):
            return
        if node.get('type') == 'object':
            node['additionalProperties'] = False
            node['required'] = list(node.get('properties', {}))
        for child in node.get('properties', {}).values():
            walk(child)
        walk(node.get('items'))
    walk(value)
    return value


def command(executor, workspace, schema, settings):
    executable = settings.get(executor + '_cli')
    if not executable:
        raise RuntimeError(f'未找到 {executor} CLI，请安装并登录后再运行。')
    if executor == 'claude':
        cmd = [executable, '-p', '--output-format', 'json', '--json-schema', json.dumps(schema),
               '--permission-mode', 'acceptEdits', '--permission-prompts', 'none', '--safe-mode',
               '--allowedTools', 'Read,Write,Edit,Glob,Grep,Bash,WebFetch,WebSearch',
               '--tools', 'Read,Write,Edit,Glob,Grep,Bash,WebFetch,WebSearch',
               '--strict-mcp-config', '--mcp-config', '{"mcpServers":{}}',
               '--disable-slash-commands', '--no-session-persistence']
    elif executor == 'codex':
        path = workspace / 'output-schema.json'
        c.write_json(path, strict_schema(schema))
        cmd = [executable, '-a', 'never', 'exec', '--skip-git-repo-check', '--ephemeral',
               '--ignore-rules', '--sandbox', 'workspace-write',
               '--disable', 'memories', '--disable', 'multi_agent', '--disable', 'apps',
               '--disable', 'hooks', '--disable', 'skill_search', '--enable', 'skip_host_skill_discovery',
               '-c', 'project_doc_max_bytes=0', '-c', 'memories.generate_memories=false',
               '-c', 'developer_instructions=""', '-c', 'mcp_servers={}',
               '--output-schema', str(path), '--output-last-message', str(workspace / 'engine-output.json'),
               '--json', '--color', 'never', '-C', str(workspace)]
        cmd += ['-']
    else:
        raise ValueError('Unknown executor: ' + executor)
    return cmd


def run(executor, workspace, prompt, schema, role, settings):
    workspace = Path(workspace)
    cmd = command(executor, workspace, schema, settings)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(('AUTOTHU_', 'LARK_', 'THU_', 'AVATARTHU_', 'CODEX_THREAD_', 'CODEX_INTERNAL_')) or key in {'CLAUDECODE', 'CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS'}:
            env.pop(key)
    env['CLAUDE_CODE_DISABLE_AUTO_MEMORY'] = '1'
    output = workspace / (executor + ('.json' if executor == 'claude' else '.jsonl'))
    errors = workspace / (executor + '.stderr.log')
    metadata = {'executor': executor, 'role': role, 'model_selection': 'cli-default',
                'started_at': c.now().isoformat(), 'fresh_session': True}
    (workspace / 'prompt.txt').write_text(prompt, encoding='utf-8')
    with c.lock('model-worker', blocking=False) as guard, output.open('w') as log, errors.open('w') as err:
        proc = subprocess.Popen(cmd, cwd=workspace, stdin=subprocess.PIPE, stdout=log, stderr=err,
                                text=True, start_new_session=True, env=env, pass_fds=(guard.fileno(),))
        c.write_json(workspace / 'process.json', dict(metadata, pid=proc.pid))
        def stop(signum, _frame):
            raise SystemExit(128 + signum)
        previous_sigterm = signal.signal(signal.SIGTERM, stop)
        try:
            proc.communicate(prompt, timeout=settings.get('stage_timeout', 7200))
        except BaseException:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait()
            except ProcessLookupError:
                pass
            raise
        finally:
            signal.signal(signal.SIGTERM, previous_sigterm)
    if proc.returncode:
        raise RuntimeError(f'{executor} {role} 退出码 {proc.returncode}；详见 {errors}')
    if executor == 'claude':
        envelope = c.read_json(output)
        if envelope.get('is_error') or envelope.get('subtype') != 'success':
            raise RuntimeError(f'Claude 未完成 {role}，详见 {output}')
        result = envelope.get('structured_output')
    else:
        result = c.read_json(workspace / 'engine-output.json')
        # JSONL contains tool output as well: only inspect top-level terminal events.
        completed, failed = False, False
        with output.open() as stream:
            for line in stream:
                if not line.strip():
                    continue
                event = json.loads(line)
                completed |= event.get('type') == 'turn.completed'
                failed |= event.get('type') in {'turn.failed', 'error'}
        if not completed or failed:
            raise RuntimeError(f'Codex 未完成 {role}，详见 {output}')
    if not isinstance(result, dict):
        raise RuntimeError(f'{executor} 没有返回 {role} 结果，详见 {output}')
    metadata['completed_at'] = c.now().isoformat()
    c.write_json(workspace / 'execution.json', metadata)
    return result
