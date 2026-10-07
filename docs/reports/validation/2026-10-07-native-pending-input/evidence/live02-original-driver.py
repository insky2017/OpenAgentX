#!/usr/bin/env python3
"""Real owned Codex TUI/Bridge/Worker input-ack acceptance; no production changes."""
import argparse
import json
import os
from pathlib import Path
import sys
import time
sys.dont_write_bytecode = True
from native_model_settings_e2e import SettingsRun, AttachedClient, require, now, sha

p = argparse.ArgumentParser()
p.add_argument('--binary', type=Path, required=True)
p.add_argument('--root', type=Path, required=True)
p.add_argument('--commit', default='working-candidate')
p.add_argument('--prepared', action='store_true')
a = p.parse_args()
a.phase = 'native'
os.umask(0o077)
r = SettingsRun.restore(a) if a.prepared else SettingsRun(a)
try:
    if not a.prepared:
        r.prepare()
    r.client = AttachedClient(r)
    pane = r.windows['native'] + '.0'
    workspace = r.root / 'workspace'
    statepath = r.root / 'profile/workers/codex' / r.aid / 'state.json'
    def send(text):
        r.tmux('send-keys', '-t', pane, '-l', text)
        time.sleep(.5)
        r.tmux('send-keys', '-t', pane, 'Enter')
    send('Run exactly one local python3 command that writes started.txt containing STARTED, sleeps 45 seconds, then writes base.txt containing BASE_DONE. After that command, follow any new user input. Do not spawn agents or inspect other paths.')
    r.wait(lambda: (workspace / 'started.txt').exists(), 120)
    started = json.loads(statepath.read_text())
    for marker in ['ACK_A', 'ACK_B', 'ACK_C']:
        suffix = ' Then sleep 35 seconds in that same Python command before returning.' if marker == 'ACK_C' else ''
        send('After the current tool returns, execute one local python3 command to append exactly one line ' + marker + ' to receipts.txt.' + suffix + ' Do not repeat any earlier command. This message is a separate one-time append. Use python3, not python.')
        time.sleep(1)
    r.capture(pane, 'pending-before-consumption', False)
    r.wait(lambda: (workspace / 'receipts.txt').exists() and (workspace / 'receipts.txt').read_text().splitlines() == ['ACK_A', 'ACK_B', 'ACK_C'], 180)
    screen = r.capture(pane, 'confirmed-inputs-active', False)
    require('Messages to be submitted after next tool call' not in screen, 'committed inputs remain in TUI pending queue')
    require(json.loads(statepath.read_text())['turn_id'] == started['turn_id'], 'steer unexpectedly created another turn')
    r.tmux('send-keys', '-t', pane, 'Escape')
    r.wait(lambda: json.loads(statepath.read_text())['state'] == 'idle', 90)
    time.sleep(2)
    after = r.capture(pane, 'after-interrupt', False)
    require('Messages to be submitted after next tool call' not in after, 'interruption recovered already committed steers')
    send('Execute one local python3 command that writes exactly FRESH_OK to fresh.txt. Do not append to receipts.txt and do not repeat earlier commands. Reply FRESH_OK.')
    r.wait(lambda: (workspace / 'fresh.txt').exists() and json.loads(statepath.read_text())['state'] == 'idle', 120)
    require((workspace / 'fresh.txt').read_text() == 'FRESH_OK', 'new input proof mismatch')
    require((workspace / 'receipts.txt').read_text().splitlines() == ['ACK_A', 'ACK_B', 'ACK_C'], 'old input repeated after interruption')
    ids = r.task_evidence()
    require(len(ids) == 3, 'unexpected Task count, possible stale input redispatch')
    detail = r.api('/api/observe/v1/tasks/' + started['task_id'])
    require(len(detail.get('messages', [])) == 3, 'expected three distinct steer messages')
    require(len(detail.get('run_attempts', [])) == 1, 'expected a single steered Run')
    r.save('pending-input-result.json', {'at': now(), 'status': 'PASS_REAL_TUI_WORKER', 'binary_sha256': sha(a.binary), 'source': a.commit, 'task_count': len(ids), 'steer_count': len(detail['messages']), 'steered_run_count': len(detail['run_attempts']), 'receipts': (workspace / 'receipts.txt').read_text(), 'fresh': (workspace / 'fresh.txt').read_text(), 'production_touched': False})
    r.capture(pane, 'final-native', False)
    r.client.close()
    r.client = None
    r.cleanup()
    print(json.dumps({'status': 'PASS_REAL_TUI_WORKER', 'evidence': str(r.out)}))
except Exception as error:
    r.save('pending-input-failure.json', {'at': now(), 'error': str(error).replace(r.password, '[REDACTED]')})
    raise
finally:
    if r.client:
        r.client.close()
