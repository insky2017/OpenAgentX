#!/usr/bin/env python3
"""E20 deterministic full chain: real daemon/Worker, executable AGY protocol fixture.

No real model or installed service is used. Reuses the live harness only for
isolated profiles, official CLI/API transport, evidence capture and owned cleanup.
"""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import sys

from agy_live import Live, owned_stop, pty_command, write

P = pathlib.Path
CASES = ('empty', 'bad-json', 'missing-terminal', 'truncated-json',
         'oversized-result', 'conflicting-terminal', 'exit-zero-stderr')
TERMINAL = {'succeeded', 'failed', 'uncertain', 'canceled', 'waiting_input'}

# This process knows only its private fixture directory and the supplied stdin.
# Save byte-exact stdout/stderr before sending them; no model is called.
FIXTURE = r'''#!/usr/bin/env python3
import hashlib,json,os,pathlib,re,sys,time
root=pathlib.Path(__file__).parent/'runtime-raw'
if '--version' in sys.argv:
    print('oax-e20-agy-protocol-fixture 1');sys.exit(0)
data=sys.stdin.buffer.read()
prompt=json.loads(data)['message']['content']
match=re.search(r'OAX-E20-CASE:([a-z-]+)',prompt)
case=match.group(1) if match else 'network-probe'
def result(status,body):
    return (json.dumps({'event':'result','result':{'status':status,'response':body}})+'\n').encode()
stdout=result('SUCCESS','ok');stderr=b''
if case=='empty':stdout=b''
elif case=='bad-json':stdout=b'not-json\n'
elif case=='missing-terminal':stdout=b'{"event":"step_update","step_update":{"text":"draft"}}\n'
elif case=='truncated-json':stdout=b'{"event":"result","result":{"status":"SUCCESS","response":"partial'
elif case=='oversized-result':stdout=result('SUCCESS','x'*32769)
elif case=='conflicting-terminal':stdout=result('SUCCESS','answer')+result('FAILED','contradiction')
elif case=='exit-zero-stderr':stdout=result('SUCCESS','answer');stderr=b'provider execution error\n'
elif case=='normal':stdout=result('SUCCESS','OAX-E20-NORMAL')
elif case!='network-probe':raise RuntimeError('unknown fixture case')
dest=root/('run-'+str(os.getpid()));dest.mkdir(parents=True)
for name,value in [('stdin',data),('stdout',stdout),('stderr',stderr)]:
    (dest/name).write_bytes(value)
metadata={'case':case,'pid':os.getpid(),'argv':sys.argv,'cwd':os.getcwd(),'at':time.time(),
          'exit_code':0,'sha256':{name:hashlib.sha256(value).hexdigest() for name,value in [('stdin',data),('stdout',stdout),('stderr',stderr)]}}
(dest/'metadata.json').write_text(json.dumps(metadata)+'\n')
sys.stdout.buffer.write(stdout);sys.stdout.buffer.flush()
sys.stderr.buffer.write(stderr);sys.stderr.buffer.flush()
'''


class Malformed(Live):
    def setup(self):
        self.workspace = self.root / 'workspace'
        self.workspace.mkdir()
        (self.workspace / 'ROLE.md').write_text('Isolated deterministic AGY protocol validation role.\n')
        fixture = self.root / 'agy-protocol-fixture'
        fixture.write_text(FIXTURE)
        fixture.chmod(0o700)
        (self.root / 'runtime-raw').mkdir(mode=0o700)
        manifest = {
            'coverage': 'D: official daemon/API + real Worker + executable AGY fixture; no real model or installed service',
            'source_commit': self.a.commit, 'binary': self.a.binary,
            'binary_sha256': hashlib.sha256(P(self.a.binary).read_bytes()).hexdigest(),
            'fixture_sha256': hashlib.sha256(fixture.read_bytes()).hexdigest(),
            'script_sha256': hashlib.sha256(P(__file__).read_bytes()).hexdigest(),
            'created_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'private_profile': str(self.root), 'url': self.url, 'argv': sys.argv,
        }
        write(self.out / 'manifest.json', manifest)
        for label, argv in [('init', [self.a.binary, 'init'])]:
            r = pty_command(argv, self.password, self.env)
            write(self.out / 'cli' / (label + '.json'), r)
            assert r['exit_code'] == 0, r
        identity = self.root / 'identity.yaml'
        identity.write_text(f'''version: 1
agent_id: {self.aid}
principal_id: agent-{self.aid}
organization_id: default
display_name: Deterministic AGY protocol verification
profile:
  instructions_path: {self.workspace}/ROLE.md
  workspace_root: {self.workspace}
  capabilities: [control-plane-testing]
''')
        r = pty_command([self.a.binary, 'agent', 'apply', '--file', str(identity)], self.password, self.env)
        write(self.out / 'cli/agent-apply.json', r)
        assert r['exit_code'] == 0, r
        self.start('daemon', [self.a.binary, 'serve', '--http-addr', '127.0.0.1:' + str(self.port), '--web-dir', self.a.web])
        self.wait(lambda: self.socket.exists())
        self.csrf = self.api('/api/auth/v1/login', {'username': 'owner', 'password': self.password}, auth=True)['csrf_token']
        config = self.root / 'worker.yaml'
        config.write_text(f'''version: 1
agent_id: {self.aid}
transport: unix
unix_socket: {self.socket}
heartbeat_interval: 1s
mailbox_wait: 5s
control_wait: 5s
shutdown_timeout: 10s
network_materialization_dir: {self.root}/network
runtime_backends:
  - backend_id: primary
    adapter_id: agy-batch
    options:
      binary: {fixture}
      models: [default]
      working_dir: {self.workspace}
      timeout: 10s
''')
        config.chmod(0o600)
        self.start('worker', [self.a.binary, 'worker', 'run', '--config', str(config)])
        self.worker = self.wait(lambda: next((x for x in self.api('/api/observe/v1/overview').get('workers', []) if x.get('agent_id') == self.aid), None))
        network = lambda: self.api('/api/observe/v1/network-profiles?agent_id=' + self.aid)
        ref = {k: self.worker[k] for k in ('worker_instance_id', 'generation')}
        receipt = self.api('/api/control/v1/network-bindings/mode/tests', dict(agent_id=self.aid, backend_id='primary', mode='inherit', **ref))['receipt']
        def tested():
            test = next((x for x in network().get('mode_tests', []) if x['test_id'] == receipt['test_id']), None)
            if test and test['state'] == 'failed':
                raise RuntimeError('network fixture probe failed: ' + test.get('diagnostic_code', 'unknown'))
            return test if test and test['state'] == 'succeeded' else None
        self.wait(tested)
        self.api('/api/control/v1/network-bindings/mode/publish', dict(test_id=receipt['test_id'], **ref))
        self.wait(lambda: next((x for x in network().get('bindings', []) if x.get('desired_status') == 'applied'), None))
        write(self.out / 'network-applied.json', network())

    def case(self, name, label):
        before = set((self.root / 'runtime-raw').glob('run-*'))
        tid = self.api('/api/control/v1/tasks', {
            'target_agent_id': self.aid, 'organization_id': 'default',
            'dispatch_mode': 'direct', 'intent': 'query', 'content': 'OAX-E20-CASE:' + name,
        })['task_id']
        def settled():
            d = self.api('/api/observe/v1/tasks/' + tid)
            return d if d['task']['status'] in TERMINAL else None
        d = self.wait(settled, 40)
        dest = self.out / 'cases' / label
        write(dest / 'task-run-journal.json', d)
        runs = d['run_attempts']
        assert len(runs) == 1, runs
        run = runs[0]
        assert run['worker_instance_id'] == self.worker['worker_instance_id'], run
        assert run['worker_generation'] == self.worker['generation'], run
        assert d.get('events'), 'missing Event Journal'
        assert any(e['aggregate_id'] == run['run_id'] for e in d['events']), 'missing Run Journal'
        after = set((self.root / 'runtime-raw').glob('run-*')) - before
        assert len(after) == 1, [str(x) for x in after]
        raw = after.pop()
        metadata = json.loads((raw / 'metadata.json').read_text())
        assert metadata['case'] == name and metadata['cwd'] == str(self.workspace), metadata
        # Fixture inputs are generated locally, contain no secrets, and are copied
        # byte-for-byte so malformed/truncated JSON remains independently auditable.
        for src in raw.iterdir():
            target = dest / 'process' / src.name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(src.read_bytes())
        proof = {'task_id': tid, 'run_id': run['run_id'], 'fixture_pid': metadata['pid'],
                 'raw_path': str(raw), 'worker_instance_id': run['worker_instance_id'],
                 'worker_generation': run['worker_generation'], 'task_status': d['task']['status'],
                 'completion_basis': d['task']['completion_basis'], 'turn_result': run.get('turn_result')}
        write(dest / 'correlation.json', proof)
        result = run.get('turn_result') or {}
        if name == 'normal':
            assert d['task']['status'] == 'succeeded', proof
            assert d['task']['completion_basis'] == 'query_result_delivered', proof
            assert d['task'].get('result') == 'OAX-E20-NORMAL', proof
            assert result.get('final_reply') is True, proof
        else:
            assert d['task']['status'] in ('failed', 'uncertain'), proof
            assert d['task']['completion_basis'] != 'query_result_delivered', proof
            assert result.get('final_reply') is False, proof
        print(json.dumps({'case': label, 'status': 'PASS', **{k: proof[k] for k in ('task_id', 'run_id', 'task_status')}}), flush=True)
        return proof


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('profile', 'evidence', 'binary', 'web', 'commit'):
        parser.add_argument('--' + name, required=True)
    parser.set_defaults(proxy='', model='default', worker_timeout='10s', keep=False)
    args = parser.parse_args()
    os.umask(0o077)
    live = Malformed(args)
    stage = 'setup'
    completed = {}
    try:
        live.setup()
        for name in CASES:
            stage = name
            completed[name] = live.case(name, name)
            stage = name + '-recovery'
            completed[stage] = live.case('normal', stage)
        write(live.out / 'verdict.json', {'status': 'PASS', 'coverage': 'D', 'completed': completed,
              'real_agy': False, 'installed_service': False})
    except Exception as exc:
        write(live.out / 'verdict.json', {'status': 'FAIL', 'coverage': 'D', 'stage': stage,
              'completed': completed, 'error': str(exc).replace(live.password, '[REDACTED]')})
        raise
    finally:
        write(live.out / 'cleanup.json', owned_stop(live.root))
        live.collect()


if __name__ == '__main__':
    main()
