"""Controlled continuation of 02, never re-execute its old mutation Task."""
import http.cookiejar, json, os, pathlib, sys, types, urllib.request
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[0]))
from agy_recovery import Recovery, stop_exact
from agy_live import owned_stop, write
P=pathlib.Path
os.umask(0o077)
r=Recovery.__new__(Recovery)
r.root=P('/home/sky/.local/state/openagentx/evidence/recovery-14d7350-02')
r.out=P(__file__).resolve().parents[1]
prior=P(__file__).resolve().parents[2]/'recovery-14d7350-02'
m=json.loads((r.root/'handoff.json').read_text())
r.a=types.SimpleNamespace(binary=m['binary'],web='/tmp/oax-live-14d7350/web/dist',commit=m['source_commit'],worker_timeout='700s')
r.password=(r.root/'password').read_text();r.url=m['url'];r.port=int(r.url.rsplit(':',1)[1]);r.aid=m['agent']
r.socket=P(next(line.split(': ',1)[1] for line in (r.root/'worker.yaml').read_text().splitlines() if line.startswith('unix_socket:')))
r.procs=json.loads((r.root/'processes.json').read_text());r.runtime_refs=[]
r.request_number=max(int(p.stem) for p in (r.root/'http-raw').glob('*.json'))
r.env=dict(os.environ,OPENAGENTX_HOME=str(r.root/'profile'),OPENAGENTX_SOCKET_PATH=str(r.socket))
for k in list(r.env):
 if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_'):r.env.pop(k)
r.env.update(HTTP_PROXY='http://127.0.0.1:7897',HTTPS_PROXY='http://127.0.0.1:7897',http_proxy='http://127.0.0.1:7897',https_proxy='http://127.0.0.1:7897',NO_PROXY='127.0.0.1,localhost',no_proxy='127.0.0.1,localhost',AGY_GRAFT_NATIVE_PROXY='1',OAX_CAPTURE_ROOT=str(r.root/'runtime-raw'))
r.jar=http.cookiejar.CookieJar();r.http=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(r.jar));r.csrf=''
stage='continue-daemon-recovery';completed={}
write(r.out/'continuation-provenance.json',{'prior_evidence':str(prior),'private_profile':str(r.root),'candidate':r.a.commit,'scope':'Continue completed daemon reconciliation using preserved DB, then independent Worker SIGKILL. No old Task re-execution.'})
try:
 r.daemon_restart('continuation')
 workers=r.api('/api/observe/v1/overview')['workers'];previous=max(workers,key=lambda w:w['generation'])
 r.worker_restart('daemon-sigkill',previous)
 query=r.query('after-daemon-sigkill-continuation')
 old=json.loads((prior/'cases/daemon-sigkill/before-task-run-journal.json').read_text());tid=old['task']['id'];final=r.detail(tid)
 write(r.out/'cases/daemon-sigkill/final-task-run-journal.json',final)
 calls=(r.root/'workspace/daemon-sigkill.invocations.jsonl').read_text().splitlines()
 proof={'task_id':tid,'old_run_id':old['run_attempts'][0]['run_id'],'invocations':len(calls),'late_effect_exists':(r.root/'workspace/daemon-sigkill.late.txt').exists(),'new_worker':r.worker,'query_task':query['task']['id']}
 write(r.out/'cases/daemon-sigkill/independent-proof.json',proof)
 assert final['task']['status']=='uncertain' and len(final['run_attempts'])==1 and final['run_attempts'][0]['run_id']==proof['old_run_id'],final
 assert len(calls)==1 and not proof['late_effect_exists'],proof
 completed['daemon']=proof
 stage='worker';completed['worker']=r.recovery_case('worker')
 write(r.out/'verdict.json',{'status':'PASS','candidate':r.a.commit,'completed':completed,'scope':'R E17 daemon evidence spans 02 and continuation; Worker complete here; not installed proof','limitations':['Reconciliation requires daemon restart after lease expiry.','New Worker generation requires formal network test and publish with binding CAS.']})
except Exception as e:
 write(r.out/'verdict.json',{'status':'FAIL','stage':stage,'completed':completed,'error':str(e).replace(r.password,'[REDACTED]')});raise
finally:
 write(r.out/'runtime-cleanup.json',stop_exact(r.runtime_refs));write(r.out/'cleanup.json',owned_stop(r.root));r.collect()
