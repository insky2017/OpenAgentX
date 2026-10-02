import os,sys,json,pathlib,pty,select,time,termios,fcntl,struct,errno,datetime,re
raw=pathlib.Path(__file__).parent;base=raw.parent;local=base/'local01';handoff=json.loads((local/'handoff.json').read_text());os.umask(0o077)
command=[str(base/'candidate02/openagentx'),'agent','open',handoff['agent'],'--native','--db',str(local/'profile/data/openagentx.db'),'--socket',handoff['socket'],'--file',str(local/'profile/fleet.yaml'),'--worker-dir',str(local/'profile/workers'),'--credentials',str(local/'profile/credentials.json')]
(raw/'open-command.json').write_text(json.dumps(command,indent=2)+'\n')
pid,fd=pty.fork()
if pid==0:
 os.chdir(handoff['workspace']);env=os.environ.copy();env['TERM']='xterm-256color';env['OPENAGENTX_HOME']=handoff['profile'];os.execvpe(command[0],command,env)
fcntl.ioctl(fd,termios.TIOCSWINSZ,struct.pack('HHHH',36,140,0,0))
log=(raw/'terminal.raw').open('wb',buffering=0);actions=(raw/'pty-actions.jsonl').open('a',buffering=1);tail=bytearray()
print(json.dumps({'started_pid':pid,'raw':str(raw)}),flush=True)
while True:
 ready,_,_=select.select([fd,sys.stdin],[],[],1)
 if fd in ready:
  try:chunk=os.read(fd,65536)
  except OSError as e:
   if e.errno==errno.EIO:break
   raise
  if not chunk:break
  log.write(chunk);tail.extend(chunk);tail=tail[-200000:]
  if b'\x1b[6n' in chunk:os.write(fd,b'\x1b[1;1R')
 if sys.stdin in ready:
  line=sys.stdin.readline()
  if not line:continue
  request=json.loads(line);actions.write(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),**request})+'\n')
  if request['action']=='send':os.write(fd,request['text'].encode());print(json.dumps({'sent_bytes':len(request['text'].encode())}),flush=True)
  elif request['action']=='snapshot':
   text=tail.decode(errors='replace');text=re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]','',text);text=re.sub(r'\x1b\][^\x07]*(?:\x07|\x1b\\)','',text);print(json.dumps({'terminal_tail':text[-request.get('limit',6000):]},ensure_ascii=False),flush=True)
os.close(fd);_,status=os.waitpid(pid,0);code=os.waitstatus_to_exitcode(status);result={'exit_code':code,'child_pid':pid,'finished_at':datetime.datetime.now(datetime.timezone.utc).isoformat()};(raw/'terminal-result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True)
