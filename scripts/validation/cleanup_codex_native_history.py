#!/usr/bin/env python3
"""Delete explicitly selected, exclusively bound Codex test threads via app-server.

Default is a metadata-only dry run. Stop selected OAX Workers before --yes.
This script never unlinks writer locks, edits a database, or stops an existing
app-server. Run before removing OAX bindings, which establish ownership.
"""

import argparse
import json
import os
from pathlib import Path
import selectors
import sqlite3
import subprocess
import sys
import time


# Match the official removal CLI's permanent protection, including the Pay
# terminal alias. Explicit --agent-id is not authorization to bypass this list.
PROTECTED_AGENTS = frozenset({
    "identity-service", "oneaxe-voice", "openagentx", "pay-service",
    "oneaxe-pay", "quote-service", "rhythm", "orchestrator",
})


class RPC:
    def __init__(self, binary):
        self.process = subprocess.Popen(
            [binary, "app-server", "--stdio"], stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
        self.buffer = b""
        self.seq = 0
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)

    def send(self, value):
        self.process.stdin.write((json.dumps(value) + "\n").encode())
        self.process.stdin.flush()

    def call(self, method, params):
        self.seq += 1
        ident = self.seq
        self.send({"id": ident, "method": method, "params": params})
        deadline = time.monotonic() + 30
        while True:
            while b"\n" in self.buffer:
                line, self.buffer = self.buffer.split(b"\n", 1)
                if not line.strip():
                    continue
                message = json.loads(line)
                if message.get("id") != ident:
                    continue
                if "error" in message:
                    raise RuntimeError(f"{method}: {message['error']}")
                if "result" not in message:
                    raise RuntimeError(f"{method}: missing result")
                return message["result"]
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not self.selector.select(remaining):
                raise RuntimeError(f"{method}: response timeout; do not retry deletion blindly")
            chunk = os.read(self.process.stdout.fileno(), 65536)
            if not chunk:
                raise RuntimeError(f"{method}: app-server exited")
            self.buffer += chunk

    def close(self):
        # Only this helper's child process is affected; never a shared server.
        self.selector.close()
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=5)


def ownership(db_path, thread_ids, agent_ids):
    protected = PROTECTED_AGENTS.intersection(agent_ids)
    if protected:
        raise RuntimeError("Protected Agents cannot be cleaned: " + ", ".join(sorted(protected)))
    with sqlite3.connect(db_path.resolve().as_uri() + "?mode=ro", uri=True) as db:
        selected = set(agent_ids)
        rows = {}
        for tid in thread_ids:
            native = list(db.execute(
                "SELECT agent_id,backend_id FROM session_bindings WHERE provider_session_id=?",
                (tid,),
            ))
            external = list(db.execute(
                "SELECT agent_id,mode FROM external_session_bindings WHERE thread_id=?", (tid,),
            ))
            if not native or any(a not in selected or b != "codex" for a, b in native):
                raise RuntimeError(f"{tid}: missing exclusive selected-agent Codex binding")
            if any(a not in selected for a, _ in external):
                raise RuntimeError(f"{tid}: retained Agent has an external binding")
            rows[tid] = sorted(set(a for a, _ in native + external))
        if set(a for values in rows.values() for a in values) != selected:
            raise RuntimeError("Each selected Agent must own at least one selected thread")
        return rows


def stopped_workers(agent_ids):
    result = {}
    for agent in agent_ids:
        # Agent IDs are also passed to systemctl, so prohibit unit/path syntax.
        if not agent or any(c not in "abcdefghijklmnopqrstuvwxyz0123456789-_" for c in agent):
            raise RuntimeError(f"Invalid Agent ID: {agent!r}")
        proc = subprocess.run(
            ["systemctl", "--user", "show", f"openagentx-worker@{agent}.service",
             "--property=ActiveState", "--property=MainPID", "--property=ControlGroup"],
            capture_output=True, text=True, timeout=10, check=True,
        )
        values = dict(line.split("=", 1) for line in proc.stdout.splitlines() if "=" in line)
        result[agent] = values
        if values.get("ActiveState") not in ("inactive", "failed") or values.get("MainPID") != "0":
            raise RuntimeError(f"{agent}: Worker must be stopped before native history cleanup")
        cgroup = values.get("ControlGroup", "")
        if cgroup:
            group = Path("/sys/fs/cgroup") / cgroup.lstrip("/")
            if group.exists():
                for procs in group.rglob("cgroup.procs"):
                    if procs.read_text().strip():
                        raise RuntimeError(f"{agent}: Worker cgroup still contains processes")
    return result


def no_writer_lock(codex_home, tid):
    # Read Linux lock metadata only. Never acquire, unlink, or modify lock files.
    lock_path = codex_home / "thread-writer-locks" / (tid + ".lock")
    if not lock_path.exists():
        return
    stat = lock_path.stat()
    expected = (os.major(stat.st_dev), os.minor(stat.st_dev), stat.st_ino)
    for line in Path("/proc/locks").read_text().splitlines():
        for field in line.split():
            parts = field.split(":")
            if len(parts) != 3:
                continue
            try:
                actual = (int(parts[0], 16), int(parts[1], 16), int(parts[2]))
            except ValueError:
                continue
            if actual == expected:
                raise RuntimeError(f"{tid}: active writer lock; use its owner's lifecycle to release it")


def preflight(rpc, args, codex_home):
    bound = ownership(args.oax_db, args.thread_id, args.agent_id)
    workers = stopped_workers(args.agent_id)
    loaded = rpc.call("thread/loaded/list", {})
    if loaded.get("data") or loaded.get("nextCursor"):
        raise RuntimeError("Temporary app-server unexpectedly contains loaded threads")
    objects = []
    sources = ["cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview",
               "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown"]
    for tid in args.thread_id:
        no_writer_lock(codex_home, tid)
        thread = rpc.call("thread/read", {"threadId": tid, "includeTurns": False})["thread"]
        if thread.get("id") != tid or thread.get("ephemeral") is not False:
            raise RuntimeError(f"{tid}: metadata mismatch or ephemeral thread")
        if thread.get("status", {}).get("type") != "notLoaded":
            raise RuntimeError(f"{tid}: thread is loaded; cleanup blocked")
        for archived in (False, True):
            descendants = rpc.call("thread/list", {
                "ancestorThreadId": tid, "archived": archived,
                "useStateDbOnly": True, "sourceKinds": sources,
            })
            if descendants.get("data") or descendants.get("nextCursor"):
                raise RuntimeError(f"{tid}: spawned descendants require separate review")
        path = Path(thread["path"]).resolve()
        if not any(path.is_relative_to(codex_home / name) for name in ("sessions", "archived_sessions")):
            raise RuntimeError(f"{tid}: rollout is outside Codex session directories")
        objects.append({"thread_id": tid, "agents": bound[tid], "path": str(path),
                        "cwd": thread.get("cwd"), "status": "notLoaded", "descendants": []})
    return {"objects": objects, "workers": workers}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--thread-id", action="append", required=True)
    parser.add_argument("--agent-id", action="append", required=True)
    parser.add_argument("--oax-db", type=Path, default=Path.home() / ".openagentx/data/openagentx.db")
    parser.add_argument("--codex", default="codex")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--yes", action="store_true", help="Permanently delete only the selected native threads")
    args = parser.parse_args()
    if len(set(args.thread_id)) != len(args.thread_id) or len(set(args.agent_id)) != len(args.agent_id):
        parser.error("Duplicate thread or Agent IDs are not allowed")
    import uuid
    for tid in args.thread_id:
        if str(uuid.UUID(tid)) != tid:
            parser.error("Thread IDs must be canonical UUIDs")
    rpc = None
    deleted = []
    try:
        # Fail before launching a helper when the Worker is still running.
        ownership(args.oax_db, args.thread_id, args.agent_id)
        stopped_workers(args.agent_id)
        rpc = RPC(args.codex)
        init = rpc.call("initialize", {
            "clientInfo": {"name": "oax_explicit_history_cleanup", "version": "1"},
            "capabilities": {"experimentalApi": True},
        })
        rpc.send({"method": "initialized", "params": {}})
        home = Path(init["codexHome"]).resolve()
        plan = preflight(rpc, args, home)
        print(json.dumps({"mode": "delete" if args.yes else "dry-run", "plan": plan}, ensure_ascii=False), flush=True)
        if args.yes:
            for obj in plan["objects"]:
                # Repeat mutable ownership/liveness guards immediately before each call.
                ownership(args.oax_db, args.thread_id, args.agent_id)
                stopped_workers(args.agent_id)
                no_writer_lock(home, obj["thread_id"])
                result = rpc.call("thread/delete", {"threadId": obj["thread_id"]})
                if result != {}:
                    raise RuntimeError("Unexpected thread/delete result; verify before retry")
                deleted.append(obj["thread_id"])
                with sqlite3.connect((home / "state_5.sqlite").as_uri() + "?mode=ro", uri=True) as db:
                    if db.execute("SELECT 1 FROM threads WHERE id=?", (obj["thread_id"],)).fetchone():
                        raise RuntimeError("thread/delete returned success but thread metadata remains")
                if Path(obj["path"]).exists():
                    raise RuntimeError("thread/delete returned success but rollout remains")
            print(json.dumps({"deleted": deleted, "verified": True}), flush=True)
        return 0
    except Exception as error:
        print(json.dumps({"error": str(error), "deleted_before_error": deleted}), file=sys.stderr, flush=True)
        return 1
    finally:
        if rpc is not None:
            rpc.close()


if __name__ == "__main__":
    sys.exit(main())
