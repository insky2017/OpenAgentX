import datetime,hashlib,http.client,json,os,re,socket,stat,subprocess,tomllib
from pathlib import Path
from urllib.parse import urlsplit
ROOT=Path('/home/sky/work/touzi/OneAxe/OpenAgentX-workflow-worktree')
H=Path('/home/sky/.local/state/openagentx/validation/2026-10-02-codex-workflow/local01/handoff.json')
h=json.loads(H.read_text()); profile=Path(h['profile'])
raw=Path('/home/sky/.local/state/openagentx/evidence/2026-10-02-codex-workflow/network-live01'); raw.mkdir(parents=True,exist_ok=True); raw.chmod(0o700)
repo=ROOT/'docs/reports/validation/2026-10-02-codex-workflow/evidence/network-live01';repo.mkdir(parents=True,exist_ok=True)
def sha(p):
 x=hashlib.sha256()
 with open(p,'rb') as f:
  while b:=f.read(1024*1024):x.update(b)
 return x.hexdigest()
def write(folder,name,obj):
 p=folder/name;p.write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n' if not isinstance(obj,str) else obj);p.chmod(0o600 if folder==raw else 0o644)
def identity(pid):
 s=Path(f'/proc/{pid}/stat').read_text();f=s[s.rfind(')')+2:].split();return {'pid':pid,'ppid':int(f[1]),'starttime_ticks':int(f[19])}
def endpoint(v):
 u=urlsplit(v);return {'scheme':u.scheme,'host':u.hostname,'port':u.port,'credentials_present':bool(u.username or u.password)}
keys=['HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','http_proxy','https_proxy','all_proxy','NO_PROXY','no_proxy','CODEX_HOME']
statefile=profile/'workers/codex'/h['agent']/'state.json';st=json.loads(statefile.read_text());app_before=identity(st['pid']);worker_before=identity(app_before['ppid'])
creds=json.loads((profile/'credentials.json').read_text()); token=next(x['token'] for x in creds['credentials'] if x['socket_path']==h['socket'])
class UDS(http.client.HTTPConnection):
 def connect(self):
  self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);self.sock.settimeout(15);self.sock.connect(h['socket'])
c=UDS('localhost');c.request('GET','/api/observe/v1/network-profiles',headers={'Authorization':'Bearer '+token});resp=c.getresponse();body=json.loads(resp.read());assert resp.status==200
binding=next(b for b in body['bindings'] if b['agent_id']==h['agent'] and b['backend_id']=='codex')
mode_test=next(b for b in body['mode_tests'] if b['test_id']==binding['test_id']); api={'method':'GET','path':'/api/observe/v1/network-profiles','transport':'unix-domain-socket','http_status':resp.status,'binding':binding,'mode_test':mode_test};write(raw,'observe-network.json',api);write(repo,'observe-network.json',api)
processes={};envs={}
for label,before in [('worker',worker_before),('appserver',app_before)]:
 pid=before['pid'];base=Path(f'/proc/{pid}');env=dict(x.split('=',1) for x in (base/'environ').read_bytes().decode().split('\0') if '=' in x);selected={k:env[k] for k in keys if k in env};envs[label]=selected
 info={**before,'exe_path':os.readlink(base/'exe'),'exe_sha256':sha(base/'exe'),'identity_stable_during_read':before==identity(pid)}; assert info['identity_stable_during_read']
 info['environment']={k:(endpoint(v) if 'proxy' in k.lower() and k.lower()!='no_proxy' else (v.split(',') if k.lower()=='no_proxy' else v)) for k,v in selected.items()};processes[label]=info;write(raw,label+'-targeted-environment.json',selected)
envfile=profile/'workers'/(h['agent']+'.env');envtext=envfile.read_text();envselected={}
for line in envtext.splitlines():
 if '=' not in line:continue
 k,v=line.split('=',1)
 if k in keys:envselected[k]=json.loads(v) if v.startswith('"') else v
write(raw,'environment-file-targeted.json',envselected)
config=tomllib.loads((Path(envs['appserver']['CODEX_HOME'])/'config.toml').read_text());provider_name=config.get('model_provider','');provider=config.get('model_providers',{}).get(provider_name,{})
provider_info={'provider':provider_name,'base_url':endpoint(provider['base_url']),'requires_openai_auth':provider.get('requires_openai_auth'),'env_key_present':bool(provider.get('env_key')),'env_http_headers_present':bool(provider.get('env_http_headers'))}
mihomo=Path('/home/sky/.config/mihomo/config.yaml');match=re.search(r'^mixed-port:\s*(\d+)\s*$',mihomo.read_text(),re.M);assert match
loops={'localhost','127.0.0.1','::1'};checks={
 'worker_exe_matches_handoff':processes['worker']['exe_sha256']==h['binary_sha256'],
 'appserver_exe_matches_runtime_identity':processes['appserver']['exe_sha256']==binding['runtime_identity']['executable_sha256'],
 'environment_file_private':stat.S_IMODE(envfile.stat().st_mode)==0o600,
 'environment_file_matches_worker':all(envs['worker'].get(k)==v for k,v in envselected.items()),
 'worker_environment_matches_appserver':envs['worker']==envs['appserver'],
 'all_proxy_endpoints_match_persistent_mihomo_port':all(endpoint(v)=={'scheme':'http','host':'127.0.0.1','port':int(match[1]),'credentials_present':False} for k,v in envs['appserver'].items() if k.lower().endswith('proxy') and k.lower()!='no_proxy'),
 'both_processes_both_no_proxy_include_loopback':all(loops.issubset(set(envs[label].get(k,'').split(','))) for label in envs for k in ['NO_PROXY','no_proxy']),
 'provider_host_present_in_both_no_proxy':all(provider_info['base_url']['host'] in envs[label].get(k,'').split(',') for label in envs for k in ['NO_PROXY','no_proxy'])}
report={'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_head_at_capture':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'scope':'R: isolated local01 process and environment assembly only','handoff_file':str(H),'handoff_sha256':sha(H),'candidate_expected_sha256':h['binary_sha256'],'agent':h['agent'],'processes':processes,'environment_file':{'path':str(envfile),'mode':oct(stat.S_IMODE(envfile.stat().st_mode)),'sha256':sha(envfile),'keys':list(envselected)},'persistent_proxy_source':{'path':str(mihomo),'field':'mixed-port','value':int(match[1]),'selection_implementation':'internal/cli/fleet/agent_environment.go:persistentAgentProxy'},'provider':provider_info,'checks':checks,'result':'PASS' if all(checks.values()) else 'FAIL','limitations':['No new Task or model call was sent.','No external traffic route or proxy forwarding success was measured.','NO_PROXY match proves configuration, not observed traffic bypass.','No systemd restart, enablement, installed artifact or installed service claim.','Formal inherit probe reports network_effect and model_call not_verified.','Source HEAD is a capture label; actual executable hashes identify the running candidate.']}
write(raw,'report.json',report);write(repo,'report.json',report)
write(raw,'capture.py',Path(__file__).read_text());write(repo,'capture.py',Path(__file__).read_text())
readme='''# Codex local01 实际进程与网络环境\n\n范围：隔离 local01 的 R 级进程/环境装配核验。通过正式只读 Observe API 读取网络绑定；凭据仅在内存使用，未保存认证头、auth.json 或完整进程环境。\n\n`report.json` 记录 Worker/app-server PID、starttime、实际 exe SHA-256、目标代理键、CODEX_HOME、私密环境文件来源及权限，逐项比对 handoff 和正式 RuntimeIdentity。provider 127.0.0.1:8080 被两种 NO_PROXY 的回环规则覆盖。\n\n这些证据不证明外网代理流量、模型调用、实际 bypass 流量、systemd 重启或正式安装。inherit 测试的 network_effect/model_call 仍为 not_verified。实际原始目标环境子集保存在本机 0700 目录，文件 0600；入库版本仅保存必要端点与路径。未创建 Task。\n\n私密原件：`'''+str(raw)+'''`。脚本为复核来源，复验应使用新批次目录，避免覆盖首批证据。\n''';write(repo,'README.md',readme)
for folder in [raw,repo]:
 files=sorted(p for p in folder.iterdir() if p.is_file() and p.name!='SHA256SUMS');write(folder,'SHA256SUMS',''.join(sha(p)+'  '+p.name+'\n' for p in files))
print(json.dumps({'result':report['result'],'checks':checks,'repo':str(repo),'processes':{k:{'pid':v['pid'],'sha256':v['exe_sha256']} for k,v in processes.items()}},indent=2))
