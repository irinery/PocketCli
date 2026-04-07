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

1. Clona o repositório em `~/.pocketcli`
2. Detecta o sistema operacional
3. Pergunta o modo de instalação
4. Instala as dependências necessárias
5. Configura ZSH + Starship + TMUX
6. Instala e ativa o Tailscale
7. Inicia o ambiente

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
| `pocket ask` | Registra a última interação local para posterior validação explícita |
| `pocket memory save [id]` | Salva a interação recente ou revalida memória existente |
| `pocket memory discard <id>` | Reduz confidence de uma memória sem apagá-la |

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
│   ├── install_deps.sh       ← instala pacotes por OS/modo
│   ├── install_tailscale.sh  ← instala e faz login no Tailscale
│   ├── start_agent.sh        ← inicia tmux com sizing adaptativo e fallback para comandos ausentes
│   ├── start_viewer.sh       ← prepara SSH; `pocket` sem args cai no menu em terminais interativos
│   └── pocketcli_menu.sh     ← dashboard TUI com layout responsivo (split, stack, compact) e fallback leve
│
└── tools/
    └── fonts.sh          ← instala JetBrainsMono Nerd Font (opcional)
```

---

## Dependências instaladas

| Ferramenta | Uso |
|---|---|
| `git` | controle de versão |
| `curl` | downloads |
| `jq` | parsing JSON |
| `tmux` | sessões de terminal |
| `zsh` | shell principal |
| `fzf` | menus interativos |
| `ripgrep` | busca rápida (agent) |
| `htop` | monitor de recursos (agent) |
| `lazygit` | git TUI (agent) |
| `starship` | prompt (agent) |
| `tailscale` | rede privada + SSH |

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

# Registrar uma interação e salvar na memória validada
pocket ask "Persistir decisões do projeto em jsonl por escopo"
pocket memory save

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

Para validar regressões da interface em cenários de terminal heterogêneo (viewer/agent), execute também:

```bash
sh tests/test_menu_fallback.sh
sh tests/test_start_agent_launcher.sh
go test ./cmd/pocket
go test ./internal/memory
```

Isso cobre layout responsivo do menu shell, fallback de launcher em ausência de TTY completo e renderização adaptativa da TUI em Go.

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

- [ ] `Fleet Mode` — `pocket connect server1`, `pocket connect server2`
- [ ] Dashboard TUI com logs, git e deploy
- [ ] Deploy automático via git hook

---

## Licença

MIT — veja [LICENSE](LICENSE).
