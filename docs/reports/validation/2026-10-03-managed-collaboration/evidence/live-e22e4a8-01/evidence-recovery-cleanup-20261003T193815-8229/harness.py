import argparse,json,os,signal,sys,time
from pathlib import Path
sys.dont_write_bytecode=True
sys.path.insert(0,'/home/sky/work/touzi/OneAxe/OpenAgentX-workflow-worktree/scripts/validation')
from managed_collaboration_recovery_e2e import Recovery,process,descendants
from managed_collaboration_e2e import Acceptance,now,sha,require
root=Path('/home/sky/.local/state/openagentx/validation/2026-10-03-managed-collaboration/live-e22e4a8-01')
a=argparse.Namespace(root=root,binary=Path('/home/sky/.local/state/openagentx/validation/2026-10-03-managed-collaboration/openagentx-e22e4a8'),web=Path('/home/sky/work/touzi/OneAxe/OpenAgentX-workflow-worktree/web/dist'),commit='e22e4a89862246c33aef822527ea3b145f150a74',phase='recovery-cleanup',replace_binary=False)
r=Acceptance(a)
(r.out/'harness.py').write_bytes(Path(__file__).read_bytes())
r.save('attempt-provenance.json',{'at':now(),'phase':'recovery-cleanup','scope':'exact fixture process identities only; no task dispatch; no production process signals','harness_sha256':sha(__file__)})
def identity(pid):
 p=Path('/proc')/str(pid); fields=(p/'stat').read_text().rsplit(')',1)[1].split()
 return {'pid':pid,'starttime':fields[19],'exe':os.readlink(p/'exe'),'exe_sha256':sha(p/'exe')}
protected={pid:identity(pid) for pid in [2578436,2578482,2578484]}
r.save('protected-before.json',list(protected.values()))
r.login();tasks=r.all_tasks()
require(all(t['status'] in {'succeeded','failed','uncertain','canceled'} for t in tasks),'active Tasks block cleanup')
r.save('before-tasks.json',tasks)
r.save('before-overview.json',r.api('/api/observe/v1/overview'))
r.process_provenance('before-owned-processes')
refs=list(r.procs); childrefs=[]
for ref in refs:
 require(ref['pid'] not in protected,'protected process in owned manifest')
 childrefs.extend(descendants(ref['pid']))
r.save('before-descendants.json',childrefs)
records=[]
def terminate(ref):
 if not process(ref): return {'process':ref,'already_stopped':True}
 require(ref['pid'] not in protected,'refuse protected PID')
 os.kill(ref['pid'],signal.SIGTERM)
 until=time.monotonic()+20
 while process(ref) and time.monotonic()<until:time.sleep(.2)
 require(not process(ref),'owned process did not exit gracefully; preserve for review')
 return {'process':ref,'signal':'SIGTERM','alive_after':False}
try:
 for ref in reversed(refs):records.append(terminate(ref))
 for ref in childrefs:
  if process(ref):records.append(terminate(ref))
 remaining=[ref for ref in refs+childrefs if process(ref)]
 after={pid:identity(pid) for pid in protected}
 require(after==protected,'protected service identity changed')
 require(not remaining,'owned engine descendant still alive')
 r.save('protected-after.json',list(after.values()))
 r.save('cleanup-result.json',{'status':'PASS','at':now(),'operations':records,'remaining_owned_processes':remaining,'protected_services_unchanged':True,'files_preserved':True,'task_dispatch':0})
 print(json.dumps({'status':'PASS','evidence':str(r.out)}),flush=True)
except Exception as error:
 r.save('cleanup-failure.json',{'at':now(),'error':str(error),'operations':records});raise
finally:
 (r.out/'SHA256SUMS').write_text('\n'.join(sha(p)+'  '+str(p.relative_to(r.out)) for p in sorted(r.out.rglob('*')) if p.is_file() and p.name!='SHA256SUMS')+'\n')
