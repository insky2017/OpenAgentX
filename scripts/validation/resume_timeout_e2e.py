#!/usr/bin/env python3
"""One isolated real Codex task crossing the old 30 minute execution limit."""
import argparse, datetime, json, os, signal, sqlite3, sys, time
from pathlib import Path
sys.dont_write_bytecode = True
from native_model_settings_e2e import SettingsRun, require, now, sha


def execute(args):
    run = SettingsRun(args)
    result = {'status': 'RUNNING', 'at': now(), 'source_commit': args.commit, 'binary_sha256': sha(args.binary)}
    try:
        run.setup()
        tool = run.root/'workspace/long-proof.py'
        tool.write_text('''import datetime,json,time
from pathlib import Path
p=Path(__file__).resolve().parent
start=time.monotonic()
a={"started_at":datetime.datetime.now(datetime.timezone.utc).isoformat(),"required_seconds":1860}
(p/"proof-start.json").write_text(json.dumps(a))
for i in range(31):
 time.sleep(60)
 print("OAX_LONG_PROGRESS",i+1,flush=True)
a.update(ended_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),elapsed_seconds=time.monotonic()-start)
(p/"proof-end.json").write_text(json.dumps(a))
print("OAX_LONG_COMPLETED",flush=True)
''')
        tool_sha=sha(tool)
        run.save('proof-tool.json', {'path':str(tool),'sha256':tool_sha,'duration_seconds':1860})
        prompt = ('在当前隔离工作目录只执行一次 python3 long-proof.py。该脚本运行31分钟，这是经用户批准的超过旧30分钟截止的真实验收。'
                  '允许此精确本地工具及两个proof文件。等待同一个工具进程正常结束，禁止重复启动、后台脱离、提前回复、子代理或读无关路径。'
                  '工具使用至少2100秒总等待上限，可由会话轮询每60秒等待原进程。完成后读取proof-end.json确认elapsed_seconds>=1860，最终回复OAX_LONG_COMPLETED。')
        receipt = run.api('/api/control/v1/tasks', {'target_agent_id':run.aid,'organization_id':'default','dispatch_mode':'direct','intent':'mutation','content':prompt})
        tid=receipt['task_id'];run.task_ids.append(tid);run.checkpoint();run.save('task-created.json',receipt)
        end=time.monotonic()+2700
        while time.monotonic()<end:
            d=run.api('/api/observe/v1/tasks/'+tid);run.save('latest-task.json',d)
            if d['task']['status'] in {'succeeded','failed','uncertain','canceled'}:break
            time.sleep(60)
        else: raise TimeoutError('isolated acceptance exceeded its 45 minute harness budget')
        require(len(d['run_attempts'])==1,'expected exactly one actual run')
        attempt=d['run_attempts'][0];rd=run.api('/api/observe/v1/run-attempts/'+attempt['run_id']);run.save('final-run.json',rd)
        require(attempt['status']=='succeeded','real Runtime did not succeed')
        elapsed=(datetime.datetime.fromisoformat(attempt['finished_at'].replace('Z','+00:00'))-datetime.datetime.fromisoformat(attempt['started_at'].replace('Z','+00:00'))).total_seconds()
        require(elapsed>=1860,'formal Run duration did not cross old limit')
        require(sha(tool)==tool_sha,'proof tool was modified')
        db=sqlite3.connect('file:'+str(run.root/'profile/data/openagentx.db')+'?mode=ro',uri=True)
        try: frozen=json.loads(db.execute('SELECT resolved_execution_json FROM run_attempts WHERE run_id=?',(attempt['run_id'],)).fetchone()[0])
        finally: db.close()
        require(frozen['spec']['timeout']==0 and frozen.get('deadline_at','0001-01-01T00:00:00Z')=='0001-01-01T00:00:00Z','frozen spec was not unlimited')
        run.save('frozen-timeout.json',{'run_id':attempt['run_id'],'timeout':frozen['spec']['timeout'],'deadline_at':frozen.get('deadline_at'),'duration_seconds':elapsed,'read_only_database':True})
        proof=json.loads((run.root/'workspace/proof-end.json').read_text());run.save('proof-end.json',proof)
        run.save('proof-start.json',json.loads((run.root/'workspace/proof-start.json').read_text()))
        require(proof['elapsed_seconds']>=1860,'did not cross old limit')
        # Console projection is derived from the persisted frozen Run, not local configuration.
        console=run.api('/api/console/v1/agents/'+run.aid+'/tasks/'+tid);run.save('console-task.json',console)
        require(not console['latest_run'].get('deadline_at'),'frozen Run unexpectedly has execution deadline')
        result.update(status='PASS',at=now(),task_id=tid,run_id=attempt['run_id'],task_status=d['task']['status'],
                      runtime_status=attempt['status'],elapsed_seconds=proof['elapsed_seconds'],frozen_deadline=None,
                      business_review='Task uncertain is retained if business_effect_unverified; independent local proof is verified',
                      production_touched=False)
    except Exception as exc:
        result.update(status='FAILED',at=now(),error=str(exc).replace(run.password,'[REDACTED]'))
        raise
    finally:
        run.save('result.json',result)
        # Never silently kill an unresolved execution. Retain owned fixture for review.
        run.checkpoint()
        print(json.dumps(result,ensure_ascii=False),flush=True)

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--binary',type=Path,required=True);p.add_argument('--root',type=Path,required=True);p.add_argument('--commit',required=True)
    a=p.parse_args();a.phase='native';os.umask(0o077);execute(a)
