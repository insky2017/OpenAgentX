#!/usr/bin/env python3
"""I entry/role-freeze smoke. Run only after owner installs the approved artifact."""
import argparse, datetime, hashlib, http.cookiejar, json, os, pathlib, pty, secrets, select, signal, subprocess, time, urllib.request
from agy_live import write, redact
from agy_workflow import Workflow
from agy_recovery import proc, living
P=pathlib.Path


class Installed(Workflow):
    def __init__(self,a):
        self.a=a;self.root=P(a.profile).resolve();self.out=P(a.evidence).resolve()
        self.root.mkdir(mode=0o700,parents=True,exist_ok=False);self.out.mkdir(parents=True,exist_ok=False)
        self.password=P(a.password_file).read_text().strip();self.url=a.url;self.aid='agy-onboarding-e2e'
        self.env=dict(os.environ)
        for k in list(self.env):
            if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_') or k.startswith('OPENAGENTX_'):self.env.pop(k)
        self.env.update(HTTP_PROXY='http://127.0.0.1:7897',HTTPS_PROXY='http://127.0.0.1:7897',AGY_GRAFT_NATIVE_PROXY='1',NO_PROXY='127.0.0.1,localhost')
        self.jar=http.cookiejar.CookieJar();self.http=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(self.jar));self.csrf='';self.request_number=0
        self.role_marker='OAX-ROLE-OLD-'+secrets.token_hex(5);self.new_marker='OAX-ROLE-NEW-'+secrets.token_hex(5)
        (self.root/'workspace').mkdir();self.role=self.root/'workspace/ROLE.md';self.write_role(self.role_marker)
        self.input_marker='OAX-FILE-'+secrets.token_hex(5);(self.root/'workspace/input.txt').write_text(self.input_marker+'\n')
        self.started_at=datetime.datetime.now(datetime.timezone.utc).isoformat()
        self.procs=[]

    def write_role(self,marker):
        self.role.write_text('Your role verification code is '+marker+'. Only work in the assigned workspace. When asked for your role code, use the code supplied in these instructions, do not read ROLE.md. Run only explicitly requested scripts, once. Never inspect credentials or unrelated files.\n')

    def cli(self,label,args,responses=(),duration=330,interrupt_after=None):
        master,slave=pty.openpty();command=[self.a.binary,*args]
        process=subprocess.Popen(command,stdin=slave,stdout=slave,stderr=slave,env=self.env,start_new_session=True);os.close(slave)
        start=time.monotonic();data=b'';pending=list(responses);interrupted=False
        try:
            while time.monotonic()-start<duration:
                if interrupt_after and not interrupted and time.monotonic()-start>=interrupt_after:process.send_signal(signal.SIGINT);interrupted=True
                if select.select([master],[],[],.2)[0]:
                    try:chunk=os.read(master,65536)
                    except OSError:break
                    data+=chunk
                    if pending and pending[0][0].encode() in data:
                        _,value=pending.pop(0);os.write(master,(value+'\n').encode())
                if process.poll() is not None:break
            if process.poll() is None:
                try:process.wait(timeout=3)
                except subprocess.TimeoutExpired:process.terminate();process.wait(timeout=5)
        finally:os.close(master)
        text=data.decode(errors='replace').replace(self.password,'[REDACTED]')
        (self.root/(label+'.pty.log')).write_text(text);(self.out/(label+'.pty.log')).write_text(redact(text))
        write(self.out/(label+'.command.json'),{'argv':command,'exit_code':process.returncode,'duration_seconds':time.monotonic()-start,'prompts_remaining':len(pending)})
        assert process.returncode==0 and not pending,(label,process.returncode,text)
        return text

    def state(self,label):
        overview=self.api('/api/observe/v1/overview');network=self.api('/api/observe/v1/network-profiles?agent_id='+self.aid)
        workers=[w for w in overview['workers'] if w['agent_id']==self.aid]
        write(self.out/(label+'.json'),{'overview':overview,'network':network})
        return max(workers,key=lambda w:w['generation']) if workers else None

    def run(self):
        login=self.api('/api/auth/v1/login',{'username':'owner','password':self.password},auth=True);self.csrf=login['csrf_token']
        before=self.api('/api/observe/v1/overview');assert not any(a['agent_id']==self.aid for a in before['agents']),'test Agent already exists; refusing conflict'
        base=['agent','add','--id',self.aid,'--password-file',self.a.password_file,'--model','gemini-3.7-flash-low','--runtime-binary','/home/sky/.local/bin/agy-graft','--no-open','--wait','5m']
        self.cli('01-wizard',base,[('Agent 名称','AGY 入口验收'),('工作目录',str(self.root/'workspace')),('职责文档路径或职责描述',str(self.role))])
        first=self.state('02-ready');assert first['status']=='online',first
        self.cli('03-idempotent-add',base+['--name','AGY 入口验收','--workspace',str(self.root/'workspace'),'--role',str(self.role)])
        same=self.state('04-idempotent-state');assert same['worker_instance_id']==first['worker_instance_id'],same
        self.cli('05-open',['agent','open',self.aid,'--no-open'])
        self.cli('06-watch',['agent','status',self.aid,'--watch'],interrupt_after=7,duration=15)
        tid=self.task('Read input.txt in your assigned workspace. Reply with exactly your role verification code, then a vertical bar, then the file contents with no trailing newline. Do not read ROLE.md.','query')
        d=self.settle(tid,'role-and-file');assert d['task']['status']=='succeeded' and d['task']['result'].strip()==self.role_marker+'|'+self.input_marker,d
        self.prepare_wait('role-freeze',25)
        old_hash=hashlib.sha256(self.role.read_bytes()).hexdigest()
        tid=self.task('Run python3 role-freeze.py exactly once and wait for it to finish. Then reply only with the role verification code already supplied in your role instructions. Do not read ROLE.md or any other file.','query')
        self.started('role-freeze',120);frozen=self.detail(tid);write(self.out/'cases/role-freeze/before-update.json',frozen)
        assert frozen['task']['status']=='running' and self.latest_run(frozen)['instructions_sha256']==old_hash
        tid2=self.task('Reply with only your role verification code from your role instructions. Do not read files or use tools.','query')
        queued=self.detail(tid2);write(self.out/'cases/pause-queued/before-pause.json',queued);assert not queued['run_attempts'],queued
        assert self.detail(tid)['task']['status']=='running','A finished before B was queued and pause requested'
        self.write_role(self.new_marker);new_hash=hashlib.sha256(self.role.read_bytes()).hexdigest()
        self.cli('07-pause-active',['agent','pause',self.aid],duration=180)
        d=self.settle(tid,'role-freeze');assert d['task']['status']=='succeeded' and d['task']['result'].strip().splitlines()[-1]==self.role_marker and self.new_marker not in d['task']['result'],d
        assert self.latest_run(d)['instructions_sha256']==old_hash and len(d['run_attempts'])==1
        calls=(self.root/'workspace/role-freeze.invocations.jsonl').read_text().splitlines();assert len(calls)==1
        paused=self.state('08-paused');assert paused['status']=='offline',paused
        queued=self.detail(tid2);write(self.out/'cases/pause-queued/while-paused.json',queued);assert not queued['run_attempts'],queued
        write(self.out/'cases/pause-queued/mailbox-while-paused.json',self.api('/api/observe/v1/mailboxes?agent_id='+self.aid))
        self.cli('09-resume',['agent','resume',self.aid,'--no-open','--wait','5m'])
        resumed=self.state('10-resumed');assert resumed['generation']>first['generation'],resumed
        fresh=self.settle(tid2,'role-next-run');assert fresh['task']['status']=='succeeded' and fresh['task']['result'].strip()==self.new_marker,fresh
        assert len(fresh['run_attempts'])==1 and self.latest_run(fresh)['instructions_sha256']==new_hash
        assert self.latest_run(fresh)['worker_instance_id']==resumed['worker_instance_id'],fresh
        write(self.out/'cases/pause-queued/after-resume.json',fresh)
        unchanged=self.detail(tid);write(self.out/'cases/role-freeze/final-after-resume.json',unchanged);assert len(unchanged['run_attempts'])==1
        write(self.out/'role-freeze-proof.json',{'old_sha256':old_hash,'new_sha256':new_hash,'current_task':tid,'next_task':tid2,'invocations':len(calls),'old_role':self.role_marker,'new_role':self.new_marker,'first_worker':first,'resumed_worker':resumed})
        self.cli('11-final-status',['agent','status',self.aid])

    def systemd_crash(self):
        # This targets only the newly created test Agent's real user service.
        unit='openagentx-worker@'+self.aid+'.service'
        properties=subprocess.check_output(['systemctl','--user','show',unit,'-p','MainPID','-p','KillMode','-p','ControlGroup'],text=True)
        props=dict(line.split('=',1) for line in properties.splitlines() if '=' in line)
        assert props['KillMode']=='control-group',props
        main=proc(int(props['MainPID']));assert main and living(main),props
        daemon=proc(int(subprocess.check_output(['systemctl','--user','show','openagentx.service','-p','MainPID','--value'],text=True).strip()))
        assert daemon and living(daemon),daemon
        prior=self.state('12-before-worker-crash')
        self.prepare_wait('systemd-crash',120)
        tid=self.task('Run python3 systemd-crash.py exactly once and wait for it to finish. Do not retry this script or execute other scripts.')
        started=self.started('systemd-crash',120);write(self.out/'cases/systemd-crash/before-task-run-journal.json',self.detail(tid))
        write(self.out/'cases/systemd-crash/fault.json',{'unit':unit,'properties':props,'main':main,'signal':'SIGKILL','at':time.time(),'tool_delay_seconds':120,'started':started,'daemon':daemon,'process_cleanup_deadline_seconds':25,'task_reconcile_deadline_seconds':75})
        assert living(main)
        os.kill(main['pid'],signal.SIGKILL)
        begin=time.monotonic()
        self.wait(lambda:not living(main) and not any(living(x) for x in started['processes']),25)
        stopped_seconds=time.monotonic()-begin
        write(self.out/'cases/systemd-crash/physical-stop-proof.json',{'old_main_alive':living(main),'tool_processes':[{**x,'alive':living(x)} for x in started['processes']],'elapsed_seconds':stopped_seconds,'deadline_seconds':25})
        def recovered():
            workers=self.api('/api/observe/v1/overview')['workers']
            candidates=[w for w in workers if w['agent_id']==self.aid and w['generation']>prior['generation'] and w['status']=='online']
            return max(candidates,key=lambda w:w['generation']) if candidates else None
        new=self.wait(recovered,max(1,75-(time.monotonic()-begin)))
        write(self.out/'cases/systemd-crash/systemd-restart-proof.json',{'old_main_alive':living(main),'tool_processes':[{**x,'alive':living(x)} for x in started['processes']],'new_worker':new,'elapsed_seconds':time.monotonic()-begin,'tool_late_effect_exists':(self.root/'workspace/systemd-crash.late.txt').exists()})
        reconciled=self.settle(tid,'systemd-crash',timeout=max(1,75-(time.monotonic()-begin)))
        reconcile_seconds=time.monotonic()-begin
        assert reconciled['task']['status']=='uncertain' and self.latest_run(reconciled)['status']=='uncertain',reconciled
        daemon_after=proc(int(subprocess.check_output(['systemctl','--user','show','openagentx.service','-p','MainPID','--value'],text=True).strip()))
        write(self.out/'cases/systemd-crash/automatic-reconcile-proof.json',{'seconds':reconcile_seconds,'deadline_seconds':75,'daemon_before':daemon,'daemon_after':daemon_after,'task_status':reconciled['task']['status'],'run_status':self.latest_run(reconciled)['status']})
        assert reconcile_seconds<=75 and daemon_after and daemon_after['pid']==daemon['pid'] and daemon_after['starttime']==daemon['starttime'] and living(daemon),daemon_after
        self.cli('13-resume-after-worker-crash',['agent','resume',self.aid,'--no-open','--wait','5m'])
        self.state('14-crash-generation-ready');self.query('after-systemd-crash')
        final=self.detail(tid);write(self.out/'cases/systemd-crash/final-task-run-journal.json',final)
        calls=(self.root/'workspace/systemd-crash.invocations.jsonl').read_text().splitlines()
        proof={'task_id':tid,'task_status':final['task']['status'],'run_statuses':[r['status'] for r in final['run_attempts']],'run_count':len(final['run_attempts']),'invocations':len(calls),'no_daemon_restart':living(daemon),'reconcile_seconds':reconcile_seconds,'new_generation':new['generation'],'old_children_stopped':not any(living(x) for x in started['processes'])}
        write(self.out/'cases/systemd-crash/independent-proof.json',proof)
        assert len(calls)==1 and len(final['run_attempts'])==1 and final['task']['status']=='uncertain' and living(daemon),proof
        return proof

    def collect_installed(self):
        unit='openagentx-worker@'+self.aid+'.service'
        for label,cmd in [('worker-service',['systemctl','--user','show',unit,'-p','MainPID','-p','ActiveState','-p','SubState','-p','ExecStart','-p','FragmentPath']),('worker-journal',['journalctl','--user','-u',unit,'--since',self.started_at,'--no-pager']),('daemon-journal',['journalctl','--user','-u','openagentx.service','--since',self.started_at,'--no-pager'])]:
            p=subprocess.run(cmd,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True);text=p.stdout.replace(self.password,'[REDACTED]');(self.root/(label+'.log')).write_text(text);(self.out/(label+'.log')).write_text(redact(text))
        write(self.out/'files.json',[{'path':str(p.relative_to(self.root/'workspace')),'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in (self.root/'workspace').rglob('*') if p.is_file()])
        (self.out/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(self.out))+'\n' for p in sorted(self.out.rglob('*')) if p.is_file() and p.name!='SHA256SUMS'))


def main():
    p=argparse.ArgumentParser()
    for n in ['profile','evidence','binary','url','password-file','commit']:p.add_argument('--'+n,required=True)
    p.add_argument('--systemd-crash',action='store_true')
    a=p.parse_args();os.umask(0o077);live=Installed(a)
    write(live.out/'manifest.json',{'candidate':a.commit,'binary':a.binary,'binary_sha256':hashlib.sha256(P(a.binary).read_bytes()).hexdigest(),'script_sha256':hashlib.sha256(P(__file__).read_bytes()).hexdigest(),'at':live.started_at,'coverage':'I actual user-systemd, formal unwrapped agy-graft, PTY onboarding, roles, pause/resume','profile':str(live.root),'url':a.url})
    try:
        live.run()
        crash=live.systemd_crash() if a.systemd_crash else None
        status='PARTIAL' if crash and crash['task_status'] not in ['uncertain','failed','canceled'] else 'PASS'
        write(live.out/'verdict.json',{'status':status,'entry_role_freeze_pause_resume':'PASS','scope':'I E01/E02 role freeze/E03/E04/E11 queued pause/resume/E13; optional crash only on new test Agent user service','systemd_crash':crash})
    except Exception as error:
        write(live.out/'verdict.json',{'status':'FAIL','error':str(error).replace(live.password,'[REDACTED]')});raise
    finally:live.collect_installed()

if __name__=='__main__':main()
