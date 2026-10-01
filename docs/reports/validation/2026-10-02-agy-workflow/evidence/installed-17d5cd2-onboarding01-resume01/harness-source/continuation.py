"""Resume after preserved strict role-output assertion; never redispatch A or B."""
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
try:
 login=r.api('/api/auth/v1/login',{'username':'owner','password':r.password},auth=True);r.csrf=login['csrf_token']
 old=json.loads((prior/'cases/role-freeze/before-update.json').read_text());tid=old['task']['id'];oldhash=old['run_attempts'][0]['instructions_sha256'];newhash=hashlib.sha256(r.role.read_bytes()).hexdigest()
 current=r.detail(tid);write(r.out/'cases/role-freeze/current-task.json',current)
 assert current['task']['status']=='succeeded' and current['task']['result'].strip().splitlines()[-1]==r.role_marker and r.new_marker not in current['task']['result'],current
 assert len(current['run_attempts'])==1 and r.latest_run(current)['instructions_sha256']==oldhash
 calls=(r.root/'workspace/role-freeze.invocations.jsonl').read_text().splitlines();assert len(calls)==1
 tid2=json.loads((prior/'cases/pause-queued/before-pause.json').read_text())['task']['id']
 paused=r.state('08-paused');assert paused['status']=='offline',paused
 queued=r.detail(tid2);write(r.out/'cases/pause-queued/while-paused.json',queued);assert not queued['run_attempts'],queued
 write(r.out/'cases/pause-queued/mailbox-while-paused.json',r.api('/api/observe/v1/mailboxes?agent_id='+r.aid))
 r.cli('09-resume',['agent','resume',r.aid,'--no-open','--wait','5m'])
 resumed=r.state('10-resumed');assert resumed['generation']>paused['generation'],resumed
 fresh=r.settle(tid2,'role-next-run');assert fresh['task']['status']=='succeeded' and fresh['task']['result'].strip()==r.new_marker,fresh
 assert len(fresh['run_attempts'])==1 and r.latest_run(fresh)['instructions_sha256']==newhash and r.latest_run(fresh)['worker_instance_id']==resumed['worker_instance_id'],fresh
 write(r.out/'cases/pause-queued/after-resume.json',fresh)
 final=r.detail(tid);write(r.out/'cases/role-freeze/final-after-resume.json',final);assert len(final['run_attempts'])==1
 write(r.out/'role-freeze-proof.json',{'old_sha256':oldhash,'new_sha256':newhash,'current_task':tid,'next_task':tid2,'invocations':len(calls),'old_role':r.role_marker,'new_role':r.new_marker,'paused_worker':paused,'resumed_worker':resumed,'role_assertion':'Role SHA256 unchanged; final response line is old marker and new marker absent; extra progress sentence retained.'})
 r.cli('11-final-status',['agent','status',r.aid])
 crash=r.systemd_crash()
 write(r.out/'verdict.json',{'status':'PASS','entry_initial_evidence':str(prior),'role_freeze_queue_pause_resume':'PASS','systemd_crash':crash,'candidate':r.a.commit})
except Exception as e:
 write(r.out/'verdict.json',{'status':'FAIL','error':str(e).replace(r.password,'[REDACTED]')});raise
finally:r.collect_installed()
