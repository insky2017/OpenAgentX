#!/usr/bin/env python3
"""Installed Console I/R smoke. Run only after installation and Agent exclusivity.

All task mutations are typed into the real installed Console through a PTY.
The Unix HTTP client is GET-only and uses the existing installation-bound CLI
credential without logging request headers. No fake Runtime or service stubs.
"""
import argparse
import datetime
import fcntl
import hashlib
import http.client
import json
import os
import pathlib
import pty
import select
import shlex
import signal
import socket
import struct
import subprocess
import termios
import time
import urllib.parse

from agy_live import write

P = pathlib.Path


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=15)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


class Console:
    def __init__(self, args):
        self.a = args
        self.out = P(args.evidence).resolve()
        self.raw = P(args.raw).resolve()
        self.out.mkdir(parents=True, exist_ok=False)
        self.raw.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.env = dict(os.environ, TERM='xterm-256color', OPENAGENTX_SOCKET_PATH=args.socket,
                        OPENAGENTX_CREDENTIALS_PATH=args.credentials)
        self.env.pop('TMUX', None)
        self.env.pop('TMUX_PANE', None)
        self.socket_name = 'oax-installed-console-' + str(os.getpid())
        self.target = '=OAX:=' + args.agent + '.0'
        self.seq = 0
        self.inputs = []
        self.token = ''
        self.pid = None
        self.fd = None
        self.started_tmux = False
        self.transcript = bytearray()
        self.api_root = '/api/console/v1/agents/' + urllib.parse.quote(args.agent) + '/tasks'

    def get(self, path, auth=True):
        conn = UnixHTTP(self.a.socket)
        headers = {'Authorization': 'Bearer ' + self.token} if auth else {}
        conn.request('GET', path, headers=headers)
        response = conn.getresponse()
        data = json.loads(response.read())
        conn.close()
        self.seq += 1
        write(self.out / 'http' / f'{self.seq:04}.json', {'at': time.time(), 'method': 'GET',
              'path': path, 'status': response.status, 'response': data})
        if response.status != 200:
            raise RuntimeError(f'GET {path}: HTTP {response.status}')
        return data

    def authenticate(self):
        probe = self.get('/api/auth/v1/cli/installation', auth=False)
        stored = json.loads(P(self.a.credentials).read_text())
        current = stored['current'][self.a.socket]
        assert current['installation_id'] == probe['installation_id'], 'credential installation mismatch'
        credential = next(c for c in stored['credentials'] if c['socket_path'] == self.a.socket
                          and c['installation_id'] == probe['installation_id']
                          and c['username'] == current['username'])
        self.token = credential['token']
        self.get('/api/auth/v1/cli/session')

    def service_processes(self, label):
        expected = hashlib.sha256(P(self.a.binary).read_bytes()).hexdigest()
        records = []
        for unit in ('openagentx.service', 'openagentx-worker@' + self.a.agent + '.service'):
            result = subprocess.run(['systemctl', '--user', 'show', unit,
                '--property=MainPID,ActiveState,SubState,FragmentPath,ExecStart'],
                capture_output=True, text=True, timeout=15, check=True)
            props = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
            pid = int(props['MainPID'])
            assert props['ActiveState'] == 'active' and pid > 0, unit + ' is not active'
            proc = P('/proc', str(pid))
            actual = hashlib.sha256((proc / 'exe').read_bytes()).hexdigest()
            records.append({'unit': unit, 'properties': props, 'pid': pid,
                'starttime': (proc / 'stat').read_text().split()[21],
                'exe': os.readlink(proc / 'exe'), 'binary_sha256': actual})
            assert actual == expected, unit + ' is not running the installed candidate'
        write(self.out / (label + '-service-processes.json'), records)
        return records

    def tmux(self, *args, check=True):
        run = subprocess.run(['tmux', '-L', self.socket_name, *args], env=self.env,
                             capture_output=True, text=True, timeout=15)
        if check and run.returncode:
            raise RuntimeError('tmux ' + args[0] + ': ' + run.stderr)
        return run.stdout

    def pump(self, seconds=.1):
        if self.fd is None:
            time.sleep(seconds)
            return
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            ready, _, _ = select.select([self.fd], [], [], min(.1, max(0, end - time.monotonic())))
            if ready:
                try:
                    data = os.read(self.fd, 65536)
                except OSError:
                    return
                if not data:
                    return
                self.transcript.extend(data)
                with (self.raw / 'console.pty.raw').open('ab') as output:
                    output.write(data)

    def wait(self, fn, seconds=30):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            value = fn()
            if value:
                return value
            self.pump(1)
        raise TimeoutError('installed Console checkpoint timed out')

    def screen(self, label=None):
        self.pump(.1)
        value = self.tmux('capture-pane', '-p', '-t', self.target)
        if label:
            (self.out / (label + '.screen.txt')).write_text(value)
        return value

    def send(self, text, label, enter=False):
        self.inputs.append({'at': time.time(), 'label': label, 'text': text, 'enter': enter})
        write(self.out / 'pty-inputs.json', self.inputs)
        for byte in text.encode():
            os.write(self.fd, bytes([byte]))
            self.pump(.012)
        if enter:
            self.pump(.15)
            os.write(self.fd, b'\r')
        self.pump(.3)

    def command(self, text):
        self.wait(lambda: 'connection connected' in self.screen() and '> /help' in self.screen(), 30)
        self.send(text, 'Console command', enter=True)

    def start(self):
        self.tmux('new-session', '-d', '-s', 'OAX', '-n', self.a.agent,
                  '-x', '80', '-y', '24', 'sh')
        self.started_tmux = True
        self.tmux('set-option', '-w', '-t', self.target, 'remain-on-exit', 'on')
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe('tmux', ['tmux', '-L', self.socket_name, 'attach-session', '-t', 'OAX'], self.env)
        self.pid, self.fd = pid, fd
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 80, 0, 0))
        self.pump(1)
        self.console_argv = [self.a.binary, 'console', 'attach', '--agent', self.a.agent]
        self.send('exec ' + shlex.join(self.console_argv), 'launch installed Console', enter=True)
        self.wait(lambda: 'connection connected' in self.screen(), 30)
        self.screen('01-attached-80x24')

    def tasks(self):
        page = self.get(self.api_root + '?limit=100')
        assert not page.get('has_more'), 'bounded smoke expects fewer than 100 agent tasks'
        return page.get('tasks', [])

    def snapshot(self, tid):
        return self.get(self.api_root + '/' + tid)

    def new_task(self, previous):
        def found():
            new = [t for t in self.tasks() if t['task_id'] not in previous]
            assert len(new) <= 1, 'duplicate or concurrent task: Agent exclusivity violated'
            return new[0]['task_id'] if new else None
        return self.wait(found)

    def finished(self, tid, label):
        def done():
            snapshot = self.snapshot(tid)
            return snapshot if snapshot['task']['status'] in ('succeeded', 'failed', 'uncertain', 'canceled') else None
        result = self.wait(done, self.a.timeout)
        write(self.out / (label + '.json'), result)
        assert result['task']['status'] == 'succeeded', label + ': Task did not succeed'
        assert result['task']['completion_basis'] == 'query_result_delivered'
        assert result['latest_run']['turn_result']['final_reply'] is True
        return result

    def reveal(self, expected, label):
        self.send('\x1b[F', 'timeline End')
        screens = []
        for index in range(12):
            value = self.screen(label + '-' + str(index))
            screens.append(value)
            merged = ''.join(''.join(screens).split())
            if ''.join(expected.split()) in merged:
                self.send('\x1b[F', 'restore timeline End')
                return
            self.send('\x1b[5~', 'timeline PageUp')
        raise AssertionError(label + ': complete reply not visible in Console timeline')

    def review(self, tid, decision, command, label):
        self.command(command)
        def reviewed():
            s = self.snapshot(tid)
            return s if s['task'].get('review', {}).get('decision') == decision else None
        snapshot = self.wait(reviewed)
        write(self.out / (label + '.json'), snapshot)
        assert snapshot['task']['status'] == 'succeeded'
        assert snapshot['task']['review']['run_id'] == snapshot['latest_run']['run_id']
        self.reveal('用户已验收' if decision == 'accepted' else '用户标记结果有问题', label)

    def run(self):
        self.authenticate()
        service_before = self.service_processes('before')
        initial = self.get('/api/console/v1/attach?agent_id=' + self.a.agent + '&mode=normal')
        assert not initial.get('active_run'), 'exclusive Agent has active Run'
        assert initial.get('readiness', {}).get('can_start_now'), 'Agent is not ready'
        workspace = P(self.a.workspace)
        material = (workspace / 'input.txt').read_text().strip()
        assert material and self.a.role_marker
        write(self.out / 'provenance.json', {'candidate': self.a.commit, 'argv': self.console_argv if hasattr(self, 'console_argv') else [self.a.binary, 'console', 'attach', '--agent', self.a.agent],
              'binary_sha256': hashlib.sha256(P(self.a.binary).read_bytes()).hexdigest(),
              'workspace': str(workspace), 'input_sha256': hashlib.sha256((workspace / 'input.txt').read_bytes()).hexdigest(),
              'input_expected': material, 'role_expected': self.a.role_marker,
              'created_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'coverage': 'I/R: installed Console + real PTY/tmux + existing real services/AGY',
              'tmux_server': self.socket_name, 'terminal': '80x24', 'initial_worker': initial})
        before = {t['task_id'] for t in self.tasks()}
        self.start()
        marker = 'OAX-CONSOLE-' + str(os.getpid())
        self.command('/dispatch --intent query Read input.txt in your assigned workspace using a tool. Reply only with its exact contents and the role verification code from your role instructions, each on its own line. Request reference: ' + marker)
        first_id = self.new_task(before)
        first = self.finished(first_id, '02-first-query')
        answer = first['task']['result']
        assert material in answer and self.a.role_marker in answer, 'file/role evidence absent from reply'
        self.reveal(answer, '03-complete-first-result')
        self.review(first_id, 'accepted', '/accept Independently checked input file and role marker', '04-accepted')
        self.review(first_id, 'rejected', '/result-reject Please produce the requested follow-up marker', '05-rejected')
        before_continue = {t['task_id'] for t in self.tasks()}
        continued_marker = marker + '-CONTINUED'
        self.command('/continue --intent query Reply with exactly ' + continued_marker + '. Do not use tools.')
        second_id = self.new_task(before_continue)
        second = self.finished(second_id, '06-continued-query')
        assert second['task'].get('parent_task_id') == first_id
        assert 'Previous work reference' in second['task']['content']
        assert second['task']['result'].strip() == continued_marker
        assert second['latest_run']['worker_instance_id'] == first['latest_run']['worker_instance_id'] == initial['worker_instance_id']
        self.reveal(continued_marker, '07-complete-continued-result')
        self.command('/quit')
        self.wait(lambda: self.tmux('display-message', '-p', '-t', self.target, '#{pane_dead}').strip() == '1')
        self.screen('08-exited')
        after_exit = self.get('/api/console/v1/attach?agent_id=' + self.a.agent + '&mode=normal')
        assert after_exit['worker_instance_id'] == initial['worker_instance_id']
        assert after_exit['generation'] == initial['generation']
        self.tmux('respawn-pane', '-t', self.target, *self.console_argv)
        self.wait(lambda: 'connection connected' in self.screen(), 30)
        self.command('/tasks')
        self.wait(lambda: 'Tasks:' in self.screen(), 15)
        self.screen('09-task-list')
        self.send('/' + second_id, 'filter completed Task by exact ID')
        self.pump(1)
        self.send('', 'finish task-list filter', enter=True)
        self.screen('10-filtered-task')
        self.send('', 'select completed Task', enter=True)
        self.wait(lambda: 'Tasks:' not in self.screen() and '> /help' in self.screen(), 15)
        self.reveal(continued_marker, '11-reopened-result')
        reopened = self.snapshot(second_id)
        write(self.out / '12-reopened-snapshot.json', reopened)
        assert reopened['task'] == second['task'], 'reopen changed persisted Task'
        assert {t['task_id'] for t in self.tasks()} == before_continue | {second_id}, 'reopen dispatched extra Task'
        self.command('/quit')
        self.wait(lambda: self.tmux('display-message', '-p', '-t', self.target, '#{pane_dead}').strip() == '1')
        service_after = self.service_processes('after')
        assert [(x['pid'], x['starttime']) for x in service_before] == [(x['pid'], x['starttime']) for x in service_after], 'service processes changed during Console smoke'
        return {'first_task': first_id, 'continued_task': second_id, 'worker_instance_id': initial['worker_instance_id']}

    def collect(self):
        if self.started_tmux:
            self.pump(.2)
            self.tmux('kill-server', check=False)
        if self.fd is not None:
            self.pump(.1)
            os.close(self.fd)
            self.fd = None
        if self.pid is not None:
            waited, _ = os.waitpid(self.pid, os.WNOHANG)
            if not waited:
                os.kill(self.pid, signal.SIGTERM)
                os.waitpid(self.pid, 0)
        body = bytes(self.transcript)
        if self.token:
            assert self.token.encode() not in body, 'credential appeared in PTY output'
        (self.out / 'console.pty.log').write_bytes(body)
        for file in self.out.rglob('*'):
            if file.is_file() and self.token:
                assert self.token.encode() not in file.read_bytes(), 'credential appeared in evidence'
        write(self.out / 'cleanup.json', {'only_private_tmux_server_stopped': self.socket_name,
              'agent_services_untouched': True, 'raw_directory': str(self.raw)})
        (self.out / 'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + str(p.relative_to(self.out)) + '\n' for p in sorted(self.out.rglob('*')) if p.is_file() and p.name != 'SHA256SUMS'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', default=str(P.home() / '.local/bin/openagentx'))
    parser.add_argument('--socket', default=str(P.home() / '.openagentx/run/openagentx.sock'))
    parser.add_argument('--credentials', default=str(P.home() / '.openagentx/credentials.json'))
    parser.add_argument('--agent', default='agy-onboarding-e2e')
    for key in ('evidence', 'raw', 'workspace', 'role-marker', 'commit'):
        parser.add_argument('--' + key, required=True)
    parser.add_argument('--timeout', type=int, default=240)
    args = parser.parse_args()
    os.umask(0o077)
    console = Console(args)
    try:
        completed = console.run()
        write(console.out / 'verdict.json', {'status': 'PASS', 'coverage': 'I/R', **completed})
        print(json.dumps({'status': 'PASS', **completed}), flush=True)
    except Exception as exc:
        write(console.out / 'verdict.json', {'status': 'FAIL', 'error': str(exc).replace(console.token, '[REDACTED]') if console.token else str(exc)})
        raise
    finally:
        console.collect()


if __name__ == '__main__':
    main()
