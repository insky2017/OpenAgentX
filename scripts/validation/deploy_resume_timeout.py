#!/usr/bin/env python3
"""Authorized local OAX rollout, isolated from the Worker hosting the operator.

Default is read-only preflight. --apply drains the six named Workers via the
formal API, waits for all Runs to finish, installs, restarts, and reconnects
only the recorded managed panes. It never force-stops a Run or retries work.
"""
import argparse
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import shlex
import shutil
import socket
import subprocess
import time
import uuid
import signal
import yaml

AGENTS = ('openagentx', 'rhythm', 'pay-service', 'quote-service', 'identity-service', 'oneaxe-voice')


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


class Rollout:
    def __init__(self, args):
        self.a = args
        args.evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.sp = str(args.profile / 'run/openagentx.sock')
        self.log = args.evidence / 'operations.jsonl'
        self.paused = False
        self.installed = False

    def save(self, name, value):
        target = self.a.evidence / name
        temporary = target.with_suffix(target.suffix + '.new')
        temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
        temporary.chmod(0o600)
        os.replace(temporary, target)

    def event(self, step, **fields):
        with self.log.open('a') as f:
            f.write(json.dumps({'at': now(), 'step': step, **fields}, ensure_ascii=False) + '\n')
        print(step, flush=True)

    def api(self, path, body=None, method=None):
        document = json.loads((self.a.profile / 'credentials.json').read_text())
        selected = document['current'][self.sp]
        credential = next(c for c in document['credentials'] if c['socket_path'] == self.sp
                          and c['installation_id'] == selected['installation_id']
                          and c['username'] == selected['username'])
        sp = self.sp
        class UnixHTTP(http.client.HTTPConnection):
            def connect(self):
                self.sock = socket.socket(socket.AF_UNIX)
                self.sock.settimeout(self.timeout)
                self.sock.connect(sp)
        connection = UnixHTTP('unix', timeout=20)
        headers = {'Authorization': 'Bearer ' + credential['token']}
        if body is not None:
            headers['Content-Type'] = 'application/json'
            if body.get('meta', {}).get('idempotency_key'):
                headers['Idempotency-Key'] = body['meta']['idempotency_key']
        connection.request(method or ('POST' if body is not None else 'GET'), path,
                           json.dumps(body).encode() if body is not None else None, headers)
        response = connection.getresponse()
        raw = response.read()
        connection.close()
        require(200 <= response.status < 300, f'API {path} HTTP {response.status}')
        return json.loads(raw) if raw else {}

    def command(self, *argv):
        p = subprocess.run(argv, text=True, capture_output=True, timeout=240)
        require(p.returncode == 0, f'command failed ({p.returncode}): {shlex.join(argv)}')
        return p.stdout.strip()

    def agent_command(self, action, agent, *options):
        return [str(self.a.installed), 'agent', action, agent, *options,
                '--db', str(self.a.profile / 'data/openagentx.db'),
                '--socket', self.sp, '--file', str(self.a.profile / 'fleet.yaml'),
                '--worker-dir', str(self.a.profile / 'workers'),
                '--credentials', str(self.a.profile / 'credentials.json')]

    def attach(self, agent):
        return self.api('/api/console/v1/attach?agent_id=' + agent)

    def panes(self):
        rows = self.command('tmux', 'list-panes', '-a', '-F',
            '#{session_name}|#{window_id}|#{window_name}|#{pane_id}|#{pane_index}|#{pane_pid}|#{pane_dead}|#{@openagentx_agent_id}|#{@openagentx_managed}').splitlines()
        result = {}
        for row in rows:
            session, window, name, pane, index, pid, dead, agent, managed = row.split('|')
            if session == 'OAX' and agent in AGENTS and index == '0':
                require(agent not in result and name == agent and managed == '1', 'ambiguous managed pane: ' + agent)
                result[agent] = {'window': window, 'pane': pane, 'pid': int(pid), 'dead': dead == '1', 'name': name}
        require(set(result) == set(AGENTS), 'six exact managed Agent panes must exist')
        return result

    def states(self):
        result = {}
        for agent in AGENTS:
            state = json.loads((self.a.profile / 'workers/codex' / agent / 'state.json').read_text())
            result[agent] = {k: state.get(k) for k in ('thread_id', 'state', 'task_id', 'run_id', 'pid')}
            require(result[agent]['thread_id'], 'missing thread: ' + agent)
        return result

    def snapshot(self):
        units = ['openagentx.service'] + ['openagentx-worker@' + a + '.service' for a in AGENTS]
        service_pids = {unit: self.command('systemctl', '--user', 'show', unit, '-p', 'MainPID', '--value')
                        for unit in units}
        service_artifacts = {}
        for unit, pid in service_pids.items():
            require(pid.isdigit() and int(pid) > 0, 'service has no live process: ' + unit)
            executable = Path('/proc') / pid / 'exe'
            service_artifacts[unit] = {'pid': int(pid), 'executable': os.readlink(executable),
                'sha256': digest(executable),
                'started_at': self.command('systemctl', '--user', 'show', unit, '-p', 'ExecMainStartTimestamp', '--value')}
        return {'at': now(), 'binary_sha256': digest(self.a.installed), 'states': self.states(),
                'panes': self.panes(), 'workers': {a: self.attach(a) for a in AGENTS},
                'service_pids': service_pids, 'service_artifacts': service_artifacts}

    def wait(self, predicate, seconds, label):
        end = time.monotonic() + seconds
        last_error = None
        while time.monotonic() < end:
            try:
                value = predicate()
                if value:
                    return value
            except (OSError, http.client.HTTPException, RuntimeError) as exc:
                last_error = str(exc)
            time.sleep(5)
        raise RuntimeError('deadline exceeded: ' + label + (': ' + last_error if last_error else ''))

    def process_tree(self, root):
        allp = {}
        for path in Path('/proc').iterdir():
            if not path.name.isdigit(): continue
            try:
                fields = (path/'stat').read_text().rsplit(')', 1)[1].split()
                argv = [v.decode() for v in (path/'cmdline').read_bytes().split(b'\0') if v]
                allp[int(path.name)] = {'pid':int(path.name), 'ppid':int(fields[1]),
                    'start':fields[19], 'argv':argv, 'comm':(path/'comm').read_text().strip()}
            except (OSError, ValueError): continue
        ids = [root]; n = 0
        while n < len(ids):
            ids.extend(p for p,v in allp.items() if v['ppid'] == ids[n]); n += 1
        return [allp[p] for p in ids if p in allp]

    def resume_panes(self, before):
        for agent in AGENTS:
            recorded = before['panes'][agent]
            current = self.panes()[agent]
            thread = before['states'][agent]['thread_id']
            require(current['window'] == recorded['window'] and current['pane'] == recorded['pane'], 'pane identity changed: '+agent)
            require(self.states()[agent]['thread_id'] == thread, 'thread drift: '+agent)
            require(self.attach(agent).get('backend_health', {}).get('codex') == 'healthy', 'backend not ready: '+agent)
            owned = self.process_tree(current['pid']) if not current['dead'] else []
            bridges = [p for p in owned if p['argv'] and Path(p['argv'][0]).name == 'openagentx' and p['argv'][1:4] == ['agent','open',agent] and '--native' in p['argv']]
            views = [p for p in owned if p['comm'] == 'codex' and len(p['argv']) > 1 and p['argv'][1] == 'resume' and '--remote' in p['argv'] and p['argv'][-1] == thread]
            shell = len(owned) == 1 and owned[0]['comm'] in ('zsh','bash','sh')
            require(current['dead'] or shell or (len(bridges) == len(views) == 1), 'unexpected native foreground: '+agent)
            self.save('native-before-'+agent+'.json', {'pane':current, 'processes':owned})
            if bridges:
                target = bridges[0]
                require(any(p['pid'] == target['pid'] and p['start'] == target['start'] for p in self.process_tree(current['pid'])), 'foreground PID changed')
                fd = os.pidfd_open(target['pid'])
                try: signal.pidfd_send_signal(fd, signal.SIGTERM)
                finally: os.close(fd)
                self.wait(lambda: not any(p['pid'] in (target['pid'],views[0]['pid']) for p in self.process_tree(current['pid'])), 30, 'old native foreground exit')
            current = self.panes()[agent]
            require(current['window'] == recorded['window'] and current['pane'] == recorded['pane'], 'target changed while reopening')
            command = shlex.join(self.agent_command('open', agent, '--native'))
            if current['dead']:
                self.command('tmux','respawn-pane','-t',current['pane'],command)
            else:
                owned = self.process_tree(current['pid'])
                require(len(owned) == 1 and owned[0]['comm'] in ('zsh','bash','sh'), 'native foreground is not an idle shell')
                self.command('tmux','send-keys','-t',current['pane'],'C-u')
                self.command('tmux','send-keys','-t',current['pane'],'-l','exec '+command)
                self.command('tmux','send-keys','-t',current['pane'],'Enter')
            def reopened():
                pane = self.panes()[agent]
                tree = self.process_tree(pane['pid'])
                return tree if any(p['comm'] == 'codex' and '--remote' in p['argv'] and p['argv'][-1] == thread for p in tree) else None
            tree = self.wait(reopened, 60, 'new native foreground '+agent)
            bridge = next(p for p in tree if p['comm'] == 'openagentx')
            require(digest(Path('/proc')/str(bridge['pid'])/'exe') == self.a.sha, 'native artifact differs')
            require(self.states()[agent]['thread_id'] == thread, 'reopen replaced thread')
            self.save('native-after-'+agent+'.json', {'pane':self.panes()[agent], 'processes':tree, 'thread_id':thread, 'bridge_sha256':self.a.sha})
            self.event('native_reopened',agent=agent,pane=current['pane'])

    def configure_timeout(self):
        for agent in AGENTS:
            path = self.a.profile/'workers'/(agent+'.yaml')
            original = path.read_bytes()
            backup = self.a.evidence/(agent+'-previous-worker.yaml')
            backup.write_bytes(original); backup.chmod(0o600)
            document = yaml.safe_load(original)
            count = 0
            for backend in document['runtime_backends']:
                if backend['adapter_id'] == 'codex-app-server':
                    backend['options']['timeout'] = '0s'; count += 1
            require(count == 1, 'expected exactly one Codex backend: '+agent)
            stage = path.with_suffix('.timeout-new')
            stage.write_text(yaml.safe_dump(document,allow_unicode=True,sort_keys=False)); stage.chmod(0o600)
            require(path.read_bytes() == original, 'worker configuration changed concurrently')
            os.replace(stage,path)
            self.event('timeout_configured',agent=agent,timeout='0s',backup_sha256=digest(backup))

    def enqueue_verify(self, status):
        content = ('继续用户已授权的Codex无总截止与已登记Agent resume修复收尾。'
            '先完整读取 '+str(self.a.evidence/'result.json')+' 和 operations.jsonl，'
            '以及 '+str(self.a.evidence.parent/'long01/evidence/recheck-result.json')+'。'
            '核验六域Worker、原thread和原pane、实际工件、正式Rhythm/Pay resume以及无deadline冻结规格。'
            '产品工作树 /home/sky/work/touzi/OneAxe/OpenAgentX-resume-timeout-worktree。'
            '若脚本失败，仅按原授权恢复受影响OAX服务，不重跑业务任务、不扩大范围。'
            '清理本轮隔离测试进程但保留证据，补充docs/reports/validation/2026-10-07-resume-timeout/DELIVERY.md，'
            '归档脱敏证据与manifest，完成提交合入并push main。给用户简洁中文汇报。最多一个子代理。')
        key = 'resume-timeout-verify-' + self.a.sha[:20]
        receipt = self.api('/api/control/v1/tasks', {'meta': {'idempotency_key': key},
            'target_agent_id': 'openagentx', 'organization_id': 'default', 'dispatch_mode': 'direct',
            'intent': 'mutation', 'content': content,
            'runtime_session': {'backend_id': 'codex', 'provider_session_id': self.before['states']['openagentx']['thread_id']}})
        self.save('verification-task.json', receipt)
        self.event('verification_queued', task_id=receipt['task_id'])

    def execute(self):
        require(self.a.profile.resolve() == (Path.home() / '.openagentx').resolve(), 'this deployment targets the existing local systemd profile only')
        require(digest(self.a.candidate) == self.a.sha, 'candidate SHA differs')
        proof = json.loads((self.a.evidence.parent/'long01/evidence/recheck-result.json').read_text())
        require(proof['status'] == 'PASS', 'real long task must pass')
        if proof['binary_sha256'] != self.a.sha:
            reuse = json.loads((self.a.evidence.parent/'release-impact.json').read_text())
            require(reuse['long_test_binary_sha256'] == proof['binary_sha256'] and reuse['release_binary_sha256'] == self.a.sha,
                    'impact review must bind both exact artifacts')
            require(reuse['status'] == 'APPROVED' and reuse['runtime_deadline_source_unchanged'] is True,
                    'deadline source equivalence was not reviewed')
            short = json.loads((self.a.evidence.parent/'release-smoke-result.json').read_text())
            require(short['status'] == 'PASS' and short['binary_sha256'] == self.a.sha, 'final artifact real smoke must pass')
        before = self.snapshot()
        before['model_settings'] = {agent:self.api('/api/console/v1/agents/'+agent+'/model-settings?backend_id=codex') for agent in AGENTS}
        self.before = before
        self.save('before.json', before)
        if not self.a.apply:
            self.event('preflight_only', candidate_sha256=self.a.sha)
            return
        try:
            # Pause is a persistent graceful-stop command: active work finishes
            # naturally, including the Run hosting the agent that launched us.
            for agent in AGENTS:
                attached = self.attach(agent)
                if attached['worker_status'] == 'offline':
                    continue
                generation = attached['generation']
                key = 'resume-timeout-stop-' + agent + '-' + uuid.uuid4().hex
                self.api('/api/admin/v1/workers/' + attached['worker_instance_id'] + '/stop',
                    {'meta': {'idempotency_key': key, 'expected_version': generation}, 'expected_generation': generation})
                self.event('graceful_stop_queued', agent=agent, generation=generation)
            self.paused = True
            self.wait(lambda: all(self.attach(a)['worker_status'] == 'offline' for a in AGENTS),
                      3600, 'all existing Runs finish and Workers release')
            self.event('all_workers_offline')
            require(self.states().keys() == before['states'].keys(), 'agent set changed')
            for agent, state in self.states().items():
                require(state['thread_id'] == before['states'][agent]['thread_id'], 'thread changed before restart')
                require(state['state'] not in ('starting', 'running', 'uncertain'), 'unresolved Runtime: ' + agent)
            # Fully stopped units prevent a late shutdown from racing restart.
            self.command('systemctl', '--user', 'stop', *['openagentx-worker@' + a + '.service' for a in AGENTS])
            backup = self.a.evidence / 'previous-openagentx'
            shutil.copy2(self.a.installed, backup)
            stage = self.a.installed.with_name('.openagentx-resume-timeout-install')
            shutil.copyfile(self.a.candidate, stage)
            stage.chmod(0o755)
            require(digest(stage) == self.a.sha, 'staged binary SHA differs')
            os.replace(stage, self.a.installed)
            self.installed = True
            self.event('binary_installed', sha256=self.a.sha)
            self.configure_timeout()
            self.command('systemctl', '--user', 'restart', 'openagentx.service')
            self.event('daemon_restarted')
            self.wait(lambda: Path(self.sp).exists(), 60, 'daemon socket')
            for agent in AGENTS:
                self.command('systemctl', '--user', 'start', 'openagentx-worker@' + agent + '.service')
            def ready():
                return all(self.attach(a)['worker_status'] == 'online' for a in AGENTS)
            self.wait(ready, 180, 'six new Workers online')
            for agent in AGENTS:
                self.command(*self.agent_command('resume', agent, '--no-open', '--wait', '3m'))
                attached = self.attach(agent)
                require(attached.get('backend_health', {}).get('codex') == 'healthy', 'network/backend not ready: ' + agent)
                self.event('network_generation_ready', agent=agent, generation=attached['generation'])
            self.resume_panes(before)
            self.wait(lambda: all(not p['dead'] for p in self.panes().values()), 60, 'six foreground views')
            # Opening a new foreground view must not create or replace a thread.
            time.sleep(10)
            after = self.snapshot()
            require(after['binary_sha256'] == self.a.sha, 'installed binary changed during rollout')
            for unit, artifact in after['service_artifacts'].items():
                require(artifact['sha256'] == self.a.sha, 'service is not running the verified binary: ' + unit)
                require(after['service_pids'][unit] != before['service_pids'][unit], 'service did not restart: ' + unit)
            for agent in AGENTS:
                require(after['states'][agent]['thread_id'] == before['states'][agent]['thread_id'], 'thread changed: ' + agent)
                require(after['workers'][agent]['generation'] > before['workers'][agent]['generation'], 'Worker generation did not advance: ' + agent)
                require(not after['panes'][agent]['dead'], 'native terminal exited: ' + agent)
                settings = self.api('/api/console/v1/agents/' + agent + '/model-settings?backend_id=codex')
                require(len(settings['models']) > 1, 'missing live model catalog: ' + agent)
                for key in ('model','effort','version'):
                    require(settings.get(key) == before['model_settings'][agent].get(key), 'model preference changed: '+agent+' '+key)
                self.save('settings-' + agent + '.json', settings)
                capture = self.command('tmux', 'capture-pane', '-p', '-t', after['panes'][agent]['pane'], '-S', '-60')
                # Native output can contain business text; only save status booleans.
                self.save('native-' + agent + '.json', {'pane': after['panes'][agent]['pane'],
                    'startup_error': '原生终端连接已结束' in capture, 'alive': not after['panes'][agent]['dead']})
            self.save('after.json', after)
            self.save('result.json', {'status': 'PASS', 'at': now(), 'binary_sha256': self.a.sha,
                'thread_preserved': list(AGENTS), 'restarted_services': list(after['service_pids']),
                'production_model_task_executed': False, 'isolated_model_e2e_required': True})
            self.event('deployment_verified')
            self.enqueue_verify('PASS')
        except Exception as exc:
            # No silent rollback, force-stop, database edit or uncertain retry.
            self.save('result.json', {'status': 'FAILED', 'at': now(), 'error': str(exc),
                'installed': self.installed, 'paused': self.paused})
            self.event('deployment_failed', error=str(exc))
            # Recover observability without replaying a business task. Never
            # replace a running backend, delete state, or silently downgrade.
            recovery = {}
            try:
                active = self.command('systemctl', '--user', 'show', 'openagentx.service', '-p', 'ActiveState', '--value')
                if active != 'active':
                    self.command('systemctl', '--user', 'start', 'openagentx.service')
                for agent in AGENTS:
                    try:
                        unit = 'openagentx-worker@' + agent + '.service'
                        def can_resume():
                            state = self.attach(agent)
                            return state['worker_status'] != 'draining'
                        self.wait(can_resume, 180, 'graceful-stop recovery ' + agent)
                        # start has no effect on an already active unit.
                        self.command('systemctl', '--user', 'start', unit)
                        self.wait(lambda: self.attach(agent)['worker_status'] == 'online', 90, 'Worker recovery ' + agent)
                        self.command(*self.agent_command('resume', agent, '--no-open', '--wait', '3m'))
                        recovery[agent] = 'resumed_with_current_generation_network_without_forcing_active_work'
                    except Exception as recovery_error:
                        recovery[agent] = str(recovery_error)
                self.wait(lambda: self.attach('openagentx').get('backend_health', {}).get('codex') == 'healthy', 180, 'operator recovery')
                try:
                    self.resume_panes(before)
                except Exception as pane_error:
                    recovery['native_views'] = str(pane_error)
                self.save('recovery.json', {'at': now(), 'results': recovery})
                self.enqueue_verify('FAILED')
            except Exception as recovery_error:
                self.save('recovery.json', {'at': now(), 'results': recovery, 'error': str(recovery_error),
                    'next_action': 'Inspect result.json and service journals; do not force-stop or retry uncertain business tasks.'})
            raise


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--profile', type=Path, default=Path.home() / '.openagentx')
    p.add_argument('--installed', type=Path, default=Path.home() / '.local/bin/openagentx')
    p.add_argument('--candidate', type=Path, required=True)
    p.add_argument('--sha', required=True)
    p.add_argument('--evidence', type=Path, required=True)
    p.add_argument('--repo', type=Path, required=True)
    p.add_argument('--apply', action='store_true')
    Rollout(p.parse_args()).execute()


if __name__ == '__main__':
    main()
