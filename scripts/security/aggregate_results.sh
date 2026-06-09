#!/usr/bin/env bash
set -u

RESULTS_DIR=${SECURITY_RESULTS_DIR:-.security-results}
REPORT_FILE=${SECURITY_REPORT_FILE:-security-report.txt}
GITHUB_OUTPUT_FILE=${GITHUB_OUTPUT:-}
GITHUB_SUMMARY_FILE=${GITHUB_STEP_SUMMARY:-}

TOTAL_FINDINGS=0
BLOCKING_COUNT=0
HAS_BLOCKING=false
EXECUTION_ERRORS=0

scanner_label() {
    case "$1" in
        01) printf '01 Secret Leak\n' ;;
        02) printf '02 Shell Injection\n' ;;
        03) printf '03 Filesystem Permissions\n' ;;
        04) printf '04 SSH/Tailscale/tmux\n' ;;
        *) printf '%s\n' "$1" ;;
    esac
}

scanner_section() {
    case "$1" in
        01) printf 'Scanner 01 - Secret Leak\n' ;;
        02) printf 'Scanner 02 - Shell Injection\n' ;;
        03) printf 'Scanner 03 - Filesystem Permissions\n' ;;
        04) printf 'Scanner 04 - SSH/Tailscale/tmux\n' ;;
        *) printf 'Scanner %s\n' "$1" ;;
    esac
}

scanner_error_text() {
    case "$1" in
        01) printf 'Scanner 01 execution error\n' ;;
        02) printf 'Scanner 02 execution error\n' ;;
        03) printf 'Scanner 03 execution error\n' ;;
        04) printf 'Scanner 04 execution error\n' ;;
        *) printf 'Scanner %s execution error\n' "$1" ;;
    esac
}

is_finding_line() {
    local line severity
    line=$1

    case "$line" in
        ''|'No '*|'#'*) return 1 ;;
    esac

    severity=$(printf '%s\n' "$line" | awk -F: '{print $4}')
    case "$severity" in
        CRITICAL|HIGH|MEDIUM|LOW) return 0 ;;
        *) return 1 ;;
    esac
}

line_severity() {
    printf '%s\n' "$1" | awk -F: '{print $4}'
}

count_findings_for() {
    local id out line severity count blocking
    id=$1
    out=$RESULTS_DIR/${id}.out
    count=0
    blocking=0

    [ -f "$out" ] || {
        printf '0 0\n'
        return 0
    }

    while IFS= read -r line || [ -n "$line" ]; do
        is_finding_line "$line" || continue
        count=$((count + 1))
        severity=$(line_severity "$line")
        case "$severity" in
            CRITICAL|HIGH) blocking=$((blocking + 1)) ;;
        esac
    done < "$out"

    printf '%s %s\n' "$count" "$blocking"
}

write_outputs() {
    if [ -n "$GITHUB_OUTPUT_FILE" ]; then
        {
            printf 'has_blocking_findings=%s\n' "$HAS_BLOCKING"
            printf 'total_findings=%s\n' "$TOTAL_FINDINGS"
            printf 'blocking_count=%s\n' "$BLOCKING_COUNT"
        } >> "$GITHUB_OUTPUT_FILE"
    fi
}

commit_sha() {
    git rev-parse --short HEAD 2>/dev/null || printf 'unknown'
}

branch_name() {
    git rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'unknown'
}

timestamp_utc() {
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

write_report() {
    local id out err rc section has_lines line

    {
        printf '# PocketCLI Security Report\n'
        printf '# Generated: %s\n' "$(timestamp_utc)"
        printf '# Commit: %s\n' "$(commit_sha)"
        printf '# Branch: %s\n' "$(branch_name)"
        printf '\n'

        for id in 01 02 03 04; do
            section=$(scanner_section "$id")
            out=$RESULTS_DIR/${id}.out
            err=$RESULTS_DIR/${id}.err
            rc=$RESULTS_DIR/${id}.rc

            printf '## %s\n' "$section"
            has_lines=0
            if [ -f "$out" ]; then
                while IFS= read -r line || [ -n "$line" ]; do
                    is_finding_line "$line" || continue
                    printf '%s\n' "$line"
                    has_lines=1
                done < "$out"
            fi

            if [ -f "$rc" ] && [ "$(cat "$rc")" -eq 2 ]; then
                printf '%s\n' "$(scanner_error_text "$id")"
                has_lines=1
            fi

            if [ -f "$err" ] && [ -s "$err" ]; then
                sed 's/^/stderr: /' "$err"
                has_lines=1
            fi

            if [ "$has_lines" -eq 0 ]; then
                printf 'No findings.\n'
            fi
            printf '\n'
        done

        printf '## Summary\n'
        printf 'TOTAL_FINDINGS: %s\n' "$TOTAL_FINDINGS"
        printf 'BLOCKING: %s\n' "$HAS_BLOCKING"
    } > "$REPORT_FILE"
}

summary_status_for() {
    local rc findings blocking
    rc=$1
    findings=$2
    blocking=$3

    if [ "$rc" -eq 2 ]; then
        printf 'ERROR'
    elif [ "$blocking" -gt 0 ]; then
        printf 'FAIL'
    elif [ "$findings" -gt 0 ]; then
        printf 'WARN'
    else
        printf 'Pass'
    fi
}

summary_findings_for() {
    local findings blocking
    findings=$1
    blocking=$2

    if [ "$findings" -eq 0 ]; then
        printf '0'
    elif [ "$blocking" -gt 0 ]; then
        printf '%s blocking' "$blocking"
    else
        printf '%s WARN' "$findings"
    fi
}

write_summary() {
    local summary_file id rc counts findings blocking status finding_text out line severity file line_no rule desc mode
    summary_file=$1

    {
        printf '## PocketCLI Security Gate\n\n'
        if [ "$HAS_BLOCKING" = false ]; then
            printf '✅ No critical findings\n\n'
        fi
        if [ "$EXECUTION_ERRORS" -gt 0 ]; then
            printf '⚠️ Scanner execution error detected\n\n'
        fi

        printf '| Scanner | Status | Findings |\n'
        printf '|---|---|---|\n'
        for id in 01 02 03 04; do
            rc=0
            if [ -f "$RESULTS_DIR/${id}.rc" ]; then
                rc=$(cat "$RESULTS_DIR/${id}.rc")
            fi
            counts=$(count_findings_for "$id")
            findings=${counts% *}
            blocking=${counts#* }
            status=$(summary_status_for "$rc" "$findings" "$blocking")
            finding_text=$(summary_findings_for "$findings" "$blocking")
            printf '| %s | %s | %s |\n' "$(scanner_label "$id")" "$status" "$finding_text"
        done

        if [ "$EXECUTION_ERRORS" -gt 0 ]; then
            printf '\n### Scanner Errors\n\n'
            for id in 01 02 03 04; do
                if [ -f "$RESULTS_DIR/${id}.rc" ] && [ "$(cat "$RESULTS_DIR/${id}.rc")" -eq 2 ]; then
                    printf -- '- ⚠️ %s\n' "$(scanner_error_text "$id")"
                fi
            done
        fi

        printf '\n### Blocking Findings\n\n'
        if [ "$BLOCKING_COUNT" -eq 0 ]; then
            printf 'No blocking findings.\n'
        else
            printf '| File | Line/Mode | Rule | Severity | Description |\n'
            printf '|---|---|---|---|---|\n'
            for id in 01 02 03 04; do
                out=$RESULTS_DIR/${id}.out
                [ -f "$out" ] || continue
                while IFS= read -r line || [ -n "$line" ]; do
                    is_finding_line "$line" || continue
                    severity=$(line_severity "$line")
                    case "$severity" in
                        CRITICAL|HIGH) ;;
                        *) continue ;;
                    esac
                    file=$(printf '%s\n' "$line" | awk -F: '{print $1}')
                    line_no=$(printf '%s\n' "$line" | awk -F: '{print $2}')
                    rule=$(printf '%s\n' "$line" | awk -F: '{print $3}')
                    desc=$(printf '%s\n' "$line" | cut -d: -f5-)
                    mode=$line_no
                    printf '| %s | %s | %s | %s | %s |\n' "$file" "$mode" "$rule" "$severity" "$desc"
                done < "$out"
            done
        fi
    } > "$summary_file"
}

mkdir -p "$RESULTS_DIR"

for id in 01 02 03 04; do
    if [ ! -f "$RESULTS_DIR/${id}.rc" ]; then
        printf '0\n' > "$RESULTS_DIR/${id}.rc"
    fi

    counts=$(count_findings_for "$id")
    findings=${counts% *}
    blocking=${counts#* }
    TOTAL_FINDINGS=$((TOTAL_FINDINGS + findings))
    BLOCKING_COUNT=$((BLOCKING_COUNT + blocking))

    rc=$(cat "$RESULTS_DIR/${id}.rc")
    if [ "$rc" -eq 2 ] || [ "$rc" -gt 2 ]; then
        EXECUTION_ERRORS=$((EXECUTION_ERRORS + 1))
    fi
done

if [ "$BLOCKING_COUNT" -gt 0 ] || [ "$EXECUTION_ERRORS" -gt 0 ]; then
    HAS_BLOCKING=true
fi

write_report
write_outputs

if [ -n "$GITHUB_SUMMARY_FILE" ]; then
    write_summary "$GITHUB_SUMMARY_FILE"
else
    TMP_SUMMARY=$(mktemp)
    write_summary "$TMP_SUMMARY"
    cat "$TMP_SUMMARY"
    rm -f "$TMP_SUMMARY"
fi

exit 0
