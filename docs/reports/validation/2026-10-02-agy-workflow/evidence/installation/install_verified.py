import pathlib, subprocess, sqlite3, json, hashlib, datetime, time, shutil, os, urllib.request
P=pathlib.Path
os.umask(0o077)
commit='17d5cd22b442fb5fd02bd661b36c5db4299fea9d'
artifact=P('/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/artifacts')/commit
home=P('/home/sky/.openagentx'); binary=P('/home/sky/.local/bin/openagentx'); db=home/'data/openagentx.db'
raw=P(__file__).parent; public=P('/home/sky/work/touzi/OneAxe/OpenAgentX-workflow-worktree/docs/reports/validation/2026-10-02-agy-workflow/evidence/installation')
public.mkdir(parents=True,exist_ok=True)
started=datetime.datetime.now(datetime.timezone.utc)
backup=home/'backups'/('agy-installed-'+started.strftime('%Y%m%dT%H%M%SZ'));backup.mkdir()
def cmd(args):
 r=subprocess.run(args,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 with (raw/'installation-commands.log').open('a') as f:f.write(json.dumps(args)+'\n'+r.stdout+'\nexit='+str(r.returncode)+'\n')
 r.check_returncode();return r.stdout
def digest(p):return hashlib.sha256(P(p).read_bytes()).hexdigest()
def read_db():return sqlite3.connect(db.as_uri()+'?mode=ro',uri=True)
c=read_db();active=c.execute("select status,count(*) from tasks where status in ('running','dispatching','cancel_requested') group by status").fetchall();before=c.execute('select status,count(*) from tasks group by status').fetchall();schema_before=c.execute('select * from schema_meta').fetchall();c.close();assert not active,active
manifest=json.loads((artifact/'manifest.json').read_text());assert manifest['source_commit']==commit and manifest['source_clean'] and not manifest['vcs_modified']
assert digest(artifact/'openagentx')==manifest['sha256']['openagentx']
cmd(['systemctl','--user','stop','openagentx.service'])
c=read_db();dest=sqlite3.connect(backup/'openagentx.db');c.backup(dest);assert dest.execute('pragma quick_check').fetchone()[0]=='ok';dest.close();c.close()
shutil.copy2(binary,backup/'openagentx-before');shutil.copytree(home/'web',backup/'web-before');shutil.copy2(home/'release.txt',backup/'release-before.txt')
for name in ['fleet.yaml','openagentx.env','credentials.json']:
 if (home/name).exists():shutil.copy2(home/name,backup/name)
for name in ['workers','identities']:
 if (home/name).exists():shutil.copytree(home/name,backup/name)
(backup/'ROLLBACK.md').write_text('Stop new Worker services and daemon first. Restore openagentx-before, web-before and release-before.txt. Restore the matching openagentx.db snapshot with daemon stopped; preserve any newer data separately and remove only stopped database WAL/SHM sidecars. Old executable must not read schema2. Restart daemon and verify its hash/schema. This backup contains development credentials and is private.\n')
tmp=binary.with_name('openagentx.workflow-new');shutil.copy2(artifact/'openagentx',tmp);tmp.chmod(0o755);os.replace(tmp,binary)
newweb=home/'web.workflow-new';assert not newweb.exists();shutil.copytree(artifact/'web',newweb)
oldweb=backup/'web-at-cutover';os.rename(home/'web',oldweb);os.rename(newweb,home/'web')
cmd(['systemctl','--user','start','openagentx.service'])
pid=0
for _ in range(60):
 pid=int(cmd(['systemctl','--user','show','openagentx.service','--property=MainPID','--value']).strip())
 try:
  opener=urllib.request.build_opener(urllib.request.ProxyHandler({}));response=opener.open('http://127.0.0.1:18100/',timeout=2)
  if pid and response.status==200:break
 except Exception:time.sleep(.5)
else:raise RuntimeError('installed daemon did not become available')
assert digest(P('/proc')/str(pid)/'exe')==digest(binary)==manifest['sha256']['openagentx']
vcs=cmd(['go','version','-m',str(binary)]);assert 'vcs.revision='+commit in vcs and 'vcs.modified=false' in vcs
c=read_db();after=c.execute('select status,count(*) from tasks group by status').fetchall();schema_after=c.execute('select * from schema_meta').fetchall();check=c.execute('pragma quick_check').fetchone()[0];c.close();assert before==after and schema_after[0][1]==2 and check=='ok'
webhash={str(p.relative_to(home/'web')):digest(p) for p in (home/'web').rglob('*') if p.is_file()}
for name,value in webhash.items():assert value==manifest['sha256']['web/'+name]
assert hashlib.sha256(opener.open('http://127.0.0.1:18100/assets/app.js').read()).hexdigest()==webhash['assets/app.js']
state=cmd(['systemctl','--user','show','openagentx.service','--property=MainPID,NRestarts,ActiveState,SubState'])
result={'status':'PASS','evidence_level':'I installation, model/UI acceptance separate','started':started.isoformat(),'finished':datetime.datetime.now(datetime.timezone.utc).isoformat(),'commit':commit,'binary_sha256':digest(binary),'pid':pid,'service':state,'web_sha256':webhash,'served_app_js_matches':True,'schema_before':schema_before,'schema_after':schema_after,'historical_counts_before':before,'historical_counts_after':after,'quick_check':check,'backup':str(backup),'production_agent_workers_started':False,'url':'http://127.0.0.1:18100'}
(public/'installed.json').write_text(json.dumps(result,indent=2)+'\n');(raw/'installed.json').write_text(json.dumps(result,indent=2)+'\n');(public/'go-version-m.txt').write_text(vcs)
(home/'release.txt').write_text('OpenAgentX AGY workflow installed candidate\nsource_revision='+commit+'\nvcs.modified=false\nbinary_sha256='+digest(binary)+'\nweb_source_revision='+commit+'\nschema_meta_version=2\nbackup='+str(backup)+'\ninstalled_at='+result['finished']+'\nacceptance=installation identity, schema migration and served Web verified; actual entry and Console acceptance in progress\n')
journal=cmd(['journalctl','--user','-u','openagentx.service','--since',started.strftime('%Y-%m-%d %H:%M:%S UTC'),'--no-pager','-o','short-iso'])
(public/'service-start.log').write_text(journal);shutil.copy2(__file__,public/'install_verified.py');shutil.copy2(raw/'installation-commands.log',public/'installation-commands.log')
print(json.dumps(result,indent=2))
