from pathlib import Path
import subprocess,json,hashlib,datetime,sqlite3,re,urllib.request,urllib.parse,os
repo=Path.cwd(); evidence=repo/'docs/reports/validation/2026-10-02-agy-workflow/evidence'
raw=Path(__file__).resolve().parent
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
manifest=json.loads((evidence/'final-deterministic/recovery-final-artifact-manifest.json').read_text())
service=subprocess.check_output(['systemctl','--user','show','openagentx.service','-p','MainPID','-p','ActiveState','-p','SubState'],text=True)
props=dict(x.split('=',1) for x in service.splitlines())
pid=int(props['MainPID']);proc=Path(f'/proc/{pid}')
args=(proc/'cmdline').read_bytes().split(b'\0')
selected={}
for i,arg in enumerate(args[:-1]):
 if arg in [b'--db',b'--web-dir',b'--http-addr']:selected[arg.decode()]=args[i+1].decode()
hash_file=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
exe_hash=hash_file(proc/'exe');installed_path=(proc/'exe').resolve()
info=subprocess.check_output(['go','version','-m',str(proc/'exe')],text=True)
(raw/'daemon-go-version-m.txt').write_text(info)
web=Path(selected['--web-dir']);expected={k[4:]:v for k,v in manifest['sha256'].items() if k.startswith('web/')}
web_hashes={p:hash_file(web/p) for p in expected}
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
url='http://'+selected['--http-addr']
served={}
for name in expected:
 with opener.open(url+'/'+name,timeout=5) as r:served[name]={'status':r.status,'sha256':hashlib.sha256(r.read()).hexdigest()}
con=sqlite3.connect('file:'+urllib.parse.quote(selected['--db'],safe='/')+'?mode=ro',uri=True,timeout=5)
con.execute('PRAGMA query_only=ON')
schema=con.execute('SELECT singleton,version,applied_at FROM schema_meta').fetchall();con.close()
adr=subprocess.run(['git','diff','--exit-code','7400806','HEAD','--','docs/decisions','docs/adr'],stdout=subprocess.PIPE)
adr_work=subprocess.run(['git','diff','--exit-code','--','docs/decisions','docs/adr'],stdout=subprocess.PIPE)
# Read known local test passwords/tokens only into this process. Never include
# values, their digests, matched text or surrounding bytes in reports.
known=set();secret_sources=0
private_roots=[raw.parent]
short=Path('/home/sky/.oax-e')
private_roots += [p for p in short.iterdir() if p.is_dir() and p.name!='final-source-20261002']
for base in private_roots:
 for p in base.rglob('*'):
  if not p.is_file() or p.is_symlink():continue
  if p.name in {'password','owner-password'}:
   b=p.read_bytes().strip()
   if len(b)>=8:known.add(b);secret_sources+=1
  elif p.name=='credentials.json':
   try:data=json.loads(p.read_text())
   except (ValueError,OSError):continue
   def visit(v):
    if isinstance(v,dict):
     for k,x in v.items():
      if isinstance(x,str) and re.search(r'(token|password|csrf)',k,re.I) and len(x)>=16:known.add(x.encode())
      else:visit(x)
    elif isinstance(v,list):
     for x in v:visit(x)
   visit(data);secret_sources+=1
excluded=[];scan=[]
for p in sorted(evidence.rglob('*')):
 if not p.is_file():continue
 relative=p.relative_to(evidence);top=relative.parts[0]
 if top.startswith('browser') or top.startswith('installed-') or top.startswith('live-17d5cd2-lease') or top in {'final-independent','FINAL-INDEPENDENT-CHECK.md'}:
  excluded.append(str(relative));continue
 scan.append(p)
# Also inspect pending reports and runners outside the evidence tree.
extra_names=set(x.decode() for x in subprocess.check_output(['git','diff','--name-only','HEAD','-z']).split(b'\0') if x)
extra_names.update(x.decode() for x in subprocess.check_output(['git','ls-files','--others','--exclude-standard','-z']).split(b'\0') if x)
extra=[]
for name in sorted(extra_names):
 p=repo/name
 if p.is_file() and not p.is_relative_to(evidence):
  scan.append(p);extra.append(name)
patterns={
 'private_key':rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
 'provider_key':rb'(?:sk-proj-|sk-ant-)[A-Za-z0-9_-]{16,}',
 'bearer_literal':rb'Bearer\s+[A-Za-z0-9_.-]{16,}',
 'credential_value':rb'(?i)["\x27](?:password|session_token|access_token|csrf_token|client_secret)["\x27]\s*:\s*["\x27]([^"\x27\r\n]{12,})["\x27]',
 'cookie_literal':rb'(?i)(?:Cookie|Set-Cookie):\s*[^\r\n]*=[A-Za-z0-9_-]{20,}',
 'proxy_userinfo':rb'https?://[^\s/:@]{2,}:[^\s/@]{4,}@',
}
exact=[];candidates=[];changed=[];scanned_hashes={};binary_count=0
for p in scan:
 before=p.stat();data=p.read_bytes();after=p.stat();rel=str(p.relative_to(evidence)) if p.is_relative_to(evidence) else '@worktree/'+str(p.relative_to(repo))
 if before.st_mtime_ns!=after.st_mtime_ns or before.st_size!=after.st_size:changed.append(rel);continue
 scanned_hashes[rel]=hashlib.sha256(data).hexdigest()
 if any(k in data for k in known):exact.append(rel)
 try:data.decode('utf8')
 except UnicodeDecodeError:binary_count+=1;continue
 for label,pattern in patterns.items():
  matches=list(re.finditer(pattern,data))
  if not matches:continue
  if label=='credential_value':
   matches=[m for m in matches if not re.search(rb'(?i)(redact|masked|hidden|placeholder|test-token|example|dummy)',m.group(1))]
  if matches:candidates.append({'path':rel,'pattern':label,'count':len(matches)})
untracked=[Path(x.decode()) for x in subprocess.check_output(['git','ls-files','--others','--exclude-standard','-z']).split(b'\0') if x]
suspicious=[str(p) for p in untracked if p.is_file() and (p.suffix in {'.db','.sqlite','.sqlite3','.env','.key','.pem','.pyc','.tmp','.bak'} or p.name.lower() in {'password','owner-password','credentials.json','cookiejar','cookies.json'} or '__pycache__' in p.parts)]
report={'started_at':started,'finished_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_head':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'installation':{'service':props,'proc_exe':str(installed_path),'actual_binary_sha256':exe_hash,'expected_binary_sha256':manifest['sha256']['openagentx'],'binary_matches':exe_hash==manifest['sha256']['openagentx'],'vcs_revision_matches':'vcs.revision='+manifest['source_commit'] in info,'vcs_modified_false':'vcs.modified=false' in info,'paths':selected,'schema_meta':schema,'schema_matches_installation':[list(r) for r in schema]==json.loads((evidence/'installation/installed.json').read_text())['schema_after'],'web_files':web_hashes,'disk_web_matches':web_hashes==expected,'served_web':served,'served_web_matches':all(served[k]['status']==200 and served[k]['sha256']==v for k,v in expected.items())},'frozen_adr':{'head_vs_7400806_unchanged':adr.returncode==0,'worktree_unchanged':adr_work.returncode==0},'secret_scan':{'scanned_files':len(scanned_hashes),'additional_pending_worktree_files':extra,'binary_files_exact_bytes_only':binary_count,'known_secret_source_files':secret_sources,'exact_secret_matches':exact,'pattern_candidates':candidates,'files_changed_during_scan':changed,'excluded_top_level':sorted(set(p.split('/')[0] for p in excluded)),'excluded_files':len(excluded),'limits':'Known secret exact bytes and text patterns only. Binary files are not OCR inspected. Active browser/installed/lease directories excluded; no claim about their final contents.'},'untracked_audit':{'file_count':len(untracked),'suspicious_files':suspicious,'oversized_files':[str(p) for p in untracked if p.is_file() and p.stat().st_size>10_000_000]}}
(raw/'result.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
(raw/'scanned-SHA256SUMS').write_text(''.join(v+'  '+k+'\n' for k,v in sorted(scanned_hashes.items())))
(raw/'git-status.txt').write_text(subprocess.check_output(['git','status','--porcelain'],text=True))
print(json.dumps(report,ensure_ascii=False,indent=2))
