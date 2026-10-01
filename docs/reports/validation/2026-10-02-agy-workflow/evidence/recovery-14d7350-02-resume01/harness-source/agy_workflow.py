#!/usr/bin/env python3
"""Final candidate real AGY API chain; never substitute for wizard or installed smoke."""
import argparse,datetime,hashlib,json,os,pathlib,secrets,time
from agy_live import Live,owned_stop,write
P=pathlib.Path
TERMINAL={'succeeded','failed','uncertain','canceled','waiting_input'}
def alive(ref):
    try:
        fields=P('/proc',str(ref['pid']),'stat').read_text().split()
        return fields[21]==str(ref['starttime']) and fields[2]!='Z'
    except FileNotFoundError:return False
class Workflow(Live):
    def task(self,content,intent='mutation',**extra):
        return self.api('/api/control/v1/tasks',dict(target_agent_id=self.aid,organization_id='default',dispatch_mode='direct',intent=intent,content=content,**extra))['task_id']
    def detail(self,tid):return self.api('/api/observe/v1/tasks/'+tid)
    def settle(self,tid,label,timeout=210,min_runs=1):
        def done():
            d=self.detail(tid)
            return d if d['task']['status'] in TERMINAL and len(d['run_attempts'])>=min_runs else None
        result=self.wait(done,timeout);write(self.out/'cases'/label/'task-run-journal.json',result);return result
    def exact_file(self,name,expected,label):
        path=self.root/'workspace'/name;data=path.read_bytes()
        proof={'path':name,'actual_bytes':len(data),'expected_bytes':len(expected),'sha256':hashlib.sha256(data).hexdigest(),'expected_sha256':hashlib.sha256(expected).hexdigest(),'exact':data==expected}
        write(self.out/'cases'/label/'independent-file.json',proof);assert data==expected,proof
    def latest_run(self,detail):
        return max(detail['run_attempts'],key=lambda r:(r.get('started_at',''),r.get('created_at',''),r['run_id']))
    def successful_run(self,detail):
        assert self.latest_run(detail)['status']=='succeeded',self.latest_run(detail)
        assert detail['task']['status'] in ['succeeded','uncertain'],detail['task']
    def role(self):
        tid=self.task('What is your role verification code? Reply with only the code from your role instructions. Do not read files or use tools.','query')
        d=self.settle(tid,'role');assert d['task']['status']=='succeeded',d['task'];assert d['task']['result'].strip()==self.role_marker,d['task']
        self.initial_worker=d['run_attempts'][0]['worker_instance_id'];return tid
    def mutation_review_continue(self):
        tid=self.task("Create result.txt in your assigned workspace with exactly these bytes: b'OAX-E2E\\n'. Use a tool to write the file. Do not change any other file. Report after the write.")
        d=self.settle(tid,'mutation');self.successful_run(d);self.exact_file('result.txt',b'OAX-E2E\n','mutation')
        run=self.latest_run(d);body={'meta':{'idempotency_key':'review-'+secrets.token_hex(8),'expected_version':d['task']['version']},'run_id':run['run_id'],'run_version':run['version'],'decision':'accepted','note':'Independent harness verified exact result.txt bytes and SHA-256.'}
        review=self.api('/api/control/v1/tasks/'+tid+'/review',body);repeat=self.api('/api/control/v1/tasks/'+tid+'/review',body);assert repeat==review,(repeat,review)
        fresh=self.detail(tid);write(self.out/'cases/review/task-run-journal.json',fresh);write(self.out/'cases/review/receipt.json',review)
        assert fresh['task']['status']==d['task']['status'],'review must preserve execution uncertainty'
        assert fresh['task']['version']==d['task']['version']+1,'review must be idempotent'
        next_tid=self.task("Continue the previous result. Add one line 'continued' to result.txt, ending in a real newline. The complete file must be exactly b'OAX-E2E\\ncontinued\\n'. Do not duplicate the line.",parent_task_id=tid,continue_context=True)
        continuation=self.settle(next_tid,'continue');self.successful_run(continuation);self.exact_file('result.txt',b'OAX-E2E\ncontinued\n','continue');assert next_tid!=tid
        return [tid,next_tid]
    def prepare_wait(self,label,delay):
        # The application/AGY must execute this independent observable tool script.
        # It does not modify the control DB and cannot report application success.
        ws=self.root/'workspace';script=ws/(label+'.py')
        script.write_text('import json,os,pathlib,subprocess,time\nroot=pathlib.Path(__file__).parent\nwith (root/'+repr(label+'.invocations.jsonl')+').open("a") as calls:calls.write(json.dumps({"pid":os.getpid(),"at":time.time()})+"\\n")\nchild=subprocess.Popen(["sleep",'+repr(str(delay))+'])\ndef ref(pid):return {"pid":pid,"starttime":pathlib.Path("/proc",str(pid),"stat").read_text().split()[21]}\n(root/'+repr(label+'.started.json')+').write_text(json.dumps({"started":time.time(),"processes":[ref(os.getpid()),ref(child.pid)]}))\nchild.wait()\n(root/'+repr(label+'.late.txt')+').write_text("late\\n")\n')
        return script
    def started(self,label,timeout=120):
        file=self.root/'workspace'/(label+'.started.json')
        def ready():
            if not file.exists():return None
            try:d=json.loads(file.read_text())
            except ValueError:return None
            return d if all(alive(x) for x in d['processes']) else None
        d=self.wait(ready,timeout);write(self.out/'cases'/label/'processes-started.json',d);return d
    def queued_supplement(self):
        self.prepare_wait('queued',20)
        tid=self.task('Run python3 queued.py in your assigned workspace and wait for it to finish. Report after completion. Do not run any other script.')
        self.started('queued');d=self.detail(tid);assert d['task']['status']=='running',d['task'];first_run_id=self.latest_run(d)['run_id']
        msg=self.api('/api/control/v1/tasks/'+tid+'/messages',{'meta':{'expected_version':d['task']['version']},'content':"Additional instruction for the next turn: create supplement.txt with exact bytes b'QUEUED-CONSUMED\\n'. Do not run queued.py again; the first run already completed it. Reply with QUEUED-CONSUMED after writing the file."})
        queued=self.api('/api/observe/v1/mailboxes?agent_id='+self.aid);write(self.out/'cases/queued/mailbox-after-message.json',queued);write(self.out/'cases/queued/message.json',msg)
        pending=next(x for x in queued if x.get('message_id')==msg['message_id'])
        assert pending['state']=='pending' and pending['lane']=='work',pending
        self.wait(lambda:(self.root/'workspace/supplement.txt').exists(),240)
        d=self.settle(tid,'queued',timeout=210,min_runs=2);self.successful_run(d);self.exact_file('supplement.txt',b'QUEUED-CONSUMED\n','queued')
        final_mailbox=self.api('/api/observe/v1/mailboxes?agent_id='+self.aid);write(self.out/'cases/queued/mailbox-final.json',final_mailbox)
        accepted=next(x for x in final_mailbox if x.get('message_id')==msg['message_id']);assert accepted['state']=='accepted',accepted
        assert any(x['id']==msg['message_id'] for x in d['messages'])
        latest=self.latest_run(d);assert latest['run_id']!=first_run_id,latest
        assert 'QUEUED-CONSUMED' in latest.get('turn_result',{}).get('body',''),latest
        calls=(self.root/'workspace/queued.invocations.jsonl').read_text().splitlines();assert len(calls)==1,calls
        write(self.out/'cases/queued/consumption-proof.json',{'first_run_id':first_run_id,'supplement_run_id':latest['run_id'],'message_id':msg['message_id'],'mailbox_state':accepted['state'],'first_script_invocation_count':len(calls)})
        return tid
    def runtime_process_tree(self):
        worker_pid=next(p['pid'] for p in self.procs if p['label']=='worker')
        rows={}
        for stat in P('/proc').glob('[0-9]*/stat'):
            try:
                text=stat.read_text();fields=text.rsplit(')',1)[1].split();pid=int(stat.parent.name)
                rows[pid]={'pid':pid,'ppid':int(fields[1]),'starttime':fields[19],'comm':text.split('(',1)[1].rsplit(')',1)[0]}
            except (FileNotFoundError,ProcessLookupError,PermissionError,ValueError,IndexError):continue
        found=[];parents={worker_pid}
        while parents:
            children=[x for x in rows.values() if x['ppid'] in parents];found.extend(children);parents={x['pid'] for x in children}
        return found
    def cancellation(self):
        self.prepare_wait('cancel',30)
        tid=self.task('Run python3 cancel.py in your assigned workspace and wait until it completes. Do not run any other script.')
        started=self.started('cancel');runtime_tree=self.runtime_process_tree()
        write(self.out/'cases/cancel/runtime-process-tree-before.json',runtime_tree)
        assert runtime_tree and any(p['pid']==started['processes'][0]['pid'] for p in runtime_tree),runtime_tree
        queued_tid=self.task("Create queued-canceled.txt containing exactly b'QUEUED-CANCELED\\n'. Only write this file in your assigned workspace.")
        queued_before=self.detail(queued_tid);write(self.out/'cases/queued-cancel/before.json',queued_before)
        assert queued_before['task']['status']=='queued' and not queued_before['run_attempts'],queued_before
        queued_body={'meta':{'idempotency_key':'cancel-queued-'+secrets.token_hex(8),'expected_version':queued_before['task']['version']}}
        queued_response=self.api('/api/control/v1/tasks/'+queued_tid+'/cancel',queued_body)
        queued_replay=self.api('/api/control/v1/tasks/'+queued_tid+'/cancel',queued_body);assert queued_response==queued_replay,(queued_response,queued_replay)
        queued_after=self.detail(queued_tid);write(self.out/'cases/queued-cancel/after.json',queued_after)
        assert queued_after['task']['status']=='canceled' and not queued_after['run_attempts'],queued_after
        mailbox=self.api('/api/observe/v1/mailboxes?agent_id='+self.aid);write(self.out/'cases/queued-cancel/mailboxes.json',mailbox)
        work=[x for x in mailbox if x.get('task_id')==queued_tid and x['kind']=='task'];assert work and all(x['state']=='superseded' for x in work),work
        write(self.out/'cases/queued-cancel/idempotent-receipt.json',queued_response)
        d=self.detail(tid);before=time.monotonic()
        response=self.api('/api/control/v1/tasks/'+tid+'/cancel',{'meta':{'expected_version':d['task']['version']}});write(self.out/'cases/cancel/request.json',response)
        self.wait(lambda:not any(alive(p) for p in started['processes']+runtime_tree),5);elapsed=time.monotonic()-before
        write(self.out/'cases/cancel/process-stop.json',{'stop_seconds':elapsed,'processes':[{**p,'alive':alive(p)} for p in started['processes']],'runtime_tree':[{**p,'alive':alive(p)} for p in runtime_tree]})
        final=self.settle(tid,'cancel',timeout=15)
        remaining=max(0,started['started']+32-time.time())
        while remaining>0:time.sleep(min(remaining,2));remaining=max(0,started['started']+32-time.time())
        late=self.root/'workspace/cancel.late.txt';proof={'stop_seconds':elapsed,'processes':[{**p,'alive':alive(p)} for p in started['processes']],'runtime_tree':[{**p,'alive':alive(p)} for p in runtime_tree],'delayed_effect_exists':late.exists(),'checked_at':time.time(),'original_delay_seconds':30};write(self.out/'cases/cancel/independent-stop-and-effect.json',proof)
        assert elapsed<=5 and not late.exists() and not any(alive(p) for p in started['processes']+runtime_tree),proof
        queued_final=self.detail(queued_tid);queued_effect=self.root/'workspace/queued-canceled.txt'
        write(self.out/'cases/queued-cancel/final-proof.json',{'running_task_id':tid,'queued_task_id':queued_tid,'queued_task_status':queued_final['task']['status'],'run_count':len(queued_final['run_attempts']),'effect_exists':queued_effect.exists(),'checked_after_original_delay':True})
        assert queued_final['task']['status']=='canceled' and not queued_final['run_attempts'] and not queued_effect.exists()
        assert final['task']['status']=='canceled',final['task']
        return {'running_task':tid,'queued_task':queued_tid}
    def timeout_case(self):
        assert self.a.worker_timeout=='35s','timeout case requires independent Worker configured --worker-timeout 35s'
        self.prepare_wait('timeout',60)
        tid=self.task('Run python3 timeout.py in your assigned workspace and wait until it completes. Do not run any other script.')
        started=self.started('timeout',timeout=30);d=self.settle(tid,'timeout',timeout=50)
        self.wait(lambda:not any(alive(p) for p in started['processes']),10)
        assert d['task']['status'] in ['failed','uncertain'],d['task']
        remaining=max(0,started['started']+62-time.time())
        while remaining>0:time.sleep(min(remaining,2));remaining=max(0,started['started']+62-time.time())
        proof={'processes':[{**p,'alive':alive(p)} for p in started['processes']],'delayed_effect_exists':(self.root/'workspace/timeout.late.txt').exists(),'execution_deadline_seconds':35};write(self.out/'cases/timeout/independent-stop-and-effect.json',proof)
        assert not proof['delayed_effect_exists'] and not any(x['alive'] for x in proof['processes']),proof
        return tid
    def next_query(self):
        d=self.query('after-effects');assert d['run_attempts'][0]['worker_instance_id']==self.initial_worker
        return d['task']['id']

def main():
    p=argparse.ArgumentParser();p.add_argument('--profile',required=True);p.add_argument('--evidence',required=True);p.add_argument('--binary',required=True);p.add_argument('--web',required=True);p.add_argument('--commit',required=True);p.add_argument('--proxy',default='http://127.0.0.1:7897');p.add_argument('--model',default='gemini-3.7-flash-low');p.add_argument('--keep',action='store_true');p.add_argument('--no-capture',action='store_true');p.add_argument('--worker-timeout',default='180s');p.add_argument('--cases',default='role,mutation,queued,cancel,next');a=p.parse_args()
    os.umask(0o077);live=Workflow(a);stage='setup';completed={};passed=False
    try:
        live.setup();live.initial_worker=live.worker['worker_instance_id']
        def ready():
            overview=live.api('/api/observe/v1/overview');agent=next(x for x in overview['agents'] if x['agent_id']==live.aid)
            return overview if agent.get('readiness',{}).get('ready') and agent.get('readiness',{}).get('can_start_now') else None
        write(live.out/'readiness.json',live.wait(ready,20))
        for name in a.cases.split(','):
            stage=name;fn={'role':live.role,'mutation':live.mutation_review_continue,'queued':live.queued_supplement,'cancel':live.cancellation,'timeout':live.timeout_case,'next':live.next_query}[name]
            completed[name]=fn();print(json.dumps({'case':name,'status':'PASS','tasks':completed[name]}),flush=True)
        passed=True
        write(live.out/'verdict.json',{'status':'PASS','candidate':a.commit,'completed':completed,'wizard':'not tested','installed':'not tested'})
    except Exception as e:
        write(live.out/'verdict.json',{'status':'FAIL','stage':stage,'completed':completed,'error':str(e).replace(live.password,'[REDACTED]')});raise
    finally:
        if not a.keep or not passed:write(live.out/'cleanup.json',owned_stop(live.root))
        live.collect()
if __name__=='__main__':main()
