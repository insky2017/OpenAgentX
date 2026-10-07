#!/usr/bin/env python3
"""Isolated real Agent session handoff acceptance; never touches production.

Model work starts only with explicit `run`. Each attempt needs a new private root.
All domain writes use the product CLI/API. SQLite reads only hash existing history
and independently verify session bindings; they do not set up or repair state.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import signal
import sqlite3
import subprocess
import sys
import time

sys.dont_write_bytecode = True
from native_model_settings_e2e import SettingsRun
from terminal_overview_e2e import AttachedClient, now, require, sha

TERMINAL = {"succeeded", "failed", "uncertain", "canceled", "waiting_input"}


class HandoffRun(SettingsRun):
    def cli(self, label, args, expected=0, timeout=240):
        # The setup's exact role response remains compatible with TerminalRun.
        if label == "01-join":
            role = Path(args[args.index("--role") + 1])
            role.write_text(
                "You are the isolated OpenAgentX session handoff verification Agent. "
                "Only your assigned workspace is in scope. Do not spawn subagents. "
                "Never inspect credentials or unrelated paths, change services or files, or run background work. "
                "Execute an explicitly requested read-only sleep command once when testing busy state. "
                "For a handoff only acknowledge receiving the handoff; do not execute inherited business work. "
                "For a consultation return the requested answer; OAX automatically sends the reply. "
                "When asked to confirm your role/workspace during initialization, reply exactly: " + self.expected + "\n")
        argv = [str(self.a.binary), *map(str, args)]
        # Setup init/login require an owned PTY, handled by the existing helper.
        if label in {"01-join", "02-init", "03-apply", "04-console-login", "05-resume"}:
            from codex_local_setup import Setup
            return Setup.cli(self, label, list(map(str, args)))
        started = now()
        result = subprocess.run(argv, env=self.env, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, timeout=timeout)
        output = (result.stdout + result.stderr).replace(self.password, "[REDACTED]")
        self.save("cli/" + label + "-" + str(time.time_ns()) + ".json", {
            "started_at": started, "finished_at": now(), "argv": argv,
            "exit_code": result.returncode, "stdout": result.stdout.replace(self.password, "[REDACTED]"),
            "stderr": result.stderr.replace(self.password, "[REDACTED]")})
        require(result.returncode == expected if expected is not None else result.returncode != 0,
                label + ": unexpected CLI exit; see retained evidence")
        if result.returncode == 0:
            try:
                return json.loads(result.stdout)
            except ValueError:
                pass
        return output

    def state(self):
        return json.loads((self.root / "profile/workers/codex" / self.aid / "state.json").read_text())

    def session(self):
        result = self.api("/api/console/v1/agents/" + self.aid + "/session?backend_id=codex")
        require(result["agent_id"] == self.aid and result["backend_id"] == "codex", "wrong active session identity")
        return result

    def all_tasks(self):
        result = self.api("/api/observe/v1/tasks?limit=100")
        require(not result.get("has_more"), "fixture exceeded one page")
        return [t for t in result["tasks"] if t["target_agent_id"] == self.aid]

    def task_set(self):
        return {t.get("id", t.get("task_id")) for t in self.all_tasks()}

    def create_query(self, content, thread=None):
        body = {"target_agent_id": self.aid, "organization_id": "default", "dispatch_mode": "direct",
                "intent": "query", "content": content}
        if thread is not None:
            body["runtime_session"] = {"backend_id": "codex", "provider_session_id": thread}
        task = self.api("/api/control/v1/tasks", body)["task_id"]
        self.task_ids.append(task)
        return task

    def settle(self, task, expected=None):
        def done():
            detail = self.api("/api/observe/v1/tasks/" + task)
            return detail if detail["task"]["status"] in TERMINAL else None
        detail = self.wait(done, 420)
        self.save("tasks/" + task + ".json", detail)
        require(detail["task"]["status"] == "succeeded", "Task did not succeed: " + task)
        require(detail["task"]["intent"] == "query" and detail["task"]["completion_basis"] == "query_result_delivered",
                "query completion evidence missing")
        require(len(detail["run_attempts"]) == 1 and detail.get("events"), "missing single Run/Journal proof")
        run = detail["run_attempts"][0]
        require(run["status"] == "succeeded" and run["adapter_id"] == "codex-app-server", "Runtime failed")
        require(run["model"] == "gpt-6-astra", "model inheritance changed")
        if run.get("reasoning_mode") == "effort":
            require(run.get("reasoning_value") in {"high", "xhigh", "max", "ultra"}, "effort below high")
        self.save("runs/" + run["run_id"] + ".json", self.api("/api/observe/v1/run-attempts/" + run["run_id"]))
        if expected is not None:
            require((detail["task"].get("result") or "").strip() == expected, "exact model reply mismatch")
        return detail

    def task_thread(self, task):
        path = self.root / "profile/workers/codex" / self.aid / "tasks" / (task + ".json")
        state = json.loads(path.read_text())
        self.save("thread/" + task + ".json", state)
        require(state.get("thread_id"), "task-specific runtime thread evidence missing")
        return state["thread_id"]

    def first_turn_evidence(self, thread):
        """Read only this owned thread before any later query is submitted."""
        codex_dir = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
        current = datetime.datetime.now(datetime.timezone.utc)
        days = {current.strftime("%Y/%m/%d"), (current - datetime.timedelta(days=1)).strftime("%Y/%m/%d")}
        files = [p for day in days for p in (codex_dir / "sessions" / day).glob("*" + thread + ".jsonl")]
        require(len(files) == 1, "owned new-thread rollout is not uniquely available")
        selected, tools, contexts = [], [], []
        for line in files[0].read_text().splitlines():
            item = json.loads(line)
            payload = item.get("payload", {})
            kind = item.get("type")
            if kind == "turn_context":
                context = {k: payload.get(k) for k in ["cwd", "model", "effort", "approval_policy", "sandbox_policy"]}
                contexts.append(context)
                selected.append({"type": kind, "timestamp": item.get("timestamp"), "payload": context})
            elif kind == "response_item" and payload.get("type") in {"function_call", "custom_tool_call", "web_search_call", "computer_call"}:
                tools.append({"type": payload.get("type"), "name": payload.get("name")})
            elif kind == "event_msg" and payload.get("type") in {"task_started", "task_complete", "agent_message"}:
                selected.append(item)
        self.save("handoff-first-turn-runtime.json", {"thread_id": thread, "source": str(files[0]),
                  "source_sha256": sha(files[0]), "events": selected, "tool_calls": tools})
        require(contexts and all(c["cwd"] == str(self.root / "workspace") for c in contexts), "new thread workspace changed")
        require(all(c["model"] == "gpt-6-astra" for c in contexts), "new thread actual model changed")
        require(not tools, "handoff first turn executed tools")

    def db_snapshot(self, label):
        path = self.root / "profile/data/openagentx.db"
        db = sqlite3.connect(path.as_uri() + "?mode=ro", uri=True)
        db.row_factory = sqlite3.Row
        result = {"at": now(), "access": "SQLite mode=ro; hashes only for history", "tables": {}}
        try:
            # One consistent read transaction, never a domain-state write.
            db.execute("BEGIN")
            for table, key, predicate in [
                ("tasks", "task_id", "target_agent_id"),
                ("run_attempts", "run_id", "agent_id"),
                ("session_bindings", "session_binding_id", "agent_id"),
            ]:
                rows = db.execute("SELECT * FROM " + table + " WHERE " + predicate + "=?", (self.aid,)).fetchall()
                result["tables"][table] = {row[key]: hashlib.sha256(json.dumps(dict(row), sort_keys=True).encode()).hexdigest() for row in rows}
            result["bindings"] = [dict(row) for row in db.execute(
                "SELECT session_binding_id,context_id,agent_id,backend_id,provider_session_id,state,version "
                "FROM session_bindings WHERE agent_id=? ORDER BY context_id", (self.aid,))]
            db.rollback()
        finally:
            db.close()
        self.save(label + ".json", result)
        return result

    def invariant_snapshot(self, label):
        # WorkerHealth persists updated_at independently of the preview. Keep
        # every routing/lifecycle field, and record the unfiltered source too.
        state = self.state()
        self.save(label + "-raw-state.json", state)
        result = {"session": self.session(), "tasks": sorted(self.task_set()),
                  "state": {key: value for key, value in state.items() if key != "updated_at"},
                  "workspace": {p.name: sha(p) for p in (self.root / "workspace").iterdir() if p.is_file()}}
        self.save(label + ".json", result)
        return result

    def handoff_args(self, old, key):
        return ["agent", "new-session", self.aid, "--handoff-file", str(self.root / "SESSION-HANDOFF.md"),
                "--apply", "--expected-thread", old["thread_id"], "--expected-version", str(old["version"]),
                "--key", key, "--wait", "3m"]

    def configure_old_thread(self, thread):
        config = self.root / "profile/workers" / (self.aid + ".yaml")
        source = config.read_text()
        # Explicit private fixture input, not an active-pointer/state repair.
        require(source.count("options:") == 1, "fixture must have one runtime backend")
        if re.search(r"(?m)^\s+thread_id:", source):
            changed = re.sub(r"(?m)^(\s+)thread_id:.*$", r"\1thread_id: " + thread, source)
        else:
            changed = re.sub(r"(?m)^(\s*)options:\s*$", lambda m: m.group(0) + "\n" + m.group(1) + "  thread_id: " + thread, source)
        require(changed != source, "old Config.ThreadID fixture was not set")
        config.write_text(changed)
        self.save("legacy-config-input.json", {"path": str(config), "thread_id": thread,
                  "before_sha256": hashlib.sha256(source.encode()).hexdigest(), "after_sha256": sha(config),
                  "scope": "owned fixture config only; demonstrates durable pointer wins over old Config.ThreadID"})

    def peer_setup(self, context_task):
        self.peer = "handoff-peer-" + secrets.token_hex(4)
        identity = self.root / "peer-identity.json"
        identity.write_text(json.dumps({"version": 1, "agent_id": self.peer, "principal_id": "agent-" + self.peer,
            "organization_id": "default", "display_name": "Session handoff isolated external peer",
            "profile": {"instructions_path": str(self.root / "workspace/ROLE.md"), "workspace_root": str(self.root / "workspace")}}))
        self.cli("peer-apply", ["agent", "apply", "--file", identity])
        scope = "fixture." + self.aid
        roles = self.root / "roles.json"
        roles.write_text(json.dumps({"organization_id": "default", "rules": [{"scope": scope,
            "owner_agent_id": self.aid, "description": "Read-only session handoff nonce confirmation; no tools or business execution."}]}))
        self.cli("peer-roles", ["collaborate", "roles", "apply", "--file", roles, "--expected-version", "0"])
        self.binding_before = self.cli("managed-bind", ["collaborate", "enable", "--agent", self.aid,
            "--peers", self.peer, "--context-task", context_task, "--backend", "codex"])["binding"]
        self.cli("external-peer-bind", ["external", "bind", "--agent", self.peer, "--peers", self.aid,
            "--host", "isolated-handoff-harness", "--thread", "external-peer-" + secrets.token_hex(4)])
        self.save("managed-binding-before.json", self.binding_before)

    def peer_verify(self, session):
        current = self.cli("managed-binding-after", ["collaborate", "status", "--agent", self.aid])
        for key in ["binding_id", "agent_id", "principal_id", "organization_id", "mode", "state", "allowed_peer_agent_ids"]:
            require(current[key] == self.binding_before[key], "managed binding identity/peers changed: " + key)
        require(current["generation"] == self.binding_before["generation"] + 1, "managed generation did not advance once")
        require(current["thread_id"] == session["thread_id"] and current["managed_context_task_id"] == session["context_task_id"],
                "managed context was not moved to new handoff")
        # Existing Agent credential must remain valid, rather than re-binding.
        self.save("managed-binding-after.json", current)
        question = self.root / "peer-question.txt"
        question.write_text("Do not use tools. From your received session handoff, return only the value of HANDOFF_NONCE, with no extra text.\n")
        request = self.cli("peer-consultation", ["collaborate", "ask", "--agent", self.peer, "--to", self.aid,
            "--scope", "fixture." + self.aid, "--key", "handoff-consultation-" + self.nonce,
            "--content-file", question])
        def reply():
            inbox = self.cli("peer-inbox", ["collaborate", "inbox", "--agent", self.peer, "--all"])
            answers = [m for m in inbox["messages"] if m.get("reply_to_message_id") == request["message_id"]]
            require(len(answers) <= 1, "duplicate collaboration reply")
            return answers[0] if answers else None
        answer = self.wait(reply, 420)
        envelope = json.loads(answer["content"])
        require(envelope["status"] == "succeeded" and envelope["result"].strip() == self.nonce,
                "external consultation did not receive nonce from new context")
        detail = self.cli("peer-request-status", ["collaborate", "status", "--agent", self.aid,
                          "--message", request["message_id"]])
        self.settle(detail["task_id"], self.nonce)
        require(self.task_thread(detail["task_id"]) == session["thread_id"], "managed consultation used old thread")
        self.save("managed-consultation.json", {"request": request, "reply": answer, "result": envelope,
                  "no_peer_worker": True, "peer_auto_continuation_tested": False})

    def native_close(self):
        pane = self.windows["native"] + ".0"
        self.tmux("send-keys", "-t", pane, "C-d")
        self.wait(lambda: self.tmux("display-message", "-p", "-t", pane, "#{pane_dead}") == "1", 45)
        require(not self.lock_held(), "native lock not released")

    def native_open(self, label, thread):
        pane = self.windows["native"] + ".0"
        launcher = self.script(label, [self.a.binary, "agent", "open", self.aid, "--native"])
        self.tmux("respawn-pane", "-t", pane, launcher)
        self.wait(self.lock_held, 60)
        self.wait(lambda: "OpenAgentX · " + self.aid in self.tmux("capture-pane", "-p", "-S", "-", "-t", pane), 60)
        require(self.state()["thread_id"] == thread, "reopened native view has wrong runtime thread")
        self.capture(pane, label, False)
        # Inspect only the owned pane process tree, without unrelated argv/env.
        pid = int(self.tmux("display-message", "-p", "-t", pane, "#{pane_pid}"))
        queue, seen, proof = [pid], set(), []
        while queue:
            child = queue.pop()
            if child in seen:
                continue
            seen.add(child)
            require(len(seen) <= 128, "owned process tree unexpectedly large")
            proc = Path("/proc") / str(child)
            try:
                argv = proc.joinpath("cmdline").read_bytes().decode().strip("\0").split("\0")
                if "resume" in argv and "--remote" in argv:
                    proof.append({"pid": child, "argv": argv})
                for children in proc.glob("task/*/children"):
                    queue.extend(map(int, children.read_text().split()))
            except (FileNotFoundError, ProcessLookupError):
                continue
        require(any(thread in p["argv"] for p in proof), "native actual resume argv did not name new thread")
        self.save(label + "-process-proof.json", proof)

    def restart_worker(self):
        old = next(p for p in reversed(self.procs) if p["label"].startswith("worker"))
        def alive():
            try:
                fields = (Path("/proc") / str(old["pid"]) / "stat").read_text().rsplit(")", 1)[1].split()
            except FileNotFoundError:
                return False
            require(fields[19] == str(old["starttime"]), "owned Worker PID reused")
            return fields[0] != "Z"
        require(self.state()["state"] == "idle", "restart requires settled fixture")
        self.save("restart-before.json", {"worker": old, "session": self.session(), "state": self.state()})
        os.kill(old["pid"], signal.SIGTERM)
        self.wait(lambda: not alive(), 45)
        config = self.root / "profile/workers" / (self.aid + ".yaml")
        base = self.env.copy()
        for line in config.with_suffix(".env").read_text().splitlines():
            if line.strip() and not line.startswith("#"):
                pair = shlex.split(line)
                require(len(pair) == 1, "invalid owned EnvironmentFile")
                key, value = pair[0].split("=", 1)
                self.env[key] = value
        self.start("worker-restarted", [str(self.a.binary), "worker", "run", "--config", str(config)])
        self.env = base
        self.wait(lambda: any(w["agent_id"] == self.aid and w["status"] == "online"
                             for w in self.api("/api/observe/v1/overview")["workers"]), 120)
        self.cli("restart-resume", ["agent", "resume", self.aid, "--password-file", self.root / "password", "--no-open", "--wait", "3m"])
        self.wait(lambda: self.api("/api/console/v1/attach?agent_id=" + self.aid).get("backend_health", {}).get("codex") == "healthy", 90)
        self.save("restart-after.json", {"worker": self.procs[-1], "session": self.session(), "state": self.state()})

    def cleanup_owned(self):
        """Stop only settled, recorded fixture processes; retain all evidence."""
        require(self.state()["state"] == "idle", "cleanup retained unresolved runtime")
        require(not self.session().get("pending_task_id"), "cleanup retained pending handoff")
        require(all(t["status"] in {"succeeded", "failed", "uncertain", "canceled"} for t in self.all_tasks()),
                "cleanup retained nonterminal Task")
        self.save("cleanup-before.json", {"at": now(), "session": self.session(), "state": self.state(), "processes": self.procs})
        pane = self.windows.get("native", "")
        if pane and self.tmux("display-message", "-p", "-t", pane + ".0", "#{pane_dead}") == "0":
            if self.client is None:
                self.client = AttachedClient(self)
            self.native_close()
        if self.client:
            self.client.close()
            self.client = None
        stopped = []
        for proc in reversed(self.procs):
            require(proc["label"] == "daemon" or proc["label"].startswith("worker"), "unexpected fixture process label")
            def alive():
                try:
                    fields = (Path("/proc") / str(proc["pid"]) / "stat").read_text().rsplit(")", 1)[1].split()
                except FileNotFoundError:
                    return False
                require(fields[19] == str(proc["starttime"]), "cleanup refused reused PID")
                return fields[0] not in {"Z", "X"}
            if alive():
                os.kill(proc["pid"], signal.SIGTERM)
                self.wait(lambda: not alive(), 45)
            stopped.append({**proc, "stopped": True})
        self.tmux("kill-server")
        self.save("cleanup.json", {"at": now(), "processes": stopped, "owned_tmux_server": self.server,
                  "evidence_preserved": True, "production_touched": False})

    def manifest(self):
        (self.out / "SHA256SUMS").write_text("\n".join(sha(p) + "  " + str(p.relative_to(self.out))
            for p in sorted(self.out.rglob("*")) if p.is_file() and p.name != "SHA256SUMS") + "\n")

    def run_acceptance(self):
        self.prepare()  # existing real CLI setup, model initialization and native PTY
        self.client = AttachedClient(self)
        self.nonce = "SESSION-HANDOFF-" + secrets.token_hex(12)
        self.save("acceptance-inputs.json", {"nonce": self.nonce, "max_model_runs": 6,
                  "harness_sha256": sha(__file__), "candidate_sha256": sha(self.a.binary), "commit": self.a.commit})
        initial_id = next(iter(self.task_set()))
        initial = self.settle(initial_id, self.expected)
        # Verified contract: RunAttemptReadModel + panel.projectRunAttempt
        # expose the frozen AgentInput snapshot digest, with omitempty.
        role_digest = initial["run_attempts"][0].get("instructions_sha256")
        require(role_digest == sha(self.root / "workspace/ROLE.md"), "initial Run lacks matching frozen role digest")
        old = self.session()
        require(old["thread_id"] == self.task_thread(initial_id), "bootstrap session does not match native initialization")
        self.configure_old_thread(old["thread_id"])
        self.peer_setup(initial_id)
        # Version-zero bootstrap now comes from the newly installed managed
        # binding. Capture that stable source after binding setup.
        old = self.session()
        handoff = self.root / "SESSION-HANDOFF.md"
        handoff.write_text("# Isolated session handoff\n\nHANDOFF_NONCE=" + self.nonce + "\n\n"
            "Keep the same Agent identity, role and workspace. This handoff conveys context only; no business execution is authorized. "
            "For the first acknowledgement do not use tools; briefly confirm your Agent identity, workspace and the HANDOFF_NONCE. "
            "On a later explicit query asking for HANDOFF_NONCE, reply with its exact value only.\n")
        before = self.invariant_snapshot("preview-before")
        history = self.db_snapshot("preview-history-before")
        self.cli("preview", ["agent", "new-session", self.aid])
        after = self.invariant_snapshot("preview-after")
        after_history = self.db_snapshot("preview-history-after")
        require(before == after and history["tables"] == after_history["tables"], "preview wrote Agent/session/task/history state")
        busy_id = self.create_query("Run the read-only shell command `sleep 40` exactly once. Then reply exactly BUSY_DONE. Do not read or change files.", thread=old["thread_id"])
        self.wait(lambda: self.api("/api/observe/v1/tasks/" + busy_id)["task"]["status"] == "running", 90)
        tasks = self.task_set()
        self.cli("busy-reject", self.handoff_args(old, "busy-" + self.nonce), expected=None)
        require(self.task_set() == tasks and self.session() == old, "busy request created a task or changed pointer")
        self.settle(busy_id, "BUSY_DONE")
        require(self.task_thread(busy_id) == old["thread_id"], "busy query unexpectedly changed old thread")
        old_history = self.db_snapshot("old-history-settled")
        key = "switch-" + self.nonce
        tasks = self.task_set()
        self.cli("handoff-apply", self.handoff_args(old, key))
        new_ids = self.task_set() - tasks
        require(len(new_ids) == 1, "handoff did not create exactly one Task")
        handoff_id = next(iter(new_ids))
        first = self.settle(handoff_id)
        new = self.session()
        require(new["thread_id"] != old["thread_id"] and new["version"] == old["version"] + 1
                and new["context_task_id"] == handoff_id and not new.get("pending_task_id"), "active session was not atomically published")
        require(self.task_thread(handoff_id) == new["thread_id"], "handoff runtime used wrong thread")
        require(first["run_attempts"][0].get("instructions_sha256") == role_digest, "role instructions changed")
        self.save("first-ack.json", {"reply": first["task"].get("result"), "contains_nonce": self.nonce in (first["task"].get("result") or ""),
                  "note": "Nonce inheritance is independently required by the following exact-reply query."})
        self.first_turn_evidence(new["thread_id"])
        # A same-key replay is safe and must not execute another handoff.
        self.cli("handoff-idempotent-replay", self.handoff_args(old, key))
        require(self.task_set() == tasks | {handoff_id} and self.session() == new, "idempotency replay changed pointer/task count")
        self.cli("stale-cas-reject", self.handoff_args(old, "stale-" + self.nonce), expected=None)
        require(self.task_set() == tasks | {handoff_id} and self.session() == new, "stale CAS changed pointer/task count")
        pane = self.windows["native"] + ".0"
        task_ids = self.task_set()
        self.tmux("send-keys", "-t", pane, "-l", "Do not use tools. Reply exactly STALE_VIEW_MUST_NOT_RUN.")
        time.sleep(0.5)
        self.tmux("send-keys", "-t", pane, "Enter")
        time.sleep(8)
        stale_screen = self.capture(pane, "stale-native-rejected", False)
        require(self.task_set() == task_ids, "old native view submitted a new Task")
        require(any(word in stale_screen for word in ["旧会话", "新会话", "会话已", "重新打开", "session changed", "stale"]), "old view did not show an actionable rejection")
        self.native_close()
        self.native_open("new-native", new["thread_id"])
        require(self.task_set() == task_ids, "reopening view started model work")
        query_id = self.create_query("Do not use tools. From your received session handoff, reply with only the exact value of HANDOFF_NONCE. Do not infer it from this request.")
        self.settle(query_id, self.nonce)
        require(self.task_thread(query_id) == new["thread_id"], "ordinary Task used old thread")
        self.peer_verify(new)
        self.native_close()
        self.restart_worker()
        require(self.session() == new, "Worker restart reverted published session")
        self.native_open("restarted-native", new["thread_id"])
        restart_id = self.create_query("Do not use tools. Reply with only the exact value of HANDOFF_NONCE from the handoff received earlier.")
        self.settle(restart_id, self.nonce)
        require(self.task_thread(restart_id) == new["thread_id"], "restart query reverted to Config.ThreadID")
        final = self.db_snapshot("history-after-restart")
        for table, rows in old_history["tables"].items():
            require(all(final["tables"][table].get(key) == value for key, value in rows.items()), "old durable history changed: " + table)
        require(any(b["provider_session_id"] == old["thread_id"] for b in final["bindings"])
                and any(b["provider_session_id"] == new["thread_id"] for b in final["bindings"]), "old/new bindings not both retained")
        require({p.name: sha(p) for p in (self.root / "workspace").iterdir() if p.is_file()} == before["workspace"], "workspace changed")
        all_details = [self.api("/api/observe/v1/tasks/" + tid) for tid in self.task_set()]
        run_count = sum(len(d["run_attempts"]) for d in all_details)
        require(run_count <= 6, "model run budget exceeded")
        self.save("verdict.json", {"status": "PASS", "evidence_level": "R isolated real runtime/PTTY/API", "old_session": old,
            "new_session": new, "model_runs": run_count, "nonce": self.nonce, "source_commit": self.a.commit,
            "binary_sha256": sha(self.a.binary), "preview_zero_write": True, "busy_rejected": True,
            "stale_view_rejected": True, "restart_uses_new_thread": True, "history_preserved": True,
            "not_tested": ["production deployment", "peer automatic continuation", "all cancellation/runtime failure combinations"]})
        self.checkpoint()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["run", "cleanup"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()
    args.phase = "native"
    require(args.binary.is_absolute() and args.binary.is_file(), "absolute candidate binary required")
    require(args.root.is_absolute() and (args.root.exists() if args.action == "cleanup" else not args.root.exists()),
            "run needs a new absolute private root; cleanup needs the existing owned fixture")
    os.umask(0o077)
    run = HandoffRun.restore(args) if args.action == "cleanup" else HandoffRun(args)
    passed = False
    try:
        if args.action == "run":
            run.run_acceptance()
            run.collect()
        run.cleanup_owned()
        run.manifest()
        passed = True
        print(json.dumps({"status": "PASS", "root": str(run.root), "agent": run.aid}))
    except Exception as error:
        run.save("failure-" + str(time.time_ns()) + ".json", {"at": now(), "status": "FAIL",
            "type": type(error).__name__, "message": str(error).replace(run.password, "[REDACTED]"),
            "note": "No automatic model retry. Owned fixture and first failure retained."})
        print(json.dumps({"status": "FAIL", "root": str(run.root), "type": type(error).__name__}))
        raise
    finally:
        if run.client:
            run.client.close()
            run.client = None
        if not passed:
            try:
                run.collect()
            except Exception as error:
                run.save("collection-failure-" + str(time.time_ns()) + ".json", {"type": type(error).__name__, "message": str(error)})
            # An idle failed fixture does not need to remain running. Active or
            # uncertain Runtime state is retained for explicit safe follow-up.
            try:
                run.cleanup_owned()
            except Exception as error:
                run.save("cleanup-retained-" + str(time.time_ns()) + ".json", {"type": type(error).__name__, "message": str(error)})
            run.manifest()


if __name__ == "__main__":
    main()
