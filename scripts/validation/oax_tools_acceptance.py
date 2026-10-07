#!/usr/bin/env python3
"""Exercise oaxctl against an isolated real OpenAgentX daemon over local TLS.

The TLS server only forwards bytes to the daemon. An isolated Worker registers
the backend and is paused before dispatch; no model or Run is started.
All state-changing requests go through the product CLI or HTTP API; SQLite is
opened read-only for independent counts. Private fixture material stays outside
the repository.
"""

import argparse
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import socketserver
import sqlite3
import ssl
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "e2e"))
from agy_live import pty_command, redact, write


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


class TLSProxy(socketserver.ThreadingMixIn, HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


class Forward(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.forward()

    def do_POST(self):
        self.forward()

    def forward(self):
        length = int(self.headers.get("Content-Length", "0"))
        data = self.rfile.read(length) if length else None
        upstream = http.client.HTTPConnection("127.0.0.1", self.server.upstream_port, timeout=20)
        headers = {key: value for key, value in self.headers.items()
                   if key.lower() not in {"host", "connection", "content-length"}}
        headers["Host"] = "127.0.0.1"
        try:
            upstream.request(self.command, self.path, body=data, headers=headers)
            response = upstream.getresponse()
            body = response.read()
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in {"connection", "transfer-encoding", "content-length"}:
                    self.send_header(key, value)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        finally:
            upstream.close()

    def log_message(self, *_):
        pass


def run(args):
    root = args.private.resolve()
    evidence = args.evidence.resolve()
    require(not root.exists() and not evidence.exists(), "use new private and evidence paths")
    os.umask(0o077)
    root.mkdir(parents=True, mode=0o700)
    evidence.mkdir(parents=True)
    password = secrets.token_urlsafe(30)
    (root / "password").write_text(password)
    workspace = root / "workspace"
    workspace.mkdir()
    (workspace / "ROLE.md").write_text("Isolated oaxctl acceptance identity. No Worker is installed.\n")
    env = {key: value for key, value in os.environ.items() if "proxy" not in key.lower()
           and not key.startswith(("OAXCTL_", "OPENAGENTX_"))}
    env.update(OPENAGENTX_HOME=str(root / "profile"), OPENAGENTX_SOCKET_PATH=str(root / "daemon.sock"),
               XDG_CONFIG_HOME=str(root / "config"), XDG_STATE_HOME=str(root / "state"), TERM="xterm-256color")
    binary = args.binary.resolve(strict=True)
    client = args.client.resolve(strict=True)
    source = {"binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "client_sha256": hashlib.sha256(client.read_bytes()).hexdigest(),
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "source_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "scope": "isolated real daemon, Worker registration, and SQLite; no model/Run/business execution"}
    write(evidence / "manifest.json", source)
    daemon = None
    proxy = None
    worker = None
    commands = []

    def cli(label, argv, expected=0):
        result = subprocess.run([str(client), *argv], env=env, text=True, capture_output=True,
                                timeout=35, stdin=subprocess.DEVNULL)
        stdout = result.stdout.replace(password, "[REDACTED]")
        stderr = result.stderr.replace(password, "[REDACTED]")
        record = {"label": label, "argv": argv, "exit_code": result.returncode,
                  "stdout": stdout, "stderr": stderr}
        commands.append(record)
        require(result.returncode == expected, f"{label}: exit {result.returncode}, expected {expected}: {stderr}")
        return json.loads(result.stdout) if result.stdout.strip().startswith(("{", "[")) else result.stdout

    try:
        for label, argv in (("init", [str(binary), "init"]),):
            result = pty_command(argv, password, env)
            write(evidence / f"{label}.json", result)
            require(result["exit_code"] == 0, f"{label} failed")
        identity = root / "identity.yaml"
        agent = "oaxctl-acceptance-" + secrets.token_hex(4)
        identity.write_text(f"version: 1\nagent_id: {agent}\nprincipal_id: agent-{agent}\n"
                            f"organization_id: default\ndisplay_name: OAX client acceptance\n"
                            f"profile:\n  instructions_path: {workspace / 'ROLE.md'}\n"
                            f"  workspace_root: {workspace}\n")
        applied = pty_command([str(binary), "agent", "apply", "--file", str(identity)], password, env)
        write(evidence / "agent-apply.json", applied)
        require(applied["exit_code"] == 0, "isolated agent apply failed")
        daemon_port = port()
        with (root / "daemon.stdout").open("wb") as out, (root / "daemon.stderr").open("wb") as err:
            daemon = subprocess.Popen([str(binary), "serve", "--http-addr", f"127.0.0.1:{daemon_port}",
                                       "--web-dir", str(workspace)], env=env, stdout=out, stderr=err,
                                      start_new_session=True)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if daemon.poll() is not None:
                raise RuntimeError("isolated daemon exited during startup")
            try:
                with socket.create_connection(("127.0.0.1", daemon_port), timeout=1):
                    break
            except OSError:
                time.sleep(.1)
        else:
            raise RuntimeError("isolated daemon HTTP port unavailable")
        cert = root / "cert.pem"
        key = root / "key.pem"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                        "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1",
                        "-keyout", str(key), "-out", str(cert)], check=True, capture_output=True, timeout=20)
        proxy = TLSProxy(("127.0.0.1", 0), Forward)
        proxy.upstream_port = daemon_port
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(str(cert), str(key))
        proxy.socket = context.wrap_socket(proxy.socket, server_side=True)
        threading.Thread(target=proxy.serve_forever, daemon=True).start()
        env.update(OAXCTL_URL=f"https://localhost:{proxy.server_port}", OAXCTL_USER="owner",
                   OAXCTL_PASSWORD=password, SSL_CERT_FILE=str(cert))
        login = cli("login", ["login"])
        require(login.get("principal", {}).get("username") == "owner", "login principal mismatch")
        whoami = cli("whoami", ["whoami"])
        require(whoami.get("principal", {}).get("username") == "owner", "session principal mismatch")
        session_view = cli("session-view", ["get", "/api/auth/v1/session"])
        require(session_view.get("principal", {}).get("username") == "owner" and
                "csrf_token" not in session_view, "public session leaked CSRF or lost principal")
        agents = cli("agents", ["agents", "--json"])
        require(any(x.get("agent_id") == agent for x in agents), "isolated agent absent")
        agents_text = cli("agents-text", ["agents"])
        require(agent in agents_text and "ready=" in agents_text and "can_start_now=" in agents_text,
                "agents text omitted readiness fields")
        task_list = cli("tasks", ["tasks", "--json", "--agent", agent])
        require(task_list.get("tasks") == [], "isolation profile unexpectedly contains tasks")
        worker_config = root / "worker.yaml"
        worker_config.write_text(f"""version: 1
agent_id: {agent}
transport: unix
unix_socket: {root / 'daemon.sock'}
heartbeat_interval: 1s
mailbox_wait: 5s
control_wait: 5s
shutdown_timeout: 10s
network_materialization_dir: {root / 'network'}
runtime_backends:
  - backend_id: primary
    adapter_id: agy-batch
    options:
      binary: /bin/true
      models: [fixture-model]
      working_dir: {workspace}
      timeout: 30s
""")
        with (root / "worker.stdout").open("wb") as out, (root / "worker.stderr").open("wb") as err:
            worker = subprocess.Popen([str(binary), "worker", "run", "--config", str(worker_config)],
                                      env=env, stdout=out, stderr=err, start_new_session=True)
        ready_deadline = time.monotonic() + 20
        while time.monotonic() < ready_deadline:
            require(worker.poll() is None, "isolated Worker exited before registration")
            overview = cli("overview-before-freeze", ["get", "/api/observe/v1/overview"])
            match = next((item for item in overview.get("workers", []) if item.get("agent_id") == agent), None)
            if match and match.get("status") == "online":
                break
            time.sleep(.5)
        else:
            raise RuntimeError("isolated Worker did not register online")
        os.kill(worker.pid, signal.SIGSTOP)
        write(evidence / "worker-freeze.json", {"pid": worker.pid, "status_before": match.get("status"),
                                                 "reason": "hold isolated Worker before dispatch; no runtime model call"})
        task_key = "oaxctl-fixture-task-" + secrets.token_hex(6)
        dispatch = cli("dispatch", ["dispatch", agent, "isolated API queue fixture", "--key", task_key])
        task = dispatch["task_id"]
        require(dispatch["idempotency_key"] == task_key, "dispatch key mismatch")
        replay = cli("dispatch-replay", ["dispatch", agent, "isolated API queue fixture", "--key", task_key])
        require(replay["task_id"] == task, "dispatch replay created second task")
        cli("dispatch-key-conflict", ["dispatch", agent, "different fixture content", "--key", task_key], expected=2)
        detail = cli("show", ["show", task])
        require(detail["task"]["status"] == "queued", "unworked task must remain queued")
        version = detail["task"]["version"]
        msg_key = "oaxctl-fixture-message-" + secrets.token_hex(6)
        say = cli("say", ["say", task, "isolated supplement", "--key", msg_key,
                          "--expected-version", str(version)])
        say_replay = cli("say-replay", ["say", task, "isolated supplement", "--key", msg_key,
                                 "--expected-version", str(version)])
        require(say["message_id"] == say_replay["message_id"], "message replay created another message")
        cli("say-stale-version", ["say", task, "stale fixture supplement", "--key",
                                   "oaxctl-fixture-stale-" + secrets.token_hex(6),
                                   "--expected-version", str(version)], expected=2)
        detail = cli("show-after-say", ["show", task])
        require(detail["task"]["version"] == version + 1, "Task version did not advance exactly once")
        cli("wait-timeout", ["wait", task, "--timeout", "0", "--quiet"], expected=3)
        db_file = root / "profile" / "data" / "openagentx.db"
        with sqlite3.connect(db_file.as_uri() + "?mode=ro", uri=True) as db:
            counts = {name: db.execute(sql, (task,)).fetchone()[0] for name, sql in {
                "tasks": "SELECT count(*) FROM tasks WHERE task_id=?",
                "messages": "SELECT count(*) FROM messages WHERE task_id=?",
                "runs": "SELECT count(*) FROM run_attempts WHERE task_id=?",
            }.items()}
            state = db.execute("SELECT status, version FROM tasks WHERE task_id=?", (task,)).fetchone()
        require(counts == {"tasks": 1, "messages": 2, "runs": 0}, f"unexpected SQLite counts: {counts}")
        require(state == ("queued", version + 1), "SQLite Task status/version mismatch")
        write(evidence / "database-crosscheck.json", {"task_id": task, "counts": counts,
                                                        "task_status": state[0], "task_version": state[1],
                                                        "access": "SQLite URI mode=ro"})
        write(evidence / "result.json", {"status": "PASS", "agent_id": agent, "task_id": task,
                                          "cases": ["HTTPS session/cookie/CSRF and public session redaction",
                                                    "agents readiness/tasks", "dispatch replay/key conflict",
                                                    "show", "say version/replay/stale version", "wait timeout",
                                                    "SQLite crosscheck"],
                                          "limit": "Isolated Worker was paused before dispatch; no model or Run started. Business execution was not tested."})
    except Exception as error:
        write(evidence / "result.json", {"status": "FAIL", "error": str(error).replace(password, "[REDACTED]")})
        raise
    finally:
        write(evidence / "client-commands.json", redact(commands))
        if proxy is not None:
            proxy.shutdown()
            proxy.server_close()
        if worker is not None and worker.poll() is None:
            os.kill(worker.pid, signal.SIGCONT)
            worker.terminate()
            worker.wait(timeout=10)
        if daemon is not None and daemon.poll() is None:
            daemon.terminate()
            daemon.wait(timeout=10)
        write(evidence / "runtime.json", {"isolated_daemon_pid": daemon.pid if daemon else None,
                                           "isolated_daemon_exit": daemon.returncode if daemon else None,
                                           "isolated_worker_pid": worker.pid if worker else None,
                                           "isolated_worker_exit": worker.returncode if worker else None,
                                           "private_root": str(root)})


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--private", type=Path, required=True)
    parser.add_argument("--evidence", type=Path, required=True)
    run(parser.parse_args())
