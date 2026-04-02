#!/usr/bin/env sh

session_resolve_entry_action() {
    case "${SESSION_INTENT:-boot}" in
        resume)
            printf 'restore_workspace\n'
            ;;
        viewer)
            printf 'show_menu\n'
            ;;
        agent)
            printf 'open_default_layout\n'
            ;;
        *)
            printf 'show_menu\n'
            ;;
    esac
}
