import os,sys,pathlib,json,hashlib,pty,select,time,signal,errno,datetime,re
raw=pathlib.Path(__file__).parent; os.umask(0o077)
env=os.environ.copy()
for key in ('OPENAGENTX_DB','OPENAGENTX_SOCKET','OPENAGENTX_FLEET_FILE','OPENAGENTX_WORKER_DIR','OPENAGENTX_CREDENTIALS','OPENAGENTX_DB_PATH','OPENAGENTX_SOCKET_PATH','OPENAGENTX_FLEET_MANIFEST','OPENAGENTX_WORKER_CONFIG_DIR','OPENAGENTX_CREDENTIALS_PATH'):env.pop(key,None)
env['OPENAGENTX_HOME']=str(raw/'profile')
binary=str(raw/'bin/openagentx')
# Explicit flags prevent any resource-specific environment override reaching a real profile.
paths=['--db',str(raw/'profile/oax.db'),'--socket',str(raw/'profile/oax.sock'),'--file',str(raw/'profile/fleet.yaml'),'--worker-dir',str(raw/'profile/workers'),'--credentials',str(raw/'profile/credentials.json')]
join=['agent','join','--id','cli-evidence','--name','CLI Evidence','--workspace',str(raw/'workspace'),'--role',str(raw/'workspace/ROLE.md'),'--handoff-file',str(raw/'workspace/HANDOFF.md'),'--prepare']+paths
commands=[('01-help',['agent','join','--help']),('02-join',join),('03-join-repeat',join),('04-status',['agent','status','cli-evidence']+paths),('05-status-json',['agent','status','cli-evidence','--json']+paths)]
results=[]
def snapshot():
 return {str(p.relative_to(raw/'profile')):{'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'mode':oct(p.stat().st_mode&0o777)} for p in sorted((raw/'profile').rglob('*')) if p.is_file()}
for label,args in commands:
 trace=raw/(label+'.strace'); start=time.time()
 command=['/usr/bin/strace','-f','-o',str(trace),'-e','trace=process,connect',binary]+args
 (raw/(label+'.command.json')).write_text(json.dumps({'argv':command,'environment_overrides':{'OPENAGENTX_HOME':env['OPENAGENTX_HOME']},'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat()},ensure_ascii=False,indent=2)+'\n')
 pid,master=pty.fork()
 if pid==0:
  os.write(1,('PTY stdin=%s stdout=%s\n'%(os.isatty(0),os.isatty(1))).encode())
  os.execvpe(command[0],command,env)
 output=bytearray();deadline=time.time()+30
 while True:
  if time.time()>deadline:
   os.killpg(pid,signal.SIGTERM);raise RuntimeError(label+' timed out')
  ready,_,_=select.select([master],[],[],.5)
  if ready:
   try: chunk=os.read(master,65536)
   except OSError as e:
    if e.errno==errno.EIO:break
    raise
   if not chunk:break
   output.extend(chunk)
 os.close(master);_,status=os.waitpid(pid,0);exit_code=os.waitstatus_to_exitcode(status)
 (raw/(label+'.pty.log')).write_bytes(output)
 trace_text=trace.read_text();execs=[line for line in trace_text.splitlines() if 'execve(' in line];connections=[line for line in trace_text.splitlines() if 'connect(' in line]
 results.append({'label':label,'exit_code':exit_code,'duration_seconds':round(time.time()-start,3),'pty':True,'execve_count':len(execs),'connect_count':len(connections)})
 if exit_code!=0:raise RuntimeError(label+' failed')
 if label=='02-join':first=snapshot();(raw/'profile-after-first.json').write_text(json.dumps(first,indent=2)+'\n')
 if label=='03-join-repeat':second=snapshot();(raw/'profile-after-repeat.json').write_text(json.dumps(second,indent=2)+'\n');assert first==second,'repeat changed persisted files'
receipt=json.loads((raw/'profile/workers/joins/cli-evidence.json').read_text())
manifest=(raw/'profile/fleet.yaml').read_text()
checks={'all_exit_zero':all(r['exit_code']==0 for r in results),'real_pty':all(r['pty'] for r in results),'repeat_same_content_and_modes':first==second,'receipt_local_prepared':receipt['state']=='local_prepared' and receipt['ready'] is False,'fleet_disabled':'enabled: false' in manifest and 'enabled: true' not in manifest,'database_absent':not (raw/'profile/oax.db').exists(),'socket_absent':not (raw/'profile/oax.sock').exists(),'no_connect_syscalls':all(r['connect_count']==0 for r in results),'no_child_program_exec':all(r['execve_count']==1 for r in results)}
report={'scope':'CLI PTY delivery plus independent persisted-file/process trace checks only; no Codex model, Worker, Control or native workflow E2E','commands':results,'checks':checks,'all_checks_pass':all(checks.values())}
(raw/'verification.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'raw':str(raw),'checks':checks},ensure_ascii=False))
