"""Run the actual oaxctl process against a local HTTP contract server."""

import http.server
import json
import os
import pathlib
import socket
import subprocess
import tempfile
import threading
import unittest


SCRIPT = pathlib.Path(__file__).resolve().parents[2] / "clients" / "oaxctl"


def header(call, name):
    return next(value for key, value in call[3].items() if key.lower() == name.lower())


class Handler(http.server.BaseHTTPRequestHandler):
    calls = []
    csrf_number = 0

    def log_message(self, *_args):
        pass

    def respond(self, value, status=200, cookie=False):
        encoded = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        if cookie:
            self.send_header("Set-Cookie", "openagentx_session=test-session; Path=/; HttpOnly")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def do_GET(self):
        self.calls.append(("GET", self.path, None, dict(self.headers)))
        if self.path == "/api/observe/v1/redirect":
            self.send_response(302)
            self.send_header("Location", "/api/auth/v1/session")
            self.end_headers()
            return
        if self.path == "/api/auth/v1/session":
            if "openagentx_session=test-session" not in self.headers.get("Cookie", ""):
                return self.respond({}, 401)
            type(self).csrf_number += 1
            return self.respond({"csrf_token": f"csrf-{self.csrf_number}", "principal": {"username": "owner"}, "absolute_expires_at": "later"})
        if self.path == "/api/observe/v1/agents":
            return self.respond([{"agent_id": "test-agent", "organization_id": "default", "display_name": "Test Agent"}])
        if self.path == "/api/observe/v1/overview":
            return self.respond({"agents": [{"agent_id": "test-agent", "display_name": "Test Agent",
                                             "readiness": {"ready": True, "can_start_now": False, "reason": "正在工作，新任务将排队"}}]})
        if self.path.startswith("/api/observe/v1/tasks/task-pages?"):
            return self.respond({"task": {"id": "task-pages", "version": 2, "status": "running"},
                                 "events": [{"sequence": 1, "event_type": "note"}], "has_more_live_events": True})
        if self.path.startswith("/api/observe/v1/tasks/task-1?"):
            return self.respond({"task": {"id": "task-1", "version": 2, "status": "waiting_input"}, "events": [], "has_more_live_events": False})
        return self.respond({}, 404)

    def do_POST(self):
        size = int(self.headers.get("Content-Length", "0"))
        body = json.loads(self.rfile.read(size))
        self.calls.append(("POST", self.path, body, dict(self.headers)))
        if self.path == "/api/auth/v1/login":
            if body != {"username": "owner", "password": "test-password"}:
                return self.respond({}, 401)
            return self.respond({"csrf_token": "csrf-0", "principal": {"username": "owner"}}, cookie=True)
        if self.headers.get("Cookie") != "openagentx_session=test-session":
            return self.respond({}, 401)
        if self.headers.get("X-CSRF-Token") != f"csrf-{self.csrf_number}":
            return self.respond({}, 403)
        if self.headers.get("Idempotency-Key") != body.get("meta", {}).get("idempotency_key"):
            return self.respond({}, 400)
        if self.path == "/api/control/v1/tasks":
            if body["content"] == "disconnect":
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()
                return
            return self.respond({"task_id": "task-1", "task_status": "queued", "task_version": 1, "sequence": 1})
        if self.path == "/api/control/v1/tasks/task-1/messages":
            if body["meta"].get("expected_version") != 2:
                return self.respond({}, 400)
            return self.respond({"task_id": "task-1", "task_status": "waiting_input", "task_version": 3, "sequence": 2})
        return self.respond({}, 404)


class OaxctlHTTPTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def setUp(self):
        Handler.calls = []
        Handler.csrf_number = 0
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.env = dict(os.environ)
        self.env.update({"XDG_CONFIG_HOME": self.temp.name + "/config", "XDG_STATE_HOME": self.temp.name + "/state",
                         "OAXCTL_URL": f"http://127.0.0.1:{self.server.server_port}",
                         "OAXCTL_USER": "owner", "OAXCTL_PASSWORD": "test-password"})

    def run_cli(self, *args):
        return subprocess.run([str(SCRIPT), *args], env=self.env, capture_output=True, text=True, timeout=10)

    def test_login_dispatch_message_and_wait(self):
        login = self.run_cli("login")
        self.assertEqual(login.returncode, 0, login.stderr)
        state = pathlib.Path(self.temp.name) / "state" / "oaxctl"
        self.assertEqual(state.stat().st_mode & 0o777, 0o700)
        self.assertEqual((state / "cookies.txt").stat().st_mode & 0o777, 0o600)
        self.assertEqual((state / "session.json").stat().st_mode & 0o777, 0o600)
        dispatch = self.run_cli("dispatch", "test-agent", "read only", "--key", "stable-key")
        self.assertEqual(dispatch.returncode, 0, dispatch.stderr)
        post = [call for call in Handler.calls if call[0] == "POST" and call[1] == "/api/control/v1/tasks"][-1]
        self.assertEqual(post[2]["meta"]["idempotency_key"], "stable-key")
        self.assertEqual(header(post, "Idempotency-Key"), "stable-key")
        self.assertEqual(header(post, "X-CSRF-Token"), "csrf-1")
        self.assertIn("oaxctl", header(post, "User-Agent"))
        say = self.run_cli("say", "task-1", "additional context", "--key", "message-key", "--expected-version", "2")
        self.assertEqual(say.returncode, 0, say.stderr)
        message = [call for call in Handler.calls if call[0] == "POST" and call[1].endswith("/messages")][-1]
        self.assertEqual(message[2]["meta"], {"idempotency_key": "message-key", "expected_version": 2})
        self.assertEqual(header(message, "X-CSRF-Token"), "csrf-2")
        before = len([call for call in Handler.calls if call[0] == "POST"])
        waiting = self.run_cli("wait", "task-1", "--timeout", "0")
        self.assertEqual(waiting.returncode, 3, waiting.stderr)
        self.assertEqual(len([call for call in Handler.calls if call[0] == "POST"]), before)

    def test_retry_requires_version_and_no_write_is_sent(self):
        self.assertEqual(self.run_cli("say", "task-1", "text", "--key", "same-key").returncode, 2)
        self.assertFalse(any(call[0] == "POST" and call[1].endswith("/messages") for call in Handler.calls))

    def test_lost_write_response_is_not_retried(self):
        self.assertEqual(self.run_cli("login").returncode, 0)
        outcome = self.run_cli("dispatch", "test-agent", "disconnect", "--key", "lost-response-key")
        self.assertEqual(outcome.returncode, 2)
        self.assertIn("lost-response-key", outcome.stderr)
        self.assertEqual(len([call for call in Handler.calls if call[0] == "POST" and call[1] == "/api/control/v1/tasks"]), 1)

    def test_session_redaction_readiness_and_redirect(self):
        self.assertEqual(self.run_cli("login").returncode, 0)
        session = self.run_cli("get", "/api/auth/v1/session")
        self.assertEqual(session.returncode, 0, session.stderr)
        self.assertNotIn("csrf-", session.stdout)
        self.assertIn("absolute_expires_at", session.stdout)
        stored = json.loads((pathlib.Path(self.temp.name) / "state" / "oaxctl" / "session.json").read_text())
        self.assertEqual(stored["csrf_token"], "csrf-1")
        agents = self.run_cli("agents")
        self.assertEqual(agents.returncode, 0, agents.stderr)
        self.assertIn("正在工作，新任务将排队", agents.stdout)
        self.assertIn("ready=yes", agents.stdout)
        self.assertIn("can_start_now=no", agents.stdout)
        blocked = self.run_cli("get", "/api/observe/v1/redirect")
        self.assertEqual(blocked.returncode, 2)
        self.assertEqual(len([call for call in Handler.calls if call[1] == "/api/auth/v1/session"]), 1)

    def test_relative_xdg_falls_back_and_paging_respects_deadline(self):
        self.assertEqual(self.run_cli("login").returncode, 0)
        waiting = self.run_cli("wait", "task-pages", "--timeout", "0", "--quiet")
        self.assertEqual(waiting.returncode, 3, waiting.stderr)
        self.assertEqual(len([call for call in Handler.calls if call[1].startswith("/api/observe/v1/tasks/task-pages?")]), 1)
        self.env["XDG_CONFIG_HOME"] = "relative-config"
        self.env["XDG_STATE_HOME"] = "relative-state"
        self.env["HOME"] = self.temp.name
        self.assertEqual(self.run_cli("login").returncode, 0)
        self.assertTrue((pathlib.Path(self.temp.name) / ".local" / "state" / "oaxctl" / "session.json").exists())

    def test_invalid_host_is_rejected(self):
        self.env["OAXCTL_URL"] = "https://"
        result = self.run_cli("agents")
        self.assertEqual(result.returncode, 2)
        self.assertIn("invalid server URL", result.stderr)


if __name__ == "__main__":
    unittest.main()
