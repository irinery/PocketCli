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

Os runners isolam a stdin de cada teste em `/dev/null` e conferem se todos os arquivos descobertos foram processados. Testes que precisam de interação devem criar seu próprio pipe/PTY; depender da stdin do runner é considerado erro de contrato.

Para validar isoladamente que o helper PTY preserva `/dev/null`, inclusive quando executado como root em container, rode `sh tests/test_pocket_resume.sh`. Pré-requisitos: `script` (util-linux), shell POSIX e `/dev/null` como character device. Variáveis de ambiente: nenhuma obrigatória.

Para reproduzir localmente a Fase 02 com a mesma ordem básica do workflow, rode:

```sh
export GOCACHE="${GOCACHE:-/tmp/pocketcli-go-build-cache}"
go vet ./...
sh tests/run_all.sh
go test ./...
go build -buildvcs=false -o /tmp/pocket-go ./cmd/pocket
POCKETCLI_GO_BINARY=/tmp/pocket-go sh tests/run_smoke.sh
```

Para reproduzir o bloco paralelo usado pelo GitHub Actions (`go vet`, regressões de menu e regressões shell), rode:

```sh
POCKETCLI_CI_PROFILE=macos sh scripts/ci/run_static_shell_gates.sh
```

Pré-requisitos: toolchain Go, shell POSIX e os mesmos comandos auxiliares exigidos pelas suítes shell. Variáveis opcionais: `POCKETCLI_CI_PROFILE`, `POCKETCLI_TEST_EXCLUDES` e `POCKETCLI_CI_SUMMARY_FILE`.

Pré-requisitos: toolchain Go suportado pelo projeto, `git` no `PATH` e shell POSIX. Variáveis opcionais: `POCKETCLI_CI_PROFILE` para simular budgets de CI (`linux`, `macos`, `alpine`) e `POCKETCLI_TEST_EXCLUDES` para pular cenários específicos.

Os testes da skill layer (`test_skill_layer_*.sh`) também exigem `python3`; o job Alpine instala essa dependência explicitamente antes da suíte.

Para validar isoladamente a regressão de hardening de `set -eu` nas capabilities, rode `sh tests/test_capabilities_hardening.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `grep`, `sed` e `ln`. Variáveis de ambiente: nenhuma obrigatória.

Para validar a detecção cross-platform do Tailscale, rode `sh tests/test_tailscale_runtime_detection.sh` e `sh tests/test_tailscale_setup_fallback.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `grep`, `env` e `chmod`. Os testes usam uma CLI nativa mockada fora do `PATH`, uma interface VPN mockada e não instalam nem alteram o Tailscale real. Variáveis de ambiente: nenhuma obrigatória; `POCKETCLI_TAILSCALE_CLI` é configurada internamente para validar o override de descoberta.

Para validar a mesma descoberta nos fluxos Go de capabilities, status e connect, rode `go test ./internal/tailscale ./internal/capabilities ./internal/connect`.
Pré-requisitos: toolchain Go suportado pelo projeto. Variáveis de ambiente: nenhuma obrigatória; os testes isolam `HOME`, `PATH` e `POCKETCLI_TAILSCALE_CLI` quando necessário.

Para validar isoladamente a regressão de render incremental do menu, rode `sh tests/test_menu_incremental_render.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `grep` e `sed`. Variáveis de ambiente: nenhuma obrigatória.

Para validar a integridade visual interativa do `pocket menu`, rode:

```sh
bash scripts/testing/visual_integrity/run_tests.sh
```

Pré-requisitos: `bash`, `tmux` e `tput`/`ncurses` no `PATH`. A suíte provisiona um terminal tmux headless, executa `pocket menu`, captura snapshots e compara com fixtures em `scripts/testing/visual_integrity/fixtures/`. Para regenerar fixtures depois de uma mudança intencional de layout, rode `POCKETCLI_VISUAL_RECORD=1 bash scripts/testing/visual_integrity/run_tests.sh`.

Para validar isoladamente o contrato da Fase 01 do módulo Ansible, rode `sh tests/test_ansible_adapter.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `wc`, `dd` e `chmod`. O teste usa mocks locais de `ansible` e `ansible-playbook`, então não exige Ansible real instalado. Variáveis de ambiente: nenhuma obrigatória.

Para validar isoladamente o contrato da Fase 02 do módulo Ansible, rode `sh tests/test_ansible_inventory.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `wc`, `jq` ou `python3`, e `chmod`. O teste usa mocks locais de `tailscale`, `ansible` e `ansible-playbook`. Variáveis de ambiente: nenhuma obrigatória.

Para validar isoladamente o contrato da Fase 03 do módulo Ansible, rode `sh tests/test_ansible_registry.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `wc`, `ln` e `chmod`. O teste usa mocks locais de `ansible` e `ansible-playbook`. Variáveis de ambiente: nenhuma obrigatória.

Para validar isoladamente o contrato da Fase 04 do módulo Ansible, rode `sh tests/test_ansible_wiki_hook.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `wc`, `tr` e `chmod`. O teste usa mocks locais de `ansible` e `ansible-playbook`. Variáveis de ambiente: nenhuma obrigatória.

Para validar isoladamente o contrato da Fase 05 do módulo Ansible, rode `sh tests/test_ansible_init.sh`.
Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `find`, `seq` e `chmod`. Variáveis de ambiente: nenhuma obrigatória.

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
