#!/usr/bin/env python3
"""Real, isolated managed Codex collaboration acceptance.

Uses formal CLI/API writes only. Keep each attempt, including failures, under a
new persistent --root. Model work starts only when this script is explicitly run.
--phase fleet resumes a prior profile after the candidate canonical installation.
Raw files are private; evidence/ contains redacted copies and SHA-256 manifests.
"""
import argparse
import datetime
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shlex
import socket
import sqlite3
import subprocess
import sys
import threading
import time
import tomllib
import urllib.request

import pexpect

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "e2e"))
from agy_live import Live, owned_stop, redact, write

TERMINAL = {"succeeded", "failed", "uncertain", "canceled", "waiting_input"}


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def require(value, message):
    if not value:
        raise RuntimeError(message)


class NativeTTY:
    def __init__(self, run, agent, label):
        self.run, self.agent, self.label = run, agent, label
        self.output = ""
        self.log = (run.root / (label + ".pty.raw.log")).open("w")
        argv = ["agent", "open", agent, "--native"]
        self.child = pexpect.spawn(str(run.a.binary), argv, env=run.env,
                                  encoding="utf-8", codec_errors="replace", dimensions=(40, 140))
        self.thread = threading.Thread(target=self.pump, daemon=True)
        self.thread.start()
        run.save(label + "-launch.json", {"at": now(), "argv": [str(run.a.binary), *argv], "pid": self.child.pid})

    def pump(self):
        while self.child.isalive():
            try:
                chunk = self.child.read_nonblocking(65536, timeout=1)
                self.output += chunk
                self.log.write(chunk)
                self.log.flush()
                if "\x1b[6n" in chunk:
                    self.child.send("\x1b[1;1R")
            except pexpect.TIMEOUT:
                pass
            except pexpect.EOF:
                break

    def wait_text(self, text, seconds=60):
        self.run.wait(lambda: text in self.output, seconds)

    def send(self, text):
        self.run.save(self.label + "-input-" + str(time.time_ns()) + ".json", {"at": now(), "input": text})
        self.child.send(text)

    def prompt(self, text):
        self.send("\x1b[200~" + text + "\x1b[201~")
        time.sleep(1)
        self.send("\r")

    def close(self):
        if self.child.isalive():
            self.send("\x03")
            time.sleep(1)
            self.send("\x03")
            for _ in range(10):
                if not self.child.isalive():
                    break
                time.sleep(1)
        if self.child.isalive():
            self.child.terminate(force=True)
        self.thread.join(3)
        self.log.close()
        self.run.save(self.label + "-closed.json", {"at": now(), "alive": self.child.isalive()})


class Acceptance(Live):
    def __init__(self, args):
        self.a = args
        self.root = args.root.resolve()
        self.out = self.root / "evidence"
        self.env = {k: v for k, v in os.environ.items()
                    if "proxy" not in k.lower() and not k.startswith(("AGY_", "OPENAGENTX_"))}
        self.env.update(OPENAGENTX_HOME=str(self.root / "profile"), TERM="xterm-256color")
        self.jar = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf, self.request_number = "", 0
        self.procs, self.tasks, self.bindings = [], [], {}
        self.tty = None
        if args.phase == "run":
            self.root.mkdir(parents=True, mode=0o700, exist_ok=False)
            self.out.mkdir(mode=0o700)
            self.password = secrets.token_urlsafe(32)
            (self.root / "password").write_text(self.password)
            self.socket = Path("/tmp") / ("oax-managed-" + secrets.token_hex(5) + ".sock")
            suffix = secrets.token_hex(4)
            self.agents = ["managed-a-" + suffix, "managed-b-" + suffix]
            with socket.socket() as local:
                local.bind(("127.0.0.1", 0))
                self.port = local.getsockname()[1]
            self.url = "http://127.0.0.1:" + str(self.port)
        else:
            state = json.loads((self.root / "handoff.json").read_text())
            self.password = (self.root / "password").read_text()
            self.socket, self.url = Path(state["socket"]), state["url"]
            self.agents, self.bindings, self.tasks = state["agents"], state["bindings"], state["tasks"]
            self.procs = json.loads((self.root / "processes.json").read_text())
            self.request_number = len(list((self.root / "http-raw").glob("*.json")))
            require(state["binary_sha256"] == sha(args.binary), "resumed candidate differs from initial binary")
        self.env["OPENAGENTX_SOCKET_PATH"] = str(self.socket)

    def save(self, name, value):
        write(self.out / name, value)

    def cli(self, label, args, binary=None):
        args = [str(v) for v in args]
        binary = binary or self.a.binary
        child = pexpect.spawn(str(binary), args, env=self.env, encoding="utf-8",
                              codec_errors="replace", timeout=240, dimensions=(40, 150))
        chunks = []
        while True:
            i = child.expect([r"(?i)password[^:\r\n]*:", r"(?i)username:", pexpect.EOF, pexpect.TIMEOUT])
            chunks.append(child.before)
            if i == 0:
                child.sendline(self.password)
            elif i == 1:
                child.sendline("owner")
            elif i == 2:
                break
            else:
                child.terminate(force=True)
                raise TimeoutError(label)
        child.close()
        output = "".join(chunks).replace(self.password, "[REDACTED]")
        self.save("cli/" + label + ".json", {"at": now(), "argv": [str(binary), *args],
                   "exit": child.exitstatus, "transcript": output})
        require(child.exitstatus == 0, label + " failed; see CLI evidence")
        try:
            return json.loads(output)
        except ValueError:
            return output

    def login(self):
        login = self.api("/api/auth/v1/login", {"username": "owner", "password": self.password}, auth=True)
        self.csrf = login["csrf_token"]

    def workspace(self, agent):
        return self.root / "workspaces" / agent

    def state(self, agent):
        return json.loads((self.root / "profile/workers/codex" / agent / "state.json").read_text())

    def all_tasks(self):
        result = self.api("/api/observe/v1/tasks?limit=100")
        require(not result.get("has_more"), "fixture exceeded one task page; do not silently omit tasks")
        return result["tasks"]

    def settle(self, task_id, seconds=360):
        def settled():
            detail = self.api("/api/observe/v1/tasks/" + task_id)
            return detail if detail["task"]["status"] in TERMINAL else None
        detail = self.wait(settled, seconds)
        self.save("tasks/" + task_id + ".json", detail)
        require(detail["task"]["status"] == "succeeded", "Task did not succeed: " + task_id)
        require(len(detail["run_attempts"]) == 1, "Task has multiple Runs: " + task_id)
        run = detail["run_attempts"][0]
        require(run["adapter_id"] == "codex-app-server" and run["model"] == "gpt-6-astra", "unexpected Runtime/model")
        if run.get("reasoning_mode") == "effort":
            require(run.get("reasoning_value") in {"high", "xhigh", "max", "ultra"}, "reasoning effort below required high")
        require(detail["task"]["completion_basis"] == "query_result_delivered", "query did not settle as delivered answer")
        self.save("runs/" + run["run_id"] + ".json", self.api("/api/observe/v1/run-attempts/" + run["run_id"]))
        require(detail.get("events"), "Task has no Journal evidence")
        if task_id not in self.tasks:
            self.tasks.append(task_id)
        return detail

    def create(self, agent, content, parent=None):
        body = {"target_agent_id": agent, "organization_id": "default", "dispatch_mode": "direct", "intent": "query",
                "content": content}
        if parent:
            body.update(parent_task_id=parent, continue_context=True,
                        runtime_session={"backend_id": "codex", "source_task_id": parent})
        task_id = self.api("/api/control/v1/tasks", body)["task_id"]
        self.tasks.append(task_id)
        return task_id

    def checkpoint(self):
        state = {"at": now(), "root": str(self.root), "socket": str(self.socket), "url": self.url,
                 "agents": self.agents, "bindings": self.bindings, "tasks": self.tasks,
                 "binary": str(self.a.binary), "binary_sha256": sha(self.a.binary), "source_commit": self.a.commit}
        write(self.root / "handoff.json", state)
        self.save("handoff.json", state)

    def migration_snapshot(self, label, columns=None):
        db = self.root / "profile/data/openagentx.db"
        connection = sqlite3.connect(db.as_uri() + "?mode=ro", uri=True)
        try:
            if columns is None:
                names = [r[0] for r in connection.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
                         if r[0] != "schema_meta" and not r[0].startswith("sqlite_")]
                columns = {name: [r[1] for r in connection.execute('PRAGMA table_info("' + name + '")')] for name in names}
            tables = {}
            quote = lambda s: '"' + s.replace('"', '""') + '"'
            for table, names in columns.items():
                rows = connection.execute("SELECT " + ",".join(map(quote, names)) + " FROM " + quote(table))
                hashes = sorted(hashlib.sha256(repr(tuple(row)).encode()).digest() for row in rows)
                tables[table] = {"rows": len(hashes), "rowset_sha256": hashlib.sha256(b"".join(hashes)).hexdigest()}
            result = {"at": now(), "mode": "read-only", "schema": connection.execute("SELECT version FROM schema_meta WHERE singleton=1").fetchone()[0],
                      "columns": columns, "tables": tables}
            self.save(label + ".json", result)
            return result
        finally:
            connection.close()

    def setup(self):
        codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
        effort = tomllib.loads((codex_home / "config.toml").read_text()).get("model_reasoning_effort")
        require(effort in {"high", "xhigh", "max", "ultra"}, "Codex default effort must be at least high; per-task overrides are unsupported")
        self.save("provenance.json", {"at": now(), "binary": str(self.a.binary), "binary_sha256": sha(self.a.binary),
                  "source_commit": self.a.commit, "harness_sha256": sha(__file__), "model": "gpt-6-astra",
                  "backend_default_effort": effort, "runtime_proxy": "join generated private EnvironmentFile; ambient proxy removed",
                  "codex_version": subprocess.check_output(["codex", "--version"], text=True).strip(),
                  "scope": "isolated daemon and owned Worker processes; not installed production service evidence"})
        a, b = self.agents
        for agent in self.agents:
            workspace = self.workspace(agent)
            workspace.mkdir(parents=True)
            role = ("You are an isolated OpenAgentX managed collaboration verification Agent. Only inspect your assigned workspace. "
                    "Never inspect credential contents, unrelated files or business projects. You may invoke the explicit OAX CLI command supplied in your task "
                    "using your own session file; do not read the credential yourself. Do not poll inbox or start background loops. "
                    "For a consultation return the requested read-only answer; OAX sends the correlated reply automatically. "
                    "When a consultation result arrives, return exactly CONSUMED: followed by the returned FACT- value. Do not send another message.\n")
            (workspace / "ROLE.md").write_text(role)
            self.cli("join-" + agent, ["agent", "join", "--id", agent, "--name", agent, "--workspace", workspace,
                     "--role", workspace / "ROLE.md", "--model", "gpt-6-astra", "--prepare"])
        legacy = Path.home() / ".local/bin/openagentx"
        self.save("migration-binaries.json", {"legacy": str(legacy), "legacy_sha256": sha(legacy), "candidate_sha256": sha(self.a.binary)})
        self.cli("legacy-init", ["init"], binary=legacy)
        for agent in self.agents:
            self.cli("legacy-apply-" + agent, ["agent", "apply", "--file", self.root / "profile/workers/identities" / (agent + ".yaml")], binary=legacy)
        before = self.migration_snapshot("migration-before")
        require(before["schema"] == 4, "legacy profile must be schema v4; save evidence and do not mislabel migration")
        self.start("daemon", [str(self.a.binary), "serve", "--http-addr", "127.0.0.1:" + str(self.port), "--web-dir", str(self.a.web)])
        self.wait(lambda: self.socket.exists())
        after = self.migration_snapshot("migration-after", before["columns"])
        require(after["schema"] == 5, "candidate must migrate profile to schema v5")
        changed = [table for table in before["tables"] if before["tables"][table] != after["tables"][table]]
        require(not changed, "migration changed existing columns: " + ",".join(changed))
        self.cli("console-login", ["console", "login"])
        self.login()
        base_env = dict(self.env)
        for agent in self.agents:
            config = self.root / "profile/workers" / (agent + ".yaml")
            envfile = config.with_suffix(".env")
            self.env = dict(base_env)
            for line in envfile.read_text().splitlines():
                if line.strip() and not line.startswith("#"):
                    pair = shlex.split(line)
                    require(len(pair) == 1, "unsupported EnvironmentFile line")
                    key, value = pair[0].split("=", 1)
                    self.env[key] = value
            self.save("environment-" + agent + ".json", {"path": str(envfile), "sha256": sha(envfile),
                       "proxy_names": sorted(k for k in self.env if "proxy" in k.lower()), "source": "agent join --prepare"})
            self.start("worker-" + agent, [str(self.a.binary), "worker", "run", "--config", str(config)])
        self.env = base_env
        for agent in self.agents:
            self.wait(lambda: any(x["agent_id"] == agent for x in self.api("/api/observe/v1/overview").get("workers", [])))
            self.cli("resume-" + agent, ["agent", "resume", agent, "--password-file", self.root / "password", "--no-open", "--wait", "3m"])
            task = self.create(agent, "Do not use tools. Reply exactly INITIALIZED-" + agent)
            self.settle(task)
            self.bindings[agent] = {"context_task": task, "thread_id": self.state(agent)["thread_id"]}
        scope = "fixture." + b
        roles = self.root / "roles.json"
        roles.write_text(json.dumps({"organization_id": "default", "rules": [{"scope": scope, "owner_agent_id": b,
                          "description": "Read only facts.txt in the assigned verification workspace and report its exact value."}]}))
        self.cli("roles", ["collaborate", "roles", "apply", "--file", roles, "--expected-version", "0"])
        for agent, peer in [(a, b), (b, a)]:
            result = self.cli("enable-" + agent, ["collaborate", "enable", "--agent", agent, "--peers", peer])
            require(result["automatic_delivery"] is True, "managed binding must have automatic delivery")
            require(result["binding"]["thread_id"] == self.bindings[agent]["thread_id"], "enable selected another thread")
            self.bindings[agent]["session_file"] = result["credential_file"]
        self.checkpoint()

    def collaboration(self):
        a, b = self.agents
        expected = "FACT-" + secrets.token_hex(12)
        (self.workspace(b) / "facts.txt").write_text(expected + "\n")
        question = self.workspace(a) / "consultation.txt"
        question.write_text("Read facts.txt only in your assigned workspace. Reply with its exact contents, without extra text. Do not modify any file.\n")
        command = [str(self.a.binary), "collaborate", "ask", "--agent", a, "--to", b, "--scope", "fixture." + b,
                   "--key", "real-consultation-01", "--socket", str(self.socket), "--session-file", self.bindings[a]["session_file"],
                   "--content-file", str(question)]
        content = ("Execute this exact CLI command once using a shell tool to ask the other domain Agent. "
                   "Do not read any credential contents and do not poll for the answer. After successful enqueue, reply exactly ASK_SENT.\n" + shlex.join(command))
        origin = self.create(a, content, self.bindings[a]["context_task"])
        origin_detail = self.settle(origin)
        require((origin_detail["task"].get("result") or "").strip() == "ASK_SENT", "sender did not report successful ask")
        def messages():
            result = self.cli("inbox-a-" + str(time.time_ns()), ["collaborate", "inbox", "--agent", a, "--all"])
            return [m for m in result.get("messages", []) if m.get("kind") == "result"]
        replies = self.wait(messages, 420)
        require(len(replies) == 1, "expected one correlated reply, no loop")
        reply = replies[0]
        request = self.cli("consultation-request", ["collaborate", "status", "--agent", b, "--message", reply["reply_to_message_id"]])
        answer = json.loads(reply["content"])
        require(answer["status"] == "succeeded" and answer["completion_basis"] == "query_result_delivered", "reply envelope does not prove delivered query result")
        require(expected == answer["result"].strip(), "reply does not match independently read facts.txt")
        self.settle(request["task_id"])
        consumption = self.settle(reply["task_id"])
        require((consumption["task"].get("result") or "").strip() == "CONSUMED:" + expected, "A did not automatically consume B's factual answer")
        for agent, tid in [(b, request["task_id"]), (a, reply["task_id"])]:
            state = json.loads((self.root / "profile/workers/codex" / agent / "tasks" / (tid + ".json")).read_text())
            require(state["thread_id"] == self.bindings[agent]["thread_id"], "managed Task changed domain thread")
            self.save("thread-" + tid + ".json", state)
        duplicate = self.cli("ask-idempotent-retry", command[1:])
        require(duplicate["message_id"] == request["message_id"] and duplicate["task_id"] == request["task_id"], "same-key retry duplicated consultation")
        self.save("collaboration-result.json", {"status": "PASS", "origin_task": origin, "request": request, "reply": reply,
                  "answer_envelope": answer, "consumption_task": consumption["task"], "query_settlement": "query_result_delivered"})
        self.save("independent-fact-verification.json", {"status": "PASS", "file": str(self.workspace(b) / "facts.txt"),
                  "fact_file_sha256": sha(self.workspace(b) / "facts.txt"), "independent_fact": expected,
                  "answer_result": answer["result"], "consumption_result": consumption["task"]["result"]})
        self.checkpoint()

    def native(self):
        a = self.agents[0]
        initial = {x["id"] for x in self.all_tasks()}
        self.tty = NativeTTY(self, a, "native-first")
        self.tty.wait_text("原生 Codex")
        time.sleep(5)
        self.tty.prompt("Do not call tools. Reply exactly OAX_MANAGED_NATIVE_INPUT_OK.")
        created = self.wait(lambda: [x for x in self.all_tasks() if x["id"] not in initial], 120)
        require(len(created) == 1, "native input created unexpected Task count")
        native = self.settle(created[0]["id"])
        require((native["task"].get("result") or "").strip() == "OAX_MANAGED_NATIVE_INPUT_OK", "native input response mismatch")
        background = self.create(a, "Use a shell tool to run sleep 12, then reply exactly OAX_MANAGED_AFTER_CLOSE_OK. Do not change files.", native["task"]["id"])
        self.wait(lambda: self.api("/api/observe/v1/tasks/" + background)["task"]["status"] == "running", 90)
        self.tty.close()
        self.tty = None
        result = self.settle(background)
        require((result["task"].get("result") or "").strip() == "OAX_MANAGED_AFTER_CLOSE_OK", "background failed after native close")
        self.tty = NativeTTY(self, a, "native-reopen")
        self.tty.wait_text("原生 Codex")
        self.tty.wait_text("OAX_MANAGED_AFTER_CLOSE_OK", 90)
        require(self.state(a)["thread_id"] == self.bindings[a]["thread_id"], "reopen changed thread")
        self.tty.close()
        self.tty = None
        self.save("native-result.json", {"status": "PASS", "native_task": native["task"]["id"], "background_task": background,
                  "thread_id": self.state(a)["thread_id"], "scope": "candidate direct native PTY; Fleet tested separately"})
        self.checkpoint()

    def idle_snapshot(self, label):
        tasks = self.all_tasks()
        require(all(x["status"] in TERMINAL for x in tasks), "idle window contains active Tasks")
        threads = {}
        codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
        for agent in self.agents:
            tid = self.bindings[agent]["thread_id"]
            files = list((codex_home / "sessions").rglob("*" + tid + ".jsonl"))
            require(len(files) == 1, "cannot uniquely locate fixture Codex rollout " + tid)
            data = files[0].read_text()
            rows = [json.loads(line) for line in data.splitlines() if line.strip()]
            require(any(r.get("type") == "session_meta" and r.get("payload", {}).get("id") == tid for r in rows), "rollout identity mismatch")
            counters = {kind: sum(r.get("type") == kind for r in rows) for kind in ["turn_context", "response_item"]}
            counters["model_turns_started"] = sum(r.get("type") == "event_msg" and r.get("payload", {}).get("type") in ["task_started", "turn_started"] for r in rows)
            counters["tool_calls"] = sum(r.get("type") == "response_item" and r.get("payload", {}).get("type") in ["function_call", "custom_tool_call"] for r in rows)
            execution = []
            for row in rows:
                if row.get("type") != "turn_context":
                    continue
                settings = row.get("payload", {})
                mode = settings.get("collaboration_mode", {}).get("settings", {})
                effort = settings.get("effort", settings.get("reasoning_effort", mode.get("reasoning_effort")))
                model = settings.get("model", mode.get("model"))
                require(model == "gpt-6-astra" and effort in {"high", "xhigh", "max", "ultra"}, "actual rollout model/effort violates verification policy")
                execution.append({"model": model, "effort": effort})
            require(execution, "rollout contains no actual model execution settings")
            threads[agent] = {"thread_id": tid, "rollout": str(files[0]), "sha256": sha(files[0]), "counts": counters, "execution": execution}
            private = self.root / "rollouts" / (label + "-" + agent + ".jsonl")
            private.parent.mkdir(exist_ok=True)
            private.write_text(data)
            public = self.out / "rollouts" / private.name
            public.parent.mkdir(exist_ok=True)
            public.write_text("\n".join(json.dumps(redact(r), ensure_ascii=False) for r in rows) + "\n")
        snapshot = {"at": now(), "tasks": sorted((x["id"], x["status"]) for x in tasks), "threads": threads}
        self.save("idle-" + label + ".json", snapshot)
        return snapshot

    def idle(self):
        before = self.idle_snapshot("before")
        start = time.monotonic()
        self.save("idle-window.json", {"status": "RUNNING", "started_at": now(), "seconds": self.a.idle_seconds})
        print(json.dumps({"stage": "idle", "seconds": self.a.idle_seconds, "root": str(self.root)}), flush=True)
        while time.monotonic() - start < self.a.idle_seconds:
            time.sleep(max(0, min(30, self.a.idle_seconds - (time.monotonic() - start))))
        after = self.idle_snapshot("after")
        require(before["tasks"] == after["tasks"], "idle created or changed Tasks")
        for agent in self.agents:
            require(before["threads"][agent]["counts"] == after["threads"][agent]["counts"], "idle caused new Codex model/tool activity")
        self.save("idle-result.json", {"status": "PASS", "elapsed_seconds": time.monotonic() - start,
                  "new_tasks": 0, "new_model_turns": 0, "new_tool_calls": 0, "scope": "OAX Task inventory and exact fixture Codex rollout event counts"})

    def fleet(self):
        canonical = Path.home() / ".local/bin/openagentx"
        require(canonical.is_file() and sha(canonical) == sha(self.a.binary), "Fleet requires matching canonical candidate installation; no old binary will be silently used")
        server = "oax-managed-" + self.agents[0]
        tmux = ["tmux", "-L", server]
        subprocess.run([*tmux, "new-session", "-d", "-s", "validation-bootstrap", "sleep", "86400"], check=True, env=self.env)
        target = subprocess.check_output([*tmux, "display-message", "-p", "-t", "validation-bootstrap", "#{socket_path},#{pid},0"], text=True).strip()
        self.env["TMUX"] = target
        self.cli("fleet-workspace", ["fleet", "workspace", "--respawn-dead"])
        windows = subprocess.check_output([*tmux, "list-panes", "-s", "-t", "=OAX", "-F", "#{window_name}\t#{pane_index}\t#{pane_dead}\t#{pane_pid}"], text=True)
        self.save("fleet-panes.json", {"at": now(), "socket": server, "panes": windows, "canonical_sha256": sha(canonical)})
        for agent in self.agents:
            require(any(line.startswith(agent + "\t0\t0\t") for line in windows.splitlines()), "Fleet native pane missing or dead: " + agent)
        a = self.agents[0]
        prompt = "Do not use tools. Reply exactly OAX_FLEET_NATIVE_OK."
        initial = {x["id"] for x in self.all_tasks()}
        time.sleep(5)
        subprocess.run([*tmux, "send-keys", "-t", "OAX:" + a + ".0", "-l", prompt], check=True)
        subprocess.run([*tmux, "send-keys", "-t", "OAX:" + a + ".0", "Enter"], check=True)
        created = self.wait(lambda: [x for x in self.all_tasks() if x["id"] not in initial], 120)
        require(len(created) == 1, "Fleet input did not create exactly one Task")
        detail = self.settle(created[0]["id"])
        require((detail["task"].get("result") or "").strip() == "OAX_FLEET_NATIVE_OK", "Fleet native reply mismatch")
        captured = subprocess.check_output([*tmux, "capture-pane", "-p", "-S", "-200", "-t", "OAX:" + a + ".0"], text=True)
        self.save("fleet-native-result.json", {"status": "PASS", "task_id": created[0]["id"], "input": prompt, "capture": captured})
        self.checkpoint()
        subprocess.run([*tmux, "kill-server"], check=True)

    def collect(self):
        for pattern in ["*.stdout", "*.stderr", "*.pty.raw.log"]:
            for path in self.root.glob(pattern):
                (self.out / path.name).write_text(redact(path.read_text(errors="replace")).replace(self.password, "[REDACTED]"))
        self.save("processes.json", self.procs)
        self.checkpoint()
        (self.out / "SHA256SUMS").write_text("\n".join(sha(p) + "  " + str(p.relative_to(self.out))
            for p in sorted(self.out.rglob("*")) if p.is_file() and p.name != "SHA256SUMS") + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--web", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--phase", choices=["run", "fleet", "idle", "cleanup"], default="run")
    parser.add_argument("--idle-seconds", type=int, default=1800)
    parser.add_argument("--skip-fleet", action="store_true")
    args = parser.parse_args()
    require(args.idle_seconds >= 0, "negative idle duration")
    args.binary = args.binary.resolve(strict=True)
    args.web = args.web.resolve(strict=True)
    os.umask(0o077)
    run = Acceptance(args)
    stage = args.phase
    try:
        if args.phase == "cleanup":
            run.save("cleanup.json", owned_stop(run.root))
            return
        if args.phase == "run":
            stage = "setup"; run.setup()
            stage = "collaboration"; run.collaboration()
            stage = "native"; run.native()
            if not args.skip_fleet:
                stage = "fleet"; run.fleet()
            stage = "idle"; run.idle()
        else:
            run.login()
            getattr(run, args.phase)()
        run.save("verdict-" + args.phase + ".json", {"status": "PASS", "phase": args.phase,
                 "fleet": "NOT_RUN" if args.phase == "run" and args.skip_fleet else "see separate evidence",
                 "idle_30_minutes": args.idle_seconds >= 1800, "at": now()})
        print(json.dumps({"status": "PASS", "root": str(run.root), "phase": args.phase}), flush=True)
    except Exception as error:
        run.save("failure-" + str(time.time_ns()) + ".json", {"status": "FAIL", "stage": stage,
                 "type": type(error).__name__, "message": str(error).replace(run.password, "[REDACTED]"), "at": now()})
        raise
    finally:
        if run.tty:
            run.tty.close()
        run.collect()


if __name__ == "__main__":
    main()
