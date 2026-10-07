import argparse,json,sys
from pathlib import Path
sys.dont_write_bytecode=True
sys.path.insert(0,'/home/sky/work/touzi/OneAxe/OpenAgentX-tmux-spinner-worktree/scripts/validation')
from native_model_settings_e2e import SettingsRun
from terminal_overview_e2e import now,require,sha
base=Path('/home/sky/.local/state/openagentx/validation/2026-10-07-tmux-work-spinner')
a=argparse.Namespace(binary=base/'artifacts/openagentx-final',root=base/'live-03',phase='native')
r=SettingsRun.restore(a);e=r.out
samples=json.loads((e/'spinner-samples.json').read_text());detail=json.loads((e/'spinner-task.json').read_text());lines=json.loads((e/'spinner-rendered-status.json').read_text());before=json.loads((e/'spinner-before.json').read_text());window=r.windows['native'];ordinary=r.windows['ordinary'];proof=r.root/'workspace/spinner-proof.txt'
frames=[c for c in '⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏' if any(c in line for line in lines)]
require(len(frames)==10,'missing real rendered frames')
require(samples[-1]['state']=='idle' and any(s['state']=='running' and s['active']=='0' for s in samples) and any(s['state']=='running' and s['active']=='1' for s in samples),'state/current matrix incomplete')
require(all(s['ordinary_format']=='USER:#W' for s in samples),'ordinary normal format changed')
require(r.tmux('show-options','-w','-v','-t',ordinary,'window-status-current-format')=='USER-CURRENT:#W','ordinary current format changed')
require(r.tmux('show-options','-v','-t','OAX','status-interval')=='1','status interval changed')
require(r.tmux('show-options','-w','-v','-t',window,'@oax_state')=='idle','idle state missing')
require(proof.read_bytes()==b'SPINNER-PROOF-DONE\n','file proof mismatch')
require(detail['task']['status']=='uncertain' and detail['task']['error']=='business_effect_unverified','mutation outcome mismatch')
require(len(detail['run_attempts'])==1 and detail['run_attempts'][0]['status']=='succeeded','Runtime not succeeded exactly once')
require(any(r.aid in line and any(c in line for c in frames) for line in lines),'name+spinner missing')
require(any(r.aid in line and not any(c in line for c in frames) for line in lines),'idle name missing')
after=r.snapshot('spinner-after');require([(p['window'],p['name']) for p in before['panes']]==[(p['window'],p['name']) for p in after['panes']],'window names changed')
result={'at':now(),'result':'PASS','evidence_level':'R real Codex Worker + formal API + isolated tmux PTY + independent file','binary_sha256':sha(a.binary),'task_id':detail['task']['id'],'task_status':detail['task']['status'],'task_error':detail['task']['error'],'first_running_seconds':next(s['elapsed'] for s in samples if s['state']=='running'),'frames_seen_in_attached_pty':frames,'proof':proof.name,'proof_sha256':sha(proof),'normal_and_current':True,'ordinary_formats_preserved':True,'reverification':'No model replay. Corrected harness tmux show-options target from =OAX to OAX; rechecked saved actual samples, PTY status and Task/Run against independent file and current window options.','limits':'Approval and two-Agent isolation use real tmux + controllable Runtime integration tests. No production installed/restarted.'}
r.save('spinner-result.json',result);r.task_evidence();print(json.dumps(result,ensure_ascii=False))
