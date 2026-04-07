# Estratégia de testes (CI)

Este diretório concentra testes de regressão shell e serve como referência para incluir novos testes.

## Tipos de testes na base

- **Unitários (Go)**: em `cmd/pocket/*_test.go`, `internal/*/*_test.go`.
- **Integração leve (Go)**: fluxo de dispatch do CLI (`root -> subcommands`) com doubles para dependências externas.
- **Regressão shell**: scripts em `tests/test_*.sh`.
- **Smoke orientado ao produto**: scripts em `tests/smoke/test_*.sh`, executados via `pocket` com budgets de tempo por cenário.

## Runners de CI

- `tests/run_all.sh`: descobre `tests/test_*.sh`, aceita exclusões via `POCKETCLI_TEST_EXCLUDES` e aplica budget total por perfil.
- `tests/run_smoke.sh`: descobre `tests/smoke/test_*.sh`, aplica budgets por cenário e registra tempos para o resumo da CI.
- `tests/lib/ci.sh`: concentra budgets por perfil (`linux`, `macos`, `alpine`) e helpers de medição.

## Como plugar novos testes por módulo

1. **Novo módulo interno (`internal/<modulo>`)**
   - Adicione `*_test.go` no mesmo pacote.
   - Evite dependências reais de sistema/rede; use injeção de função para mocks.
   - Para rodar só os testes do Tool Contract (fase 6), use `go test ./internal/tools` e `go test ./internal/contextcollector`.
   - Para rodar só os testes do módulo Router (fase 4), use `go test ./internal/router`.
   - Para rodar só os testes do módulo Backend Contract (fase 5), use `go test ./internal/backend`.
   - Para rodar só os testes de Audit Log e Memory Cleanup (fase 7), use `go test ./internal/audit`, `go test ./internal/memory` e `go test ./cmd/pocket`.
   - Pré-requisitos do Tool Contract: toolchain Go suportado pelo projeto e `git` disponível no `PATH` para o cenário `git_status`. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Context Collector: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Router: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Backend Contract: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória.
   - Pré-requisitos do Audit Log e Memory Cleanup: nenhum além do toolchain Go suportado pelo projeto. Variáveis de ambiente: `HOME` pode ser apontado para um diretório temporário ao validar fluxos de auditoria em `~/.pocket`.

2. **Novo comando CLI (`cmd/pocket`)**
   - Cubra:
     - parsing de argumentos;
     - dispatch no `newRootCommand`;
     - propagação de erro do executor.
   - Adicione pelo menos um teste de integração leve executando `root.Execute()` com argumentos de processo controlados em teste (`os.Args`).

3. **Novo fluxo shell (`scripts/` / `bin/`)**
   - Adicione um script `tests/test_<fluxo>.sh` idempotente.
   - O script passa a ser descoberto automaticamente por `tests/run_all.sh`.

4. **Novo cenário crítico do CLI (`pocket`)**
   - Adicione um script `tests/smoke/test_<cenario>.sh`.
   - Faça o teste entrar pelo comando `pocket`, com mocks explícitos para dependências externas.
   - Se o cenário for sensível a performance, defina ou ajuste o budget em `tests/lib/ci.sh`.

## Perfil de CI “viewer-ish”

O workflow inclui o job `viewer-ish-sim`, que simula um ambiente próximo do viewer/iSH:

- container Alpine;
- sem instalar Tailscale real;
- binário `tailscale` mockado em `/usr/local/bin/tailscale`;
- limites de execução (`GOMAXPROCS=1`, `GOMEMLIMIT=768MiB`).

Ao adicionar testes que dependam de Tailscale, mantenha compatibilidade com esse mock.
