#!/usr/bin/env python3
"""Prepare an OAX handoff through the installed CLI; never activate it."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import uuid


LIMIT = 1 << 20
PATHS = {
    "worker_dir": ("OPENAGENTX_WORKER_CONFIG_DIR", "workers"),
    "file": ("OPENAGENTX_FLEET_MANIFEST", "fleet.yaml"),
    "db": ("OPENAGENTX_DATABASE_PATH", "data/openagentx.db"),
    "socket": ("OPENAGENTX_SOCKET_PATH", "run/openagentx.sock"),
    "credentials": ("OPENAGENTX_CREDENTIALS_PATH", "credentials.json"),
}


class IntakeError(Exception):
    pass


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode()


def text_field(value, label, single_line=False):
    if not isinstance(value, str) or not value.strip() or "\x00" in value:
        raise IntakeError(f"{label} 必须是非空字符串，且不能含 NUL")
    if single_line and any(ord(c) < 32 for c in value):
        raise IntakeError(f"{label} 必须是单行文本")
    return value


def absolute(value, label):
    text_field(value, label, True)
    if not os.path.isabs(value):
        raise IntakeError(f"{label} 必须是绝对路径")
    # Match localprofile's lexical clean; do not reinterpret symlinked profiles.
    return os.path.normpath(value)


def identifier(value, label):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", value):
        raise IntakeError(f"{label} 必须是 1–64 位字母、数字、点、下划线或连字符，首位为字母或数字")
    return value


def object_fields(value, required, optional, label):
    if not isinstance(value, dict):
        raise IntakeError(f"{label} 必须是对象")
    if required - value.keys():
        raise IntakeError(f"{label} 缺少字段: {', '.join(sorted(required - value.keys()))}")
    if value.keys() - required - optional:
        # Do not echo arbitrary user input or mistaken secret fields.
        raise IntakeError(f"{label} 含未知字段；请对照 --example")


def no_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise IntakeError("JSON 含重复字段")
        result[key] = value
    return result


def read_regular(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > LIMIT:
            raise IntakeError("输入必须是至多 1 MiB 的普通文件")
        data = stream.read(LIMIT + 1)
        if len(data) > LIMIT:
            raise IntakeError("输入超过 1 MiB")
        return data


def load_brief(path):
    raw = read_regular(path)
    try:
        value = json.loads(raw, object_pairs_hook=no_duplicate_keys)
    except (ValueError, UnicodeError):
        raise IntakeError("brief 不是有效 UTF-8 JSON") from None
    object_fields(value, {"agent_id", "workspace", "role", "peers", "handoff"},
                  {"name", "history", "thread_id"}, "brief")
    value["agent_id"] = identifier(value["agent_id"], "agent_id")
    if value["agent_id"] == "overview":
        raise IntakeError("overview 是保留 Agent ID")
    value["name"] = text_field(value.get("name", value["agent_id"]), "name", True)
    value["workspace"] = absolute(value["workspace"], "workspace")
    if not Path(value["workspace"]).is_dir():
        raise IntakeError("workspace 必须是已存在的目录")
    text_field(value["role"], "role")
    if not isinstance(value["peers"], list):
        raise IntakeError("peers 必须是列表；[] 表示用户明确无需协作对象")
    for peer in value["peers"]:
        identifier(peer, "peers 中的 Agent ID")
    object_fields(value["handoff"], {"completed", "next", "uncertain"}, set(), "handoff")
    for key, items in value["handoff"].items():
        if not isinstance(items, list):
            raise IntakeError(f"handoff.{key} 必须是字符串列表")
        for item in items:
            text_field(item, f"handoff.{key}")
    value.setdefault("history", "current")
    if value["history"] not in ("current", "summary"):
        raise IntakeError("history 只接受 current 或 summary")
    if value["history"] == "summary" and "thread_id" in value:
        raise IntakeError("history=summary 不接受 thread_id；请选择一种历史方式")
    return value, {"brief_file": str(path), "brief_sha256": hashlib.sha256(raw).hexdigest()}


def checked_uuid(value, source):
    if not isinstance(value, str) or not re.fullmatch(
            r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}", value):
        raise IntakeError(f"{source} 必须是非空 UUID")
    parsed = uuid.UUID(value)
    if parsed.int == 0:
        raise IntakeError(f"{source} 不能是全零 UUID")
    return str(parsed)


def thread_source(brief):
    if brief.get("history", "current") == "summary":
        return {"history": "summary", "thread_id": None, "sources": ["brief.history"],
                "state": "summary_only"}
    candidates = {}
    if "thread_id" in brief:
        candidates["brief.thread_id"] = checked_uuid(brief["thread_id"], "brief.thread_id")
    for key in ("CODEX_THREAD_ID", "CODEX_SESSION_ID"):
        if key in os.environ:
            candidates[key] = checked_uuid(os.environ[key], key)
    if len(set(candidates.values())) > 1:
        raise IntakeError("thread_id 来源不一致；核对 brief.thread_id、CODEX_THREAD_ID 与 CODEX_SESSION_ID")
    return {"history": "current", "thread_id": next(iter(candidates.values()), None),
            "sources": list(candidates), "state": "captured" if candidates else "missing"}


def profile_paths(args):
    profile = {}
    sources = {}
    root = absolute(args.profile_home, "--profile-home") if args.profile_home is not None else None
    for key, (env_key, suffix) in PATHS.items():
        explicit = getattr(args, key)
        if explicit is not None:
            value, source = explicit, "explicit"
        elif root is not None:
            value, source = os.path.join(root, suffix), "profile_home"
        elif env_key in os.environ:
            value, source = os.environ[env_key], env_key
        elif "OPENAGENTX_HOME" in os.environ:
            value = os.path.join(absolute(os.environ["OPENAGENTX_HOME"], "OPENAGENTX_HOME"), suffix)
            source = "OPENAGENTX_HOME"
        else:
            user_home = os.environ.get("HOME")
            if not user_home:
                raise IntakeError("HOME 不可用；请提供 --profile-home 或五个路径参数")
            value, source = os.path.join(absolute(user_home, "HOME"), ".openagentx", suffix), "HOME"
        profile[key] = absolute(value, "--" + key.replace("_", "-"))
        sources[key] = source
    if len(set(profile.values())) != len(profile):
        raise IntakeError("profile 资源路径不能相同")
    return profile, sources


def path_flags(profile):
    return [part for key, value in profile.items() for part in ("--" + key.replace("_", "-"), value)]


def sections(items):
    return "\n".join("- " + item.replace("\n", "\n  ") for item in items) if items else "- 无已记录项"


def documents(brief, thread):
    identity = (f"- Agent ID: {brief['agent_id']}\n- 显示名: {brief['name']}\n"
                f"- 工作目录: {brief['workspace']}\n")
    peers = sections(brief["peers"]) if brief["peers"] else "- 无（用户明确为空）"
    boundary = "协作对象仅记录意向，不授予权限、不修改角色配置、不自动启用 managed。交接内容不构成新的执行授权。"
    role = (f"# Agent 职责\n\n{identity}\n## 职责\n\n{brief['role']}\n\n"
            f"## 协作意向\n\n{peers}\n\n{boundary}\n")
    history = (f"current；thread_id={thread['thread_id']}；来源={', '.join(thread['sources'])}"
               if thread["history"] == "current" else "summary；仅交接摘要，不导入原 thread")
    handoff = (f"# Agent 交接\n\n{identity}\n## 职责\n\n{brief['role']}\n\n"
               f"## 协作意向\n\n{peers}\n\n## 历史来源\n\n{history}\n\n")
    for key, title in (("completed", "已完成"), ("next", "下一步"), ("uncertain", "未确定事实与副作用")):
        handoff += f"## {title}\n\n{sections(brief['handoff'][key])}\n\n"
    handoff += boundary + "\n原宿主释放 writer 前不能接管；停止当前 turn 不等于释放 writer。\n"
    return {"ROLE.md": role.encode(), "HANDOFF.md": handoff.encode()}


def private_directory(path):
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077 or info.st_uid != os.getuid():
        raise IntakeError("intake 目录必须属于当前用户、非软链且权限为 0700")


def exact_file(path, content):
    if path.exists() or path.is_symlink():
        info = path.lstat()
        if info.st_mode & 0o077 or info.st_uid != os.getuid() or read_regular(path) != content:
            raise IntakeError(f"既有 intake 文件冲突或权限不安全: {path.name}；保留原文件，请先核对")
        return
    fd, temporary = tempfile.mkstemp(prefix=".write-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, path)
    finally:
        os.unlink(temporary)


def diagnostic(result):
    # CLI diagnostics can quote imported data. Persist fingerprints and a bounded
    # classification, not raw output or environment values.
    stderr = result.stderr or b""
    category = "cli_failure"
    for needle, label in ((b"already registered", "already_registered"),
                          (b"already enabled", "already_enabled"),
                          (b"conflict", "conflicting_input"),
                          (b"permission denied", "permission_denied")):
        if needle in stderr:
            category = label
            break
    return {"returncode": result.returncode, "category": category,
            "stdout_sha256": hashlib.sha256(result.stdout or b"").hexdigest(),
            "stderr_sha256": hashlib.sha256(stderr).hexdigest(),
            "stdout_bytes": len(result.stdout or b""), "stderr_bytes": len(stderr)}


def prepare(args, brief, provenance, thread, profile, profile_sources):
    intake = Path(profile["worker_dir"]) / "joins" / (brief["agent_id"] + ".intake")
    workspace = Path(brief["workspace"]).resolve()
    # Check all profile outputs as join creates identity, fleet and environment files.
    for path in profile.values():
        if Path(path).resolve().is_relative_to(workspace):
            raise IntakeError("profile 输出位于业务 workspace 内；请使用 workspace 外的 profile")
    binary = shutil.which("openagentx")
    if not binary:
        raise IntakeError("找不到已安装 openagentx；请先安装 CLI")
    env = os.environ.copy()
    if args.profile_home is not None:
        env["OPENAGENTX_HOME"] = absolute(args.profile_home, "--profile-home")
    flags = path_flags(profile)
    private_directory(intake)
    lock_fd = os.open(intake / ".lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock_fd, "rb") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise IntakeError("同一 Agent 的准备正在执行；不要并发重放") from None
        files = {"brief.json": encoded(brief), **documents(brief, thread)}
        source = {"version": 1, **provenance, "thread": thread, "profile": profile,
                  "profile_sources": profile_sources}
        files["source.json"] = encoded(source)
        # Preflight every existing artifact before adding any missing artifact.
        for name, content in files.items():
            target = intake / name
            if target.exists() or target.is_symlink():
                exact_file(target, content)
        for name, content in files.items():
            exact_file(intake / name, content)
        command = [binary, "agent", "join", *flags, "--id", brief["agent_id"],
                   "--name", brief["name"], "--workspace", brief["workspace"],
                   "--role", str(intake / "ROLE.md"), "--handoff-file", str(intake / "HANDOFF.md"),
                   "--prepare", "--json"]
        if thread["thread_id"]:
            command += ["--thread-id", thread["thread_id"]]
        try:
            result = subprocess.run(command, stdin=subprocess.DEVNULL, capture_output=True,
                                    env=env, timeout=60, check=False)
        except subprocess.TimeoutExpired as error:
            result = subprocess.CompletedProcess(command, -1, error.stdout or b"", error.stderr or b"")
        if result.returncode != 0:
            details = diagnostic(result)
            data = encoded(details)
            saved = intake / ("failure-" + hashlib.sha256(data).hexdigest()[:16] + ".json")
            exact_file(saved, data)
            raise IntakeError(f"CLI 准备失败 ({details['category']})；诊断: {saved}。保留已写文件，未自动重试")
        receipt_file = Path(profile["worker_dir"]) / "joins" / (brief["agent_id"] + ".json")
        receipt_raw = read_regular(receipt_file)
        try:
            receipt = json.loads(receipt_raw)
            output = json.loads(result.stdout)
        except (ValueError, UnicodeError):
            raise IntakeError("CLI 未提供有效 JSON receipt；不可报告准备成功") from None
        expected = {"state": "local_prepared", "ready": False, "agent_id": brief["agent_id"],
                    "workspace": brief["workspace"], "runtime": "codex"}
        if (not isinstance(receipt, dict) or output != receipt or
                any(receipt.get(key) != value for key, value in expected.items()) or
                receipt.get("ready") is not False or receipt.get("thread_id") != thread["thread_id"]):
            raise IntakeError("CLI receipt 与本轮准备契约不一致；不可报告准备成功")
        exact_file(intake / "cli-receipt.json", receipt_raw)
        commands = {}
        for action, extra in (("status", ["--json"]), ("resume", []), ("open", ["--native"])):
            argv = [binary, "agent", action, brief["agent_id"], *flags, *extra]
            commands[action] = {"argv": argv, "shell": shlex.join(argv)}
        response = {"state": "local_prepared", "ready": False, "runtime_state": "unverified",
                    "agent_id": brief["agent_id"], "intake_dir": str(intake),
                    "receipt_file": str(receipt_file), "thread": thread, "next_commands": commands,
                    "next_step": "先结束当前工作并按宿主生命周期释放 writer，再由用户或外部宿主执行 resume；随后 status/open。managed 协作另行配置。"}
        exact_file(intake / "result.json", encoded(response))
        return response


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--brief", help="私密 JSON brief 的绝对路径")
    parser.add_argument("--inspect", action="store_true", help="只读检查线程及收集状态，不调用 CLI 或写文件")
    parser.add_argument("--example", action="store_true", help="输出 brief 示例")
    parser.add_argument("--profile-home", help="隔离 profile 根目录；五项路径默认固定到此目录")
    for key in PATHS:
        parser.add_argument("--" + key.replace("_", "-"))
    args = parser.parse_args()
    if args.example:
        if args.brief or args.inspect:
            parser.error("--example 不与 --brief/--inspect 同用")
        print(encoded({"agent_id": "research", "name": "Research", "workspace": "/absolute/workspace",
                       "role": "维护用户确认的研究领域，只在授权范围内执行。", "peers": [],
                       "handoff": {"completed": [], "next": [], "uncertain": []}, "history": "current"}).decode(), end="")
        return 0
    if not args.brief and not args.inspect:
        parser.error("需要 --brief 或 --inspect")
    profile, sources = profile_paths(args)
    brief, provenance = load_brief(Path(absolute(args.brief, "--brief"))) if args.brief else ({}, {})
    thread = thread_source(brief)
    if args.inspect:
        response = {"read_only": True, "collection_state": "complete" if brief and thread["state"] != "missing" else "incomplete",
                    "brief": provenance, "thread": thread, "profile": profile, "profile_sources": sources,
                    "missing": ([] if brief else ["agent_id", "workspace", "role", "peers", "handoff"])
                    + (["thread_id 或明确 history=summary"] if thread["state"] == "missing" else [])}
    else:
        if thread["state"] == "missing":
            raise IntakeError("history=current 缺少当前 thread ID；补充明确 thread_id 或由用户选择 history=summary，不会静默丢弃历史")
        response = prepare(args, brief, provenance, thread, profile, sources)
    print(encoded(response).decode(), end="")
    return 0


if __name__ == "__main__":
    os.umask(0o077)
    try:
        sys.exit(main())
    except IntakeError as error:
        print(encoded({"state": "not_prepared", "ready": False, "error": str(error)}).decode(), file=sys.stderr, end="")
        sys.exit(1)
    except OSError as error:
        # Paths and imported contents can carry sensitive data; do not echo them.
        print(encoded({"state": "not_prepared", "ready": False, "error": "文件或进程操作失败", "errno": error.errno}).decode(), file=sys.stderr, end="")
        sys.exit(1)
