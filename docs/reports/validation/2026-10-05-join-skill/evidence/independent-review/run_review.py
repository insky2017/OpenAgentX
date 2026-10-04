import os, json, pathlib, subprocess, hashlib, datetime, copy, stat
os.umask(0o077)
root=pathlib.Path('/home/sky/.local/state/openagentx/validation/2026-10-05-join-skill/independent-review')
repo=pathlib.Path('/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX')
install=repo/'scripts/oax-skill.py'; prepare=repo/'skills/openagentx-join/scripts/prepare.py'
project=root/'project'; project.mkdir(exist_ok=True)
profile=root/'profile'; workspace=root/'business'; workspace.mkdir(exist_ok=True)
env=os.environ.copy()
for k in list(env):
    if k.startswith('OPENAGENTX_'): del env[k]
for k in ('CODEX_THREAD_ID','CODEX_SESSION_ID'): env.pop(k,None)
test_thread='1a5ea370-7788-4000-a555-111111111111'
current=env|{'CODEX_THREAD_ID':test_thread,'CODEX_SESSION_ID':test_thread}
records=[]; checks=[]
def save(name,obj): (root/name).write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n')
def run(name,argv,childenv=env):
    start=datetime.datetime.now(datetime.timezone.utc).isoformat()
    p=subprocess.run([str(x) for x in argv],env=childenv,cwd=project,capture_output=True,text=True,timeout=70)
    record={'name':name,'argv':[str(x) for x in argv],'start':start,'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
    save(name+'.json',record); records.append(name+'.json'); print(name,p.returncode,flush=True)
    return p

def check(label,value):
    checks.append({'check':label,'pass':bool(value)})
    print(label, bool(value),flush=True)

def hashes(path):
    return {str(x.relative_to(path)):{'sha256':hashlib.sha256(x.read_bytes()).hexdigest(),'mode':oct(stat.S_IMODE(x.stat().st_mode))} for x in path.rglob('*') if x.is_file()}
run('git-init',['git','init','--quiet',project])
run('codex-version',['codex','--version'])
run('oax-build',['go','version','-m','/home/sky/.local/bin/openagentx'])
run('oax-contract',['openagentx','agent','join','--help'])
save('sources.json',{str(p.relative_to(repo)):hashlib.sha256(p.read_bytes()).hexdigest() for p in (install,prepare,repo/'skills/openagentx-join/SKILL.md')})
save('binary.json',{'openagentx_sha256':hashlib.sha256(pathlib.Path('/home/sky/.local/bin/openagentx').read_bytes()).hexdigest(),'codex_sha256':hashlib.sha256(pathlib.Path('/home/sky/.local/bin/codex').resolve().read_bytes()).hexdigest()})
p=run('project-install',['python3',install,'install','--scope','project','--project',project]); check('project linked',p.returncode==0)
link=project/'.agents/skills/openagentx-join'; before=(os.readlink(link),link.lstat().st_mtime_ns)
p=run('project-install-replay',['python3',install,'install','--scope','project','--project',project]); check('install replay unchanged',p.returncode==0 and before==(os.readlink(link),link.lstat().st_mtime_ns))
p=run('project-check',['python3',install,'check','--project',project]); data=json.loads(p.stdout); check('real skills/list visible enabled',p.returncode==0 and data['discovery']['visible'] and data['discovery']['enabled'])
conflict=root/'conflict'; conflict.mkdir(); entry=conflict/'.agents/skills/openagentx-join'; entry.mkdir(parents=True); (entry/'sentinel').write_text('preserve')
p=run('install-conflict',['python3',install,'install','--scope','project','--project',conflict]); check('install conflict preserved',p.returncode!=0 and (entry/'sentinel').read_text()=='preserve')
broken=root/'broken'; broken.mkdir(); entry2=broken/'.agents/skills/openagentx-join'; entry2.parent.mkdir(parents=True); entry2.symlink_to(root/'missing-source')
p=run('install-dangling',['python3',install,'install','--scope','project','--project',broken]); check('dangling link preserved',p.returncode!=0 and entry2.is_symlink())
p=run('check-conflict',['python3',install,'check','--project',conflict]); check('conflict check nonzero',p.returncode!=0)
p=run('check-no-binary',['python3',install,'check','--project',project,'--codex-binary',root/'no-binary']); check('discovery failure not success',p.returncode!=0 and not json.loads(p.stdout)['ok'])
brief={'agent_id':'review-join','name':'审阅接入','workspace':str(workspace),'role':'仅审阅隔离资料，不发信、不执行支付。','peers':['peer-one'],'handoff':{'completed':['已核对隔离目录。'],'next':['等待用户后续安排。'],'uncertain':['没有验证运行态；未产生业务副作用。']}}
brief_file=root/'brief.json'; save('brief.json',brief)
base=['python3',prepare,'--brief',brief_file,'--profile-home',profile]
p=run('inspect-readonly',base+['--inspect'],current); check('inspect writes no profile',p.returncode==0 and not profile.exists())
p=run('prepare-current',base,current); check('real CLI current preparation',p.returncode==0)
response=json.loads(p.stdout); initial=hashes(profile); save('profile-before-replay.json',initial)
check('correct capture',response['thread']['thread_id']==test_thread)
receipt=json.loads((profile/'workers/joins/review-join.json').read_text())
check('receipt identity workspace runtime thread',receipt['agent_id']==brief['agent_id'] and receipt['workspace']==str(workspace) and receipt['runtime']=='codex' and receipt['thread_id']==test_thread and receipt['ready'] is False)
check('no db socket credentials',all(not (profile/s).exists() for s in ('data/openagentx.db','run/openagentx.sock','credentials.json')))
check('business workspace unchanged',not list(workspace.iterdir()))
check('fleet disabled','enabled: false' in (profile/'fleet.yaml').read_text())
p=run('profile-status',response['next_commands']['status']['argv'],env); status=json.loads(p.stdout); check('status same prepared profile',p.returncode==0 and status['state']=='local_prepared' and status['ready'] is False)
p=run('prepare-replay',base,current); check('same input replay unchanged',p.returncode==0 and initial==hashes(profile))
changed=copy.deepcopy(brief); changed['role']='changed'; save('changed.json',changed)
p=run('changed-refused',['python3',prepare,'--brief',root/'changed.json','--profile-home',profile],current); check('changed input preserves files',p.returncode!=0 and initial==hashes(profile))
p=run('thread-conflict',base,current|{'CODEX_SESSION_ID':'2a5ea370-7788-4000-a555-111111111111'}); check('thread conflict closed',p.returncode!=0 and initial==hashes(profile))
p=run('thread-missing',['python3',prepare,'--brief',brief_file,'--profile-home',root/'missing-profile'],env); check('missing thread no write',p.returncode!=0 and not (root/'missing-profile').exists())
summary=copy.deepcopy(brief); summary['agent_id']='review-summary'; summary['history']='summary'; save('summary.json',summary)
p=run('prepare-summary',['python3',prepare,'--brief',root/'summary.json','--profile-home',root/'summary-profile'],current); check('summary explicit no thread',p.returncode==0 and json.loads(p.stdout)['thread']['thread_id'] is None)
inside=workspace/'profile'
p=run('workspace-output-refused',['python3',prepare,'--brief',brief_file,'--profile-home',inside],current); check('workspace output refuses writes',p.returncode!=0 and not inside.exists())
missing=copy.deepcopy(brief); del missing['peers']; save('missing-peer.json',missing)
p=run('missing-peers',['python3',prepare,'--brief',root/'missing-peer.json','--profile-home',root/'missing-peers-profile'],current); check('fourth field mandatory',p.returncode!=0 and not (root/'missing-peers-profile').exists())
save('observations.json',{'scope':'真实 CLI 离线集成；一致 UUID 为隔离分支测试，不证明当前模型 thread 自动捕获。未更改 HOME/CODEX_HOME；不执行 resume/open、不发信。','checks':checks,'commands':records,'profile_hashes':hashes(profile)})
