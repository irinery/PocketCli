# Session Manager (Fase 0)

Esta fase introduz um fluxo mestre mínimo:

`entrypoint -> session manager -> inventory refresh -> entry action`

## O que já foi centralizado

- Detecção de capacidades (`scripts/runtime/capabilities.sh`)
- Decisão de modo (`scripts/runtime/mode.sh`)
- Estado persistido de sessão (`~/.local/share/pocketcli/session.json`)
- Inventário persistido (`~/.local/share/pocketcli/inventory.json`)
- Log de runtime (`~/.cache/pocketcli/runtime.log`)

## Compatibilidade

- Comandos legados (`connect`, `run`, `copy`, `update`) permanecem ativos.
- `start_viewer.sh`, `start_agent.sh` e `resume` agora delegam para o manager.
