from pathlib import Path
import subprocess,os,time,json
out=Path(__file__).resolve().parent
pid=subprocess.check_output(['systemctl','--user','show','openagentx-worker@quote-service.service','-p','MainPID','--value'],text=True).strip()
formal=dict(x.decode().split('=',1) for x in Path('/proc/'+pid+'/environ').read_bytes().split(b'\0') if b'=' in x)
env=os.environ.copy()
keys=[k for k in env if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_')]
for k in keys: env.pop(k,None)
allowed=[k for k in formal if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_')]
for k in allowed: env[k]=formal[k]
cmd=['go','test','-overlay='+str(out/'runtime-overlay.json'),'-count=1','-v','-timeout=7m','./internal/cli/worker','-run','^TestAssessmentRealAGYConsecutiveTurns$']
head='COMMAND '+' '.join(cmd)+'\nformal_worker_environment_keys='+','.join(sorted(allowed))+'\nformal_worker_environment_values=omitted\n'
t=time.monotonic()
r=subprocess.run(cmd,cwd='/tmp/oax-assessment-20260923-source',env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=450)
summary=json.dumps({'exit_code':r.returncode,'duration_seconds':round(time.monotonic()-t,3)})
(out/'runtime-live-test.txt').write_text(head+r.stdout+'\n'+summary+'\n')
print(head+r.stdout+'\n'+summary)
