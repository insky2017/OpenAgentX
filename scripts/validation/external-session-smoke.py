#!/usr/bin/env python3
"""Real, isolated external-message CLI/API smoke; no model or Worker is started.

All state mutations use the real binary or HTTP API. SQLite is opened read-only
solely to independently count tasks/runs/workers. A unique --root preserves each
failure; reruns must use another directory. Secrets live outside raw/exported
evidence. This is communication API evidence, NOT original-session E04/E05.
"""
import argparse
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import pty
import re
import secrets
import selectors
import signal
import socket
import sqlite3
import subprocess
import sys
import time


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=10)
        self.path = str(path)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


class Smoke:
    def __init__(self, args):
        self.args = args
        self.root = args.root.resolve()
        self.root.mkdir(parents=True, mode=0o700, exist_ok=False)
        self.raw = self.root / 'raw'
        self.private = self.root / 'private'
        self.raw.mkdir(mode=0o700)
        self.private.mkdir(mode=0o700)
        self.binary = args.binary.resolve()
        self.db = self.private / 'openagentx.db'
        # Unix paths have a small platform limit; never reuse a live socket.
        self.socket = Path('/tmp') / ('oax-external-smoke-' + secrets.token_hex(8) + '.sock')
        self.owner_file = self.private / 'owner.json'
        self.password = secrets.token_urlsafe(32)
        (self.private / 'owner-password').write_text(self.password)
        self.secret_values = [self.password]
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith(('OPENAGENTX_', 'AGY_')) and 'proxy' not in k.lower()}
        self.env.update(OPENAGENTX_HOME=str(self.private / 'profile'), TERM='xterm-256color')
        self.process = None
        self.daemon_number = 0
        self.sequence = 0
        self.checks = []
        suffix = secrets.token_hex(4)
        self.agents = ('external-smoke-a-' + suffix, 'external-smoke-b-' + suffix)

    def redact(self, value):
        if isinstance(value, dict):
            return {k: '[REDACTED]' if k.lower() in ('token', 'password', 'authorization', 'cookie', 'csrf_token')
                    else self.redact(v) for k, v in value.items()}
        if isinstance(value, list):
            return [self.redact(v) for v in value]
        if isinstance(value, str):
            for secret in self.secret_values:
                value = value.replace(secret, '[REDACTED]')
            value = re.sub(r'oax_ext_[A-Za-z0-9_-]{43}', '[REDACTED]', value)
            value = re.sub(r'(?i)(Bearer\s+)[^\s"\x27]+', r'\1[REDACTED]', value)
        return value

    def save(self, name, value):
        path = self.raw / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(self.redact(value), ensure_ascii=False, indent=2) + '\n')

    def cli(self, label, args, expected=0, interactive=False):
        argv = [str(self.binary), *map(str, args)]
        started = now()
        if not interactive:
            result = subprocess.run(argv, env=self.env, capture_output=True, text=True, timeout=30)
            code, stdout, stderr = result.returncode, result.stdout, result.stderr
        else:
            master, slave = pty.openpty()
            process = subprocess.Popen(argv, env=self.env, stdin=slave, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, start_new_session=True)
            os.close(slave)
            selector = selectors.DefaultSelector()
            selector.register(process.stdout, selectors.EVENT_READ, 'stdout')
            selector.register(process.stderr, selectors.EVENT_READ, 'stderr')
            chunks = {'stdout': bytearray(), 'stderr': bytearray()}
            prompt_buffer = b''
            deadline = time.monotonic() + 30
            try:
                while selector.get_map() and time.monotonic() < deadline:
                    for key, _ in selector.select(.2):
                        chunk = os.read(key.fileobj.fileno(), 65536)
                        if not chunk:
                            selector.unregister(key.fileobj)
                            continue
                        chunks[key.data].extend(chunk)
                        prompt_buffer += chunk
                        match = re.search(rb'(?i)(password|username)[^:\r\n]*:', prompt_buffer)
                        if match:
                            answer = self.password if match.group(1).lower() == b'password' else 'owner'
                            os.write(master, (answer + '\n').encode())
                            prompt_buffer = prompt_buffer[match.end():]
                if selector.get_map() and process.poll() is None:
                    process.terminate()
                code = process.wait(timeout=5)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                selector.close()
                os.close(master)
            stdout = chunks['stdout'].decode(errors='replace')
            stderr = chunks['stderr'].decode(errors='replace')
        self.sequence += 1
        self.save(f'cli/{self.sequence:03}-{label}.json', {
            'started_at': started, 'finished_at': now(), 'argv': argv,
            'exit_code': code, 'stdout': stdout, 'stderr': stderr,
            'stdin': 'private PTY credentials omitted' if interactive else 'none'})
        require(code == expected, f'{label}: exit {code}, expected {expected}; see CLI evidence')
        if expected == 0 and stdout.strip().startswith(('{', '[')):
            return json.loads(stdout)
        return stdout + stderr

    def external(self, label, command, agent, *args, expected=0):
        return self.cli(label, ['external', command, '--agent', agent, '--socket', self.socket,
                               '--credentials', self.owner_file, '--session-file', self.private / (agent + '.json'),
                               *args], expected=expected)

    def token(self, agent):
        document = json.loads((self.private / (agent + '.json')).read_text())
        selected = [c for c in document['credentials'] if c['username'] == agent
                    and c['socket_path'] == str(self.socket)]
        require(len(selected) == 1, 'expected one isolated credential')
        token = selected[0]['token']
        self.secret_values.append(token)
        return token

    def api(self, label, method, path, token, body=None, expected=200):
        connection = UnixHTTP(self.socket)
        headers = {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token}
        if body and 'idempotency_key' in body:
            headers['Idempotency-Key'] = body['idempotency_key']
        try:
            connection.request(method, '/api/external/v1/' + path,
                               None if body is None else json.dumps(body), headers)
            response = connection.getresponse()
            status, data = response.status, response.read().decode()
        finally:
            connection.close()
        self.sequence += 1
        result = json.loads(data)
        self.save(f'http/{self.sequence:03}-{label}.json', {'at': now(), 'method': method,
                  'path': '/api/external/v1/' + path, 'request': body, 'authorization': 'omitted',
                  'status': status, 'response': result})
        require(status == expected, f'{label}: HTTP {status}, expected {expected}; see HTTP evidence')
        return result

    def start(self):
        self.daemon_number += 1
        label = 'daemon-' + str(self.daemon_number)
        argv = [str(self.binary), 'serve', '--db', str(self.db), '--socket', str(self.socket),
                '--http-addr', '127.0.0.1:0', '--web-dir', str(self.args.web.resolve())]
        handles = [(self.raw / (label + '.' + stream)).open('wb') for stream in ('stdout', 'stderr')]
        self.process = subprocess.Popen(argv, env=self.env, stdout=handles[0], stderr=handles[1],
                                        start_new_session=True)
        for handle in handles:
            handle.close()
        proc = Path('/proc') / str(self.process.pid)
        fields = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
        self.save(label + '-start.json', {'at': now(), 'argv': argv, 'pid': self.process.pid,
                  'starttime': fields[19], 'exe_sha256': sha(proc / 'exe')})
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            require(self.process.poll() is None, 'isolated daemon exited before readiness')
            connection = UnixHTTP(self.socket)
            try:
                connection.request('GET', '/api/external/v1/status')
                response = connection.getresponse()
                response.read()
                if response.status == 401:
                    return
            except (OSError, http.client.HTTPException):
                pass
            finally:
                connection.close()
            time.sleep(.2)
        raise RuntimeError('isolated daemon readiness timed out')

    def stop(self):
        if self.process is None:
            return
        process, self.process = self.process, None
        if process.poll() is None:
            process.send_signal(signal.SIGTERM)
        try:
            code = process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            code = process.wait(timeout=5)
        self.save(f'daemon-{self.daemon_number}-stop.json', {'at': now(), 'pid': process.pid,
                  'exit_code': code, 'alive_after': process.poll() is None})

    def counts(self, label):
        # Independent observation only. No fixtures or writes via SQLite.
        connection = sqlite3.connect(self.db.as_uri() + '?mode=ro', uri=True)
        try:
            counts = {table: connection.execute('SELECT count(*) FROM ' + table).fetchone()[0]
                      for table in ('tasks', 'run_attempts', 'worker_instances', 'external_messages')}
            self.save(label + '.json', {'at': now(), 'mode': 'sqlite read-only', 'counts': counts})
            return counts
        finally:
            connection.close()

    def upgrade_copy(self):
        source = self.args.upgrade_source_db.resolve()
        require(source.is_file() and source != self.db, 'upgrade source must be an existing separate database')
        directory = self.private / 'upgrade'
        directory.mkdir()
        destination = directory / 'openagentx.db'
        live = sqlite3.connect(source.as_uri() + '?mode=ro', uri=True)
        copy = sqlite3.connect(destination)
        try:
            live.backup(copy)
        finally:
            copy.close()
            live.close()

        def snapshot():
            db = sqlite3.connect(destination.as_uri() + '?mode=ro', uri=True)
            try:
                version = db.execute('SELECT version FROM schema_meta WHERE singleton=1').fetchone()[0]
                tables = {}
                names = [r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
                for name in names:
                    if name == 'schema_meta' or name.startswith('sqlite_'):
                        continue
                    quoted = '"' + name.replace('"', '""') + '"'
                    hashes = sorted(hashlib.sha256(repr(row).encode()).digest()
                                    for row in db.execute('SELECT * FROM ' + quoted))
                    tables[name] = {'rows': len(hashes), 'rowset_sha256': hashlib.sha256(b''.join(hashes)).hexdigest()}
                return {'schema_version': version, 'tables': tables}
            finally:
                db.close()

        before = snapshot()
        self.save('upgrade-before.json', {'source': str(source), 'source_open_mode': 'ro',
                  'backup': str(destination), 'backup_sha256': sha(destination), 'snapshot': before})
        require(before['schema_version'] == 2, 'upgrade evidence requires an actual schema v2 source')
        self.cli('upgrade-copy-schema-verify', ['schema', 'verify', '--db', destination])
        after = snapshot()
        self.save('upgrade-after.json', {'snapshot': after, 'backup_sha256': sha(destination)})
        require(after['schema_version'] == 3, 'candidate must migrate isolated v2 copy to v3')
        for name, fingerprint in before['tables'].items():
            require(after['tables'].get(name) == fingerprint, 'upgrade changed existing table data: ' + name)
        self.checks.append('real-v2-readonly-backup-upgraded-to-v3-old-rowsets-preserved')

    def run(self):
        self.save('provenance.json', {'at': now(), 'binary': str(self.binary), 'binary_sha256': sha(self.binary),
                  'harness_sha256': sha(__file__), 'source_commit': self.args.commit,
                  'scope': 'real isolated communication CLI/API; no original-session E04/E05 claim',
                  'socket': str(self.socket), 'database': str(self.db)})
        if self.args.upgrade_source_db:
            self.upgrade_copy()
        self.cli('external-help', ['external', '--help'])
        self.cli('init', ['init', '--db', self.db], interactive=True)
        for agent in self.agents:
            workspace = self.private / agent
            workspace.mkdir()
            (workspace / 'ROLE.md').write_text('Isolated communication API fixture. No model execution.\n')
            identity = workspace / 'identity.yaml'
            identity.write_text(f'version: 1\nagent_id: {agent}\nprincipal_id: agent-{agent}\n'
                                f'organization_id: default\ndisplay_name: External communication smoke\n'
                                f'profile:\n  instructions_path: ROLE.md\n  workspace_root: {workspace}\n'
                                f'  capabilities: [communication-api-verification]\n')
            self.cli('apply-' + agent, ['agent', 'apply', '--db', self.db, '--file', identity], interactive=True)
        self.start()
        self.cli('owner-login', ['console', 'login', '--socket', self.socket,
                                '--credentials', self.owner_file], interactive=True)
        owner_document = json.loads(self.owner_file.read_text())
        self.secret_values.extend(c['token'] for c in owner_document['credentials'])
        a, b = self.agents
        bindings = {}
        for agent, peer in ((a, b), (b, a)):
            result = self.external('bind-' + agent, 'bind', agent, '--host', 'isolated-test-host',
                                   '--thread', 'isolated-original-' + agent, '--peers', peer)
            require(result['automatic_delivery'] is False, 'binding must not claim automatic delivery')
            bindings[agent] = result['binding']
            require(bindings[agent]['generation'] == 1, 'first binding generation')
        token_a, token_b = self.token(a), self.token(b)
        self.checks.append('owner-login-and-two-bindings')
        request = self.private / 'request.txt'
        request.write_text('OAX_EXTERNAL_SMOKE_CONSULTATION\n')
        changed = self.private / 'changed.txt'
        changed.write_text('OAX_EXTERNAL_SMOKE_CHANGED\n')
        reply_file = self.private / 'reply.txt'
        reply_file.write_text('OAX_EXTERNAL_SMOKE_RESULT\n')
        first = self.external('send', 'send', a, '--to', b, '--key', 'consultation-1', '--content-file', request)
        message_id = first['message_id']
        require(first['sender_agent_id'] == a and first['target_agent_id'] == b
                and first['kind'] == 'consultation' and first['delivery_state'] == 'pending', 'send attribution/state')
        same = self.external('same-key-same-content', 'send', a, '--to', b, '--key', 'consultation-1', '--content-file', request)
        require(same == first, 'idempotent send must return identical original')
        conflict = self.external('same-key-conflict', 'send', a, '--to', b, '--key', 'consultation-1',
                                 '--content-file', changed, expected=1)
        require('HTTP 409' in conflict, 'changed payload must conflict')
        inbox = self.external('inbox-before-ack', 'inbox', b)
        require([m['message_id'] for m in inbox['messages']] == [message_id], 'single durable inbox message')
        status = self.external('read-does-not-ack', 'status', a, '--message', message_id)
        require(status['delivery_state'] == 'pending' and not status.get('acknowledged_at'), 'inbox read must not acknowledge')
        self.api('wrong-token', 'GET', 'status', 'invalid-smoke-credential', expected=401)
        self.api('wrong-peer', 'POST', 'messages', token_a, {
            'target_agent_id': a, 'kind': 'consultation', 'content': 'forbidden peer',
            'idempotency_key': 'wrong-peer'}, expected=403)
        self.api('sender-cannot-ack', 'POST', 'messages/' + message_id + '/ack', token_a, {}, expected=403)
        self.checks.extend(['send-inbox-status', 'idempotent-send-and-conflict', 'read-is-not-ack', 'wrong-token-peer-and-ack-denied'])
        acknowledged = self.external('recipient-ack', 'ack', b, '--message', message_id)
        require(acknowledged['delivery_state'] == 'acknowledged' and acknowledged.get('acknowledged_at'), 'recipient ack')
        require(self.external('repeated-ack', 'ack', b, '--message', message_id) == acknowledged, 'idempotent ack')
        require(not self.external('pending-inbox-after-ack', 'inbox', b)['messages'], 'default inbox hides acknowledged')
        require(len(self.external('all-inbox-after-ack', 'inbox', b, '--all')['messages']) == 1, 'all inbox preserves history')
        result = self.external('reply', 'reply', b, '--message', message_id, '--content-file', reply_file)
        result_id = result['message_id']
        require(result['kind'] == 'result' and result['reply_to_message_id'] == message_id
                and result['sender_agent_id'] == b and result['target_agent_id'] == a, 'result correlation and derived target')
        require(self.external('repeat-reply', 'reply', b, '--message', message_id,
                              '--content-file', reply_file) == result, 'idempotent reply')
        denied = self.external('no-reply-to-result', 'reply', a, '--message', result_id,
                               '--content-file', reply_file, expected=1)
        require('HTTP 400' in denied, 'result may not request another result')
        self.api('api-result-observation', 'GET', 'messages/' + result_id, token_a)
        require(len(self.external('recipient-result-inbox', 'inbox', a)['messages']) == 1, 'result appears in sender inbox')
        self.checks.extend(['ack-and-history', 'reply-correlation-and-idempotence', 'result-cannot-be-replied'])
        pending = self.external('request-before-restart', 'send', b, '--to', a, '--kind', 'request',
                                '--key', 'request-2', '--content-file', request)
        require(pending['kind'] == 'request', 'explicit request kind')
        # Each NUL is six JSON wire bytes; this exercises the 64KiB content
        # contract through its worst escaped representation, not a small ASCII body.
        large_text = 'x' + '\x00' * 65535
        large_file = self.private / 'large-content.txt'
        large_file.write_text(large_text)
        large = self.external('64k-worst-escaping-send', 'send', a, '--to', b, '--key', 'large-1',
                              '--content-file', large_file)
        require(large['content'] == large_text, '64KiB content must survive worst JSON escaping')
        large_read = self.external('64k-worst-escaping-inbox', 'inbox', b)
        require(len(large_read['messages']) == 1 and large_read['messages'][0]['content'] == large_text,
                '64KiB inbox content must remain complete')
        self.api('over-64k-api-rejected', 'POST', 'messages', token_a, {
            'target_agent_id': b, 'kind': 'consultation', 'content': large_text + 'x',
            'idempotency_key': 'too-large'}, expected=400)
        self.checks.append('64k-worst-json-escaping-and-oversize-api-rejection')
        before = self.counts('counts-before-restart')
        require(before == {'tasks': 0, 'run_attempts': 0, 'worker_instances': 0, 'external_messages': 4}, 'messages must not create runtime work')
        self.stop()
        self.start()
        for agent in self.agents:
            require(self.external('persisted-binding-' + agent, 'status', agent) == bindings[agent], 'binding/token persistence')
        require(self.external('owner-after-restart', 'status', a, '--owner') == bindings[a], 'owner credential persistence')
        require(self.external('ack-after-restart', 'status', a, '--message', message_id) == acknowledged, 'ack state persistence')
        after_inbox = self.external('inbox-after-restart', 'inbox', a)
        require([m['message_id'] for m in after_inbox['messages']] == [result_id, pending['message_id']], 'pending inbox persistence/order')
        retried = self.external('retry-after-restart', 'send', a, '--to', b, '--key', 'consultation-1', '--content-file', request)
        require(retried == acknowledged, 'idempotent history survives restart')
        require(self.counts('counts-after-restart') == before, 'restart/retry must not duplicate messages or spawn work')
        self.cli('schema-verify', ['schema', 'verify', '--db', self.db])
        self.checks.extend(['request-kind', 'restart-binding-ack-inbox-idempotency-persistence', 'no-tasks-runs-workers', 'schema-verify'])

    def finish(self, error):
        self.stop()
        self.save('result.json', {'at': now(), 'status': 'PASS' if error is None else 'FAIL',
                  'checks_passed': self.checks, 'error': None if error is None else str(error),
                  'boundary': 'Communication API only. No model, Worker, original Desktop session, automatic wakeup, E04 or E05 proof.'})
        entries = []
        public = self.args.evidence.resolve() if self.args.evidence else None
        if public:
            require(public != self.root and self.root not in public.parents and public not in self.root.parents,
                    'export must be separate from the private root')
            public.mkdir(parents=True, exist_ok=False)
        for path in sorted(self.raw.rglob('*')):
            if not path.is_file():
                continue
            # Raw excludes credentials; exact-value scrub also guards daemon logs.
            data = self.redact(path.read_text(errors='replace')).encode()
            path.write_bytes(data)
            relative = path.relative_to(self.raw)
            if public:
                destination = public / relative
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(data)
            entries.append({'path': str(relative), 'raw_sha256': sha(path), 'sha256': hashlib.sha256(data).hexdigest()})
        manifest = {'raw_root': str(self.raw), 'generated_at': now(), 'files': entries,
                    'secrets': 'credential files and authentication input excluded from raw and export'}
        for directory in (self.raw, public):
            if directory:
                (directory / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        print(json.dumps({'status': 'PASS' if error is None else 'FAIL', 'raw': str(self.raw),
                          'evidence': str(public) if public else None, 'checks_passed': len(self.checks)}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--root', type=Path, required=True, help='new private persistent directory per attempt')
    parser.add_argument('--web', type=Path, default=Path('web/dist'))
    parser.add_argument('--evidence', type=Path, help='new separate directory for sanitized evidence export')
    parser.add_argument('--commit', default='uncommitted candidate; see binary hash')
    parser.add_argument('--upgrade-source-db', type=Path,
                        help='optional actual v2 database; opened read-only for backup; only isolated copy migrates')
    args = parser.parse_args()
    os.umask(0o077)
    smoke = Smoke(args)
    error = None
    try:
        smoke.run()
    except Exception as failure:
        error = failure
    finally:
        smoke.finish(error)
    return 1 if error else 0


if __name__ == '__main__':
    sys.exit(main())
