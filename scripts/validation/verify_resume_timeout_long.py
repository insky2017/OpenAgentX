#!/usr/bin/env python3
"""Read-only re-evaluation of one existing long Task; never starts/retries work."""
import argparse, datetime, json, os, sqlite3, sys
from pathlib import Path
sys.dont_write_bytecode=True
from native_model_settings_e2e import SettingsRun, require, now, sha

def verify(args):
    run=SettingsRun.restore(args)
    tid=json.loads((run.out/'task-created.json').read_text())['task_id']
    d=run.api('/api/observe/v1/tasks/'+tid)
    require(len(d['run_attempts'])==1,'must retain exactly one original Run')
    attempt=d['run_attempts'][0]
    require(attempt['status']=='succeeded','original Runtime has not succeeded')
    elapsed=(datetime.datetime.fromisoformat(attempt['finished_at'].replace('Z','+00:00'))-datetime.datetime.fromisoformat(attempt['started_at'].replace('Z','+00:00'))).total_seconds()
    require(elapsed>=1860,'formal Run did not cross the old limit')
    original=json.loads((run.out/'proof-tool.json').read_text())
    require(sha(run.root/'workspace/long-proof.py')==original['sha256'],'tool source changed')
    start=json.loads((run.root/'workspace/proof-start.json').read_text())
    end=json.loads((run.root/'workspace/proof-end.json').read_text())
    require(start['started_at']==end['started_at'] and end['elapsed_seconds']>=1860,'proof duration differs')
    db=sqlite3.connect('file:'+str(run.root/'profile/data/openagentx.db')+'?mode=ro',uri=True)
    try:
        row=db.execute('SELECT resolved_execution_json FROM run_attempts WHERE run_id=?',(attempt['run_id'],)).fetchone()
        frozen=json.loads(row[0])
    finally: db.close()
    require(frozen['spec']['timeout']==0 and frozen.get('deadline_at','0001-01-01T00:00:00Z')=='0001-01-01T00:00:00Z','frozen deadline was not unlimited')
    console=run.api('/api/console/v1/agents/'+run.aid+'/tasks/'+tid)
    require(not console['latest_run'].get('deadline_at'),'Console projects a deadline')
    run.save('recheck-task.json',d);run.save('recheck-console.json',console)
    run.save('recheck-run.json',run.api('/api/observe/v1/run-attempts/'+attempt['run_id']))
    run.save('proof-start.json',start);run.save('proof-end.json',end)
    run.save('frozen-timeout.json',{'run_id':attempt['run_id'],'timeout':0,'deadline_at':frozen.get('deadline_at'),'duration_seconds':elapsed,'read_only_database':True})
    state=json.loads((run.root/'profile/workers/codex'/run.aid/'state.json').read_text())
    run.save('recheck-engine.json',{k:state.get(k) for k in ('state','thread_id','task_id','run_id','turn_id')})
    result={'status':'PASS','at':now(),'source_commit':json.loads((run.out/'provenance.json').read_text())['commit'],
        'binary_sha256':sha(args.binary),'task_id':tid,'run_id':attempt['run_id'],'task_status':d['task']['status'],
        'runtime_status':attempt['status'],'formal_duration_seconds':elapsed,'proof_elapsed_seconds':end['elapsed_seconds'],
        'frozen_timeout':0,'frozen_deadline':None,'thread_id':state['thread_id'],'model_work_repeated':False,
        'initial_harness_result_preserved':(run.out/'result.json').exists(),
        'scope':'same original Run; real tools and persisted freeze verified; mutation Task review status retained'}
    run.save('recheck-result.json',result);print(json.dumps(result,ensure_ascii=False))
    return result

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--binary',type=Path,required=True);p.add_argument('--root',type=Path,required=True)
    args=p.parse_args();args.phase='native';os.umask(0o077);verify(args)
