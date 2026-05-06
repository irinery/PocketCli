#!/usr/bin/env bash

set -euo pipefail

pocket_skill_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

pocket_skill_code_dir() {
    printf '%s\n' "${POCKETCLI_DIR:-${HOME}/.pocketcli}"
}

pocket_skill_ansible_dir() {
    printf '%s\n' "${POCKETCLI_ANSIBLE_DIR:-$(pocket_skill_code_dir)/ansible}"
}

pocket_skill_playbook_dir() {
    printf '%s/playbooks\n' "$(pocket_skill_ansible_dir)"
}

pocket_skill_allowed_file() {
    printf '%s/allowed_skills.txt\n' "$(pocket_skill_ansible_dir)"
}

pocket_skill_inventory_file() {
    printf '%s/inventory.ini\n' "$(pocket_skill_ansible_dir)"
}

pocket_skill_log_file() {
    printf '%s\n' "${POCKETCLI_SKILL_LOG_FILE:-$(pocket_skill_code_dir)/logs/skills.jsonl}"
}

pocket_skill_lock_dir() {
    printf '%s\n' "${POCKETCLI_SKILL_LOCK_DIR:-/tmp/pocketcli_locks}"
}

pocket_skill_require_python() {
    if ! command -v python3 >/dev/null 2>&1; then
        printf 'missing_dependency:python3\n' >&2
        return 1
    fi
}

pocket_skill_json_get() {
    local file expr
    file=$1
    expr=$2
    python3 - "$file" "$expr" <<'PY'
import json
import sys

path, expr = sys.argv[1], sys.argv[2]
try:
    with open(path, "r", encoding="utf-8") as handle:
        data = json.load(handle)
except Exception:
    raise SystemExit(1)

current = data
for part in expr.split("."):
    if not part:
        continue
    if isinstance(current, dict):
        current = current.get(part)
    else:
        current = None
        break

if current is None:
    print("")
elif isinstance(current, bool):
    print("true" if current else "false")
else:
    print(str(current))
PY
}

pocket_skill_json_escape() {
    python3 -c 'import json,sys; print(json.dumps(sys.stdin.read(), ensure_ascii=False))'
}
