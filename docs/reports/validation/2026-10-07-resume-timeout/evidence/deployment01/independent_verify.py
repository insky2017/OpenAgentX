#!/usr/bin/env python3
"""Read-only deployment verification. GET APIs, /proc, tmux show, RO SQLite only.
Writes sanitized evidence in this script's validation directory only.
"""
import sys,json,os,sqlite3,time,hashlib
from pathlib import Path
from types import SimpleNamespace
sys.path.insert(0,'/home/sky/work/touzi/OneAxe/OpenAgentX-resume-timeout-worktree/scripts/validation')
from deploy_resume_timeout import Rollout,AGENTS,digest,now
import yaml
base=Path(__file__).resolve().parent
profile=Path('/home/sky/.openagentx')
sha='259ffc489f5a85a369c5ab40577fbba762846b15d18cc2e7aeaed195cb833e69'
r=Rollout(SimpleNamespace(evidence=base,profile=profile,installed=Path('/home/sky/.local/bin/openagentx')))
checks={}; evidence={'at':now(),'script_sha256':digest(__file__),'binary_sha256':sha,'read_only':True}
def check(name,value):checks[name]=bool(value)
def select(d,keys):return {k:d.get(k) for k in keys}
def windows():
 keys=['session','window','name','managed','agent','state','format','current_format']
 rows=r.command('tmux','list-windows','-a','-F','#{session_name}|#{window_id}|#{window_name}|#{@openagentx_managed}|#{@openagentx_agent_id}|#{@oax_state}|#{window-status-format}|#{window-status-current-format}')
 return [dict(zip(keys,line.split('|'))) for line in rows.splitlines()]
try:
 before=json.loads((base/'before.json').read_text()); after=json.loads((base/'after.json').read_text())
 snap=r.snapshot()
 for a,w in snap['workers'].items():
  snap['workers'][a]=select(w,['agent_id','generation','worker_status','worker_instance_id','last_heartbeat_at','lease_until','backend_health','readiness','active_run'])
 evidence['snapshot']=snap
 check('installed_artifact',snap['binary_sha256']==sha)
 check('seven_live_service_artifacts',len(snap['service_artifacts'])==7 and all(v['sha256']==sha for v in snap['service_artifacts'].values()))
 evidence['service_active']={u:r.command('systemctl','--user','is-active',u) for u in snap['service_artifacts']}
 check('seven_services_active',all(v=='active' for v in evidence['service_active'].values()))
 evidence['agents']={}
 for a in AGENTS:
  w=snap['workers'][a]; p=snap['panes'][a]; state=snap['states'][a]; tree=r.process_tree(p['pid'])
  bridges=[x for x in tree if x['argv'] and Path(x['argv'][0]).name=='openagentx' and x['argv'][1:4]==['agent','open',a] and '--native' in x['argv']]
  views=[x for x in tree if x['comm']=='codex' and '--remote' in x['argv'] and x['argv'][-1]==state['thread_id']]
  config=yaml.safe_load((profile/'workers'/(a+'.yaml')).read_text())
  timeout=[x['options'].get('timeout') for x in config['runtime_backends'] if x['adapter_id']=='codex-app-server']
  model=r.api('/api/console/v1/agents/'+a+'/model-settings?backend_id=codex')
  bridge=[{'pid':x['pid'],'start':x['start'],'sha256':digest(Path('/proc')/str(x['pid'])/'exe')} for x in bridges]
  evidence['agents'][a]={'generation_before':before['workers'][a]['generation'],'generation_now':w['generation'],'generation_after':after['workers'][a]['generation'],'bridge':bridge,'native_view_pids':[x['pid'] for x in views],'timeout':timeout,'worker_config_sha256':digest(profile/'workers'/(a+'.yaml')),'model_settings':select(model,['agent_id','backend_id','model','effort','version']),'model_preferences_before':select(before['model_settings'][a],['agent_id','backend_id','model','effort','version'])}
  check(a+'_healthy_ready',w['worker_status']=='online' and w['backend_health'].get('codex')=='healthy' and w['readiness'].get('ready') is True)
  check(a+'_generation',w['generation']==after['workers'][a]['generation'] and w['generation']>before['workers'][a]['generation'])
  check(a+'_thread_pane_preserved',state['thread_id']==before['states'][a]['thread_id'] and all(p[k]==before['panes'][a][k] for k in ['window','pane','name']) and not p['dead'])
  check(a+'_native_artifact',len(bridge)==1 and bridge[0]['sha256']==sha and len(views)==1)
  check(a+'_timeout_zero',timeout==['0s'])
  check(a+'_model_preferences_preserved',evidence['agents'][a]['model_settings']==evidence['agents'][a]['model_preferences_before'])
 tid=snap['states']['openagentx']['task_id']
 detail=r.api('/api/console/v1/agents/openagentx/tasks/'+tid)
 latest=detail['latest_run']; runid=latest['run_id']
 evidence['current_run']={'task_id':tid,'latest_run':select(latest,['run_id','status','started_at','finished_at','deadline_at','worker_generation'])}
 db=sqlite3.connect('file:'+str(profile/'data/openagentx.db')+'?mode=ro',uri=True)
 try:frozen=json.loads(db.execute('SELECT resolved_execution_json FROM run_attempts WHERE run_id=?',(runid,)).fetchone()[0])
 finally:db.close()
 evidence['current_run']['frozen']={'timeout':frozen['spec']['timeout'],'deadline_at':frozen.get('deadline_at'),'read_only_sqlite':True}
 check('current_formal_run_no_deadline',not latest.get('deadline_at') and frozen['spec']['timeout']==0 and frozen.get('deadline_at') in [None,'','0001-01-01T00:00:00Z'])
 ws=windows(); evidence['windows']=ws
 managed=[w for w in ws if w['managed']=='1' and w['agent'] in AGENTS]
 ordinary=[w for w in ws if w['managed']!='1']
 check('six_managed_spinner_formats',len(managed)==6 and all(w['format'].count('tmux-spinner')==w['current_format'].count('tmux-spinner')==1 and w['state'] in ['running','idle'] for w in managed))
 check('ordinary_windows_currently_no_spinner',len(ordinary)>0 and all('tmux-spinner' not in w['format']+w['current_format'] and w['state']=='' for w in ordinary))
 check('spinner_matches_active_run',all((w['state']=='running')==bool(snap['workers'][w['agent']].get('active_run')) for w in managed))
 evidence['status_interval']=r.command('tmux','show-options','-v','-t','OAX','status-interval')
 check('status_interval_one_second',evidence['status_interval']=='1')
 evidence['boundaries']=['No deployment, restart, input injection, task submission, or business replay performed.','Current ordinary windows have no spinner; deployment before.json has no ordinary-window baseline, so historical unchanged claim is not established by this evidence.','No new 31-minute or native pending-message E2E run; prior exact-artifact acceptance remains separately recorded.','This is point-in-time deployment verification, not full lifecycle acceptance.']
 evidence['status']='PASS' if all(checks.values()) else 'FAILED'
except Exception as exc:
 evidence['status']='FAILED'; evidence['error_type']=type(exc).__name__; evidence['error']=str(exc)
evidence['checks']=checks;evidence['finished_at']=now()
r.save('independent-verification.json',evidence)
print(json.dumps({'status':evidence['status'],'failed_checks':[k for k,v in checks.items() if not v],'error':evidence.get('error'),'path':str(base/'independent-verification.json')}))
