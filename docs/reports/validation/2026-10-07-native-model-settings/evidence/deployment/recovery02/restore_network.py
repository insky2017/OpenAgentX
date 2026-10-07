import argparse,importlib.util,pathlib,time,uuid,json
source=pathlib.Path('/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/scripts/validation/deploy_native_model_settings.py')
s=importlib.util.spec_from_file_location('deployment',source);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
r=m.Rollout(argparse.Namespace(profile=pathlib.Path.home()/'.openagentx',evidence=pathlib.Path(__file__).parent))
for agent in ('rhythm','pay-service'):
 attached=r.attach(agent)
 assert attached['worker_status']=='online'
 current=r.api('/api/observe/v1/network-profiles')
 binding=next(b for b in current['bindings'] if b['agent_id']==agent and b['backend_id']=='codex')
 assert binding['mode']=='inherit'
 r.save('network-before-'+agent+'.json',{'worker':attached,'binding':binding})
 expected=binding['version'];key='model-settings-recovery-'+agent+'-'+uuid.uuid4().hex
 reply=r.api('/api/control/v1/network-bindings/mode/tests',{'meta':{'idempotency_key':key+'-test','expected_version':expected},'agent_id':agent,'backend_id':'codex','mode':binding['mode'],'worker_instance_id':attached['worker_instance_id'],'generation':attached['generation']})
 test_id=reply['receipt']['test_id'];r.save('network-test-receipt-'+agent+'.json',reply)
 deadline=time.monotonic()+120
 while True:
  current=r.api('/api/observe/v1/network-profiles');test=next(t for t in current['mode_tests'] if t['test_id']==test_id)
  if test['state']=='succeeded':break
  if test['state'] in ('failed','stale'):raise RuntimeError('network test '+test['state'])
  assert time.monotonic()<deadline,'network test timeout'
  time.sleep(5)
 r.save('network-test-'+agent+'.json',test)
 reply=r.api('/api/control/v1/network-bindings/mode/publish',{'meta':{'idempotency_key':key+'-publish','expected_version':expected},'test_id':test_id,'worker_instance_id':attached['worker_instance_id'],'generation':attached['generation']})
 r.save('network-publish-'+agent+'.json',reply)
 deadline=time.monotonic()+120
 while True:
  now=r.attach(agent);current=r.api('/api/observe/v1/network-profiles');b=next(b for b in current['bindings'] if b['agent_id']==agent and b['backend_id']=='codex')
  assert now['generation']==attached['generation'] and now['worker_instance_id']==attached['worker_instance_id']
  if b['desired_status']=='applied' and b['applied_generation']==now['generation'] and b['applied_worker_id']==now['worker_instance_id'] and b['applied_binding_revision']==b['version'] and now['backend_health']['codex']=='healthy':break
  assert time.monotonic()<deadline,'network publish timeout'
  time.sleep(5)
 r.save('network-after-'+agent+'.json',{'worker':now,'binding':b})
 r.event('network_restored',agent=agent,generation=now['generation'],test_id=test_id)
