#!/usr/bin/env python3
"""Targeted real AGY lease/reconcile verification; isolated R evidence, no capture."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import signal
import secrets
import sqlite3
import sys
import time
from agy_live import owned_stop, write
from agy_recovery import Recovery, descendants, living, stop_exact

P = pathlib.Path


def epoch(value):
    return datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()


class LeaseRecovery(Recovery):
    def lease_snapshot(self, tid):
        # Observation only: safe Observe projections intentionally omit Run lease.
        database = self.root/'profile/data/openagentx.db'
        with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True) as db:
            db.row_factory = sqlite3.Row
            runs = [dict(row) for row in db.execute(
                'SELECT run_id,task_id,status,worker_instance_id,lease_until,started_at,updated_at '
                'FROM run_attempts WHERE task_id=? ORDER BY started_at', (tid,))]
        overview = self.api('/api/observe/v1/overview')
        workers = [w for w in overview['workers'] if w['agent_id'] == self.aid]
        return {'at': time.time(), 'observation': 'read-only SQLite Run lease plus official Observe API',
                'runs': runs, 'workers': workers, 'task_detail': self.detail(tid)}

    def invocation_records(self, label):
        return [json.loads(line) for line in
                (self.root/'workspace'/(label+'.invocations.jsonl')).read_text().splitlines()]

    def long_lease_case(self):
        label = 'long-lease'
        self.prepare_wait(label, 95)
        tid = self.task('Run python3 long-lease.py in your assigned workspace exactly once and wait for it to finish. This query explicitly authorizes this one diagnostic tool. Do not run any other script or retry this command. After it completes reply exactly LONG-LEASE-DONE.', intent='query')
        started = self.started(label, 150)
        tree = descendants([self.current('worker')])
        self.runtime_refs.extend(tree)
        write(self.out/'cases'/label/'process-tree.json', tree)
        before = self.detail(tid)
        first_run_id = self.latest_run(before)['run_id']
        supplement_marker = 'QUERY-SUPPLEMENT-' + self.root.name[-8:]
        body = {'meta': {'expected_version': before['task']['version'], 'idempotency_key': 'query-supplement-' + secrets.token_hex(8)},
                'content': 'For the next turn only: reply exactly ' + supplement_marker + '. Do not use tools or rerun long-lease.py; the first turn already handles the diagnostic.'}
        receipt = self.api('/api/control/v1/tasks/' + tid + '/messages', body)
        replay = self.api('/api/control/v1/tasks/' + tid + '/messages', body)
        assert receipt == replay, (receipt, replay)
        write(self.out/'cases'/label/'supplement-receipt-replay.json', {'receipt': receipt, 'replay': replay})
        pending = self.api('/api/observe/v1/mailboxes?agent_id=' + self.aid)
        write(self.out/'cases'/label/'supplement-mailbox-pending.json', pending)
        item = next(x for x in pending if x.get('message_id') == receipt['message_id'])
        assert item['state'] == 'pending' and item['lane'] == 'work', item
        checkpoints = []
        origin = time.monotonic()
        for offset in range(0, 91, 15):
            time.sleep(max(0, origin + offset - time.monotonic()))
            snapshot = self.lease_snapshot(tid)
            snapshot['elapsed_since_first_checkpoint'] = time.monotonic() - origin
            snapshot['tool_processes'] = [{**r, 'alive': living(r)} for r in started['processes']]
            checkpoints.append(snapshot)
            write(self.out/'cases'/label/'lease-checkpoints.json', checkpoints)
            assert snapshot['task_detail']['task']['status'] == 'running', snapshot
            assert len(snapshot['runs']) == 1 and snapshot['runs'][0]['status'] in ['starting', 'running'], snapshot
            assert all(r['alive'] for r in snapshot['tool_processes']), snapshot
            assert epoch(snapshot['runs'][0]['lease_until']) > snapshot['at'], snapshot
            print(json.dumps({'case': label, 'elapsed_seconds': round(snapshot['elapsed_since_first_checkpoint']), 'status': 'running'}), flush=True)
        assert epoch(checkpoints[-1]['runs'][0]['lease_until']) - epoch(checkpoints[0]['runs'][0]['lease_until']) >= 85, checkpoints
        final = self.settle(tid, label, timeout=210, min_runs=2)
        self.successful_run(final)
        assert len(final['run_attempts']) == 2, final
        first = next(r for r in final['run_attempts'] if r['run_id'] == first_run_id)
        second = self.latest_run(final)
        write(self.out/'cases'/label/'first-run-terminal.json', first)
        assert first['status'] == 'succeeded' and first.get('turn_result', {}).get('body', '').strip().endswith('LONG-LEASE-DONE'), first
        assert second['run_id'] != first_run_id and second.get('turn_result', {}).get('body', '').strip() == supplement_marker, second
        assert final['task']['status'] == 'succeeded' and final['task']['completion_basis'] == 'query_result_delivered', final['task']
        assert final['task']['result'].strip() == supplement_marker, final['task']
        accepted = self.api('/api/observe/v1/mailboxes?agent_id=' + self.aid)
        write(self.out/'cases'/label/'supplement-mailbox-final.json', accepted)
        item = next(x for x in accepted if x.get('message_id') == receipt['message_id'])
        assert item['state'] == 'accepted', item
        assert len([m for m in final['messages'] if m['id'] == receipt['message_id']]) == 1, final['messages']
        self.exact_file(label+'.late.txt', b'late\n', label)
        records = self.invocation_records(label)
        assert len(records) == 1, records
        query = self.query('after-long-lease')
        assert query['run_attempts'][0]['worker_instance_id'] == self.worker['worker_instance_id']
        proof = {'task_id': tid, 'first_run_id': first_run_id, 'supplement_run_id': second['run_id'],
                 'supplement_message_id': receipt['message_id'], 'supplement_reply': supplement_marker, 'idempotent_replay': True,
                 'tool_wait_seconds': 95, 'checkpoint_span_seconds': checkpoints[-1]['elapsed_since_first_checkpoint'],
                 'run_lease_advance_seconds': epoch(checkpoints[-1]['runs'][0]['lease_until']) - epoch(checkpoints[0]['runs'][0]['lease_until']),
                 'invocation_records': records, 'same_worker_query_task_id': query['task']['id']}
        write(self.out/'cases'/label/'independent-proof.json', proof)
        return proof

    def worker_periodic_recovery_case(self):
        label = 'worker-periodic-recovery'
        self.prepare_wait(label, 240)
        tid = self.task('Run python3 worker-periodic-recovery.py in your assigned workspace exactly once and wait for it to finish. Do not run any other script or retry this command.')
        started = self.started(label, 150)
        before = self.lease_snapshot(tid)
        assert before['task_detail']['task']['status'] == 'running' and len(before['runs']) == 1, before
        old_worker = self.worker
        daemon = self.current('daemon')
        fault = self.current('worker')
        tree = descendants([fault])
        self.runtime_refs.extend(tree)
        case = self.out/'cases'/label
        write(case/'before-lease-task-run-journal.json', before)
        write(case/'before-process-tree.json', tree)
        assert living(daemon) and living(fault)
        fault_at = time.time()
        remaining_lease = max(0, epoch(before['runs'][0]['lease_until']) - fault_at)
        assert remaining_lease <= 35, ('unexpected Run lease: candidate may be wrong', remaining_lease)
        bound = min(75, remaining_lease + 30)
        write(case/'fault.json', {'worker': fault, 'daemon_must_remain': daemon, 'signal': 'SIGKILL',
              'at': fault_at, 'observed_remaining_run_lease_seconds': remaining_lease,
              'recovery_wait_bound_seconds': bound, 'periodic_reconcile_seconds': 10})
        os.kill(fault['pid'], signal.SIGKILL)
        self.wait(lambda: not living(fault), 10)
        observed = []
        deadline = time.monotonic() + bound
        recovered = None
        while time.monotonic() < deadline:
            snapshot = self.lease_snapshot(tid)
            snapshot['elapsed_after_fault_seconds'] = time.time() - fault_at
            snapshot['daemon_same_pid_starttime_alive'] = living(daemon)
            observed.append(snapshot)
            write(case/'recovery-observations.json', observed)
            assert living(daemon), 'daemon unexpectedly stopped'
            assert len(snapshot['runs']) == 1, snapshot
            if snapshot['task_detail']['task']['status'] == 'uncertain':
                recovered = snapshot
                break
            time.sleep(5)
        assert recovered, 'periodic reconcile did not close the task within lease plus bounded margin'
        assert recovered['runs'][0]['status'] == 'uncertain', recovered
        write(case/'recovered-task-run-journal.json', recovered['task_detail'])
        pre_cleanup = [{**r, 'alive': living(r)} for r in tree + started['processes']]
        write(case/'old-runtime-before-test-cleanup.json', {
            'note': 'State after automatic ledger recovery, BEFORE harness cleanup; child termination is not asserted as product behavior.',
            'processes': pre_cleanup})
        cleanup = stop_exact(tree + started['processes'])
        write(case/'old-runtime-test-cleanup.json', {
            'note': 'Explicit test-only PID/starttime cleanup; not product automatic cleanup.', 'processes': cleanup})
        assert not any(living(r) for r in tree + started['processes'])
        records = self.invocation_records(label)
        assert len(records) == 1, records
        self.worker_restart(label, old_worker)
        query = self.query('after-worker-periodic-recovery')
        assert query['run_attempts'][0]['worker_instance_id'] == self.worker['worker_instance_id']
        final = self.detail(tid)
        write(case/'final-old-task-run-journal.json', final)
        assert final['task']['status'] == 'uncertain' and len(final['run_attempts']) == 1, final
        assert len(self.invocation_records(label)) == 1
        assert not (self.root/'workspace'/(label+'.late.txt')).exists()
        assert living(daemon), 'daemon PID/starttime changed'
        proof = {'task_id': tid, 'run_id': before['runs'][0]['run_id'],
                 'recovery_seconds': recovered['elapsed_after_fault_seconds'],
                 'daemon': daemon, 'daemon_restarted': False,
                 'old_runtime_alive_before_test_cleanup': sum(r['alive'] for r in pre_cleanup),
                 'runtime_cleanup': 'explicit test cleanup, not product automatic termination',
                 'invocation_records': records, 'late_effect_exists': False,
                 'old_worker': old_worker, 'new_worker': self.worker,
                 'new_generation_query_task_id': query['task']['id']}
        write(case/'independent-proof.json', proof)
        return proof


def main():
    parser = argparse.ArgumentParser()
    for name in ['profile', 'evidence', 'binary', 'web', 'commit']:
        parser.add_argument('--'+name, required=True)
    parser.add_argument('--build-manifest', required=True)
    parser.add_argument('--proxy', default='http://127.0.0.1:7897')
    parser.add_argument('--model', default='gemini-3.7-flash-low')
    parser.add_argument('--worker-timeout', default='360s')
    parser.add_argument('--cases', choices=['all', 'recovery'], default='all')
    args = parser.parse_args()
    args.no_capture = True
    os.umask(0o077)
    live = LeaseRecovery(args)
    live.runtime_refs = []
    stage = 'setup'
    completed = {}
    write(live.out/'invocation.json', {'argv': sys.argv, 'at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
          'harness_files_sha256': {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in
           [P(__file__), P(__file__).with_name('agy_recovery.py'), P(__file__).with_name('agy_workflow.py'), P(__file__).with_name('agy_live.py')]}})
    try:
        live.setup()
        role_path = live.root/'workspace/ROLE.md'
        role_path.write_text(role_path.read_text().replace('For query tasks reply with requested marker and do not use tools or access files.',
                             'For query tasks reply with the requested marker. Do not use tools or access files unless the query explicitly requests a named diagnostic tool; then run only that tool.'))
        write(live.out/'role-instructions.json', {'text': role_path.read_text(), 'sha256': hashlib.sha256(role_path.read_bytes()).hexdigest()})
        manifest = json.loads((live.out/'manifest.json').read_text())
        build = json.loads(P(args.build_manifest).read_text())
        manifest['source_kind'] = 'frozen candidate; source and build provenance in build-manifest.json'
        write(live.out/'manifest.json', manifest)
        write(live.out/'build-manifest.json', build)
        actions = [('worker-periodic-recovery', live.worker_periodic_recovery_case)]
        if args.cases == 'all':
            actions.insert(0, ('long-lease', live.long_lease_case))
        for stage, action in actions:
            completed[stage] = action()
            print(json.dumps({'case': stage, 'status': 'PASS'}), flush=True)
        write(live.out/'verdict.json', {'status': 'PASS', 'candidate': args.commit,
              'scope': 'R targeted real AGY: ' + ', '.join(completed),
              'completed': completed, 'limits': ['No installed-service or first-use wizard claim',
               'Old Runtime children were explicitly cleaned by the test harness; no automatic child termination claim',
               'Formal Worker/API diagnostics only; no raw AGY byte interception']})
    except Exception as error:
        write(live.out/'verdict.json', {'status': 'FAIL', 'stage': stage, 'completed': completed,
              'error': str(error).replace(live.password, '[REDACTED]')})
        raise
    finally:
        write(live.out/'runtime-test-cleanup.json', stop_exact(live.runtime_refs))
        write(live.out/'cleanup.json', owned_stop(live.root))
        live.collect()


if __name__ == '__main__':
    main()
