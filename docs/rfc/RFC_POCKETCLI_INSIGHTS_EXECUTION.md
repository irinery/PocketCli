# RFC - PocketCli Insights Execution Runtime

**Projeto:** PocketCli
**Data:** 2026-06-08
**Status:** COMPLETO
**Fases:** 8 arquivos a gerar
**Gerado por:** Codex GPT-5, sessao local PocketCli

## 1. Visao Geral

### 1.1 Problema

O PocketCli ja funciona como CLI/TUI portatil para iPad/iSH, Linux, macOS e hosts remotos: ele detecta modo viewer/agent, abre menu em terminal interativo, usa tmux, Tailscale/SSH, coleta contexto local, consulta memoria JSONL e roteia chamadas entre backend local/remoto. O problema e que os sinais operacionais que melhorariam a execucao ainda ficam espalhados entre shell scripts, Go, audit log, session state, inventory e memoria.

Na pratica, o CLI ainda decide pouco com base no proprio historico. O menu sabe status local, mas isso nao vira insight reutilizavel pelo `pocket ask`. O `pocket ask` sabe contexto de projeto, mas nao sabe que um host falha sempre por DNS, que a sessao atual esta em iSH sem `tmux`, que o ultimo `pocket update` preservou profile customizado, ou que uma acao remota precisa de aprovacao. O audit log hoje e util para contabilidade simples, mas nao e uma fonte pesquisavel de aprendizado operacional.

O Hermes Agent foi usado como referencia externa em `https://github.com/NousResearch/hermes-agent`, commit verificado `c3055d61857751ad82a2bf9e4f5de5d26a8f2a16`. Insights relevantes: toolsets compostos, session store pesquisavel, scheduler auditavel, skills com metadata, context fencing, safety guardrails, ambientes de execucao por SSH e delegacao restrita. Para o PocketCli, isso deve ser adaptado ao escopo dele: terminal leve, POSIX sh onde fizer sentido, Go sem dependencia pesada obrigatoria, Tailscale/SSH como foco e funcionamento autonomo sem PocketWiki ou MiddlewareAuth.

### 1.2 Solucao

Implementar um PocketCli Insights Execution Runtime: uma camada local, rebuildable e opcionalmente assistida por LLM que coleta sinais do proprio CLI/TUI, gera insights acionaveis, alimenta menus/comandos/contexto e melhora execucao remota sem depender de outros apps da stack.

### 1.3 Fora de escopo

- Exigir PocketWiki para o PocketCli funcionar.
- Exigir MiddlewareAuth para `pocket ask`, `pocket context`, `pocket hosts`, `pocket ssh` ou `pocket exec`.
- Substituir o menu POSIX atual por framework TUI pesado.
- Criar runtime always-on obrigatorio em background.
- Fazer browser automation, controle desktop ou scraping web.
- Salvar segredos, tokens ou conteudo de `.env` no ledger.
- Autoexecutar comandos destrutivos sem aprovacao explicita.
- Migrar memoria existente de `~/.pocket` de forma irreversivel.

## 2. Mapa de Fases

| Fase | Arquivo | Nome do componente | Depende de | Paralelizavel com |
|------|---------|--------------------|------------|-------------------|
| 01 | 01_capability_manifest.md | Capability Manifest e Mode Detector | - | - |
| 02 | 02_event_ledger.md | Event Ledger Local e Session Index | 01 | - |
| 03 | 03_insight_collector.md | Insight Collector e Scoring | 01, 02 | - |
| 04 | 04_tui_action_registry.md | TUI Action Registry e Command Palette | 01, 03 | 05 |
| 05 | 05_context_compiler.md | AI Context Compiler e Backend Policy | 01, 02, 03 | 04 |
| 06 | 06_host_fleet_insights.md | Host/Fleet Execution Insights | 01, 02, 03 | 04, 05 |
| 07 | 07_safety_approvals.md | SafetyPolicy, Run Envelope e Aprovacoes | 01, 02, 06 | 05 |
| 08 | 08_doctor_eval_gates.md | Doctor, Eval e Gates de Release | 01, 02, 03, 04, 05, 06, 07 | - |

### Convencoes globais

- Todos os caminhos devem respeitar os diretories existentes: config em `~/.config/pocketcli`, dados em `~/.local/share/pocketcli`, cache em `~/.cache/pocketcli` e codigo em `~/.pocketcli`.
- Dados persistentes novos devem ser rebuildable sempre que possivel. A fonte primaria de eventos e JSONL append-only.
- JSONL e o formato padrao de eventos. Cada linha deve ser um JSON completo terminado por `\n`.
- Nenhum componente pode ler ou persistir arquivos bloqueados por politica: `.env*`, chaves privadas, `.ssh`, `.aws`, `.kube`, credenciais, tokens e arquivos com sufixos `.pem`, `.key`, `.p12`, `.pfx`.
- Erros externos devem retornar objeto com `code`, `message` e `recoverable`.
- Timeouts sao em milissegundos.
- IDs gerados localmente usam UUID v4 ou formato deterministico documentado.
- O modo standalone e obrigatorio: se PocketWiki e MiddlewareAuth nao existirem, todas as fases continuam entregando valor local.
- Shell novo deve ser POSIX sh quando fizer parte dos scripts principais.

### Definition of Done global

- [ ] todos os testes das fases 01 a 08 passando
- [ ] `sh scripts/run_local_ci.sh` documentado e passando em ambiente com Go disponivel
- [ ] nenhum teste novo depende de rede externa real sem fallback/mock
- [ ] `pocket` sem argumentos continua abrindo menu em terminal interativo
- [ ] `pocket ask` continua funcionando sem PocketWiki e sem MiddlewareAuth
- [ ] `pocket context` mostra claramente quando o contexto foi parcial
- [ ] nenhum arquivo fora de `profile/` passa a carregar personalizacao direta do usuario
- [ ] update preserva arquivos locais diferentes do padrao

## 3.1 - Fase 01: Capability Manifest e Mode Detector

### O que e

Componente que consolida capacidades locais do PocketCli em um contrato unico. Ele transforma checks espalhados em um manifesto consumivel por CLI, TUI, contexto, doctor e fluxos de execucao. O objetivo e o PocketCli saber exatamente o que pode fazer no dispositivo atual antes de sugerir ou executar qualquer acao.

**Responsabilidades:**
- Detectar modo efetivo `viewer`, `agent` ou `degraded`.
- Detectar ferramentas locais: `sh`, `ssh`, `scp`, `tmux`, `tailscale`, `jq`, `fzf`, `rg`, `git`, `go`.
- Detectar caracteristicas de terminal: TTY, largura, altura, UTF-8, iSH, tmux ativo.
- Publicar `pocket capabilities --json` e arquivo cacheado `~/.local/share/pocketcli/capabilities.json`.
- Expor razoes de degradacao legiveis para TUI e comandos.

**Fora do escopo deste componente:**
- Executar acoes remotas.
- Fazer login Tailscale.
- Corrigir dependencia ausente automaticamente.
- Consultar PocketWiki ou MiddlewareAuth.

---

### Testes obrigatorios

```
TESTE 01-01
dado:    ambiente com ssh, sh, git e tty valido
quando:  `pocket capabilities --json` e executado
entao:   retorna JSON com schema_version=1, mode_effective definido e capabilities.has_ssh=true

TESTE 01-02
dado:    PATH sem tailscale
quando:  o detector roda
entao:   retorna capabilities.has_tailscale=false e degradation_reasons contem "tailscale_missing"

TESTE 01-03  [edge case]
dado:    terminal com largura 40 e altura 10
quando:  o detector calcula perfil de TUI
entao:   retorna tui_layout="compact" e nao falha por terminal pequeno

TESTE 01-04  [seguranca]
dado:    HOME contendo arquivo `.env` e chave privada em `.ssh`
quando:  o detector roda
entao:   o manifesto nao inclui conteudo, path completo de segredo, nem valor de variavel sensivel
```

---

### Contratos de interface

```yaml
interface:
  id: IF-01-1
  tipo: cli
  nome: pocket capabilities

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "valor omitido equivale a text"
      valores_enum: ["text", "json"]

  saida:
    sucesso:
      tipo: PocketCapabilityManifest
      campos:
        - campo: schema_version
          tipo: int
        - campo: mode_effective
          tipo: enum
        - campo: capabilities
          tipo: PocketCapabilitySet
        - campo: degradation_reasons
          tipo: array<string>
    falha:
      valores_possiveis:
        - code: ERR_CAP_HOME_UNAVAILABLE
          quando: "HOME nao pode ser resolvido"
          acao: "retorna erro e nao grava cache"
        - code: ERR_CAP_CACHE_WRITE
          quando: "cache nao pode ser gravado"
          acao: "imprime manifesto calculado e marca cache_status=write_failed"

  comportamento_em_falha:
    - condicao: "ferramenta opcional ausente"
      acao: "marca boolean false e adiciona reason"
      observavel: "comando retorna exit 0"
    - condicao: "diretorio de dados inacessivel"
      acao: "retorna erro estruturado"
      observavel: "exit 1 com code ERR_CAP_CACHE_WRITE"
```

```yaml
interface:
  id: IF-01-2
  tipo: file
  nome: ~/.local/share/pocketcli/capabilities.json

  entrada:
    - campo: write_mode
      tipo: enum
      obrigatorio: true
      validacao: "sempre escrita atomica via arquivo temporario no mesmo diretorio"
      valores_enum: ["atomic_replace"]

  saida:
    sucesso:
      tipo: PocketCapabilityManifest
      campos:
        - campo: generated_at
          tipo: string
        - campo: ttl_seconds
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_CAP_INVALID_JSON
          quando: "arquivo cacheado esta truncado ou invalido"
          acao: "ignora cache e recalcula"
        - code: ERR_CAP_PERMISSION
          quando: "diretorio nao permite leitura"
          acao: "usa manifest in-memory somente nesta execucao"

  comportamento_em_falha:
    - condicao: "cache invalido"
      acao: "rebuild"
      observavel: "manifesto novo com cache_status=recovered"
```

---

### Estruturas de dados

```yaml
tipo: PocketCapabilityManifest
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: "valor fixo 1"
  - nome: generated_at
    tipo: string
    obrigatorio: true
    default: "nenhum"
    limite: "RFC3339 UTC"
  - nome: ttl_seconds
    tipo: int
    obrigatorio: true
    default: 60
    limite: "1..3600"
  - nome: mode_requested
    tipo: enum
    obrigatorio: true
    default: "auto"
    limite: "auto|viewer|agent"
  - nome: mode_effective
    tipo: enum
    obrigatorio: true
    default: "viewer"
    limite: "viewer|agent|degraded"
  - nome: host
    tipo: PocketHostIdentity
    obrigatorio: true
    default: "objeto vazio"
    limite: "hostname 128 chars"
  - nome: terminal
    tipo: PocketTerminalProfile
    obrigatorio: true
    default: "objeto detectado"
    limite: "nenhum"
  - nome: capabilities
    tipo: PocketCapabilitySet
    obrigatorio: true
    default: "todos false"
    limite: "nenhum"
  - nome: degradation_reasons
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 32 itens, 80 chars por item"
```

```yaml
tipo: PocketCapabilitySet
campos:
  - nome: has_tty
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_tmux
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_tailscale
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_ssh
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_scp
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_jq
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_fzf
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_rg
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_git
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: has_go
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketTerminalProfile
campos:
  - nome: is_interactive
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: is_ish
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: is_tmux
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: cols
    tipo: int
    obrigatorio: true
    default: 80
    limite: "10..500"
  - nome: rows
    tipo: int
    obrigatorio: true
    default: 24
    limite: "3..200"
  - nome: tui_layout
    tipo: enum
    obrigatorio: true
    default: "stack"
    limite: "split|stack|compact|plain"
```

```yaml
tipo: PocketHostIdentity
campos:
  - nome: hostname
    tipo: string
    obrigatorio: true
    default: ""
    limite: "128 chars"
  - nome: os
    tipo: string
    obrigatorio: true
    default: ""
    limite: "64 chars"
  - nome: arch
    tipo: string
    obrigatorio: true
    default: ""
    limite: "64 chars"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| has_tty=false | qualquer | mode_effective=degraded | desabilitar TUI interativa |
| has_tty=true | has_tmux=true | mode_effective conforme solicitado | habilitar resume/layout |
| mode_requested=agent | has_tmux=false | mode_effective=degraded | reason `tmux_missing` |
| has_ssh=false | qualquer | SSH indisponivel | ocultar acoes ssh/fleet |
| has_tailscale=false | has_ssh=true | SSH manual disponivel | ocultar radar Tailscale |

**Limites numericos explicitos:**
- timeout de deteccao por comando: 500 ms
- timeout total do detector: 3000 ms
- ttl do cache: 60 s
- tamanho maximo do manifesto: 64 KB
- maximo de degradation_reasons: 32

---

### Dependencias desta fase

```yaml
- id: DEP-01-1
  tipo: componente_interno
  nome: scripts/runtime/paths.sh
  versao: "schema atual"
  papel: "definir diretorios padrao"
  fallback: "usar HOME/.local/share/pocketcli e HOME/.config/pocketcli"
  obrigatoria: false
- id: DEP-01-2
  tipo: biblioteca
  nome: Go standard library
  versao: "1.22"
  papel: "detector do comando Go"
  fallback: "detector POSIX sh para menu"
  obrigatoria: true
- id: DEP-01-3
  tipo: servico_externo
  nome: tailscale
  versao: "qualquer"
  papel: "capacidade opcional de rede privada"
  fallback: "marcar has_tailscale=false"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket capabilities --json`
- [ ] cache atomico em `~/.local/share/pocketcli/capabilities.json`
- [ ] detector POSIX reutilizavel pelo menu
- [ ] todos os testes 01-01 a 01-04 passando

## 3.2 - Fase 02: Event Ledger Local e Session Index

### O que e

Ledger local append-only para registrar eventos operacionais do PocketCli com granularidade suficiente para gerar insights. Ele substitui o audit log pobre como fonte primaria, mas nao remove compatibilidade com o audit log atual. A estrutura precisa ser leve, legivel e rebuildable em iSH.

**Responsabilidades:**
- Registrar eventos de comandos, TUI, SSH, update, contexto, memoria, backend e erros.
- Manter indice compacto por sessao em `~/.local/share/pocketcli/session-index.json`.
- Permitir busca por session_id, host_id, comando, status e janela de tempo.
- Fazer rotacao e compactacao sem banco externo.
- Redigir segredo antes de persistir.

**Fora do escopo deste componente:**
- Armazenar transcript completo de LLM.
- Indexacao semantica.
- Sincronizar eventos com PocketWiki.
- Scheduler ou execucao automatica.

---

### Testes obrigatorios

```
TESTE 02-01
dado:    comando `pocket ask "teste"` com session_id conhecido
quando:  o comando termina com sucesso
entao:   o ledger contem evento command.completed com command="ask" e mesmo session_id

TESTE 02-02
dado:    arquivo JSONL truncado na ultima linha
quando:  `pocket ledger rebuild-index` roda
entao:   ignora apenas a linha invalida, reporta skipped_lines=1 e recria indice valido

TESTE 02-03  [edge case]
dado:    10000 eventos em um dia
quando:  busca por session_id e executada
entao:   retorna no maximo limit informado e nao carrega arquivos de outros dias sem necessidade

TESTE 02-04  [seguranca]
dado:    evento contendo `Authorization: Bearer abc` e `password=123`
quando:  evento e gravado
entao:   ledger persiste valores redigidos como `[REDACTED]`
```

---

### Contratos de interface

```yaml
interface:
  id: IF-02-1
  tipo: function
  nome: LedgerAppend

  entrada:
    - campo: event
      tipo: PocketLedgerEvent
      obrigatorio: true
      validacao: "schema_version=1, type nao vazio, timestamp RFC3339 UTC"
      valores_enum: []

  saida:
    sucesso:
      tipo: LedgerAppendResult
      campos:
        - campo: path
          tipo: string
        - campo: offset
          tipo: int
        - campo: redacted
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_LEDGER_INVALID_EVENT
          quando: "evento nao passa validacao"
          acao: "recusa gravacao"
        - code: ERR_LEDGER_WRITE_FAILED
          quando: "append falha"
          acao: "retorna erro recuperavel e mantem comando principal rodando se possivel"
        - code: ERR_LEDGER_SECRET_BLOCKED
          quando: "payload contem path de arquivo sensivel"
          acao: "remove payload e grava evento com redacted=true"

  comportamento_em_falha:
    - condicao: "ledger indisponivel"
      acao: "continua comando principal quando seguro"
      observavel: "stderr contem warning com code ERR_LEDGER_WRITE_FAILED"
```

```yaml
interface:
  id: IF-02-2
  tipo: cli
  nome: pocket ledger search

  entrada:
    - campo: session_id
      tipo: string
      obrigatorio: false
      validacao: "UUID ou string vazia"
      valores_enum: []
    - campo: host_id
      tipo: string
      obrigatorio: false
      validacao: "max 128 chars"
      valores_enum: []
    - campo: since
      tipo: string
      obrigatorio: false
      validacao: "RFC3339 ou YYYY-MM-DD"
      valores_enum: []
    - campo: limit
      tipo: int
      obrigatorio: false
      validacao: "1..500"
      valores_enum: []

  saida:
    sucesso:
      tipo: LedgerSearchResult
      campos:
        - campo: events
          tipo: array<PocketLedgerEvent>
        - campo: truncated
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_LEDGER_BAD_FILTER
          quando: "since ou limit invalido"
          acao: "retorna erro sem pesquisar"
        - code: ERR_LEDGER_INDEX_CORRUPT
          quando: "indice invalido"
          acao: "sugere `pocket ledger rebuild-index`"

  comportamento_em_falha:
    - condicao: "indice ausente"
      acao: "busca linear nos arquivos recentes"
      observavel: "resultado inclui index_status=missing"
```

---

### Estruturas de dados

```yaml
tipo: PocketLedgerEvent
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: "valor fixo 1"
  - nome: event_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: timestamp
    tipo: string
    obrigatorio: true
    default: "agora UTC"
    limite: "RFC3339 UTC"
  - nome: session_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "80 chars"
  - nome: type
    tipo: enum
    obrigatorio: true
    default: "command.started"
    limite: "command.started|command.completed|command.failed|tui.action|ssh.probe|ssh.exec|update.started|update.completed|context.collected|backend.call|memory.hit|safety.denied"
  - nome: command
    tipo: string
    obrigatorio: false
    default: ""
    limite: "128 chars"
  - nome: host_id
    tipo: string
    obrigatorio: false
    default: ""
    limite: "128 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|error|denied|partial|timeout"
  - nome: duration_ms
    tipo: int
    obrigatorio: false
    default: 0
    limite: "0..86400000"
  - nome: payload
    tipo: PocketLedgerPayload
    obrigatorio: true
    default: "campos vazios"
    limite: "32 KB depois de redaction"
```

```yaml
tipo: PocketLedgerPayload
campos:
  - nome: message
    tipo: string
    obrigatorio: false
    default: ""
    limite: "4096 chars"
  - nome: error_code
    tipo: string
    obrigatorio: false
    default: ""
    limite: "80 chars"
  - nome: backend
    tipo: string
    obrigatorio: false
    default: ""
    limite: "80 chars"
  - nome: model
    tipo: string
    obrigatorio: false
    default: ""
    limite: "120 chars"
  - nome: token_usage
    tipo: int
    obrigatorio: false
    default: 0
    limite: "0..1000000"
  - nome: redaction_count
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..1000"
```

```yaml
tipo: LedgerAppendResult
campos:
  - nome: path
    tipo: string
    obrigatorio: true
    default: ""
    limite: "1024 chars"
  - nome: offset
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..9223372036854775807"
  - nome: redacted
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: LedgerSearchResult
campos:
  - nome: events
    tipo: array<PocketLedgerEvent>
    obrigatorio: true
    default: "[]"
    limite: "max 500 eventos"
  - nome: truncated
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: index_status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|missing|rebuilt|corrupt"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| payload contem segredo | redaction possivel | evento gravado | substituir valor por `[REDACTED]` |
| payload contem path bloqueado | qualquer | payload removido | gravar evento minimo com redacted=true |
| arquivo diario > 10 MB | write seguinte | rotaciona segmento | criar `YYYY-MM-DD.N.jsonl` |
| index ausente | search | fallback linear | limitar a 7 dias por padrao |
| JSONL truncado | rebuild | indice parcial | reportar skipped_lines |

**Limites numericos explicitos:**
- tamanho maximo de payload por evento: 32 KB
- tamanho maximo de arquivo JSONL antes de segmento: 10 MB
- retencao padrao de eventos: 30 dias
- busca linear padrao: 7 dias
- maximo de eventos retornados: 500
- timeout de search: 2000 ms

---

### Dependencias desta fase

```yaml
- id: DEP-02-1
  tipo: componente_interno
  nome: internal/audit/logger.go
  versao: "atual"
  papel: "compatibilidade e migracao progressiva"
  fallback: "manter audit.log antigo se ledger falhar"
  obrigatoria: false
- id: DEP-02-2
  tipo: arquivo
  nome: ~/.local/share/pocketcli
  versao: "schema atual"
  papel: "armazenar eventos e indice"
  fallback: "usar cache temporario somente na execucao"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] writer JSONL com redaction
- [ ] `pocket ledger search`
- [ ] `pocket ledger rebuild-index`
- [ ] eventos integrados a `ask`, `context`, `ssh`, `exec`, `hosts` e menu
- [ ] todos os testes 02-01 a 02-04 passando

## 3.3 - Fase 03: Insight Collector e Scoring

### O que e

Motor local que transforma eventos, capacidades, contexto, inventario e memoria em insights acionaveis. Ele nao precisa de LLM para gerar os principais sinais: falhas repetidas, dependencia ausente, host instavel, contexto parcial, update pendente, TUI em modo compacto, ou comandos de risco. A IA entra apenas como explicador opcional.

**Responsabilidades:**
- Coletar sinais de ledger, session state, inventory, audit legado, memoria e contexto.
- Gerar `PocketInsight` com severidade, confianca, evidencia e acao recomendada.
- Deduplicar insights repetidos por fingerprint.
- Expor `pocket insights list`, `pocket insights explain` e feed para TUI.
- Marcar insight como resolvido, ignorado ou recorrente.

**Fora do escopo deste componente:**
- Executar automaticamente a acao sugerida.
- Criar conhecimento de wiki.
- Fazer embedding ou busca vetorial.
- Escrever memoria permanente sem comando explicito do usuario.

---

### Testes obrigatorios

```
TESTE 03-01
dado:    tres eventos ssh.probe com timeout para o mesmo host em 10 minutos
quando:  collector roda
entao:   gera insight kind=host_unstable severity=warning com evidence_count=3

TESTE 03-02
dado:    capabilities.has_tmux=false e mode_requested=agent
quando:  collector roda
entao:   gera insight kind=missing_capability com recommended_action="install_tmux_or_use_viewer"

TESTE 03-03  [edge case]
dado:    ledger vazio e session.json valido
quando:  `pocket insights list --json` roda
entao:   retorna lista vazia, summary.total=0 e exit 0

TESTE 03-04  [seguranca]
dado:    evento redigido com payload message contendo `[REDACTED]`
quando:  insight e gerado
entao:   evidence_preview preserva `[REDACTED]` e nao tenta reconstruir valor original
```

---

### Contratos de interface

```yaml
interface:
  id: IF-03-1
  tipo: cli
  nome: pocket insights list

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a text"
      valores_enum: ["text", "json"]
    - campo: scope
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a active"
      valores_enum: ["active", "all", "host", "project"]
    - campo: limit
      tipo: int
      obrigatorio: false
      validacao: "1..100"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketInsightList
      campos:
        - campo: insights
          tipo: array<PocketInsight>
        - campo: summary
          tipo: PocketInsightSummary
    falha:
      valores_possiveis:
        - code: ERR_INSIGHT_LEDGER_UNREADABLE
          quando: "ledger nao pode ser lido"
          acao: "usa session.json e capabilities como fontes parciais"
        - code: ERR_INSIGHT_BAD_SCOPE
          quando: "scope invalido"
          acao: "retorna erro sem gerar insights"

  comportamento_em_falha:
    - condicao: "uma fonte falha"
      acao: "gera insights parciais"
      observavel: "summary.partial=true"
```

```yaml
interface:
  id: IF-03-2
  tipo: function
  nome: ComputeInsights

  entrada:
    - campo: request
      tipo: PocketInsightRequest
      obrigatorio: true
      validacao: "time_window_minutes 1..10080"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketInsightList
      campos:
        - campo: insights
          tipo: array<PocketInsight>
        - campo: summary
          tipo: PocketInsightSummary
    falha:
      valores_possiveis:
        - code: ERR_INSIGHT_SOURCE_TIMEOUT
          quando: "coleta passa do timeout"
          acao: "retorna parcial"
        - code: ERR_INSIGHT_SCHEMA
          quando: "evento nao pode ser normalizado"
          acao: "ignora evento e incrementa skipped_events"

  comportamento_em_falha:
    - condicao: "evento invalido"
      acao: "pular evento"
      observavel: "summary.skipped_events incrementado"
```

---

### Estruturas de dados

```yaml
tipo: PocketInsightRequest
campos:
  - nome: scope
    tipo: enum
    obrigatorio: true
    default: "active"
    limite: "active|all|host|project"
  - nome: host_id
    tipo: string
    obrigatorio: false
    default: ""
    limite: "128 chars"
  - nome: project_path
    tipo: string
    obrigatorio: false
    default: ""
    limite: "1024 chars"
  - nome: time_window_minutes
    tipo: int
    obrigatorio: true
    default: 1440
    limite: "1..10080"
```

```yaml
tipo: PocketInsight
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: "fingerprint deterministico"
    limite: "128 chars"
  - nome: kind
    tipo: enum
    obrigatorio: true
    default: "runtime_notice"
    limite: "missing_capability|host_unstable|context_partial|backend_fallback|update_pending|risky_command|memory_candidate|runtime_notice"
  - nome: severity
    tipo: enum
    obrigatorio: true
    default: "info"
    limite: "info|warning|critical"
  - nome: confidence
    tipo: int
    obrigatorio: true
    default: 50
    limite: "0..100"
  - nome: title
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: summary
    tipo: string
    obrigatorio: true
    default: ""
    limite: "500 chars"
  - nome: evidence
    tipo: array<PocketInsightEvidence>
    obrigatorio: true
    default: "[]"
    limite: "max 10 itens"
  - nome: recommended_action
    tipo: string
    obrigatorio: false
    default: ""
    limite: "120 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "active"
    limite: "active|resolved|ignored"
```

```yaml
tipo: PocketInsightEvidence
campos:
  - nome: source
    tipo: enum
    obrigatorio: true
    default: "ledger"
    limite: "ledger|session|capabilities|inventory|memory|context|audit"
  - nome: ref
    tipo: string
    obrigatorio: true
    default: ""
    limite: "200 chars"
  - nome: preview
    tipo: string
    obrigatorio: true
    default: ""
    limite: "300 chars"
```

```yaml
tipo: PocketInsightList
campos:
  - nome: insights
    tipo: array<PocketInsight>
    obrigatorio: true
    default: "[]"
    limite: "max 100 itens"
  - nome: summary
    tipo: PocketInsightSummary
    obrigatorio: true
    default: "totais zerados"
    limite: "nenhum"
```

```yaml
tipo: PocketInsightSummary
campos:
  - nome: total
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..100"
  - nome: critical
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..100"
  - nome: warning
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..100"
  - nome: partial
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: skipped_events
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..100000"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| 3 timeouts SSH | mesma janela 10 min | host_unstable | severity warning |
| 5 falhas SSH | mesma janela 30 min | host_unstable | severity critical |
| backend local timeout | remote usado | backend_fallback | recomendar revisar local backend |
| contexto parcial | prompt longo | context_partial | recomendar `pocket context --debug` |
| capacidade ausente | acao depende dela | missing_capability | ocultar acao no TUI |
| comando remoto contem risco | sem aprovacao | risky_command | bloquear na fase 07 |

**Limites numericos explicitos:**
- janela padrao: 1440 min
- timeout do collector: 1500 ms
- maximo de eventos analisados por execucao: 5000
- maximo de insights ativos exibidos: 20
- evidence por insight: 10
- minimo de confidence para exibir no TUI: 40

---

### Dependencias desta fase

```yaml
- id: DEP-03-1
  tipo: componente_interno
  nome: Fase 01 Capability Manifest
  versao: "schema 1"
  papel: "fonte de capacidade local"
  fallback: "detectar capacidades inline"
  obrigatoria: true
- id: DEP-03-2
  tipo: componente_interno
  nome: Fase 02 Event Ledger
  versao: "schema 1"
  papel: "fonte principal de eventos"
  fallback: "session.json, audit.log e inventory.json"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `pocket insights list --json`
- [ ] regras iniciais para capability, SSH, backend, contexto e memoria
- [ ] persistencia de estado ignored/resolved
- [ ] todos os testes 03-01 a 03-04 passando

## 3.4 - Fase 04: TUI Action Registry e Command Palette

### O que e

Camada que transforma menus fixos em acoes declarativas. O TUI continua leve e POSIX-friendly, mas passa a renderizar acoes a partir de um registry com requisitos de capacidade, status, labels compactos, hotkeys e comandos. Isso permite que insights aparecam como cards acionaveis sem acoplar tudo ao script do menu.

**Responsabilidades:**
- Definir `PocketAction` para menu, command palette e cards de insight.
- Filtrar acoes por capacidade e modo efetivo.
- Expor `pocket actions --json`.
- Integrar acoes principais: connect, radar, status, hosts, update, insights, doctor, ask, context.
- Renderizar mensagens de degradacao claras no menu atual.

**Fora do escopo deste componente:**
- Reescrever o TUI em Bubble Tea.
- Executar acoes sem passar por SafetyPolicy quando aplicavel.
- Persistir configuracao customizada fora de `profile/`.
- Criar UI grafica.

---

### Testes obrigatorios

```
TESTE 04-01
dado:    capabilities.has_ssh=true e has_tailscale=false
quando:  `pocket actions --json` roda
entao:   inclui acao connect_manual e exclui radar_tailscale

TESTE 04-02
dado:    terminal sem TTY
quando:  `pocket` roda sem argumentos
entao:   nao tenta abrir TUI e imprime help/erro claro conforme modo

TESTE 04-03  [edge case]
dado:    largura de terminal 38
quando:  menu renderiza cards de insight
entao:   nenhum texto passa da largura e a layout compact e usado

TESTE 04-04  [seguranca]
dado:    action command contem shell metacharacters vindos de input externo
quando:  registry valida action
entao:   action e rejeitada com ERR_ACTION_UNSAFE_COMMAND
```

---

### Contratos de interface

```yaml
interface:
  id: IF-04-1
  tipo: cli
  nome: pocket actions

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a text"
      valores_enum: ["text", "json"]
    - campo: include_disabled
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketActionList
      campos:
        - campo: actions
          tipo: array<PocketAction>
        - campo: disabled_count
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_ACTION_CAPABILITIES
          quando: "manifesto nao pode ser carregado"
          acao: "usa detector inline e marca partial=true"
        - code: ERR_ACTION_REGISTRY
          quando: "action invalida no registry"
          acao: "oculta action invalida e retorna warning"

  comportamento_em_falha:
    - condicao: "uma action invalida"
      acao: "nao renderizar action"
      observavel: "warning no modo debug"
```

```yaml
interface:
  id: IF-04-2
  tipo: function
  nome: ResolveActions

  entrada:
    - campo: request
      tipo: PocketActionResolveRequest
      obrigatorio: true
      validacao: "surface e enum valido"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketActionList
      campos:
        - campo: actions
          tipo: array<PocketAction>
        - campo: disabled_count
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_ACTION_BAD_SURFACE
          quando: "surface invalida"
          acao: "retorna erro"
        - code: ERR_ACTION_UNSAFE_COMMAND
          quando: "command nao passa politica"
          acao: "rejeita action"

  comportamento_em_falha:
    - condicao: "capacidade requerida ausente"
      acao: "marcar enabled=false"
      observavel: "disabled_reason preenchido"
```

---

### Estruturas de dados

```yaml
tipo: PocketActionResolveRequest
campos:
  - nome: surface
    tipo: enum
    obrigatorio: true
    default: "menu"
    limite: "menu|palette|insight_card|plain"
  - nome: include_disabled
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: query
    tipo: string
    obrigatorio: false
    default: ""
    limite: "120 chars"
```

```yaml
tipo: PocketAction
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: title
    tipo: string
    obrigatorio: true
    default: ""
    limite: "40 chars"
  - nome: description
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: hotkey
    tipo: string
    obrigatorio: false
    default: ""
    limite: "8 chars"
  - nome: command
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 12 argumentos, 160 chars por argumento"
  - nome: requires
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 12 capacidades"
  - nome: enabled
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
  - nome: disabled_reason
    tipo: string
    obrigatorio: false
    default: ""
    limite: "120 chars"
  - nome: safety_level
    tipo: enum
    obrigatorio: true
    default: "safe"
    limite: "safe|confirm|required_policy"
```

```yaml
tipo: PocketActionList
campos:
  - nome: actions
    tipo: array<PocketAction>
    obrigatorio: true
    default: "[]"
    limite: "max 80 actions"
  - nome: disabled_count
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..80"
  - nome: partial
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| surface=menu | TTY true | render interativo | usar layout detectado |
| surface=plain | qualquer | saida textual | nao usar ANSI obrigatorio |
| action requer has_tailscale | has_tailscale=false | disabled | explicar dependencia |
| safety_level=confirm | TTY false | blocked | exigir flag explicita futura |
| query nao vazia | palette | filtrar por title/description/id | preservar ordem por score |

**Limites numericos explicitos:**
- max actions por surface: 80
- max cards de insight no menu: 5
- render budget por frame shell: 100 ms
- largura minima compacta: 22 colunas
- max hotkeys por surface: 36

---

### Dependencias desta fase

```yaml
- id: DEP-04-1
  tipo: componente_interno
  nome: scripts/pocketcli_menu.sh
  versao: "atual"
  papel: "surface TUI POSIX inicial"
  fallback: "render plain text"
  obrigatoria: true
- id: DEP-04-2
  tipo: componente_interno
  nome: Fase 03 Insight Collector
  versao: "schema 1"
  papel: "cards de insight"
  fallback: "menu sem cards"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket actions --json`
- [ ] menu usando registry para itens principais
- [ ] cards compactos de insight
- [ ] fallback plain para sem TTY
- [ ] todos os testes 04-01 a 04-04 passando

## 3.5 - Fase 05: AI Context Compiler e Backend Policy

### O que e

Evolucao do fluxo `pocket ask` para compilar contexto com base em intencao, capacidades e insights. O componente mantem o backend local/remoto atual, mas adiciona politica explicita para selecionar fontes e evitar prompt inflado. Ele tambem prepara o caminho para MiddlewareAuth como backend remoto opcional, sem tornar isso obrigatorio.

**Responsabilidades:**
- Produzir `PocketCompiledContext` com secoes fixas, limites e provenance.
- Escolher fontes: projeto, git, memoria, insights, host, capabilities e anexos seguros.
- Expor `pocket context --json` e `pocket ask --explain-context`.
- Respeitar backend policy: local primeiro, remoto fallback, middleware opcional.
- Evitar vazamento de prompt interno, memoria fenced ou segredo.

**Fora do escopo deste componente:**
- Chamar PocketWiki.
- Implementar provider OAuth.
- Fazer streaming multi-turn completo.
- Persistir resposta inteira no ledger.

---

### Testes obrigatorios

```
TESTE 05-01
dado:    projeto git com README, diff e dois insights ativos
quando:  `pocket context --json` roda
entao:   retorna compiled_context com secoes project, git, insights e token_estimate <= 4000

TESTE 05-02
dado:    backend local timeout e backend remoto disponivel
quando:  `pocket ask --mode auto "x"` roda
entao:   usa remote, registra backend_fallback no ledger e notifica o usuario

TESTE 05-03  [edge case]
dado:    user_input com 10000 caracteres
quando:  contexto e compilado
entao:   user_input e truncado por limite, contexto informa truncated=true e comando nao panica

TESTE 05-04  [seguranca]
dado:    attachment apontando para `.env.local`
quando:  `pocket context --attachment .env.local` roda
entao:   retorna ERR_CONTEXT_ATTACHMENT_BLOCKED e nao le o arquivo
```

---

### Contratos de interface

```yaml
interface:
  id: IF-05-1
  tipo: cli
  nome: pocket context

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a text"
      valores_enum: ["text", "json"]
    - campo: host
      tipo: string
      obrigatorio: false
      validacao: "max 128 chars"
      valores_enum: []
    - campo: attachment
      tipo: array<string>
      obrigatorio: false
      validacao: "cada path precisa passar SensitivePathPolicy"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketCompiledContext
      campos:
        - campo: sections
          tipo: array<PocketContextSection>
        - campo: token_estimate
          tipo: int
        - campo: truncated
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_CONTEXT_CWD
          quando: "cwd nao e diretorio"
          acao: "retorna erro"
        - code: ERR_CONTEXT_ATTACHMENT_BLOCKED
          quando: "attachment sensivel"
          acao: "bloqueia leitura"
        - code: ERR_CONTEXT_TIMEOUT
          quando: "coleta passa do timeout"
          acao: "retorna contexto parcial se houver dados seguros"

  comportamento_em_falha:
    - condicao: "uma fonte falha"
      acao: "omitir fonte e registrar notice"
      observavel: "compiled_context.partial=true"
```

```yaml
interface:
  id: IF-05-2
  tipo: function
  nome: ResolveBackendPolicy

  entrada:
    - campo: request
      tipo: PocketBackendPolicyRequest
      obrigatorio: true
      validacao: "mode em local|auto|remote"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketBackendDecision
      campos:
        - campo: selected_backend
          tipo: enum
        - campo: fallback_occurred
          tipo: bool
        - campo: reason
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_BACKEND_INVALID_MODE
          quando: "modo invalido"
          acao: "retorna erro"
        - code: ERR_BACKEND_NONE
          quando: "nenhum backend disponivel"
          acao: "retorna contexto compilado para uso manual"

  comportamento_em_falha:
    - condicao: "backend opcional ausente"
      acao: "testa proximo backend permitido"
      observavel: "fallback_occurred=true"
```

---

### Estruturas de dados

```yaml
tipo: PocketCompiledContext
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: "valor fixo 1"
  - nome: request_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: sections
    tipo: array<PocketContextSection>
    obrigatorio: true
    default: "[]"
    limite: "max 16 secoes"
  - nome: token_estimate
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..4000"
  - nome: truncated
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: partial
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketContextSection
campos:
  - nome: name
    tipo: enum
    obrigatorio: true
    default: "project"
    limite: "system|request|project|git|memory|insights|host|capabilities|attachments|notices"
  - nome: priority
    tipo: int
    obrigatorio: true
    default: 50
    limite: "0..100"
  - nome: content
    tipo: string
    obrigatorio: true
    default: ""
    limite: "max 16000 chars antes de truncamento global"
  - nome: provenance
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 20 refs"
```

```yaml
tipo: PocketBackendPolicyRequest
campos:
  - nome: mode
    tipo: enum
    obrigatorio: true
    default: "auto"
    limite: "local|auto|remote"
  - nome: allow_middleware
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: local_timeout_ms
    tipo: int
    obrigatorio: true
    default: 8000
    limite: "100..60000"
  - nome: remote_timeout_ms
    tipo: int
    obrigatorio: true
    default: 15000
    limite: "100..120000"
```

```yaml
tipo: PocketBackendDecision
campos:
  - nome: selected_backend
    tipo: enum
    obrigatorio: true
    default: "none"
    limite: "local|remote|middleware|none"
  - nome: fallback_occurred
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: reason
    tipo: string
    obrigatorio: true
    default: ""
    limite: "200 chars"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| mode=local | local indisponivel | selected_backend=none | nao usar fallback |
| mode=auto | local disponivel | selected_backend=local | nao chamar remoto |
| mode=auto | local timeout | tentar remote | registrar fallback |
| allow_middleware=true | middleware health ok | backend middleware elegivel | usar conforme prioridade configurada |
| attachment bloqueado | qualquer | erro | nao compilar attachment |
| token_estimate > 4000 | qualquer | truncated=true | remover secoes por menor prioridade |

**Limites numericos explicitos:**
- max context tokens: 4000
- max output tokens default: 1200
- local timeout: 8000 ms
- remote timeout: 15000 ms
- middleware timeout opcional: 30000 ms
- max attachments: 8
- max attachment size: 128 KB por arquivo

---

### Dependencias desta fase

```yaml
- id: DEP-05-1
  tipo: componente_interno
  nome: internal/contextcollector
  versao: "atual"
  papel: "coleta base do projeto"
  fallback: "contexto minimo com cwd e sistema"
  obrigatoria: true
- id: DEP-05-2
  tipo: componente_interno
  nome: internal/router
  versao: "atual"
  papel: "politica local/remote inicial"
  fallback: "backend none com contexto impresso"
  obrigatoria: true
- id: DEP-05-3
  tipo: servico_externo
  nome: middlewareAuth
  versao: "llm_* MCP/HTTP"
  papel: "backend remoto opcional"
  fallback: "usar backend remoto atual ou none"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket context --json`
- [ ] `pocket ask --explain-context`
- [ ] secoes de insights e capabilities no prompt
- [ ] bloqueio de attachments sensiveis
- [ ] todos os testes 05-01 a 05-04 passando

## 3.6 - Fase 06: Host/Fleet Execution Insights

### O que e

Camada que melhora execucao via SSH/Tailscale com base em historico, inventario e politicas. Ela transforma hosts em entidades observaveis: disponibilidade, ultimo sucesso, falhas recorrentes, tags, origem, aprovacao e plano de execucao. O PocketCli ganha parte do valor de ambientes SSH do Hermes, mas focado no seu proprio modelo leve.

**Responsabilidades:**
- Normalizar inventario de hosts salvos, Tailscale e seeds.
- Registrar probes e execucoes no ledger.
- Gerar insights por host e por grupo.
- Expor `pocket hosts --json`, `pocket fleet plan` e `pocket fleet exec`.
- Aplicar limites de concorrencia e timeout por host.

**Fora do escopo deste componente:**
- Provisionar hosts automaticamente.
- Sincronizar arquivos em massa como ambiente remoto completo.
- Rodar comandos destrutivos sem SafetyPolicy.
- Substituir Tailscale SSH por outro transporte.

---

### Testes obrigatorios

```
TESTE 06-01
dado:    inventory.json com dois hosts online e um offline
quando:  `pocket hosts --json` roda
entao:   retorna tres hosts com status normalizado e online_count=2

TESTE 06-02
dado:    selector que nao encontra host
quando:  `pocket fleet plan --selector tag:db -- echo ok` roda
entao:   retorna ERR_FLEET_EMPTY_SELECTION e nao executa comando

TESTE 06-03  [edge case]
dado:    50 hosts selecionados e max_parallel=4
quando:  plano e executado
entao:   nunca ha mais de 4 execucoes simultaneas

TESTE 06-04  [seguranca]
dado:    comando remoto `rm -rf /`
quando:  `pocket fleet exec` e chamado sem aprovacao
entao:   retorna ERR_SAFETY_APPROVAL_REQUIRED e nenhum host recebe o comando
```

---

### Contratos de interface

```yaml
interface:
  id: IF-06-1
  tipo: cli
  nome: pocket fleet plan

  entrada:
    - campo: selector
      tipo: string
      obrigatorio: true
      validacao: "max 200 chars, sem newline"
      valores_enum: []
    - campo: command
      tipo: array<string>
      obrigatorio: true
      validacao: "min 1 arg, max 32 args"
      valores_enum: []
    - campo: max_parallel
      tipo: int
      obrigatorio: false
      validacao: "1..16"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketFleetPlan
      campos:
        - campo: plan_id
          tipo: string
        - campo: targets
          tipo: array<PocketFleetTarget>
        - campo: requires_approval
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_FLEET_EMPTY_SELECTION
          quando: "selector nao encontra hosts"
          acao: "nao executar"
        - code: ERR_FLEET_BAD_SELECTOR
          quando: "selector invalido"
          acao: "retornar erro"
        - code: ERR_FLEET_COMMAND_UNSAFE
          quando: "comando bloqueado por politica"
          acao: "marcar requires_approval=true ou bloquear"

  comportamento_em_falha:
    - condicao: "host sem aprovacao"
      acao: "remover do plano executavel"
      observavel: "target.status=approval_required"
```

```yaml
interface:
  id: IF-06-2
  tipo: cli
  nome: pocket fleet exec

  entrada:
    - campo: plan_id
      tipo: string
      obrigatorio: false
      validacao: "UUID ou vazio quando selector+command informados"
      valores_enum: []
    - campo: approval_token
      tipo: string
      obrigatorio: false
      validacao: "token emitido pela fase 07"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketFleetResult
      campos:
        - campo: run_id
          tipo: string
        - campo: results
          tipo: array<PocketHostRunResult>
    falha:
      valores_possiveis:
        - code: ERR_FLEET_PLAN_NOT_FOUND
          quando: "plan_id inexistente"
          acao: "retornar erro"
        - code: ERR_SAFETY_APPROVAL_REQUIRED
          quando: "plano exige aprovacao"
          acao: "nao executar"
        - code: ERR_FLEET_TIMEOUT
          quando: "execucao excede timeout global"
          acao: "interromper pendentes e reportar parcial"

  comportamento_em_falha:
    - condicao: "um host falha"
      acao: "continuar demais hosts dentro da politica"
      observavel: "result.status=failed para aquele host"
```

---

### Estruturas de dados

```yaml
tipo: PocketFleetPlan
campos:
  - nome: plan_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: selector
    tipo: string
    obrigatorio: true
    default: ""
    limite: "200 chars"
  - nome: command
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 32 args"
  - nome: targets
    tipo: array<PocketFleetTarget>
    obrigatorio: true
    default: "[]"
    limite: "max 200 targets"
  - nome: max_parallel
    tipo: int
    obrigatorio: true
    default: 4
    limite: "1..16"
  - nome: requires_approval
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketFleetTarget
campos:
  - nome: host_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "128 chars"
  - nome: hostname
    tipo: string
    obrigatorio: true
    default: ""
    limite: "128 chars"
  - nome: address
    tipo: string
    obrigatorio: false
    default: ""
    limite: "128 chars"
  - nome: source
    tipo: enum
    obrigatorio: true
    default: "saved"
    limite: "saved|tailscale|seed|manual"
  - nome: approval_status
    tipo: enum
    obrigatorio: true
    default: "unknown"
    limite: "approved|approval_required|denied|unknown"
```

```yaml
tipo: PocketFleetResult
campos:
  - nome: run_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: plan_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "36 chars"
  - nome: results
    tipo: array<PocketHostRunResult>
    obrigatorio: true
    default: "[]"
    limite: "max 200 results"
```

```yaml
tipo: PocketHostRunResult
campos:
  - nome: host_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "128 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "pending"
    limite: "pending|running|ok|failed|timeout|denied"
  - nome: exit_code
    tipo: int
    obrigatorio: false
    default: 0
    limite: "0..255"
  - nome: duration_ms
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..86400000"
  - nome: output_preview
    tipo: string
    obrigatorio: false
    default: ""
    limite: "4000 chars"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| selector vazio | qualquer | erro | nao selecionar todos implicitamente |
| host offline | command read-only | target warning | manter no plano se usuario pediu |
| host offline | command mutating | target denied | exigir aprovacao especifica |
| max_parallel omitido | targets > 1 | max_parallel=4 | limitar concorrencia |
| comando risco alto | qualquer | requires_approval=true | fase 07 decide |

**Limites numericos explicitos:**
- max targets por plano: 200
- max_parallel default: 4
- max_parallel absoluto: 16
- timeout por probe: 5000 ms
- timeout por comando remoto default: 60000 ms
- output_preview por host: 4000 chars

---

### Dependencias desta fase

```yaml
- id: DEP-06-1
  tipo: componente_interno
  nome: scripts/inventory
  versao: "schema atual"
  papel: "fonte de hosts e selectors"
  fallback: "hosts salvos em ~/.pocketcli/hosts"
  obrigatoria: true
- id: DEP-06-2
  tipo: componente_interno
  nome: internal/ssh
  versao: "atual"
  papel: "execucao ssh"
  fallback: "comando shell ssh direto com timeout"
  obrigatoria: true
- id: DEP-06-3
  tipo: servico_externo
  nome: tailscale
  versao: "qualquer"
  papel: "descoberta e status da tailnet"
  fallback: "modo manual/saved hosts"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket hosts --json`
- [ ] `pocket fleet plan`
- [ ] `pocket fleet exec` com concorrencia limitada
- [ ] eventos por host no ledger
- [ ] todos os testes 06-01 a 06-04 passando

## 3.7 - Fase 07: SafetyPolicy, Run Envelope e Aprovacoes

### O que e

Camada de seguranca operacional antes de qualquer acao com risco: exec remoto, update, copy, fleet, escrita de arquivos e comandos com mutacao. A ideia vem dos guardrails do Hermes, mas adaptada ao PocketCli: simples, auditavel, previsivel e sem tentar ser sandbox perfeito.

**Responsabilidades:**
- Classificar comandos em `safe`, `confirm`, `blocked`.
- Criar `RunEnvelope` antes de execucao remota/local sensivel.
- Emitir approval token de curta duracao para TTY interativo.
- Bloquear paths e conteudos sensiveis.
- Registrar deny/approval no ledger.

**Fora do escopo deste componente:**
- Ser fronteira de seguranca contra usuario root.
- Impedir tudo que um shell direto conseguiria fazer.
- Gerenciar OAuth ou tokens de LLM.
- Aprovar subagents externos.

---

### Testes obrigatorios

```
TESTE 07-01
dado:    comando `uptime`
quando:  SafetyPolicy avalia
entao:   retorna classification=safe e approval_required=false

TESTE 07-02
dado:    comando `sudo reboot`
quando:  SafetyPolicy avalia sem TTY
entao:   retorna ERR_SAFETY_APPROVAL_REQUIRED e nao executa

TESTE 07-03  [edge case]
dado:    comando com newline e 5000 caracteres
quando:  RunEnvelope e criado
entao:   retorna ERR_SAFETY_COMMAND_INVALID por tamanho/formato

TESTE 07-04  [seguranca]
dado:    path destino `~/.ssh/authorized_keys`
quando:  acao copy/write e avaliada
entao:   retorna classification=blocked e code ERR_SAFETY_PATH_BLOCKED
```

---

### Contratos de interface

```yaml
interface:
  id: IF-07-1
  tipo: function
  nome: EvaluateSafety

  entrada:
    - campo: request
      tipo: PocketSafetyRequest
      obrigatorio: true
      validacao: "action e enum valido"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketSafetyDecision
      campos:
        - campo: classification
          tipo: enum
        - campo: approval_required
          tipo: bool
        - campo: reasons
          tipo: array<string>
    falha:
      valores_possiveis:
        - code: ERR_SAFETY_COMMAND_INVALID
          quando: "comando vazio, longo demais ou com newline proibido"
          acao: "bloquear"
        - code: ERR_SAFETY_PATH_BLOCKED
          quando: "path sensivel"
          acao: "bloquear"
        - code: ERR_SAFETY_POLICY_UNAVAILABLE
          quando: "policy nao pode ser carregada"
          acao: "usar policy padrao restritiva"

  comportamento_em_falha:
    - condicao: "policy custom invalida"
      acao: "ignorar custom e usar default"
      observavel: "decision.policy_status=fallback_default"
```

```yaml
interface:
  id: IF-07-2
  tipo: cli
  nome: pocket approve

  entrada:
    - campo: envelope_id
      tipo: string
      obrigatorio: true
      validacao: "UUID v4"
      valores_enum: []
    - campo: duration_seconds
      tipo: int
      obrigatorio: false
      validacao: "30..900"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketApprovalToken
      campos:
        - campo: approval_token
          tipo: string
        - campo: expires_at
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_APPROVAL_NOT_FOUND
          quando: "envelope nao existe"
          acao: "retornar erro"
        - code: ERR_APPROVAL_NOT_INTERACTIVE
          quando: "sem TTY para confirmar"
          acao: "recusar"
        - code: ERR_APPROVAL_BLOCKED
          quando: "classification=blocked"
          acao: "recusar"

  comportamento_em_falha:
    - condicao: "token expirado"
      acao: "recusar execucao"
      observavel: "ERR_APPROVAL_EXPIRED"
```

---

### Estruturas de dados

```yaml
tipo: PocketSafetyRequest
campos:
  - nome: action
    tipo: enum
    obrigatorio: true
    default: "exec"
    limite: "exec|fleet|copy|update|write"
  - nome: command
    tipo: array<string>
    obrigatorio: false
    default: "[]"
    limite: "max 32 args, 256 chars por arg"
  - nome: target_path
    tipo: string
    obrigatorio: false
    default: ""
    limite: "1024 chars"
  - nome: host_count
    tipo: int
    obrigatorio: true
    default: 1
    limite: "0..200"
  - nome: interactive
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketSafetyDecision
campos:
  - nome: classification
    tipo: enum
    obrigatorio: true
    default: "confirm"
    limite: "safe|confirm|blocked"
  - nome: approval_required
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
  - nome: reasons
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 16 reasons, 120 chars cada"
  - nome: policy_status
    tipo: enum
    obrigatorio: true
    default: "default"
    limite: "default|custom|fallback_default"
```

```yaml
tipo: PocketApprovalToken
campos:
  - nome: approval_token
    tipo: string
    obrigatorio: true
    default: "token aleatorio 32 bytes hex"
    limite: "64 chars"
  - nome: envelope_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "36 chars"
  - nome: expires_at
    tipo: string
    obrigatorio: true
    default: "agora + 300s"
    limite: "RFC3339 UTC"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| comando read-only conhecido | host_count <= 20 | safe | executar sem prompt |
| comando contem sudo | qualquer | confirm | exigir aprovacao |
| comando contem rm -rf | path raiz ou home | blocked | negar |
| target_path sensivel | qualquer | blocked | negar |
| sem TTY | approval_required=true | erro | nao solicitar input |
| policy custom invalida | qualquer | fallback default | registrar warning |

**Limites numericos explicitos:**
- validade default approval token: 300 s
- validade minima token: 30 s
- validade maxima token: 900 s
- max args de comando: 32
- max chars por arg: 256
- max hosts safe sem confirmacao: 20

---

### Dependencias desta fase

```yaml
- id: DEP-07-1
  tipo: componente_interno
  nome: Fase 02 Event Ledger
  versao: "schema 1"
  papel: "auditar deny e approve"
  fallback: "stderr warning"
  obrigatoria: true
- id: DEP-07-2
  tipo: arquivo
  nome: ~/.config/pocketcli/ssh-policy.json
  versao: "schema atual"
  papel: "politica local customizavel"
  fallback: "politica default embutida"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] policy default
- [ ] `pocket approve`
- [ ] RunEnvelope para `exec` e `fleet`
- [ ] denylist de paths sensiveis
- [ ] todos os testes 07-01 a 07-04 passando

## 3.8 - Fase 08: Doctor, Eval e Gates de Release

### O que e

Conjunto de verificacoes locais para garantir que o PocketCli standalone continua saudavel depois das novas camadas. Ele une doctor operacional, suite de smoke/eval para insights e gates de release. A meta e impedir regressao em iSH/viewer, update profile-safe e fluxos Go.

**Responsabilidades:**
- Expor `pocket doctor --json`.
- Expor `pocket eval insights`.
- Validar schemas de capabilities, ledger, insights, actions, context e safety.
- Integrar com `scripts/run_local_ci.sh`.
- Documentar prerequisitos e variaveis de ambiente para testes locais.

**Fora do escopo deste componente:**
- Rodar testes de PocketWiki.
- Rodar testes do MiddlewareAuth.
- Publicar release no GitHub.
- Criar ambiente Docker obrigatorio.

---

### Testes obrigatorios

```
TESTE 08-01
dado:    instalacao local com Go disponivel
quando:  `pocket doctor --json` roda
entao:   retorna checks com status ok|warning|error e exit 0 quando nao ha error

TESTE 08-02
dado:    schema de ledger invalido em fixture
quando:  `pocket eval insights --fixtures tests/fixtures/insights`
entao:   retorna ERR_EVAL_FIXTURE_INVALID e lista o fixture quebrado

TESTE 08-03  [edge case]
dado:    ambiente sem go
quando:  doctor roda
entao:   marca go_toolchain=warning e nao falha checks runtime viewer

TESTE 08-04  [seguranca]
dado:    fixture contendo `.env` com segredo
quando:  eval carrega fixtures
entao:   fixture e recusado com ERR_EVAL_SECRET_FIXTURE
```

---

### Contratos de interface

```yaml
interface:
  id: IF-08-1
  tipo: cli
  nome: pocket doctor

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a text"
      valores_enum: ["text", "json"]
    - campo: strict
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketDoctorReport
      campos:
        - campo: checks
          tipo: array<PocketDoctorCheck>
        - campo: status
          tipo: enum
    falha:
      valores_possiveis:
        - code: ERR_DOCTOR_SCHEMA
          quando: "schema local invalido"
          acao: "retornar erro em strict"
        - code: ERR_DOCTOR_RUNTIME
          quando: "falha interna do doctor"
          acao: "retornar erro"

  comportamento_em_falha:
    - condicao: "check opcional falha"
      acao: "status warning"
      observavel: "exit 0 sem strict, exit 1 com strict"
```

```yaml
interface:
  id: IF-08-2
  tipo: cli
  nome: pocket eval insights

  entrada:
    - campo: fixtures
      tipo: string
      obrigatorio: true
      validacao: "diretorio dentro do repo ou tests/fixtures"
      valores_enum: []
    - campo: update_snapshots
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketEvalReport
      campos:
        - campo: passed
          tipo: int
        - campo: failed
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_EVAL_FIXTURE_INVALID
          quando: "fixture malformado"
          acao: "falhar eval"
        - code: ERR_EVAL_SECRET_FIXTURE
          quando: "fixture contem segredo"
          acao: "bloquear leitura"

  comportamento_em_falha:
    - condicao: "snapshot diverge"
      acao: "falhar teste"
      observavel: "diff textual no stdout"
```

---

### Estruturas de dados

```yaml
tipo: PocketDoctorReport
campos:
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|warning|error"
  - nome: checks
    tipo: array<PocketDoctorCheck>
    obrigatorio: true
    default: "[]"
    limite: "max 100 checks"
  - nome: generated_at
    tipo: string
    obrigatorio: true
    default: "agora UTC"
    limite: "RFC3339 UTC"
```

```yaml
tipo: PocketDoctorCheck
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|warning|error"
  - nome: message
    tipo: string
    obrigatorio: true
    default: ""
    limite: "240 chars"
  - nome: remediation
    tipo: string
    obrigatorio: false
    default: ""
    limite: "240 chars"
```

```yaml
tipo: PocketEvalReport
campos:
  - nome: passed
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..10000"
  - nome: failed
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..10000"
  - nome: duration_ms
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..3600000"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| check error | strict=true | exit 1 | imprimir report |
| check warning | strict=false | exit 0 | imprimir warning |
| fixture contem segredo | qualquer | fail | bloquear eval |
| go ausente | viewer checks ok | warning | nao impedir uso viewer |
| schema invalido | qualquer | error | sugerir rebuild/reset seguro |

**Limites numericos explicitos:**
- timeout total doctor: 5000 ms
- timeout eval fixtures: 30000 ms
- max fixtures por rodada: 1000
- max tamanho fixture: 256 KB
- max checks: 100

---

### Dependencias desta fase

```yaml
- id: DEP-08-1
  tipo: componente_interno
  nome: scripts/run_local_ci.sh
  versao: "atual"
  papel: "gate local documentado"
  fallback: "rodar comandos individuais documentados"
  obrigatoria: true
- id: DEP-08-2
  tipo: biblioteca
  nome: Go toolchain
  versao: "1.22"
  papel: "go test e build"
  fallback: "doctor warning para viewer sem Go"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket doctor --json`
- [ ] `pocket eval insights`
- [ ] fixtures sem segredo
- [ ] README/testes atualizado com prerequisitos
- [ ] todos os testes 08-01 a 08-04 passando

## 4. Cenarios de Falha Transversais

### Entrada invalida / malformada

Todo comando novo deve validar flags antes de coletar contexto ou abrir arquivo. JSON invalido em ledger, capabilities, actions ou fixtures deve retornar erro com code fechado. Quando possivel, componentes devem ignorar apenas o registro invalido e continuar com `partial=true`.

### Dependencia indisponivel ou lenta

Ferramentas opcionais ausentes viram capabilities false, nunca panic. Tailscale ausente desativa radar/fleet por tailnet, mas SSH manual continua. MiddlewareAuth ausente nao afeta standalone. Timeout de probes deve gerar insight e nao travar TUI.

### Estado inconsistente entre fontes de dados

Se `session.json`, `inventory.json`, ledger e capabilities discordarem, o dado mais recente vence quando timestamp existir. Sem timestamp confiavel, capabilities runtime vence para capacidade local; inventory vence para lista de hosts; ledger vence para historico.

### Concorrencia / race condition

Ledger e indices devem usar escrita atomica e lock por arquivo quando houver update de indice. JSONL append deve ser tolerante a linhas truncadas. Fleet deve limitar concorrencia e nao compartilhar buffer mutavel entre hosts.

### Exaustao de recurso

Se disco estiver cheio, o comando principal continua quando seguro e emite warning. Contexto, output preview, ledger payload e fixtures tem limites fixos. TUI nao deve renderizar listas sem limite.

### Seguranca - entrada controlada por ator externo

Selectors, hostnames, attachments e comandos remotos sao entrada nao confiavel. Paths sensiveis sao bloqueados. Commands devem ser representados como array de argumentos internamente; shell string so no limite do transporte SSH, com quoting centralizado.

### Timeout em operacao normalmente rapida

Deteccao de capacidade, coleta de contexto, search e probes tem timeout. Em timeout, retorna estado parcial observavel, registra evento e gera insight quando recorrente.

## 5. Decisoes e Restricoes

| ID | Decisao | Motivo | Reversivel |
|----|---------|--------|------------|
| DEC-01 | PocketCli deve continuar standalone | Uso principal e iPad/iSH/SSH mesmo sem apps auxiliares | nao |
| DEC-02 | JSONL append-only e fonte primaria do ledger | Simples, portavel e rebuildable em ambientes minimos | sim |
| DEC-03 | SQLite nao e obrigatorio no PocketCli | Evita dependencia pesada/cgo e problemas em iSH | sim |
| DEC-04 | PocketWiki nao participa do RFC PocketCli standalone | Separacao clara de responsabilidade | nao |
| DEC-05 | MiddlewareAuth e backend opcional | Evita acoplamento de auth ao CLI | nao |
| DEC-06 | SafetyPolicy e guardrail operacional, nao sandbox | Usuario tem shell direto; prometer isolamento seria falso | nao |
| DEC-07 | Personalizacao versionada continua centralizada em `profile/` | Preserva regra do projeto e update seguro | nao |
| DEC-08 | TUI shell atual evolui por registry antes de reescrita | Menor risco para iSH e ambientes minimos | sim |

**Alternativas descartadas:**
- Copiar o Hermes com SQLite + daemon + toolsets completos: descartada porque aumenta dependencia e foge do escopo SSH-first do PocketCli.
- Fazer PocketWiki obrigatorio para contexto: descartada porque quebra autonomia do CLI.
- Fazer MiddlewareAuth obrigatorio para LLM: descartada porque o CLI ja tem fallback local/remoto e deve operar offline.
- Usar `.env` para configurar novos componentes: descartada porque o projeto ja bloqueia `.env*` e isso piora seguranca/update.
- Autoaprovar comandos remotos destrutivos em modo agent: descartada porque o ganho de velocidade nao compensa o risco operacional.
