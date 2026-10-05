#!/usr/bin/env python3
"""Read-only evidence for the approved test-Agent cleanup; contains no credentials."""
import argparse
import json
import os
from pathlib import Path
import sqlite3

import terminal_overview_snapshot as runtime


CANDIDATES = [
    "agy-onboarding-e2e", "codex-domain-e2e",
    "domain-installed-a-8304eb9e5aa7", "domain-installed-b-8304eb9e5aa7",
    "domain-installed-a-9730f99e2b42", "domain-installed-b-9730f99e2b42",
    "external-installed-a-51e44952c2", "external-installed-b-51e44952c2",
    "test-fake-agent", "test-fake-multiturn", "verification-runner",
]
RETAINED = runtime.AGENTS + ["orchestrator"]


def capture(profile, go_bin):
    data = runtime.capture(profile, go_bin)
    conn = sqlite3.connect((profile / "data/openagentx.db").as_uri() + "?mode=ro", uri=True)
    try:
        conn.execute("PRAGMA query_only=ON")
        data["schema_version"] = conn.execute("SELECT version FROM schema_meta").fetchone()[0]
        data["foreign_key_violations"] = list(conn.execute("PRAGMA foreign_key_check"))
        data["agent_ids"] = [r[0] for r in conn.execute("SELECT agent_id FROM agents ORDER BY agent_id")]
        data["agent_history"] = {}
        for agent in RETAINED + CANDIDATES:
            entry = {}
            for label, table, key, owner in [
                ("tasks", "tasks", "task_id", "target_agent_id"),
                ("runs", "run_attempts", "run_id", "agent_id"),
                ("bindings", "session_bindings", "session_binding_id", "agent_id"),
                ("external_bindings", "external_session_bindings", "binding_id", "agent_id"),
                ("mailbox", "mailbox_items", "mailbox_item_id", "target_agent_id"),
                ("workers", "worker_instances", "worker_instance_id", "agent_id"),
            ]:
                entry[label] = [r[0] for r in conn.execute(
                    f"SELECT {key} FROM {table} WHERE {owner}=? ORDER BY {key}", (agent,))]
            entry["external_messages"] = [r[0] for r in conn.execute(
                "SELECT message_id FROM external_messages WHERE sender_agent_id=? OR target_agent_id=? ORDER BY message_id",
                (agent, agent))]
            entry["task_states"] = dict(conn.execute(
                "SELECT status,count(*) FROM tasks WHERE target_agent_id=? GROUP BY status", (agent,)))
            data["agent_history"][agent] = entry
    finally:
        conn.close()
    # Compare only metadata, never copy Worker environment or token-bearing files.
    import yaml
    manifest = yaml.safe_load((profile / "fleet.yaml").read_text())
    data["fleet_entries"] = {
        a["agent_id"]: a for a in manifest.get("agents", [])
    }
    return data


def compare(before, after):
    def latest_workers(data):
        latest = {}
        for worker in data["workers"]:
            agent = worker["agent_id"]
            if agent not in latest or worker["generation"] > latest[agent]["generation"]:
                latest[agent] = worker
        return list(latest.values())
    # Overview is capped at 100 historical Worker rows. Removing fixtures may
    # expose older retained rows; compare the current generation, not pagination.
    before = dict(before, workers=latest_workers(before))
    after = dict(after, workers=latest_workers(after))
    # Runtime's original comparator requires byte-identical Fleet. Removal changes
    # exactly the selected entries, so verify retained entries separately here.
    adapted = dict(after, fleet_sha256=before["fleet_sha256"])
    checks = runtime.compare(before, adapted)
    checks.pop("fleet_unchanged", None)
    checks["retained_fleet_unchanged"] = all(
        before["fleet_entries"].get(a) == after["fleet_entries"].get(a) for a in RETAINED)
    checks["retained_history_preserved"] = all(
        set(before["agent_history"][a][kind]).issubset(after["agent_history"][a][kind])
        for a in RETAINED for kind in ("tasks", "runs", "bindings", "external_bindings", "mailbox", "workers", "external_messages"))
    checks["candidates_absent"] = not (set(CANDIDATES) & set(after["agent_ids"]))
    checks["candidate_histories_empty"] = all(
        not records for a in CANDIDATES for records in after["agent_history"][a].values())
    checks["candidate_fleet_absent"] = not (set(CANDIDATES) & set(after["fleet_entries"]))
    checks["foreign_keys_valid"] = not after["foreign_key_violations"]
    checks["pass"] = all(v for k, v in checks.items() if k != "agents") and all(
        all(v.values()) for v in checks["agents"].values())
    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", type=Path, default=Path.home() / ".openagentx")
    parser.add_argument("--go-bin", default="go")
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--compare", type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    data = capture(args.profile.resolve(), args.go_bin)
    if args.compare:
        data["cleanup_verification"] = compare(json.loads(args.compare.read_text()), data)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    with args.out.open("x") as output:
        json.dump(data, output, ensure_ascii=False, indent=2)
        output.write("\n")
    print(json.dumps({"out": str(args.out), "agents": data["agent_ids"],
                      "verification": data.get("cleanup_verification")}, ensure_ascii=False))
    return 0 if data.get("cleanup_verification", {}).get("pass", True) else 1


if __name__ == "__main__":
    raise SystemExit(main())
