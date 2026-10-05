#!/usr/bin/env python3
"""One approved migration of six live OAX windows; never restarts any process."""
import argparse
import json
import os
from pathlib import Path
import subprocess

from terminal_overview_snapshot import AGENTS, capture, compare

LEGACY = ['@oax-managed', '@oax-agent-id']


def save(path, value):
    with path.open('x') as output:
        json.dump(value, output, ensure_ascii=False, indent=2)
        output.write('\n')


def tmux(args, commands):
    entry = {'argv': ['tmux', *args]}
    commands.append(entry)
    result = subprocess.run(entry['argv'], capture_output=True, text=True, timeout=10)
    entry.update(exit_code=result.returncode, stdout=result.stdout, stderr=result.stderr)
    if result.returncode:
        raise RuntimeError('tmux operation failed; see recorded commands')
    return result.stdout.strip()


def plan(snapshot):
    result = []
    for agent in AGENTS:
        candidates = [(wid, window) for wid, window in snapshot['windows'].items() if window['name'] == agent]
        if len(candidates) != 1:
            raise RuntimeError(f'{agent}: window identity ambiguous')
        wid, window = candidates[0]
        panes = [p for p in snapshot['panes'] if p['window_id'] == wid and p['pane_index'] == '0']
        if len(panes) != 1 or panes[0]['dead'] != '0':
            raise RuntimeError(f'{agent}: live pane 0 missing')
        pane = panes[0]
        processes = [pane['process'], *pane.get('native_children', [])]
        if not any(p.get('role') == 'native_bridge' and p.get('agent_id') == agent for p in processes):
            raise RuntimeError(f'{agent}: actual native bridge identity not proven')
        tid = snapshot['threads'][agent]['thread_id']
        if not any(p.get('role') == 'codex_tui' and tid in p.get('thread_ids', []) for p in processes):
            raise RuntimeError(f'{agent}: actual native frontend thread not proven')
        opts = window['options']
        for key, expected in [('@oax-managed', '1'), ('@oax-agent-id', agent),
                              ('@openagentx_managed', '1'), ('@openagentx_agent_id', agent)]:
            if opts[key]['set'] and opts[key]['value'] != expected:
                raise RuntimeError(f'{agent}: conflicting marker {key}')
        for other_id, other in snapshot['windows'].items():
            if other_id != wid and any(other['options'][key]['value'] == agent
                                     for key in ['@oax-agent-id', '@openagentx_agent_id']):
                raise RuntimeError(f'{agent}: marker used by another window')
        desired = {'automatic-rename': 'off', 'allow-rename': 'off', 'remain-on-exit': 'on',
                   '@openagentx_managed': '1', '@openagentx_agent_id': agent}
        result.append({'agent': agent, 'window_id': wid, 'pane_id': pane['pane_id'],
                       'pane_pid': pane['pane_pid'], 'before': opts, 'set': desired, 'unset': LEGACY})
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True, help='New private evidence directory')
    parser.add_argument('--profile', type=Path, default=Path.home() / '.openagentx')
    parser.add_argument('--go-bin', default='go')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    os.umask(0o077)
    args.out.mkdir(parents=True, exist_ok=False)
    commands, touched, verdict = [], [], {'status': 'FAILED', 'applied': args.apply}
    try:
        before = capture(args.profile, args.go_bin)
        save(args.out / 'before.json', before)
        continuity = compare(json.loads(args.baseline.read_text()), before)
        save(args.out / 'preflight-continuity.json', continuity)
        if not continuity['pass']:
            raise RuntimeError('business runtime changed since approved baseline; do not mutate')
        changes = plan(before)
        save(args.out / 'plan.json', changes)
        if args.apply:
            for change in changes:
                wid = change['window_id']
                handle = tmux(['display-message', '-p', '-t', change['pane_id'], '-F',
                               '#{session_name}|#{window_id}|#{window_name}|#{pane_index}|#{pane_pid}'], commands)
                expected = '|'.join(['OAX', wid, change['agent'], '0', change['pane_pid']])
                if handle != expected:
                    raise RuntimeError('target pane changed before mutation')
                touched.append(change)
                for key, value in change['set'].items():
                    tmux(['set-option', '-w', '-t', wid, key, value], commands)
                    if tmux(['show-options', '-wqv', '-t', wid, key], commands) != value:
                        raise RuntimeError('canonical marker/option readback mismatch')
                for key in LEGACY:
                    tmux(['set-option', '-wu', '-t', wid, key], commands)
                    if tmux(['show-options', '-wqv', '-t', wid, key], commands):
                        raise RuntimeError('legacy marker remained after removal')
            after = capture(args.profile, args.go_bin)
            save(args.out / 'after.json', after)
            continuity = compare(before, after)
            save(args.out / 'postflight-continuity.json', continuity)
            if not continuity['pass']:
                raise RuntimeError('runtime continuity check failed')
            if any(w['options'][key]['set'] for w in after['windows'].values() for key in LEGACY):
                raise RuntimeError('legacy markers remain in OAX; no success claim')
        verdict.update(status='PASS' if args.apply else 'READY', windows=len(changes),
                       note='existing border labels retained; canonical product applies shared labels on future open')
    except Exception as exc:
        verdict['error'] = str(exc)
        if touched:
            failures = []
            for change in reversed(touched):
                for key in [*change['set'], *LEGACY]:
                    old = change['before'][key]
                    try:
                        argv = ['set-option', '-w', '-t', change['window_id'], key, old['value']] if old['set'] else ['set-option', '-wu', '-t', change['window_id'], key]
                        tmux(argv, commands)
                    except Exception as restore_error:
                        failures.append(str(restore_error))
            verdict['restoration_failures'] = failures
    finally:
        save(args.out / 'commands.json', commands)
        save(args.out / 'verdict.json', verdict)
    print(json.dumps(verdict, ensure_ascii=False, indent=2))
    return 0 if verdict['status'] in ['PASS', 'READY'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
