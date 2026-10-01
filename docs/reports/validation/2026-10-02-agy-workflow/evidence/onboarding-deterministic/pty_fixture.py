#!/usr/bin/env python3
"""D evidence: real CLI/PTY/daemon, isolated identity DB; systemctl is a stub.
No actual Worker service or AGY execution is claimed by this fixture.
"""
import hashlib, json, os, pathlib, pty, select, signal, subprocess, sys, time, uuid

binary = pathlib.Path(sys.argv[1]).resolve()
raw = pathlib.Path.home()/'.local/state/openagentx/evidence'/('onboarding-d-'+time.strftime('%Y%m%d-%H%M%S'))
raw.mkdir(parents=True, mode=0o700)
profile=raw/'profile'; profile.mkdir(mode=0o700)
workspace=raw/'workspace'; workspace.mkdir(mode=0o700)
role=workspace/'ROLE.md'; role.write_text('# Fixture role\nRead isolated fixture files.\n')
password='fixture-'+uuid.uuid4().hex
secret=raw/'password';secret.write_text(password);secret.chmod(0o600)
stubs=raw/'stubs';stubs.mkdir(mode=0o700)
stub=stubs/'systemctl';stub.write_text('#!/bin/sh\nif [ "$*" = "--user start openagentx.service" ]; then exit 0; fi\nexit 1\n');stub.chmod(0o700)
env=dict(os.environ,OPENAGENTX_HOME=str(profile),PATH=str(stubs)+':'+os.environ['PATH'])
# Keep this fixture independent from developer credentials and runtime settings.
for key in list(env):
    if key.startswith('OPENAGENTX_') and key!='OPENAGENTX_HOME': env.pop(key)
log=[]
def run_pty(args,responses,timeout=30,stop_after=None):
    master,slave=pty.openpty()
    p=subprocess.Popen([str(binary)]+args,stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True)
    os.close(slave); captured=b''; pending=list(responses);start=time.monotonic();interrupted=False
    while time.monotonic()-start<timeout:
        if stop_after is not None and not interrupted and time.monotonic()-start>=stop_after:
            p.send_signal(signal.SIGINT);interrupted=True
        if select.select([master],[],[],0.2)[0]:
            try:chunk=os.read(master,65536)
            except OSError:break
            if not chunk:break
            captured+=chunk
            if pending and pending[0][0].encode() in captured:
                _,value=pending.pop(0);time.sleep(0.1);os.write(master,(value+'\n').encode())
        if p.poll() is not None: break
    if p.poll() is None:
        p.wait(timeout=3)
    os.close(master)
    text=captured.decode(errors='replace').replace(password,'[REDACTED fixture password]')
    log.append('$ '+str(binary)+' '+' '.join(args)+'\n'+text+'\nexit='+str(p.returncode)+'\n')
    if p.returncode:raise RuntimeError('CLI failed; raw evidence retained')
    return text

daemon=None
try:
    run_pty(['init'],[('Owner password:',password),('Confirm owner password:',password)])
    web=raw/'web';web.mkdir();(web/'index.html').write_text('<html>fixture</html>')
    daemonlog=open(raw/'daemon.log','w')
    daemon=subprocess.Popen([str(binary),'serve','--http-addr','127.0.0.1:0','--web-dir',str(web)],env=env,stdout=daemonlog,stderr=subprocess.STDOUT)
    socket=profile/'run/openagentx.sock'
    for _ in range(100):
        if socket.exists():break
        if daemon.poll() is not None:raise RuntimeError('isolated daemon stopped')
        time.sleep(0.1)
    run_pty(['agent','add','--password-file',str(secret),'--configure-only'],[('Agent 名称:','fixture-one'),('工作目录:',str(workspace)),('职责文档路径或职责描述:',str(role))])
    run_pty(['agent','add','--name','fixture-two','--workspace',str(workspace),'--role',str(role),'--password-file',str(secret),'--configure-only'],[])
    run_pty(['agent','add','--name','fixture-one','--workspace',str(workspace),'--role',str(role),'--password-file',str(secret),'--configure-only'],[])
    run_pty(['agent','open','fixture-one','--no-open'],[])
    run_pty(['agent','status'],[])
    watch=run_pty(['agent','status','--watch'],[],stop_after=6)
    if watch.count('\x1b[H\x1b[2J')<2:raise RuntimeError('TTY watch did not refresh in place')
    result={'layer':'D','systemd':'mock; no real service or AGY execution','cli':'real binary with real PTY','daemon':'real isolated process/API/credential login','raw_directory':str(raw),'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'result':'PASS'}
except Exception as exc:
    result={'layer':'D','result':'FAIL','error':str(exc),'raw_directory':str(raw)}
    raise
finally:
    (raw/'pty.log').write_text('\n'.join(log))
    (raw/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    if daemon:
        daemon.terminate()
        try:daemon.wait(timeout=10)
        except subprocess.TimeoutExpired:daemon.kill();daemon.wait()
    print(json.dumps(result,ensure_ascii=False))
