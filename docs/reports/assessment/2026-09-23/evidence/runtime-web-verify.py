from pathlib import Path
import sqlite3,json,hashlib,sys,datetime
root=Path('/tmp/oax-web-assessment');out=Path(__file__).resolve().parent
c=sqlite3.connect('file:'+str(root/'db.sqlite')+'?mode=ro',uri=True);c.row_factory=sqlite3.Row
expected=b'OAX-WEB-E2E-20260923\n';artifact=root/'real-agy-workspace'/'artifact.txt'
data=artifact.read_bytes() if artifact.exists() else b''
result={'checked_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'agent_id':'web-real-agy','artifact_exists':artifact.exists(),'artifact_bytes':len(data),'artifact_sha256':hashlib.sha256(data).hexdigest(),'artifact_exact':data==expected,'workers':[],'tasks':[],'production_e2e':False,'browser_chain':'see web-real-e2e.json and web-real-e2e-run.txt'}
result['workers']=[dict(r) for r in c.execute("select worker_instance_id,generation,status,last_heartbeat_at from worker_instances where agent_id='web-real-agy'")]
for task in c.execute("select task_id,status,error from tasks where target_agent_id='web-real-agy' order by created_at"):
 row=dict(task);row['runs']=[]
 for r in c.execute('select run_id,status,worker_instance_id,adapter_id,backend_id,model,result_json from run_attempts where task_id=? order by created_at',(task['task_id'],)):
  run=dict(r);raw=json.loads(run.pop('result_json') or '{}');run['runtime_status']=raw.get('status');run['side_effects_known']=raw.get('side_effects_known');run['reply_bytes']=len(raw.get('result','').encode());run['reply_has_marker']='OAX-WEB-E2E-20260923' in raw.get('result','');run['reply_sha256']=hashlib.sha256(raw.get('result','').encode()).hexdigest();run['provider_session_recorded']=bool(raw.get('provider_session_id'))
  run['runtime_event_counts']={r[0]:r[1] for r in c.execute('select event_type,count(*) from event_journal where aggregate_id=? group by event_type',(run['run_id'],))}
  row['runs'].append(run)
 row['task_event_counts']={r[0]:r[1] for r in c.execute('select event_type,count(*) from event_journal where aggregate_id=? group by event_type',(task['task_id'],))}
 row['mailbox']=[dict(r) for r in c.execute('select kind,lane,state,attempts from mailbox_items where task_id=?',(task['task_id'],))]
 result['tasks'].append(row)
name='runtime-web-'+(sys.argv[1] if len(sys.argv)>1 else 'final')+'.json'
(out/name).write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False,indent=2))
