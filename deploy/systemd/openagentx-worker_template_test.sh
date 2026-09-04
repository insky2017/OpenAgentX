#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
UNIT_FILE="$SCRIPT_DIR/openagentx-worker@.service"

grep -Fq 'EnvironmentFile=-/etc/openagentx/workers/%i.env' "$UNIT_FILE"
grep -Fq 'Environment=PATH=' "$UNIT_FILE"
if grep -Fq 'Environment=AGY_GRAFT_CONFIG=' "$UNIT_FILE"; then
  echo 'worker template must not force a global AGY_GRAFT_CONFIG' >&2
  exit 1
fi
if grep -Fq '/home/sky' "$UNIT_FILE"; then
  echo 'worker template must not depend on /home/sky' >&2
  exit 1
fi

echo "worker template static assertions passed: $UNIT_FILE"
