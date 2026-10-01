#!/usr/bin/env python3
"""Transparent byte forwarding to formal agy-graft, with private raw evidence.
Adds one supervisor PID; final installed smoke must also use unwrapped agy-graft.
"""
import json,os,pathlib,subprocess,sys,threading,time
if sys.argv[1:]==['--version']: os.execv('/home/sky/.local/bin/agy-graft',['/home/sky/.local/bin/agy-graft','--version'])
root=(pathlib.Path(__file__).resolve().parent/'runtime-raw')/('run-'+str(os.getpid()));root.mkdir(parents=True,mode=0o700)
argv=['/home/sky/.local/bin/agy-graft',*sys.argv[1:]]
(root/'argv.json').write_text(json.dumps(argv));(root/'pid').write_text(str(os.getpid()))
p=subprocess.Popen(argv,stdin=sys.stdin.buffer,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
(root/'child.pid').write_text(str(p.pid))
(root/'environment-manifest.json').write_text(json.dumps({'cwd':os.getcwd(),'native_proxy_flag_forwarded':os.environ.get('AGY_GRAFT_NATIVE_PROXY')=='1','proxy_variable_names':sorted(k for k in os.environ if 'proxy' in k.lower()),'capture_pid':os.getpid(),'child_pid':p.pid}))
def copy(source,target,name):
    with open(root/name,'wb',buffering=0) as output:
        while data:=source.read1(65536):output.write(data);target.write(data);target.flush()
threads=[threading.Thread(target=copy,args=(p.stdout,sys.stdout.buffer,'stdout')),threading.Thread(target=copy,args=(p.stderr,sys.stderr.buffer,'stderr'))]
for t in threads:t.start()
rc=p.wait()
for t in threads:t.join()
(root/'exit.json').write_text(json.dumps({'exit_code':rc,'at':time.time()}));sys.exit(rc)
