import datetime,json,time
from pathlib import Path
p=Path(__file__).resolve().parent
start=time.monotonic()
a={"started_at":datetime.datetime.now(datetime.timezone.utc).isoformat(),"required_seconds":1860}
(p/"proof-start.json").write_text(json.dumps(a))
for i in range(31):
 time.sleep(60)
 print("OAX_LONG_PROGRESS",i+1,flush=True)
a.update(ended_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),elapsed_seconds=time.monotonic()-start)
(p/"proof-end.json").write_text(json.dumps(a))
print("OAX_LONG_COMPLETED",flush=True)
