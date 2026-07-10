#!/usr/bin/env sh

pocket_code_dir() {
    printf '%s/.pocketcli' "${HOME}"
}

pocket_config_dir() {
    printf '%s/.config/pocketcli' "${HOME}"
}

pocket_data_dir() {
    printf '%s/.local/share/pocketcli' "${HOME}"
}

pocket_cache_dir() {
    printf '%s/.cache/pocketcli' "${HOME}"
}

pocket_runtime_log_file() {
    printf '%s/runtime.log' "$(pocket_cache_dir)"
}

pocket_session_file() {
    printf '%s/session.json' "$(pocket_data_dir)"
}

pocket_inventory_file() {
    printf '%s/inventory.json' "$(pocket_data_dir)"
}

pocket_ssh_policy_file() {
    printf '%s/ssh-policy.json' "$(pocket_config_dir)"
}

pocket_saved_hosts_file() {
    printf '%s/hosts' "$(pocket_code_dir)"
}

pocket_seed_hosts_file() {
    printf '%s/fallback_seeds' "$(pocket_code_dir)"
}

pocket_paths_init() {
    umask 077
    mkdir -p "$(pocket_config_dir)" "$(pocket_data_dir)" "$(pocket_cache_dir)"
    chmod 700 "$(pocket_config_dir)" "$(pocket_data_dir)" "$(pocket_cache_dir)"
}
