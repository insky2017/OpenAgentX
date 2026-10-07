import sys,argparse,json
from pathlib import Path
sys.path.insert(0,'scripts/validation')
from native_model_settings_e2e import SettingsRun,require,now,sha
a=argparse.Namespace(binary=Path('/home/sky/.local/state/openagentx/validation/2026-10-07-pending-input/openagentx'),root=Path('/home/sky/.local/state/openagentx/validation/2026-10-07-pending-input/live02'),phase='native')
r=SettingsRun.restore(a);ids=r.task_evidence();require(len(ids)==3,'unexpected task count')
d=json.loads((r.out/'tasks/task-e84dc321-8548-4e27-8a22-e1fc435983b9.json').read_text());supplements=[m for m in d['messages'] if m['kind']=='supplement'];require(len(supplements)==3 and len(d['run_attempts'])==1,'invalid supplement/run count')
fresh=json.loads((r.out/'tasks/task-bdecc78c-245b-4115-9648-c20a15c506c7.json').read_text());require(fresh['run_attempts'][0]['status']=='succeeded','fresh run failed')
require((r.root/'workspace/receipts.txt').read_text()=='ACK_A\nACK_B\nACK_C\n','duplicate append');require((r.root/'workspace/fresh.txt').read_text()=='FRESH_OK','fresh proof mismatch')
r.capture(r.windows['native']+'.0','final-native',False)
r.save('pending-input-result.json',{'at':now(),'status':'PASS_INPUT_ACK_WITH_MANUAL_VIEW_REOPEN','binary_sha256':sha(a.binary),'task_count':len(ids),'steer_count':len(supplements),'steered_run_count':len(d['run_attempts']),'receipts':'ACK_A\nACK_B\nACK_C\n','fresh':'FRESH_OK','fresh_task_status':fresh['task']['status'],'fresh_run_status':fresh['run_attempts'][0]['status'],'task_uncertainty':'business_effect_unverified retained; fixture independently checks requested file','manual_same_thread_view_reopen':True,'automatic_post_cancel_reconnect':'FAILED_CANCEL_FALLBACK_REPLACED_ENDPOINT','production_touched':False})
r.cleanup();print('PASS_INPUT_ACK_WITH_MANUAL_VIEW_REOPEN')
