#!/usr/bin/env python3
"""Verify external messages on an ALREADY RUNNING installed OAX service.

Creates only two random test Agent identities via the official CLI. Never
installs, starts, stops or restarts any service or Worker, and never uses real
business Agent credentials. SQLite observations are read-only and restricted
to these test identities. See external-session-smoke.py for broader isolation
and restart coverage; this script does not repeat that matrix or prove E04/E05.
"""
import argparse
from collections import Counter
import importlib.util
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess
import sys


spec = importlib.util.spec_from_file_location('external_smoke', Path(__file__).with_name('external-session-smoke.py'))
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)
require, now, sha = helpers.require, helpers.now, helpers.sha


class Installed(helpers.Smoke):
    def __init__(self, args):
        self.args = args
        self.root = args.root.resolve()
        self.root.mkdir(parents=True, mode=0o700, exist_ok=False)
        self.raw, self.private = self.root / 'raw', self.root / 'private'
        self.raw.mkdir(mode=0o700)
        self.private.mkdir(mode=0o700)
        self.binary, self.db, self.socket = args.binary.resolve(), args.db.resolve(), args.socket.resolve()
        self.owner_file = args.credentials.resolve()
        require(args.password_file.stat().st_mode & 0o077 == 0, 'password source must have private permissions')
        self.password = args.password_file.read_text().strip()
        require(bool(self.password), 'owner password file is empty')
        document = json.loads(self.owner_file.read_text())
        selection = document.get('current', {}).get(str(self.socket), {})
        matches = [c for c in document['credentials'] if c['socket_path'] == str(self.socket)
                   and c['username'] == selection.get('username', 'owner')
                   and c['installation_id'] == selection.get('installation_id', c['installation_id'])]
        require(len(matches) == 1, 'expected exactly one selected owner credential')
        self.owner = matches[0]
        self.secret_values = [self.password, *(c['token'] for c in document['credentials'])]
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith(('OPENAGENTX_', 'AGY_')) and 'proxy' not in k.lower()}
        self.env.update(OPENAGENTX_HOME=str(self.private / 'profile'), TERM='xterm-256color')
        self.sequence = 0
        self.checks = []
        suffix = secrets.token_hex(5)
        self.agents = ('external-installed-a-' + suffix, 'external-installed-b-' + suffix)

    def start(self):
        raise RuntimeError('installed verifier cannot start services')

    def stop(self):
        # The shared service belongs to the user, never to this verifier.
        pass

    def services(self, label):
        records = []
        for unit in self.args.unit:
            argv = ['systemctl', '--user', 'show', unit, '-p', 'MainPID', '-p', 'ActiveState',
                    '-p', 'SubState', '-p', 'ExecMainStartTimestampMonotonic']
            response = subprocess.run(argv, text=True, capture_output=True, timeout=10)
            require(response.returncode == 0, 'cannot observe installed service ' + unit)
            properties = dict(line.split('=', 1) for line in response.stdout.splitlines() if '=' in line)
            pid = int(properties.get('MainPID', '0'))
            require(pid > 0 and properties.get('ActiveState') == 'active', 'service is not active: ' + unit)
            fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
            records.append({'unit': unit, 'argv': argv, 'exit_code': response.returncode,
                            'stdout': response.stdout, 'stderr': response.stderr,
                            'properties': properties, 'pid': pid, 'starttime': fields[19],
                            'exe_sha256': sha(f'/proc/{pid}/exe')})
        self.save(label + '.json', records)
        return records

    def managed_task_rejected(self, agent):
        key = 'installed-external-reject-' + secrets.token_hex(8)
        body = {'meta': {'idempotency_key': key}, 'target_agent_id': agent,
                'organization_id': self.args.organization, 'dispatch_mode': 'direct', 'intent': 'query',
                'content': 'Isolated API guard verification. Do not run tools or perform any business operation.'}
        connection = helpers.UnixHTTP(self.socket)
        try:
            connection.request('POST', '/api/control/v1/tasks', json.dumps(body), {
                'Content-Type': 'application/json', 'Authorization': 'Bearer ' + self.owner['token'],
                'Idempotency-Key': key})
            response = connection.getresponse()
            status, data = response.status, response.read().decode()
        finally:
            connection.close()
        self.save('managed-task-rejection.json', {'at': now(), 'method': 'POST', 'path': '/api/control/v1/tasks',
                  'request': body, 'authorization': 'omitted', 'status': status, 'response_body': data})
        # Existing Panel transports domain conflict as 400. Require the exact
        # guard reason; an arbitrary parse/auth error does not prove exclusion.
        require(status == 400 and 'external session uses messages, not managed tasks' in data,
                'managed Task must be rejected by the external-session guard, not an unrelated error')

    def observe(self, label):
        db = sqlite3.connect(self.db.as_uri() + '?mode=ro', uri=True)
        db.row_factory = sqlite3.Row
        try:
            require(db.execute('SELECT version FROM schema_meta WHERE singleton=1').fetchone()[0] == 3,
                    'installed verification requires schema v3; never migrates the source database')
            a, b = self.agents
            counts = {}
            for table, column in (('tasks', 'target_agent_id'), ('messages', 'target_agent_id'),
                                  ('mailbox_items', 'target_agent_id'), ('run_attempts', 'agent_id'),
                                  ('worker_instances', 'agent_id')):
                counts[table] = db.execute(f'SELECT count(*) FROM {table} WHERE {column} IN (?,?)', (a, b)).fetchone()[0]
            messages = [dict(row) for row in db.execute(
                'SELECT message_id,sequence,sender_agent_id,target_agent_id,kind,reply_to_message_id,delivery_state,acknowledged_at '
                'FROM external_messages WHERE sender_agent_id IN (?,?) OR target_agent_id IN (?,?) ORDER BY sequence', (a, b, a, b))]
            journal = [dict(row) for row in db.execute(
                'SELECT sequence,event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at '
                'FROM event_journal WHERE aggregate_id IN (?,?) OR instr(payload_json,?)>0 OR instr(payload_json,?)>0 ORDER BY sequence',
                (a, b, a, b))]
            result = {'mode': 'sqlite read-only, test identities only', 'agents': self.agents,
                      'counts': counts, 'external_messages': messages, 'journal': journal}
            self.save(label + '.json', result)
            return result
        finally:
            db.close()

    def run(self):
        self.save('provenance.json', {'at': now(), 'binary': str(self.binary), 'binary_sha256': sha(self.binary),
                  'source_commit': self.args.commit, 'harness_sha256': sha(__file__),
                  'helper_sha256': sha(Path(__file__).with_name('external-session-smoke.py')),
                  'scope': 'installed CLI/API, random test identities only; no service lifecycle or original-session E04/E05',
                  'agents': self.agents, 'socket': str(self.socket), 'database': str(self.db)})
        before_services = self.services('services-before')
        require(all(x['exe_sha256'] == sha(self.binary) for x in before_services), 'installed processes must match tested binary')
        self.observe('before')
        for agent in self.agents:
            workspace = self.private / agent
            workspace.mkdir()
            (workspace / 'ROLE.md').write_text('Installed communication API fixture only. No Worker or model execution.\n')
            identity = workspace / 'identity.yaml'
            identity.write_text(f'version: 1\nagent_id: {agent}\nprincipal_id: agent-{agent}\n'
                                f'organization_id: {self.args.organization}\ndisplay_name: Installed communication verification\n'
                                f'profile:\n  instructions_path: ROLE.md\n  workspace_root: {workspace}\n'
                                '  capabilities: [communication-api-verification]\n')
            self.cli('apply-' + agent, ['agent', 'apply', '--db', self.db, '--file', identity,
                                       '--owner-username', self.owner['username']], interactive=True)
        a, b = self.agents
        for agent, peer in ((a, b), (b, a)):
            result = self.external('bind-' + agent, 'bind', agent, '--host', 'installed-test-only',
                                   '--thread', 'isolated-' + agent, '--peers', peer)
            require(result['automatic_delivery'] is False, 'binding must not claim automatic delivery')
            self.token(agent)  # Register secrets for redaction; never print/store them in evidence.
        self.checks.append('installed-owner-authorized-apply-and-bind')
        request_file, result_file = self.private / 'request.txt', self.private / 'result.txt'
        request_file.write_text('Installed communication verification: TEST REQUEST ONLY.\n')
        result_file.write_text('Installed communication verification: TEST RESULT ONLY.\n')
        first = self.external('send', 'send', a, '--to', b, '--key', 'installed-request', '--content-file', request_file)
        message_id = first['message_id']
        require(first['sender_agent_id'] == a and first['target_agent_id'] == b, 'message attribution')
        require(self.external('same-key-retry', 'send', a, '--to', b, '--key', 'installed-request',
                              '--content-file', request_file) == first, 'same key must not duplicate')
        inbox = self.external('inbox', 'inbox', b)
        require([m['message_id'] for m in inbox['messages']] == [message_id], 'recipient must see the same request')
        ack = self.external('ack', 'ack', b, '--message', message_id)
        require(ack['delivery_state'] == 'acknowledged', 'recipient acknowledgement')
        result = self.external('reply', 'reply', b, '--message', message_id, '--content-file', result_file)
        require(result['reply_to_message_id'] == message_id and result['target_agent_id'] == a, 'reply correlation')
        observed = self.external('result-status', 'status', a, '--message', result['message_id'])
        require(observed == result and result['content'] == result_file.read_text(), 'sender must observe exact result')
        duplicate = self.external('duplicate-result', 'reply', b, '--message', message_id,
                                  '--key', 'different-result-key', '--content-file', result_file, expected=1)
        require('HTTP 409' in duplicate, 'second distinct result must conflict')
        self.checks.extend(['installed-send-inbox-ack-reply-status', 'same-key-idempotency', 'duplicate-result-rejected'])
        self.managed_task_rejected(a)
        self.checks.append('managed-task-rejected-with-specific-external-guard')
        independent = self.observe('after')
        require(all(count == 0 for count in independent['counts'].values()), 'test identities must have no managed work')
        require([m['message_id'] for m in independent['external_messages']] == [message_id, result['message_id']],
                'independent DB must contain exactly the request and result')
        require(all(m['sender_agent_id'] in self.agents and m['target_agent_id'] in self.agents
                    for m in independent['external_messages']), 'no test message may cross into a business identity')
        events = Counter(e['event_type'] for e in independent['journal'] if e['event_type'].startswith('external_'))
        require(events == {'external_session.bound': 2, 'external_message.sent': 2, 'external_message.acknowledged': 1},
                'journal must show two binds, two sends, one ack without duplicate writes')
        require(not any(e['aggregate_type'] in ('task', 'message', 'mailbox_item', 'run_attempt')
                        for e in independent['journal']), 'rejected Task must not write managed-work journal')
        after_services = self.services('services-after')
        require(after_services == before_services, 'service process identities must remain unchanged')
        self.checks.extend(['independent-test-only-db-and-journal', 'installed-processes-unchanged'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for flag in ('binary', 'root', 'db', 'socket', 'credentials', 'password-file'):
        parser.add_argument('--' + flag, type=Path, required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--organization', default='default')
    parser.add_argument('--unit', action='append', default=[], help='existing user service to observe without changing it')
    args = parser.parse_args()
    if not args.unit:
        args.unit = ['openagentx.service']
    os.umask(0o077)
    verifier = Installed(args)
    error = None
    try:
        verifier.run()
    except Exception as failure:
        error = failure
    finally:
        verifier.finish(error)
    return 1 if error else 0


if __name__ == '__main__':
    sys.exit(main())
