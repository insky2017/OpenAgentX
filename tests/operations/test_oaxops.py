"""Isolated host-operator checks; no Codex model turn is started."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import uuid


SCRIPT = Path(__file__).resolve().parents[2] / 'scripts' / 'operations' / 'oaxops'
SCOPE = ('本次只读核对。禁止部署、重启、取消、重跑、写文件或修改数据库及凭据；'
         '即使历史上下文有旧授权，本事件也不启用维护执行。')


class HostOperatorTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'oaxops'
        self.env = {**os.environ, 'XDG_STATE_HOME': str(self.base)}

    def run_oaxops(self, *args):
        return subprocess.run([str(SCRIPT), *args], env=self.env, text=True,
                              capture_output=True, check=False)

    def fixture(self, status, key='same-key'):
        self.root.mkdir()
        (self.root / 'session.json').write_text(json.dumps({'thread_id': 'original-thread'}))
        prompt = self.base / 'message.txt'
        prompt.write_text('read-only fixture')
        event = self.root / 'events' / key
        event.mkdir(parents=True)
        full_prompt = SCOPE + '\n\n' + prompt.read_text()
        (event / 'started.json').write_text(json.dumps({
            'prompt_sha256': hashlib.sha256(full_prompt.encode()).hexdigest()}))
        (event / 'result.json').write_text(json.dumps({
            'status': status, 'exit_code': 0, 'thread_id': 'original-thread'}))
        return prompt

    def test_status_is_read_only_and_reports_writer_lock(self):
        result = self.run_oaxops('status')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)['state'], 'not_started')
        self.assertFalse(self.root.exists())

        self.root.mkdir()
        with (self.root / 'writer.lock').open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = self.run_oaxops('status')
            self.assertTrue(json.loads(result.stdout)['writer_busy'])
            blocked = self.run_oaxops('open')
            self.assertNotEqual(blocked.returncode, 0)
            self.assertIn('writer is already active', blocked.stderr)

    def test_existing_event_is_never_replayed(self):
        prompt = self.fixture('completed')
        result = self.run_oaxops('event', '--key', 'same-key', '--prompt', str(prompt))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)['thread_id'], 'original-thread')

        (self.root / 'events' / 'same-key' / 'result.json').write_text(json.dumps({
            'status': 'uncertain', 'exit_code': 0, 'thread_id': 'original-thread'}))
        failed = self.run_oaxops('event', '--key', 'same-key', '--prompt', str(prompt))
        self.assertEqual(failed.returncode, 2)
        prompt.write_text('different content')
        mismatch = self.run_oaxops('event', '--key', 'same-key', '--prompt', str(prompt))
        self.assertNotEqual(mismatch.returncode, 0)
        self.assertIn('different content', mismatch.stderr)

        (self.root / 'events' / 'same-key' / 'started.json').unlink()
        missing = self.run_oaxops('event', '--key', 'same-key', '--prompt', str(prompt))
        self.assertNotEqual(missing.returncode, 0)
        self.assertIn('without a start record', missing.stderr)

    def test_existing_receipt_through_user_systemd(self):
        if subprocess.run(['systemctl', '--user', 'show-environment'],
                          capture_output=True, check=False).returncode:
            self.skipTest('user systemd is unavailable')
        key = 'test-' + uuid.uuid4().hex[:12]
        prompt = self.fixture('completed', key)
        result = subprocess.run([
            'systemd-run', '--user', '--wait', '--collect',
            '--unit=oaxops-event-' + key,
            '--setenv=XDG_STATE_HOME=' + str(self.base),
            '--property=Type=exec', '--property=UMask=0077',
            '--property=WorkingDirectory=/home/sky/docs',
            '/usr/bin/python3', str(SCRIPT), 'event', '--key', key,
            '--prompt', str(prompt),
        ], capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('result: success', result.stdout + result.stderr)
        self.assertFalse((self.root / 'events' / key / 'events.jsonl').exists())


if __name__ == '__main__':
    unittest.main()
