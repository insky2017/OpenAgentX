from pathlib import Path
import os,pty,select,subprocess,time,json,hashlib
root=Path('/tmp/oax-web-assessment'); workspace=root/'real-agy-workspace';workspace.mkdir(mode=0o700,exist_ok=True)
(workspace/'ROLE.md').write_text('Only perform explicitly requested assessment actions in this workspace. Do not inspect unrelated files or credentials.\n')
aid='web-real-agy'; identity=root/(aid+'.yaml'); config=root/(aid+'-worker.yaml')
identity.write_text(f'''version: 1
agent_id: {aid}
principal_id: agent-{aid}
organization_id: default
display_name: {aid}
profile:
  instructions_path: {workspace}/ROLE.md
  workspace_root: {workspace}
  capabilities: [control-plane-testing]
''');identity.chmod(0o600)
pw=(root/'password').read_text();binary='/home/sky/.local/bin/openagentx'
args=[binary,'agent','apply','--db',str(root/'db.sqlite'),'--file',str(identity)]
pid,fd=pty.fork()
if pid==0:os.execv(binary,args)
buf=b'';sent=False;exitcode=None;start=time.monotonic()
while time.monotonic()-start<30:
 ready,_,_=select.select([fd],[],[],1)
 if ready:
  try:buf+=os.read(fd,65536)
  except OSError:break
  if not sent and b'password:' in buf.lower():os.write(fd,(pw+'\n').encode());sent=True;buf=b''
 waited,status=os.waitpid(pid,os.WNOHANG)
 if waited:exitcode=os.waitstatus_to_exitcode(status);break
if exitcode is None:
 _,status=os.waitpid(pid,0);exitcode=os.waitstatus_to_exitcode(status)
os.close(fd)
print('agent_apply_exit='+str(exitcode));print(buf.decode(errors='replace').replace(pw,'[redacted]').strip())
if exitcode:raise SystemExit(exitcode)
config.write_text(f'''version: 1
agent_id: {aid}
transport: unix
unix_socket: {root}/daemon.sock
heartbeat_interval: 1s
mailbox_wait: 5s
control_wait: 5s
shutdown_timeout: 10s
network_materialization_dir: {root}/real-agy-network
runtime_backends:
  - backend_id: primary
    adapter_id: agy-batch
    options:
      binary: /home/sky/.local/bin/agy-graft
      models: [gemini-3.7-flash-low]
      working_dir: {workspace}
''');config.chmod(0o600)
formalpid=subprocess.check_output(['systemctl','--user','show','openagentx-worker@quote-service.service','-p','MainPID','--value'],text=True).strip()
formal=dict(x.decode().split('=',1) for x in Path('/proc/'+formalpid+'/environ').read_bytes().split(b'\0') if b'=' in x)
env=os.environ.copy()
for k in list(env):
 if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_'):env.pop(k)
for k,v in formal.items():
 if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_'):env[k]=v
logfile=root/'real-agy-worker.log';fd=os.open(logfile,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
proc=subprocess.Popen([binary,'worker','run','--config',str(config)],env=env,stdout=fd,stderr=fd,start_new_session=True)
os.close(fd);(root/'real-agy-worker.pid').write_text(str(proc.pid))
print('isolated_worker_pid='+str(proc.pid));print('agent_id='+aid);print('workspace='+str(workspace));print('worker_config_sha256='+hashlib.sha256(config.read_bytes()).hexdigest())
