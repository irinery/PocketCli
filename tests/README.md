# Estratégia de testes (CI)

Este diretório concentra testes de regressão shell e serve como referência para incluir novos testes.

## Tipos de testes na base

- **Unitários (Go)**: em `cmd/pocket/*_test.go`, `internal/*/*_test.go`.
- **Integração leve (Go)**: fluxo de dispatch do CLI (`root -> subcommands`) com doubles para dependências externas.
- **Regressão shell**: scripts em `tests/test_*.sh`.

## Como plugar novos testes por módulo

1. **Novo módulo interno (`internal/<modulo>`)**
   - Adicione `*_test.go` no mesmo pacote.
   - Evite dependências reais de sistema/rede; use injeção de função para mocks.

2. **Novo comando CLI (`cmd/pocket`)**
   - Cubra:
     - parsing de argumentos;
     - dispatch no `newRootCommand`;
     - propagação de erro do executor.
   - Adicione pelo menos um teste de integração leve executando `root.Execute()` com argumentos de processo controlados em teste (`os.Args`).

3. **Novo fluxo shell (`scripts/` / `bin/`)**
   - Adicione um script `tests/test_<fluxo>.sh` idempotente.
   - Inclua o script no loop `shell-tests` em `.github/workflows/ci.yml`.

## Perfil de CI “viewer-ish”

O workflow inclui o job `viewer-ish-sim`, que simula um ambiente próximo do viewer/iSH:

- container Alpine;
- sem instalar Tailscale real;
- binário `tailscale` mockado em `/usr/local/bin/tailscale`;
- limites de execução (`GOMAXPROCS=1`, `GOMEMLIMIT=768MiB`).

Ao adicionar testes que dependam de Tailscale, mantenha compatibilidade com esse mock.
