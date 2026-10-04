import os,json,pathlib,subprocess,hashlib,datetime,stat
os.umask(0o077)
root=pathlib.Path('/home/sky/.local/state/openagentx/validation/2026-10-05-join-skill/independent-review'); repo=pathlib.Path('/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX'); helper=repo/'skills/openagentx-join/scripts/prepare.py'
env=os.environ.copy()
for k in list(env):
    if k.startswith('OPENAGENTX_') or k in ('CODEX_THREAD_ID','CODEX_SESSION_ID'): del env[k]
checks=[]; records=[]
def save(n,v): (root/n).write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
def run(n,args,e=env):
    p=subprocess.run([str(x) for x in args],env=e,cwd=repo,capture_output=True,text=True,timeout=70)
    save(n+'.json',{'argv':[str(x) for x in args],'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}); records.append(n+'.json'); return p

def check(n,b): checks.append({'check':n,'pass':bool(b)}); print(n,bool(b))
def hashes(p): return {str(x.relative_to(p)):hashlib.sha256(x.read_bytes()).hexdigest() for x in p.rglob('*') if x.is_file()}
brief=json.loads((root/'brief.json').read_text()); brief['history']='summary'; brief['agent_id']='review-env'; save('env-brief.json',brief)
resource_env=env|{'OPENAGENTX_HOME':str(root/'env-profile'),'OPENAGENTX_WORKER_CONFIG_DIR':str(root/'env-worker'),'OPENAGENTX_AGENT_ID':'source-agent','OPENAGENTX_ORGANIZATION_ID':'source-org'}
args=['python3',helper,'--brief',root/'env-brief.json','--db',root/'explicit-db/openagentx.db']
p=run('env-profile-prepare',args,resource_env); check('env and explicit actual prepare',p.returncode==0); result=json.loads(p.stdout)
source=json.loads((pathlib.Path(result['intake_dir'])/'source.json').read_text()); check('precedence retained',source['profile']['worker_dir']==str(root/'env-worker') and source['profile']['db']==str(root/'explicit-db/openagentx.db') and source['profile']['file']==str(root/'env-profile/fleet.yaml'))
identity=(root/'env-worker/identities/review-env.yaml').read_text(); check('target identity independent of source env','agent_id: review-env\n' in identity and 'principal_id: agent-review-env\n' in identity and 'organization_id: default\n' in identity)
p=run('env-profile-status-in-other-context',result['next_commands']['status']['argv'],env|{'OPENAGENTX_HOME':str(root/'wrong-profile')}); check('next status pins correct profile',p.returncode==0 and json.loads(p.stdout)['agent_id']=='review-env' and not (root/'wrong-profile').exists())
p=run('profile-home-priority-inspect',args+['--profile-home',root/'home-priority','--inspect'],resource_env); inspected=json.loads(p.stdout); check('profile-home overrides inherited resource env',inspected['profile']['worker_dir']==str(root/'home-priority/workers') and inspected['profile']['db']==str(root/'explicit-db/openagentx.db') and not (root/'home-priority').exists())
conflict=root/'cli-conflict-profile'; role=root/'existing-role.md'; role.write_text('original role - preserve\n')
flags=['--worker-dir',conflict/'workers','--file',conflict/'fleet.yaml','--db',conflict/'data/db','--socket',conflict/'run/socket','--credentials',conflict/'credentials.json']
p=run('cli-existing-prepare',['openagentx','agent','join',*flags,'--id','review-existing','--name','Existing','--workspace',root/'business','--role',role,'--prepare','--json']); check('existing identity setup via real CLI',p.returncode==0); before=hashes(conflict)
brief['agent_id']='review-existing'; brief['name']='Existing'; save('cli-conflict-brief.json',brief)
p=run('cli-existing-conflict',['python3',helper,'--brief',root/'cli-conflict-brief.json',*flags]); after=hashes(conflict); check('CLI conflict nonzero preserves prior identity',p.returncode!=0 and all(after.get(k)==v for k,v in before.items()))
err=json.loads(p.stderr); check('CLI failure not ready',err['state']=='not_prepared' and err['ready'] is False)
failures=list((conflict/'workers/joins/review-existing.intake').glob('failure-*.json')); check('failure diagnostics persisted',len(failures)==1 and json.loads(failures[0].read_text())['returncode']!=0)
save('additional-observations.json',{'checks':checks,'commands':records,'existing_before':before,'existing_after':after})
