import datetime,hashlib,http.client,json,os,socket,stat,subprocess,sys,tomllib,urllib.request
from pathlib import Path
from urllib.parse import urlsplit
stage=sys.argv[1]; assert stage in ['before','after']
root=Path('/home/sky/work/touzi/OneAxe/OpenAgentX-workflow-worktree'); profile=Path('/home/sky/.openagentx'); release=Path('/home/sky/.local/state/openagentx/validation/2026-10-02-codex-workflow/release-clone')
raw=Path('/home/sky/.local/state/openagentx/evidence/2026-10-02-codex-workflow/installed-provenance01'); raw.mkdir(parents=True,exist_ok=True);raw.chmod(0o700)
repo=root/'docs/reports/validation/2026-10-02-codex-workflow/evidence/installed-provenance01';repo.mkdir(parents=True,exist_ok=True)
expected='f9b74b7df378ee05a38d3b49dc465c9168ff3ad7c70a49f81430e21b7f6b3366'; commit='6b68eeb810c2b65d629cd8a817bad25a21483d5c'
def sha(path):
 h=hashlib.sha256()
 with open(path,'rb') as f:
  while b:=f.read(1024*1024):h.update(b)
 return h.hexdigest()
def write(folder,name,obj):
 p=folder/name;p.write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n' if not isinstance(obj,str) else obj);p.chmod(0o600 if folder==raw else 0o644)
def run(cmd):
 r=subprocess.run(cmd,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True);return {'argv':cmd,'exit_code':r.returncode,'output':r.stdout}
def ident(pid):
 txt=Path(f'/proc/{pid}/stat').read_text();f=txt[txt.rfind(')')+2:].split();return {'pid':pid,'ppid':int(f[1]),'starttime_ticks':int(f[19])}
def readenv(pid):return dict(x.split('=',1) for x in Path(f'/proc/{pid}/environ').read_bytes().decode().split('\0') if '=' in x)
def endpoint(v):
 u=urlsplit(v);return {'scheme':u.scheme,'host':u.hostname,'port':u.port,'credentials_present':bool(u.username or u.password)}
units={};processes={};envs={};keys=['HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','http_proxy','https_proxy','all_proxy','NO_PROXY','no_proxy','CODEX_HOME']
for label,unit in [('daemon','openagentx.service'),('agy_worker','openagentx-worker@agy-onboarding-e2e.service'),('codex_worker','openagentx-worker@codex-domain-e2e.service')]:
 result=run(['systemctl','--user','show',unit,'--property=Id,MainPID,ExecStart,FragmentPath,EnvironmentFiles,ActiveState,SubState']);assert result['exit_code']==0;units[label]=result;values=dict(x.split('=',1) for x in result['output'].splitlines() if '=' in x);pid=int(values['MainPID']);before=ident(pid);info={**before,'exe':os.readlink(f'/proc/{pid}/exe'),'sha256':sha(f'/proc/{pid}/exe'),'unit':unit,'identity_stable':before==ident(pid)};processes[label]=info
statepath=profile/'workers/codex/codex-domain-e2e/state.json';state=json.loads(statepath.read_text());before=ident(state['pid']);processes['appserver']={**before,'exe':os.readlink(f'/proc/{before["pid"]}/exe'),'sha256':sha(f'/proc/{before["pid"]}/exe'),'identity_stable':before==ident(before['pid'])}
for label in ['codex_worker','appserver']:
 allenv=readenv(processes[label]['pid']);selected={k:allenv[k] for k in keys if k in allenv};envs[label]=selected;write(raw,stage+'-'+label+'-targeted-environment.json',selected)
 processes[label]['environment']={k:(endpoint(v) if k.lower().endswith('proxy') and k.lower()!='no_proxy' else (v.split(',') if k.lower()=='no_proxy' else v)) for k,v in selected.items()}
envfile=profile/'workers/codex-domain-e2e.env';selected={}
for line in envfile.read_text().splitlines():
 if '=' in line:
  k,v=line.split('=',1)
  if k in keys:selected[k]=json.loads(v) if v.startswith('"') else v
write(raw,stage+'-environment-file-targeted.json',selected)
creds=json.loads((profile/'credentials.json').read_text());uds=str(profile/'run/openagentx.sock'); token=next(c['token'] for c in creds['credentials'] if c['socket_path']==uds)
class UDS(http.client.HTTPConnection):
 def connect(self):
  self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);self.sock.settimeout(20);self.sock.connect(uds)
def get(path):
 c=UDS('localhost');c.request('GET',path,headers={'Authorization':'Bearer '+token});r=c.getresponse();data=json.loads(r.read());assert r.status==200;return data
network=get('/api/observe/v1/network-profiles');binding=next(x for x in network['bindings'] if x['agent_id']=='codex-domain-e2e' and x['backend_id']=='codex');overview=get('/api/observe/v1/overview');agent=next(a for a in overview['agents'] if a['agent_id']=='codex-domain-e2e');api={'method':'GET','transport':'unix-domain-socket','paths':['/api/observe/v1/network-profiles','/api/observe/v1/overview'],'binding':binding,'agent':agent}
# The formal safe Observe projection is retained; no raw authentication headers.
write(raw,stage+'-observe.json',api);write(repo,stage+'-observe.json',api)
providerconfig=tomllib.loads((Path(envs['appserver']['CODEX_HOME'])/'config.toml').read_text());providername=providerconfig.get('model_provider','');provider=providerconfig['model_providers'][providername];pinfo={'provider':providername,'base_url':endpoint(provider['base_url']),'requires_openai_auth':provider.get('requires_openai_auth')}
checks={'all_oax_proc_sha_match_release':all(processes[k]['sha256']==expected for k in ['daemon','agy_worker','codex_worker']),'installed_binary_matches_release':sha('/home/sky/.local/bin/openagentx')==expected,'all_process_identities_stable':all(x['identity_stable'] for x in processes.values()),'appserver_child_of_codex_worker':processes['appserver']['ppid']==processes['codex_worker']['pid'],'appserver_sha_matches_formal_identity':processes['appserver']['sha256']==binding['runtime_identity']['executable_sha256'],'environment_file_private':stat.S_IMODE(envfile.stat().st_mode)==0o600,'environment_file_matches_worker':all(envs['codex_worker'].get(k)==v for k,v in selected.items()),'worker_and_appserver_environment_match':envs['codex_worker']==envs['appserver'],'both_no_proxy_include_loopback':all({'localhost','127.0.0.1','::1'}.issubset(set(envs[l].get(k,'').split(','))) for l in envs for k in ['NO_PROXY','no_proxy']),'provider_host_matches_no_proxy':all(pinfo['base_url']['host'] in envs[l].get(k,'').split(',') for l in envs for k in ['NO_PROXY','no_proxy']),'network_applied_current_generation':binding['desired_status']=='applied' and binding.get('applied_generation')==agent.get('generation',binding.get('applied_generation'))}
report={'stage':stage,'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'expected_commit':commit,'expected_binary_sha256':expected,'release_clone_head':run(['git','-C',str(release),'rev-parse','HEAD'])['output'].strip(),'units':units,'processes':processes,'state_file':str(statepath),'state':state,'environment_file':{'path':str(envfile),'mode':oct(stat.S_IMODE(envfile.stat().st_mode)),'sha256':sha(envfile),'targeted_keys':list(selected)},'provider':pinfo,'checks':checks,'limitations':['No new task, model call or service action performed.','Environment assembly and NO_PROXY configuration do not prove external traffic/proxy forwarding or observed bypass.','Version provenance uses Go build info because openagentx has no version subcommand.']}
write(raw,stage+'-snapshot.json',report);write(repo,stage+'-snapshot.json',report)
if stage=='before':
 write(raw,'capture.py',Path(__file__).read_text());write(repo,'capture.py',Path(__file__).read_text())
 write(repo,'version-command-first-failure.json',{'argv':['/home/sky/.local/bin/openagentx','version'],'exit_code':1,'output':'Unknown command: version\n','next':'Use go version -m and supported --help; no product change required.'})
for folder in [raw,repo]:write(folder,'SHA256SUMS',''.join(sha(p)+'  '+p.name+'\n' for p in sorted(folder.iterdir()) if p.is_file() and p.name!='SHA256SUMS'))
print(json.dumps({'stage':stage,'checks':checks,'processes':{k:{'pid':v['pid'],'sha256':v['sha256']} for k,v in processes.items()},'repo':str(repo)},indent=2))
