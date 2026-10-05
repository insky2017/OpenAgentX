#!/usr/bin/env python3
"""Read-only runtime evidence. Never saves credentials, argv, or model output."""
import argparse
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess

AGENTS = ['openagentx', 'rhythm', 'pay-service', 'quote-service', 'identity-service', 'oneaxe-voice']
OPTIONS = ['@openagentx_managed', '@openagentx_agent_id', '@oax-managed', '@oax-agent-id',
           'automatic-rename', 'allow-rename', 'remain-on-exit', 'pane-border-status', 'pane-border-format']


def run(argv):
    p = subprocess.run(argv, capture_output=True, text=True, timeout=20)
    if p.returncode:
        raise RuntimeError(f'command failed ({p.returncode}): {argv[0]} {argv[1]}')
    return p.stdout.strip()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def process(pid):
    base = Path('/proc') / str(pid)
    stat = (base / 'stat').read_text().rsplit(')', 1)[1].split()
    result = {'pid': int(pid), 'starttime': stat[19], 'comm': (base / 'comm').read_text().strip()}
    try:
        result.update(executable=os.readlink(base / 'exe'), executable_sha256=sha(base / 'exe'))
    except PermissionError:
        result['executable_inspection'] = 'kernel_permission_denied; identity uses PID and starttime'
    return result


def children(pid):
    found = set()
    for thread in (Path('/proc') / str(pid) / 'task').glob('*/children'):
        try:
            found.update(int(x) for x in thread.read_text().split())
        except FileNotFoundError:
            pass
    return sorted(found)


def durable_children(pid, kind):
    result = []
    queue = [(child, 1) for child in children(pid)]
    seen = set()
    while queue:
        child, depth = queue.pop(0)
        if child in seen:
            continue
        seen.add(child)
        try:
            args = (Path('/proc') / str(child) / 'cmdline').read_bytes().split(b'\0')
            match = (b'app-server' in args if kind == 'backend' else
                     (b'resume' in args and b'--remote' in args) or
                     (b'agent' in args and b'open' in args and b'--native' in args))
            if match:
                result.append(process(child))
            if depth < 5 and not (match and kind == 'backend'):
                queue.extend((p, depth + 1) for p in children(child))
        except (FileNotFoundError, ProcessLookupError):
            pass
        except PermissionError:
            result.append({'pid': child, 'inspection': 'kernel_permission_denied'})
    return sorted(result, key=lambda p: p['pid'])


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=15)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


def overview(profile):
    path = str(profile / 'run/openagentx.sock')
    credentials = json.loads((profile / 'credentials.json').read_text())
    selection = credentials['current'][path]
    entry = next(c for c in credentials['credentials'] if c['socket_path'] == path and
                 c['installation_id'] == selection['installation_id'] and c['username'] == selection['username'])
    conn = UnixHTTP(path)
    try:
        conn.request('GET', '/api/observe/v1/overview', headers={'Authorization': 'Bearer ' + entry['token']})
        response = conn.getresponse()
        if response.status != 200:
            raise RuntimeError(f'Observe overview HTTP {response.status}')
        return json.loads(response.read())
    finally:
        conn.close()


def capture(profile, go_binary):
    fmt = '|'.join('#{' + k + '}' for k in ['session_name', 'window_id', 'window_name', 'pane_id',
                  'pane_index', 'pane_pid', 'pane_current_command', 'pane_dead'])
    keys = ['session', 'window_id', 'window_name', 'pane_id', 'pane_index', 'pane_pid', 'command', 'dead']
    panes = [dict(zip(keys, line.split('|'))) for line in run(['tmux', 'list-panes', '-s', '-t', 'OAX', '-F', fmt]).splitlines()]
    windows = {}
    for pane in panes:
        wid = pane['window_id']
        if wid not in windows:
            explicit = run(['tmux', 'show-options', '-w', '-t', wid])
            set_options = {line.split(' ', 1)[0] for line in explicit.splitlines()}
            values = {}
            for opt in OPTIONS:
                values[opt] = {'set': opt in set_options, 'value': run(['tmux', 'show-options', '-wqv', '-t', wid, opt])}
            windows[wid] = {'name': pane['window_name'], 'options': values}
        pane['process'] = process(int(pane['pane_pid'])) if pane['dead'] == '0' else None
        if pane['window_name'] in AGENTS and pane['pane_index'] == '0':
            pane['native_children'] = durable_children(int(pane['pane_pid']), 'terminal')
    data = {'captured_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'panes': panes, 'windows': windows,
            'installed_binary': {'path': str(Path.home() / '.local/bin/openagentx'),
                                 'sha256': sha(Path.home() / '.local/bin/openagentx'),
                                 'build_info': run([go_binary, 'version', '-m', str(Path.home() / '.local/bin/openagentx')])},
            'services': {}, 'threads': {}, 'configs_sha256': {}, 'workers': []}
    for agent in ['daemon'] + AGENTS:
        unit = 'openagentx.service' if agent == 'daemon' else f'openagentx-worker@{agent}.service'
        fields = run(['systemctl', '--user', 'show', unit, '--property=MainPID,ActiveState,SubState,ExecMainStartTimestampMonotonic'])
        service = dict(line.split('=', 1) for line in fields.splitlines())
        pid = int(service['MainPID'])
        service['process'] = process(pid) if pid else None
        service['backends'] = durable_children(pid, 'backend') if pid else []
        data['services'][agent] = service
        if agent != 'daemon':
            state = json.loads((profile / 'workers/codex' / agent / 'state.json').read_text())
            data['threads'][agent] = {key: state.get(key) for key in ['thread_id', 'task_id', 'run_id', 'state']}
            data['configs_sha256'][agent] = sha(profile / 'workers' / f'{agent}.yaml')
    live = overview(profile)
    data['observe'] = {'server_time': live.get('server_time'), 'latest_sequence': live.get('latest_sequence')}
    for worker in live.get('workers', []):
        if worker.get('agent_id') in AGENTS:
            data['workers'].append({k: worker.get(k) for k in ['agent_id', 'worker_instance_id', 'generation', 'status', 'started_at']})
    data['fleet_sha256'] = sha(profile / 'fleet.yaml')
    return data


def compare(before, after):
    def stable(value):
        if isinstance(value, dict):
            return {key: stable(item) for key, item in value.items() if key != 'executable'}
        if isinstance(value, list):
            return [stable(item) for item in value]
        return value
    checks = {}
    for agent in AGENTS:
        checks[agent] = {'service_unchanged': stable(before['services'][agent]) == stable(after['services'][agent]),
                         'thread_unchanged': before['threads'][agent]['thread_id'] == after['threads'][agent]['thread_id'],
                         'config_unchanged': before['configs_sha256'][agent] == after['configs_sha256'][agent]}
        old = next(p for p in before['panes'] if p['window_name'] == agent and p['pane_index'] == '0')
        new = next(p for p in after['panes'] if p['pane_id'] == old['pane_id'])
        checks[agent]['terminal_unchanged'] = all(stable(old.get(k)) == stable(new.get(k)) for k in
                ['window_id', 'window_name', 'pane_id', 'pane_index', 'pane_pid', 'process', 'native_children', 'dead'])
        identity = lambda snap: sorted((w['worker_instance_id'], w['generation'], w['started_at'])
                                       for w in snap['workers'] if w['agent_id'] == agent)
        checks[agent]['worker_identity_unchanged'] = identity(before) == identity(after) and bool(identity(after))
    extra_panes_unchanged = all(any(p['pane_id'] == old['pane_id'] and stable(p.get('process')) == stable(old.get('process'))
                                       for p in after['panes']) for old in before['panes'] if old['window_name'] != 'overview')
    return {'agents': checks, 'all_nonoverview_panes_preserved': extra_panes_unchanged,
            'daemon_unchanged': stable(before['services']['daemon']) == stable(after['services']['daemon']),
            'fleet_unchanged': before['fleet_sha256'] == after['fleet_sha256'],
            'pass': all(all(v.values()) for v in checks.values()) and extra_panes_unchanged and
                    stable(before['services']['daemon']) == stable(after['services']['daemon']) and before['fleet_sha256'] == after['fleet_sha256']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--profile', type=Path, default=Path.home() / '.openagentx')
    parser.add_argument('--go-bin', default='go')
    parser.add_argument('--compare', type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    result = capture(args.profile, args.go_bin)
    if args.compare:
        result['continuity'] = compare(json.loads(args.compare.read_text()), result)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    with args.out.open('x') as out:
        json.dump(result, out, ensure_ascii=False, indent=2)
        out.write('\n')
    print(json.dumps({'out': str(args.out), 'agents': AGENTS, 'continuity': result.get('continuity')}, ensure_ascii=False))
    return 0 if result.get('continuity', {}).get('pass', True) else 1


if __name__ == '__main__':
    raise SystemExit(main())
