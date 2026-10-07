import argparse,importlib.util,pathlib,json,os,signal,subprocess,time,shlex
root=pathlib.Path.home()/'.local/state/openagentx/validation/2026-10-07-native-model-settings'
s=importlib.util.spec_from_file_location('deployment','/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/scripts/validation/deploy_native_model_settings.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
r=m.Rollout(argparse.Namespace(profile=pathlib.Path.home()/'.openagentx',installed=pathlib.Path.home()/'.local/bin/openagentx',evidence=root/'recovery02'))
before=json.loads((root/'deployment01/before.json').read_text())
def processes():
 result={}
 for p in pathlib.Path('/proc').iterdir():
  if not p.name.isdigit():continue
  try:
   st=(p/'stat').read_text().rsplit(')',1)[1].split();args=[a.decode() for a in (p/'cmdline').read_bytes().split(b'\0') if a]
   result[int(p.name)]={'pid':int(p.name),'ppid':int(st[1]),'start':st[19],'argv':args,'comm':(p/'comm').read_text().strip()}
  except (OSError,ValueError):continue
 return result
def tree(root,all):
 ids=[root];n=0
 while n<len(ids):
  ids.extend(p for p,v in all.items() if v['ppid']==ids[n]);n+=1
 return [all[p] for p in ids if p in all]
for agent in m.AGENTS:
 current=r.panes()[agent];recorded=before['panes'][agent];thread=before['states'][agent]['thread_id']
 assert current['window']==recorded['window'] and current['pane']==recorded['pane']
 assert r.states()[agent]['thread_id']==thread
 assert r.attach(agent)['backend_health']['codex']=='healthy'
 ps=processes();owned=tree(current['pid'],ps)
 bridges=[p for p in owned if pathlib.Path(p['argv'][0]).name=='openagentx' and p['argv'][1:4]==['agent','open',agent] and '--native' in p['argv']]
 views=[p for p in owned if p['comm']=='codex' and len(p['argv'])>1 and p['argv'][1]=='resume' and '--remote' in p['argv'] and p['argv'][-1]==thread]
 assert (len(bridges)==len(views)==1) or (not bridges and not views and len(owned)==1 and owned[0]['comm'] in ('zsh','bash','sh')), 'unexpected foreground process tree'
 r.save('native-before-'+agent+'.json',{'pane':current,'processes':owned})
 # Retain raw screen privately, never commit business transcript.
 raw=r.command('tmux','capture-pane','-p','-t',current['pane'],'-S','-80')
 screen=r.a.evidence/('private-screen-'+agent+'.txt');screen.write_text(raw);screen.chmod(0o600)
 if bridges:
  target=bridges[0]
  assert processes()[target['pid']]['start']==target['start']
  handle=os.pidfd_open(target['pid']);signal.pidfd_send_signal(handle,signal.SIGTERM);os.close(handle)
  deadline=time.monotonic()+20
  while target['pid'] in processes() or views[0]['pid'] in processes():
   assert time.monotonic()<deadline,'old foreground did not stop'
   time.sleep(.5)
 current=r.panes()[agent]
 assert current['window']==recorded['window'] and current['pane']==recorded['pane']
 command=shlex.join(r.agent_command('open',agent,'--native'))
 if current['dead']:
  r.command('tmux','respawn-pane','-t',current['pane'],command)
 else:
  owned=tree(current['pid'],processes())
  assert len(owned)==1 and owned[0]['comm'] in ('zsh','bash','sh'),'foreground shell is not idle'
  r.command('tmux','send-keys','-t',current['pane'],'C-u')
  r.command('tmux','send-keys','-t',current['pane'],'-l','exec '+command)
  r.command('tmux','send-keys','-t',current['pane'],'Enter')
 deadline=time.monotonic()+30
 while True:
  current=r.panes()[agent];owned=tree(current['pid'],processes())
  views=[p for p in owned if p['comm']=='codex' and len(p['argv'])>1 and p['argv'][1]=='resume' and '--remote' in p['argv'] and p['argv'][-1]==thread]
  if len(views)==1:break
  assert time.monotonic()<deadline,'new native view did not start'
  time.sleep(1)
 time.sleep(2)
 assert r.states()[agent]['thread_id']==thread
 r.save('native-after-'+agent+'.json',{'pane':current,'processes':owned,'thread_id':thread})
 r.event('foreground_reconnected',agent=agent,pane=current['pane'],thread_preserved=True)
