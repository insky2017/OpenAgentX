from pathlib import Path
import importlib.util,argparse,json,time,hashlib,sys
spec=importlib.util.spec_from_file_location('workflow_evidence','scripts/validation/codex_workflow_e2e.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
base=Path('/home/sky/.local/state/openagentx/validation/2026-10-02-codex-workflow');local=base/'local01';h=json.loads((local/'handoff.json').read_text());thread=json.loads((base/'real-native01/provenance.json').read_text())['thread_id'];workspace=Path(h['workspace']);start=workspace/'native-queue-start01.txt';output=workspace/'native-queue-result01.txt'
assert not start.exists() and not output.exists(),'fixture files already exist; no replay'
a=argparse.Namespace(raw=base/'real-native-contention01',evidence=Path('docs/reports/validation/2026-10-02-codex-workflow/evidence/real-native-contention01'),password_file=local/'password',url=h['url'],agent=h['agent'],organization='default',adapter_id='codex-app-server',timeout=600,poll_interval=30,unit=[])
e=m.Evidence(a);v={'status':'FAIL','scope':'real managed-native input competing with formal bus input on explicitly identical thread; no tool cancellation'}
try:
 e.save('driver.json',{'command':[sys.executable,str(Path(__file__).resolve())],'sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'thread_id':thread,'backend_candidate':h['binary_sha256']})
 deadline=time.monotonic()+180
 while not start.exists():
  assert time.monotonic()<deadline,'native start file did not appear'
  time.sleep(1)
 e.api('/api/auth/v1/login',{'username':'owner','password':e.password},login=True)
 state=json.loads((local/'profile/workers/codex/codex-local-e2e/state.json').read_text());native_task=state['task_id'];e.save('native-state-at-bus-dispatch.json',state);assert state['state']=='running',state['state'];assert state['thread_id']==thread
 active=e.api('/api/observe/v1/tasks/'+native_task);e.save('native-before-bus.json',active);assert active['task']['status']=='running',active['task']['status']
 receipt=e.api('/api/control/v1/tasks',{'target_agent_id':h['agent'],'organization_id':'default','dispatch_mode':'direct','intent':'query','runtime_session':{'backend_id':'codex','provider_session_id':thread},'content':'Read native-queue-result01.txt in the assigned workspace. Reply with exactly its contents without trailing newline, then |BUS-AFTER-NATIVE01. Do not change any file.'})
 bus=receipt['task_id'];e.tasks.extend([native_task,bus]);e.save('created-tasks.json',e.tasks)
 queued=e.api('/api/observe/v1/tasks/'+bus);e.save('bus-immediately-after-dispatch.json',queued);assert queued['task']['status']=='queued',queued['task']['status'];assert not queued.get('run_attempts'),'bus ran while native active'
 native=e.settle(native_task);nr=e.check_run(native);busdetail=e.settle(bus);br=e.check_run(busdetail)
 assert busdetail['task']['status']=='succeeded';assert busdetail['task']['result'].strip()=='NATIVE-QUEUE-END01|BUS-AFTER-NATIVE01'
 actual=output.read_bytes();assert actual==b'NATIVE-QUEUE-END01\n';assert start.read_bytes()==b'START\n'
 e.save('independent-file.json',{'path':str(output),'exact':True,'sha256':hashlib.sha256(actual).hexdigest(),'bytes':len(actual)})
 assert nr['worker_instance_id']==br['worker_instance_id']
 v.update(status='PASS',native_task=native_task,bus_task=bus,native_run=nr['run_id'],bus_run=br['run_id'],native_task_status=native['task']['status'],bus_task_status=busdetail['task']['status'],worker=nr['worker_instance_id'],thread_id=thread)
except Exception as error:v['error']=e.redact(str(error))
finally:e.save('verdict.json',v);e.finish()
print(json.dumps(v));raise SystemExit(0 if v['status']=='PASS' else 1)
