#!/usr/bin/env python3
"""Owned native settings acceptance; no production services or default tmux server."""
import argparse,json,os,secrets,sys,time,signal,shlex
from pathlib import Path
sys.dont_write_bytecode=True
sys.path.insert(0,str(Path(__file__).resolve().parent) if (Path(__file__).resolve().parent/'terminal_overview_e2e.py').exists() else '/home/sky/work/touzi/OneAxe/OpenAgentX-native-model-worktree/scripts/validation')
from terminal_overview_e2e import TerminalRun,AttachedClient,require,sha,now

class SettingsRun(TerminalRun):
 def cli(self,label,args):
  if label=='01-join':
   role=Path(args[args.index('--role')+1]);role.write_text('You are an isolated OpenAgentX native model settings verification Agent. Work only in this assigned workspace. Execute exact requested local tools and write only requested proof files. Never read credentials, message other Agents, alter services, inspect unrelated paths, or act on production. Do not spawn subagents. When asked to confirm role/workspace reply exactly: '+self.expected+'\n')
  return super().cli(label,args)
 def prepare(self):
  self.setup()
  self.windows['native']=self.tmux('new-session','-d','-x','160','-y','45','-P','-F','#{window_id}','-s','OAX','-n',self.aid,'sleep','86400')
  self.tmux('set-option','-w','-t',self.windows['native'],'pane-base-index','0')
  self.tmux('set-option','-w','-t',self.windows['native'],'automatic-rename','off')
  self.tmux('set-option','-w','-t',self.windows['native'],'remain-on-exit','on')
  self.client=AttachedClient(self)
  launcher=self.script('native',[self.a.binary,'agent','open',self.aid,'--native'])
  self.tmux('respawn-pane','-k','-t',self.windows['native']+'.0',launcher)
  self.wait(self.lock_held,90)
  def initial_done():
   tasks=[t for t in self.api('/api/observe/v1/tasks?limit=100')['tasks'] if t['target_agent_id']==self.aid]
   require(len(tasks)<=1,'native initialization duplicated')
   if tasks and tasks[0]['status'] in {'succeeded','failed','uncertain','canceled'}: return self.api('/api/observe/v1/tasks/'+tasks[0].get('id',tasks[0].get('task_id')))
  initial=self.wait(initial_done,360);self.save('initialization.json',initial)
  require(initial['task']['status']=='succeeded','initialization failed')
  self.task_ids.append(initial['task'].get('id',initial['task'].get('task_id')))
  self.wait(lambda:'OpenAgentX · '+self.aid in self.capture(self.windows['native']+'.0','native-ready'),60)
  self.checkpoint();self.snapshot('ready')
  self.client.close();self.client=None
  print(json.dumps({'status':'READY','root':str(self.root),'agent':self.aid,'tmux_server':self.server}),flush=True)
 def restart(self,release):
  require(release and release.is_file(),'fixed release is required')
  pane=self.windows['native']+'.0';statepath=self.root/'profile/workers/codex'/self.aid/'state.json'
  before=json.loads(statepath.read_text());require(before['state']=='idle','fixture backend is not idle')
  settings=self.api('/api/console/v1/agents/'+self.aid+'/model-settings?backend_id=codex')
  self.save('restart-before.json',{'at':now(),'engine':before,'settings':settings,'processes':self.procs,'binary':str(release),'binary_sha256':sha(release)})
  self.client=AttachedClient(self);self.tmux('send-keys','-t',pane,'C-d');self.wait(lambda:self.tmux('display-message','-p','-t',pane,'#{pane_dead}')=='1',30);self.client.close();self.client=None
  oldprocs=list(self.procs)
  for label in ['worker','daemon']:
   proc=next(p for p in reversed(oldprocs) if p['label']==label)
   def alive():
    try:fields=(Path('/proc')/str(proc['pid'])/'stat').read_text().rsplit(')',1)[1].split()
    except FileNotFoundError:return False
    require(fields[19]==str(proc['starttime']),'PID identity changed');return fields[0]!='Z'
   if alive():os.kill(proc['pid'],signal.SIGTERM);self.wait(lambda:not alive(),45)
  
  for label in ['worker','daemon']:
   for stream in ['stdout','stderr']:
    source=self.root/(label+'.'+stream)
    if source.exists(): (self.root/('before-restart-'+label+'.'+stream)).write_bytes(source.read_bytes())
  self.procs=[];self.a.binary=release;self.port=int(self.url.rsplit(':',1)[1]);self.a.web=self.root/'webroot'
  self.start('daemon',[str(release),'serve','--http-addr','127.0.0.1:'+str(self.port),'--web-dir',str(self.a.web)])
  self.wait(lambda:self.socket.exists(),30)
  config=self.root/'profile/workers'/(self.aid+'.yaml');savedenv=dict(self.env)
  for line in config.with_suffix('.env').read_text().splitlines():
   if line.strip() and not line.startswith('#'):
    pair=shlex.split(line);require(len(pair)==1,'invalid fixture env');key,value=pair[0].split('=',1);self.env[key]=value
  self.start('worker',[str(release),'worker','run','--config',str(config)]);self.env=savedenv
  self.wait(lambda:any(w['agent_id']==self.aid and w['status']=='online' for w in self.api('/api/observe/v1/overview')['workers']),120)
  self.cli('restart-network-resume',['agent','resume',self.aid,'--password-file',str(self.root/'password'),'--no-open','--wait','3m'])
  self.wait(lambda:self.api('/api/console/v1/attach?agent_id='+self.aid).get('backend_health',{}).get('codex')=='healthy',60)
  after_settings=self.api('/api/console/v1/agents/'+self.aid+'/model-settings?backend_id=codex');require(after_settings==settings,'settings did not survive restart')
  launcher=self.script('native-release',[release,'agent','open',self.aid,'--native']);self.client=AttachedClient(self);self.tmux('respawn-pane','-k','-t',pane,launcher);self.wait(self.lock_held,60);time.sleep(2)
  after=json.loads(statepath.read_text());require(after['thread_id']==before['thread_id'],'restart replaced thread')
  self.save('restart-after.json',{'at':now(),'engine':after,'settings':after_settings,'processes':self.procs,'binary_sha256':sha(release)})
  self.capture(pane,'restart-native',False);self.checkpoint();self.client.close();self.client=None
  print(json.dumps({'status':'RESTARTED','thread':after['thread_id'],'model':after_settings['model'],'effort':after_settings['effort']}))
 def cleanup(self):
  statepath=self.root/'profile/workers/codex'/self.aid/'state.json';state=json.loads(statepath.read_text());require(state['state']=='idle','refuse cleanup of active/unresolved fixture')
  self.save('before-cleanup.json',{'at':now(),'engine':state,'attach':self.api('/api/console/v1/attach?agent_id='+self.aid),'processes':self.procs})
  pane=self.windows['native']+'.0';self.client=AttachedClient(self);self.tmux('send-keys','-t',pane,'C-d');self.wait(lambda:self.tmux('display-message','-p','-t',pane,'#{pane_dead}')=='1',30);self.client.close();self.client=None
  stopped=[]
  for label in ['worker','daemon']:
   proc=next(p for p in reversed(self.procs) if p['label']==label)
   def alive():
    try:fields=(Path('/proc')/str(proc['pid'])/'stat').read_text().rsplit(')',1)[1].split()
    except FileNotFoundError:return False
    require(fields[19]==str(proc['starttime']),'PID identity changed');return fields[0]!='Z'
   if alive():os.kill(proc['pid'],signal.SIGTERM);self.wait(lambda:not alive(),45)
   stopped.append({'label':label,'pid':proc['pid'],'stopped':True})
  self.tmux('kill-server');self.save('cleanup.json',{'at':now(),'processes':stopped,'owned_tmux_server':self.server,'evidence_preserved':True,'production_touched':False});print('owned fixture stopped; evidence retained')
 def task_evidence(self):
  ids=[]
  for t in self.api('/api/observe/v1/tasks?limit=100')['tasks']:
   if t['target_agent_id']!=self.aid:continue
   tid=t.get('id',t.get('task_id'));d=self.api('/api/observe/v1/tasks/'+tid);self.save('tasks/'+tid+'.json',d);ids.append(tid)
   for r in d.get('run_attempts',[]):self.save('runs/'+r['run_id']+'.json',self.api('/api/observe/v1/run-attempts/'+r['run_id']))
  self.task_ids=ids;self.checkpoint()
  return ids

if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--binary',type=Path,required=True);p.add_argument('--root',type=Path,required=True);p.add_argument('--commit',default='working-candidate');p.add_argument('action',choices=['prepare','capture','send','keys','evidence','restart','cleanup']);p.add_argument('value',nargs='?');p.add_argument('--release',type=Path);a=p.parse_args();a.phase='native';os.umask(0o077)
 run=SettingsRun(a) if a.action=='prepare' else SettingsRun.restore(a)
 try:
  pane=run.windows.get('native','')+'.0'
  if a.action=='prepare':run.prepare()
  elif a.action=='restart':run.restart(a.release)
  elif a.action=='cleanup':run.cleanup()
  elif a.action=='capture':print(run.capture(pane,'capture-'+str(time.time_ns()),False))
  elif a.action in {'send','keys'}:
   run.client=AttachedClient(run)
   if a.action=='send':run.tmux('send-keys','-t',pane,'-l',a.value);time.sleep(.4);run.tmux('send-keys','-t',pane,'Enter')
   else:run.tmux('send-keys','-t',pane,*a.value.split())
   time.sleep(2);print(run.capture(pane,'capture-'+str(time.time_ns()),False));run.client.close();run.client=None
  else:print(json.dumps({'tasks':run.task_evidence()}))
 except Exception as e:
  run.save('failure-'+str(time.time_ns())+'.json',{'at':now(),'type':type(e).__name__,'message':str(e).replace(run.password,'[REDACTED]')});raise
 finally:
  if run.client:run.client.close()
