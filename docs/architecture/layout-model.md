# Layout engine

`layout_engine_apply_or_restore` carrega spec declarativa em `specs/layouts/*.json`,
valida requisitos (`tty`, `tmux`) e aplica fallback para `viewer-default` quando necessário.

No modo `restore_workspace`, tenta primeiro restaurar sessão tmux existente antes de reaplicar layout.
