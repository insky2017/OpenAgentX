#!/usr/bin/env python3
"""Formal API / real Codex workflow evidence; never starts or configures services.

Use observe to archive existing Task/Run/Journal facts without dispatching work.
Use workflow only with a dedicated, already joined Agent/workspace. The script
does not prove PTY/browser interaction, default network setup or crash recovery.
Collect those separately. No SQLite writes, fake runtime, or implicit retries.
"""
import argparse
import datetime
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


TERMINAL = {'succeeded', 'failed', 'uncertain', 'canceled'}
SENSITIVE = re.compile(r'password|token|cookie|csrf|authorization|fencing|secret', re.I)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def timestamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


class Evidence:
    def __init__(self, args):
        self.args = args
        self.raw = args.raw.resolve()
        self.public = args.evidence.resolve()
        require(self.raw != self.public and self.raw not in self.public.parents and
                self.public not in self.raw.parents, 'raw and public evidence must be separate directories')
        self.raw.mkdir(parents=True, mode=0o700, exist_ok=False)
        self.public.mkdir(parents=True, mode=0o700, exist_ok=False)
        self.password = args.password_file.read_text().strip()
        require(bool(self.password), 'empty password file')
        self.secrets = [self.password]
        self.jar = http.cookiejar.CookieJar()
        # The control plane endpoint is local, independently of Runtime proxy settings.
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf = ''
        self.request_count = 0
        self.tasks = []

    def redact(self, value):
        if isinstance(value, dict):
            return {k: '[REDACTED]' if SENSITIVE.search(k) else self.redact(v)
                    for k, v in value.items()}
        if isinstance(value, list):
            return [self.redact(v) for v in value]
        if isinstance(value, str):
            for secret in self.secrets:
                if secret:
                    value = value.replace(secret, '[REDACTED]')
            value = re.sub(r'(?i)(Bearer\s+)[^\s"\x27]+', r'\1[REDACTED]', value)
            value = re.sub(r'(https?://)[^\s/@]+:[^\s/@]+@', r'\1[REDACTED]@', value)
        return value

    def save(self, name, value):
        for root, data in [(self.raw, value), (self.public, self.redact(value))]:
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')

    def api(self, path, body=None, login=False):
        headers = {'Content-Type': 'application/json'}
        # The server marks cookies Secure even on its supported local HTTP
        # endpoint. This harness accepts only loopback URLs (checked in main),
        # and explicitly carries the authenticated jar there, like the CLI.
        if self.jar:
            headers['Cookie'] = '; '.join(cookie.name + '=' + cookie.value for cookie in self.jar)
        if body is not None and not login:
            body = dict(body)
            body['meta'] = dict(body.get('meta', {}))
            key = body['meta'].setdefault('idempotency_key', secrets.token_hex(16))
            headers.update({'X-CSRF-Token': self.csrf, 'Idempotency-Key': key})
        request = urllib.request.Request(self.args.url.rstrip('/') + path, headers=headers,
                                         data=None if body is None else json.dumps(body).encode())
        try:
            with self.http.open(request, timeout=20) as response:
                status, data = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, data = error.code, error.read()
        try:
            result = json.loads(data)
        except ValueError:
            result = {'body_text': data.decode(errors='replace')}
        if login and isinstance(result, dict):
            self.csrf = result.get('csrf_token', '')
            self.secrets.extend([self.csrf, *(cookie.value for cookie in self.jar)])
        self.request_count += 1
        # Authentication secrets/headers are unnecessary even in raw evidence.
        self.save(f'http/{self.request_count:05}.json', {
            'at': timestamp(), 'method': request.get_method(), 'path': path,
            'request': {'login': 'omitted'} if login else body, 'status': status,
            'response': self.redact(result) if login else result})
        require(status == 200, f'HTTP {status} on {path}; see saved response')
        return result

    def provenance(self, label):
        records = []
        for unit in self.args.unit:
            argv = ['systemctl', '--user', 'show', unit, '-p', 'MainPID', '-p', 'ActiveState',
                    '-p', 'SubState', '-p', 'FragmentPath', '-p', 'KillMode']
            result = subprocess.run(argv, capture_output=True, text=True, timeout=15)
            require(result.returncode == 0, f'cannot inspect service {unit}')
            properties = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
            pid = int(properties.get('MainPID', '0'))
            require(pid > 0 and properties.get('ActiveState') == 'active', f'service not active: {unit}')
            proc = Path('/proc') / str(pid)
            # Do not copy arbitrary argv/environ: they may contain credentials.
            fields = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
            records.append({'unit': unit, 'command': argv, 'properties': properties,
                            'pid': pid, 'starttime': fields[19],
                            'exe': os.readlink(proc / 'exe'), 'exe_sha256': sha(proc / 'exe')})
        self.save(label + '-service-provenance.json', {'at': timestamp(), 'services': records})
        return records

    def settle(self, task_id):
        require(bool(re.fullmatch(r'[A-Za-z0-9_-]+', task_id)), 'unsafe Task identifier')
        deadline = time.monotonic() + self.args.timeout
        while True:
            detail = self.api('/api/observe/v1/tasks/' + urllib.parse.quote(task_id, safe=''))
            self.save('tasks/' + task_id + '/latest.json', detail)
            if detail['task']['status'] in TERMINAL:
                for run in detail.get('run_attempts', []):
                    self.save('tasks/' + task_id + '/' + run['run_id'] + '.json',
                              self.api('/api/observe/v1/run-attempts/' + run['run_id']))
                return detail
            require(time.monotonic() < deadline, f'task {task_id} exceeded observation deadline')
            time.sleep(self.args.poll_interval)

    def create(self, content, intent):
        receipt = self.api('/api/control/v1/tasks', {
            'target_agent_id': self.args.agent, 'organization_id': self.args.organization,
            'dispatch_mode': 'direct', 'intent': intent, 'content': content})
        task_id = receipt['task_id']
        self.tasks.append(task_id)
        self.save('created-tasks.json', self.tasks)
        return self.settle(task_id)

    def check_run(self, detail):
        runs = detail.get('run_attempts', [])
        require(len(runs) == 1, 'expected exactly one Run; see task evidence')
        run = runs[0]
        require(run['adapter_id'] == self.args.adapter_id, 'unexpected Runtime adapter')
        require(run['status'] == 'succeeded', 'Runtime did not succeed')
        result = run.get('turn_result') or {}
        require(result.get('final_reply') is True and not result.get('body_truncated') and
                not result.get('error'), 'missing complete final reply')
        require(detail.get('events'), 'Task detail has no Journal evidence')
        return run

    def workflow(self):
        workspace = self.args.workspace.resolve(strict=True)
        require(workspace.is_dir(), 'workspace must be a directory')
        marker = 'OAX-CODEX-' + secrets.token_hex(8)
        input_name, output_name = marker + '-input.txt', marker + '-result.txt'
        with (workspace / input_name).open('x') as file:
            file.write(marker + '\n')
        role = self.args.role_marker
        question = f'Read {input_name} in the assigned workspace. Reply with only its contents, without a trailing newline.'
        expected = marker
        if role:
            question = (f'Read {input_name} in the assigned workspace. Reply with its contents (without newline), '
                        'then | then the role verification code supplied in your role instructions. Do not read role files.')
            expected += '|' + role
        first = self.create(question, 'query')
        first_run = self.check_run(first)
        require(first['task']['status'] == 'succeeded' and
                first['task']['completion_basis'] == 'query_result_delivered' and
                first['task'].get('result', '').strip() == expected, 'query result mismatch')
        expected_bytes = (marker + '\n').encode()
        mutation = self.create(f'Create {output_name} in the assigned workspace with exactly the bytes '
                               f'{expected_bytes!r}. Write once using a tool. Do not change other files. Report completion.',
                               'mutation')
        mutation_run = self.check_run(mutation)
        require(mutation['task']['status'] in {'succeeded', 'uncertain'}, 'unexpected mutation Task status')
        actual = (workspace / output_name).read_bytes()
        self.save('independent-file.json', {'path': str(workspace / output_name),
                  'actual_bytes': len(actual), 'sha256': hashlib.sha256(actual).hexdigest(),
                  'expected_sha256': hashlib.sha256(expected_bytes).hexdigest(),
                  'exact': actual == expected_bytes})
        require(actual == expected_bytes, 'independent output bytes mismatch')
        last = self.create('Do not call tools. Reply with exactly this text: ' + marker + '-NEXT', 'query')
        last_run = self.check_run(last)
        require(last['task']['status'] == 'succeeded' and
                last['task']['completion_basis'] == 'query_result_delivered' and
                last['task'].get('result', '').strip() == marker + '-NEXT', 'next query result mismatch')
        identities = {(run['worker_instance_id'], run.get('worker_generation'))
                      for run in [first_run, mutation_run, last_run]}
        require(len(identities) == 1, 'Worker changed during consecutive workflow')
        return {'cases': ['query-role-file' if role else 'query-file', 'exact-file-write', 'same-worker-next-query'],
                'tasks': self.tasks, 'worker': first_run['worker_instance_id'],
                'generation': first_run.get('worker_generation')}

    def finish(self):
        for root in [self.raw, self.public]:
            files = sorted(p for p in root.rglob('*') if p.is_file() and p.name != 'SHA256SUMS')
            if root == self.public:
                for path in files:
                    require(not any(secret.encode() in path.read_bytes() for secret in self.secrets if secret),
                            'credential found in public evidence')
            (root / 'SHA256SUMS').write_text(''.join(sha(p) + '  ' + str(p.relative_to(root)) + '\n' for p in files))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['observe', 'workflow'])
    for name in ['url', 'agent', 'candidate', 'adapter-id']:
        parser.add_argument('--' + name, required=True)
    for name in ['password-file', 'raw', 'evidence']:
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--username', default='owner')
    parser.add_argument('--organization', default='default')
    parser.add_argument('--workspace', type=Path)
    parser.add_argument('--role-marker')
    parser.add_argument('--task', action='append', default=[])
    parser.add_argument('--unit', action='append', default=[])
    parser.add_argument('--timeout', type=float, default=600)
    parser.add_argument('--poll-interval', type=float, default=5)
    args = parser.parse_args()
    parsed = urllib.parse.urlsplit(args.url)
    require(parsed.scheme in {'http', 'https'} and parsed.hostname in {'localhost', '127.0.0.1', '::1'} and
            not parsed.username and not parsed.password and not parsed.query and not parsed.fragment,
            'this installed harness requires a credential-free loopback URL')
    require(args.timeout > 0 and 1 <= args.poll_interval <= 60, 'invalid timing bounds')
    require(args.workspace is not None if args.mode == 'workflow' else bool(args.task),
            'workflow requires --workspace; observe requires --task')
    os.umask(0o077)
    evidence = Evidence(args)
    result = {'status': 'FAIL', 'mode': args.mode, 'started_at': timestamp()}
    try:
        evidence.save('manifest.json', {'candidate_claim': args.candidate, 'agent': args.agent,
                      'command': [sys.executable, *sys.argv],
                      'adapter_expected': args.adapter_id, 'url': args.url, 'mode': args.mode,
                      'script_sha256': sha(__file__), 'started_at': result['started_at'],
                      'scope': 'Formal API and independent filesystem effects. Candidate installation, native PTY, '
                               'browser, proxy routing and crash cases require separate evidence. No service/config changes.'})
        before = evidence.provenance('before')
        evidence.api('/api/auth/v1/login', {'username': args.username, 'password': evidence.password}, login=True)
        evidence.save('overview-before.json', evidence.api('/api/observe/v1/overview'))
        if args.mode == 'workflow':
            result.update(evidence.workflow())
        else:
            result['observed_tasks'] = {}
            for task_id in args.task:
                detail = evidence.settle(task_id)
                require(detail['task']['target_agent_id'] == args.agent, 'observed Task belongs to another Agent')
                result['observed_tasks'][task_id] = detail['task']['status']
            result['scope'] = 'Observation completed; does not assert task success or runtime effects'
        evidence.save('overview-after.json', evidence.api('/api/observe/v1/overview'))
        evidence.save('mailbox-after.json', evidence.api('/api/observe/v1/mailboxes?agent_id=' + urllib.parse.quote(args.agent)))
        after = evidence.provenance('after')
        require(before == after, 'service process or binary changed during capture')
        result['status'] = 'PASS'
    except Exception as error:
        result['error'] = evidence.redact(str(error))
    finally:
        result['finished_at'] = timestamp()
        evidence.save('verdict.json', result)
        evidence.finish()
    print(json.dumps(evidence.redact(result), ensure_ascii=False))
    return 0 if result['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
