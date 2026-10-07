import sys,argparse,json,time
from pathlib import Path
sys.path.insert(0,'scripts/validation')
from native_model_settings_e2e import SettingsRun,AttachedClient,require,now,sha
a=argparse.Namespace(binary=Path('/home/sky/.local/state/openagentx/validation/2026-10-07-pending-input/openagentx'),root=Path('/home/sky/.local/state/openagentx/validation/2026-10-07-pending-input/live02'),phase='native')
r=SettingsRun.restore(a);pane=r.windows['native']+'.0';statepath=r.root/'profile/workers/codex'/r.aid/'state.json';before=json.loads(statepath.read_text());ids=r.task_evidence();require(len(ids)==2,'unexpected task before view reopen')
r.client=AttachedClient(r)
try:
 launcher=r.script('native-reopened',[a.binary,'agent','open',r.aid,'--native'])
 r.tmux('respawn-pane','-k','-t',pane,launcher)
 r.wait(r.lock_held,60);time.sleep(5)
 screen=r.capture(pane,'reopened-native',False)
 require('Reconnecting to server' not in screen,'new view did not attach')
 require(json.loads(statepath.read_text())['thread_id']==before['thread_id'],'view reopen changed thread')
 r.tmux('send-keys','-t',pane,'-l','Execute one local python3 command that writes exactly FRESH_OK to fresh.txt. Do not append to receipts.txt and do not repeat earlier commands. Reply FRESH_OK.')
 time.sleep(.5);r.tmux('send-keys','-t',pane,'Enter')
 r.wait(lambda:(r.root/'workspace/fresh.txt').exists() and json.loads(statepath.read_text())['state']=='idle',120)
 ids=r.task_evidence();require(len(ids)==3,'unexpected redispatch count')
 d=r.api('/api/observe/v1/tasks/'+before['task_id'])
 require(len(d['messages'])==3,'expected three supplemental messages');require(len(d['run_attempts'])==1,'expected one steered Run')
 receipts=(r.root/'workspace/receipts.txt').read_text();require(receipts=='ACK_A\nACK_B\nACK_C\n','old input repeated')
 require((r.root/'workspace/fresh.txt').read_text()=='FRESH_OK','new input proof mismatch')
 r.capture(pane,'final-native',False)
 r.save('pending-input-result.json',{'at':now(),'status':'PASS_INPUT_ACK_WITH_MANUAL_VIEW_REOPEN','binary_sha256':sha(a.binary),'task_count':len(ids),'steer_count':len(d['messages']),'steered_run_count':len(d['run_attempts']),'receipts':receipts,'fresh':'FRESH_OK','same_thread':before['thread_id'],'automatic_post_cancel_reconnect':'FAILED_EXISTING_CANCEL_FALLBACK_REPLACED_ENDPOINT','manual_same_thread_view_reopen':True,'production_touched':False})
 r.client.close();r.client=None;r.cleanup()
 print('PASS_INPUT_ACK_WITH_MANUAL_VIEW_REOPEN')
finally:
 if r.client:r.client.close()
