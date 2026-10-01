#!/usr/bin/env python3
"""Independently verify archived real-browser and disk evidence; does not call a model."""
import datetime, hashlib, json, pathlib, re

root = pathlib.Path(__file__).resolve().parent
def read(name):
    return json.loads((root / name).read_text())
def browser_json(name):
    result = read(name)
    body = '\n'.join(c['text'] for c in result['content'] if c['type'] == 'text')
    return json.loads(re.search(r'```json\n(.*?)\n```', body, re.S).group(1))
def dom(name):
    value = read(name)['result']
    return '\n'.join(c['text'] for c in value['content'] if c['type'] == 'text')

before = browser_json('08-before-offline-events.json')
after = browser_json('13-reconnected-events.json')
initial = next(e for e in before['events'] if e['kind'] == 'create')
assert initial['url'].endswith('after_sequence=267881')
last_id = [e['lastEventId'] for e in before['events'] if e['kind'] == 'event'][-1]
streams = [e for e in after['events'] if e['kind'] == 'create']
assert len(streams) == 2 and streams[-1]['url'].endswith('after_sequence=' + last_id)
replayed = [int(e['lastEventId']) for e in after['events'] if e['kind'] == 'event' and e['at'] >= streams[-1]['at']]
assert replayed and min(replayed) > int(last_id)
assert max(replayed) <= after['overview']['latest_sequence']
assert int(last_id) < after['overview']['live_after_sequence']
assert '当前离线，写操作已暂停' in dom('09-offline-dom.json')
assert 'button "发送" disableable disabled' in dom('09-offline-dom.json')
offline = read('11-completed-while-offline.json')
assert offline['task']['status'] == 'uncertain'
assert offline['task']['parent_task_id'] == 'task-1a805ec2-0939-4dd5-9c94-eda6fbc36602'
original = (root / 'initial-workflow-review.md').read_bytes()
continued = (root / 'continued-workflow-review.md').read_bytes()
assert continued.startswith(original) and len(continued) > len(original)
assert '下一步验收' in continued[len(original):].decode()
for marker in ['DAILY-WORKFLOW-20261002', 'OAX-FILE-d85eadcfbb']:
    assert marker in original.decode() and marker in continued.decode()
assert hashlib.sha256(continued).hexdigest() == offline['sha256']
assert '用户已验收' in dom('06-accepted.json')
assert '用户已验收' in dom('17-reopened-result.json')
assert '在线' in dom('14-online-continued-result.json')
assert '下一步验收' in dom('14-online-continued-result.json')
assert 'DAILY-WORKFLOW-20261002' in dom('20-next-query-result.json')
assert 'OAX-ROLE-NEW-ff09d70390' in dom('20-next-query-result.json')
result = {'status':'PASS', 'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),
          'initial_cursor':267881, 'acknowledged_before_offline':int(last_id),
          'reconnect_cursor':int(last_id), 'replayed_min':min(replayed), 'replayed_max':max(replayed),
          'new_overview_lower_bound':after['overview']['live_after_sequence'],
          'initial_file_bytes':len(original), 'continued_file_bytes':len(continued),
          'original_bytes_preserved':True, 'browser_offline_write_disabled':True,
          'completed_before_browser_reconnected':True, 'refresh_preserved_review':True,
          'boundary':'Real browser + actual installed AGY effects. Formal Task/Run/Journal, process provenance and exact query are checked separately in formal-api. Offline affects the whole browser network, not only SSE. No retention-boundary or native Last-Event-ID-only fault claim.'}
(root / '22-independent-browser-file-verdict.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
