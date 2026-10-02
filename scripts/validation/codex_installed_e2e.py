#!/usr/bin/env python3
"""Installed Codex API acceptance against one explicitly prepared new Agent.

Does not install, configure, restart or stop services. Requires a previously
seeded native thread and its recall code in a private context JSON file. This
script verifies API and independent effects; native PTY and browser evidence
must be collected separately. Failed runs are retained and never auto-retried.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shlex
import subprocess
import sys
import time
import urllib.parse

from codex_workflow_e2e import Evidence, TERMINAL, require, sha, timestamp


def process_identity(pid):
    try:
        fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(')', 1)[1].split()
        return {'pid': pid, 'starttime': fields[19], 'state': fields[0]}
    except FileNotFoundError:
        return {'pid': pid, 'absent': True}


def same_live_process(expected, current):
    return (current.get('starttime') == expected['starttime'] and
            current.get('state') not in {'Z', 'X'} and not current.get('absent'))


class InstalledEvidence(Evidence):
    def __init__(self, args):
        super().__init__(args)
        self.context = json.loads(args.context_file.read_text())
        require(bool(self.context.get('provider_session_id')) and
                bool(self.context.get('recall_marker')), 'context needs provider_session_id and recall_marker')
        self.workspace = args.workspace.resolve(strict=True)
        require(self.workspace.is_dir(), 'workspace is not a directory')
        require(args.context_file.resolve() != self.workspace and
                self.workspace not in args.context_file.resolve().parents,
                'recall context must be outside the model workspace')
        self.marker = 'OAX-INSTALLED-' + secrets.token_hex(8)
        self.runs = []
        self.started_at = timestamp()

    def dispatch(self, content, intent, source_task=None):
        reference = {'backend_id': self.args.backend_id}
        if source_task:
            reference['source_task_id'] = source_task
        else:
            reference['provider_session_id'] = self.context['provider_session_id']
        receipt = self.api('/api/control/v1/tasks', {
            'target_agent_id': self.args.agent, 'organization_id': self.args.organization,
            'dispatch_mode': 'direct', 'intent': intent, 'content': content,
            'runtime_session': reference})
        task_id = receipt['task_id']
        self.tasks.append(task_id)
        self.save('created-tasks.json', self.tasks)
        return task_id

    def state(self, detail):
        task_id = detail['task']['id']
        path = self.args.runtime_state_dir / 'tasks' / (task_id + '.json')
        state = json.loads(path.read_text())
        self.save('runtime-state/' + task_id + '.json', state)
        require(state.get('task_id') == task_id, 'runtime state Task does not match')
        require(state.get('thread_id') == self.context['provider_session_id'], 'native thread changed')
        require(bool(state.get('turn_id')), 'runtime turn ID missing')
        runs = detail.get('run_attempts', [])
        require(len(runs) == 1 and state.get('run_id') == runs[0]['run_id'], 'runtime Run does not match')
        return state

    def checked_query(self, content, expected, source_task=None):
        detail = self.settle(self.dispatch(content, 'query', source_task))
        run = self.check_run(detail)
        self.runs.append(run)
        self.state(detail)
        require(detail['task']['status'] == 'succeeded' and
                detail['task']['completion_basis'] == 'query_result_delivered' and
                detail['task'].get('result', '').strip() == expected, 'query result mismatch')
        return detail

    def three_rounds(self):
        recall_prompt = self.context.get('recall_prompt',
            'Recall the verification code that was supplied in this native thread before it joined OpenAgentX.')
        require(self.context['recall_marker'] not in recall_prompt, 'recall question contains the expected answer')
        first = self.checked_query(
            'Do not call any tools or read files. ' + recall_prompt + ' Reply with exactly that code, then |, '
            'then the role verification code supplied in your current role instructions. No other text.',
            self.context['recall_marker'] + '|' + self.args.role_marker)
        tool_outputs = [event for event in first.get('events', [])
                        if any(kind in (event.get('output') or {}).get('text', '')
                               for kind in ['commandExecution', 'fileChange', 'mcpToolCall', 'dynamicToolCall'])]
        self.save('recall-no-tool-events.json', {'tool_outputs': tool_outputs,
                  'scope': 'No completed tool item in authoritative Task Journal'})
        require(not tool_outputs, 'recall query unexpectedly used a tool')
        output = self.workspace / (self.marker + '-result.txt')
        require(not output.exists(), 'output already exists')
        expected = (self.marker + '\n').encode()
        mutation = self.settle(self.dispatch(
            'Create ' + output.name + ' in the assigned workspace with exactly these bytes: ' +
            repr(expected) + '. Use a tool to write it once. Do not change other files. Report completion.',
            'mutation', first['task']['id']))
        run = self.check_run(mutation)
        self.runs.append(run)
        self.state(mutation)
        require(mutation['task']['status'] in {'succeeded', 'uncertain'}, 'mutation has unexpected status')
        actual = output.read_bytes()
        self.save('independent-file.json', {
            'path': str(output), 'actual_bytes': len(actual), 'sha256': sha(output),
            'expected_sha256': hashlib.sha256(expected).hexdigest(), 'exact': actual == expected,
            'task_status': mutation['task']['status'],
            'completion_basis': mutation['task'].get('completion_basis')})
        require(actual == expected, 'mutation file bytes mismatch')
        third = self.checked_query('Do not call tools. Reply with exactly: ' + self.marker + '-THIRD',
                                   self.marker + '-THIRD', mutation['task']['id'])
        return third['task']['id']

    def cancel_long_shell(self, source_task):
        directory = self.workspace / (self.marker + '-cancel')
        directory.mkdir(mode=0o700)
        script = directory / 'long_shell.py'
        # The child owns the delayed side effect; both real process identities
        # are persisted before the control-plane cancellation is requested.
        code = '''import json, os, subprocess, sys, time
from pathlib import Path
root = Path(__file__).resolve().parent
def identity(pid):
    fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(')', 1)[1].split()
    return {'pid': pid, 'starttime': fields[19], 'state': fields[0]}
with (root / 'invocations.txt').open('a') as f:
    f.write(str(os.getpid()) + '\\n')
started = time.time()
delay = DELAY_SECONDS
child_code = "import sys,time; from pathlib import Path; time.sleep(float(sys.argv[2])); Path(sys.argv[1]).write_text('late side effect')"
child = subprocess.Popen([sys.executable, '-c', child_code, str(root / 'late-sentinel.txt'), str(delay)])
data = {'started_epoch': started, 'write_due_epoch': started + delay,
        'parent': identity(os.getpid()), 'child': identity(child.pid)}
(root / 'started.tmp').write_text(json.dumps(data))
(root / 'started.tmp').replace(root / 'started.json')
raise SystemExit(child.wait())
'''.replace('DELAY_SECONDS', repr(self.args.cancel_delay))
        script.write_text(code)
        # Syntax-check the fixture without executing its delayed side effect.
        compile(code, str(script), 'exec')
        self.save('cancel/fixture.json', {'path': str(script), 'sha256': sha(script), 'source': code})
        task_id = self.dispatch(
            'Run exactly this command once in a shell tool: python3 ' + shlex.quote(str(script)) +
            '. Wait for its completion in the foreground; do not background, replace, edit, rerun or '
            'manually terminate the script. It is a bounded cancellation acceptance fixture. '
            'Do not call any other tools or change files yourself.', 'mutation', source_task)
        deadline = time.monotonic() + self.args.timeout
        started_path = directory / 'started.json'
        while not started_path.exists():
            detail = self.api('/api/observe/v1/tasks/' + task_id)
            require(detail['task']['status'] not in TERMINAL, 'long shell terminated before start marker')
            require(time.monotonic() < deadline, 'long shell did not create start marker')
            time.sleep(self.args.poll_interval)
        started = json.loads(started_path.read_text())
        before = {key: process_identity(started[key]['pid']) for key in ['parent', 'child']}
        self.save('cancel/processes-before.json', {'fixture': started, 'observed': before})
        require(all(same_live_process(started[key], before[key]) for key in before),
                'fixture processes not both alive before cancellation')
        require(started['write_due_epoch'] - time.time() >= 20, 'too little time remains for cancellation')
        detail = self.api('/api/observe/v1/tasks/' + task_id)
        self.save('cancel/task-before.json', detail)
        self.state(detail)
        self.save('cancel/receipt.json', self.api('/api/control/v1/tasks/' + task_id + '/cancel',
                  {'meta': {'expected_version': detail['task']['version']}}))
        canceled = self.settle(task_id)
        self.save('cancel/task-after.json', canceled)
        self.state(canceled)
        # Always wait beyond the original side-effect deadline, even when the
        # API reports failure/uncertainty, so first-failure effects are retained.
        while time.time() <= started['write_due_epoch'] + 5:
            time.sleep(min(self.args.poll_interval, max(0.1, started['write_due_epoch'] + 5 - time.time())))
        after = {key: process_identity(started[key]['pid']) for key in ['parent', 'child']}
        count = len((directory / 'invocations.txt').read_text().splitlines())
        sentinel = directory / 'late-sentinel.txt'
        result = {'at': timestamp(), 'fixture': started, 'observed': after,
                  'sentinel_exists': sentinel.exists(), 'invocation_count': count,
                  'checked_epoch': time.time(), 'task_status': canceled['task']['status']}
        self.save('cancel/independent-effects.json', result)
        require(canceled['task']['status'] == 'canceled', 'cancel Task did not become canceled')
        require(all(not same_live_process(started[key], after[key]) for key in after),
                'a fixture process survived cancellation')
        require(not sentinel.exists() and count == 1, 'late side effect or duplicate invocation observed')
        self.runs.extend(canceled.get('run_attempts', []))
        return task_id

    def collect(self):
        errors = []
        for name, path in [('overview-after.json', '/api/observe/v1/overview'),
                           ('mailbox-after.json', '/api/observe/v1/mailboxes?agent_id=' +
                            urllib.parse.quote(self.args.agent))]:
            try:
                self.save(name, self.api(path))
            except Exception as error:
                errors.append(str(error))
        for task_id in self.tasks:
            try:
                detail = self.api('/api/observe/v1/tasks/' + task_id)
                self.save('tasks/' + task_id + '/final-observation.json', detail)
                for run in detail.get('run_attempts', []):
                    self.save('tasks/' + task_id + '/' + run['run_id'] + '.json',
                              self.api('/api/observe/v1/run-attempts/' + run['run_id']))
            except Exception as error:
                errors.append(str(error))
        for index, unit in enumerate(self.args.unit):
            command = ['journalctl', '--user', '-u', unit, '--since', self.started_at,
                       '--no-pager', '-o', 'short-iso']
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.save(f'journal/{index:02}.json', {'unit': unit, 'command': command,
                      'returncode': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr})
            if result.returncode:
                errors.append('journal capture failed: ' + unit)
        self.save('collection-errors.json', errors)
        return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['url', 'agent', 'candidate', 'role-marker']:
        parser.add_argument('--' + name, required=True)
    for name in ['password-file', 'raw', 'evidence', 'workspace', 'context-file', 'runtime-state-dir']:
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--backend-id', default='codex')
    parser.add_argument('--adapter-id', default='codex-app-server')
    parser.add_argument('--username', default='owner')
    parser.add_argument('--organization', default='default')
    parser.add_argument('--unit', action='append', required=True,
                        help='Repeat for the dedicated Worker, daemon and all pre-existing active AGY Workers')
    parser.add_argument('--timeout', type=float, default=600)
    parser.add_argument('--poll-interval', type=float, default=3)
    parser.add_argument('--cancel-delay', type=float, default=90)
    args = parser.parse_args()
    parsed = urllib.parse.urlsplit(args.url)
    require(parsed.scheme in {'http', 'https'} and parsed.hostname in {'localhost', '127.0.0.1', '::1'} and
            not parsed.username and not parsed.password and not parsed.query and not parsed.fragment,
            'requires a credential-free loopback URL')
    require(args.timeout > 0 and 1 <= args.poll_interval <= 10 and 60 <= args.cancel_delay <= 180,
            'invalid timing bounds')
    require(len(set(args.unit)) == len(args.unit), 'duplicate service unit')
    os.umask(0o077)
    evidence = InstalledEvidence(args)
    verdict = {'status': 'FAIL', 'started_at': evidence.started_at, 'scope': 'Installed API and independent effects'}
    before = None
    try:
        evidence.save('manifest.json', {
            'candidate_claim': args.candidate, 'agent': args.agent, 'url': args.url,
            'workspace': str(evidence.workspace), 'command': [sys.executable, *sys.argv],
            'script_sha256': sha(__file__), 'base_harness_sha256': sha(Path(__file__).with_name('codex_workflow_e2e.py')),
            'context_file_sha256': sha(args.context_file), 'started_at': evidence.started_at,
            'excluded': ['installation provenance supplied separately', 'native PTY', 'browser', 'crash recovery']})
        before = evidence.provenance('before')
        evidence.api('/api/auth/v1/login', {'username': args.username, 'password': evidence.password}, login=True)
        evidence.save('overview-before.json', evidence.api('/api/observe/v1/overview'))
        third = evidence.three_rounds()
        canceled = evidence.cancel_long_shell(third)
        evidence.checked_query('Do not call tools. Reply with exactly: ' + evidence.marker + '-AFTER-CANCEL',
                               evidence.marker + '-AFTER-CANCEL', canceled)
        identities = {(run['worker_instance_id'], run.get('worker_generation')) for run in evidence.runs}
        require(len(identities) == 1, 'Worker identity changed during workflow')
        verdict.update({'status': 'PASS', 'tasks': evidence.tasks, 'worker_identity': list(identities)[0],
                        'cases': ['native-thread-recall-and-role', 'exact-mutation', 'third-round',
                                  'target-turn-cancel-processes-and-no-late-effect', 'next-query-after-cancel']})
    except Exception as error:
        verdict['error'] = evidence.redact(str(error))
    finally:
        try:
            after = evidence.provenance('after')
            require(before is not None and before == after, 'service process or binary changed during acceptance')
            verdict['service_identities_unchanged'] = True
        except Exception as error:
            verdict.update({'status': 'FAIL', 'provenance_error': evidence.redact(str(error))})
        try:
            errors = evidence.collect()
            if errors:
                verdict.update({'status': 'FAIL', 'collection_errors': errors})
        except Exception as error:
            verdict.update({'status': 'FAIL', 'collection_error': evidence.redact(str(error))})
        verdict['finished_at'] = timestamp()
        verdict['tasks'] = evidence.tasks
        evidence.save('verdict.json', verdict)
        evidence.finish()
    print(json.dumps(evidence.redact(verdict), ensure_ascii=False))
    return 0 if verdict['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
