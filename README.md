# PocketCli

> Ambiente portátil de terminal para **acesso remoto, automação e gerenciamento de máquinas**.
> Funciona bem em dispositivos com poucos recursos — inclusive iPad com iSH.

---

## Instalação — 1 comando

```bash
curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/main/bootstrap.sh | bash
```

> **Recomendado:** verificar o checksum antes de executar:
> ```bash
> curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/main/bootstrap.sh -o bootstrap.sh
> sha256sum bootstrap.sh   # compare com o hash publicado nas Releases
> bash bootstrap.sh
> ```

---

## O que o instalador faz

1. `bootstrap.sh` clona ou baixa o repositório em `~/.pocketcli`
2. `install.sh` detecta o sistema operacional
3. Pergunta o modo de instalação
4. Chama `scripts/install_requirements.sh`
5. Roda o playbook Ansible local de requisitos quando `ansible-playbook` está disponível
6. Usa fallback shell mínimo apenas para destravar Ansible ou viewer/iSH sem Ansible em runtime
7. Configura profile + TMUX + Starship
8. Instala/ativa Tailscale quando possível
9. Inicia o ambiente

---

## Modos de instalação

```
1) Viewer  →  iPad ou terminal leve (apenas cliente SSH)
2) Agent   →  servidor ou máquina remota (ambiente completo)

No modo Agent, o instalador agora oferece três estratégias de configuração:
- primeiro ele compara a config atual do host com a config do projeto
- manter a config original do host
- aplicar a config do projeto
- testar o modo original, preservando as duas configs com troca automatizada
```

### Viewer
- Apenas cliente SSH
- Sem serviços rodando em background
- Ideal para iPad + iSH
- Conecta nas máquinas via Tailscale

### Agent
- Instala o toolkit completo
- Habilita Tailscale SSH
- Inicia ambiente tmux com `htop` + `lazygit`
- Permite gerenciamento remoto
- Ajusta automaticamente o tamanho inicial da sessão tmux ao terminal disponível, com fallback seguro em ambientes sem TTY completo
- Mostra um comparativo entre `tmux`, `starship` e integração de shell antes da escolha
- Pode preservar a config atual do host, aplicar a do projeto ou alternar entre ambas com `~/.pocketcli/scripts/switch_config.sh`

---

## Comandos disponíveis após instalação

| Comando | Descrição |
|---|---|
| `pocket-menu` | Control Deck leve com dashboard local, ações SSH e atalhos prontos para iPad/tmux |
| `pocket-radar` | Lista máquinas online no Tailscale |
| `pocket-update` | Atualiza o PocketCli preservando diferenças locais relevantes |
| `pocket ask [--mode ...] <prompt...>` | Executa o fluxo completo de contexto, memória, roteamento e backend |
| `pocket recall [--project NOME] [--host HOST] <query...>` | Busca memórias relevantes em `global`, projeto Git atual e host opcional |
| `pocket context [--json]` | Exibe ou serializa o contexto coletado sem chamar backend |
| `pocket memory save [id]` | Salva a interação recente ou revalida memória existente |
| `pocket memory discard <id>` | Reduz confidence de uma memória sem apagá-la |
| `pocket memory clean [--dry-run\|--force]` | Lista candidatos à remoção ou executa limpeza manual com confirmação por entrada |
| `pocket capabilities [--json]` | Detecta modo efetivo, TTY, layout TUI e dependências disponíveis |
| `pocket actions [--json]` | Resolve ações disponíveis para menu/TUI com base nas capacidades locais |
| `pocket ledger search [--json]` | Busca eventos operacionais locais por sessão, host e janela de tempo |
| `pocket ledger rebuild-index` | Reconstrói o índice local de sessões do ledger |
| `pocket insights list [--json]` | Mostra insights operacionais calculados a partir de capabilities e ledger |
| `pocket insights explain <id>` | Explica um insight específico com evidências e ação recomendada |
| `pocket hosts [--json]` | Lista hosts conhecidos em modo TUI ou inventário JSON |
| `pocket ssh <host>` | Abre sessão SSH interativa |
| `pocket exec [--json] [--timeout N] [--requested-by human\|llm_plan] [--session-id ID] [--approve] <host> <command...>` | Executa comando remoto via SSH/Tailscale SSH com allowlist, blocklist, timeout, truncamento e auditoria JSONL |
| `pocket exec --prepare <host> <command...>` | Cria envelope de execução para comando que precisa de aprovação |
| `pocket approve <envelope_id>` | Emite token temporário para um envelope |
| `pocket fleet plan --selector <selector> -- <command...>` | Cria plano de execução em múltiplos hosts |
| `pocket fleet exec --plan-id <id>` | Executa plano fleet salvo, validando token quando necessário |
| `pocket doctor [--json]` | Roda checks locais do runtime |
| `pocket eval insights --fixtures <dir>` | Valida fixtures de insights |
| `scripts/skills/skill_endpoint.sh [request.json]` | Endpoint local para `skill_request` do PocketWiki, com schema, guard rails, dispatcher Ansible e audit log JSONL |

---

## Runtime operacional

O PocketCli agora mantém um runtime local leve, sem depender do PocketWiki ou de middleware externo para funcionar:

- capabilities cache em `~/.local/share/pocketcli/capabilities.json`
- ledger JSONL em `~/.local/share/pocketcli/ledger/events/`
- envelopes em `~/.local/share/pocketcli/envelopes/`
- approvals temporários em `~/.local/share/pocketcli/approvals/`
- planos fleet em `~/.local/share/pocketcli/fleet/plans/`

Isso permite rodar o PocketCli sozinho no iSH/iPad, mas também deixa uma base limpa para compor com PocketWiki e MiddlewareAuth depois.

---

## Execução segura por envelope

Comando simples e read-only roda direto:

```bash
pocket exec prod-api uptime
pocket exec homelab df -h
```

Comando com risco operacional cria um envelope antes de executar:

```bash
pocket exec --prepare prod-api sudo systemctl restart nginx
```

O JSON retornado contém `envelope_id`, host, comando, decisão de safety e os próximos comandos sugeridos. Depois:

```bash
pocket approve <envelope_id>
pocket exec --envelope-id <envelope_id> --approval-token <token>
```

Quando `--envelope-id` é usado, o PocketCli executa o host e comando salvos no envelope. Ele não aceita host/comando extra nesse modo, justamente para evitar aprovar uma coisa e executar outra.

---

## Fleet

Fleet usa o mesmo modelo de safety do `exec`, mas aplicado a vários hosts:

```bash
# todos os hosts conhecidos
pocket fleet plan --selector all -- uptime

# host específico
pocket fleet plan --selector host:prod-api -- df -h

# por origem/tag do inventário
pocket fleet plan --selector tag:saved -- hostname
```

Se o plano exigir aprovação, o JSON do plano traz `requires_approval=true` e `envelope_id`:

```bash
pocket approve <envelope_id>
pocket fleet exec --plan-id <plan_id> --approval-token <token>
```

Para comandos seguros, dá para executar direto:

```bash
pocket fleet exec --selector all -- uptime
```

---

## Possibilidades práticas

Alguns usos que ficam possíveis com o runtime novo:

- usar `pocket capabilities --json` para a TUI esconder ações indisponíveis no iSH;
- usar `pocket actions --json` como registry declarativo para menu, atalhos e futuras integrações;
- usar `pocket ledger search --host-id <host>` para investigar falhas recentes sem depender de backend;
- usar `pocket insights list` para detectar ausência de SSH/Tailscale, fallback de backend ou host instável;
- usar `pocket ask --explain-context` para debugar o contexto antes de gastar chamada em LLM;
- usar `pocket fleet plan` para revisar impacto antes de rodar comando em várias máquinas.

---

## Estrutura do projeto

```
PocketCli/
├── bootstrap.sh          ← entrada: curl | bash
├── install.sh            ← orquestra a instalação
├── detect_os.sh          ← detecta OS, exporta $OS
├── radar.sh              ← lista máquinas Tailscale
│
├── profile/
│   ├── tmux.conf         ← perfil padrão compartilhado para tmux
│   ├── zshrc             ← aliases e integração shell referenciados pelo instalador
│   └── starship.toml     ← prompt padrão compartilhado
│
├── scripts/
│   ├── install_requirements.sh ← caminho feliz dos requisitos via Ansible
│   ├── install_deps.sh       ← fallback mínimo para destravar Ansible/viewer
│   ├── install_tailscale.sh  ← instala e faz login no Tailscale
│   ├── setup/
│   │   └── requirements.yml  ← playbook local de requisitos por OS/modo
│   ├── start_agent.sh        ← inicia tmux com sizing adaptativo e fallback para comandos ausentes
│   ├── start_viewer.sh       ← prepara SSH; `pocket` sem args cai no menu em terminais interativos
│   └── pocketcli_menu.sh     ← dashboard TUI com layout responsivo (split, stack, compact) e fallback leve
│
└── tools/
    └── fonts.sh          ← instala JetBrainsMono Nerd Font (opcional)
```

---

## Requisitos

O padrão do projeto é: requisitos de sistema entram em `scripts/setup/requirements.yml` e o instalador chama esse playbook por `scripts/install_requirements.sh`. Shell direto fica restrito ao bootstrap/download e ao fallback para conseguir rodar Ansible quando ele ainda não existe.

Resumo:

| Perfil | Ansible em runtime? | Requisitos principais |
|---|---|---|
| `viewer` | não | `sh`, `ssh`, `git`, `curl`, `jq`, `tmux`, `zsh`, Tailscale recomendado |
| `agent` | sim | viewer + `python3`, `ansible-playbook`, `ripgrep`, `htop`, `lazygit`, `starship` |
| desenvolvimento/teste | sim para validar tudo | agent + `go`, `gofmt`, `bash` e ferramentas de teste |

Detalhes em [docs/system-requirements.md](docs/system-requirements.md).

Achados de seguranca, usabilidade e integracoes da rodada atual estao em [docs/reviews/2026-07-09](docs/reviews/2026-07-09/README.md).

---

## Terminal

### ZSH + Starship
Prompt minimalista com informações de git e duração de comandos.

```
➜ projeto git:(main) !
```

### TMUX
| Atalho | Ação |
|---|---|
| `Ctrl+S` | Prefixo |
| `Ctrl+S + \|` | Split horizontal |
| `Ctrl+S + -` | Split vertical |
| `Ctrl+S + h/j/k/l` | Navegar entre painéis |
| `Ctrl+S + Enter` | Abrir novo pane já pedindo um host SSH |
| `Ctrl+S + e` | Split horizontal com prompt rápido para SSH |
| `Ctrl+S + Space` | Alternar layout do tmux |
| `Ctrl+S + z` | Zoom no pane atual |
| `Ctrl+S + R` | Recarregar config |

> No iPad/iSH, `pocket` abre o menu principal por padrão sempre que houver um terminal interativo acessível via `/dev/tty`; use `pocket resume` para reanexar a sessão tmux nomeada quando quiser retomar o workspace. If iSH is killed by low memory, PocketCli recreates the last saved command automatically on the next launch.

### Nerd Font (opcional)
Para exibir os ícones corretamente no emulador de terminal:

```bash
~/.pocketcli/tools/fonts.sh
```

---

## Uso típico

```bash
# Conectar a um servidor
pocket-menu

# Abrir o menu principal padrão do PocketCli
pocket

# Forçar a recriação/anexação da sessão tmux persistente
pocket resume

# Ver máquinas disponíveis
pocket-radar
pocket hosts --json

# Executar o fluxo completo de pergunta com fallback automático
pocket ask "Persistir decisões do projeto em jsonl por escopo"

# Ver o contexto compilado que seria enviado ao backend, sem chamar LLM
pocket ask --explain-context "por que esse host caiu?"

# Inspecionar o contexto bruto coletado antes de chamar qualquer backend
pocket context
pocket context --json

# Recuperar memórias relevantes para a query atual
pocket recall "ssh timeout"

# Salvar a interação recente na memória validada
pocket memory save

# Inspecionar ou executar limpeza manual das memórias candidatas
pocket memory clean --dry-run
pocket memory clean --force

# Detectar capacidades e modo efetivo do runtime
pocket capabilities
pocket capabilities --json

# Ver ações que a TUI/menu pode oferecer neste ambiente
pocket actions
pocket actions --include-disabled --json

# Rodar checks locais
pocket doctor
pocket doctor --json

# Ver insights derivados do histórico local
pocket insights list
pocket insights list --scope active --limit 5 --json

# Consultar o ledger operacional
pocket ledger search --since "$(date +%F)" --limit 20
pocket ledger search --host-id prod-api --json

# Executar comando read-only direto via SSH
pocket exec prod-api uptime

# Preparar execução que exige aprovação
pocket exec --prepare prod-api sudo systemctl restart nginx

# Aprovar o envelope retornado pelo prepare
pocket approve <envelope_id>

# Executar exatamente o comando salvo no envelope aprovado
pocket exec --envelope-id <envelope_id> --approval-token <token>

# Planejar execução em frota
pocket fleet plan --selector all --max-parallel 4 -- uptime

# Executar plano salvo
pocket fleet exec --plan-id <plan_id>

# Para plano fleet que exige aprovação
pocket approve <envelope_id>
pocket fleet exec --plan-id <plan_id> --approval-token <token>

# Atualizar
pocket-update

# Abrir tmux manualmente
tmux new -s work
```

---

## Compatibilidade

| Plataforma | Suporte |
|---|---|
| iPad + iSH | ✅ Viewer |
| Linux (Debian/Ubuntu) | ✅ Viewer + Agent |
| Alpine | ✅ Viewer + Agent |
| macOS | ✅ Viewer + Agent |
| Windows WSL | ✅ Viewer + Agent |
| Servidores remotos | ✅ Agent |

---

## Testes

Pré-requisitos para os checks locais em Go:

```bash
# confirme que o toolchain está disponível
go version
gofmt -h >/dev/null
```

Se `go` ou `gofmt` não estiverem no `PATH`, instale o Go antes de seguir. Em ambientes sandboxados, como Codex, use um `GOCACHE` gravável em `/tmp`.

Para reproduzir localmente o fluxo principal do CI em um único comando, execute:

```bash
sh scripts/run_local_ci.sh
```

Esse script:
- formata os arquivos Go em `cmd/` e `internal/` com `gofmt`
- executa a shell regression suite
- roda `go test ./...`
- gera o binário `./cmd/pocket`
- executa os smoke tests usando o binário Go recém-gerado

Se precisar rodar manualmente em um ambiente sandboxado, use:

```bash
env GOCACHE=/tmp/pocketcli-go-build-cache go test ./...
env GOCACHE=/tmp/pocketcli-go-build-cache go build -buildvcs=false -o /tmp/pocket-go ./cmd/pocket
env POCKETCLI_GO_BINARY=/tmp/pocket-go sh tests/run_smoke.sh
```

Para validar o fluxo logo após baixar o repositório, execute:

```bash
sh tests/test_bootstrap_install.sh
```

Esse teste usa dados mockados para validar o bootstrap inicial, a atualização do clone existente e a orquestração do `install.sh` sem depender de rede ou instalar pacotes reais.

Para validar a Skill Layer localmente sem Ansible real, execute:

```bash
shellcheck scripts/skills/*.sh
sh tests/test_skill_layer_schema.sh
sh tests/test_skill_layer_guardrails.sh
sh tests/test_skill_layer_dispatcher.sh
sh tests/test_skill_layer_audit.sh
```

Para validar regressões da interface em cenários de terminal heterogêneo (viewer/agent), execute também:

```bash
sh tests/test_menu_fallback.sh
sh tests/test_menu_incremental_render.sh
sh tests/test_start_agent_launcher.sh
go test ./cmd/pocket
go test ./internal/contextcollector
go test ./internal/backend
go test ./internal/tools
go test ./internal/memory
go test ./internal/audit
sh tests/test_release_body.sh
```

Para validar a etapa 8 do CLI isoladamente, execute:

```bash
go test ./cmd/pocket
```

Para validar o módulo TUI Renderer da fase 3 isoladamente, execute:

```bash
env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/tui/renderer
```

Para validar o módulo TUI Runtime da fase 4 isoladamente, execute:

```bash
env GOCACHE=/tmp/pocketcli-go-build-cache go test ./internal/tui/runtime
```

Para simular respostas de backend sem depender de um provedor real, você pode exportar uma destas variáveis antes do `pocket ask`:

```bash
export POCKETCLI_LOCAL_BACKEND_RESPONSE="resposta local de teste"
export POCKETCLI_REMOTE_BACKEND_RESPONSE="resposta remota de teste"
```

Ou apontar para um comando local:

```bash
export POCKETCLI_LOCAL_BACKEND_CMD='ollama run llama3.1'
```

Isso cobre layout responsivo do menu shell, diff incremental das linhas navegáveis, fallback de launcher em ausência de TTY completo e renderização adaptativa da TUI em Go.

---

## Filosofia

- **1 comando** para instalar tudo
- **Leve** — sem Electron, sem Docker, sem dependências pesadas
- **Keyboard-first** — menu principal com navegação estilo Vim, dashboard confiável e atalhos pensados para touch + teclado
- **Portátil** — mesmo ambiente em qualquer lugar
- **SSH-first** — funciona confortavelmente via tablet

---

## Roadmap

Plano detalhado por fases: `docs/implementation-plan.md`.

- [x] `Fleet Mode` básico — `fleet plan`, `fleet exec`, selectors e approvals
- [ ] `Fleet Mode` avançado — output preview, retry policy e ações TUI por lote
- [ ] Dashboard TUI com logs, git e deploy
- [ ] Deploy automático via git hook

---

## Licença

MIT — veja [LICENSE](LICENSE).
