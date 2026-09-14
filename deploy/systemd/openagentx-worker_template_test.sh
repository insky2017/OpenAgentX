#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
UNIT_FILE="$SCRIPT_DIR/openagentx-worker@.service"
USER_UNIT_FILE="$SCRIPT_DIR/openagentx-worker-user@.service"

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

grep -Fq 'EnvironmentFile=-%h/.openagentx/workers/%i.env' "$USER_UNIT_FILE"
grep -Fq 'ExecStart=%h/.local/bin/openagentx worker run --config %h/.openagentx/workers/%i.yaml' "$USER_UNIT_FILE"
grep -Fq 'WorkingDirectory=%h' "$USER_UNIT_FILE"
grep -Fq 'Wants=openagentx.service' "$USER_UNIT_FILE"
if grep -Eq '^(Requires|BindsTo|PartOf)=openagentx.service$' "$USER_UNIT_FILE"; then
  echo 'user worker template must not stop resident Workers with the daemon unit' >&2
  exit 1
fi
grep -Fq 'ProtectHome=false' "$USER_UNIT_FILE"
grep -Fq 'ProtectSystem=false' "$USER_UNIT_FILE"
if grep -Eq '(--username|password|token)' "$USER_UNIT_FILE"; then
  echo 'user worker template must not contain credentials' >&2
  exit 1
fi
if grep -Fq '/home/sky' "$USER_UNIT_FILE"; then
  echo 'user worker template must not depend on /home/sky' >&2
  exit 1
fi

echo "worker template static assertions passed: $UNIT_FILE and $USER_UNIT_FILE"
