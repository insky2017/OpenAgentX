#!/usr/bin/env python3
"""Read existing isolated live instance through authenticated official Observe API.
No Task writes, network configuration, SQL writes, or model calls.
"""
import argparse,hashlib,http.cookiejar,json,pathlib,secrets,urllib.request
from agy_live import Live,write,redact
P=pathlib.Path
p=argparse.ArgumentParser();p.add_argument('--profile',required=True);p.add_argument('--evidence',required=True);p.add_argument('--task',required=True);p.add_argument('--expected',required=True);a=p.parse_args()
live=Live.__new__(Live);live.root=P(a.profile);live.out=P(a.evidence);live.out.mkdir(parents=True,exist_ok=False)
manifest=json.loads((live.root/'handoff.json').read_text());live.url=manifest['url'];live.password=(live.root/'password').read_text();live.csrf='';live.request_number=100000+int(secrets.token_hex(3),16);live.jar=http.cookiejar.CookieJar();live.http=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(live.jar));live.procs=json.loads((live.root/'processes.json').read_text())
login=live.api('/api/auth/v1/login',{'username':'owner','password':live.password},auth=True);live.csrf=login['csrf_token']
task=live.api('/api/observe/v1/tasks/'+a.task);write(live.out/'task-run-journal.json',task)
assert task['task']['status']=='succeeded',task['task']
assert task['task']['completion_basis']=='query_result_delivered',task['task']
assert task['task']['result'].strip()==a.expected,task['task']
for run in task['run_attempts']:write(live.out/('run-'+run['run_id']+'.json'),live.api('/api/observe/v1/run-attempts/'+run['run_id']))
write(live.out/'overview.json',live.api('/api/observe/v1/overview'));live.collect()
write(live.out/'verdict.json',{'status':'PASS','task_id':a.task,'expected':a.expected,'evidence_scope':'authenticated Observe cross-check of task created by actual CUA UI action','browser_tool':'mcp__cua_repl','source_commit':manifest['source_commit']})
(live.out/'SHA256SUMS').write_text('\n'.join(hashlib.sha256(f.read_bytes()).hexdigest()+'  '+str(f.relative_to(live.out)) for f in sorted(live.out.rglob('*')) if f.is_file() and f.name!='SHA256SUMS')+'\n')
print(json.dumps({'status':'PASS','task':a.task,'evidence':str(live.out)}))
