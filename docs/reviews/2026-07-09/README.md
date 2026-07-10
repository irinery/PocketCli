# Review de seguranca, usabilidade e integracoes - 2026-07-09

Esta rodada revisou os caminhos de execucao remota, persistencia local, entrada shell, instalacao, Tailscale, backend por comando e coleta de contexto.

| Item | Criticidade | Estado |
|---|---:|---|
| [SEC-01](SEC-01-approval-capability.md) | 5 | corrigido |
| [SEC-02](SEC-02-private-runtime-data.md) | 4 | corrigido |
| [SEC-03](SEC-03-remote-executor.md) | 4 | corrigido |
| [SEC-04](SEC-04-shell-transport.md) | 4 | corrigido |
| [SEC-05](SEC-05-install-update-permissions.md) | 4 | corrigido |
| [SEC-06](SEC-06-context-and-backend-boundaries.md) | 4 | corrigido |
| [UX-01](UX-01-posix-runtime.md) | 3 | corrigido |
| [CI-01](CI-01-macos-go-toolchain.md) | 3 | corrigido |
| [SEC-07](SEC-07-tailscale-supply-chain.md) | 4 | aberto |

Validacao final da branch: `sh scripts/run_local_ci.sh`, `shellcheck -S warning ...`, `go vet ./...` e scanners em `scripts/security/`.

O scanner de filesystem ainda emite `SCRIPT_WORLD_EXECUTABLE` para scripts versionados em `0755`. Isso permanece o [L-01 aceito](../../review-2026-07-03.md) da rodada anterior: o bit e necessario para distribuicao de scripts e nao equivale a arquivo de estado ou segredo legivel.
