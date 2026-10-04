#!/usr/bin/env python3
"""Real Codex Skill -> installed OAX prepare; no activation or business messages."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time


def save(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def workspace_files(workspace):
    return {str(f.relative_to(workspace)): hashlib.sha256(f.read_bytes()).hexdigest()
            for f in workspace.rglob("*")
            if f.is_file() and ".git" not in f.relative_to(workspace).parts}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True, help="New absolute evidence directory")
    parser.add_argument("--sandbox", choices=["workspace-write", "danger-full-access"],
                        default="workspace-write", help="Use only a mode authorized for this test host")
    args = parser.parse_args()
    root = args.root
    if not root.is_absolute() or root.exists():
        parser.error("--root must be a new absolute directory")
    os.umask(0o077)
    root.mkdir(parents=True)
    workspace, profile = root / "workspace", root / "profile"
    workspace.mkdir()
    (workspace / "README.md").write_text("OAX Skill 接入验证工程。只用于准备，不执行业务任务。\n")
    (workspace / "AGENTS.md").write_text(
        "本次只验证 Skill 的离线准备。此 workspace 保持只读。\n"
        f"资料和 OAX profile 只能写在 {root} 下。\n"
        "禁止 resume/open、启动 Worker、向其他 Agent 发消息或修改默认 OAX profile。\n")
    subprocess.run(["git", "init", "-q", str(workspace)], check=True)
    initial = workspace_files(workspace)
    save(root / "initial-workspace.json", initial)
    prompt = (
        "使用 $openagentx-join 完成一次真实的离线接入准备。先读取该 Skill。\n"
        "稳定 Agent ID 是 join-skill-model-e2e，显示名是接入验证领域。\n"
        f"工作目录是 {workspace}。职责：只负责该验证工程资料整理，不负责支付、部署或其他业务领域。\n"
        "协作对象明确为 pay-domain，只记录将来咨询接口的意向，本轮不发信。\n"
        "交接记录：已完成阅读本工程 README；下一步等待用户正式启用；未知事项为尚未配置协作权限。\n"
        "保留本次 Codex 当前 thread，请由 helper 自动捕获，不在 brief 中手填 thread_id。\n"
        f"本轮唯一允许的 profile 是 {profile}。必须给 prepare helper 传 --profile-home 指向它。\n"
        f"私密 brief 放在 {root / 'brief.json'}；本工作目录只读。\n"
        "只做 prepare，不能 resume/open 或创建任何业务任务。完成后报告 prepared、ready 与 thread 来源。"
    )
    (root / "prompt.txt").write_text(prompt)
    env = os.environ.copy()
    # A newly launched CLI must supply its own tool thread IDs, never inherit the
    # current runner's session identifiers. Neither HOME nor CODEX_HOME is changed.
    inherited_thread = env.pop("CODEX_THREAD_ID", None)
    env.pop("CODEX_SESSION_ID", None)
    command = ["codex", "exec", "--json", "-m", "gpt-6-astra",
               "-c", 'model_reasoning_effort="high"', "-c", 'approval_policy="never"',
               "--sandbox", args.sandbox, "--cd", str(workspace),
               "--add-dir", str(root), "--output-last-message", str(root / "final.txt"), "-"]
    save(root / "invocation.json", {"argv": command, "stdin": "prompt.txt",
         "started": time.time(), "parent_thread_id": inherited_thread,
         "removed_inherited_env": ["CODEX_THREAD_ID", "CODEX_SESSION_ID"],
         "codex_version": subprocess.check_output(["codex", "--version"], text=True).strip()})
    outcome = {"status": "FAILED", "scope": "real-model skill preparation; no managed activation"}
    with (root / "codex.jsonl").open("w") as stdout, (root / "codex.stderr.log").open("w") as stderr:
        process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=stdout,
                                   stderr=stderr, env=env, start_new_session=True, text=True)
        try:
            process.communicate(prompt, timeout=600)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
            outcome["error"] = "model_timeout"
        outcome["exit_code"] = process.returncode
    try:
        events = [json.loads(line) for line in (root / "codex.jsonl").read_text().splitlines()]
        threads = [e["thread_id"] for e in events if e.get("type") == "thread.started"]
        assert len(threads) == 1 and threads[0] != inherited_thread, "new thread not established"
        intake = profile / "workers/joins/join-skill-model-e2e.intake"
        receipt = json.loads((intake / "cli-receipt.json").read_text())
        result = json.loads((intake / "result.json").read_text())
        brief = json.loads((intake / "brief.json").read_text())
        assert process.returncode == 0 and any(e.get("type") == "turn.completed" for e in events)
        assert receipt["state"] == "local_prepared" and receipt["ready"] is False
        assert receipt["thread_id"] == threads[0] == result["thread"]["thread_id"]
        assert "thread_id" not in brief, "model bypassed auto-capture"
        assert "CODEX_THREAD_ID" in result["thread"]["sources"]
        assert brief["peers"] == ["pay-domain"]
        assert brief["workspace"] == str(workspace)
        assert not (profile / "data/openagentx.db").exists()
        assert not (profile / "run/openagentx.sock").exists()
        assert "enabled: false" in (profile / "fleet.yaml").read_text()
        assert initial == workspace_files(workspace)
        status_cmd = result["next_commands"]["status"]["argv"]
        status = subprocess.run(status_cmd, capture_output=True, text=True, timeout=30)
        save(root / "prepared-status.json", {"argv": status_cmd, "exit_code": status.returncode,
                                            "stdout": status.stdout, "stderr": status.stderr})
        assert status.returncode == 0
        status_data = json.loads(status.stdout)
        assert status_data["state"] == "local_prepared" and status_data["ready"] is False
        outcome.update(status="PASS", thread_id=threads[0], auto_thread_matches=True,
                       workspace_unchanged=True, no_daemon_artifacts=True,
                       fleet_enabled=False, profile_status_verified=True,
                       intake_dir=str(intake))
    except (AssertionError, KeyError, ValueError, OSError) as exc:
        outcome["error"] = str(exc)
    save(root / "verdict.json", outcome)
    print(json.dumps(outcome, ensure_ascii=False, indent=2))
    return 0 if outcome["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
