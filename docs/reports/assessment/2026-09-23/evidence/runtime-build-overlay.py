from pathlib import Path
import json
base=Path('/tmp/oax-assessment-20260923-source')
evidence=Path(__file__).resolve().parent
source=(base/'internal/cli/worker/process_test.go').read_text()
source=source.replace('"context"','"context"\n "crypto/sha256"\n "strings"\n "net/http"\n "sync/atomic"')
source=source.replace('3 * time.Second','20 * time.Second')
source += r'''

// Assessment fixture: production assembly and real AGY, isolated identity/DB/UDS/workspace.
// Identity is seeded through the established fixture; all Task/control operations use services.
func TestAssessmentRealAGYConsecutiveTurns(t *testing.T) {
 ctx, cancelAll := context.WithTimeout(context.Background(), 6*time.Minute)
 defer cancelAll()
 root:=t.TempDir()
 workspace:=filepath.Join(root,"workspace")
 if err:=os.MkdirAll(workspace,0700); err!=nil {t.Fatal(err)}
 repository,err:=openagentsqlite.Open(ctx,filepath.Join(root,"isolated.db"),openagentsqlite.Options{})
 if err!=nil {t.Fatal(err)}
 defer repository.Close()
 seedWorkerProcessIdentity(t,repository)
 broker:=controlplane.NewMemoryWakeupBroker()
 workflow:=newWorkerProcessNetworkWorkflow(t,repository,broker)
 service,err:=controlplane.NewWorkerService(repository,broker,controlplane.WorkerServiceOptions{NetworkWorkflow:workflow})
 if err!=nil {t.Fatal(err)}
 handler,err:=workerapi.NewHandler(service,workerapi.StaticPrincipal("worker-principal"))
 if err!=nil {t.Fatal(err)}
 var requests atomic.Int64
 wrapped:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){requests.Add(1);handler.ServeHTTP(w,r)})
 socket:=filepath.Join(root,"s.sock")
 server,err:=unixhttp.NewServer(socket,wrapped,nil)
 if err!=nil {t.Fatal(err)}
 sc,stopServer:=context.WithCancel(ctx)
 serverResult:=make(chan error,1)
 go func(){serverResult<-server.Start(sc)}()
 waitForPath(t,socket)
 defer func(){stopServer();select{case <-serverResult:case <-time.After(10*time.Second):t.Error("server shutdown timeout")}}()
 configPath:=filepath.Join(root,"worker.yaml")
 config:=fmt.Sprintf(`version: 1
agent_id: quote
transport: unix
unix_socket: %s
capabilities: [coding]
heartbeat_interval: 1s
mailbox_wait: 5s
control_wait: 5s
shutdown_timeout: 10s
network_materialization_dir: %s
enable_worker_control: true
runtime_backends:
  - backend_id: local
    adapter_id: agy-batch
    options:
      binary: /home/sky/.local/bin/agy-graft
      models: [gemini-3.7-flash-low]
      working_dir: %s
`,socket,filepath.Join(root,"network"),workspace)
 if err:=os.WriteFile(configPath,[]byte(config),0600);err!=nil {t.Fatal(err)}
 wc,stopWorker:=context.WithCancel(ctx)
 workerResult:=make(chan error,1)
 go func(){workerResult<-RunWorkerProcess(wc,configPath)}()
 defer func(){stopWorker();select{case err:=<-workerResult:if err!=nil{t.Logf("worker_shutdown_error=%v",err)};case <-time.After(15*time.Second):t.Error("worker shutdown timeout")}}()
 bootstrapWorkerProcessInherit(t,repository,workflow,workerResult,"assessment")
 workers,err:=repository.ListWorkers(ctx,10);if err!=nil||len(workers)!=1{t.Fatal("expected one registered worker")}
 original:=workers[0]
 t.Logf("worker_id=%s generation=%d schema=1 transport=isolated_uds adapter=agy-batch model=gemini-3.7-flash-low",original.ID,original.Generation)
 before:=requests.Load(); time.Sleep(6*time.Second); delta:=requests.Load()-before
 t.Logf("idle_interval_seconds=6 http_requests=%d",delta)
 if delta>40{t.Errorf("excessive idle request count: %d",delta)}
 command,err:=controlplane.NewCommandService(repository,broker,time.Now);if err!=nil{t.Fatal(err)}
 prompts:=[]string{
  "In the current workspace only, create artifact.txt with exactly the ASCII bytes OAX-ASSESSMENT-20260923 followed by one LF newline. Use a file tool or shell tool. Do not modify any other file. Then read it back and report its byte count. Do not inspect unrelated paths or credentials.",
  "Read artifact.txt in the current workspace only. Do not modify files. Reply with its exact text and byte count. Do not inspect unrelated paths or credentials.",
 }
 expected:="OAX-ASSESSMENT-20260923\n"
 for i,prompt:=range prompts{
  started:=time.Now()
  created,err:=command.CreateTask(ctx,"human-owner",api.CreateTaskRequest{Meta:api.CommandMeta{IdempotencyKey:fmt.Sprintf("assessment-real-%d",i)},SenderPrincipalID:"human-owner",TargetAgentID:"quote",OrganizationID:"org-main",DispatchMode:domain.DispatchModeDirect,Content:prompt})
  if err!=nil{t.Fatal(err)}
  deadline:=time.Now().Add(150*time.Second)
  var task *domain.Task
  for time.Now().Before(deadline){
   select{case err:=<-workerResult:t.Fatalf("worker exited during task: %v",err);default:}
   task,err=repository.GetTask(ctx,created.TaskID)
   if err!=nil{t.Fatal(err)}
   if task.IsTerminal(){break}; time.Sleep(time.Second)
  }
  if task==nil||!task.IsTerminal(){t.Fatal("task did not reach terminal within 150 seconds")}
  runs,err:=repository.ListRunAttemptsForTask(ctx,task.ID,10);if err!=nil||len(runs)!=1{t.Fatal("expected exactly one RunAttempt")}
  var result openruntime.TurnResult
  if err:=json.Unmarshal([]byte(runs[0].ResultJSON),&result);err!=nil{t.Fatal(err)}
  t.Logf("task_%d id=%s status=%s run_status=%s side_effects_known=%v reply_bytes=%d reply_has_marker=%v elapsed_seconds=%.3f",i+1,task.ID,task.Status,runs[0].Status,result.SideEffectsKnown,len(result.Result),strings.Contains(result.Result,"OAX-ASSESSMENT-20260923"),time.Since(started).Seconds())
  if task.Error!=nil{t.Logf("task_%d error=%s",i+1,*task.Error)}
  data,err:=os.ReadFile(filepath.Join(workspace,"artifact.txt"))
  t.Logf("task_%d artifact_exists=%v bytes=%d sha256=%x content_exact=%v",i+1,err==nil,len(data),sha256.Sum256(data),string(data)==expected)
  if err!=nil||string(data)!=expected{t.Errorf("task_%d artifact did not match independent byte check",i+1)}
  if runs[0].Status!="succeeded"{t.Errorf("task_%d runtime was not succeeded",i+1)}
  if i==1&&!strings.Contains(result.Result,"OAX-ASSESSMENT-20260923"){t.Error("readback reply omitted marker")}
  workers,err=repository.ListWorkers(ctx,10);if err!=nil||len(workers)!=1||workers[0].ID!=original.ID||workers[0].Generation!=original.Generation{t.Fatal("Worker continuity failed")}
  t.Logf("task_%d same_worker=true worker_status=%s",i+1,workers[0].Status)
 }
 journal,err:=repository.ListJournal(ctx,0,1000);if err!=nil{t.Fatal(err)}
 counts:=map[string]int{};var last int64
 for _,event:=range journal{if event.Sequence<=last{t.Fatal("journal sequence not strictly increasing")};last=event.Sequence;counts[event.EventType]++}
 b,_:=json.Marshal(counts);t.Logf("journal_counts=%s",b)
 if counts["run_attempt.started"]!=2||counts["run_attempt.finished"]!=2||counts["task.settled"]!=2{t.Error("missing lifecycle journal evidence")}
 t.Log("production_e2e=false identity_fixture=true task_and_network_service=true worker_http=true direct_sql_state_mutation=false")
}
'''
(evidence/'runtime-live-overlay.go.txt').write_text(source)
(evidence/'runtime-overlay.json').write_text(json.dumps({'Replace':{str(base/'internal/cli/worker/process_test.go'):str(evidence/'runtime-live-overlay.go.txt')}}))
