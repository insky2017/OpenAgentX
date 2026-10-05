from pathlib import Path
import os,subprocess,json,hashlib,datetime
os.umask(0o077)
root=Path('/home/sky/.local/state/openagentx/validation/2026-10-05-terminal-overview-join'); out=root/'final-review'; repo=Path('/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX')
expected_sha='9ff957ac0a6c043b9cd8e8a41ba6270e72e1d8dafe032bfac7f1b6950748144e';expected_commit='eb043ca3012b40ca5515a97c83e5422f0598f839'
commands=[]; checks={}
def save(name,value): (out/name).write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n')
def run(args):
 p=subprocess.run(args,capture_output=True,text=True,timeout=20);commands.append({'argv':args,'exit_code':p.returncode,'stdout':p.stdout,'stderr':p.stderr});return p
snap=json.loads((out/'final-independent.json').read_text()); checks['baseline_continuity_all']=snap['continuity']['pass']
installed=snap['installed_binary'];checks['installed_binary_exact_sha']=installed['sha256']==expected_sha;checks['installed_source_exact_clean']='vcs.revision='+expected_commit in installed['build_info'] and 'vcs.modified=false' in installed['build_info']
links={}
for name in ['oax-join','openagentx-join']:
 p=Path.home()/'.agents/skills'/name;links[name]={'symlink':p.is_symlink(),'target':os.readlink(p) if p.is_symlink() else None,'resolved':str(p.resolve()),'inode':p.lstat().st_ino}
 checks[name+'_canonical_link']=p.is_symlink() and p.resolve()==repo/'skills'/name
check=json.loads((out/'skill-check.json').read_text());checks['actual_codex_discovery_both_enabled']=check['ok'] and not check['conflict'] and all(s['visible'] and s['enabled'] for s in check['skills'])
checks['shared_helper_one_file']=(repo/'skills/oax-join/scripts/prepare.py').resolve()==(repo/'skills/openagentx-join/scripts/prepare.py').resolve()
markers={}; targets={}
for aid in ['openagentx','rhythm','pay-service','quote-service','identity-service','oneaxe-voice']:
 wins=[(wid,w) for wid,w in snap['windows'].items() if w['name']==aid];checks[aid+'_unique_window']=len(wins)==1;wid,w=wins[0];opts=w['options'];markers[aid]=opts
 checks[aid+'_canonical_only']=opts['@openagentx_managed']=={'set':True,'value':'1'} and opts['@openagentx_agent_id']=={'set':True,'value':aid} and not opts['@oax-managed']['set'] and not opts['@oax-agent-id']['set']
 checks[aid+'_terminal_options']=all(opts[k]['value']==v for k,v in [('automatic-rename','off'),('allow-rename','off'),('remain-on-exit','on')])
 pane=next(p for p in snap['panes'] if p['window_id']==wid and p['pane_index']=='0')
 checks[aid+'_live_pane0']=pane['dead']=='0'
 targets[aid]={'window':wid,'pane':pane['pane_id'],'pid':pane['pane_pid'],'thread_id':snap['threads'][aid]['thread_id']}
for aid,service in snap['services'].items(): checks[aid+'_service_active']=service['ActiveState']=='active' and service['SubState']=='running'
clients=run(['tmux','list-clients','-F','#{client_tty}|#{session_name}|#{window_id}|#{pane_id}']);prior=json.loads((root/'overview-installed/clients-before.json').read_text());checks['user_client_selection_unchanged']=clients.returncode==0 and clients.stdout.strip()==prior.strip()
overview=next(p for p in snap['panes'] if p['window_id']=='@2' and p['pane_id']=='%2');checks['overview_expected_pid_alive']=overview['pane_pid']=='2304458' and overview['dead']=='0';checks['overview_executable_expected_sha']=overview['process'].get('executable_sha256')==expected_sha
# Read-only capture preserves current selected client/window. Raw rendering is private evidence.
capture=run(['tmux','capture-pane','-p','-t','%2']);(out/'overview.rendered.txt').write_text(capture.stdout);checks['overview_rendered_online']='在线' in capture.stdout and '上次同步' in capture.stdout and 'OAX 总览' in capture.stdout and '离线 · 旧数据' not in capture.stdout
clients2=run(['tmux','list-clients','-F','#{client_tty}|#{session_name}|#{window_id}|#{pane_id}']);checks['review_did_not_switch_clients']=clients.stdout==clients2.stdout
save('links.json',links);save('markers.json',markers);save('navigation-targets-readonly.json',targets);save('read-only-commands.json',commands)
report={'verdict':'PASS' if all(checks.values()) else 'FAIL','checked_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'有界最终安装只读独立核查；未修改现场、未切窗、未发业务消息或模型调用。','source_commit':expected_commit,'installed_binary_sha256':expected_sha,'checks':checks,'continuity':snap['continuity'],'overview':{'window':'@2','pane':'%2','pid':overview['pane_pid'],'live_and_online':checks['overview_expected_pid_alive'] and checks['overview_rendered_online'],'runtime_binary_verified':checks['overview_executable_expected_sha']},'limits':['未实际执行导航switch；仅核对当前唯一canonical身份窗口/pane0映射。','Skill check 只验证新app-server真实发现/启用；本轮未重复模型。'],'evidence':['final-independent.json','skill-check.json','links.json','markers.json','navigation-targets-readonly.json','read-only-commands.json','overview.rendered.txt','verify_final.py']}
save('review.json',report);save('manifest.json',{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in out.iterdir() if p.is_file() and p.name!='manifest.json'});print(json.dumps({'verdict':report['verdict'],'checks':len(checks),'failed':[k for k,v in checks.items() if not v],'overview':report['overview']},ensure_ascii=False))
