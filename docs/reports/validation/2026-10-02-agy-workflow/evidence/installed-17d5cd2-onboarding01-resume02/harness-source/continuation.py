"""Continue existing systemd crash after preserved 25s new-worker deadline failure."""
import datetime,http.cookiejar,json,os,pathlib,re,types,urllib.request,hashlib
from agy_installed import Installed
from agy_live import write
P=pathlib.Path;os.umask(0o077)
r=Installed.__new__(Installed);r.root=P('/home/sky/.local/state/openagentx/evidence/installed-17d5cd2-onboarding01');r.out=P(__file__).resolve().parents[1]
prior=r.out.parent/'installed-17d5cd2-onboarding01';m=json.loads((prior/'manifest.json').read_text())
r.a=types.SimpleNamespace(binary=m['binary'],commit=m['candidate'],password_file='/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/installation/owner-password')
r.password=P(r.a.password_file).read_text().strip();r.url=m['url'];r.aid='agy-onboarding-e2e';r.role=r.root/'workspace/ROLE.md'
r.new_marker=re.search(r'OAX-ROLE-NEW-[0-9a-f]+',r.role.read_text()).group()
original=json.loads((prior/'cases/role-and-file/task-run-journal.json').read_text());r.role_marker=original['task']['result'].split('|')[0];r.input_marker=(r.root/'workspace/input.txt').read_text().strip()
r.env=dict(os.environ)
for k in list(r.env):
 if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_') or k.startswith('OPENAGENTX_'):r.env.pop(k)
r.env.update(HTTP_PROXY='http://127.0.0.1:7897',HTTPS_PROXY='http://127.0.0.1:7897',AGY_GRAFT_NATIVE_PROXY='1',NO_PROXY='127.0.0.1,localhost')
r.jar=http.cookiejar.CookieJar();r.http=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(r.jar));r.csrf='';r.request_number=max(int(p.stem) for p in (r.root/'http-raw').glob('*.json'));r.started_at=datetime.datetime.now(datetime.timezone.utc).isoformat();r.procs=[]
write(r.out/'continuation-provenance.json',{'prior':str(prior),'candidate':r.a.commit,'binary_sha256':hashlib.sha256(P(r.a.binary).read_bytes()).hexdigest(),'at':r.started_at,'scope':'No A/B redispatch. Assert unchanged old role digest and final marker, then finish queued B and systemd crash.'})
from agy_recovery import proc,living
import subprocess
crash_prior=prior.parent/'installed-17d5cd2-onboarding01-resume01'
fault=json.loads((crash_prior/'cases/systemd-crash/fault.json').read_text())
try:
 login=r.api('/api/auth/v1/login',{'username':'owner','password':r.password},auth=True);r.csrf=login['csrf_token']
 tid=json.loads((crash_prior/'cases/systemd-crash/before-task-run-journal.json').read_text())['task']['id']
 d=r.detail(tid);write(r.out/'cases/systemd-crash/automatic-reconcile-task-run-journal.json',d)
 assert d['task']['status']=='uncertain' and len(d['run_attempts'])==1 and d['run_attempts'][0]['status']=='uncertain',d
 at=datetime.datetime.fromisoformat(d['task']['updated_at'].replace('Z','+00:00')).timestamp();elapsed=at-fault['at'];assert 0<=elapsed<=75,elapsed
 daemon=proc(int(subprocess.check_output(['systemctl','--user','show','openagentx.service','-p','MainPID','--value'],text=True).strip()))
 assert daemon['pid']==fault['daemon']['pid'] and daemon['starttime']==fault['daemon']['starttime'] and living(daemon),daemon
 assert not living(fault['main']) and not any(living(x) for x in fault['started']['processes'])
 # In the archived method, overview requests occur only after all recorded PIDs stop.
 first=next(json.loads(p.read_text()) for p in sorted((crash_prior/'http').glob('*.json')) if json.loads(p.read_text())['path']=='/api/observe/v1/overview' and datetime.datetime.fromisoformat(json.loads(p.read_text())['at']).timestamp()>=fault['at'])
 stop_bound=datetime.datetime.fromisoformat(first['at']).timestamp()-fault['at'];assert stop_bound<=25,stop_bound
 write(r.out/'cases/systemd-crash/recovery-timing-proof.json',{'fault_at':fault['at'],'physical_stop_upper_bound_seconds':stop_bound,'physical_stop_evidence':'Prior HTTP 0048 overview only called after source recovered() verified all tracked old PIDs stopped; control-flow-derived bound, not separately saved liveness snapshot.','task_updated_at':d['task']['updated_at'],'automatic_reconcile_seconds':elapsed,'daemon_before':fault['daemon'],'daemon_after':daemon,'old_processes_now_alive':False})
 r.cli('13-resume-after-worker-crash',['agent','resume',r.aid,'--no-open','--wait','5m'])
 new=r.state('14-crash-generation-ready');assert new['generation']>2,new
 r.query('after-systemd-crash')
 final=r.detail(tid);write(r.out/'cases/systemd-crash/final-task-run-journal.json',final)
 calls=(r.root/'workspace/systemd-crash.invocations.jsonl').read_text().splitlines();assert len(calls)==1 and len(final['run_attempts'])==1 and final['task']['status']=='uncertain'
 assert not (r.root/'workspace/systemd-crash.late.txt').exists() and living(daemon)
 proof={'status':'PASS','candidate':r.a.commit,'original_crash_evidence':str(crash_prior),'physical_stop_upper_bound_seconds':stop_bound,'automatic_reconcile_seconds':elapsed,'daemon_pid_unchanged':daemon['pid'],'worker_generation':new['generation'],'task_id':tid,'invocations':len(calls),'task_status':final['task']['status'],'installed_entry_role_queue_evidence':[str(prior),str(crash_prior)]}
 write(r.out/'verdict.json',proof)
 write(r.out/'handoff.json',{'agent':r.aid,'workspace':str(r.root/'workspace'),'role_marker':r.new_marker,'input':r.input_marker,'worker':new,'daemon':daemon,'url':r.url})
except Exception as e:
 write(r.out/'verdict.json',{'status':'FAIL','error':str(e).replace(r.password,'[REDACTED]')});raise
finally:r.collect_installed()
