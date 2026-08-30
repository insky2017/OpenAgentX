#!/usr/bin/env bash
set -euo pipefail

mode=${1:---dry-run}
if [[ "$mode" != "--dry-run" && "$mode" != "--execute" ]]; then
    printf 'Usage: %s [--dry-run|--execute]\n' "$0" >&2
    exit 2
fi

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
release_dir=${OPENAGENTX_RELEASE_DIR:-$root_dir}
old_db=${OPENAGENTX_OLD_DB:-}
archive_dir=${OPENAGENTX_ARCHIVE_DIR:-$root_dir/.cutover-archive}
target_db=${OPENAGENTX_TARGET_DB:-/var/lib/openagentx/openagentx.db}

run() {
    printf '+ %s\n' "$*"
    if [[ "$mode" == "--execute" ]]; then
        "$@"
    fi
}

printf 'OpenAgentX cutover rehearsal (%s)\n' "$mode"
printf 'release=%s target_db=%s archive=%s\n' "$release_dir" "$target_db" "$archive_dir"

[[ -x "$release_dir/bin/openagentx" ]] || { printf 'missing canonical binary: %s\n' "$release_dir/bin/openagentx" >&2; exit 1; }
[[ -z "$old_db" || -f "$old_db" ]] || { printf 'old database does not exist: %s\n' "$old_db" >&2; exit 1; }

run mkdir -p "$archive_dir"
if [[ -n "$old_db" ]]; then
    run cp --reflink=auto --preserve=mode,timestamps "$old_db" "$archive_dir/old-database.sqlite"
    run sha256sum "$archive_dir/old-database.sqlite"
    run chmod 0440 "$archive_dir/old-database.sqlite"
fi

run install -d -m 0700 "$(dirname "$target_db")" /run/openagentx
run "$release_dir/bin/openagentx" schema verify --db "$target_db"
run systemctl stop openagentx-worker@quote-service.service
run systemctl stop openagentx.service
run systemctl start openagentx.service
run systemctl start openagentx-worker@quote-service.service
run systemctl is-active openagentx.service openagentx-worker@quote-service.service
run nginx -t
run systemctl reload nginx

printf 'Rehearsal checks passed. Execute mode is the only mode that mutates services.\n'
