from pathlib import Path
import json
base=Path('/tmp/oax-assessment-20260923-source'); out=Path(__file__).resolve().parent
mapping={}
p=base/'internal/runtime/codebuddy/adapter_test.go';src=p.read_text()+r'''

func TestAssessmentCodeBuddyEmptyOutputAndTimeout(t *testing.T) {
 t.Run("empty_exit_zero",func(t *testing.T){
  dir:=t.TempDir();binary:=writeFixture(t,dir,"#!/bin/sh\ncat >/dev/null\nexit 0\n")
  adapter,err:=NewAdapter(Config{Binary:binary,Models:[]string{"hy4-preview"},WorkingDir:dir});if err!=nil{t.Fatal(err)}
  handle,err:=adapter.StartTurn(context.Background(),turnRequest("empty"),nil);if err!=nil{t.Fatal(err)}
  result,err:=handle.Wait(context.Background())
  t.Logf("empty_exit_zero status=%s result_bytes=%d error_nil=%v side_effects_known=%v",result.Status,len(result.Result),err==nil,result.SideEffectsKnown)
  if result.Status!=openruntime.TurnResultUncertain {t.Errorf("fail-closed contract violated: empty output classified %s",result.Status)}
 })
 t.Run("resolved_timeout",func(t *testing.T){
  dir:=t.TempDir();binary:=writeFixture(t,dir,"#!/bin/sh\ncat >/dev/null\nsleep 0.25\nprintf 'late reply'\n")
  adapter,err:=NewAdapter(Config{Binary:binary,Models:[]string{"hy4-preview"},WorkingDir:dir});if err!=nil{t.Fatal(err)}
  request:=turnRequest("bounded");request.Execution.Spec.Timeout=40*time.Millisecond
  ctx,cancel:=context.WithTimeout(context.Background(),2*time.Second);defer cancel()
  start:=time.Now();handle,err:=adapter.StartTurn(ctx,request,nil);if err!=nil{t.Fatal(err)}
  result,err:=handle.Wait(ctx);elapsed:=time.Since(start)
  t.Logf("declared_timeout_ms=40 elapsed_ms=%d status=%s error_nil=%v",elapsed.Milliseconds(),result.Status,err==nil)
  if elapsed>150*time.Millisecond&&result.Status==openruntime.TurnResultSucceeded{t.Error("resolved execution timeout not enforced")}
 })
}
'''
pout=out/'runtime-codebuddy-probe.go.txt';pout.write_text(src);mapping[str(p)]=str(pout)
p=base/'internal/runtime/acp/adapter_test.go';src=p.read_text()+r'''
func TestAssessmentACPContradictoryTerminal(t *testing.T) {
 result,err:=collectFixture(t,"cat",`{"status":"succeeded","result":"done","error":"actual execution failed"}`+"\n",nil)
 t.Logf("contradictory_terminal status=%s error_field=%q wait_error_nil=%v",result.Status,result.Error,err==nil)
 if result.Status!=openruntime.TurnResultUncertain{t.Error("contradictory terminal accepted")}
}
'''
pout=out/'runtime-acp-probe.go.txt';pout.write_text(src);mapping[str(p)]=str(pout)
(out/'runtime-probes-overlay.json').write_text(json.dumps({'Replace':mapping}))
