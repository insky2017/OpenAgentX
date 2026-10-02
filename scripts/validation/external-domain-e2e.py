#!/usr/bin/env python3
"""Real domain-directory/receipt/recovery CLI acceptance.

Default: isolated v3 -> v4 database and daemon. --installed: already-running
service, random test identities and temporary test scopes via official commands,
with CAS cleanup preserving business roles. Never operates Desktop threads,
Workers, models, payments or deployments. Runtime credentials stay private;
evidence is redacted. Each attempt needs a NEW --output and preserves failures.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess
import sys

sys.dont_write_bytecode = True
HELPER = Path(__file__).with_name('external-session-smoke.py')
spec = importlib.util.spec_from_file_location('external_session_smoke', HELPER)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)
require, now, sha = smoke.require, smoke.now, smoke.sha


class DomainAcceptance(smoke.Smoke):
    def __init__(self, args):
        args.root, args.evidence, args.upgrade_source_db = args.output, None, None
        super().__init__(args)
        self.candidate = self.binary
        self.legacy_binary = args.legacy_binary.resolve()
        suffix = secrets.token_hex(4)
        self.agents = tuple('domain-' + role + '-' + suffix for role in ('a', 'b', 'c', 'd'))
        self.bindings = {}
        self.tokens = {}
        self.inputs = self.raw / 'inputs'
        self.inputs.mkdir()

    def file(self, name, body):
        path = self.inputs / name
        path.write_text(body if isinstance(body, str) else json.dumps(body, indent=2) + '\n')
        return path

    def roles_apply(self, label, rules, version, org='default', expected=0):
        path = self.file(label + '.json', {'organization_id': org, 'rules': rules})
        return self.cli(label, ['external', 'roles', 'apply', '--file', path,
                              '--expected-version', version, '--socket', self.socket,
                              '--credentials', self.owner_file], expected=expected)

    def status(self, agent, message, label):
        return self.external(label, 'status', agent, '--message', message['message_id'])

    def receipt(self, agent, message, state, label, expected=0):
        note = self.file(label + '.txt', 'Isolated message-state fixture: ' + state +
                         '. No real action was executed. Unknown prior effects require clarification.\n')
        return self.external(label, 'receipt', agent, '--message', message['message_id'],
                             '--state', state, '--content-file', note, expected=expected)

    def recover(self, agent, label):
        cursor, collected = 0, []
        while True:
            page = self.external(label + '-' + str(cursor), 'inbox', agent, '--recover', '--after', cursor)
            collected.extend(page['messages'])
            if not page['has_more']:
                break
            require(page['next_after'] > cursor, 'recovery cursor must advance')
            cursor = page['next_after']
        require(len({m['message_id'] for m in collected}) == len(collected), 'recover must not duplicate IDs')
        return {m['message_id']: m for m in collected}

    def rejected(self, output, status, code=None):
        require('HTTP ' + str(status) in output, 'expected HTTP ' + str(status) + '; see CLI evidence')
        if code:
            require(code in output, 'expected error code ' + code + '; see CLI evidence')

    def rowsets(self, label, columns=None):
        connection = sqlite3.connect(self.db.as_uri() + '?mode=ro', uri=True)
        try:
            schema = connection.execute('SELECT version FROM schema_meta WHERE singleton=1').fetchone()[0]
            if columns is None:
                names = [x[0] for x in connection.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
                         if x[0] != 'schema_meta' and not x[0].startswith('sqlite_')]
                columns = {name: [x[1] for x in connection.execute('PRAGMA table_info("' + name + '")')]
                           for name in names}
            tables = {}
            for name, cols in columns.items():
                quote = lambda s: '"' + s.replace('"', '""') + '"'
                sql = 'SELECT ' + ','.join(map(quote, cols)) + ' FROM ' + quote(name)
                hashes = sorted(hashlib.sha256(repr(row).encode()).digest() for row in connection.execute(sql))
                tables[name] = {'rows': len(hashes), 'rowset_sha256': hashlib.sha256(b''.join(hashes)).hexdigest()}
            result = {'observed_at': now(), 'database': str(self.db), 'mode': 'read-only',
                      'schema_version': schema, 'columns': columns, 'tables': tables}
            self.save(label + '.json', result)
            return result
        finally:
            connection.close()

    def journal_count(self):
        connection = sqlite3.connect(self.db.as_uri() + '?mode=ro', uri=True)
        try:
            return connection.execute('SELECT count(*) FROM event_journal').fetchone()[0]
        finally:
            connection.close()

    def run(self):
        root = Path(__file__).resolve().parents[2]
        source_paths = [Path(__file__), HELPER, root / 'skills/oax-collaborate/SKILL.md',
                        root / 'internal/cli/external/command.go', root / 'internal/api/external/handler.go',
                        root / 'internal/controlplane/external_session_service.go',
                        root / 'internal/domain/external_session.go',
                        root / 'internal/persistence/sqlite/external_roles_repository.go',
                        root / 'internal/persistence/sqlite/external_session_repository.go',
                        root / 'internal/persistence/sqlite/migrations/004_external_roles.sql']
        self.save('provenance.json', {'at': now(), 'candidate_binary': str(self.candidate),
                  'candidate_sha256': sha(self.candidate), 'legacy_binary': str(self.legacy_binary),
                  'legacy_sha256': sha(self.legacy_binary), 'source_commit': self.args.commit,
                  'source_files': {str(p.relative_to(root)): sha(p) for p in source_paths},
                  'scope': 'real isolated daemon CLI/API only; no semantic policy or original Desktop isolation proof',
                  'socket': str(self.socket), 'database': str(self.db), 'agents': self.agents})
        self.binary = self.legacy_binary
        self.cli('legacy-init', ['init', '--db', self.db], interactive=True)
        for agent in self.agents:
            workspace = self.private / agent
            workspace.mkdir()
            (workspace / 'ROLE.md').write_text('Communication fixture only; no model or business execution.\n')
            identity = workspace / 'identity.yaml'
            identity.write_text(f'version: 1\nagent_id: {agent}\nprincipal_id: agent-{agent}\n'
                                f'organization_id: default\ndisplay_name: Domain acceptance fixture\n'
                                f'profile:\n  instructions_path: ROLE.md\n  workspace_root: {workspace}\n'
                                f'  capabilities: [communication-api-verification]\n')
            self.cli('legacy-apply-' + agent, ['agent', 'apply', '--db', self.db, '--file', identity], interactive=True)
        self.start()
        self.cli('legacy-owner-login', ['console', 'login', '--socket', self.socket,
                                      '--credentials', self.owner_file], interactive=True)
        owner_document = json.loads(self.owner_file.read_text())
        self.secret_values.extend(c['token'] for c in owner_document['credentials'])
        a, b, c, d = self.agents
        peers = {a: [b, c, d], b: [a, c], c: [a, b, d], d: [a, c]}
        for agent in self.agents:
            result = self.external('legacy-bind-' + agent, 'bind', agent, '--host', 'isolated-domain-fixture',
                                   '--thread', 'isolated-' + agent, '--peers', ','.join(peers[agent]))
            require(result['automatic_delivery'] is False, 'bind must not claim model delivery')
            self.bindings[agent] = result['binding']
            self.tokens[agent] = self.token(agent)
        request = self.file('request.txt', 'Inspect fixture metadata only; no payment, deployment or credential change.\n')
        answer = self.file('answer.txt', 'Fixture inspection reply. Communication completed, no business success claimed.\n')
        changed = self.file('changed.txt', 'Changed fixture question.\n')
        old = self.external('legacy-pending', 'send', a, '--to', b, '--key', 'legacy-pending', '--content-file', request)
        self.external('legacy-pending-ack', 'ack', b, '--message', old['message_id'])
        done = self.external('legacy-done-request', 'send', a, '--to', b, '--key', 'legacy-done', '--content-file', request)
        done_reply = self.external('legacy-done-reply', 'reply', b, '--message', done['message_id'], '--content-file', answer)
        self.stop()
        before = self.rowsets('migration-before')
        require(before['schema_version'] == 3, 'legacy binary must produce actual schema v3')
        self.binary = self.candidate
        self.cli('candidate-schema-migrate', ['schema', 'verify', '--db', self.db])
        after = self.rowsets('migration-after-old-columns', before['columns'])
        require(after['schema_version'] == 4, 'candidate must migrate isolated v3 to v4')
        require(before['tables'] == after['tables'], 'migration changed a pre-existing column rowset')
        self.checks.append('real-v3-to-v4-migration-preserves-every-old-table-column-rowset')
        self.start()
        for agent in self.agents:
            require(self.external('migrated-binding-' + agent, 'status', agent) == self.bindings[agent], 'binding changed during migration')
        require(self.status(b, done, 'migrated-answered-request')['processing_state'] == 'completed', 'legacy answered request must migrate completed')
        require(self.status(a, done_reply, 'migrated-result')['processing_state'] == 'completed', 'legacy result must migrate completed')
        require(old['message_id'] in self.recover(b, 'migrated-recover'), 'legacy acknowledged pending must remain recoverable')
        require(self.external('legacy-key-retry', 'send', a, '--to', b, '--key', 'legacy-pending', '--content-file', request)['message_id'] == old['message_id'], 'legacy payload idempotency changed')
        rules = [{'scope': 'fixture.' + role, 'owner_agent_id': agent, 'description': 'Owns isolated ' + role + ' fixture questions.'}
                 for role, agent in zip(('a', 'b', 'c', 'd'), self.agents)]
        denied = self.api('agent-cannot-apply-roles', 'PUT', 'roles', self.tokens[b],
                          {'organization_id': 'default', 'expected_version': 0, 'rules': rules}, expected=401)
        require(denied.get('code') in ('UNAUTHORIZED', 'FORBIDDEN', 'CLI_UNAUTHENTICATED', 'CLI_FORBIDDEN'), 'agent role write denial needs auth error')
        catalog = self.roles_apply('owner-apply-roles', rules, 0)
        require(catalog['revision'] == 1, 'initial catalog revision')
        count = self.journal_count()
        self.rejected(self.roles_apply('stale-catalog-cas', rules, 0, expected=1), 409)
        require(self.journal_count() == count, 'stale CAS must not append a journal event')
        self.rejected(self.roles_apply('foreign-catalog-owner', rules, 0, org='different-fixture-organization', expected=1), 403)
        require(self.journal_count() == count, 'cross-organization directory rejection must not mutate')
        visible = self.external('peer-filtered-roles', 'roles', b)
        require({r['owner_agent_id'] for r in visible['rules']} == {a, b, c}, 'roles must show only self and allowed peers')
        owner_view = self.external('owner-roles-via-binding', 'roles', a, '--owner')
        require(owner_view == catalog, 'owner binding resolves whole organization catalog')
        self.checks.extend(['roles-owner-only', 'catalog-CAS-and-same-organization-owner', 'self-peer-role-visibility'])
        for key, scope, expected_code in [('missing-scope', '', 'SCOPE_REQUIRED'), ('unknown-scope', 'fixture.unknown', 'UNKNOWN_SCOPE'), ('wrong-owner', 'fixture.a', 'OUT_OF_SCOPE')]:
            args = ['--to', b, '--key', key, '--content-file', request]
            if scope:
                args += ['--scope', scope]
            self.rejected(self.external(key, 'send', a, *args, expected=1), 422, expected_code)
        wrong = self.api('api-owner-guidance', 'POST', 'messages', self.tokens[a],
                         {'target_agent_id': b, 'kind': 'consultation', 'scope': 'fixture.a', 'content': 'fixture', 'idempotency_key': 'api-wrong-owner'}, expected=422)
        require(wrong['owner_agent_id'] == a, 'wrong scope must identify authoritative owner')
        self.rejected(self.receipt(b, old, 'accepted', 'legacy-unscoped-accept-denied', expected=1), 422, 'SCOPE_REQUIRED')
        self.rejected(self.external('legacy-unscoped-reply-denied', 'reply', b, '--message', old['message_id'], '--content-file', answer, expected=1), 422, 'SCOPE_REQUIRED')
        clarified = self.receipt(b, old, 'needs_clarification', 'legacy-needs-clarification')
        require(clarified['processing_state'] == 'needs_clarification', 'clarification is nonterminal')
        require(old['message_id'] in self.recover(b, 'legacy-clarification-recover'), 'clarification must remain recoverable')
        self.checks.extend(['scope-required-unknown-wrong-owner-denied', 'legacy-unscoped-held-for-clarification-not-executed'])
        current = self.external('scoped-send', 'send', a, '--to', b, '--scope', 'fixture.b', '--key', 'scoped-current', '--content-file', request)
        repeated = self.external('scoped-send-retry', 'send', a, '--to', b, '--scope', 'fixture.b', '--key', 'scoped-current', '--content-file', request)
        require(repeated == current, 'same key same payload must return same record')
        self.rejected(self.external('changed-payload-conflict', 'send', a, '--to', b, '--scope', 'fixture.b', '--key', 'scoped-current', '--content-file', changed, expected=1), 409)
        self.rejected(self.external('scoped-reply-without-accept', 'reply', b, '--message', current['message_id'], '--content-file', answer, expected=1), 409)
        self.external('scoped-ack', 'ack', b, '--message', current['message_id'])
        accepted = self.receipt(b, current, 'accepted', 'scoped-accept')
        require(accepted['processing_state'] == 'accepted' and accepted['delivery_state'] == 'acknowledged', 'accepted and ACK are independent fields')
        count = self.journal_count()
        require(self.receipt(b, current, 'accepted', 'scoped-accept-retry') == accepted, 'receipt replay changed record')
        require(self.journal_count() == count, 'same receipt must not duplicate journal')
        require(current['message_id'] in self.recover(b, 'accepted-recover'), 'ACK must not hide unfinished accepted work')
        unread = self.external('default-inbox-after-ack', 'inbox', b)['messages']
        require(not {old['message_id'], current['message_id']} & {m['message_id'] for m in unread},
                'default inbox should hide the two acknowledged requests; independently unacknowledged legacy items may remain')
        clarification = self.receipt(b, current, 'needs_clarification', 'accepted-work-unknown-effect')
        delayed = self.receipt(b, current, 'accepted', 'delayed-old-accepted-retry')
        require(delayed == clarification, 'delayed accepted retry must not overwrite newer clarification')
        self.rejected(self.external('clarification-cannot-final-reply', 'reply', b, '--message', current['message_id'], '--content-file', answer, expected=1), 409)
        fresh_note = self.file('fresh-accept-after-inspection.txt', 'New checkpoint: independently checked fixture metadata; no business action occurred; safe to continue the fixture reply.\n')
        accepted = self.external('fresh-accept-after-clarification', 'receipt', b, '--message', current['message_id'], '--state', 'accepted', '--content-file', fresh_note)
        require(accepted['processing_state'] == 'accepted', 'new clarification evidence should allow fresh acceptance')
        self.checks.append('accepted-ACK-recover-and-idempotency')
        self.checks.append('needs-clarification-not-completed-or-overwritten-by-delayed-retry')
        # Reassignment must be rechecked at final reply; no business action is run.
        reassigned = [dict(r, owner_agent_id=c) if r['scope'] == 'fixture.b' else r for r in rules]
        require(self.roles_apply('owner-reassign-scope', reassigned, 1)['revision'] == 2, 'CAS valid update')
        self.rejected(self.external('stale-owner-accepted-replay-denied', 'receipt', b, '--message', current['message_id'], '--state', 'accepted', '--content-file', fresh_note, expected=1), 422, 'OUT_OF_SCOPE')
        self.rejected(self.external('stale-owner-cannot-reply', 'reply', b, '--message', current['message_id'], '--content-file', answer, expected=1), 422, 'OUT_OF_SCOPE')
        require(self.status(b, current, 'stale-owner-still-accepted')['processing_state'] == 'accepted', 'rejected reply must not complete')
        require(self.roles_apply('owner-restore-scope', rules, 2)['revision'] == 3, 'restore owner through valid CAS')
        # Restart while accepted and needs_clarification; saved notes must survive.
        counts_before = self.counts('counts-before-restart')
        self.stop()
        self.start()
        recovered = self.recover(b, 'after-restart-recover')
        require(recovered[current['message_id']] == accepted, 'accepted state/ACK/note changed across restart')
        require(recovered[old['message_id']] == clarified, 'clarification state/note changed across restart')
        require(self.external('catalog-after-restart', 'roles', a)['revision'] == 3, 'catalog revision persistence')
        require(self.counts('counts-after-restart') == counts_before, 'restart changed counts')
        result = self.external('final-scoped-reply', 'reply', b, '--message', current['message_id'], '--content-file', answer)
        require(result['scope'] == 'fixture.b' and result['reply_to_message_id'] == current['message_id'], 'result scope/correlation inheritance')
        require(self.status(a, current, 'request-completed')['processing_state'] == 'completed', 'final reply must atomically complete original request')
        require(current['message_id'] not in self.recover(b, 'completed-not-recovered'), 'completed request must leave recover')
        require(self.external('final-reply-retry', 'reply', b, '--message', current['message_id'], '--content-file', answer) == result, 'final reply idempotency')
        self.rejected(self.receipt(b, current, 'accepted', 'completed-no-regression', expected=1), 409)
        self.rejected(self.external('result-no-reply-loop', 'reply', a, '--message', result['message_id'], '--content-file', answer, expected=1), 400)
        self.checks.extend(['current-owner-rechecked-before-final-reply', 'restart-catalog-ACK-accepted-clarification-persistence', 'final-reply-completes-request-and-removes-recovery'])
        # d owns fixture.d but b has no peer permission for d.
        self.rejected(self.external('forward-no-peer-expansion', 'forward', b, '--message', old['message_id'], '--to', d, '--scope', 'fixture.d', '--key', 'denied-forward', '--content-file', request, expected=1), 403)
        require(self.status(b, old, 'failed-forward-keeps-source')['processing_state'] == 'needs_clarification', 'rejected forward changed source')
        forwarded = self.external('explicit-forward', 'forward', b, '--message', old['message_id'], '--to', c, '--scope', 'fixture.c', '--key', 'forward-once', '--content-file', request)
        require(forwarded['forwarded_from_message_id'] == old['message_id'] and forwarded['kind'] == 'request', 'forward needs original linkage')
        require(self.status(b, old, 'forwarded-source-state')['processing_state'] == 'out_of_scope', 'forward source must terminally leave recovery')
        require(old['message_id'] not in self.recover(b, 'forwarded-source-no-recover'), 'forward source must not be re-executed')
        require(self.external('forward-key-retry', 'forward', b, '--message', old['message_id'], '--to', c, '--scope', 'fixture.c', '--key', 'forward-once', '--content-file', request) == forwarded, 'forward idempotency')
        before_denied_forward = self.journal_count()
        self.rejected(self.external('forward-cannot-fan-out', 'forward', b, '--message', old['message_id'], '--to', a, '--scope', 'fixture.a', '--key', 'forward-twice', '--content-file', request, expected=1), 409)
        self.rejected(self.external('forward-cannot-loop', 'forward', c, '--message', forwarded['message_id'], '--to', b, '--scope', 'fixture.b', '--key', 'forward-loop', '--content-file', request, expected=1), 409)
        require(self.journal_count() == before_denied_forward, 'rejected duplicate/loop forward must not append journal')
        self.receipt(c, forwarded, 'accepted', 'forward-recipient-accept')
        self.external('forward-recipient-result', 'reply', c, '--message', forwarded['message_id'], '--content-file', answer)
        self.checks.append('explicit-one-hop-forward-no-fanout-loop-or-peer-expansion')
        final_counts = self.counts('final-counts')
        require(all(final_counts[t] == 0 for t in ('tasks', 'run_attempts', 'worker_instances')), 'message fixture must not spawn managed work')
        require(final_counts['external_messages'] == 7, 'rejections/retries must not duplicate message ledger')
        self.cli('final-schema-verify', ['schema', 'verify', '--db', self.db])
        self.save('skill-compatibility.json', {'at': now(), 'skill': str(root / 'skills/oax-collaborate/SKILL.md'),
                  'sha256': sha(root / 'skills/oax-collaborate/SKILL.md'), 'result': 'CLI names/flags exercised in this run',
                  'limits': 'Declared scope and message states only. No semantic body enforcement, business effect, Desktop cross-repository isolation or wakeup proof.'})
        self.checks.append('no-managed-tasks-runs-workers-and-final-schema4')


class InstalledDomain(DomainAcceptance):
    """Small installed check: append only unique test scopes, then CAS-remove them."""
    def __init__(self, args):
        self.args = args
        args.evidence = None
        self.root = args.output.resolve()
        self.root.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.raw, self.private = self.root / 'raw', self.root / 'private'
        self.raw.mkdir(mode=0o700)
        self.private.mkdir(mode=0o700)
        self.inputs = self.raw / 'inputs'
        self.inputs.mkdir()
        self.binary, self.db, self.socket = args.binary.resolve(), args.db.resolve(), args.socket.resolve()
        self.owner_file = args.credentials.resolve()
        password_source = args.password_file.resolve()
        protected = password_source.stat().st_mode & 0o077 == 0 or any(
            p.stat().st_uid == os.getuid() and p.stat().st_mode & 0o077 == 0
            for p in password_source.parents)
        require(password_source.stat().st_uid == os.getuid() and protected,
                'password source must be user-owned and protected by file or ancestor-directory permissions')
        self.password = args.password_file.read_text().strip()
        require(bool(self.password), 'owner password source is empty')
        document = json.loads(self.owner_file.read_text())
        selection = document.get('current', {}).get(str(self.socket), {})
        matches = [c for c in document['credentials'] if c['socket_path'] == str(self.socket)
                   and c['username'] == selection.get('username', 'owner')
                   and c['installation_id'] == selection.get('installation_id', c['installation_id'])]
        require(len(matches) == 1, 'expected selected owner credential')
        self.owner = matches[0]
        self.secret_values = [self.password, *(c['token'] for c in document['credentials'])]
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith(('OPENAGENTX_', 'AGY_')) and 'proxy' not in k.lower()}
        self.env.update(OPENAGENTX_HOME=str(self.private / 'profile'), TERM='xterm-256color')
        self.sequence, self.checks = 0, []
        suffix = secrets.token_hex(6)
        self.agents = ('domain-installed-a-' + suffix, 'domain-installed-b-' + suffix)
        self.scopes = ('installed.' + suffix + '.a', 'installed.' + suffix + '.b')
        self.bindings, self.tokens = {}, {}
        self.catalog_appended = False
        self.before_services = None

    def start(self):
        raise RuntimeError('installed mode must not start a service')

    def stop(self):
        pass

    def services(self, label):
        records = []
        for unit in self.args.unit or ['openagentx.service']:
            argv = ['systemctl', '--user', 'show', unit, '-p', 'MainPID', '-p', 'ActiveState',
                    '-p', 'SubState', '-p', 'ExecMainStartTimestampMonotonic']
            result = subprocess.run(argv, capture_output=True, text=True, timeout=10)
            require(result.returncode == 0, 'cannot observe service: ' + unit)
            properties = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
            pid = int(properties.get('MainPID', '0'))
            require(pid > 0 and properties.get('ActiveState') == 'active', 'service must be active: ' + unit)
            fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
            records.append({'unit': unit, 'argv': argv, 'exit_code': result.returncode,
                            'stdout': result.stdout, 'stderr': result.stderr, 'pid': pid,
                            'starttime': fields[19], 'exe_sha256': sha(f'/proc/{pid}/exe')})
        self.save(label + '.json', records)
        return records

    def catalog(self, label):
        return self.cli(label, ['external', 'roles', '--owner', '--organization', self.args.organization,
                               '--socket', self.socket, '--credentials', self.owner_file])

    def observe_installed(self, label):
        db = sqlite3.connect(self.db.as_uri() + '?mode=ro', uri=True)
        try:
            version = db.execute('SELECT version FROM schema_meta WHERE singleton=1').fetchone()[0]
            require(version == 4, 'installed mode requires already-migrated schema4; will not migrate')
            counts = {}
            for table, column in [('tasks', 'target_agent_id'), ('messages', 'target_agent_id'),
                                  ('mailbox_items', 'target_agent_id'), ('run_attempts', 'agent_id'),
                                  ('worker_instances', 'agent_id')]:
                counts[table] = db.execute(f'SELECT count(*) FROM {table} WHERE {column} IN (?,?)', self.agents).fetchone()[0]
            rows = db.execute('SELECT message_id,sender_agent_id,target_agent_id,kind,reply_to_message_id,scope,processing_state,delivery_state FROM external_messages WHERE sender_agent_id IN (?,?) OR target_agent_id IN (?,?) ORDER BY sequence', self.agents + self.agents).fetchall()
            result = {'at': now(), 'mode': 'read-only; generated test identities only', 'schema_version': version,
                      'agents': self.agents, 'counts': counts, 'external_message_rows': rows}
            self.save(label + '.json', result)
            return result
        finally:
            db.close()

    def run(self):
        self.save('provenance.json', {'at': now(), 'binary': str(self.binary), 'binary_sha256': sha(self.binary),
                  'source_commit': self.args.commit, 'harness_sha256': sha(__file__), 'helper_sha256': sha(HELPER),
                  'scope': 'installed API only; random test identities, CAS append/remove test scopes; no service lifecycle',
                  'agents': self.agents, 'scopes': self.scopes, 'organization': self.args.organization,
                  'socket': str(self.socket), 'database': str(self.db)})
        self.before_services = self.services('services-before')
        require(all(r['exe_sha256'] == sha(self.binary) for r in self.before_services), 'live service binary must equal installed candidate')
        self.observe_installed('before')
        original = self.catalog('business-catalog-before')
        require(original['revision'] >= 1 and original['rules'], 'actual business roles baseline must already exist')
        require(not set(self.scopes) & {r['scope'] for r in original['rules']}, 'random test scopes must be absent')
        for agent in self.agents:
            workspace = self.private / agent
            workspace.mkdir()
            (workspace / 'ROLE.md').write_text('Installed API fixture. No Worker/model or business side effect.\n')
            identity = workspace / 'identity.yaml'
            identity.write_text(f'version: 1\nagent_id: {agent}\nprincipal_id: agent-{agent}\n'
                                f'organization_id: {self.args.organization}\ndisplay_name: Installed domain verification\n'
                                f'profile:\n  instructions_path: ROLE.md\n  workspace_root: {workspace}\n'
                                '  capabilities: [communication-api-verification]\n')
            self.cli('apply-' + agent, ['agent', 'apply', '--db', self.db, '--file', identity,
                                       '--owner-username', self.owner['username']], interactive=True)
        for agent, peer in zip(self.agents, reversed(self.agents)):
            result = self.external('bind-' + agent, 'bind', agent, '--host', 'installed-test-only',
                                   '--thread', 'isolated-' + agent, '--peers', peer)
            self.bindings[agent] = result['binding']
            self.tokens[agent] = self.token(agent)
        test_rules = [{'scope': scope, 'owner_agent_id': agent, 'description': 'Temporary installed communication fixture only.'}
                      for scope, agent in zip(self.scopes, self.agents)]
        # Arm bounded cleanup before the write: a timeout can hide a committed CAS.
        self.catalog_appended = True
        appended = self.roles_apply('append-only-test-scopes', original['rules'] + test_rules,
                                    original['revision'], org=self.args.organization)
        require([r for r in appended['rules'] if r['scope'] not in self.scopes] == original['rules'], 'append must preserve all business rules verbatim')
        self.checks.append('installed-random-identities-and-CAS-append-with-business-rules-preserved')
        a, b = self.agents
        body = self.file('request.txt', 'Installed test message only. No business action requested.\n')
        answer = self.file('answer.txt', 'Installed test reply only; no business outcome claimed.\n')
        self.rejected(self.external('missing-scope-denied', 'send', a, '--to', b, '--key', 'missing', '--content-file', body, expected=1), 422, 'SCOPE_REQUIRED')
        self.rejected(self.external('wrong-owner-denied', 'send', a, '--to', b, '--scope', self.scopes[0], '--key', 'wrong', '--content-file', body, expected=1), 422, 'OUT_OF_SCOPE')
        request = self.external('scoped-send', 'send', a, '--to', b, '--scope', self.scopes[1], '--key', 'installed-scoped', '--content-file', body)
        require(self.external('scoped-idempotent', 'send', a, '--to', b, '--scope', self.scopes[1], '--key', 'installed-scoped', '--content-file', body) == request, 'installed same-key retry must not duplicate')
        self.external('recipient-ack', 'ack', b, '--message', request['message_id'])
        self.receipt(b, request, 'accepted', 'accepted-receipt')
        require(request['message_id'] in self.recover(b, 'installed-accepted-recover'), 'installed accepted ACK must remain recoverable')
        result = self.external('final-reply', 'reply', b, '--message', request['message_id'], '--content-file', answer)
        require(result['scope'] == self.scopes[1] and result['reply_to_message_id'] == request['message_id'], 'installed reply must inherit scope/correlation')
        require(self.status(a, request, 'completed-request')['processing_state'] == 'completed', 'installed reply must complete original')
        require(request['message_id'] not in self.recover(b, 'installed-completed-recover'), 'installed completed must leave recover')
        self.external('result-recipient-ack', 'ack', a, '--message', result['message_id'])
        observation = self.observe_installed('after-roundtrip')
        require(all(v == 0 for v in observation['counts'].values()), 'installed test must not create managed work')
        require(len(observation['external_message_rows']) == 2, 'installed test must have exactly request and reply')
        require(all(r[1] in self.agents and r[2] in self.agents for r in observation['external_message_rows']), 'no business agent messages')
        self.checks.extend(['installed-scope-rejections-and-idempotency', 'installed-ACK-accepted-recover-final-completed', 'installed-test-only-ledger-no-managed-work'])

    def finish(self, error):
        failures = []
        if self.catalog_appended:
            try:
                # Re-read on every attempt; only remove owned test entries. Never
                # restore a stale whole catalog over somebody else's changes.
                for attempt in range(3):
                    current = self.catalog('cleanup-current-catalog-' + str(attempt))
                    found = [r for r in current['rules'] if r['scope'] in self.scopes]
                    for rule in found:
                        require(rule['owner_agent_id'] == self.agents[self.scopes.index(rule['scope'])], 'test scope ownership changed; refuse cleanup overwrite')
                    remaining = [r for r in current['rules'] if r['scope'] not in self.scopes]
                    if not found:
                        break
                    path = self.file('cleanup-roles-' + str(attempt) + '.json', {'organization_id': self.args.organization, 'rules': remaining})
                    # Record an expected-success first; only CAS conflict retries.
                    try:
                        cleaned = self.cli('cleanup-CAS-' + str(attempt), ['external', 'roles', 'apply', '--file', path,
                                           '--expected-version', current['revision'], '--socket', self.socket, '--credentials', self.owner_file])
                    except RuntimeError:
                        evidence = json.loads(sorted((self.raw / 'cli').glob('*.json'))[-1].read_text())
                        if 'HTTP 409' in evidence['stderr'] and attempt < 2:
                            continue
                        raise
                    require(cleaned['rules'] == remaining, 'cleanup must preserve non-test rules from latest snapshot')
                    break
                else:
                    raise RuntimeError('test catalog cleanup exhausted CAS attempts')
                self.checks.append('cleanup-CAS-removes-only-test-scopes-preserves-latest-business-rules')
            except Exception as failure:
                failures.append(str(failure))
        for agent, binding in self.bindings.items():
            try:
                self.external('cleanup-revoke-' + agent, 'revoke', agent, '--expected-generation', binding['generation'])
                self.api('cleanup-old-token-denied-' + agent, 'GET', 'status', self.tokens[agent], expected=401)
            except Exception as failure:
                failures.append(str(failure))
        if self.before_services is not None:
            try:
                require(self.services('services-after') == self.before_services, 'service process changed during installed verification')
                self.checks.append('installed-service-PID-starttime-and-binary-unchanged')
            except Exception as failure:
                failures.append(str(failure))
        self.save('cleanup.json', {'at': now(), 'revoked_test_agents': list(self.bindings),
                  'test_scopes': self.scopes, 'failures': failures,
                  'identity_and_message_records': 'retained for audit; credentials revoked'})
        if failures:
            error = RuntimeError((str(error) + '; ' if error else '') + '; '.join(failures))
        smoke.Smoke.finish(self, error)
        self.final_error = error


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path, help='candidate v4 binary; never installed by this harness')
    parser.add_argument('--legacy-binary', type=Path, help='actual v3 binary used ONLY with new isolated database')
    parser.add_argument('--output', required=True, type=Path, help='new private persistent attempt directory')
    parser.add_argument('--web', type=Path, default=Path('web/dist'))
    parser.add_argument('--commit', default='uncommitted candidate; see recorded source hashes')
    parser.add_argument('--installed', action='store_true', help='minimal already-installed API verification; never restarts services')
    parser.add_argument('--db', type=Path)
    parser.add_argument('--socket', type=Path)
    parser.add_argument('--credentials', type=Path)
    parser.add_argument('--password-file', type=Path)
    parser.add_argument('--organization', default='default')
    parser.add_argument('--unit', action='append', default=[])
    args = parser.parse_args()
    if args.installed:
        for name in ('db', 'socket', 'credentials', 'password_file'):
            if getattr(args, name) is None:
                parser.error('--installed requires --' + name.replace('_', '-'))
    elif args.legacy_binary is None:
        parser.error('isolated mode requires --legacy-binary')
    os.umask(0o077)
    test = InstalledDomain(args) if args.installed else DomainAcceptance(args)
    error = None
    try:
        test.run()
    except Exception as failure:
        error = failure
    finally:
        test.finish(error)
    return int(getattr(test, 'final_error', error) is not None)


if __name__ == '__main__':
    sys.exit(main())
