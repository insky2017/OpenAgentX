#!/usr/bin/env python3
"""Install the repository skill and check discovery with a private Codex process."""

import argparse
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import time


NAMES = ("oax-join", "openagentx-join")
SOURCES = {name: Path(__file__).resolve().parents[1] / "skills" / name for name in NAMES}


class CheckError(Exception):
    """Only fixed, non-secret diagnostic codes cross the process boundary."""


def emit(value):
    print(json.dumps(value, ensure_ascii=False, indent=2))


def absolute_directory(value):
    path = Path(value).expanduser()
    if not path.is_absolute() or not path.is_dir():
        raise argparse.ArgumentTypeError("必须提供已存在目录的绝对路径")
    return path.resolve()


def location(path, scope, name):
    present = os.path.lexists(path)
    target_matches = present and path.resolve() == SOURCES[name].resolve()
    return {
        "name": name,
        "scope": scope,
        "path": str(path),
        "exists": present,
        "symlink": path.is_symlink(),
        "skill_exists": (path / "SKILL.md").is_file(),
        "target_matches_source": target_matches,
        "conflict": present and not target_matches,
    }


def install(args):
    if not all((source / "SKILL.md").is_file() for source in SOURCES.values()):
        raise CheckError("source_skill_missing")
    base = Path.home() if args.scope == "user" else args.project
    if base is None:
        raise CheckError("project_scope_requires_project")
    destinations = {name: base / ".agents" / "skills" / name for name in NAMES}
    # Check both before writing: an old incompatible entry must not cause a
    # partial upgrade. An existing same-source long-name link is left intact.
    states = [location(dest, args.scope, name) for name, dest in destinations.items()]
    if any(state["exists"] and (not state["symlink"] or not state["target_matches_source"])
           for state in states):
        emit({"ok": False, "error": "existing_path_conflict", "locations": states})
        return 1
    installed = []
    for name, dest in destinations.items():
        result = "already_linked"
        if not os.path.lexists(dest):
            dest.parent.mkdir(parents=True, exist_ok=True)
            target = str(SOURCES[name]) if args.scope == "user" else os.path.relpath(SOURCES[name], dest.parent)
            try:
                dest.symlink_to(target, target_is_directory=True)
            except FileExistsError:
                state = location(dest, args.scope, name)
                if not state["symlink"] or not state["target_matches_source"]:
                    raise CheckError("concurrent_path_conflict")
            else:
                result = "linked"
        state = location(dest, args.scope, name)
        if not state["symlink"] or not state["target_matches_source"]:
            raise CheckError("concurrent_path_conflict")
        installed.append({"name": name, "result": result, "source": str(SOURCES[name]), "location": state})
    emit({"ok": True, "skills": installed, "discovery_checked": False,
          "message": "标准 oax-join 与兼容 openagentx-join 链接已就绪；请运行 check 验证实际发现状态。"})
    return 0


class SkillServer:
    def __init__(self, binary, cwd, timeout):
        self.binary, self.cwd, self.timeout = binary, cwd, timeout
        self.process = None
        self.selector = selectors.DefaultSelector()
        self.buffer = b""
        self.bytes_read = 0

    def __enter__(self):
        try:
            self.process = subprocess.Popen(
                [self.binary, "app-server", "--listen", "stdio://"],
                cwd=self.cwd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL, start_new_session=True,
            )
        except OSError:
            self.selector.close()
            raise CheckError("codex_start_failed") from None
        os.set_blocking(self.process.stdout.fileno(), False)
        self.selector.register(self.process.stdout, selectors.EVENT_READ)
        self.deadline = time.monotonic() + self.timeout
        return self

    def __exit__(self, *_):
        self.selector.close()
        if self.process is None:
            return
        try:
            self.process.stdin.close()
        except OSError:
            pass
        # The fresh session belongs only to this invocation, never a shared server.
        try:
            os.killpg(self.process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            self.process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(self.process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            self.process.wait(timeout=3)
        self.process.stdout.close()

    def send(self, message):
        try:
            self.process.stdin.write((json.dumps(message) + "\n").encode())
            self.process.stdin.flush()
        except (BrokenPipeError, OSError):
            raise CheckError("codex_stdin_closed") from None

    def request(self, request_id, method, params):
        self.send({"id": request_id, "method": method, "params": params})
        while True:
            remaining = self.deadline - time.monotonic()
            if remaining <= 0:
                raise CheckError("codex_response_timeout")
            if b"\n" not in self.buffer:
                if not self.selector.select(remaining):
                    raise CheckError("codex_response_timeout")
                data = os.read(self.process.stdout.fileno(), 65536)
                if not data:
                    raise CheckError("codex_stdout_closed")
                self.bytes_read += len(data)
                if self.bytes_read > 8 * 1024 * 1024:
                    raise CheckError("codex_output_limit")
                self.buffer += data
                continue
            line, self.buffer = self.buffer.split(b"\n", 1)
            try:
                reply = json.loads(line)
            except (ValueError, UnicodeDecodeError):
                raise CheckError("codex_invalid_json") from None
            if not isinstance(reply, dict):
                raise CheckError("codex_invalid_response")
            if reply.get("id") != request_id:
                continue
            if "error" in reply:
                raise CheckError("codex_rpc_error_" + method.replace("/", "_"))
            if not isinstance(reply.get("result"), dict):
                raise CheckError("codex_invalid_result")
            return reply["result"]


def discovery(args):
    with SkillServer(args.codex_binary, args.project, args.timeout) as server:
        server.request(1, "initialize", {
            "clientInfo": {"name": "openagentx_skill_check", "version": "1"},
            "capabilities": {"experimentalApi": True},
        })
        server.send({"method": "initialized", "params": {}})
        result = server.request(2, "skills/list", {
            "cwds": [str(args.project)], "forceReload": True,
        })
    groups = result.get("data")
    if not isinstance(groups, list):
        raise CheckError("skills_list_invalid_data")
    records, error_count, matched_cwd = [], 0, False
    for group in groups:
        if not isinstance(group, dict) or group.get("cwd") != str(args.project):
            continue
        matched_cwd = True
        skills, errors = group.get("skills"), group.get("errors")
        if not isinstance(skills, list) or not isinstance(errors, list):
            raise CheckError("skills_list_invalid_group")
        error_count += len(errors)
        for skill in skills:
            if not isinstance(skill, dict) or skill.get("name") not in NAMES:
                continue
            name = skill["name"]
            path, enabled = skill.get("path"), skill.get("enabled")
            if not isinstance(path, str) or not Path(path).is_absolute() or type(enabled) is not bool:
                raise CheckError("skills_list_invalid_skill")
            records.append({"name": name, "path": path, "enabled": enabled,
                            "target_matches_source": Path(path).resolve() == (SOURCES[name] / "SKILL.md").resolve()})
    if not matched_cwd:
        raise CheckError("skills_list_missing_cwd")
    return {"completed": True, "method": "skills/list", "force_reload": True,
            "same_name_skills": records, "discovery_error_count": error_count,
            "visible": all(any(r["name"] == name and r["target_matches_source"] for r in records) for name in NAMES),
            "enabled": all(any(r["name"] == name and r["target_matches_source"] and r["enabled"] for r in records) for name in NAMES)}


def check(args):
    entries = []
    for directory in [args.project, *args.project.parents]:
        entries.extend(location(directory / ".agents" / "skills" / name, "project", name) for name in NAMES)
        if (directory / ".git").exists():
            break
    entries.extend(location(Path.home() / ".agents" / "skills" / name, "user", name) for name in NAMES)
    codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
    entries.extend(location(codex_home / "skills" / name, "legacy_user", name) for name in NAMES)
    entries = list({entry["path"]: entry for entry in entries}.values())
    report = {"ok": False, "sources": {name: str(source) for name, source in SOURCES.items()},
              "source_skill_exists": all((source / "SKILL.md").is_file() for source in SOURCES.values()),
              "project": str(args.project), "locations": entries,
              "linked": all(any(e["name"] == name and e["symlink"] and e["target_matches_source"]
                                for e in entries) for name in NAMES)}
    try:
        report["discovery"] = discovery(args)
    except CheckError as exc:
        report["discovery"] = {"completed": False, "error": str(exc)}
        emit(report)
        return 1
    found = report["discovery"]
    records = found["same_name_skills"]
    report["skills"] = [{
        "name": name, "source": str(SOURCES[name] / "SKILL.md"),
        "visible": any(r["name"] == name and r["target_matches_source"] for r in records),
        "enabled": any(r["name"] == name and r["target_matches_source"] and r["enabled"] for r in records),
        "same_name_location_count": sum(e["name"] == name and e["exists"] for e in entries),
        "conflict": any(e["name"] == name and e["conflict"] for e in entries) or any(
            r["name"] == name and not r["target_matches_source"] for r in records),
        "duplicate": sum(e["name"] == name and e["exists"] for e in entries) > 1 or sum(
            r["name"] == name for r in records) > 1,
    } for name in NAMES]
    report["conflict"] = any(s["conflict"] for s in report["skills"])
    report["duplicate"] = any(s["duplicate"] for s in report["skills"])
    report["warnings"] = ["发现同一名称的多个入口；同源链接不影响此次发现，但建议保留单一安装范围。"] if report["duplicate"] else []
    report["ok"] = report["source_skill_exists"] and found["visible"] and found["enabled"] and not report["conflict"]
    report["message"] = "分别核对标准短名与兼容长名；仅验证新 app-server 发现，不保证当前已运行 turn 自动注入。"
    emit(report)
    return 0 if report["ok"] else 1


def main():
    parser = argparse.ArgumentParser(description="安装 OpenAgentX 接入 Skill，并验证 Codex 实际可见性。")
    sub = parser.add_subparsers(dest="command", required=True)
    add = sub.add_parser("install", help="显式安装 oax-join 与 openagentx-join 两个链接，不覆盖已有路径")
    add.add_argument("--scope", choices=["user", "project"], required=True)
    add.add_argument("--project", type=absolute_directory)
    probe = sub.add_parser("check", help="调用独立 app-server 的 skills/list，不创建 thread/turn")
    probe.add_argument("--project", type=absolute_directory, default=Path.cwd().resolve())
    probe.add_argument("--codex-binary", default="codex")
    probe.add_argument("--timeout", type=float, default=30, help="整个协议往返超时秒数，范围 1–120")
    args = parser.parse_args()
    if args.command == "check" and not 1 <= args.timeout <= 120:
        parser.error("--timeout 必须在 1–120 秒之间")
    if args.command == "install" and args.scope == "user" and args.project is not None:
        parser.error("--project 仅用于 project 安装")
    try:
        return install(args) if args.command == "install" else check(args)
    except CheckError as exc:
        emit({"ok": False, "error": str(exc)})
    except (OSError, RuntimeError):
        emit({"ok": False, "error": "filesystem_or_process_error"})
    return 1


if __name__ == "__main__":
    sys.exit(main())
