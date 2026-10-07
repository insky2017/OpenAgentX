#!/usr/bin/env python3
"""One isolated final-artifact native TUI/Worker smoke, never a deployment."""
import argparse
import json
import os
from pathlib import Path
import sqlite3
import sys
import time

sys.dont_write_bytecode = True
from native_model_settings_e2e import SettingsRun, AttachedClient, require, now, sha

TERMINAL = {'succeeded', 'failed', 'uncertain', 'canceled'}
PENDING = 'Messages to be submitted after next tool call'
PROOF = b'OAX_RELEASE_PROOF_OK\n'
STEER_PROOF = b'RELEASE_STEER_OK'


def execute(args):
    run = SettingsRun(args)
    verdict = {'status': 'RUNNING', 'at': now(), 'source_commit': args.commit,
               'binary_sha256': sha(args.binary), 'harness_sha256': sha(__file__),
               'production_touched': False}
    try:
        run.prepare()
        pane = run.windows['native'] + '.0'
        workspace = run.root / 'workspace'
        state_path = run.root / 'profile/workers/codex' / run.aid / 'state.json'
        thread = json.loads(state_path.read_text())['thread_id']
        initial_tasks = set(run.task_ids)
        require(len(initial_tasks) == 1, 'expected exactly one native initialization query')
        settings_path = '/api/console/v1/agents/' + run.aid + '/model-settings?backend_id=codex'
        settings_before = run.api(settings_path)
        run.save('release-settings-before.json', settings_before)
        tool = workspace / 'release-proof.py'
        tool.write_text('''import json,os,time
from pathlib import Path
p=Path(__file__).resolve().parent
with (p/'release-start.json').open('x') as f: json.dump({'pid':os.getpid()},f)
time.sleep(15)
with (p/'release-proof.txt').open('xb') as f: f.write(b'OAX_RELEASE_PROOF_OK\\n')
print('OAX_RELEASE_PROOF_OK',flush=True)
''')
        steer_tool = workspace / 'release-steer.py'
        steer_tool.write_text("from pathlib import Path\np=Path(__file__).resolve().parent\nwith (p/'release-steer.txt').open('xb') as f: f.write(b'RELEASE_STEER_OK')\nprint('RELEASE_STEER_OK',flush=True)\n")
        tools = {tool.name: sha(tool), steer_tool.name: sha(steer_tool)}
        run.save('release-tools.json', {'sha256': tools, 'duration_seconds': 15,
                                      'exclusive_creation_rejects_duplicate_effects': True})
        prompt = ('在当前隔离目录仅执行一次 python3 release-proof.py，等待同一个工具进程正常结束，'
                  '不要后台脱离、重复启动、修改脚本或使用子代理。它会等待15秒并写入精确验收文件。'
                  '完成后回复 OAX_RELEASE_PROOF_OK；若收到一条补充要求，只按补充要求继续，不重复之前的工具。')
        run.client = AttachedClient(run)
        run.tmux('send-keys', '-t', pane, '-l', prompt)
        time.sleep(.4)
        run.tmux('send-keys', '-t', pane, 'Enter')

        def new_task():
            selected = [t for t in run.api('/api/observe/v1/tasks?limit=100')['tasks']
                        if t['target_agent_id'] == run.aid and t['id'] not in initial_tasks]
            require(len(selected) <= 1, 'native input dispatched multiple Tasks')
            return selected[0] if selected else None

        task = run.wait(new_task, 120)
        tid = task['id']
        run.task_ids.append(tid)
        run.checkpoint()
        run.wait(lambda: (workspace / 'release-start.json').exists(), 180)
        active = run.api('/api/observe/v1/tasks/' + tid)
        require(active['task']['status'] not in TERMINAL, 'tool completed before active observation')
        require(len(active['run_attempts']) == 1, 'expected one active Run')
        run.save('release-active-task.json', active)
        run.capture(pane, 'release-active-screen', False)
        running_state = run.tmux('show-options', '-w', '-v', '-t', run.windows['native'], '@oax_state')
        require(running_state == 'running', 'active tool has no canonical running indicator')
        formats = {name: run.tmux('show-options', '-w', '-A', '-v', '-t', run.windows['native'], name)
                   for name in ('window-status-format', 'window-status-current-format')}
        run.save('release-running-indicator.json', {'window': run.windows['native'],
                 'state': running_state, 'formats': formats})
        steer = None
        if args.steer:
            steer = ('After the current tool returns, execute exactly once python3 release-steer.py '
                     'in this same workspace. Do not repeat any previous command. '
                     'Then reply OAX_RELEASE_PROOF_OK RELEASE_STEER_OK.')
            run.tmux('send-keys', '-t', pane, '-l', steer)
            time.sleep(.4)
            run.tmux('send-keys', '-t', pane, 'Enter')
            time.sleep(1)
            run.capture(pane, 'release-steer-submitted-screen', False)

        def completed():
            detail = run.api('/api/observe/v1/tasks/' + tid)
            return detail if detail['task']['status'] in TERMINAL else None

        detail = run.wait(completed, 420)
        run.save('release-final-task.json', detail)
        require(len(detail['run_attempts']) == 1, 'native task/steer created another Run')
        attempt = detail['run_attempts'][0]
        rid = attempt['run_id']
        require(attempt['status'] == 'succeeded', 'Runtime did not succeed')
        require(detail['task']['status'] in {'succeeded', 'uncertain'}, 'unexpected authoritative Task outcome')
        require(detail['events'], 'missing persisted Event Journal')
        require((workspace / 'release-proof.txt').read_bytes() == PROOF, 'local proof bytes differ')
        if args.steer:
            require((workspace / 'release-steer.txt').read_bytes() == STEER_PROOF, 'steer effect missing or differs')
            messages = [m for m in detail.get('messages', []) if m.get('content') == steer]
            require(len(messages) == 1, 'expected exactly one persisted steer message')
        for name, expected in tools.items():
            require(sha(workspace / name) == expected, 'proof tool changed: ' + name)
        db = sqlite3.connect('file:' + str(run.root / 'profile/data/openagentx.db') + '?mode=ro', uri=True)
        try:
            row = db.execute('SELECT resolved_execution_json FROM run_attempts WHERE run_id=?', (rid,)).fetchone()
            require(row is not None, 'persisted Run missing')
            frozen = json.loads(row[0])
        finally:
            db.close()
        require(frozen['spec']['timeout'] == 0 and frozen.get('deadline_at', '0001-01-01T00:00:00Z') == '0001-01-01T00:00:00Z',
                'final artifact did not freeze an unlimited Codex Run')
        run.save('release-frozen-timeout.json', {'run_id': rid, 'timeout': 0,
                 'deadline_at': frozen.get('deadline_at'), 'read_only_database': True})
        run.save('release-final-run.json', run.api('/api/observe/v1/run-attempts/' + rid))
        console = run.api('/api/console/v1/agents/' + run.aid + '/tasks/' + tid)
        run.save('release-console-task.json', console)
        require(not console['latest_run'].get('deadline_at'), 'Console unexpectedly projects a deadline')
        time.sleep(5)
        final_screen = run.capture(pane, 'release-final-screen', False)
        require(PENDING not in final_screen, 'consumed message remains in pending UI')
        idle_state = run.tmux('show-options', '-w', '-v', '-t', run.windows['native'], '@oax_state')
        require(idle_state == 'idle', 'completed Runtime left the canonical running indicator')
        run.save('release-idle-indicator.json', {'window': run.windows['native'], 'state': idle_state})
        engine = json.loads(state_path.read_text())
        require(engine['thread_id'] == thread and engine['state'] == 'idle', 'thread changed or Runtime is not idle')
        require(run.tmux('display-message', '-p', '-t', pane, '#{pane_dead}') == '0', 'native pane exited')
        require(run.lock_held(), 'native foreground lock released unexpectedly')
        all_tasks = [t for t in run.api('/api/observe/v1/tasks?limit=100')['tasks'] if t['target_agent_id'] == run.aid]
        require(len(all_tasks) == len(initial_tasks) + 1, 'unexpected additional model Task')
        settings_after = run.api(settings_path)
        run.save('release-settings-after.json', settings_after)
        require(settings_after == settings_before, 'read-only smoke changed model settings')
        run.snapshot('release-final-snapshot')
        run.save('release-file-proof.json', {'primary': {'sha256': sha(workspace / 'release-proof.txt'),
                 'bytes_exact': True}, 'steer': {'checked': args.steer, 'bytes_exact': args.steer}})
        verdict.update(status='PASS', at=now(), agent_id=run.aid, thread_id=thread,
                       task_id=tid, run_id=rid, task_status=detail['task']['status'],
                       completion_basis=detail['task'].get('completion_basis'), runtime_status=attempt['status'],
                       frozen_timeout=0, frozen_deadline=None, steer_checked=args.steer,
                       pending_ui_clear=True, native_foreground_alive=True,
                       canonical_indicator_running_then_idle=True,
                       scope='Final artifact isolated native input, one mutation Run, exact local effects, Journal, settings reads, same thread and idle terminal. Task uncertainty is retained.',
                       not_tested=['production deployment', 'business workflows', '31-minute duration'])
    except Exception as exc:
        verdict.update(status='FAILED', at=now(), error=str(exc).replace(run.password, '[REDACTED]'))
        raise
    finally:
        if run.client:
            run.client.close()
            run.client = None
        run.save('release-result.json', verdict)
        run.checkpoint()
        print(json.dumps(verdict, ensure_ascii=False), flush=True)
    if args.cleanup:
        run.cleanup()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--steer', action='store_true', help='Submit exactly one native steer while the tool is active')
    parser.add_argument('--cleanup', action='store_true', help='After PASS stop only this idle owned fixture, retaining evidence')
    args = parser.parse_args()
    args.phase = 'native'
    os.umask(0o077)
    execute(args)
