#!/usr/bin/env python3
"""Verify a prepared isolated real Codex Worker and tmux status rendering.

Preparation/cleanup reuse native_model_settings_e2e.py. This script touches only
its owned profile and -L tmux server, uses formal APIs, and preserves evidence.
"""
import argparse
import json
import re
from pathlib import Path
import sys
import time

sys.dont_write_bytecode = True
from native_model_settings_e2e import SettingsRun
from terminal_overview_e2e import AttachedClient, now, require, sha


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--root', type=Path, required=True)
    args = parser.parse_args()
    args.phase = 'native'
    run = SettingsRun.restore(args)
    window = run.windows['native']
    pane = window + '.0'
    ordinary = run.tmux('new-window', '-P', '-F', '#{window_id}', '-t', '=OAX', '-n', 'ordinary', 'sleep', '86400')
    run.windows['ordinary'] = ordinary
    for option, value in {'window-status-format': 'USER:#W', 'window-status-current-format': 'USER-CURRENT:#W'}.items():
        run.tmux('set-option', '-w', '-t', ordinary, option, value)
    names_before = run.tmux('list-windows', '-t', '=OAX', '-F', '#{window_id}:#{window_name}')
    before = run.snapshot('spinner-before')
    require(run.tmux('show-options', '-w', '-v', '-t', window, '@oax_state') == 'idle', 'initial state is not idle')
    run.client = AttachedClient(run)
    log_path = run.root / 'attached-client.pty.log'
    offset = log_path.stat().st_size
    state = json.loads((run.root / 'profile/workers/codex' / run.aid / 'state.json').read_text())
    dispatch_at = time.monotonic()
    receipt = run.api('/api/control/v1/tasks', {
        'target_agent_id': run.aid, 'organization_id': 'default', 'dispatch_mode': 'direct', 'intent': 'mutation',
        'runtime_session': {'backend_id': 'codex', 'provider_session_id': state['thread_id']},
        'content': 'Execute exactly one local Python command in this workspace: python3 -c "import pathlib,time; pathlib.Path(\'spinner-started.txt\').write_text(\'STARTED\\n\'); time.sleep(20); pathlib.Path(\'spinner-proof.txt\').write_text(\'SPINNER-PROOF-DONE\\n\')". Wait for the command to finish. Then reply exactly: SPINNER-PROOF-DONE. What next? Do not perform any other work or spawn subagents.'})
    task_id = receipt['task_id']
    samples = []
    first_running = None
    deadline = time.monotonic() + 180
    try:
        while time.monotonic() < deadline:
            current = run.tmux('show-options', '-w', '-v', '-t', window, '@oax_state')
            elapsed = time.monotonic() - dispatch_at
            if current == 'running' and first_running is None:
                first_running = elapsed
            if first_running is not None and elapsed - first_running >= 6:
                run.tmux('select-window', '-t', window)
            samples.append({'at': now(), 'elapsed': elapsed, 'state': current,
                'active': run.tmux('display-message', '-p', '-t', window, '#{window_active}'),
                'normal': run.tmux('display-message', '-p', '-t', window, '#{E:window-status-format}'),
                'current': run.tmux('display-message', '-p', '-t', window, '#{E:window-status-current-format}'),
                'ordinary_format': run.tmux('show-options', '-w', '-v', '-t', ordinary, 'window-status-format')})
            if first_running is not None and current == 'idle':
                break
            time.sleep(1)
        require(first_running is not None, 'never observed running')
        require(samples[-1]['state'] == 'idle', 'completion did not clear running')
        detail = run.wait(lambda: terminal(run, task_id), 120)
        run.save('spinner-samples.json', samples)
        run.save('spinner-task.json', detail)
        require(len(detail['run_attempts']) == 1 and detail['run_attempts'][0]['status'] == 'succeeded', 'real Runtime did not succeed')
        require(detail['task']['status'] == 'uncertain' and detail['task']['error'] == 'business_effect_unverified', 'mutation Task contract changed')
        proof = run.root / 'workspace/spinner-proof.txt'
        require(proof.read_bytes() == b'SPINNER-PROOF-DONE\n', 'independent file proof missing')
        time.sleep(2)
        run.client.close()
        run.client = None
        pty = log_path.read_bytes()[offset:].decode('utf-8', errors='replace')
        plain_pty = re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]', '', pty)
        status_lines = re.findall(r'\[OAX\].{0,155}', plain_pty)
        status_text = '\n'.join(status_lines)
        run.save('spinner-rendered-status.json', status_lines)
        frames = [frame for frame in '⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏' if frame in status_text]
        require(any(run.aid in line and any(frame in line for frame in frames) for line in status_lines), 'window name was lost from rendered running status')
        require(any(run.aid in line and not any(frame in line for frame in frames) for line in status_lines), 'rendered idle status missing')
        require(len(frames) >= 5, 'attached tmux did not animate enough distinct frames')
        require(any(sample['state'] == 'running' and sample['active'] == '0' for sample in samples), 'non-current window was not observed')
        require(any(sample['state'] == 'running' and sample['active'] == '1' for sample in samples), 'current window was not observed')
        require(all(sample['ordinary_format'] == 'USER:#W' for sample in samples), 'ordinary window format changed')
        require(run.tmux('show-options', '-w', '-v', '-t', ordinary, 'window-status-current-format') == 'USER-CURRENT:#W', 'ordinary current format changed')
        require(run.tmux('list-windows', '-t', '=OAX', '-F', '#{window_id}:#{window_name}') == names_before, 'real window names changed')
        require(run.tmux('show-options', '-v', '-t', 'OAX', 'status-interval') == '1', 'status interval is not one second')
        run.save('spinner-samples.json', samples)
        run.save('spinner-task.json', detail)
        run.save('spinner-result.json', {'at': now(), 'result': 'PASS', 'evidence_level': 'R real Codex Worker + formal API + isolated tmux PTY + independent file',
            'binary_sha256': sha(args.binary), 'task_id': task_id, 'task_status': detail['task']['status'], 'task_error': detail['task']['error'], 'first_running_seconds': first_running,
            'frames_seen_in_attached_pty': frames, 'proof': proof.name, 'proof_sha256': sha(proof),
            'names_before_after': names_before, 'normal_and_current': True, 'ordinary_formats_preserved': True,
            'limits': 'Approval and two-Agent isolation use real tmux + controllable Runtime integration tests; no production services installed/restarted.'})
        run.task_evidence()
        run.snapshot('spinner-after')
        print(json.dumps({'result': 'PASS', 'task_id': task_id, 'task_status': detail['task']['status'], 'task_error': detail['task']['error'], 'first_running_seconds': first_running, 'frames': len(frames)}, ensure_ascii=False))
    finally:
        if run.client:
            run.client.close()
        run.checkpoint()


def terminal(run, task_id):
    detail = run.api('/api/observe/v1/tasks/' + task_id)
    return detail if detail['task']['status'] in {'succeeded', 'failed', 'uncertain', 'canceled'} else None


if __name__ == '__main__':
    main()
