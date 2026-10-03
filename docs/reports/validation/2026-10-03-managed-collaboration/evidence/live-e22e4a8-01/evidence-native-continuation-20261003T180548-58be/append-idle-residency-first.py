#!/usr/bin/env python3
"""Append read-only process and heartbeat evidence after the existing idle window."""
import datetime
import hashlib
import http.cookiejar
import json
from pathlib import Path
import urllib.request

root = Path(__file__).resolve().parent
out = root / 'evidence-native-continuation-20261003T180548-58be'
result = json.loads((out / 'idle-result.json').read_text())
assert result['status'] == 'PASS' and result['elapsed_seconds'] >= 1800
assert json.loads((out / 'verdict-native-continuation.json').read_text())['status'] == 'PASS'
state = json.loads((root / 'handoff.json').read_text())
mid = json.loads((out / 'idle-residency-mid.json').read_text())
records = []
for entry in mid['processes']:
    proc = Path('/proc') / str(entry['pid'])
    fields = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
    assert fields[19] == entry['starttime'] and fields[0] not in {'Z', 'X'}
    executable_sha = hashlib.sha256((proc / 'exe').read_bytes()).hexdigest()
    assert executable_sha == state['binary_sha256']
    records.append({**entry, 'state': fields[0], 'exe_sha256': executable_sha})
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))
req = urllib.request.Request(state['url'] + '/api/auth/v1/login', data=json.dumps({'username':'owner', 'password':(root / 'password').read_text()}).encode(), headers={'Content-Type':'application/json'})
with opener.open(req, timeout=15) as response:
    assert response.status == 200
    response.read()
with opener.open(state['url'] + '/api/observe/v1/overview', timeout=15) as response:
    overview = json.load(response)
current = datetime.datetime.now(datetime.timezone.utc)
workers = []
for agent in state['agents']:
    candidates = [w for w in overview['workers'] if w['agent_id'] == agent]
    assert len(candidates) == 1
    w = candidates[0]
    before = next(x for x in mid['workers'] if x['agent_id'] == agent)
    assert w['worker_instance_id'] == before['worker_instance_id'] and w['generation'] == before['generation'] and w['status'] == 'online'
    age = (current - datetime.datetime.fromisoformat(w['last_heartbeat_at'].replace('Z','+00:00'))).total_seconds()
    assert 0 <= age < 10
    workers.append({k:w[k] for k in ('agent_id','worker_instance_id','generation','status','last_heartbeat_at')})
record = {'status':'PASS','at':current.isoformat(),'processes':records,'workers':workers,'scope':'After completed 1800-second idle; exact process starttimes, binary SHA, Worker identity and fresh online heartbeats match mid-window evidence.'}
(out / 'idle-residency-end.json').write_text(json.dumps(record, indent=2) + '\n')
(out / 'append-idle-residency.py').write_bytes(Path(__file__).read_bytes())
(out / 'SHA256SUMS').write_text('\n'.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(out)) for p in sorted(out.rglob('*')) if p.is_file() and p.name != 'SHA256SUMS')+'\n')
print(json.dumps({'status':'PASS','processes':len(records),'workers':len(workers),'at':current.isoformat()}))
