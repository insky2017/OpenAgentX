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


NAME = "openagentx-join"
SOURCE = Path(__file__).resolve().parents[1] / "skills" / NAME


class CheckError(Exception):
    """Only fixed, non-secret diagnostic codes cross the process boundary."""


def emit(value):
    print(json.dumps(value, ensure_ascii=False, indent=2))


def absolute_directory(value):
    path = Path(value).expanduser()
    if not path.is_absolute() or not path.is_dir():
        raise argparse.ArgumentTypeError("必须提供已存在目录的绝对路径")
    return path.resolve()


def location(path, scope):
    present = os.path.lexists(path)
    target_matches = present and path.resolve() == SOURCE.resolve()
    return {
        "scope": scope,
        "path": str(path),
        "exists": present,
        "symlink": path.is_symlink(),
        "skill_exists": (path / "SKILL.md").is_file(),
        "target_matches_source": target_matches,
        "conflict": present and not target_matches,
    }


def install(args):
    if not (SOURCE / "SKILL.md").is_file():
        raise CheckError("source_skill_missing")
    base = Path.home() if args.scope == "user" else args.project
    if base is None:
        raise CheckError("project_scope_requires_project")
    dest = base / ".agents" / "skills" / NAME
    state = location(dest, args.scope)
    if state["exists"]:
        if not state["symlink"] or not state["target_matches_source"]:
            emit({"ok": False, "error": "existing_path_conflict", "location": state})
            return 1
        result = "already_linked"
    else:
        dest.parent.mkdir(parents=True, exist_ok=True)
        target = str(SOURCE) if args.scope == "user" else os.path.relpath(SOURCE, dest.parent)
        try:
            dest.symlink_to(target, target_is_directory=True)
        except FileExistsError:
            # A concurrent install must never replace the winner's entry.
            state = location(dest, args.scope)
            if not state["symlink"] or not state["target_matches_source"]:
                raise CheckError("concurrent_path_conflict")
            result = "already_linked"
        else:
            result = "linked"
    emit({"ok": True, "result": result, "source": str(SOURCE),
          "location": location(dest, args.scope), "discovery_checked": False,
          "message": "链接已就绪；请运行 check 验证 Codex 实际发现状态。"})
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
            if not isinstance(skill, dict) or skill.get("name") != NAME:
                continue
            path, enabled = skill.get("path"), skill.get("enabled")
            if not isinstance(path, str) or not Path(path).is_absolute() or type(enabled) is not bool:
                raise CheckError("skills_list_invalid_skill")
            records.append({"path": path, "enabled": enabled,
                            "target_matches_source": Path(path).resolve() == (SOURCE / "SKILL.md").resolve()})
    if not matched_cwd:
        raise CheckError("skills_list_missing_cwd")
    return {"completed": True, "method": "skills/list", "force_reload": True,
            "same_name_skills": records, "discovery_error_count": error_count,
            "visible": any(r["target_matches_source"] for r in records),
            "enabled": any(r["target_matches_source"] and r["enabled"] for r in records)}


def check(args):
    entries = []
    for directory in [args.project, *args.project.parents]:
        entries.append(location(directory / ".agents" / "skills" / NAME, "project"))
        if (directory / ".git").exists():
            break
    entries.append(location(Path.home() / ".agents" / "skills" / NAME, "user"))
    codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
    entries.append(location(codex_home / "skills" / NAME, "legacy_user"))
    # Home may also be an ancestor of cwd. Report each physical entry only once.
    entries = list({entry["path"]: entry for entry in entries}.values())
    report = {"ok": False, "source": str(SOURCE),
              "source_skill_exists": (SOURCE / "SKILL.md").is_file(),
              "project": str(args.project), "locations": entries,
              "linked": any(e["symlink"] and e["target_matches_source"] for e in entries),
              "same_name_location_count": sum(e["exists"] for e in entries)}
    try:
        report["discovery"] = discovery(args)
    except CheckError as exc:
        report["discovery"] = {"completed": False, "error": str(exc)}
        emit(report)
        return 1
    found = report["discovery"]
    report["conflict"] = any(e["conflict"] for e in entries) or any(
        not r["target_matches_source"] for r in found["same_name_skills"])
    report["duplicate"] = report["same_name_location_count"] > 1 or len(found["same_name_skills"]) > 1
    report["warnings"] = ["发现多个同名入口；同源链接不影响此次发现，但建议保留单一安装范围。"] if report["duplicate"] else []
    report["ok"] = report["source_skill_exists"] and found["visible"] and found["enabled"] and not report["conflict"]
    report["message"] = "仅验证新 app-server 的技能发现；不保证当前已运行 turn 自动注入。"
    emit(report)
    return 0 if report["ok"] else 1


def main():
    parser = argparse.ArgumentParser(description="安装 OpenAgentX 接入 Skill，并验证 Codex 实际可见性。")
    sub = parser.add_subparsers(dest="command", required=True)
    add = sub.add_parser("install", help="建立软链接，不覆盖已有路径")
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
