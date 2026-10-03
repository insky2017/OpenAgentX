#!/usr/bin/env python3
"""Supplement M04/M05/M06 against an existing isolated managed acceptance profile.

Default prints a plan only. --execute is a harness interlock, not another user
approval. Run only after the primary harness/idle window has finished. Never
writes SQLite, stops a daemon, or cleans up the whole profile. Optional
--include-cancel performs one bounded cancellation of a read-only sleep query.
"""
import argparse
import datetime
import fcntl
import http.client
import json
import os
from pathlib import Path
import shlex
import signal
import socket
import sys
import time

sys.dont_write_bytecode = True
from managed_collaboration_e2e import Acceptance, now, owned_stop, redact, require, sha, write

FINAL = {"succeeded", "failed", "uncertain", "canceled"}


def completed_primary_idle(root):
    """Read only: the newest idle-capable phase must have collected its PASS."""
    phases = {"run", "chain", "restart-chain", "native-continuation", "idle"}
    attempts = []
    for directory in [root / "evidence", *root.glob("evidence-*")]:
        if not directory.is_dir() or directory.name.startswith("evidence-recovery-"):
            continue
        provenance_path = directory / "attempt-provenance.json"
        provenance = json.loads(provenance_path.read_text()) if provenance_path.exists() else {}
        if provenance.get("phase") == "recovery":
            continue
        phase = provenance.get("phase", "run" if directory.name == "evidence" else None)
        # Fleet is a later, independent check. Its completion does not replace
        # the 30-minute idle handoff. Active work is rejected by quiescent().
        if phase in {"fleet", "fleet-observe-existing"}:
            continue
        # Include directories whose harness has not written provenance yet: a
        # partially started attempt must not accidentally expose an older PASS.
        marker = provenance_path if provenance_path.exists() else directory
        attempts.append((marker.stat().st_mtime_ns, directory, phase, provenance))
    require(attempts, "primary acceptance evidence is missing")
    _, directory, phase, provenance = max(attempts, key=lambda item: item[0])
    require(phase in phases, "latest primary phase has no completed idle handoff: " + directory.name)
    window_path = directory / "idle-window.json"
    result_path = directory / "idle-result.json"
    verdict_path = directory / ("verdict-" + phase + ".json")
    require(all(path.is_file() for path in [window_path, result_path, verdict_path]),
            "latest primary phase is incomplete; wait for its idle result and PASS verdict: " + directory.name)
    window, result, verdict = [json.loads(path.read_text()) for path in [window_path, result_path, verdict_path]]
    require(result.get("status") == "PASS" and verdict.get("status") == "PASS" and verdict.get("phase") == phase,
            "latest primary idle/phase did not pass")
    require(window.get("seconds", 0) >= 1800 and result.get("elapsed_seconds", 0) >= 1800,
            "primary idle evidence does not cover the required 30 minutes")
    started = datetime.datetime.fromisoformat(window["started_at"].replace("Z", "+00:00"))
    finished = datetime.datetime.fromisoformat(verdict["at"].replace("Z", "+00:00"))
    require(started.tzinfo is not None and finished.tzinfo is not None and finished >= started,
            "primary idle timestamps are inconsistent")
    require(window_path.stat().st_mtime_ns <= result_path.stat().st_mtime_ns <= verdict_path.stat().st_mtime_ns,
            "primary idle result/verdict predates its current window")
    manifest = directory / "SHA256SUMS"
    require(manifest.is_file() and manifest.stat().st_mtime_ns >= verdict_path.stat().st_mtime_ns,
            "primary collection has not finished after its PASS verdict")
    checksums = dict(line.split("  ", 1)[::-1] for line in manifest.read_text().splitlines())
    for path in [window_path, result_path, verdict_path]:
        require(checksums.get(path.name) == sha(path), "primary idle evidence SHA mismatch: " + path.name)
    if provenance.get("at"):
        attempt_started = datetime.datetime.fromisoformat(provenance["at"].replace("Z", "+00:00"))
        require(attempt_started.tzinfo is not None and attempt_started <= started,
                "primary idle window predates this phase attempt")
    return {"directory": str(directory), "phase": phase, "provenance": provenance,
            "window": window, "result": result, "verdict": verdict,
            "evidence_sha256": {path.name: sha(path) for path in [window_path, result_path, verdict_path]}}


def process(ref):
    try:
        raw = Path("/proc", str(ref["pid"]), "stat").read_text()
    except (FileNotFoundError, ProcessLookupError):
        return None
    fields = raw.rsplit(")", 1)[1].split()
    require(fields[19] == str(ref["starttime"]), "PID identity changed; refusing process operation")
    return None if fields[0] == "Z" else {**ref, "state": fields[0], "ppid": int(fields[1])}


def descendants(parent):
    rows = {}
    for stat in Path("/proc").glob("[0-9]*/stat"):
        try:
            raw = stat.read_text(); fields = raw.rsplit(")", 1)[1].split()
            rows[int(stat.parent.name)] = {"pid": int(stat.parent.name), "ppid": int(fields[1]),
                "starttime": fields[19], "comm": raw.split("(", 1)[1].rsplit(")", 1)[0]}
        except (OSError, ValueError, IndexError):
            continue
    found, parents = [], {parent}
    while parents:
        children = [entry for entry in rows.values() if entry["ppid"] in parents]
        found.extend(children); parents = {entry["pid"] for entry in children}
    return found


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost", timeout=15); self.path = str(path)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout); self.sock.connect(self.path)


class Recovery(Acceptance):
    def __init__(self, args):
        args.phase = "recovery"
        args.replace_binary = False
        super().__init__(args)
        self.attempt = self.out.name.removeprefix("evidence-")
        self.external_number = 0
        self.stopped_worker = None
        (self.out / "harness.py").write_bytes(Path(__file__).read_bytes())
        self.save("attempt-provenance.json", {"at": now(), "phase": "recovery", "harness_sha256": sha(__file__),
                  "binary_sha256": sha(args.binary), "source_commit": args.commit})

    def quiescent(self):
        tasks = self.all_tasks()
        require(all(task["status"] in FINAL for task in tasks), "fixture still has active/waiting work; do not overlap primary acceptance")
        return tasks

    def idle_guard(self):
        proof = completed_primary_idle(self.root)
        self.save("primary-idle-handoff.json", proof)
        return proof

    def external(self, agent, method, route, body=None, expected=200):
        # Only the fixture's dedicated communication credential is loaded. It is
        # never copied into a prompt, argv, log, or evidence document.
        path = Path(self.bindings[agent]["session_file"])
        require(path.resolve().is_relative_to((self.root / "profile").resolve()), "credential outside isolated profile")
        require(path.stat().st_mode & 0o077 == 0, "communication credential is not private")
        document = json.loads(path.read_text())
        credentials = [item for item in document["credentials"]
                       if item["username"] == agent and item["socket_path"] == str(self.socket)]
        require(len(credentials) == 1, "cannot uniquely select participant credential")
        credential = credentials[0]
        headers = {"Content-Type": "application/json", "Authorization": "Bearer " + credential["token"]}
        if body is not None and body.get("idempotency_key"):
            headers["Idempotency-Key"] = body["idempotency_key"]
        connection = UnixHTTP(self.socket)
        try:
            connection.request(method, "/api/external/v1/" + route,
                               body=None if body is None else json.dumps(body), headers=headers)
            response = connection.getresponse(); status = response.status
            data = json.loads(response.read())
        finally:
            connection.close()
        self.external_number += 1
        evidence = {"at": now(), "transport": "formal external UDS API", "agent": agent,
                    "method": method, "route": route, "request": body, "status": status, "response": data}
        raw = self.root / self.attempt / "external-api-raw" / f"{self.external_number:04}.json"
        raw.parent.mkdir(parents=True, exist_ok=True)
        raw.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n")
        self.save("external-api/" + f"{self.external_number:04}.json", evidence)
        require(status == expected, "unexpected external API status; see recorded response")
        return data

    def detail(self, task):
        return self.api("/api/observe/v1/tasks/" + task)

    def worker(self, agent):
        candidates = [entry for entry in self.procs
                      if (entry["label"] in {"worker-" + agent, "worker-replacement-" + agent}
                          or entry["label"].startswith("worker-" + agent + "-recovery-")) and process(entry)]
        require(len(candidates) == 1, "cannot identify exactly one owned fixture Worker")
        return candidates[0]

    def active_worker(self, agent):
        entries = [entry for entry in self.api("/api/observe/v1/overview").get("workers", [])
                   if entry["agent_id"] == agent and entry["status"] in {"online", "degraded"}]
        require(len(entries) <= 1, "multiple online Workers for fixture Agent")
        return entries[0] if entries else None

    def fact(self):
        path = self.workspace(self.agents[1]) / "facts.txt"
        value = path.read_text().strip()
        require(value.startswith("FACT-"), "primary fact fixture is missing")
        if hasattr(self, "baseline_fact"):
            require((value, sha(path)) == self.baseline_fact, "read-only query changed the independent fact fixture")
        return value, {"path": str(path), "sha256": sha(path), "value": value}

    def ask(self, label, content=None):
        a, b = self.agents
        body = {"kind": "consultation", "target_agent_id": b, "scope": "fixture." + b,
                "idempotency_key": self.attempt + "-" + label,
                "content": content or "Read facts.txt only in your assigned workspace. Reply with its exact contents, without extra text. Do not modify any file."}
        message = self.external(a, "POST", "messages", body)
        require(message.get("task_id"), "managed consultation did not commit a Task")
        if message["task_id"] not in self.tasks:
            self.tasks.append(message["task_id"])
        return message, body

    def reply_and_consume(self, message):
        a, b = self.agents
        answer = self.settle(message["task_id"], 420)
        def find_reply():
            inbox = self.external(a, "GET", "inbox?after=0&limit=100")
            require(len(inbox["messages"]) < 100, "inbox fixture requires pagination; do not silently omit replies")
            found = [item for item in inbox["messages"] if item.get("reply_to_message_id") == message["message_id"]]
            require(len(found) <= 1, "multiple results for one consultation")
            return found[0] if found else None
        reply = self.wait(find_reply, 420)
        envelope = json.loads(reply["content"])
        fact, proof = self.fact()
        require(envelope["status"] == "succeeded" and envelope["result"].strip() == fact, "reply does not match independent fact")
        require(reply.get("task_id"), "successful result has no continuation Task")
        consumption = self.settle(reply["task_id"], 420)
        require(consumption["task"]["result"].strip() == "CONSUMED:" + fact, "requester did not consume result")
        for agent, tid in [(b, message["task_id"]), (a, reply["task_id"])]:
            state = json.loads((self.root / "profile/workers/codex" / agent / "tasks" / (tid + ".json")).read_text())
            require(state["thread_id"] == self.bindings[agent]["thread_id"], "recovery forked native thread")
            self.save("thread-" + tid + ".json", state)
        return {"request": message, "reply": reply, "answer": answer, "consumption": consumption,
                "independent_fact": proof}

    def sleep_process(self, agent):
        parent = self.worker(agent)
        found = []
        for entry in descendants(parent["pid"]):
            if entry["comm"] != "sleep" or not process(entry):
                continue
            try:
                argv = Path("/proc", str(entry["pid"]), "cmdline").read_bytes().split(b"\0")
            except OSError:
                continue
            if len(argv) >= 2 and Path(os.fsdecode(argv[0])).name == "sleep" and argv[1] == str(self.a.sleep_seconds).encode():
                found.append({**entry, "argv": [x.decode() for x in argv if x]})
        require(len(found) <= 1, "ambiguous sleep process under fixture Worker")
        return found[0] if found else None

    def queued_while_busy(self):
        self.quiescent(); a, b = self.agents
        marker = "BUSY-DONE-" + self.attempt
        busy = self.create(b, "Use a shell tool to run exactly sleep " + str(self.a.sleep_seconds) +
                           ", wait for it to finish, then reply exactly " + marker +
                           ". Do not change files or run background commands.", self.bindings[b]["context_task"])
        sleep_ref = self.wait(lambda: self.sleep_process(b), 120)
        require(self.detail(busy)["task"]["status"] == "running", "observed sleep does not belong to an active fixture Task")
        self.save("M04-sleep-process.json", sleep_ref)
        message, _ = self.ask("busy-queue")
        samples = []
        while process(sleep_ref):
            current = self.detail(message["task_id"])
            if not process(sleep_ref):
                break
            require(current["task"]["status"] == "queued" and not current["run_attempts"], "consultation ran concurrently with busy Agent")
            samples.append({"at": now(), "sleep": process(sleep_ref), "task": current["task"], "run_count": 0})
            time.sleep(2)
        require(len(samples) >= 2, "sleep window too short to prove queued work; preserve failure instead of retrying")
        self.save("M04-queue-samples.json", samples)
        busy_detail = self.settle(busy, 240)
        require(busy_detail["task"]["result"].strip() == marker, "busy query failed")
        proof = self.reply_and_consume(message)
        finished = datetime.datetime.fromisoformat(busy_detail["run_attempts"][0]["finished_at"].replace("Z", "+00:00"))
        started = datetime.datetime.fromisoformat(proof["answer"]["run_attempts"][0]["started_at"].replace("Z", "+00:00"))
        require(started >= finished, "persisted Run intervals overlap")
        self.save("M04-result.json", {"status": "PASS", "busy_task": busy_detail, "samples": len(samples),
                  "run_intervals_do_not_overlap": True, **proof})

    def restart_worker(self):
        agent = self.agents[1]
        require(self.stopped_worker is not None and not process(self.stopped_worker), "old Worker is still alive")
        self.save("M05-retired-worker.json", {"at": now(), "process": self.stopped_worker, "alive": False})
        self.procs = [entry for entry in self.procs if entry != self.stopped_worker]
        config = self.root / "profile/workers" / (agent + ".yaml")
        envfile = config.with_suffix(".env")
        saved = self.env; self.env = dict(saved)
        try:
            for line in envfile.read_text().splitlines():
                if line.strip() and not line.startswith("#"):
                    pair = shlex.split(line); require(len(pair) == 1, "unsupported EnvironmentFile line")
                    key, value = pair[0].split("=", 1); self.env[key] = value
            self.start("worker-" + agent + "-" + self.attempt,
                       [str(self.a.binary), "worker", "run", "--config", str(config)])
        finally:
            self.env = saved
        self.stopped_worker = None
        current = self.wait(lambda: self.active_worker(agent), 90)
        self.cli("M05-resume-after-restart-" + agent,
                 ["agent", "resume", agent, "--no-open", "--wait", "3m"])
        self.save("M05-restarted-worker.json", {"at": now(), "worker": current,
                  "config_sha256": sha(config), "environment_sha256": sha(envfile),
                  "scope": "same Agent/config; registration instance or generation may change"})
        return current

    def finish_existing(self, directory):
        """Collect an already completed batch without dispatching model work."""
        directory = directory.resolve(strict=True)
        require(directory.parent == self.root and directory.name.startswith("evidence-recovery-"),
                "finish-existing must reference this isolated profile's recovery evidence")
        manifest = directory / "SHA256SUMS"
        checksums = dict(line.split("  ", 1)[::-1] for line in manifest.read_text().splitlines())
        proofs = {}
        for name in ["M04-result.json", "M05-result.json", "M06-rejections.json", "M05-before-stop.json"]:
            path = directory / name
            require(checksums.get(name) == sha(path), "existing recovery evidence SHA mismatch: " + name)
            proofs[name] = json.loads(path.read_text())
        require(all(proofs[name].get("status") == "PASS" for name in
                    ["M04-result.json", "M05-result.json", "M06-rejections.json"]), "existing cases have not passed")
        old = proofs["M05-before-stop.json"]["process"]
        require(not process(old), "retired Worker unexpectedly alive")
        self.save("retired-worker.json", {"process": old, "alive": False, "source": str(directory)})
        self.procs = [entry for entry in self.procs if entry != old]
        write(self.root / "processes.json", self.procs)
        for label in ["M04-result.json", "M05-result.json"]:
            proof = proofs[label]
            task_ids = [proof["answer"]["task"]["id"], proof["consumption"]["task"]["id"]]
            if label == "M04-result.json":
                task_ids.append(proof["busy_task"]["task"]["id"])
            for task_id in task_ids:
                self.settle(task_id, 30)
        self.save("completed-cases-reused.json", {"at": now(), "source": str(directory),
                  "source_manifest_sha256": sha(manifest), "cases": proofs, "new_model_tasks": 0})

    def offline_queue(self):
        self.quiescent(); a, b = self.agents
        before = self.active_worker(b); require(before, "target Worker is not online")
        ref = self.worker(b); config = self.root / "profile/workers" / (b + ".yaml")
        config_hash = sha(config)
        self.save("M05-before-stop.json", {"worker": before, "process": ref, "config_sha256": config_hash})
        # Graceful signal to this exact Worker PID only, never killpg/all-profile.
        require(process(ref), "owned Worker disappeared before stop")
        os.kill(ref["pid"], signal.SIGTERM)
        self.wait(lambda: not process(ref), 45)
        self.stopped_worker = ref
        self.wait(lambda: self.active_worker(b) is None, 30)
        # Reuse the primary harness ownership verifier on a one-process manifest
        # AFTER graceful exit. It must be a no-op, never a force-stop fallback.
        scoped = self.root / self.attempt / "stopped-worker-verification"
        write(scoped / "processes.json", [ref])
        stopped = owned_stop(scoped)
        require(len(stopped) == 1 and stopped[0].get("already_stopped"), "stop verification was not a no-op")
        self.save("M05-owned-stop-verification.json", stopped)
        task_ids = {task["id"] for task in self.all_tasks()}
        message, body = self.ask("offline-queue")
        duplicate = self.external(a, "POST", "messages", body)
        require((duplicate["message_id"], duplicate["task_id"]) == (message["message_id"], message["task_id"]), "same-key offline retry duplicated work")
        added = {task["id"] for task in self.all_tasks()} - task_ids
        require(added == {message["task_id"]}, "offline enqueue created anything other than one Task")
        for _ in range(3):
            detail = self.detail(message["task_id"])
            require(detail["task"]["status"] == "queued" and not detail["run_attempts"], "offline work was lost or ran before restart")
            time.sleep(2)
        self.save("M05-offline-queued.json", {"request": message, "duplicate": duplicate, "task": detail,
                  "mailbox": self.api("/api/observe/v1/mailboxes?agent_id=" + b)})
        self.restart_worker()
        require(sha(config) == config_hash, "restart changed Worker configuration")
        proof = self.reply_and_consume(message)
        self.save("M05-result.json", {"status": "PASS", "before_worker": before, "one_durable_task": True, **proof})

    def rejection(self):
        self.quiescent(); a, b = self.agents
        before = {task["id"] for task in self.all_tasks()}
        cases = [("unknown-scope", a, b, "fixture.unknown", "consultation", 422, "UNKNOWN_SCOPE"),
                 ("wrong-owner", b, a, "fixture." + b, "consultation", 422, "OUT_OF_SCOPE"),
                 ("action-request", a, b, "fixture." + b, "request", 400, "INVALID_INPUT")]
        evidence = []
        for label, sender, target, scope, kind, status, code in cases:
            result = self.external(sender, "POST", "messages", {"target_agent_id": target, "scope": scope,
                "kind": kind, "content": "Read facts.txt only; this fixture verifies protocol rejection, no change is authorized.",
                "idempotency_key": self.attempt + "-" + label}, status)
            require(result.get("code") == code, "wrong rejection reason for " + label)
            require({task["id"] for task in self.all_tasks()} == before, "rejected request created a Task")
            evidence.append({"case": label, "expected_status": status, "response": result})
        self.save("M06-rejections.json", {"status": "PASS", "new_tasks": 0, "cases": evidence})

    def cancel_consultation(self):
        self.quiescent(); a, b = self.agents
        message, _ = self.ask("cancel-once", "Read facts.txt only, then use a shell tool to run sleep " +
                              str(self.a.sleep_seconds) + " before returning its exact contents. Wait in the foreground. Do not modify files.")
        ref = self.wait(lambda: self.sleep_process(b), 120)
        before = self.detail(message["task_id"])
        self.save("M06-cancel-before.json", {"sleep_process": ref, "task": before})
        self.api("/api/control/v1/tasks/" + message["task_id"] + "/cancel",
                 {"meta": {"expected_version": before["task"]["version"], "idempotency_key": self.attempt + "-cancel"}})
        def settled():
            detail = self.detail(message["task_id"])
            return detail if detail["task"]["status"] in FINAL else None
        detail = self.wait(settled, 150)
        require(detail["task"]["status"] in {"canceled", "uncertain"}, "canceled consultation reported success")
        self.wait(lambda: not process(ref), 45)
        request = self.external(b, "GET", "messages/" + message["message_id"])
        require(request.get("managed_state") == "needs_review", "cancel outcome lacks review state")
        def returned():
            replies = self.external(a, "GET", "inbox?after=0&limit=100")["messages"]
            match = [item for item in replies if item.get("reply_to_message_id") == message["message_id"]]
            require(len(match) <= 1, "cancellation duplicated result")
            return match[0] if match else None
        reply = self.wait(returned, 30)
        require(not reply.get("task_id"), "canceled/uncertain result automatically resumed work")
        envelope = json.loads(reply["content"])
        require(envelope["status"] == detail["task"]["status"], "reply hides failed physical outcome")
        self.save("M06-cancellation.json", {"status": "PASS", "task": detail, "request": request,
                  "reply": reply, "sleep_process_alive": False, "automatic_retry": False})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--web", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--include-cancel", action="store_true")
    parser.add_argument("--finish-existing", type=Path, help="Only collect a completed recovery batch; never dispatch new work")
    parser.add_argument("--sleep-seconds", type=int, default=75)
    args = parser.parse_args()
    require(45 <= args.sleep_seconds <= 90, "bounded sleep must be 45..90 seconds")
    if not args.execute:
        print(json.dumps({"status": "PREPARED_NOT_RUN", "cases": ["M04 queued while busy", "M05 graceful Worker restart", "M06 rejected scope/owner/action"],
                          "cancel": "one read-only consultation" if args.include_cancel else "NOT_RUN",
                          "execution": "only after the primary idle window, add --execute"}))
        return
    os.umask(0o077)
    args.binary = args.binary.resolve(strict=True); args.web = args.web.resolve(strict=True)
    # Gate before reading credentials or constructing a resumed harness. A
    # rejected overlap must not call collect()/checkpoint() on the live primary.
    completed_primary_idle(args.root.resolve())
    lock = (args.root / ".managed-recovery.lock").open("a")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    run = Recovery(args); stage = "preflight"; may_collect = False
    try:
        run.idle_guard(); may_collect = True
        run.login(); run.quiescent()
        fact, fact_proof = run.fact()
        run.baseline_fact = (fact, fact_proof["sha256"])
        run.save("independent-fact-before.json", fact_proof)
        for agent in run.agents:
            binding = run.external(agent, "GET", "status")
            require(binding["agent_id"] == agent and binding["mode"] == "managed" and
                    binding["thread_id"] == run.bindings[agent]["thread_id"], "fixture identity/thread mismatch")
        run.save("provenance.json", {"at": now(), "binary_sha256": sha(args.binary), "source_commit": args.commit,
                  "harness_sha256": sha(__file__), "primary_harness_sha256": sha(Path(__file__).with_name("managed_collaboration_e2e.py")),
                  "scope": "existing isolated profile; formal APIs; same Agent/config Worker restart; no production services"})
        if args.finish_existing:
            require(not args.include_cancel, "finish-existing cannot dispatch cancellation")
            stage = "finish-existing"; run.finish_existing(args.finish_existing)
        else:
            stage = "M06-rejections"; run.rejection()
            stage = "M04-busy"; run.queued_while_busy()
            stage = "M05-restart"; run.offline_queue()
            if args.include_cancel:
                stage = "M06-cancellation"; run.cancel_consultation()
        run.quiescent()
        stage = "final-collection"
        run.idle_snapshot(run.attempt + "-final-rollouts")
        run.save("verdict.json", {"status": "PASS", "at": now(), "M04": "PASS", "M05": "PASS", "M06_rejections": "PASS",
                  "M06_cancellation": "PASS" if args.include_cancel else "NOT_RUN", "idle_window": "prior evidence retained; no concurrent supplementation"})
        print(json.dumps({"status": "PASS", "evidence": str(run.out)}), flush=True)
    except Exception as error:
        run.save("failure.json", {"status": "FAIL", "stage": stage, "at": now(),
                  "error": str(error).replace(run.password, "[REDACTED]"), "no_automatic_work_retry": True})
        raise
    finally:
        # Restoring an idle stopped Worker is not replaying a failed model turn.
        # Do not issue a second cancel, regenerate a Task, or kill a process tree.
        if run.stopped_worker is not None:
            try:
                run.restart_worker()
            except Exception as error:
                run.save("restore-worker-failure.json", {"error": str(error), "requires_operator_review": True})
        if may_collect:
            run.collect()
        lock.close()


if __name__ == "__main__":
    main()
