#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(cd "${script_dir}/.." && pwd)
cd "${repo_dir}"

mode=${1:---inventory}
case "${mode}" in
    --inventory|--release)
        ;;
    --help|-h)
        printf 'Usage: %s [--inventory|--release]\n' "$0"
        exit 0
        ;;
    *)
        printf 'unknown mode: %s\n' "${mode}" >&2
        exit 2
        ;;
esac

release_failures=0

print_matches() {
    local label=$1
    local pattern=$2
    shift 2

    local existing=()
    local scan_path
    for scan_path in "$@"; do
        if [[ -e "${scan_path}" ]]; then
            existing+=("${scan_path}")
        fi
    done

    if [[ ${#existing[@]} -eq 0 ]]; then
        printf 'CLEAN %s (no paths)\n' "${label}"
        return
    fi

    local matches
    matches=$(rg -n -S \
        --glob '*.go' \
        --glob '*.mod' \
        --glob '*.yaml' \
        --glob '*.yml' \
        --glob '*.json' \
        --glob '*.md' \
        --glob '*.sh' \
        --glob '*.service' \
        -- "${pattern}" "${existing[@]}" 2>/dev/null || true)

	# ADR-005 requires documenting and negatively testing forbidden tmux
	# control commands. Those literals are evidence, not executable paths.
	if [[ "${label}" == "tmux_control" && -n "${matches}" ]]; then
		matches=$(printf '%s\n' "${matches}" | rg -v '(^|/)[^:]*_test\.go:|\.md:' || true)
	fi

    if [[ -z "${matches}" ]]; then
        printf 'CLEAN %s\n' "${label}"
        return
    fi

    local match_count
    match_count=$(printf '%s\n' "${matches}" | wc -l | tr -d '[:space:]')
    printf 'FOUND %s count=%s\n' "${label}" "${match_count}"
    printf '%s\n' "${matches}" | sed -n '1,12p'
    if [[ ${match_count} -gt 12 ]]; then
        printf '... %s additional matches omitted\n' "$((match_count - 12))"
    fi

    if [[ "${mode}" == "--release" ]]; then
        release_failures=$((release_failures + 1))
    fi
}

check_forbidden_path() {
    local legacy_path=$1
    if [[ ! -e "${legacy_path}" ]]; then
        printf 'CLEAN path %s\n' "${legacy_path}"
        return
    fi

    printf 'FOUND path %s\n' "${legacy_path}"
    if [[ "${mode}" == "--release" ]]; then
        release_failures=$((release_failures + 1))
    fi
}

runtime_paths=(go.mod cmd internal agents integrations)
current_docs=(README.md docs/ARCHITECTURE.md docs/runtime)

check_forbidden_path cmd/agentbus
check_forbidden_path internal/connector/tmux.go
check_forbidden_path integrations/agy/hooks.json.example

print_matches \
    agentbus_runtime_naming \
    '(^|[^[:alnum:]_])(agentbus|AgentBus)([^[:alnum:]_]|$)|AGENTBUS_' \
    "${runtime_paths[@]}"

print_matches \
    agentbus_current_docs \
    '(^|[^[:alnum:]_])(agentbus|AgentBus)([^[:alnum:]_]|$)|AGENTBUS_' \
    "${current_docs[@]}"

print_matches \
    tmux_control \
    'TmuxConnector|NewTmuxConnector|ProbePane|NotifyBootstrap|NotifyTask|paste-buffer|send-keys|capture-pane|load-buffer' \
    "${runtime_paths[@]}" "${current_docs[@]}"

print_matches \
    pane_manifest \
    '(^|[[:space:]])(connector|address):[[:space:]]*(tmux|"?%|auto)' \
    agents

print_matches \
    pane_lifecycle \
    'AttachAgent|BootstrapAgent|ReadySession|agent (attach|bootstrap|launch)|session ready' \
    cmd internal README.md docs/ARCHITECTURE.md docs/runtime

print_matches \
    agy_stop_hook \
    'agy-hook|Stop Gate|StopGate|AGYEventStop' \
    cmd internal integrations README.md docs/ARCHITECTURE.md docs/runtime

print_matches \
    legacy_schema \
    'uq_tasks_target_active|task_events|resolved_pane_id|delivery_error' \
    internal

print_matches \
    legacy_runtime_paths \
    'agentbus\.sock|agentbus\.db|cmd/agentbus|bin/agentbus' \
    "${runtime_paths[@]}" "${current_docs[@]}"

if [[ "${mode}" == "--release" && ${release_failures} -ne 0 ]]; then
    printf 'Legacy control-path release check failed: %s category/path failures.\n' "${release_failures}" >&2
    exit 1
fi

if [[ "${mode}" == "--inventory" ]]; then
    printf 'Inventory complete. Matches are expected until Task 18.\n'
else
    printf 'Legacy control-path release check passed.\n'
fi
