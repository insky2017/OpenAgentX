from pathlib import Path
p=Path(__file__).resolve().parent
with (p/'release-steer.txt').open('xb') as f: f.write(b'RELEASE_STEER_OK')
print('RELEASE_STEER_OK',flush=True)
