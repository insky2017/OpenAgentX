import argparse,importlib.util,pathlib,json,socket,hashlib,websocket,os
root=pathlib.Path.home()/'.local/state/openagentx/validation/2026-10-07-native-model-settings'
s=importlib.util.spec_from_file_location('deployment','/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/scripts/validation/deploy_native_model_settings.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
r=m.Rollout(argparse.Namespace(profile=pathlib.Path.home()/'.openagentx',installed=pathlib.Path.home()/'.local/bin/openagentx',evidence=root/'recovery02'))
before=json.loads((root/'deployment01/before.json').read_text());current=r.snapshot()
sha='780adff7f44cc6c71f37e52e8afc1bcacdfde8436045b3f52d0bc2dfe3614dbe'
assert current['binary_sha256']==sha
for unit,a in current['service_artifacts'].items():
 assert a['sha256']==sha and a['pid']!=int(before['service_pids'][unit]),unit
checks={}
for agent in m.AGENTS:
 a=current['workers'][agent];pane=current['panes'][agent]
 assert a['worker_status']=='online' and a['backend_health']['codex']=='healthy',agent
 assert a['generation']>before['workers'][agent]['generation']
 assert current['states'][agent]['thread_id']==before['states'][agent]['thread_id']
 assert pane['window']==before['panes'][agent]['window'] and pane['pane']==before['panes'][agent]['pane'] and not pane['dead']
 api=r.api('/api/console/v1/agents/'+agent+'/model-settings?backend_id=codex');r.save('final-settings-'+agent+'.json',api)
 native=json.loads((root/'recovery02'/('native-after-'+agent+'.json')).read_text())
 bridge=next(p for p in native['processes'] if p['comm']=='openagentx')
 assert m.digest(pathlib.Path('/proc')/str(bridge['pid'])/'exe')==sha
 tui=next(p for p in native['processes'] if p['comm']=='codex')
 endpoint=tui['argv'][tui['argv'].index('--remote')+1];assert endpoint.startswith('unix://')
 conn=socket.socket(socket.AF_UNIX);conn.settimeout(15);conn.connect(endpoint[len('unix://'):])
 ws=websocket.create_connection('ws://localhost',socket=conn,suppress_origin=True,timeout=15)
 def call(i,method,params):
  ws.send(json.dumps({'id':i,'method':method,'params':params}))
  while True:
   v=json.loads(ws.recv())
   if v.get('id')==i:
    assert 'error' not in v,(agent,method,'RPC error')
    return v['result']
 call(1,'initialize',{'clientInfo':{'name':'oax-deploy-readonly-check','version':'1'}})
 ws.send(json.dumps({'method':'initialized','params':{}}))
 models=call(2,'model/list',{});cfg=call(3,'config/read',{'includeLayers':False});ws.close()
 names=[x.get('model',x.get('id')) for x in models['data']]
 assert len(names)>1
 checks[agent]={'generation':a['generation'],'thread_id':current['states'][agent]['thread_id'],'pane':pane['pane'],'window':pane['window'],'bridge_pid':bridge['pid'],'bridge_binary_sha256':sha,'model_settings_api':True,'bridge_model_list':names,'bridge_config_model':cfg.get('config',{}).get('model'),'bridge_config_effort':cfg.get('config',{}).get('model_reasoning_effort'),'backend_healthy':True}
r.save('final-snapshot.json',current);r.save('final-runtime-checks.json',checks)
task='task-ba0f9dac-d4ca-4f86-ae10-a5a4a003b932';d=r.api('/api/console/v1/agents/openagentx/tasks/'+task)
r.save('automatic-verification-task.json',{'task_id':task,'task_status':d['task']['status'],'run_id':d['latest_run']['run_id'],'worker_generation':d['latest_run']['worker_generation'],'started_at':d['latest_run']['started_at'],'original_thread':current['states']['openagentx']['thread_id']})
r.save('result.json',{'at':m.now(),'status':'PASS_AFTER_SCOPED_RECOVERY','source_commit':'4774f19103ff2b9fff7eaf2ce2527578f3d75a55','installed_sha256':sha,'six_workers_online_healthy':True,'six_original_threads_preserved':True,'six_original_window_panes_reconnected':True,'seven_services_and_six_bridges_match_verified_artifact':True,'six_settings_apis_and_bridge_read_rpcs':True,'production_business_tasks_replayed':False,'initial_deployment_status':'FAILED (preserved in deployment01/result.json)','remaining':['rhythm registered handoff checksum mismatch','pay-service join receipt invalid JSON','30 minute Run timeout unchanged'],'isolated_real_model_e2e':'PASS; production verification reads only'})
print('PASS_AFTER_SCOPED_RECOVERY: six agents, seven services, six native bridges')
