#!/usr/bin/env python3
"""Isolated old-runtime continuity across the official online removal CLI.

No production unit or tmux server is changed. Only this fixture's exact PIDs,
profile and tmux server are owned. Credentials never enter saved API evidence.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import signal
import sqlite3
import subprocess
import sys
import threading
import time

sys.dont_write_bytecode = True
from terminal_overview_e2e import TerminalRun, AttachedClient, require, sha, now
from terminal_overview_snapshot import process, durable_children
from agy_live import owned_stop


class RemovalRun(TerminalRun):
    def __init__(self, args):
        super().__init__(args)
        self.aid = 'cleanup-keep-' + secrets.token_hex(3)
        self.delete_id = self.aid.replace('keep', 'delete')
        self.shared_id = self.aid.replace('keep', 'shared')

    def cli(self, label, args):
        if label == '01-join':
            role = Path(args[args.index('--role') + 1])
            role.write_text('You are an isolated OpenAgentX continuity verification Agent. Work only in this assigned workspace. '
                            'You may run the exact requested local tools and write only requested proof files. Never read credentials, '
                            'message other Agents, alter services, inspect unrelated paths, or act on production. '
                            'When asked to confirm role/workspace reply exactly: ' + self.expected + '\n')
        return super().cli(label, args)

    @classmethod
    def restore(cls, args):
        args.phase = 'native'
        self = super().restore(args)
        self.__class__ = cls
        saved = json.loads((self.root / 'removal-context.json').read_text())
        self.delete_id, self.shared_id = saved['delete_agent'], saved['shared_agent']
        self.context = saved
        return self

    def create_task(self, agent, content, parent=None, thread=None):
        body = {'target_agent_id': agent, 'organization_id': 'default',
                'dispatch_mode': 'direct', 'intent': 'mutation' if thread else 'query', 'content': content}
        if parent:
            body['parent_task_id'] = parent
        if thread:
            body['runtime_session'] = {'backend_id': 'codex', 'provider_session_id': thread}
        receipt = self.api('/api/control/v1/tasks', body)
        self.task_ids.append(receipt['task_id'])
        self.checkpoint()
        return receipt['task_id']

    def detail(self, tid):
        return self.api('/api/observe/v1/tasks/' + tid)

    def settled(self, tid):
        def done():
            detail = self.detail(tid)
            return detail if detail['task']['status'] in {'succeeded', 'failed', 'uncertain', 'canceled'} else None
        return self.wait(done, 360)

    def cancel_queued(self, tid):
        detail = self.detail(tid)
        require(detail['task']['status'] == 'queued' and not detail.get('run_attempts'), 'fixture unexpectedly executed')
        self.api('/api/control/v1/tasks/' + tid + '/cancel',
                 {'requested_by': 'owner', 'meta': {'expected_version': detail['task']['version']}})
        require(self.detail(tid)['task']['status'] == 'canceled', 'fixture cancel failed')

    def identities(self, label):
        state = json.loads((self.root / 'profile/workers/codex' / self.aid / 'state.json').read_text())
        pane = self.windows['native'] + '.0'
        pane_pid = int(self.tmux('display-message', '-p', '-t', pane, '#{pane_pid}'))
        overview = self.api('/api/observe/v1/overview')
        value = {'at': now(), 'processes': [process(p['pid']) for p in self.procs],
                 'native': process(pane_pid), 'native_children': durable_children(pane_pid, 'terminal'),
                 'worker_backends': durable_children(next(p['pid'] for p in self.procs if p['label'] == 'worker'), 'backend'),
                 'thread': state['thread_id'], 'worker': [{k: w.get(k) for k in ('worker_instance_id', 'generation', 'started_at')}
                     for w in overview['workers'] if w['agent_id'] == self.aid],
                 'pane': self.tmux('display-message', '-p', '-t', pane, '#{pane_id}|#{pane_pid}|#{pane_dead}'),
                 'terminal_lock_held': self.lock_held(),
                 'keep_config_sha256': sha(self.root / 'profile/workers' / (self.aid + '.yaml'))}
        self.save(label + '.json', value)
        return value

    def assert_same(self, old, new):
        for key in old:
            if key != 'at':
                require(old[key] == new[key], 'continuity changed: ' + key)
        require(new['terminal_lock_held'], 'native writer lock missing')

    def prepare_live(self):
        super().setup()
        role = self.root / 'workspace/ROLE.md'
        role.write_text('You are an isolated OpenAgentX continuity verification Agent. Work only in this assigned workspace. '
                        'You may run the exact requested local tools and write only requested proof files. Never read credentials, '
                        'message other Agents, alter services, inspect unrelated paths, or act on production. '
                        'When asked to confirm role/workspace reply exactly: ' + self.expected + '\n')
        for aid in [self.delete_id, self.shared_id]:
            workspace = self.root / ('workspace-' + aid)
            workspace.mkdir(mode=0o700)
            (workspace / 'ROLE.md').write_text('Isolated offline cleanup fixture. No model work or production access.\n')
            self.cli('join-' + aid, ['agent', 'join', '--id', aid, '--name', aid, '--workspace', str(workspace),
                                    '--role', str(workspace / 'ROLE.md'), '--model', 'gpt-6-astra', '--prepare'])
            self.cli('apply-' + aid, ['agent', 'apply', '--file', str(self.root / 'profile/workers/identities' / (aid + '.yaml'))])
        self.windows['native'] = self.tmux('new-session', '-d', '-x', '140', '-y', '40', '-P', '-F', '#{window_id}',
                                          '-s', 'OAX', '-n', self.aid, 'sleep', '86400')
        self.tmux('set-option', '-w', '-t', self.windows['native'], 'pane-base-index', '0')
        self.tmux('set-option', '-w', '-t', self.windows['native'], 'automatic-rename', 'off')
        self.tmux('set-option', '-w', '-t', self.windows['native'], 'remain-on-exit', 'on')
        self.client = AttachedClient(self)
        launcher = self.script('native', [self.a.binary, 'agent', 'open', self.aid, '--native'])
        self.tmux('respawn-pane', '-k', '-t', self.windows['native'] + '.0', launcher)
        self.wait(self.lock_held, 60)
        def initial_done():
            tasks = [t for t in self.api('/api/observe/v1/tasks?limit=100')['tasks'] if t['target_agent_id'] == self.aid]
            require(len(tasks) <= 1, 'native initialization duplicated')
            if tasks and tasks[0]['status'] in {'succeeded', 'failed', 'uncertain', 'canceled'}:
                return self.detail(tasks[0].get('id', tasks[0].get('task_id')))
        initial = self.wait(initial_done, 360)
        require(initial['task']['status'] == 'succeeded', 'native initialization failed')
        initial_id = initial['task'].get('id', initial['task'].get('task_id'))
        self.task_ids.append(initial_id)
        self.save('native-initialization.json', initial)
        self.wait(lambda: 'OpenAgentX · ' + self.aid in self.capture(self.windows['native'] + '.0', 'native-ready'), 60)
        deletion_task = self.create_task(self.delete_id, 'CLEANUP-OWNED-HISTORY: never executed; cancel then remove this isolated history.')
        self.cancel_queued(deletion_task)
        shared_task = self.create_task(self.shared_id, 'CLEANUP-SHARED-BOUNDARY: preserve this cross-Agent parent relation.', parent=initial_id)
        self.cancel_queued(shared_task)
        baseline = self.identities('baseline')
        self.context = {'keep_agent': self.aid, 'delete_agent': self.delete_id, 'shared_agent': self.shared_id,
                        'initial_task': initial_id, 'delete_task': deletion_task, 'shared_task': shared_task,
                        'thread': baseline['thread'], 'baseline_at': now()}
        (self.root / 'removal-context.json').write_text(json.dumps(self.context, indent=2) + '\n')
        self.checkpoint()
        self.save('baseline-verdict.json', {'status': 'READY', 'scope': 'old daemon/Worker/native with actual Codex initialization; online cleanup not executed', **self.context})

    def execute_live(self):
        require(self.a.candidate and self.a.candidate.is_file(), 'candidate binary is required')
        candidate_source = self.a.candidate
        fixed_candidate = self.root / 'candidate-under-test'
        with candidate_source.open('rb') as source, fixed_candidate.open('xb') as target:
            shutil.copyfileobj(source, target)
        fixed_candidate.chmod(0o700)
        self.a.candidate = fixed_candidate
        # Permit real read-only service discovery for our exact fixture identities;
        # never permit a systemctl mutation, even if the candidate misidentifies a template.
        systemctl = self.tools / 'systemctl'
        systemctl.write_text('#!/bin/sh\ncase "$1:$2:$3" in\n' +
            '  --user:show:openagentx-worker@cleanup-*.service) exec /usr/bin/systemctl "$@" ;;\n' +
            '  *) echo "fixture forbids systemctl mutation or non-fixture discovery" >&2; exit 97 ;;\nesac\n')
        systemctl.chmod(0o700)
        self.client = AttachedClient(self)
        old = self.identities('before-candidate')
        original = json.loads((self.out / 'baseline.json').read_text())
        self.assert_same(original, old)
        self.save('candidate-provenance.json', {'at': now(), 'binary': str(self.a.candidate),
            'source_artifact': str(candidate_source),
            'sha256': sha(self.a.candidate), 'harness_sha256': sha(__file__),
            'source_sha256': {str(p): sha(p) for pattern in ['internal/cli/fleet/agent*.go',
                'internal/domain/agent_removal.go', 'internal/persistence/sqlite/agent_removal*.go',
                'internal/persistence/sqlite/migrations/*agent_removal*', 'internal/persistence/sqlite/migrations/migrations.go']
                for p in Path('.').glob(pattern) if p.is_file()},
            'build_info': subprocess.check_output(['/home/sky/tools/go/bin/go', 'version', '-m', str(self.a.candidate)], text=True)})

        def schema_version():
            path = self.root / 'profile/data/openagentx.db'
            with sqlite3.connect(path.as_uri() + '?mode=ro', uri=True) as db:
                return db.execute('SELECT version FROM schema_meta').fetchone()[0]

        def candidate(label, aid, apply):
            command = [str(self.a.candidate), 'agent', 'remove', aid, '--purge-history',
                       '--yes' if apply else '--dry-run', '--wait', '30s']
            trace = self.out / (label + '.fcntl.log')
            invocation = ['/usr/bin/strace', '-f', '-ttt', '-yy', '-e', 'trace=fcntl', '-o', str(trace), '--', *command]
            started = time.monotonic()
            proc = subprocess.run(invocation, env=self.env, capture_output=True, text=True, timeout=60)
            elapsed = time.monotonic() - started
            value = {'at': now(), 'command': command, 'returncode': proc.returncode,
                     'elapsed_seconds': elapsed, 'stdout': proc.stdout, 'stderr': proc.stderr}
            self.save(label + '.json', value)
            return value

        version = schema_version()
        require(version == 5, 'fixture must start at v5')
        shared = candidate('shared-boundary-preview', self.shared_id, False)
        require(shared['returncode'] == 0, 'shared boundary preview failed')
        shared_apply = candidate('shared-boundary-refused', self.shared_id, True)
        require(shared_apply['returncode'] != 0, 'cross-Agent shared history unexpectedly purged')
        require(schema_version() == 5, 'blocked removal performed migration')
        require(self.detail(self.context['shared_task'])['task']['status'] == 'canceled', 'shared history changed')
        require(self.detail(self.context['initial_task'])['task']['status'] == 'succeeded', 'preserved parent changed')
        preview = candidate('delete-preview', self.delete_id, False)
        require(preview['returncode'] == 0 and schema_version() == 5, 'dry-run failed or mutated schema')

        marker = 'OAX-CLEANUP-LIVE-' + secrets.token_hex(6)
        script = ("from pathlib import Path\nimport time\np=Path('.')\n"
                  "p.joinpath('before-proof.txt').write_text(" + repr(marker + '\n') + ")\n"
                  "p.joinpath('migration-ready.txt').write_text('ready\\n')\n"
                  "end=time.monotonic()+180\n"
                  "while not p.joinpath('migration-release.txt').exists():\n"
                  " if time.monotonic()>end: raise TimeoutError('migration release not received')\n"
                  " time.sleep(1)\n"
                  "p.joinpath('after-proof.txt').write_text(" + repr(marker + '\n') + ")\n")
        prompt = ('Execute exactly one local Python command in your assigned workspace using the script below. '
                  'It writes proof before and after the external migration harness releases it. '
                  'Do not access other directories or run other commands. Wait for this command to finish, then reply exactly '
                  + marker + '.\npython3 -c ' + shlex.quote(script))
        active = self.create_task(self.aid, prompt, thread=old['thread'])
        workspace = self.root / 'workspace'
        def proof_ready():
            if (workspace / 'migration-ready.txt').exists():
                return True
            detail = self.detail(active)
            self.save('active-task-awaiting-proof.json', detail)
            require(detail['task']['status'] not in {'succeeded', 'failed', 'uncertain', 'canceled'},
                    'real model ended before producing pre-migration proof; see Task evidence')
            return False
        self.wait(proof_ready, 180)
        active_detail = self.detail(active)
        self.save('active-task-before-migration.json', active_detail)
        require(active_detail['task']['status'] == 'running', 'proof command not inside active Task')
        require((workspace / 'before-proof.txt').read_text() == marker + '\n', 'missing actual pre-migration effect')
        require(not (workspace / 'after-proof.txt').exists(), 'tool already finished before migration')
        before = self.identities('active-before-migration')
        self.assert_same(old, before)
        try:
            migration = candidate('online-removal', self.delete_id, True)
        finally:
            # Always release our bounded model tool, including a candidate failure.
            (workspace / 'migration-release.txt').write_text('release\n')
        require(migration['returncode'] == 0, 'candidate online removal failed; see saved evidence')
        held_at, holds = None, []
        for line in (self.out / 'online-removal.fcntl.log').read_text().splitlines():
            if 'openagentx.db-shm>' not in line or 'F_SETLK' not in line or not line.rstrip().endswith('= 0'):
                continue
            match = re.search(r'(\d+\.\d+)\s+fcntl\(.*l_type=(F_\w+),.*l_start=(\d+), l_len=(\d+)', line)
            if not match:
                continue
            stamp, kind, start, length = float(match[1]), match[2], int(match[3]), int(match[4])
            if not (start <= 120 and (length == 0 or start + length > 120)):
                continue
            if kind == 'F_WRLCK' and held_at is None:
                held_at = stamp
            elif kind != 'F_WRLCK' and held_at is not None:
                holds.append({'acquired_at_epoch': held_at, 'released_at_epoch': stamp, 'seconds': stamp - held_at})
                held_at = None
        require(holds and held_at is None, 'WAL write-lock evidence incomplete')
        max_lock = max(h['seconds'] for h in holds)
        self.save('wal-write-lock-budget.json', {'method': 'strace successful fcntl locks on this fixture DB-shm byte 120',
                  'holds': holds, 'max_seconds': max_lock, 'budget_seconds': 2, 'pass': max_lock < 2})
        require(max_lock < 2, 'online operation exceeded conservative write-lock budget')
        require(schema_version() == 6, 'online schema did not become v6')
        after = self.identities('active-after-migration')
        self.assert_same(before, after)
        outcome = self.settled(active)
        self.save('active-task-completed.json', outcome)
        self.finish_live(before, active, outcome, marker, max_lock, migration)

    def verify_effectful_run(self, detail):
        require(len(detail['run_attempts']) == 1, 'actual model task duplicated')
        run = detail['run_attempts'][0]
        result = run.get('turn_result', {})
        require(run['status'] == 'succeeded' and result.get('final_reply') is True,
                'real Runtime did not deliver a complete successful reply')
        # Deployed v5 deliberately leaves mutation business effects uncertain
        # without internal effect records. Independent file checks below prove
        # this fixture's effect; they do not rewrite its authoritative Task state.
        require(detail['task']['status'] == 'succeeded' or
                (detail['task']['status'] == 'uncertain' and result.get('side_effects_source') == 'not_recorded'),
                'Task has an unexpected terminal state')

    def finish_live(self, before, active, outcome, marker, max_lock, migration):
        workspace = self.root / 'workspace'
        self.verify_effectful_run(outcome)
        require((workspace / 'after-proof.txt').read_text() == marker + '\n', 'missing actual post-migration effect')
        self.save('active-run.json', self.api('/api/observe/v1/run-attempts/' + outcome['run_attempts'][0]['run_id']))

        # Enter a subsequent task through the real, still-open native TUI.
        pane = self.windows['native'] + '.0'
        intent_path = self.out / 'native-followup-intent.json'
        if intent_path.exists():
            next_marker = json.loads(intent_path.read_text())['marker']
        else:
            next_marker = 'OAX-CLEANUP-NEXT-' + secrets.token_hex(6)
            text = 'Write next-proof.txt in the assigned workspace containing exactly ' + next_marker + ' followed by one newline. Then reply exactly ' + next_marker + '. Do nothing else.'
            self.capture(pane, 'native-before-next')
            self.save('native-followup-intent.json', {'at': now(), 'marker': next_marker, 'text': text,
                      'rule': 'persist before typing; resumed verification must not resend this task'})
            self.tmux('send-keys', '-t', pane, '-l', text)
            # Let Codex finish classifying the literal burst as pasted input.
            # An Enter in the same burst can otherwise become a draft newline.
            time.sleep(1)
            self.tmux('send-keys', '-t', pane, 'Enter')
        def next_dispatched():
            # Observe list rows carry a truncated summary, not Task content.
            # Inspect full authoritative Task details to match the unique input.
            listed = self.api('/api/observe/v1/tasks?limit=100')['tasks']
            tasks = [self.detail(t['id'])['task'] for t in listed if t['target_agent_id'] == self.aid]
            tasks = [t for t in tasks if next_marker in t.get('content', '')]
            require(len(tasks) <= 1, 'native follow-up duplicated')
            return tasks[0]['id'] if tasks else None
        next_id = self.wait(next_dispatched, 90)
        self.task_ids.append(next_id)
        next_result = self.settled(next_id)
        self.save('native-next-task.json', next_result)
        self.verify_effectful_run(next_result)
        require((workspace / 'next-proof.txt').read_text() == next_marker + '\n', 'native follow-up filesystem effect missing')
        final = self.identities('final-continuity')
        self.assert_same(before, final)
        remaining = self.api('/api/observe/v1/overview')
        ids = {a['agent_id'] for a in remaining['agents']}
        require(self.delete_id not in ids and {self.aid, self.shared_id} <= ids, 'Agent removal escaped exact target set')
        remaining_tasks = self.api('/api/observe/v1/tasks?limit=100')['tasks']
        require(not any(t['target_agent_id'] == self.delete_id for t in remaining_tasks), 'deleted Agent history remains')
        require((self.root / ('workspace-' + self.delete_id) / 'ROLE.md').exists(), 'business workspace was removed')
        self.save('verdict.json', {'status': 'PASS', 'scope': 'R old real daemon/Worker/Codex/native continuity across new CLI migration and exact history purge',
                  'old_schema': 5, 'new_schema': 6, 'active_task': active, 'next_native_task': next_id,
                  'active_task_status': outcome['task']['status'], 'next_task_status': next_result['task']['status'],
                  'thread': before['thread'], 'worker': before['worker'], 'deleted_agent': self.delete_id,
                  'cross_agent_boundary_refused': True, 'processes_native_generation_preserved': True,
                  'model_effects': [marker, next_marker], 'cleanup_elapsed_seconds': migration['elapsed_seconds'],
                  'max_wal_write_lock_seconds': max_lock,
                  'limitations': ['No production changes', 'No systemd unit teardown claim; fixture uses directly owned processes',
                                  'Mutation Task status is preserved as reported; independent file verification does not rewrite uncertain to succeeded',
                                  'Duration applies to this tested fixture size; production counts need bounded preflight']})
        self.capture(pane, 'native-final')
        self.collect()

    def continue_verification(self):
        self.client = AttachedClient(self)
        before = json.loads((self.out / 'active-before-migration.json').read_text())
        self.assert_same(before, self.identities('before-native-followup'))
        outcome = json.loads((self.out / 'active-task-completed.json').read_text())
        active = outcome['task'].get('id', outcome['task'].get('task_id'))
        marker = (self.root / 'workspace/before-proof.txt').read_text().strip()
        max_lock = json.loads((self.out / 'wal-write-lock-budget.json').read_text())['max_seconds']
        migration = json.loads((self.out / 'online-removal.json').read_text())
        self.finish_live(before, active, outcome, marker, max_lock, migration)

    def cleanup(self):
        self.collect()
        self.tmux('kill-server')
        self.save('owned-cleanup.json', owned_stop(self.root))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phase', choices=['prepare', 'execute', 'continue-verification', 'cleanup'], required=True)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--binary', type=Path, required=True, help='Fixed 886ba7f old installed artifact')
    parser.add_argument('--candidate', type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    args.commit = '886ba7f'
    phase = args.phase
    run = RemovalRun(args) if phase == 'prepare' else RemovalRun.restore(args)
    try:
        if phase == 'prepare': run.prepare_live()
        elif phase == 'execute': run.execute_live()
        elif phase == 'continue-verification': run.continue_verification()
        else: run.cleanup()
    except Exception as error:
        run.save('failure-' + phase + '-' + str(time.time_ns()) + '.json',
                 {'at': now(), 'type': type(error).__name__, 'message': str(error).replace(run.password, '[REDACTED]')})
        raise
    finally:
        if run.client: run.client.close()
    print(json.dumps({'phase': phase, 'root': str(run.root), 'status': 'COMPLETE'}, ensure_ascii=False), flush=True)


if __name__ == '__main__':
    main()
