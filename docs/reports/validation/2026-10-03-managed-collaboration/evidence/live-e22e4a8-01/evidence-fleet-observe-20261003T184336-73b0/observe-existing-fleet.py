#!/usr/bin/env python3
"""Observe the single existing Fleet input; never send a prompt or create a Task."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys

repo = Path('/home/sky/work/touzi/OneAxe/OpenAgentX-workflow-worktree')
source = repo / 'scripts/validation/managed_collaboration_e2e.py'
spec = importlib.util.spec_from_file_location('managed_acceptance', source)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
root = Path(__file__).resolve().parent
args = argparse.Namespace(root=root, binary=root.parent/'openagentx-e22e4a8', web=repo/'web/dist', phase='fleet-observe', replace_binary=False, commit='e22e4a89862246c33aef822527ea3b145f150a74')
run = module.Acceptance(args)
(run.out/'harness.py').write_bytes(source.read_bytes())
(run.out/'observe-existing-fleet.py').write_bytes(Path(__file__).read_bytes())
run.save('attempt-provenance.json', {'at':module.now(),'phase':'fleet-observe-existing','source_commit':args.commit,'binary_sha256':module.sha(args.binary),'scope':'Only formal observation of the one existing Fleet Task; no prompt or Task creation'})
first = root/'evidence-fleet-20261003T183859-0ffd'
reads = [json.loads(p.read_text()) for p in sorted((first/'http').glob('*.json'))]
base = next(x['response']['tasks'] for x in reads if x.get('path') == '/api/observe/v1/tasks?limit=100')
try:
    run.login()
    current = run.all_tasks()
    created = [t for t in current if t['id'] not in {t['id'] for t in base}]
    module.require(len(created)==1, 'Fleet created unexpected Task count')
    task_id = 'task-6ef2c283-822d-4047-a9d2-f1284756ee07'
    module.require(created[0]['id']==task_id, 'Fleet Task differs from observed pane')
    detail = run.settle(task_id, native_mutation=True)
    a = run.agents[0]
    proof = run.workspace(a)/'fleet-proof.txt'
    expected = proof.read_text().strip()
    module.require(expected.startswith('OAX_FLEET_NATIVE_') and proof.read_bytes()==(expected+'\n').encode(), 'Fleet independent artifact malformed')
    module.require((detail['task'].get('result') or '').strip()==expected,'Fleet native readback mismatch')
    server = 'oax-managed-'+a
    tmux = ['tmux','-L',server]
    capture = subprocess.check_output([*tmux,'capture-pane','-p','-S','-150','-t','OAX:'+a+'.0'], text=True)
    module.require(expected in capture,'Fleet pane lacks exact random result')
    panes = subprocess.check_output([*tmux,'list-panes','-s','-t','=OAX','-F','#{window_name}\t#{pane_index}\t#{pane_dead}\t#{pane_pid}'],text=True)
    for agent in run.agents:
        module.require(any(line.startswith(agent+'\t0\t0\t') for line in panes.splitlines()),'native pane missing/dead')
        module.require(run.state(agent)['thread_id']==run.bindings[agent]['thread_id'],'Fleet changed thread')
    run.process_provenance('fleet-owned-processes')
    run.save('fleet-native-result.json', {'status':'PASS','at':module.now(),'task_id':task_id,'run_id':detail['run_attempts'][0]['run_id'],'task_status':detail['task']['status'],'task_error':detail['task']['error'],'run_status':detail['run_attempts'][0]['status'],'new_tasks':1,'input':detail['task'].get('content'),'capture':capture,'panes':panes,'independent_file_bytes':proof.read_text(),'independent_file_sha256':module.sha(proof),'canonical_sha256':module.sha(Path.home()/'.local/bin/openagentx'),'threads':{agent:run.state(agent)['thread_id'] for agent in run.agents},'prior_attempt':str(first),'scope':'Formal fleet workspace default native panes and one input; same existing editor submitted after paste-settling, no repeat execution; native Task remains uncertain/business_effect_unverified; fleet up/systemd first-start not covered'})
    subprocess.run([*tmux,'kill-server'],check=True)
    run.save('fleet-owned-tmux-close.json',{'at':module.now(),'server':server,'action':'kill this fixture tmux server only','daemon_workers':'left running'})
    run.save('verdict-fleet-observe.json',{'status':'PASS','at':module.now(),'scope':'existing Fleet input observed; first harness timeout retained'})
    print(json.dumps({'status':'PASS','out':str(run.out),'task_id':task_id}))
finally:
    run.collect()
