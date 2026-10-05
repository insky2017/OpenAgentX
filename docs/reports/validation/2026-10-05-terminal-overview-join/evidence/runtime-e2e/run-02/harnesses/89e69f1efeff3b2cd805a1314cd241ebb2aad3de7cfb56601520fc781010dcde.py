#!/usr/bin/env python3
"""Owned tmux/profile acceptance; retains the fixture for Overview follow-up.

Uses the existing Codex setup CLI helpers, real Worker and formal HTTP API.
Never invokes the default tmux server or the user's service manager.
"""
import argparse
import datetime
import fcntl
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import signal
import subprocess
import sys
import threading
import time
import tomllib
import urllib.error
import urllib.request
import unicodedata

import pexpect

sys.dont_write_bytecode = True
from codex_local_setup import Setup
from agy_live import redact, write


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


class AttachedClient:
    """Real terminal attached to the owned tmux server, with persistent output."""
    def __init__(self, run):
        self.child = pexpect.spawn(run.tmux_binary, ["-L", run.server, "attach-session", "-t", "=OAX"],
                                  env=run.env, encoding="utf-8", codec_errors="replace", dimensions=(45, 160))
        self.log = (run.root / "attached-client.pty.log").open("a")
        self.thread = threading.Thread(target=self.pump, daemon=True)
        self.thread.start()

    def pump(self):
        while self.child.isalive():
            try:
                chunk = self.child.read_nonblocking(65536, timeout=1)
                self.log.write(chunk)
                self.log.flush()
                if "\x1b[6n" in chunk:
                    self.child.send("\x1b[1;1R")
            except pexpect.TIMEOUT:
                pass
            except pexpect.EOF:
                break

    def close(self):
        if self.child.isalive():
            self.child.send("\x02d")  # Detach the owned client; preserve every pane.
            try:
                self.child.expect(pexpect.EOF, timeout=5)
            except (pexpect.TIMEOUT, pexpect.EOF):
                self.child.terminate(force=True)
        self.thread.join(3)
        self.log.close()


class TerminalRun(Setup):
    def __init__(self, args):
        args.profile, args.evidence = args.root, args.root / "evidence"
        args.web = args.root / "webroot"
        super().__init__(args)
        self.aid = "terminal-e2e-" + secrets.token_hex(3)
        self.server = "oax-terminal-" + secrets.token_hex(6)
        self.tmux_binary = shutil.which("tmux")
        require(bool(self.tmux_binary), "tmux is required")
        for key in ("TMUX", "TMUX_PANE", "CODEX_THREAD_ID", "CODEX_SESSION_ID"):
            self.env.pop(key, None)
        self.tools = self.root / "tools"
        self.tools.mkdir(mode=0o700)
        self.a.web.mkdir(mode=0o700)
        tmux_wrapper = self.tools / "tmux"
        tmux_wrapper.write_text("#!/bin/sh\nexec " + shlex.join([self.tmux_binary, "-f", "/dev/null", "-L", self.server]) + ' "$@"\n')
        tmux_wrapper.chmod(0o700)
        systemctl = self.tools / "systemctl"
        systemctl.write_text("#!/bin/sh\necho 'isolated fixture forbids systemctl' >&2\nexit 97\n")
        systemctl.chmod(0o700)
        self.env["PATH"] = str(self.tools) + os.pathsep + self.env["PATH"]
        self.client = None
        self.tmux_calls = 0
        self.windows = {}
        self.task_ids = []

    @classmethod
    def restore(cls, args):
        self = cls.__new__(cls)
        self.a, self.root, self.out = args, args.root, args.root / "evidence"
        saved = json.loads((self.root / "handoff.json").read_text())
        require(args.phase == "overview" or sha(args.binary) == saved["binary_sha256"], "resume-fixture must use the same fixed binary")
        self.aid, self.server = saved["agent"], saved["server"]
        self.socket, self.url = Path(saved["socket"]), saved["url"]
        self.windows, self.procs, self.task_ids = saved["windows"], saved["processes"], saved["tasks"]
        self.password = (self.root / "password").read_text()
        self.env = {key: value for key, value in os.environ.items()
                    if "proxy" not in key.lower() and not key.startswith(("AGY_", "OPENAGENTX_"))
                    and key not in {"TMUX", "TMUX_PANE", "CODEX_THREAD_ID", "CODEX_SESSION_ID"}}
        self.tools = self.root / "tools"
        self.env.update(OPENAGENTX_HOME=str(self.root / "profile"), OPENAGENTX_SOCKET_PATH=str(self.socket),
                        TERM="xterm-256color", PATH=str(self.tools) + os.pathsep + self.env["PATH"])
        self.tmux_binary = shutil.which("tmux")
        self.tmux_calls = len(list((self.out / "tmux").glob("*.json")))
        self.request_number = len(list((self.out / "http").glob("*.json")))
        self.jar = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf, self.client = "", None
        self.expected = "OAX-TERMINAL-ROLE " + self.aid + " WORKSPACE=" + str(self.root / "workspace")
        self.workspace_before = json.loads((self.out / "workspace-before.json").read_text()) if (self.out / "workspace-before.json").exists() else {"ROLE.md": sha(self.root / "workspace/ROLE.md")}
        login = self.api("/api/auth/v1/login", {"username": "owner", "password": self.password}, auth=True)
        self.csrf = login["csrf_token"]
        return self

    def save(self, name, value):
        write(self.out / name, value)

    def api(self, path, body=None, auth=False):
        # Same formal API as Live, but never persist Cookie/CSRF/password headers.
        headers = {"Content-Type": "application/json"}
        if list(self.jar):
            headers["Cookie"] = "; ".join(cookie.name + "=" + cookie.value for cookie in self.jar)
        if body is not None and not auth:
            body = dict(body)
            key = body.setdefault("meta", {}).setdefault("idempotency_key", secrets.token_hex(12))
            headers.update({"X-CSRF-Token": self.csrf, "Idempotency-Key": key})
        request = urllib.request.Request(self.url + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
        try:
            with self.http.open(request, timeout=15) as response:
                code, payload = response.status, response.read()
        except urllib.error.HTTPError as error:
            code, payload = error.code, error.read()
        try:
            result = json.loads(payload)
        except ValueError:
            result = {"non_json_response_bytes": len(payload), "sha256": hashlib.sha256(payload).hexdigest()}
        self.request_number += 1
        self.save("http/%04d.json" % self.request_number, {"at": now(), "path": path, "method": request.get_method(),
                  "request": {"login": "omitted"} if auth else body, "status": code, "response": result})
        require(code == 200, "formal API failed: HTTP " + str(code) + " " + path)
        require("non_json_response_bytes" not in result, "formal API returned non-JSON response")
        return result

    def wait(self, predicate, seconds=90):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            value = predicate()
            if value:
                return value
            time.sleep(5)
        raise TimeoutError("bounded fixture wait expired")

    def tmux(self, *args):
        argv = [self.tmux_binary, "-f", "/dev/null", "-L", self.server, *args]
        process = subprocess.run(argv, env=self.env, capture_output=True, text=True, timeout=20)
        self.tmux_calls += 1
        self.save("tmux/%04d.json" % self.tmux_calls, {"at": now(), "argv": argv,
                  "returncode": process.returncode, "stdout": process.stdout, "stderr": process.stderr})
        require(process.returncode == 0, "owned tmux operation failed: " + args[0])
        return process.stdout.strip()

    def capture(self, pane, label, history=True):
        args = ["capture-pane", "-p"] + (["-S", "-"] if history else []) + ["-t", pane]
        text = self.tmux(*args)
        (self.out / (label + ".rendered.txt")).write_text(text + "\n")
        return text

    def snapshot(self, label):
        lines = self.tmux("list-panes", "-a", "-F", "#{session_name}\t#{window_id}\t#{window_name}\t#{pane_id}\t#{pane_index}\t#{pane_pid}\t#{pane_dead}\t#{pane_title}")
        panes = []
        for line in lines.splitlines():
            values = line.split("\t", 7)
            panes.append(dict(zip(("session", "window", "name", "pane", "index", "pid", "dead", "title"), values)))
        options = {window: self.tmux("show-options", "-w", "-t", window) for window in self.windows.values()}
        state_file = self.root / "profile/workers/codex" / self.aid / "state.json"
        state = json.loads(state_file.read_text()) if state_file.exists() else {}
        active = self.tmux("list-windows", "-t", "=OAX", "-F", "#{window_id}\t#{window_active}")
        result = {"at": now(), "panes": panes, "options": options, "engine": state,
                  "processes": self.procs, "active_window": next(line.split("\t")[0] for line in active.splitlines() if line.endswith("\t1"))}
        self.save(label + ".json", result)
        return result

    def script(self, name, argv):
        path = self.root / (name + ".sh")
        # Only non-secret routing settings are written; credentials remain files.
        settings = {key: value for key, value in self.env.items()
                    if key in {"PATH", "OPENAGENTX_HOME", "OPENAGENTX_SOCKET_PATH", "TERM"}}
        path.write_text("#!/bin/sh\n" + "\n".join("export " + key + "=" + shlex.quote(value) for key, value in settings.items())
                        + "\nexec " + shlex.join([str(arg) for arg in argv]) + "\n")
        path.chmod(0o700)
        return str(path)

    def lock_held(self):
        path = self.root / "profile/workers/codex" / self.aid / "terminal.lock"
        if not path.exists():
            return False
        with path.open("rb") as handle:
            try:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
                fcntl.flock(handle, fcntl.LOCK_UN)
                return False
            except BlockingIOError:
                return True

    def setup(self):
        codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
        effort = tomllib.loads((codex_home / "config.toml").read_text()).get("model_reasoning_effort")
        require(effort in {"high", "xhigh", "max", "ultra"}, "existing Codex effort must be at least high")
        self.save("provenance.json", {"at": now(), "binary": str(self.a.binary), "binary_sha256": sha(self.a.binary),
                  "commit": self.a.commit, "harness_sha256": sha(__file__), "codex_binary": shutil.which("codex"),
                  "codex_version": subprocess.check_output(["codex", "--version"], text=True).strip(),
                  "model": "gpt-6-astra", "configured_effort": effort, "tmux_server": self.server,
                  "scope": "R isolated real CLI/Worker/tmux, no installed service or business acceptance"})
        workspace = self.root / "workspace"
        workspace.mkdir(mode=0o700)
        self.expected = "OAX-TERMINAL-ROLE " + self.aid + " WORKSPACE=" + str(workspace)
        role = workspace / "ROLE.md"
        role.write_text("You are the isolated OAX terminal verification Agent. Only your assigned workspace is in scope. "
                        "Never read credentials or unrelated files, send messages, change files, or run tools. "
                        "When asked to confirm your role and workspace, reply exactly the following one line, then wait:\n" + self.expected + "\n")
        self.workspace_before = {p.name: sha(p) for p in workspace.iterdir()}
        self.save("workspace-before.json", self.workspace_before)
        self.cli("01-join", ["agent", "join", "--id", self.aid, "--name", "终端真实验收", "--workspace", str(workspace),
                 "--role", str(role), "--model", "gpt-6-astra", "--prepare"])
        self.cli("02-init", ["init"])
        config = self.root / "profile/workers" / (self.aid + ".yaml")
        self.cli("03-apply", ["agent", "apply", "--file", str(config.parent / "identities" / (self.aid + ".yaml"))])
        self.start("daemon", [str(self.a.binary), "serve", "--http-addr", "127.0.0.1:" + str(self.port), "--web-dir", str(self.a.web)])
        self.wait(lambda: self.socket.exists())
        self.cli("04-console-login", ["console", "login"])
        base_env = dict(self.env)
        for line in config.with_suffix(".env").read_text().splitlines():
            if line.strip() and not line.startswith("#"):
                pair = shlex.split(line)
                require(len(pair) == 1, "unsupported generated environment line")
                key, value = pair[0].split("=", 1)
                self.env[key] = value
        self.start("worker", [str(self.a.binary), "worker", "run", "--config", str(config)])
        self.env = base_env
        login = self.api("/api/auth/v1/login", {"username": "owner", "password": self.password}, auth=True)
        self.csrf = login["csrf_token"]
        self.wait(lambda: any(item["agent_id"] == self.aid for item in self.api("/api/observe/v1/overview").get("workers", [])))
        self.cli("05-resume", ["agent", "resume", self.aid, "--password-file", str(self.root / "password"), "--no-open", "--wait", "3m"])
        self.checkpoint()

    def checkpoint(self):
        self.save("handoff.json", {"root": str(self.root), "socket": str(self.socket), "url": self.url,
                  "agent": self.aid, "binary": str(self.a.binary), "binary_sha256": sha(self.a.binary),
                  "server": self.server, "windows": self.windows, "tasks": self.task_ids,
                  "processes": self.procs, "profile": str(self.root / "profile"), "at": now()})
        shutil.copyfile(self.out / "handoff.json", self.root / "handoff.json")

    def native(self, continuing=False):
        if not continuing:
            self.windows["overview"] = self.tmux("new-session", "-d", "-x", "160", "-y", "45", "-P", "-F", "#{window_id}", "-s", "OAX", "-n", "overview", "exec /bin/sh")
            self.windows["native"] = self.tmux("new-window", "-d", "-P", "-F", "#{window_id}", "-t", "=OAX", "-n", "scratch", "sleep", "86400")
            self.windows["other"] = self.tmux("new-window", "-d", "-P", "-F", "#{window_id}", "-t", "=OAX", "-n", "other", "exec /bin/sh")
            for window in self.windows.values():
                self.tmux("set-option", "-w", "-t", window, "pane-base-index", "0")
                self.tmux("set-option", "-w", "-t", window, "automatic-rename", "off")
        target = self.windows["native"]
        pane = self.tmux("display-message", "-p", "-t", target + ".0", "#{pane_id}")
        if not continuing:
            self.tmux("split-window", "-d", "-t", target, "sleep", "86400")
        before = json.loads((self.out / "10-before-native.json").read_text()) if continuing else self.snapshot("10-before-native")
        extra = next(p for p in before["panes"] if p["window"] == target and p["index"] == "1")
        self.client = AttachedClient(self)
        self.tmux("select-window", "-t", self.windows["other"])
        launcher = self.script("native", [self.a.binary, "agent", "open", self.aid, "--native"])
        if not continuing:
            self.tmux("respawn-pane", "-k", "-t", pane, launcher)
        self.wait(self.lock_held)
        def settled():
            tasks = self.api("/api/observe/v1/tasks?limit=100")["tasks"]
            selected = [t for t in tasks if t["target_agent_id"] == self.aid]
            require(len(selected) <= 1, "native initialization dispatched duplicate Tasks")
            if selected and selected[0]["status"] in {"succeeded", "failed", "uncertain", "canceled"}:
                return self.api("/api/observe/v1/tasks/" + selected[0]["id"])
        detail = self.wait(settled, 360)
        self.save("11-native-task.json", detail)
        task = detail["task"]
        require(task["status"] == "succeeded" and task["intent"] == "query", "native initialization query failed")
        require((task.get("result") or "").strip() == self.expected, "role/workspace response differs from requested proof")
        require(task["completion_basis"] == "query_result_delivered" and len(detail["run_attempts"]) == 1 and detail["events"], "missing Task/Run/Journal completion evidence")
        run = detail["run_attempts"][0]
        require(run["status"] == "succeeded" and run["model"] == "gpt-6-astra", "unexpected Runtime result or model")
        self.save("12-native-run.json", self.api("/api/observe/v1/run-attempts/" + run["run_id"]))
        task_id = task.get("task_id", task.get("id"))
        self.task_ids = [task_id]
        self.wait(lambda: "OpenAgentX · " + self.aid + " · 原生 Codex" in self.tmux("capture-pane", "-p", "-S", "-", "-t", pane))
        self.capture(pane, "13-native-initial")
        after = self.snapshot("14-native-bound")
        self.assert_native(after, target, extra)
        require(self.lock_held(), "native terminal lock is not held")
        thread = after["engine"]["thread_id"]
        self.checkpoint()
        self.tmux("send-keys", "-t", pane, "C-d")
        self.wait(lambda: self.tmux("display-message", "-p", "-t", pane, "#{pane_dead}") == "1", 60)
        require(not self.lock_held(), "closed native terminal retained the lock")
        self.capture(pane, "15-native-closed")
        self.tmux("set-option", "-w", "-u", "-t", target, "allow-rename")
        self.tmux("set-option", "-w", "-t", target, "pane-border-format", "fixture drift")
        self.tmux("respawn-pane", "-t", pane, launcher)
        self.wait(self.lock_held)
        self.wait(lambda: "原生 Codex" in self.tmux("capture-pane", "-p", "-S", "-", "-t", pane))
        reopened = self.snapshot("16-native-reopened")
        self.assert_native(reopened, target, extra)
        require(reopened["engine"]["thread_id"] == thread, "reopen changed thread")
        self.capture(pane, "17-native-reopened")
        require(len(self.api("/api/observe/v1/tasks?limit=100")["tasks"]) == 1, "reopen started unexpected model work")
        require(self.workspace_before == {p.name: sha(p) for p in (self.root / "workspace").iterdir()}, "model changed the read-only workspace")
        self.save("native-verdict.json", {"status": "PASS", "scope": "real native initialization query, tmux binding and same-thread repair/reopen",
                  "task_id": task_id, "run_id": run["run_id"], "thread_id": thread,
                  "terminal_lock_held_after_reopen": self.lock_held(), "extra_pane_preserved": extra,
                  "not_tested": ["Overview", "Console navigation", "installed service", "business workflows"]})
        self.checkpoint()

    def assert_native(self, snapshot, target, extra):
        options = snapshot["options"][target]
        for expected in ("@openagentx_managed 1", "@openagentx_agent_id " + self.aid,
                         "automatic-rename off", "allow-rename off", "remain-on-exit on", "Codex 受管原生终端"):
            require(expected in options, "missing native terminal option: " + expected)
        require(all(p["name"] == self.aid for p in snapshot["panes"] if p["window"] == target), "wrong native window name")
        require(next(p for p in snapshot["panes"] if p["pane"] == extra["pane"]) == {**extra, "name": self.aid}, "extra pane was changed")

    def console(self):
        # Fixture setup of the reserved Overview identity precedes observations.
        self.tmux("set-option", "-w", "-t", self.windows["overview"], "@openagentx_managed", "1")
        self.client = AttachedClient(self)
        target = self.windows["native"]
        launcher = self.script("console-open", [self.a.binary, "agent", "open", self.aid, "--console"])
        reports = []
        attempt = "console-" + str(time.time_ns())
        for name in ("overview", "other"):
            source = self.windows[name]
            pane = source + ".0"
            self.tmux("select-window", "-t", source)
            before = self.snapshot(attempt + "-" + name + "-before")
            marker = "CONSOLE-" + secrets.token_hex(4) + "-EXIT-"
            command = shlex.quote(launcher) + "; printf '\\n" + marker + "%s\\n' \"$?\""
            self.tmux("send-keys", "-t", pane, "-l", command)
            self.tmux("send-keys", "-t", pane, "Enter")
            self.wait(lambda: marker + "0" in self.tmux("capture-pane", "-p", "-S", "-", "-t", pane).splitlines(), 60)
            after = self.snapshot(attempt + "-" + name + "-after")
            require(after["active_window"] == target, "Console did not navigate to the exact target window")
            require(before["panes"] == after["panes"], "Console navigation changed pane identity/process/title")
            require(before["options"] == after["options"], "Console navigation changed source or target window options")
            require(before["engine"]["thread_id"] == after["engine"]["thread_id"], "Console navigation changed the thread")
            self.capture(pane, attempt + "-" + name + "-source")
            reports.append({"source": name, "target": target, "pane_processes_preserved": True, "window_options_preserved": True})
        require(len(self.api("/api/observe/v1/tasks?limit=100")["tasks"]) == 1, "Console navigation triggered model work")
        self.save("console-verdict.json", {"status": "PASS", "scope": "real agent open --console from overview and unrelated OAX source", "cases": reports})

    def overview(self):
        pending_agent = "pending-e2e-" + self.aid.rsplit("-", 1)[1]
        pending_file = self.root / "pending-task.json"
        if pending_file.exists():
            pending_id = json.loads(pending_file.read_text())["task_id"]
        else:
            identity = self.root / "pending-identity.json"
            identity.write_text(json.dumps({"version": 1, "agent_id": pending_agent,
                "principal_id": "agent-" + pending_agent, "organization_id": "default", "display_name": "隔离待处理验收",
                "profile": {"instructions_path": str(self.root / "workspace/ROLE.md"), "workspace_root": str(self.root / "workspace")}}))
            self.cli("overview-apply-offline", ["agent", "apply", "--file", str(identity)])
            receipt = self.api("/api/control/v1/tasks", {"target_agent_id": pending_agent, "organization_id": "default",
                      "dispatch_mode": "direct", "intent": "query", "content": "OVERVIEW-PENDING-READONLY: isolated queue evidence; no Worker will execute this task."})
            pending_id = receipt["task_id"]
            write(pending_file, {"task_id": pending_id, "agent_id": pending_agent})
        detail = self.api("/api/observe/v1/tasks/" + pending_id)
        require(detail["task"]["status"] == "queued" and not detail["run_attempts"], "pending fixture unexpectedly ran")
        self.save("overview-pending-task-before.json", detail)
        self.client = AttachedClient(self)
        source, target = self.windows["overview"], self.windows["native"]
        pane = source + ".0"
        self.tmux("select-window", "-t", source)
        launcher = self.script("overview", [self.a.binary, "overview", "--socket", self.socket,
                  "--credentials", self.root / "profile/credentials.json", "--worker-dir", self.root / "profile/workers"])
        self.tmux("send-keys", "-t", pane, "-l", shlex.quote(launcher))
        self.tmux("send-keys", "-t", pane, "Enter")
        def screen():
            return self.tmux("capture-pane", "-p", "-t", pane)
        self.wait(lambda: "在线" in screen() and "2 个 Agent" in screen(), 60)
        self.tmux("send-keys", "-t", pane, "Home")
        self.wait(lambda: "OVERVIEW-PENDING-READONLY" in screen())
        initial_text = self.capture(pane, "overview-list-pending-detail")
        require("排队" in initial_text and pending_agent in initial_text and self.aid in initial_text,
                "Overview missing real queue/Agent/detail projection")
        self.tmux("send-keys", "-t", pane, "Tab", "End")
        self.capture(pane, "overview-detail-scroll")
        self.tmux("send-keys", "-t", pane, "Tab", "End")
        self.wait(lambda: "终端真实验收 (" + self.aid + ")" in screen())
        ready = self.snapshot("overview-before-navigation")
        self.capture(pane, "overview-native-selected")
        self.tmux("send-keys", "-t", pane, "Enter")
        def navigated():
            rows = self.tmux("list-windows", "-t", "=OAX", "-F", "#{window_id}\t#{window_active}")
            return target + "\t1" in rows.splitlines()
        self.wait(navigated, 60)
        navigated_state = self.snapshot("overview-after-navigation")
        require(ready["panes"] == navigated_state["panes"] and ready["options"] == navigated_state["options"],
                "Overview navigation changed terminal processes, titles or options")
        require(ready["engine"]["thread_id"] == navigated_state["engine"]["thread_id"], "Overview navigation changed thread")
        self.tmux("select-window", "-t", source)
        self.client.child.setwinsize(24, 80)
        self.wait(lambda: self.tmux("display-message", "-p", "-t", pane, "#{pane_width}") == "80")
        self.tmux("send-keys", "-t", pane, "Home")
        self.wait(lambda: pending_agent in screen() and "排队" in screen())
        narrow = self.capture(pane, "overview-80x24-pending")
        width = lambda line: sum(0 if unicodedata.combining(ch) else 2 if unicodedata.east_asian_width(ch) in {"W", "F"} else 1 for ch in line)
        require(all(width(line) <= 80 for line in narrow.splitlines()), "80-column Overview overflowed")
        require("q 退出" in narrow, "80x24 Overview hid navigation footer")
        self.tmux("send-keys", "-t", pane, "Tab", "End")
        self.capture(pane, "overview-80x24-detail-scroll")
        self.tmux("send-keys", "-t", pane, "Tab", "End")
        self.wait(lambda: "终端真实验收 (" + self.aid + ")" in screen())
        self.capture(pane, "overview-80x24-native-selected")
        self.tmux("send-keys", "-t", pane, "Enter")
        self.wait(navigated, 60)
        narrow_state = self.snapshot("overview-80x24-after-navigation")
        require(narrow_state["panes"] == ready["panes"] and narrow_state["options"] == ready["options"], "80x24 navigation changed terminal state")
        self.tmux("select-window", "-t", source)
        self.client.child.setwinsize(45, 160)
        self.wait(lambda: self.tmux("display-message", "-p", "-t", pane, "#{pane_width}") == "160")
        daemon = next(entry for entry in self.procs if entry["label"] == "daemon")
        current = Path("/proc") / str(daemon["pid"]) / "stat"
        require(current.read_text().rsplit(")", 1)[1].split()[19] == str(daemon["starttime"]), "daemon PID reused")
        interrupted_at = now()
        os.kill(daemon["pid"], signal.SIGSTOP)
        try:
            self.wait(lambda: "离线 · 旧数据 · 禁止跳转" in screen(), 30)
            self.capture(pane, "overview-disconnected")
            self.tmux("send-keys", "-t", pane, "Enter")
            time.sleep(1)
            disconnected = self.snapshot("overview-disconnected-navigation-blocked")
            require(disconnected["active_window"] == source, "disconnected Overview allowed navigation")
        finally:
            os.kill(daemon["pid"], signal.SIGCONT)
            self.save("overview-daemon-pause.json", {"pid": daemon["pid"], "starttime": daemon["starttime"],
                      "paused_at": interrupted_at, "continued_at": now(), "scope": "owned isolated daemon only"})
        self.wait(lambda: "在线" in screen() and "离线 · 旧数据" not in screen(), 60)
        recovered = self.snapshot("overview-recovered")
        self.capture(pane, "overview-recovered")
        require(recovered["engine"]["thread_id"] == ready["engine"]["thread_id"], "disconnect recovery changed thread")
        require(ready["panes"] == recovered["panes"], "disconnect recovery changed pane processes")
        tasks = self.api("/api/observe/v1/tasks?limit=100")["tasks"]
        require(len(tasks) == 2, "Overview generated unexpected Task")
        pending = self.api("/api/observe/v1/tasks/" + pending_id)
        require(pending["task"]["status"] == "queued" and not pending["run_attempts"], "read-only pending task executed")
        self.api("/api/control/v1/tasks/" + pending_id + "/cancel", {"requested_by": "owner", "meta": {"expected_version": pending["task"]["version"]}})
        self.save("overview-pending-task-canceled.json", self.api("/api/observe/v1/tasks/" + pending_id))
        self.tmux("send-keys", "-t", pane, "q")
        self.save("overview-verdict.json", {"status": "PASS", "scope": "real TTY list, queued details, selection/scroll, exact native pane navigation, disconnect/recovery",
                  "overview_binary_sha256": sha(self.a.binary), "daemon_binary_sha256": sha(Path('/proc') / str(daemon['pid']) / 'exe'),
                  "native_thread_preserved": ready["engine"]["thread_id"], "pending_task": pending_id,
                  "real_client_sizes": [{"columns": 160, "rows": 45}, {"columns": 80, "rows": 24}],
                  "model_tasks_before": 1, "model_tasks_after": 1, "no_shared_processes_touched": True})

    def overview_scroll(self):
        """Only the necessary overflow check; never repeat the model or recovery."""
        pending_agent = "pending-e2e-" + self.aid.rsplit("-", 1)[1]
        existing = self.out / "overview-scroll-task-before.json"
        continuing = existing.exists()
        if continuing:
            saved = json.loads(existing.read_text())["task"]
            task_id = saved["id"]
            marker = saved["content"].splitlines()[0].removesuffix("-START")
        else:
            marker = "OVERVIEW-SCROLL-" + secrets.token_hex(4)
            content = marker + "-START\n" + "\n".join(f"LINE-{i:02d}: isolated read-only detail scrolling evidence." for i in range(1, 41)) + "\n" + marker + "-END"
            receipt = self.api("/api/control/v1/tasks", {"target_agent_id": pending_agent, "organization_id": "default",
                      "dispatch_mode": "direct", "intent": "query", "content": content})
            task_id = receipt["task_id"]
        detail = self.api("/api/observe/v1/tasks/" + task_id)
        require(detail["task"]["status"] == "queued" and not detail["run_attempts"], "scroll fixture must remain unexecuted")
        self.save("overview-scroll-task-before.json", detail)
        self.client = AttachedClient(self)
        self.client.child.setwinsize(24, 80)
        source = self.windows["overview"]
        pane = source + ".0"
        self.tmux("select-window", "-t", source)
        launcher = self.script("overview", [self.a.binary, "overview", "--socket", self.socket,
                  "--credentials", self.root / "profile/credentials.json", "--worker-dir", self.root / "profile/workers"])
        if not continuing:
            self.tmux("send-keys", "-t", pane, "-l", shlex.quote(launcher))
            self.tmux("send-keys", "-t", pane, "Enter")
        screen = lambda: self.tmux("capture-pane", "-p", "-t", pane)
        details = lambda text: text.split("── 选中详情", 1)[1].split("提示：", 1)[0]
        def detail_mode(wanted):
            if ("详情滚动中" in screen()) != wanted:
                self.tmux("send-keys", "-t", pane, "Tab")
            self.wait(lambda: ("详情滚动中" in screen()) == wanted)
        self.wait(lambda: "在线" in screen() and marker + "-START" in screen(), 60)
        detail_mode(False)
        self.tmux("send-keys", "-t", pane, "Home")
        detail_mode(True)
        self.tmux("send-keys", "-t", pane, "Home")
        self.wait(lambda: "LINE-01" in details(screen()) and "LINE-40" not in details(screen()))
        detail_mode(False)
        initial = self.capture(pane, "overview-scroll-80x24-start", history=False)
        require(marker + "-END" not in details(initial) and "LINE-01" in details(initial), "long details did not overflow initially")
        detail_mode(True)
        self.tmux("send-keys", "-t", pane, "End")
        self.wait(lambda: marker + "-END" in screen())
        end = self.capture(pane, "overview-scroll-80x24-end", history=False)
        require("LINE-40" in details(end) and "LINE-01" not in details(end) and end != initial, "detail End did not reach a different tail view")
        self.tmux("send-keys", "-t", pane, "Home")
        self.wait(lambda: "LINE-01" in details(screen()) and "LINE-40" not in details(screen()))
        start = self.capture(pane, "overview-scroll-80x24-home", history=False)
        require(marker + "-START" in details(start) and marker + "-END" not in details(start), "detail Home did not restore start")
        detail = self.api("/api/observe/v1/tasks/" + task_id)
        require(detail["task"]["status"] == "queued" and not detail["run_attempts"], "scrolling caused unexpected runtime work")
        self.api("/api/control/v1/tasks/" + task_id + "/cancel", {"requested_by": "owner", "meta": {"expected_version": detail["task"]["version"]}})
        canceled = self.api("/api/observe/v1/tasks/" + task_id)
        self.save("overview-scroll-task-canceled.json", canceled)
        require(canceled["task"]["status"] == "canceled" and not canceled["run_attempts"], "scroll task cleanup failed")
        self.tmux("send-keys", "-t", pane, "q")
        self.save("overview-scroll-verdict.json", {"status": "PASS", "scope": "real 80x24 overflow details; Tab/End tail and Home start",
                  "task_id": task_id, "runs": 0, "marker": marker, "task_status_after": "canceled", "new_model_tasks": 0,
                  "overview_binary_sha256": sha(self.a.binary), "harness_sha256": sha(__file__)})

    def collect(self):
        for path in [*self.root.glob("*.stderr"), *self.root.glob("*.stdout")]:
            (self.out / path.name).write_text(redact(path.read_text(errors="replace")).replace(self.password, "[REDACTED]"))
        runtime_dir = self.root / "profile/workers/codex" / self.aid
        if (runtime_dir / "app-server.log").exists():
            (self.out / "app-server.log").write_text(redact((runtime_dir / "app-server.log").read_text(errors="replace")))
        if (runtime_dir / "state.json").exists():
            state = json.loads((runtime_dir / "state.json").read_text())
            tid = state.get("thread_id")
            if tid:
                codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
                # Exact owned thread only; never inspect the latest/arbitrary history.
                day = datetime.datetime.fromisoformat(state["updated_at"].replace("Z", "+00:00")).strftime("%Y/%m/%d")
                for source in (codex_home / "sessions" / day).glob("*" + tid + ".jsonl"):
                    selected = []
                    for line in source.read_text().splitlines():
                        item = json.loads(line)
                        payload = item.get("payload", {})
                        if item.get("type") == "turn_context":
                            selected.append({"timestamp": item.get("timestamp"), "type": "turn_context",
                                "payload": {key: payload.get(key) for key in ("cwd", "model", "effort", "approval_policy", "sandbox_policy")}})
                        elif item.get("type") == "event_msg" and payload.get("type") in {"task_started", "task_complete", "agent_message"}:
                            selected.append(item)
                    self.save("runtime-owned-thread.json", {"source": str(source), "source_sha256": sha(source),
                              "thread_id": tid, "selected_runtime_events": selected})
        processes = []
        for entry in self.procs:
            process = Path("/proc") / str(entry["pid"])
            if process.exists():
                fields = (process / "stat").read_text().rsplit(")", 1)[1].split()
                require(fields[19] == str(entry["starttime"]), "owned process PID reused during evidence collection")
                processes.append({**entry, "state": fields[0], "exe": os.readlink(process / "exe"), "exe_sha256": sha(process / "exe")})
        self.save("runtime-processes.json", processes)
        self.save("processes.json", self.procs)
        self.checkpoint()
        (self.out / "SHA256SUMS").write_text("\n".join(sha(p) + "  " + str(p.relative_to(self.out))
              for p in sorted(self.out.rglob("*")) if p.is_file() and p.name != "SHA256SUMS") + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--resume-fixture", action="store_true", help="Continue evidence checks after setup without another model dispatch")
    parser.add_argument("--phase", choices=("native", "console", "overview", "overview-scroll"), default="native")
    args = parser.parse_args()
    require(args.phase == "native" or args.resume_fixture, "Console phase requires an existing owned fixture")
    require(args.binary.is_absolute() and args.binary.is_file(), "binary must be an existing absolute path")
    require(args.root.is_absolute() and (args.root.exists() if args.resume_fixture else not args.root.exists()), "root existence does not match selected mode")
    os.umask(0o077)
    run = TerminalRun.restore(args) if args.resume_fixture else TerminalRun(args)
    run.save("phase-" + args.phase + "-" + str(time.time_ns()) + ".json", {"at": now(), "phase": args.phase,
             "harness_sha256": sha(__file__), "binary_sha256": sha(args.binary), "source_commit": args.commit,
             "continued_owned_fixture": args.resume_fixture})
    try:
        if not args.resume_fixture:
            run.setup()
        if args.phase == "native":
            run.native(continuing=args.resume_fixture)
        elif args.phase == "console":
            run.console()
        elif args.phase == "overview":
            run.overview()
        else:
            run.overview_scroll()
        print(json.dumps({"status": "PASS", "root": str(run.root), "agent": run.aid, "server": run.server}))
    except Exception as error:
        run.save("failure-" + args.phase + "-" + str(time.time_ns()) + ".json", {"status": "FAIL", "type": type(error).__name__, "message": str(error).replace(run.password, "[REDACTED]"),
                 "note": "Owned fixture retained; no automatic retry or shared-resource cleanup"})
        print(json.dumps({"status": "FAIL", "root": str(run.root), "error_type": type(error).__name__}))
        raise
    finally:
        if run.client:
            run.client.close()
        run.collect()


if __name__ == "__main__":
    main()
