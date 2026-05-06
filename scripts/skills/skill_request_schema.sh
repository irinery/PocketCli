#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/skills/lib.sh
. "${SCRIPT_DIR}/lib.sh"

validate_skill_request() {
    local request_file audit_file
    request_file=$1
    audit_file=${2:-$(pocket_skill_log_file)}

    pocket_skill_require_python >/dev/null
    python3 - "$request_file" "$audit_file" <<'PY'
import json
import os
import re
import sys

request_path, audit_path = sys.argv[1], sys.argv[2]

UUID_V4 = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
SKILL_NAME = re.compile(r"^[a-z][a-z0-9_]{2,47}$")
HOST = re.compile(r"^[a-zA-Z0-9._-]{1,253}$")
PARAM_KEY = re.compile(r"^[a-z][a-z0-9_]{0,31}$")
TIMESTAMP = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
RISK_LEVELS = {"read", "diagnose", "suggest", "execute", "destructive"}
ACTIVE_RISK_LEVELS = {"read", "diagnose", "suggest", "execute"}
SOURCES = {"pocketwiki", "manual", "test"}
HOST_BLOCKED = [";", "$", "`", " ", "&", "|", ">", "<", "\\"]


def emit(valid, error=None, status="validation_failed", data=None):
    data = data if isinstance(data, dict) else {}
    payload = {
        "valid": bool(valid),
        "status": "valid" if valid else status,
        "error": error,
        "request_id": data.get("request_id"),
        "skill_name": data.get("skill_name"),
        "host": data.get("host"),
        "risk_level": data.get("risk_level"),
    }
    print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
    raise SystemExit(0 if valid else 1)


try:
    with open(request_path, "r", encoding="utf-8") as handle:
        data = json.load(handle)
except Exception:
    emit(False, "invalid_json", data={})

if not isinstance(data, dict):
    emit(False, "invalid_json", data={})

for field in ["request_id", "skill_name", "host", "risk_level", "source", "timestamp"]:
    if field not in data:
        emit(False, f"missing_field:{field}", data=data)

request_id = data.get("request_id")
if not isinstance(request_id, str) or not UUID_V4.match(request_id):
    emit(False, "invalid_format:request_id", data=data)

skill_name = data.get("skill_name")
if not isinstance(skill_name, str) or not SKILL_NAME.match(skill_name):
    emit(False, "invalid_format:skill_name", data=data)

host = data.get("host")
if not isinstance(host, str):
    emit(False, "invalid_format:host", data=data)
if any(marker in host for marker in HOST_BLOCKED):
    emit(False, "injection_attempt:host", status="blocked", data=data)
if not HOST.match(host):
    emit(False, "invalid_format:host", data=data)

risk_level = data.get("risk_level")
if not isinstance(risk_level, str) or risk_level not in RISK_LEVELS:
    emit(False, "invalid_enum:risk_level", data=data)
if risk_level == "destructive":
    emit(False, "blocked_risk_level:destructive", status="blocked", data=data)
if risk_level not in ACTIVE_RISK_LEVELS:
    emit(False, "invalid_enum:risk_level", data=data)

source = data.get("source")
if not isinstance(source, str) or source not in SOURCES:
    emit(False, "invalid_enum:source", data=data)

timestamp = data.get("timestamp")
if not isinstance(timestamp, str) or not TIMESTAMP.match(timestamp):
    emit(False, "invalid_format:timestamp", data=data)

params = data.get("params", {})
if params is None:
    params = {}
if not isinstance(params, dict):
    emit(False, "invalid_format:params", data=data)
if len(params) > 10:
    emit(False, "too_many_keys:params", data=data)
for key, value in params.items():
    if not isinstance(key, str) or not PARAM_KEY.match(key):
        emit(False, f"invalid_format:params.{key}", data=data)
    if not isinstance(value, str):
        emit(False, f"invalid_format:params.{key}", data=data)
    if len(value) > 4096:
        emit(False, "field_too_long:params", data=data)

if "confirmation_token" in data:
    token = data.get("confirmation_token")
    if not isinstance(token, str) or len(token) < 32 or len(token) > 128:
        emit(False, "invalid_format:confirmation_token", data=data)

if os.path.exists(audit_path):
    try:
        with open(audit_path, "r", encoding="utf-8") as audit:
            for line in audit:
                line = line.strip()
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                except Exception:
                    continue
                if entry.get("request_id") == request_id:
                    emit(False, "duplicate_request_id", data=data)
    except OSError:
        pass

emit(True, None, status="valid", data=data)
PY
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    if [[ $# -ne 1 ]]; then
        printf '{"valid":false,"status":"validation_failed","error":"usage:skill_request_schema.sh <request_file>","request_id":null,"skill_name":null,"host":null,"risk_level":null}\n'
        exit 1
    fi
    validate_skill_request "$1"
fi
