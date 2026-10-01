import sys, pathlib, subprocess, datetime, json, hashlib, os
label, cwd = sys.argv[1:3]
command = sys.argv[3:]
root = pathlib.Path(__file__).resolve().parent
meta = {"evidence_level": "D", "real_e2e": False, "command": command, "cwd": cwd, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat()}
for key, args in [("git_head", ["git", "rev-parse", "HEAD"]), ("git_tree", ["git", "rev-parse", "HEAD^{tree}"]), ("git_status_before", ["git", "status", "--porcelain"]), ("go_version", ["go", "version"])]:
    meta[key] = subprocess.check_output(args, cwd=cwd, text=True).strip()
with (root/(label+".stdout.log")).open("wb") as out, (root/(label+".stderr.log")).open("wb") as err:
    try:
        result = subprocess.run(command, cwd=cwd, stdout=out, stderr=err, timeout=1800)
        meta["exit_code"] = result.returncode
    except subprocess.TimeoutExpired:
        meta["exit_code"] = 124
        meta["timeout_seconds"] = 1800
meta["finished_at"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
meta["git_status_after"] = subprocess.check_output(["git", "status", "--porcelain"], cwd=cwd, text=True).strip()
meta["stdout_sha256"] = hashlib.sha256((root/(label+".stdout.log")).read_bytes()).hexdigest()
meta["stderr_sha256"] = hashlib.sha256((root/(label+".stderr.log")).read_bytes()).hexdigest()
(root/(label+".result.json")).write_text(json.dumps(meta, ensure_ascii=False, indent=2)+"\n")
print(json.dumps({k:meta[k] for k in ["command", "git_head", "exit_code", "started_at", "finished_at"]}, ensure_ascii=False))
sys.exit(meta["exit_code"])
