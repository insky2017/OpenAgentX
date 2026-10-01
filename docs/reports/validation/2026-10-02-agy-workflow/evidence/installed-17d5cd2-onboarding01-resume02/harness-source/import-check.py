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
try:
 login=r.api('/api/auth/v1/login',{'username':'owner','password':r.password},auth=True);r.csrf=login['csrf_token']
 before=r.api('/api/observe/v1/overview');tasks=[t for t in before['tasks'] if t['target_agent_id']==r.aid]
 snapshots={t['id']:r.detail(t['id']) for t in tasks}
 worker=max((w for w in before['workers'] if w['agent_id']==r.aid),key=lambda w:w['generation'])
 identity='/home/sky/.openagentx/workers/identities/agy-onboarding-e2e.yaml';config='/home/sky/.openagentx/workers/agy-onboarding-e2e.yaml'
 hashes={p:hashlib.sha256(P(p).read_bytes()).hexdigest() for p in [identity,config]}
 write(r.out/'canonical-import-before.json',{'overview':before,'tasks':snapshots,'hashes':hashes})
 r.cli('15-canonical-import',['agent','add','--identity',identity,'--worker-config',config,'--no-open','--password-file',r.a.password_file])
 after=r.api('/api/observe/v1/overview');newtasks=[t for t in after['tasks'] if t['target_agent_id']==r.aid];newworker=max((w for w in after['workers'] if w['agent_id']==r.aid),key=lambda w:w['generation'])
 final={t['id']:r.detail(t['id']) for t in newtasks};newhashes={p:hashlib.sha256(P(p).read_bytes()).hexdigest() for p in [identity,config]}
 write(r.out/'canonical-import-after.json',{'overview':after,'tasks':final,'hashes':newhashes})
 assert newworker['worker_instance_id']==worker['worker_instance_id'] and newworker['generation']==worker['generation']
 assert set(final)==set(snapshots) and hashes==newhashes
 for tid in snapshots:
  assert final[tid]['task']['status']==snapshots[tid]['task']['status'] and final[tid]['task']['version']==snapshots[tid]['task']['version']
  assert {x['run_id'] for x in final[tid]['run_attempts']}=={x['run_id'] for x in snapshots[tid]['run_attempts']}
 write(r.out/'canonical-import-verdict.json',{'status':'PASS','worker_instance_id':newworker['worker_instance_id'],'generation':newworker['generation'],'task_count':len(final),'run_count':sum(len(d['run_attempts']) for d in final.values()),'identity_worker_bytes_unchanged':True,'task_run_sets_unchanged':True})
finally:r.collect_installed()
