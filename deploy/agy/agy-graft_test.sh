#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
WRAPPER="${1:-$SCRIPT_DIR/agy-graft}"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf -- "$TEST_DIR"' EXIT

REAL_AGY="$TEST_DIR/agy"
MGRAFTCP="$TEST_DIR/mgraftcp"
ARGV_LOG="$TEST_DIR/argv.log"
REAL_ENV_LOG="$TEST_DIR/real-env.log"
CONFIG_FILE="$TEST_DIR/agy-graft.conf"
MISSING_ENDPOINT_CONFIG="$TEST_DIR/missing-endpoint.conf"
BLACKIP_FILE="$TEST_DIR/direct.blackip"
WHITEIP_FILE="$TEST_DIR/proxy.whiteip"

printf '#!/usr/bin/env bash\nenv | grep "^AGY_GRAFT_" >"$AGY_TEST_REAL_ENV_LOG" || true\nexit 0\n' >"$REAL_AGY"
printf '#!/usr/bin/env bash\nprintf "%%s\\0" "$@" >"$AGY_TEST_ARGV_LOG"\nreal="${@: -2:1}"\n"$real" "${@: -1}"\n' >"$MGRAFTCP"
chmod 0700 "$REAL_AGY" "$MGRAFTCP"

printf '%s\n' \
  'select_proxy_mode = only_socks5' \
  'socks5 = config-socks.example.invalid:28080' \
  'http_proxy = config-http.example.invalid:28081' \
  'socks5_username = fixture-user' \
  'socks5_password = fixture-password' >"$CONFIG_FILE"
chmod 0600 "$CONFIG_FILE"

printf '%s\n' \
  'select_proxy_mode = only_socks5' \
  'socks5_username = fixture-user' \
  'socks5_password = fixture-password' >"$MISSING_ENDPOINT_CONFIG"
chmod 0600 "$MISSING_ENDPOINT_CONFIG"
printf '%s\n' '203.0.113.8' >"$BLACKIP_FILE"
printf '%s\n' '198.51.100.7' >"$WHITEIP_FILE"
chmod 0600 "$BLACKIP_FILE" "$WHITEIP_FILE"

run_wrapper() {
  env -i \
    PATH=/usr/bin:/bin \
    AGY_TEST_ARGV_LOG="$ARGV_LOG" \
    AGY_TEST_REAL_ENV_LOG="$REAL_ENV_LOG" \
    AGY_GRAFT_REAL_BIN="$REAL_AGY" \
    AGY_GRAFT_MGRAFTCP_BIN="$MGRAFTCP" \
    AGY_GRAFT_IPV4_ONLY=0 \
    "$@"
}

assert_argv() {
  local -a expected=("$@")
  local -a actual=()
  mapfile -d '' -t actual <"$ARGV_LOG"
  if [[ "${#actual[@]}" != "${#expected[@]}" ]]; then
    printf 'argv length mismatch: got=%q want=%q\n' "${actual[*]}" "${expected[*]}" >&2
    exit 1
  fi
  local index
  for index in "${!expected[@]}"; do
    if [[ "${actual[index]}" != "${expected[index]}" ]]; then
      printf 'argv mismatch at %s: got=%q want=%q\n' "$index" "${actual[*]}" "${expected[*]}" >&2
      exit 1
    fi
  done
}

expect_rc2() {
  local output="$TEST_DIR/error.log"
  set +e
  run_wrapper "$@" >"$output" 2>&1
  local rc=$?
  set -e
  if [[ "$rc" != "2" ]]; then
    echo "expected rc=2, got rc=$rc for: $*" >&2
    sed -n '1,20p' "$output" >&2
    exit 1
  fi
}

run_case() {
  local name="$1"
  shift
  : >"$ARGV_LOG"
  run_wrapper "$@" "$WRAPPER" --version
  if [[ -s "$REAL_ENV_LOG" ]]; then
    echo "wrapper control variables leaked to real AGY in case: $name" >&2
    cat "$REAL_ENV_LOG" >&2
    exit 1
  fi
  echo "passed: $name"
}

# Mode and endpoint are resolved independently. Standard env wins when its
# scheme matches the selected mode; incompatible or mode-mismatched env falls
# through to the matching config endpoint.
run_case 'explicit socks5 + compatible ALL_PROXY => environment' \
  AGY_GRAFT_SELECT_PROXY_MODE=only_socks5 \
  AGY_GRAFT_CONFIG="$CONFIG_FILE" \
  ALL_PROXY=socks5h://env-socks.example.invalid:28080
assert_argv --socks5 env-socks.example.invalid:28080 --select_proxy_mode only_socks5 "$REAL_AGY" --version

run_case 'explicit socks5 + only HTTP env => config' \
  AGY_GRAFT_SELECT_PROXY_MODE=only_socks5 \
  AGY_GRAFT_CONFIG="$CONFIG_FILE" \
  HTTP_PROXY=http://env-http.example.invalid:28080
assert_argv --select_proxy_mode only_socks5 --config "$CONFIG_FILE" "$REAL_AGY" --version

run_case 'explicit socks5 + AGY HTTP endpoint => config' \
  AGY_GRAFT_SELECT_PROXY_MODE=only_socks5 \
  AGY_GRAFT_CONFIG="$CONFIG_FILE" \
  AGY_GRAFT_HTTP_PROXY=http://agy-http.example.invalid:28080
assert_argv --select_proxy_mode only_socks5 --config "$CONFIG_FILE" "$REAL_AGY" --version

run_case 'inferred socks5 + compatible ALL_PROXY => environment' \
  AGY_GRAFT_CONFIG="$CONFIG_FILE" \
  ALL_PROXY=socks5h://env-socks.example.invalid:28080
assert_argv --socks5 env-socks.example.invalid:28080 --select_proxy_mode only_socks5 "$REAL_AGY" --version

run_case 'no env + config => config' AGY_GRAFT_CONFIG="$CONFIG_FILE"
assert_argv --select_proxy_mode only_socks5 --config "$CONFIG_FILE" "$REAL_AGY" --version

run_case 'blacklist precedes IPv4 whitelist' \
  AGY_GRAFT_CONFIG="$CONFIG_FILE" \
  AGY_GRAFT_BLACKIP_FILE="$BLACKIP_FILE" \
  AGY_GRAFT_IPV4_ONLY=1 \
  AGY_GRAFT_IPV4_ONLY_FILE="$WHITEIP_FILE"
assert_argv --blackip-file "$BLACKIP_FILE" --whiteip-file "$WHITEIP_FILE" --select_proxy_mode only_socks5 --config "$CONFIG_FILE" "$REAL_AGY" --version

# The default path is accepted only with a controlled local listener.
python3 -c 'import socket, time; s=socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind(("127.0.0.1", 7897)); s.listen(); time.sleep(3)' 2>/dev/null &
candidate_listener_pid=$!
listener_pid=0
sleep 0.1
if kill -0 "$candidate_listener_pid" 2>/dev/null; then
  listener_pid="$candidate_listener_pid"
elif ! ss -ltn 'sport = :7897' | grep -q ':7897'; then
  echo 'default listener failed to start' >&2
  exit 1
fi
run_case 'no env/config + local default => default'
assert_argv --http_proxy 127.0.0.1:7897 --select_proxy_mode only_http_proxy "$REAL_AGY" --version
if [[ "$listener_pid" != "0" ]]; then
  kill "$listener_pid" 2>/dev/null || true
  wait "$listener_pid" 2>/dev/null || true
fi

expect_rc2 AGY_GRAFT_CONFIG="$MISSING_ENDPOINT_CONFIG" "$WRAPPER" --version
expect_rc2 AGY_GRAFT_SELECT_PROXY_MODE=only_socks5 AGY_GRAFT_CONFIG="$MISSING_ENDPOINT_CONFIG" "$WRAPPER" --version

# Explicit config safety checks remain fail-closed.
chmod 0644 "$CONFIG_FILE"
expect_rc2 AGY_GRAFT_CONFIG="$CONFIG_FILE" "$WRAPPER" --version
chmod 0600 "$CONFIG_FILE"
expect_rc2 AGY_GRAFT_CONFIG="$TEST_DIR" "$WRAPPER" --version
expect_rc2 AGY_GRAFT_CONFIG="$TEST_DIR/missing.conf" "$WRAPPER" --version
ln -s "$CONFIG_FILE" "$TEST_DIR/symlink.conf"
expect_rc2 AGY_GRAFT_CONFIG="$TEST_DIR/symlink.conf" "$WRAPPER" --version
chmod 0644 "$BLACKIP_FILE"
expect_rc2 AGY_GRAFT_CONFIG="$CONFIG_FILE" AGY_GRAFT_BLACKIP_FILE="$BLACKIP_FILE" "$WRAPPER" --version

echo "agy-graft fixture passed: $WRAPPER"
