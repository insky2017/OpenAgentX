#!/usr/bin/env python3
"""E17: isolated real AGY faults; official APIs only, no installed-service changes."""
import argparse, datetime, hashlib, json, os, pathlib, signal, sys, time
from agy_live import owned_stop, write
from agy_workflow import Workflow
P = pathlib.Path


def proc(pid):
    try:
        # comm may contain spaces; parse after its closing parenthesis.
        tail = P('/proc', str(pid), 'stat').read_text().rsplit(')', 1)[1].split()
        return dict(pid=int(pid), state=tail[0], ppid=int(tail[1]), pgid=int(tail[2]), starttime=tail[19])
    except (FileNotFoundError, ProcessLookupError):
        return None


def living(ref):
    now = proc(ref['pid'])
    return bool(now and now['starttime'] == ref['starttime'] and now['state'] != 'Z')


def descendants(refs):
    found = {r['pid']: proc(r['pid']) for r in refs if living(r)}
    table = [proc(p.name) for p in P('/proc').iterdir() if p.name.isdigit()]
    while True:
        children = [r for r in table if r and r['ppid'] in found and r['pid'] not in found]
        if not children:
            return list(found.values())
        found.update({r['pid']: r for r in children})


def stop_exact(refs):
    refs = descendants(refs)
    for sig in [signal.SIGTERM, signal.SIGKILL]:
        for r in reversed(refs):
            if living(r):
                try: os.kill(r['pid'], sig)
                except ProcessLookupError: pass
        deadline = time.monotonic() + 5
        while any(living(r) for r in refs) and time.monotonic() < deadline:
            time.sleep(.2)
    return [{**r, 'alive_after': living(r)} for r in refs]


class Recovery(Workflow):
    def current(self, prefix):
        return next(r for r in reversed(self.procs) if r['label'].startswith(prefix))

    def daemon_restart(self, label):
        self.start('daemon-' + label, [self.a.binary, 'serve', '--http-addr', '127.0.0.1:' + str(self.port), '--web-dir', self.a.web])
        def login():
            try:
                result = self.api('/api/auth/v1/login', {'username': 'owner', 'password': self.password}, auth=True)
                self.csrf = result['csrf_token']
                return True
            except OSError:
                return False
        self.wait(login, 30)

    def worker_restart(self, label, previous):
        self.start('worker-' + label, [self.a.binary, 'worker', 'run', '--config', str(self.root/'worker.yaml')])
        def registered():
            workers = self.api('/api/observe/v1/overview')['workers']
            candidates = [w for w in workers if w['agent_id'] == self.aid and w['generation'] > previous['generation']]
            return max(candidates, key=lambda w: w['generation']) if candidates else None
        worker = self.wait(registered, 30)
        binding = next(b for b in self.api('/api/observe/v1/network-profiles?agent_id=' + self.aid)['bindings'] if b['backend_id'] == 'primary')
        assert binding['mode'] in ['inherit', 'direct'], binding
        receipt = self.api('/api/control/v1/network-bindings/mode/tests', {'meta': {'expected_version': binding['version']}, 'agent_id': self.aid, 'backend_id': 'primary', 'mode': binding['mode'], 'worker_instance_id': worker['worker_instance_id'], 'generation': worker['generation']})['receipt']
        def tested():
            tests = self.api('/api/observe/v1/network-profiles?agent_id=' + self.aid).get('mode_tests', [])
            test = next((t for t in tests if t['test_id'] == receipt['test_id']), None)
            if test and test['state'] == 'failed': raise RuntimeError('new generation network test failed: ' + test.get('diagnostic_code', ''))
            return test if test and test['state'] == 'succeeded' else None
        self.wait(tested, 30)
        self.api('/api/control/v1/network-bindings/mode/publish', {'meta': {'expected_version': binding['version']}, 'test_id': receipt['test_id'], 'worker_instance_id': worker['worker_instance_id'], 'generation': worker['generation']})
        def ready():
            overview = self.api('/api/observe/v1/overview')
            workers = [w for w in overview['workers'] if w['agent_id'] == self.aid and w['generation'] > previous['generation']]
            if not workers: return None
            worker = max(workers, key=lambda w: w['generation'])
            net = self.api('/api/observe/v1/network-profiles?agent_id=' + self.aid)
            applied = [b for b in net.get('bindings', []) if b.get('desired_status') == 'applied' and b.get('applied_worker_id') == worker['worker_instance_id'] and b.get('applied_generation') == worker['generation'] and b.get('applied_binding_revision') == b.get('version') and b.get('applied_policy_version') == b.get('policy_version')]
            agent = next(a for a in overview['agents'] if a['agent_id'] == self.aid)
            if applied and agent.get('readiness', {}).get('ready') and agent['readiness'].get('can_start_now'):
                self.worker = worker
                return {'overview': overview, 'network': net}
        write(self.out/'cases'/label/'new-generation-ready.json', self.wait(ready, 45))

    def recovery_case(self, target):
        label = target + '-sigkill'
        self.prepare_wait(label, 600)
        tid = self.task('Run python3 ' + label + '.py in your assigned workspace exactly once and wait for it to finish. Do not run any other script or retry this command.')
        started = self.started(label, 150)
        before = self.detail(tid)
        run = self.latest_run(before)
        assert before['task']['status'] == 'running' and len(before['run_attempts']) == 1, before
        old_worker = self.worker
        tree = descendants([self.current('worker')])
        self.runtime_refs.extend(tree)
        fault = self.current(target)
        assert living(fault), fault
        case = self.out/'cases'/label
        write(case/'before-task-run-journal.json', before)
        write(case/'before-process-tree.json', tree)
        write(case/'fault.json', {'target': fault, 'signal': 'SIGKILL', 'at': time.time(), 'started': started, 'lease_wait_bound_seconds': 305, 'lease_bound_basis': 'worker_service.go RunLease=5m; heartbeat max(existing,now+30s)'})
        os.kill(fault['pid'], signal.SIGKILL)
        self.wait(lambda: not living(fault), 10)
        expiry = time.time() + 305
        while time.time() < expiry:
            print(json.dumps({'case': label, 'waiting_for_run_lease_seconds': round(expiry-time.time())}), flush=True)
            time.sleep(min(30, max(0, expiry-time.time())))
        write(case/'expired-process-state.json', [{**r, 'alive': living(r)} for r in tree])
        if target == 'worker':
            expired = self.detail(tid)
            write(case/'lease-expired-before-daemon-restart.json', expired)
            write(case/'daemon-controlled-stop.json', stop_exact([self.current('daemon')]))
        self.daemon_restart(label)
        recovered = self.settle(tid, label, timeout=25)
        assert recovered['task']['status'] == 'uncertain', recovered['task']
        assert len(recovered['run_attempts']) == 1 and recovered['run_attempts'][0]['status'] == 'uncertain', recovered['run_attempts']
        write(case/'old-runtime-controlled-cleanup.json', stop_exact(tree + started['processes']))
        assert not any(living(r) for r in tree + started['processes'])
        self.worker_restart(label, old_worker)
        query = self.query('after-' + label)
        assert query['run_attempts'][0]['worker_instance_id'] == self.worker['worker_instance_id']
        final = self.detail(tid)
        write(case/'final-task-run-journal.json', final)
        calls = (self.root/'workspace'/(label+'.invocations.jsonl')).read_text().splitlines()
        proof = {'task_id': tid, 'run_id': run['run_id'], 'invocations': len(calls), 'invocation_records': [json.loads(x) for x in calls], 'late_effect_exists': (self.root/'workspace'/(label+'.late.txt')).exists(), 'new_worker': self.worker, 'query_task': query['task']['id'], 'recovery_requires_daemon_startup': True}
        write(case/'independent-proof.json', proof)
        assert len(calls) == 1 and not proof['late_effect_exists'], proof
        assert final['task']['status'] == 'uncertain' and len(final['run_attempts']) == 1, final
        print(json.dumps({'case': label, 'status': 'PASS', 'task_id': tid}), flush=True)
        return proof


def main():
    p = argparse.ArgumentParser()
    for name in ['profile', 'evidence', 'binary', 'web', 'commit']: p.add_argument('--' + name, required=True)
    p.add_argument('--proxy', default='http://127.0.0.1:7897')
    p.add_argument('--model', default='gemini-3.7-flash-low')
    p.add_argument('--worker-timeout', default='700s')
    args = p.parse_args()
    os.umask(0o077)
    live = Recovery(args); live.runtime_refs = []; stage = 'setup'; completed = {}
    write(live.out/'invocation.json', {'argv': sys.argv, 'script_sha256': hashlib.sha256(P(__file__).read_bytes()).hexdigest(), 'at': datetime.datetime.now(datetime.timezone.utc).isoformat()})
    try:
        live.setup()
        for stage in ['daemon', 'worker']: completed[stage] = live.recovery_case(stage)
        write(live.out/'verdict.json', {'status': 'PASS', 'coverage': 'R real AGY isolated E17 daemon and Worker SIGKILL; not installed service', 'candidate': args.commit, 'completed': completed, 'limitation': 'Expired runs reconcile at daemon startup; Worker lease expiry alone does not trigger reconciliation.'})
    except Exception as error:
        write(live.out/'verdict.json', {'status': 'FAIL', 'stage': stage, 'completed': completed, 'error': str(error).replace(live.password, '[REDACTED]')})
        raise
    finally:
        write(live.out/'runtime-cleanup.json', stop_exact(live.runtime_refs))
        write(live.out/'cleanup.json', owned_stop(live.root))
        live.collect()


if __name__ == '__main__': main()
