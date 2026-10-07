#!/usr/bin/env python3
"""Final artifact smoke then the single authorized deployment; no business replay."""
import argparse, fcntl, json, os, shutil, subprocess, sys
from pathlib import Path
sys.dont_write_bytecode=True
from deploy_resume_timeout import Rollout
root=Path.home()/'.local/state/openagentx/validation/2026-10-07-resume-timeout'
repo=Path('/home/sky/work/touzi/OneAxe/OpenAgentX-resume-timeout-worktree')
meta=json.loads((root/'final-artifact.json').read_text())
a=argparse.Namespace(profile=Path.home()/'.openagentx',installed=Path.home()/'.local/bin/openagentx',candidate=root/'openagentx-release',sha=meta['binary_sha256'],evidence=root/'deployment01',repo=repo,apply=True)
r=Rollout(a)
with (root/'batch.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
 if (a.evidence/'result.json').exists():
  raise SystemExit('This batch already has a result; inspect it, do not replay deployment.')
 try:
  require_log=root/'combined-tests.log'
  assert require_log.exists() and '\nFAIL' not in require_log.read_text()
  with (root/'release-smoke.log').open('w') as log:
   subprocess.run([sys.executable,str(repo/'scripts/validation/resume_timeout_release_smoke.py'),'--binary',str(a.candidate),'--root',str(root/'release01'),'--commit',meta['source_commit'],'--steer','--cleanup'],cwd=repo,stdout=log,stderr=subprocess.STDOUT,check=True)
  proof=root/'release01/evidence/release-result.json';result=json.loads(proof.read_text())
  assert result['status']=='PASS' and result['binary_sha256']==a.sha
  shutil.copy2(proof,root/'release-smoke-result.json')
  r.execute()
 except Exception as exc:
  if not (a.evidence/'result.json').exists():
   r.save('result.json',{'status':'FAILED_BEFORE_DEPLOYMENT','error':str(exc),'production_changed':False})
   r.event('batch_failed_before_deployment',error=str(exc))
   r.before={'states':r.states()}
   r.enqueue_verify('FAILED')
  raise
