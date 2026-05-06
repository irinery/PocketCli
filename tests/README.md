# Estratégia de testes (CI)

Este diretório concentra testes de regressão shell e serve como referência para incluir novos testes.

## Tipos de testes na base

- **Unitários (Go)**: em `cmd/pocket/*_test.go`, `internal/*/*_test.go`.
- **Integração leve (Go)**: fluxo de dispatch do CLI (`root -> subcommands`) com doubles para dependências externas.
- **Regressão shell**: scripts em `tests/test_*.sh`.
- **Smoke orientado ao produto**: scripts em `tests/smoke/test_*.sh`, executados via `pocket` com budgets de tempo por cenário.
- **Release workflow helpers**: scripts `tests/test_release_*.sh` validam contratos auxiliares usados pelos workflows de tag/release.

## Runners de CI

- `tests/run_all.sh`: descobre `tests/test_*.sh`, aceita exclusões via `POCKETCLI_TEST_EXCLUDES` e aplica budget total por perfil.
- `tests/run_smoke.sh`: descobre `tests/smoke/test_*.sh`, aplica budgets por cenário e registra tempos para o resumo da CI.
- `tests/lib/ci.sh`: concentra budgets por perfil (`linux`, `macos`, `alpine`) e helpers de medição.

Para reproduzir localmente a Fase 02 com a mesma ordem básica do workflow, rode:

```sh
export GOCACHE="${GOCACHE:-/tmp/pocketcli-go-build-cache}"
go vet ./...
sh tests/run_all.sh
go test ./...
go build -buildvcs=false -o /tmp/pocket-go ./cmd/pocket
POCKETCLI_GO_BINARY=/tmp/pocket-go sh tests/run_smoke.sh
```

Pré-requisitos: toolchain Go suportado pelo projeto, `git` no `PATH` e shell POSIX. Variáveis opcionais: `POCKETCLI_CI_PROFILE` para simular budgets de CI (`linux`, `macos`, `alpine`) e `POCKETCLI_TEST_EXCLUDES` para pular cenários específicos.

Para validar isoladamente a regressão de hardening de `set -eu` nas capabilities, rode `sh tests/test_capabilities_hardening.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `grep`, `sed` e `ln`. Variáveis de ambiente: nenhuma obrigatória.

Para validar isoladamente a regressão de render incremental do menu, rode `sh tests/test_menu_incremental_render.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `grep` e `sed`. Variáveis de ambiente: nenhuma obrigatória.

Para validar isoladamente o Security Gate, rode `sh tests/test_security_scanners.sh`.
Pré-requisitos: `bash`, shell POSIX, `awk`, `sed`, `find`, `stat`, `chmod`, `mktemp` e `git`. Variáveis opcionais: `REPO_ROOT` para apontar os scanners para outro diretório, `SECURITY_RESULTS_DIR` para o diretório de saídas individuais e `SECURITY_REPORT_FILE` para o relatório consolidado.

Para validar isoladamente a política de comandos remotos das Fases 04/05, rode:

```sh
env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/commandpolicy ./internal/remoteaccess ./cmd/pocket
sh tests/test_remote_command_policy.sh
```

Pré-requisitos: toolchain Go suportado pelo projeto, shell POSIX, `awk`, `grep`, `mktemp` e `tr`. Variáveis de ambiente: nenhuma obrigatória; use `GOCACHE` quando o ambiente não puder escrever no cache padrão do Go.

Para validar isoladamente a Skill Layer PocketWiki -> PocketCli, rode:

```sh
shellcheck scripts/skills/*.sh
sh tests/test_skill_layer_schema.sh
sh tests/test_skill_layer_guardrails.sh
sh tests/test_skill_layer_dispatcher.sh
sh tests/test_skill_layer_audit.sh
```

Pré-requisitos: `bash`, `python3`, `shellcheck`, shell POSIX, `grep`, `find`, `mktemp`, `wc`, `chmod` e `tr`. Os testes usam mocks de `ansible-playbook` e `timeout`; para execução real da skill layer em Agent, instale `ansible`/`ansible-core` e mantenha `inventory.ini` em `~/.pocketcli/ansible/`.

## Como plugar novos testes por módulo

1. **Novo módulo interno (`internal/<modulo>`)**
   - Adicione `*_test.go` no mesmo pacote.
   - Evite dependências reais de sistema/rede; use injeção de função para mocks.
   - Para rodar só os testes do Tool Contract (fase 6), use `go test ./internal/tools` e `go test ./internal/contextcollector`.
   - Para rodar só os testes do módulo Router (fase 4), use `go test ./internal/router`.
   - Para rodar só os testes do módulo Backend Contract (fase 5), use `go test ./internal/backend`.
   - Para rodar só os testes de Audit Log e Memory Cleanup (fase 7), use `go test ./internal/audit`, `go test ./internal/memory` e `go test ./cmd/pocket`.
   - Para rodar os testes da Fase 8 (CLI Commands), use `go test ./cmd/pocket`.
   - Para rodar os testes do módulo TUI Terminal (fase 1), use `env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/tui/terminal`.
   - Para rodar os testes do módulo TUI EventLoop (fase 2), use `env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/tui/event`.
   - Para rodar os testes do módulo TUI Renderer (fase 3), use `env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/tui/renderer`.
   - Para rodar os testes do módulo TUI Runtime (fase 4), use `env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/tui/runtime`.
   - Pré-requisitos do Tool Contract: toolchain Go suportado pelo projeto e `git` disponível no `PATH` para o cenário `git_status`. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Context Collector: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Router: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Backend Contract: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Audit Log e Memory Cleanup: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: `HOME` pode ser apontado para um diretório temporário ao validar fluxos de auditoria em `~/.pocket`.
   - Pré-requisitos do TUI Terminal: `/dev/ptmx` acessível e suporte a sinais POSIX (`SIGWINCH`, `SIGINT`, `SIGTERM`). Variáveis de ambiente: nenhuma obrigatória além de `GOCACHE` quando o ambiente não puder escrever em `~/Library/Caches/go-build`.
   - Pré-requisitos do TUI EventLoop: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória além de `GOCACHE` quando o ambiente não puder escrever em `~/Library/Caches/go-build`.
   - Pré-requisitos do TUI Renderer: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória além de `GOCACHE` quando o ambiente não puder escrever em `~/Library/Caches/go-build`.
   - Pré-requisitos do TUI Runtime: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória além de `GOCACHE` quando o ambiente não puder escrever em `~/Library/Caches/go-build`.

2. **Novo comando CLI (`cmd/pocket`)**
   - Cubra:
     - parsing de argumentos;
     - dispatch no `newRootCommand`;
     - propagação de erro do executor.
   - Adicione pelo menos um teste de integração leve executando `root.Execute()` com argumentos de processo controlados em teste (`os.Args`).

3. **Novo fluxo shell (`scripts/` / `bin/`)**
   - Adicione um script `tests/test_<fluxo>.sh` idempotente.
   - O script passa a ser descoberto automaticamente por `tests/run_all.sh`.
   - Para o helper de release body usado no workflow de release, rode `sh tests/test_release_body.sh`. Pré-requisitos: shell POSIX; variáveis de ambiente opcionais: `RELEASE_DISPLAY_TAG` e `RELEASE_COMMIT_SHA`.

4. **Novo cenário crítico do CLI (`pocket`)**
   - Adicione um script `tests/smoke/test_<cenario>.sh`.
   - Faça o teste entrar pelo comando `pocket`, com mocks explícitos para dependências externas.
   - Se o cenário for sensível a performance, defina ou ajuste o budget em `tests/lib/ci.sh`.

## Perfil de CI “alpine”

O workflow inclui o job `build-and-smoke-alpine`, que simula um ambiente próximo do viewer/iSH:

- container Alpine;
- sem instalar Tailscale real;
- binário `tailscale` mockado em `/usr/local/bin/tailscale`;
- limites de execução (`GOMAXPROCS=1`, `GOMEMLIMIT=768MiB`).

Ao adicionar testes que dependam de Tailscale, mantenha compatibilidade com esse mock.
