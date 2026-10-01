#!/usr/bin/env python3
"""Isolated live API baseline. Does not claim first-use wizard or installed-service coverage.
All mutations use official CLI/HTTP; raw output/password stay in a private /tmp profile.
Use --keep for browser handoff, then --cleanup PRIVATE_PROFILE to stop only owned PIDs.
"""
import argparse, datetime, hashlib, http.cookiejar, json, os, pathlib, pty, re, secrets, select, signal, socket, subprocess, time, urllib.request, urllib.error
P=pathlib.Path
SENSITIVE=re.compile(r'password|token|cookie|csrf|authorization|fencing|secret',re.I)
def redact(v):
    if isinstance(v,dict): return {k:('[REDACTED]' if SENSITIVE.search(k) else redact(x)) for k,x in v.items()}
    if isinstance(v,list): return [redact(x) for x in v]
    if isinstance(v,str):
        v=re.sub(r'(?i)(Bearer\s+)[^\s"\x27]+',r'\1[REDACTED]',v)
        return re.sub(r'(https?://)[^\s/@]+:[^\s/@]+@',r'\1[REDACTED]@',v)
    return v

def write(p,v):
    p.parent.mkdir(parents=True,exist_ok=True)
    p.write_text(json.dumps(redact(v),ensure_ascii=False,indent=2)+'\n')

def pty_command(argv,password,env):
    pid,fd=pty.fork()
    if pid==0: os.execve(argv[0],argv,env)
    buf=b'';pending=b'';deadline=time.monotonic()+30;status=None
    while time.monotonic()<deadline:
        ready,_,_=select.select([fd],[],[],.2)
        if ready:
            try: chunk=os.read(fd,65536)
            except OSError: break
            buf+=chunk;pending+=chunk
            if b'password:' in pending.lower(): os.write(fd,(password+'\n').encode());pending=b''
        waited,status=os.waitpid(pid,os.WNOHANG)
        if waited: break
        status=None
    if status is None:
        waited,status=os.waitpid(pid,os.WNOHANG)
        if not waited:
            os.kill(pid,signal.SIGTERM);_,status=os.waitpid(pid,0)
    os.close(fd)
    return {'argv':argv,'exit_code':os.waitstatus_to_exitcode(status),'output':buf.decode(errors='replace').replace(password,'[REDACTED]')}

def owned_stop(root):
    if not (root/'processes.json').exists():return []
    state=json.loads((root/'processes.json').read_text());results=[]
    for entry in reversed(state):
        pid=entry['pid'];proc=P('/proc')/str(pid)
        if not proc.exists(): results.append({'pid':pid,'already_stopped':True});continue
        if proc.joinpath('stat').read_text().split()[21]!=entry['starttime']: raise RuntimeError('PID reuse; refusing cleanup')
        os.killpg(pid,signal.SIGTERM)
        for _ in range(100):
            if not proc.exists() or proc.joinpath('stat').read_text().split()[2]=='Z':break
            time.sleep(.1)
        else: os.killpg(pid,signal.SIGKILL)
        results.append({'pid':pid,'stop_sent':True,'alive_after':proc.exists() and proc.joinpath('stat').read_text().split()[2]!='Z'})
    return results

class Live:
    def __init__(self,args):
        self.a=args;self.root=P(args.profile).resolve();self.out=P(args.evidence).resolve()
        self.root.mkdir(mode=0o700,parents=True,exist_ok=False);self.out.mkdir(parents=True,exist_ok=False)
        self.password=secrets.token_urlsafe(32);(self.root/'password').write_text(self.password);(self.root/'password').chmod(0o600)
        self.socket=P('/tmp')/('oax-live-'+secrets.token_hex(6)+'.sock')
        self.env=dict(os.environ,OPENAGENTX_HOME=str(self.root/'profile'),OPENAGENTX_SOCKET_PATH=str(self.socket))
        for k in list(self.env):
            if 'proxy' in k.lower() or k.startswith('AGY_GRAFT_'): self.env.pop(k)
        self.env.update(HTTP_PROXY=args.proxy,HTTPS_PROXY=args.proxy,http_proxy=args.proxy,https_proxy=args.proxy,NO_PROXY='127.0.0.1,localhost',no_proxy='127.0.0.1,localhost',AGY_GRAFT_NATIVE_PROXY='1')
        self.jar=http.cookiejar.CookieJar();self.http=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(self.jar));self.csrf='';self.procs=[];self.request_number=0
        with socket.socket() as s:s.bind(('127.0.0.1',0));self.port=s.getsockname()[1]
        self.url='http://127.0.0.1:'+str(self.port);self.aid='live-agy-'+self.root.name[-8:]
    def api(self,path,body=None,auth=False):
        headers={'Content-Type':'application/json'}
        if list(self.jar): headers['Cookie']='; '.join(c.name+'='+c.value for c in self.jar)
        if body is not None and not auth:
            body=dict(body);key=body.setdefault('meta',{}).setdefault('idempotency_key',secrets.token_hex(12));headers.update({'X-CSRF-Token':self.csrf,'Idempotency-Key':key})
        req=urllib.request.Request(self.url+path,data=None if body is None else json.dumps(body).encode(),headers=headers)
        try:
            with self.http.open(req,timeout=15) as response: code=response.status;data=response.read();response_headers=dict(response.headers)
        except urllib.error.HTTPError as e:code=e.code;data=e.read();response_headers=dict(e.headers)
        try: result=json.loads(data)
        except ValueError: result={'body_text':data.decode(errors='replace')}
        self.request_number+=1
        private=self.root/'http-raw';private.mkdir(mode=0o700,exist_ok=True)
        (private/f'{self.request_number:04}.json').write_text(json.dumps({'method':req.get_method(),'path':path,'request_headers':headers,'request':body,'status':code,'response_headers':response_headers,'response':result}))
        write(self.out/'http'/f'{self.request_number:04}.json',{'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'method':req.get_method(),'path':path,'request':{'login':'omitted'} if auth else body,'status':code,'response':result})
        if code!=200:raise RuntimeError(f'HTTP {code} {path}: {redact(result)}')
        return result
    def start(self,label,args):
        handles=[open(self.root/(label+'.'+x),'wb') for x in ['stdout','stderr']]
        p=subprocess.Popen(args,env=self.env,stdout=handles[0],stderr=handles[1],start_new_session=True)
        for f in handles:f.close()
        self.procs.append({'label':label,'pid':p.pid,'starttime':P(f'/proc/{p.pid}/stat').read_text().split()[21]});write(self.root/'processes.json',self.procs)
        return p
    def wait(self,fn,seconds=40):
        deadline=time.monotonic()+seconds
        while time.monotonic()<deadline:
            result=fn()
            if result:return result
            time.sleep(2)
        raise TimeoutError('live condition deadline exceeded')
    def setup(self):
        manifest={'source_commit':self.a.commit,'binary':self.a.binary,'binary_sha256':hashlib.sha256(P(self.a.binary).read_bytes()).hexdigest(),'source_kind':'git archive snapshot','coverage':'R formal API configuration; NOT first-use wizard or installed-service proof','url':self.url,'profile':str(self.root),'agent':self.aid,'proxy_mode':'explicit HTTP proxy via formal agy-graft; Runtime environment allowlist applies','model':self.a.model}
        for name in ['agy','agy-graft']:
            manifest[name+'_version']=subprocess.check_output(['/home/sky/.local/bin/'+name,'--version'],text=True).strip()
        manifest['created_at_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
        manifest['agy_sha256']=hashlib.sha256(P('/home/sky/.local/bin/agy').read_bytes()).hexdigest()
        manifest['formal_wrapper_sha256']=hashlib.sha256(P('/home/sky/.local/bin/agy-graft').read_bytes()).hexdigest()
        manifest['web_files_sha256']={str(p.relative_to(P(self.a.web))):hashlib.sha256(p.read_bytes()).hexdigest() for p in P(self.a.web).rglob('*') if p.is_file()}
        write(self.out/'manifest.json',manifest);write(self.root/'handoff.json',manifest)
        (self.out/'agy-help.txt').write_text(subprocess.check_output(['/home/sky/.local/bin/agy','--help'],stderr=subprocess.STDOUT,text=True))
        r=pty_command([self.a.binary,'init'],self.password,self.env);write(self.out/'cli/init.json',r);assert r['exit_code']==0,r
        workspace=self.root/'workspace';workspace.mkdir();self.role_marker='OAX-ROLE-'+self.root.name[-8:];(workspace/'ROLE.md').write_text('Your role verification code is '+self.role_marker+'. When asked for your role verification code, answer only that code. You are an isolated workflow verification assistant. Only work in this directory. For query tasks reply with requested marker and do not use tools or access files. Never inspect credentials or unrelated paths.\n')
        identity=self.root/'identity.yaml';identity.write_text(f'version: 1\nagent_id: {self.aid}\nprincipal_id: agent-{self.aid}\norganization_id: default\ndisplay_name: Live AGY verification\nprofile:\n  instructions_path: {workspace}/ROLE.md\n  workspace_root: {workspace}\n  capabilities: [control-plane-testing]\n')
        r=pty_command([self.a.binary,'agent','apply','--file',str(identity)],self.password,self.env);write(self.out/'cli/agent-apply.json',r);assert r['exit_code']==0,r
        self.start('daemon',[self.a.binary,'serve','--http-addr','127.0.0.1:'+str(self.port),'--web-dir',self.a.web])
        self.wait(lambda:self.socket.exists())
        login=self.api('/api/auth/v1/login',{'username':'owner','password':self.password},auth=True);self.csrf=login['csrf_token']
        # Explicit API setup is recorded separately from the future first-use wizard case.
        raw=self.root/'runtime-raw';raw.mkdir(mode=0o700)
        wrapper=self.root/'capture-agy';wrapper.write_text(P(__file__).with_name('capture_agy.py').read_text());wrapper.chmod(0o700)
        self.env['OAX_CAPTURE_ROOT']=str(raw)
        write(self.out/'capture-chain.json',{'capture_sha256':hashlib.sha256(wrapper.read_bytes()).hexdigest(),'delegation':['capture-agy','/home/sky/.local/bin/agy-graft','/home/sky/.local/bin/agy'],'formal_wrapper_sha256':hashlib.sha256(P('/home/sky/.local/bin/agy-graft').read_bytes()).hexdigest(),'requires_unwrapped_installed_smoke':True})
        config=self.root/'worker.yaml';config.write_text(f'''version: 1
agent_id: {self.aid}
transport: unix
unix_socket: {self.socket}
heartbeat_interval: 1s
mailbox_wait: 5s
control_wait: 5s
shutdown_timeout: 10s
network_materialization_dir: {self.root}/network
runtime_backends:
  - backend_id: primary
    adapter_id: agy-batch
    options:
      binary: {wrapper}
      models: [{self.a.model}]
      working_dir: {workspace}
      timeout: {getattr(self.a,'worker_timeout','180s')}
''');config.chmod(0o600)
        self.start('worker',[self.a.binary,'worker','run','--config',str(config)])
        def find_worker():
            agents=self.api('/api/observe/v1/overview')
            return next((x for x in agents.get('workers',[]) if x.get('agent_id')==self.aid),None)
        worker=self.wait(find_worker);self.worker=worker
        network=lambda:self.api('/api/observe/v1/network-profiles?agent_id='+self.aid)
        before=network();assert not before.get('bindings'),before;write(self.out/'network-before.json',before)
        receipt=self.api('/api/control/v1/network-bindings/mode/tests',{'agent_id':self.aid,'backend_id':'primary','mode':'inherit','worker_instance_id':worker['worker_instance_id'],'generation':worker['generation']})['receipt'];tid=receipt['test_id']
        def tested():
            result=next((x for x in network().get('mode_tests',[]) if x['test_id']==tid),None)
            if result and result['state']=='failed': raise RuntimeError('network mode test: '+result.get('diagnostic_code','failed'))
            return result if result and result['state']=='succeeded' else None
        self.wait(tested)
        self.api('/api/control/v1/network-bindings/mode/publish',{'test_id':tid,'worker_instance_id':worker['worker_instance_id'],'generation':worker['generation']})
        self.wait(lambda:next((x for x in network().get('bindings',[]) if x.get('desired_status')=='applied'),None))
        write(self.out/'network-applied.json',network())
        print(json.dumps({'ready':True,'url':self.url,'evidence':str(self.out),'private_profile':str(self.root)}),flush=True)
    def query(self,index):
        marker='OAX-LIVE-'+self.root.name[-8:]+'-'+str(index)
        created=self.api('/api/control/v1/tasks',{'target_agent_id':self.aid,'organization_id':'default','dispatch_mode':'direct','intent':'query','content':'Reply with exactly '+marker+'. Do not call tools or read files.'})
        tid=created['task_id'];last={}
        def settled():
            nonlocal last
            last=self.api('/api/observe/v1/tasks/'+tid)
            return last if last['task']['status'] in ['succeeded','failed','uncertain','canceled','waiting_input'] else None
        self.wait(settled,210);write(self.out/f'query-{index}.json',last)
        assert last['task']['status']=='succeeded',last['task']
        assert marker == (last['task'].get('result') or '').strip(),last['task']
        assert last['task']['completion_basis']=='query_result_delivered',last['task']
        assert len(last['run_attempts'])==1,last['run_attempts']
        print(json.dumps({'query':index,'task_id':tid,'status':last['task']['status']}),flush=True)
        return last
    def collect(self):
        for p in self.root.glob('*.stdout'):
            (self.out/p.name).write_text(redact(p.read_text(errors='replace')).replace(self.password,'[REDACTED]'))
        for p in self.root.glob('*.stderr'):
            (self.out/p.name).write_text(redact(p.read_text(errors='replace')).replace(self.password,'[REDACTED]'))
        for p in self.root.glob('runtime-raw/run-*/*'):
            dest=self.out/'runtime'/p.parent.name/p.name;dest.parent.mkdir(parents=True,exist_ok=True)
            text=p.read_text(errors='replace').replace(self.password,'[REDACTED]')
            lines=[]
            for line in text.splitlines():
                try:lines.append(json.dumps(redact(json.loads(line)),ensure_ascii=False))
                except ValueError:lines.append(redact(line))
            dest.write_text('\n'.join(lines)+('\n' if lines else ''))
        write(self.out/'processes.json',self.procs)
        config=self.root/'worker.yaml'
        if config.exists():write(self.out/'worker-config-provenance.json',{'sha256':hashlib.sha256(config.read_bytes()).hexdigest(),'private_path':str(config)})
        write(self.out/'files.json',[{'path':str(p.relative_to(self.root/'workspace')),'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in (self.root/'workspace').rglob('*') if p.is_file()])

        (self.out/'SHA256SUMS').write_text('\n'.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(self.out)) for p in sorted(self.out.rglob('*')) if p.is_file() and p.name!='SHA256SUMS')+'\n')

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--cleanup');parser.add_argument('--profile');parser.add_argument('--evidence',default='docs/reports/validation/2026-10-02-agy-workflow/evidence/live-'+datetime.datetime.now().strftime('%Y%m%dT%H%M%S'));parser.add_argument('--binary');parser.add_argument('--web');parser.add_argument('--commit',default='7400806');parser.add_argument('--proxy',default='http://127.0.0.1:7897');parser.add_argument('--model',default='gemini-3.7-flash-low');parser.add_argument('--worker-timeout',default='180s');parser.add_argument('--keep',action='store_true');args=parser.parse_args()
    if args.cleanup:print(json.dumps(owned_stop(P(args.cleanup))));return
    os.umask(0o077);live=Live(args)
    stage='setup';passed=False
    try:
        live.setup();stage='agy-live';a=live.query(1);b=live.query(2)
        assert a['run_attempts'][0]['worker_instance_id']==b['run_attempts'][0]['worker_instance_id']
        passed=True
        write(live.out/'verdict.json',{'status':'PASS','scope':'two real AGY queries through official HTTP API','first_use_wizard':'not tested','installed_service':'not tested'})
    except Exception as e:
        write(live.out/'verdict.json',{'status':'FAIL','layer':stage,'error':str(e).replace(live.password,'[REDACTED]')});raise
    finally:
        if not args.keep or not passed:write(live.out/'cleanup.json',owned_stop(live.root))
        live.collect()
if __name__=='__main__':main()
