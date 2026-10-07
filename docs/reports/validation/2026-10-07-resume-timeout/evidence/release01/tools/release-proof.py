import json,os,time
from pathlib import Path
p=Path(__file__).resolve().parent
with (p/'release-start.json').open('x') as f: json.dump({'pid':os.getpid()},f)
time.sleep(15)
with (p/'release-proof.txt').open('xb') as f: f.write(b'OAX_RELEASE_PROOF_OK\n')
print('OAX_RELEASE_PROOF_OK',flush=True)
