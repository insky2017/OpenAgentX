#!/usr/bin/env python3
"""Independent, single-writer native Codex commander. Each event runs one turn."""
import argparse
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path('/home/sky/docs/.oax-commander')
WORKSPACE = Path('/home/sky/docs')
CODEX = '/home/sky/.local/bin/codex'


def save(path, value):
    tmp = path.with_suffix(path.suffix + '.new')
    tmp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    tmp.chmod(0o600)
    os.replace(tmp, path)


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('action', choices=['init', 'event', 'send', 'open', 'status'])
    p.add_argument('--key')
    p.add_argument('--prompt', type=Path)
    p.add_argument('--maintenance', action='store_true', help='Explicitly authorized maintenance event; follow only its bounded handoff.')
    a = p.parse_args()
    if a.maintenance and a.action not in ('send', 'event'):
        p.error('--maintenance is only available for an explicit send/event handoff')
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    state_file = ROOT / 'session.json'
    if a.action == 'status':
        visible = json.loads(state_file.read_text()) if state_file.exists() else {'state': 'not_started'}
        with (ROOT / 'writer.lock').open('a') as probe:
            try:
                fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
                visible['writer_busy'] = False
            except BlockingIOError:
                visible['writer_busy'] = True
        print(json.dumps(visible, ensure_ascii=False, indent=2))
        return 0
    if a.action == 'send':
        if not a.key or not re.fullmatch(r'[a-zA-Z0-9_-]{1,100}', a.key) or not a.prompt:
            raise SystemExit('send requires --key SAFE_EVENT_ID --prompt FILE')
        return subprocess.call(['systemd-run', '--user',
            '--unit=oax-commander-event-' + a.key,
            '--property=Type=exec', '--property=UMask=0077',
            '--property=WorkingDirectory=' + str(WORKSPACE),
            '/usr/bin/python3', str(Path(__file__).resolve()), 'event',
            '--key', a.key, '--prompt', str(a.prompt.resolve())] + (['--maintenance'] if a.maintenance else []))
    with (ROOT / 'writer.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise SystemExit('Commander writer is already active; this event was not submitted.')
        state = json.loads(state_file.read_text()) if state_file.exists() else {}
        base = [CODEX, '-c', 'model_reasoning_effort="high"', '-c', 'approval_policy="never"']
        if a.action == 'open':
            if not state.get('thread_id'):
                raise SystemExit('No verified commander thread exists.')
            return subprocess.call(base + ['resume', '-m', 'gpt-6-astra', '-s', 'danger-full-access',
                '-C', str(WORKSPACE), '--no-alt-screen', state['thread_id']], cwd=WORKSPACE)
        if not a.key or not re.fullmatch(r'[a-zA-Z0-9_-]{1,100}', a.key) or not a.prompt:
            raise SystemExit('init/event requires --key SAFE_EVENT_ID --prompt FILE')
        scope = ('本次是显式授权的维护交接事件。仅执行本事件正文指定的受控脚本和范围；'
                 '不另开部署、不取消或重跑业务任务、不修改数据库，不派子代理。'
                 if a.maintenance else
                 '本次只读核对。禁止部署、重启、取消、重跑、写文件或修改数据库及凭据；'
                 '即使历史上下文有旧授权，本事件也不启用维护执行。')
        prompt = scope + '\n\n' + a.prompt.read_text()
        prompt_hash = hashlib.sha256(prompt.encode()).hexdigest()
        event = ROOT / 'events' / a.key
        if event.exists():
            started_file = event / 'started.json'
            if started_file.exists() and json.loads(started_file.read_text()).get('prompt_sha256') != prompt_hash:
                raise SystemExit('Event key was already used with different content; refusing replay.')
            receipt = event / 'result.json'
            print(receipt.read_text() if receipt.exists() else
                  'Event already started; inspect evidence before any retry. No model was called.')
            return 0 if receipt.exists() else 2
        if a.action == 'init' and state.get('thread_id'):
            raise SystemExit('Commander already initialized; use event to resume its exact thread.')
        if a.action == 'event' and not state.get('thread_id'):
            raise SystemExit('No verified commander thread exists.')
        event.mkdir(parents=True, mode=0o700)
        (event / 'prompt.md').write_text(prompt)
        started = {'at': now(), 'event': a.key, 'prompt_sha256': prompt_hash,
                   'workspace': str(WORKSPACE), 'model': 'gpt-6-astra', 'effort': 'high',
                   'sandbox': 'danger-full-access', 'task_scope': 'authorized-maintenance' if a.maintenance else 'read-only', 'thread_before': state.get('thread_id'),
                   'operator_pid': os.getpid()}
        save(event / 'started.json', started)
        cmd = base + ['-c', 'sandbox_mode="danger-full-access"', 'exec']
        if a.action == 'event':
            cmd += ['resume', '--json', '--skip-git-repo-check', '-m', 'gpt-6-astra',
                    '-o', str(event / 'final.md'), state['thread_id'], '-']
        else:
            cmd += ['--json', '--skip-git-repo-check', '-m', 'gpt-6-astra',
                    '-C', str(WORKSPACE), '-o', str(event / 'final.md'), '-']
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('OAX_', 'OPENAGENTX_')) and k not in ('CODEX_THREAD_ID', 'CODEX_INTERNAL_ORIGINATOR_OVERRIDE')}
        turn_completed = False
        with (event / 'events.jsonl').open('w') as output, (event / 'stderr.log').open('w') as err:
            proc = subprocess.Popen(cmd, cwd=WORKSPACE, env=env, stdin=subprocess.PIPE,
                                    stdout=subprocess.PIPE, stderr=err, text=True)
            proc.stdin.write(prompt)
            proc.stdin.close()
            for line in proc.stdout:
                output.write(line)
                output.flush()
                try:
                    record = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if record.get('type') == 'turn.completed':
                    turn_completed = True
                if record.get('type') == 'thread.started':
                    thread = record.get('thread_id')
                    if state.get('thread_id') and thread != state['thread_id']:
                        proc.terminate()
                        raise RuntimeError('Refusing a changed commander thread.')
                    state.update({'thread_id': thread, 'workspace': str(WORKSPACE),
                                  'model': 'gpt-6-astra', 'effort': 'high', 'host': 'independent-native-codex',
                                  'created_at': state.get('created_at', now())})
                    save(state_file, state)
            code = proc.wait()
        result = {**started, 'finished_at': now(), 'exit_code': code,
                  'thread_id': state.get('thread_id'), 'final_exists': (event / 'final.md').exists(), 'turn_completed': turn_completed,
                  'status': 'completed' if code == 0 and turn_completed and state.get('thread_id') and (event / 'final.md').exists() else 'uncertain'}
        save(event / 'result.json', result)
        state.update({'last_event': a.key, 'last_result': result['status']})
        save(state_file, state)
        print(json.dumps(result, ensure_ascii=False))
        return code if code else (0 if result['status'] == 'completed' else 2)


if __name__ == '__main__':
    sys.exit(main())
