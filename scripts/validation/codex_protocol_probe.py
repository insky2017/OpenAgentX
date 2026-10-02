#!/usr/bin/env python3
"""Isolated real Codex protocol probe; never connects to the user's daemon."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import signal
import socket
import subprocess
import threading
import time
import traceback
from websockets.sync.client import unix_connect
import pexpect


class Tui:
    def __init__(self, root, tid):
        self.root = root
        self.output = ''
        self.log = (root / 'tui.raw.log').open('a', buffering=1)
        self.actions = (root / 'tui-actions.jsonl').open('a', buffering=1)
        self.process = pexpect.spawn('codex', ['resume', '--remote', 'unix://' + str(root / 'server.sock'),
            '--no-alt-screen', tid], cwd=str(root / 'workspace'),
            env=dict(os.environ, CODEX_HOME=str(root / 'home'), TERM='xterm-256color'),
            encoding='utf-8', codec_errors='replace', dimensions=(40, 140))
        self.thread = threading.Thread(target=self.pump, daemon=True)
        self.thread.start()

    def pump(self):
        while self.process.isalive():
            try:
                data = self.process.read_nonblocking(65536, timeout=1)
                self.output += data
                self.log.write(data)
                if '\x1b[6n' in data:
                    self.process.send('\x1b[1;1R')
            except pexpect.TIMEOUT: pass
            except pexpect.EOF: break

    def send(self, text):
        self.actions.write(json.dumps({'at': time.time(), 'input': text}) + '\n')
        self.process.send(text)

    def wait(self, text, timeout=30, after=0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if text in self.output[after:]: return True
            if not self.process.isalive(): break
            time.sleep(.2)
        raise TimeoutError('TUI text missing: ' + text + '; tail=' + repr(self.output[-2000:]))

    def close(self):
        if self.process.isalive():
            self.process.terminate(force=True)
        self.thread.join(3)


def scenarios(root):
    os.umask(0o077)
    tid = (root / 'ready').read_text()
    result = {'thread_id': tid, 'started': time.time()}
    tui = None
    clients = []
    try:
        tui = Tui(root, tid)
        tui.wait('OAX_PROTOCOL_QUERY_OK', timeout=45)
        result['tui_initial_history'] = True
        print('TUI attached and displays initial RPC query', flush=True)
        b = Client(root, root / 'server.sock', 'b'); clients.append(b)
        b.request('thread/resume', {'threadId': tid})
        c = Client(root, root / 'server.sock', 'c'); clients.append(c)
        c.request('thread/resume', {'threadId': tid})
        turn = b.request('turn/start', {'threadId': tid, 'input': [{'type': 'text', 'text':
            'Create a file protocol-artifact.txt in the current workspace containing exactly OAX_PROTOCOL_FILE_OK followed by newline. Use a tool to create it. Then reply exactly OAX_EXTERNAL_TURN_OK.'}],
            'effort': 'high', 'clientUserMessageId': 'oax-file-2'})
        turnid = turn['turn']['id']
        done = b.wait(lambda m: m.get('method') == 'turn/completed' and m['params']['turn']['id'] == turnid, timeout=240)
        broadcast = c.wait(lambda m: m.get('method') == 'turn/completed' and m['params']['turn']['id'] == turnid, timeout=10)
        tui.wait('OAX_EXTERNAL_TURN_OK', timeout=15)
        artifact = (root / 'workspace/protocol-artifact.txt').read_bytes()
        assert artifact == b'OAX_PROTOCOL_FILE_OK\n', repr(artifact)
        result['external_turn'] = done
        result['two_client_broadcast'] = broadcast
        result['tui_external_output'] = True
        result['artifact_sha256'] = hashlib.sha256(artifact).hexdigest()
        (root / 'scenario-checkpoint.json').write_text(json.dumps(result, indent=2))
        print('External turn and file verified; native TUI displays output; both RPC clients receive same completion', flush=True)
        start = len(b.events)
        time.sleep(2)
        tui.send('\x1b[200~Do not call tools. Reply exactly OAX_NATIVE_INPUT_OK.\x1b[201~')
        time.sleep(1)
        tui.send('\r')
        native = b.wait(lambda m: m.get('method') == 'turn/completed', timeout=240, after=start)
        tui.wait('OAX_NATIVE_INPUT_OK', timeout=10)
        result['native_input_turn'] = native
        print('Native TUI input after idle completed and RPC client received event', flush=True)
        start = len(b.events)
        longturn = b.request('turn/start', {'threadId': tid, 'input': [{'type': 'text', 'text':
            'Use the shell tool to run: sleep 15; printf "OAX_AFTER_DETACH_OK\\n" > after-detach.txt . After the command completes reply exactly OAX_DETACHED_TURN_OK.'}],
            'clientUserMessageId': 'oax-detach-4', 'effort': 'high'})
        tui.close()
        result['tui_detached_at'] = time.time()
        detached = b.wait(lambda m: m.get('method') == 'turn/completed' and m['params']['turn']['id'] == longturn['turn']['id'], timeout=240, after=start)
        assert (root / 'workspace/after-detach.txt').read_text() == 'OAX_AFTER_DETACH_OK\n'
        result['detached_turn'] = detached
        print('TUI disconnect did not stop backend turn; detached artifact verified', flush=True)
    except Exception:
        result['error'] = traceback.format_exc()
        print(result['error'], flush=True)
    finally:
        if tui: tui.close()
        for client in clients:
            try: client.close()
            except Exception: pass
        (root / 'scenarios.json').write_text(json.dumps(result, indent=2))


class Client:
    def __init__(self, root, sockpath, name):
        self.name = name
        self.log = (root / (name + '.jsonl')).open('a', buffering=1)
        self.socket = unix_connect(str(sockpath), uri='ws://localhost', max_size=32 * 1024 * 1024)
        self.events = []
        self.cv = threading.Condition()
        self.counter = 0
        self.closed = False
        self.thread = threading.Thread(target=self.read, daemon=True)
        self.thread.start()
        self.request('initialize', {'clientInfo': {'name': 'oax_probe_' + name, 'version': '1'},
                                   'capabilities': {'experimentalApi': True}})
        self.send({'method': 'initialized', 'params': {}})

    def send(self, message):
        self.log.write(json.dumps({'at': time.time(), 'direction': 'send', 'message': message}) + '\n')
        self.socket.send(json.dumps(message))

    def read(self):
        try:
            for line in self.socket:
                message = json.loads(line)
                with self.cv:
                    self.log.write(json.dumps({'at': time.time(), 'direction': 'recv', 'message': message}) + '\n')
                    self.events.append(message)
                    self.cv.notify_all()
        except Exception as exc:
            self.log.write(json.dumps({'reader_error': str(exc)}) + '\n')
        finally:
            with self.cv:
                self.closed = True
                self.cv.notify_all()

    def wait(self, predicate, timeout=180, after=0):
        deadline = time.monotonic() + timeout
        with self.cv:
            while True:
                for m in self.events[after:]:
                    if predicate(m):
                        return m
                if self.closed:
                    raise RuntimeError(self.name + ' connection closed')
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError(self.name + ' wait timeout')
                self.cv.wait(remaining)

    def request(self, method, params, timeout=30):
        self.counter += 1
        rid = self.name + '-' + str(self.counter)
        self.send({'id': rid, 'method': method, 'params': params})
        response = self.wait(lambda m: m.get('id') == rid and 'method' not in m, timeout)
        if 'error' in response:
            raise RuntimeError(json.dumps(response))
        return response['result']

    def close(self):
        self.socket.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--scenarios', action='store_true')
    args = parser.parse_args()
    if args.scenarios:
        scenarios(args.root.resolve())
        return
    os.umask(0o077)
    root = args.root.resolve()
    env = dict(os.environ, CODEX_HOME=str(root / 'home'), TERM='xterm-256color')
    sockpath = root / 'server.sock'
    endpoint = 'unix://' + str(sockpath)
    server_log = (root / 'app-server.log').open('ab', buffering=0)
    command = ['codex', 'app-server', '--listen', endpoint]
    (root / 'launch.json').write_text(json.dumps({'argv': command, 'cwd': str(root / 'workspace'),
                                                'CODEX_HOME': str(root / 'home')}))
    server = subprocess.Popen(command, cwd=root / 'workspace', env=env, stdout=server_log,
                              stderr=server_log, start_new_session=True)
    (root / 'server.pid').write_text(str(server.pid))
    clients = []
    result = {'started': time.time(), 'server_pid': server.pid}
    try:
        deadline = time.monotonic() + 15
        while not sockpath.exists():
            if server.poll() is not None or time.monotonic() > deadline:
                raise RuntimeError('isolated server socket unavailable')
            time.sleep(.1)
        a = Client(root, sockpath, 'a'); clients.append(a)
        thread = a.request('thread/start', {'cwd': str(root / 'workspace'), 'model': 'gpt-6-astra',
            'modelProvider': 'oneaxe', 'approvalPolicy': 'never', 'sandbox': 'danger-full-access',
            'developerInstructions': 'You are an isolated OpenAgentX protocol verification agent. Only access the supplied workspace. Do not inspect credentials or other projects. Follow precise file and output requests; do not delegate.'})
        tid = thread['thread']['id']; result['thread_id'] = tid
        (root / 'thread.json').write_text(json.dumps(thread, indent=2))
        print(json.dumps({'stage': 'thread-started', 'thread_id': tid, 'endpoint': endpoint}), flush=True)
        turn = a.request('turn/start', {'threadId': tid, 'input': [{'type': 'text', 'text':
            'Do not call tools. Reply exactly OAX_PROTOCOL_QUERY_OK.'}], 'effort': 'high',
            'clientUserMessageId': 'oax-query-1'})
        turnid = turn['turn']['id']
        terminal = a.wait(lambda m: m.get('method') == 'turn/completed' and m['params']['turn']['id'] == turnid)
        result['first_turn'] = terminal
        print(json.dumps({'stage': 'query-completed', 'turn': terminal}), flush=True)
        (root / 'checkpoint.json').write_text(json.dumps(result, indent=2))
        # Leave enough time for incremental orchestration without touching other daemons.
        (root / 'ready').write_text(tid)
        while not (root / 'finish').exists():
            time.sleep(1)
    except Exception:
        result['error'] = traceback.format_exc()
        print(result['error'], flush=True)
    finally:
        (root / 'result.json').write_text(json.dumps(result, indent=2))
        for client in clients:
            try: client.close()
            except OSError: pass
        if server.poll() is None:
            server.terminate()
            try: server.wait(10)
            except subprocess.TimeoutExpired:
                server.kill(); server.wait(5)
        result['server_exit'] = server.returncode
        (root / 'result.json').write_text(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
