#!/usr/bin/env python3
"""Read-only, content-minimizing observation of exact Codex Desktop threads.

Writes only a fresh --output directory. SQLite is opened mode=ro/query_only;
no app-server, automation, process, or rollout mutation is performed. Evidence
contains structural metadata and explicit marker matches, never full prompts,
tool arguments, outputs, reasoning, or conversation text. Keep raw evidence
separately in a private directory if needed. A missing event is not a failure
or completion verdict. Source hashes describe bounded snapshots, not live files.
"""

import argparse
import collections
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import sys
import uuid


def timestamp(value):
    if not isinstance(value, str):
        return None
    try:
        result = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
        return result if result.tzinfo is not None else None
    except ValueError:
        return None


def identifier(value):
    # IDs are deliberately narrow; arbitrary tool strings are never exported.
    if isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9_.:/-]{1,160}", value):
        return value
    return None


def json_strings(value):
    if isinstance(value, str):
        yield value
    elif isinstance(value, list):
        for item in value:
            yield from json_strings(item)
    elif isinstance(value, dict):
        for item in value.values():
            yield from json_strings(item)


def sha(value):
    return hashlib.sha256(value).hexdigest()


def snapshot_lines(path, metadata):
    with path.open("rb") as source:
        before = os.fstat(source.fileno())
        remaining = before.st_size
        digest = hashlib.sha256()
        line_number = 0
        while remaining:
            raw = source.readline(remaining)
            if not raw:
                break
            remaining -= len(raw)
            digest.update(raw)
            line_number += 1
            yield line_number, raw
        after = os.fstat(source.fileno())
    metadata.update(path=str(path), snapshot_bytes=before.st_size - remaining,
                    snapshot_sha256=digest.hexdigest(), initial_size=before.st_size,
                    final_size=after.st_size, changed_during_read=(
                        before.st_size != after.st_size or before.st_mtime_ns != after.st_mtime_ns),
                    inode=before.st_ino)


def observe_rollout(thread_id, path, since, markers):
    source, events, counts = {}, [], collections.Counter()
    current_turn, identity, invalid = None, None, 0
    oax_calls = {}
    last_lifecycle = None
    for line, raw in snapshot_lines(path, source):
        try:
            record = json.loads(raw)
        except (ValueError, UnicodeDecodeError):
            invalid += 1
            continue
        if not isinstance(record, dict):
            continue
        outer = record.get("type", record.get("event"))
        payload = record.get("payload", {})
        if not isinstance(payload, dict):
            continue
        kind = payload.get("type")
        if outer == "session_meta":
            identity = payload.get("id")
            if identity != thread_id:
                raise ValueError("rollout session_meta identity does not match requested thread")
        turn = payload.get("turn_id", payload.get("turnId"))
        if turn:
            current_turn = identifier(turn)
        when = timestamp(record.get("timestamp"))
        lifecycle = kind if outer == "event_msg" else outer
        if lifecycle in ("task_started", "task_complete", "turn_aborted", "turn_started", "turn_completed"):
            last_lifecycle = {"timestamp": record.get("timestamp"), "event": lifecycle,
                              "turn_id": current_turn, "line": line}
        if when is None or when < since:
            continue
        counts[str(outer) + "/" + str(kind)] += 1
        base = {"line": line, "timestamp": record.get("timestamp"), "turn_id": current_turn,
                "outer_type": outer, "payload_type": kind, "record_sha256": sha(raw)}
        trigger_labels = []
        for key in ("turn_trigger", "turnTrigger", "source", "trigger"):
            value = payload.get(key)
            if isinstance(value, str) and re.fullmatch(r"automation_heartbeat(?:_scheduled|_run_now)?", value):
                trigger_labels.append(value)
        if trigger_labels:
            events.append({**base, "observation": "heartbeat_trigger", "trigger_labels": trigger_labels})
        if lifecycle in ("task_started", "task_complete", "turn_aborted", "turn_started", "turn_completed"):
            events.append({**base, "observation": "turn_lifecycle", "event": lifecycle})
        if outer == "turn_context" or (outer == "event_msg" and kind == "thread_settings_applied"):
            settings = payload.get("thread_settings", payload.get("settings", payload))
            settings = settings if isinstance(settings, dict) else {}
            mode = settings.get("collaboration_mode", settings.get("collaborationMode", {}))
            mode = mode if isinstance(mode, dict) else {}
            mode_settings = mode.get("settings", {})
            mode_settings = mode_settings if isinstance(mode_settings, dict) else {}
            events.append({**base, "observation": "execution_settings",
                           "model": identifier(settings.get("model", mode_settings.get("model"))),
                           "reasoning_effort": identifier(settings.get("effort", settings.get(
                               "reasoning_effort", mode_settings.get("reasoning_effort")))),
                           "mode": identifier(mode.get("mode"))})
        # Inspect only messages / tool records, excluding reasoning and unrelated context.
        item = payload.get("item") if kind == "item_completed" else payload
        if not isinstance(item, dict):
            continue
        item_kind = item.get("type", "")
        # Desktop injects a heartbeat as an automation_update tool result,
        # without a preceding call_id or user message in this turn.
        native_output = item.get("output")
        if (item_kind == "function_call_output" and item.get("name") == "automation_update"
                and item.get("namespace") == "codex_app" and isinstance(native_output, str)
                and "<heartbeat>" in native_output):
            aid = re.search(r"<automation_id>([A-Za-z0-9_.-]{1,160})</automation_id>", native_output)
            trigger_time = re.search(r"<current_time_iso>([^<]{1,50})</current_time_iso>", native_output)
            events.append({**base, "observation": "native_heartbeat_input",
                           "automation_id": aid.group(1) if aid else None,
                           "trigger_time": trigger_time.group(1) if trigger_time and timestamp(trigger_time.group(1)) else None,
                           "matched_markers": [marker for marker in markers if marker in native_output],
                           "content_sha256": sha(native_output.encode())})
        if item_kind == "message" and item.get("role") == "assistant" and item.get("phase") in ("final", "final_answer"):
            final_text = "\n".join(block.get("text", "") for block in item.get("content", [])
                                   if isinstance(block, dict) and isinstance(block.get("text", ""), str))
            events.append({**base, "observation": "final_response_metadata",
                           "matched_markers": [marker for marker in markers if marker in final_text],
                           "content_sha256": sha(final_text.encode()), "content_bytes": len(final_text.encode())})
        is_user = item.get("role") == "user" or item_kind == "user_message"
        is_call = item_kind in ("function_call", "custom_tool_call", "mcpToolCall", "commandExecution")
        is_output = item_kind in ("function_call_output", "custom_tool_call_output")
        if not (is_user or is_call or is_output):
            continue
        body = "\n".join(json_strings(item))
        hits = [marker for marker in markers if marker in body]
        triggers = sorted(set(re.findall(r"automation_heartbeat_(?:scheduled|run_now)", body)))
        if is_user and (hits or "[AUTOMATION " in body or "heartbeat" in body.lower()):
            events.append({**base, "observation": "user_marker", "matched_markers": hits,
                           "heartbeat_word_present": "heartbeat" in body.lower(),
                           "trigger_labels": triggers})
        call_id = identifier(item.get("call_id", item.get("callId", item.get("id"))))
        # A shell/exec wrapper may contain the CLI invocation inside JavaScript/JSON.
        operations = sorted(set(re.findall(
            r"\bexternal\s+(status|inbox|ack|reply|send|bind)\b", body)))
        is_oax = is_call and (bool(operations) or "openagentx" in body.lower() or ".openagentx/external/" in body)
        if is_oax:
            # Mere references (e.g. reading an OAX plan) must not look like
            # successful communication API calls in the evidence counts.
            name = identifier(item.get("name", item.get("tool")))
            direct_tool = bool(name and re.search(r"(?:^|__)openagentx(?:__|$)", name))
            observation = "oax_tool_call" if operations or direct_tool else "oax_reference_tool_call"
            if call_id:
                oax_calls[call_id] = observation
            events.append({**base, "observation": observation, "call_id": call_id,
                           "detection": "command_pattern_or_tool_name" if operations or direct_tool else "reference_only",
                           "tool_name": name,
                           "operations": operations, "matched_markers": hits,
                           "content_sha256": sha(body.encode()), "content_bytes": len(body.encode())})
        elif is_output and call_id in oax_calls:
            # No arbitrary return content: bounded structural cues are sufficient
            # to locate the exact private raw record without leaking message bodies.
            cues = sorted(set(re.findall(r"\b(?:exit_code|exit code|status|isError)\b", body)))
            exit_codes = re.findall(r'(?:"exit_code"\s*:\s*|exit code\s+)(-?\d{1,4})\b', body)
            result_labels = sorted(set(re.findall(
                r'"(?:status|state)"\s*:\s*"(accepted|queued|delivered|acknowledged|replied|pending|completed|failed|error|read)"', body)))
            message_ids = sorted(set(re.findall(
                r'"(?:message_id|reply_to|reply_to_message_id|correlation_id|conversation_id)"\s*:\s*"((?:external-message-)?[0-9a-fA-F]{8}-[0-9a-fA-F-]{27,36})"', body)))
            events.append({**base, "observation": oax_calls[call_id].replace("_call", "_output"), "call_id": call_id,
                           "matched_markers": hits, "structural_cues": cues,
                           "exit_codes": [int(code) for code in exit_codes[:8]],
                           "result_labels": result_labels, "message_ids": message_ids[:16],
                           "content_sha256": sha(body.encode()), "content_bytes": len(body.encode())})
    if identity != thread_id:
        raise ValueError("requested thread identity was not confirmed by session_meta")
    return {"thread_id": thread_id, "source": source, "events": events,
            "event_counts_since": dict(counts), "invalid_json_lines": invalid,
            "last_lifecycle_metadata": last_lifecycle,
            "note": "Lifecycle metadata alone does not establish idle, host ownership, delivery, or business success."}


def observe_log(path, since, selectors):
    source, events = {}, []
    reasons = ("thread_missing", "missing_collaboration_mode", "waiting_on_user_input",
               "waiting_on_approval", "active_with_flags", "active_without_rollout_path",
               "active_without_terminal_event", "active_recent_rollout_activity",
               "cooldown_not_elapsed", "turn_in_progress", "missing_conversation",
               "unsupported_host", "resuming", "missing_turn", "pending_request",
               "already has an active writer", "already has a live local writer")
    for line, raw in snapshot_lines(path, source):
        text = raw.decode("utf-8", "replace")
        hits = [value for value in selectors if value in text]
        if not hits or not re.search(r"heartbeat|automation|active writer|live local writer", text, re.I):
            continue
        match = re.search(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)", text)
        when = timestamp(match.group()) if match else None
        if when is None or when < since:
            continue
        events.append({"line": line, "timestamp": match.group(), "matched_selectors": hits,
                       "reason_labels": [reason for reason in reasons if reason in text],
                       "has_failure_label": bool(re.search(r"failed|blocked|skipped", text, re.I)),
                       "record_sha256": sha(raw)})
    return {"source": source, "events": events}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--thread", action="append", required=True, help="Exact thread UUID; repeatable")
    parser.add_argument("--state-db", type=Path, help="Exact SQLite DB used only to map requested IDs")
    parser.add_argument("--rollout", action="append", default=[], metavar="THREAD_ID=PATH")
    parser.add_argument("--since", required=True, help="ISO 8601 timestamp with timezone")
    parser.add_argument("--marker", action="append", default=[], help="Non-secret literal marker; repeatable")
    parser.add_argument("--automation-id", action="append", default=[])
    parser.add_argument("--log-file", action="append", type=Path, default=[])
    parser.add_argument("--output", required=True, type=Path, help="New, non-existing output directory")
    args = parser.parse_args()
    since = timestamp(args.since)
    if since is None:
        parser.error("--since must include an explicit timezone")
    tids = list(dict.fromkeys(str(uuid.UUID(t)) for t in args.thread))
    paths = dict(entry.split("=", 1) for entry in args.rollout)
    if set(paths) - set(tids):
        parser.error("--rollout includes a thread not selected with --thread")
    if any(not x or len(x) > 160 for x in args.marker + args.automation_id):
        parser.error("markers and automation IDs must be nonempty and at most 160 characters")
    if args.state_db:
        with sqlite3.connect(args.state_db.resolve().as_uri() + "?mode=ro") as conn:
            conn.execute("PRAGMA query_only=ON")
            for tid in tids:
                if tid not in paths:
                    row = conn.execute("SELECT rollout_path FROM threads WHERE id = ?", (tid,)).fetchone()
                    if row:
                        paths[tid] = row[0]
    if set(tids) - set(paths):
        parser.error("each thread requires --rollout or an exact --state-db mapping")
    if args.output.exists():
        parser.error("--output must not exist; use a fresh observation directory")
    result = {"schema_version": 1, "observed_at": dt.datetime.now(dt.timezone.utc).isoformat(),
              "since": since.isoformat(), "read_only_sources": True,
              "privacy": "Content omitted; only explicit markers, structural metadata, hashes and fixed labels exported.",
              "threads": [observe_rollout(t, Path(paths[t]).resolve(), since, args.marker) for t in tids],
              "logs": [observe_log(p.resolve(), since, tids + args.automation_id) for p in args.log_file]}
    args.output.mkdir(parents=True, mode=0o700)
    data = (json.dumps(result, ensure_ascii=False, indent=2) + "\n").encode()
    target = args.output / "observation.json"
    with target.open("xb") as f:
        os.chmod(target, 0o600)
        f.write(data)
    manifest = args.output / "SHA256SUMS"
    with manifest.open("x") as f:
        os.chmod(manifest, 0o600)
        f.write(sha(data) + "  observation.json\n")
    print(json.dumps({"output": str(args.output.resolve()), "threads": len(tids),
                      "selected_events": sum(len(t["events"]) for t in result["threads"]),
                      "log_events": sum(len(t["events"]) for t in result["logs"])}, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, sqlite3.Error) as error:
        print("Observation failed: " + str(error), file=sys.stderr)
        sys.exit(1)
