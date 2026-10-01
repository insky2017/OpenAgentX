from pathlib import Path
import subprocess,json,hashlib,sqlite3,urllib.request,urllib.parse,datetime
raw=Path(__file__).resolve().parent
evidence=Path('docs/reports/validation/2026-10-02-agy-workflow/evidence')
manifest=json.loads((evidence/'final-deterministic/sse-final-artifact-manifest.json').read_text())
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
service=dict(line.split('=',1) for line in subprocess.check_output(['systemctl','--user','show','openagentx.service','-p','MainPID','-p','ActiveState','-p','SubState'],text=True).splitlines())
pid=int(service['MainPID']);proc=Path(f'/proc/{pid}');argv=(proc/'cmdline').read_bytes().split(b'\0');selected={}
for i,arg in enumerate(argv[:-1]):
 if arg in [b'--db',b'--web-dir',b'--http-addr']:selected[arg.decode()]=argv[i+1].decode()
hash_file=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
actual_hash=hash_file(proc/'exe');info=subprocess.check_output(['go','version','-m',str(proc/'exe')],text=True)
(raw/'daemon-go-version-m.txt').write_text(info)
expected={k[4:]:v for k,v in manifest['sha256'].items() if k.startswith('web/')}
web_hashes={name:hash_file(Path(selected['--web-dir'])/name) for name in expected}
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}));served={}
for name in expected:
 with opener.open('http://'+selected['--http-addr']+'/'+name,timeout=5) as response:served[name]={'status':response.status,'sha256':hashlib.sha256(response.read()).hexdigest()}
con=sqlite3.connect('file:'+urllib.parse.quote(selected['--db'],safe='/')+'?mode=ro',uri=True,timeout=5);con.execute('PRAGMA query_only=ON')
schema=con.execute('SELECT singleton,version,applied_at FROM schema_meta').fetchall();con.close()
checks={'service_active':service['ActiveState']=='active' and service['SubState']=='running','binary_hash_matches':actual_hash==manifest['sha256']['openagentx'],'revision_matches':'vcs.revision='+manifest['source_commit'] in info,'vcs_modified_false':'vcs.modified=false' in info,'schema_v2':len(schema)==1 and schema[0][1]==2,'web_disk_matches':web_hashes==expected,'web_http_matches':all(served[k]['status']==200 and served[k]['sha256']==v for k,v in expected.items()),'frozen_adr_unchanged':subprocess.run(['git','diff','--exit-code','7400806','HEAD','--','docs/decisions','docs/adr'],stdout=subprocess.DEVNULL).returncode==0 and subprocess.run(['git','diff','--exit-code','HEAD','--','docs/decisions','docs/adr'],stdout=subprocess.DEVNULL).returncode==0}
result={'evidence_level':'I read-only installed artifact verification; browser behavior and AGY effects separate','started_at':started,'finished_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'candidate':manifest['source_commit'],'checks':checks,'status':'PASS' if all(checks.values()) else 'FAIL','service':service,'proc_exe':str((proc/'exe').resolve()),'paths':selected,'actual_binary_sha256':actual_hash,'schema_meta':schema,'web_disk_sha256':web_hashes,'web_http':served,'scope':'No service mutation, no tests, no credential use, no database write. Active browser/installed/lease evidence directories were not scanned.'}
(raw/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'status':result['status'],'candidate':result['candidate'],'pid':pid,'checks':checks},ensure_ascii=False,indent=2))
