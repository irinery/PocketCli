# RFC — Evolucao Pocket Stack inspirada no Hermes Agent

**Projeto:** PocketCli, PocketWiki, middlewareAuth
**Data:** 2026-06-08
**Status:** COMPLETO
**Fases:** 10 arquivos a gerar
**Gerado por:** Codex GPT-5, sessao local PocketCli

## 1. Visao Geral

### 1.1 Problema

PocketCli, PocketWiki e middlewareAuth ja cobrem partes fortes do espaco que o Hermes Agent ocupa, mas hoje elas ainda funcionam como tres pecas acopladas por convencao humana, nao como um runtime composto com contrato unico.

O Hermes junta em um unico produto estes blocos principais: CLI/TUI de agente, selecao de modelo/provedor, gateway de mensagens, memoria persistente pesquisavel, skills, automacoes agendadas, toolsets, MCP, execucao local/remota/sandboxed, delegacao, aprovacoes e observabilidade. O trio local tem boa parte dos fundamentos, mas em lugares separados:

| Area | Hermes Agent | Pocket stack hoje | Lacuna pratica | Direcao recomendada |
|------|--------------|-------------------|----------------|---------------------|
| Entrada principal | CLI/TUI com historico, slash commands, interrupcao e streaming | PocketCli tem `pocket`, menu, `ask`, `context`, `recall`, `hosts`, `ssh`, `exec`; TUI Go em evolucao | O fluxo de agente ainda nao e o centro operacional do ecossistema | Fazer do PocketCli o runtime/operador comum |
| Memoria | Memoria por provider, busca em sessoes com FTS5, contexto injetado por turno, skills persistentes | PocketCli tem memoria JSONL por escopo; PocketWiki indexa wiki e ja tem RFC de knowledge store | Memoria operacional e wiki nao compartilham contrato de citacao, sessao e recall | Criar ledger de sessoes e provider PocketWiki para contexto citavel |
| Auth/modelos | Troca de provedor/modelo sem mudar codigo | middlewareAuth ja expoe `llm_*`, OAuth, refresh, MCP e contrato generico | PocketCli ainda usa backend por comando/env simples | Integrar PocketCli direto ao contrato `llm_*` do middlewareAuth |
| Toolsets | Toolsets compostos por plataforma/cenario, com tools dinamicas e MCP | PocketCli tem registry interno de tools de contexto; middlewareAuth tem MCP; PocketWiki tem skill | Falta registry unico e politica de habilitacao por modo Viewer/Agent | Criar toolset manifest do Pocket Stack |
| Gateway | Telegram, Discord, Slack, WhatsApp, Signal, email etc. em processo unico | PocketWiki serve HTTP/LAN/Tailscale; middlewareAuth serve HTTP/MCP local | Nao ha bridge de conversas/eventos entre apps | Criar MCP/HTTP bridge pequeno, sem copiar gateway multi-plataforma inteiro |
| Scheduler | Cron interno com entrega por plataforma, saida auditavel e toolsets limitados | PocketCli nao tem scheduler formal | Rotinas de infra ainda dependem de cron externo/manual | Implementar scheduler Agent-only com saida local/PocketWiki/webhook |
| Execucao remota | Backends local, Docker, SSH, Singularity, Modal, Daytona | PocketCli e SSH/Tailscale-first, com fleet e inventory em shell/Go | Execucao remota nao tem envelope auditavel e politica unificada | Formalizar run envelopes, concorrencia e aprovacoes por host |
| Delegacao | Subagents isolados, concorrencia e bloqueios de tools | PocketCli pode operar hosts, mas nao tem delegacao de tarefas de agente | Paralelismo ainda e operacional, nao cognitivo/contratual | Criar workers por host/tarefa com toolsets restritos |
| Skills | Skills com frontmatter, plataforma, arquivos auxiliares e comandos | PocketWiki tem `SKILL/`; PocketCli tem pasta `skills/` ainda sem contrato claro | Falta ciclo de vida de skill entre wiki, CLI e runtime | Criar skill store em `profile/skills` e skill adoption manual |
| Seguranca | Redacao de segredo, denylist de arquivos, aprovacoes, guardrails de loop, limites de cron | Regras existem espalhadas: PocketCli bloqueia contexto sensivel, middlewareAuth redige e protege token, PocketWiki tem SensitiveFilePolicy | Politica nao e compartilhada nem testavel de ponta a ponta | Criar SafetyPolicy comum e gates por tool/scheduler/fleet/contexto |
| Observabilidade | state.db, logs, doctor/status, custos, historico, eventos | PocketCli tem audit log simples; middlewareAuth tem auditoria/metrics; PocketWiki tem dashboard saude | Falta visao unificada do ecossistema | `pocket doctor --ecosystem` e eventos normalizados |

O ponto-chave: sozinho, nenhum dos tres projetos locais deve tentar virar um clone do Hermes. Juntos, eles podem formar um sistema mais pragmatico para o teu uso: leve no iSH, SSH/Tailscale-first, wiki local como memoria inspecionavel, auth centralizado em Go e sem daemon no Viewer.

Fontes externas usadas como referencia de comportamento, nao como dependencia: repositorio publico `https://github.com/nousresearch/hermes-agent`, README atual em 2026-06-08 e clone local temporario em `/tmp/hermes-agent` para inspecao de `toolsets.py`, `hermes_state.py`, `cron/`, `mcp_serve.py`, `gateway/`, `agent/memory_*`, `tools/environments/`, `tools/delegate_tool.py`, `tools/code_execution_tool.py`, `agent/file_safety.py`, `agent/tool_guardrails.py` e `agent/redact.py`.

### 1.2 Solucao

Criar uma camada Pocket Stack que use PocketCli como runtime operacional, PocketWiki como memoria/contexto citavel e middlewareAuth como broker de autenticacao/modelos, com contratos comuns para capacidades, sessoes, contexto, tools, scheduler, fleet, skills, seguranca e diagnostico.

### 1.3 Fora de escopo

- Copiar o Hermes Agent ou depender dele em runtime.
- Transformar o Viewer/iSH em daemon, gateway ou servidor residente.
- Implementar Telegram/Discord/Slack/WhatsApp/Signal completos nesta entrega.
- Introduzir Python obrigatorio no PocketCli.
- Trocar SSH/Tailscale por SDK pesado de orquestracao.
- Trocar PocketWiki por banco vetorial externo obrigatorio.
- Armazenar access token, refresh token, API key ou segredo bruto no PocketCli.
- Permitir escrita autonoma em `profile/`, `knowledge/`, scheduler ou politicas sem aprovacao humana.
- Implementar Modal, Daytona, Singularity ou Docker como requisito do MVP.
- Resolver sincronizacao multiusuario em tempo real.
- Fazer migracao automatica destrutiva de memoria, wiki ou configs existentes.

## 2. Mapa de Fases

| Fase | Arquivo | Nome do componente | Depende de | Paralelizavel com |
|------|---------|-------------------|------------|-------------------|
| 01 | `01_ecosystem_manifest.md` | Manifesto do Pocket Stack e Registry de Capacidades | — | — |
| 02 | `02_middleware_llm_provider.md` | Integracao middlewareAuth como Provider Broker | 01 | 03 |
| 03 | `03_session_ledger_search.md` | Session Ledger, Historico e Busca Rebuildable | 01 | 02 |
| 04 | `04_pocketwiki_context_provider.md` | PocketWiki Context Provider e Context Compiler | 01, 03 | 05 |
| 05 | `05_toolsets_mcp_bridge.md` | Toolsets Compostos e MCP Bridge Pocket | 01, 02 | 04 |
| 06 | `06_agent_scheduler.md` | Scheduler Agent-only com Entrega Auditavel | 01, 03, 05 | 08 |
| 07 | `07_fleet_delegation_runtime.md` | Fleet Runtime, Run Envelopes e Delegacao Restrita | 01, 03, 05 | 06 |
| 08 | `08_skill_lifecycle.md` | Skills, Procedural Memory e Adocao Manual | 01, 04, 05 | 06 |
| 09 | `09_safety_approval_guardrails.md` | SafetyPolicy, Aprovacoes e Guardrails | 01, 02, 05, 06, 07, 08 | — |
| 10 | `10_doctor_eval_release_gates.md` | Doctor, Avaliacao e Gates de Release | 01-09 | — |

### Convencoes globais

- PocketCli continua sendo Go + POSIX sh, com shell scripts como fallback quando fizer sentido.
- Viewer nunca inicia daemon, scheduler, gateway, watcher ou servidor.
- Agent pode iniciar processos residentes opcionais, sempre explicitamente.
- Customizacao versionada do projeto fica em `profile/`; estado runtime do usuario fica em `~/.pocket`.
- Arquivos padrao do projeto fora de `profile/` nao recebem personalizacao direta.
- Todo estado novo gravado em disco usa escrita atomica: temporario no mesmo diretorio, flush/fsync best-effort e rename.
- JSONL usa um objeto JSON por linha, UTF-8, linha maxima de 256 KiB e shard maximo de 10 MiB.
- Erros publicos seguem `{code: string, message: string, details: array}`.
- Timeouts sao em milissegundos.
- Datas sao ISO-8601 UTC `YYYY-MM-DDTHH:MM:SSZ`.
- IDs publicos usam lowercase ASCII, digitos, `-`, `_`, `.`, `:` e `/`, tamanho maximo 180 caracteres, salvo onde declarado diferente.
- Nomes de capacidades usam prefixos: `pocketcli.*`, `pocketwiki.*`, `middlewareauth.*`.
- Nenhum componente grava access token, refresh token, API key ou segredo bruto fora do store criptografado do middlewareAuth.
- Segredos em logs, eventos, contexto e saida de tools devem ser redigidos antes de persistir.
- Toolsets sao deny-by-default no Viewer para tools mutantes de rede/FS/remoto.
- Operacoes de rede devem ter timeout explicito.
- Operacoes que usam `ssh`, `tailscale`, `tmux`, `git`, `jq`, `fzf`, `rg` ou `go` devem degradar com erro controlado quando o binario nao existir.
- PocketWiki e middlewareAuth sao dependencias opcionais no PocketCli; ausencia deles reduz capacidade, nao quebra comandos basicos.
- `pocket update` deve continuar preservando arquivos locais divergentes e centralizando customizacao em `profile/`.

### Definition of Done global

- [ ] todos os testes de todas as fases passando
- [ ] `sh scripts/run_local_ci.sh` documentado e executado para mudancas Go do PocketCli
- [ ] `go test ./...` executado em middlewareAuth quando contrato dele for alterado
- [ ] testes PocketWiki relevantes documentados quando o Context Provider tocar API/wiki
- [ ] Viewer/iSH permanece sem processo residente apos `pocket`, `pocket ask`, `pocket recall`, `pocket context` e `pocket hosts`
- [ ] `pocket doctor --ecosystem --format json` retorna estado dos tres projetos sem vazar segredo
- [ ] `pocket ask --provider middleware` funciona quando middlewareAuth esta saudavel e degrada com erro controlado quando nao esta
- [ ] `pocket history search` acha sessoes recentes sem depender de SQLite
- [ ] `pocket context --wiki` retorna citacoes com `source_id` e `path` quando PocketWiki esta disponivel
- [ ] scheduler nao executa no Viewer e bloqueia jobs de ciclo de vida do proprio Pocket Stack
- [ ] fleet registra run envelope por host, com timeout, exit code e resumo
- [ ] SafetyPolicy comum bloqueia `.env*`, chaves SSH, `.middleware-state`, stores OAuth, tokens e paths sensiveis em contexto, MCP, scheduler e fleet
- [ ] documentacao de execucao de testes novos inclui prerequisitos e variaveis de ambiente

## 3.1 — Fase 01: Manifesto do Pocket Stack e Registry de Capacidades

### O que e

Contrato central para o PocketCli descobrir, reportar e usar capacidades locais dos tres projetos. Ele substitui acoplamento por convencao por um manifesto verificavel, mantendo cada projeto independente.

**Responsabilidades:**
- declarar componentes instalados: PocketCli, PocketWiki e middlewareAuth;
- declarar endpoints, comandos, paths e capacidades disponiveis;
- separar capacidades por modo `viewer`, `agent`, `server` e `external`;
- produzir snapshot JSON consumivel por CLI, TUI, scheduler, MCP e doctor;
- degradar quando um projeto local nao existir ou nao estiver rodando.

**Fora do escopo deste componente:**
- autenticar LLM;
- indexar wiki;
- executar comandos remotos;
- criar scheduler;
- expor MCP.

---

### Testes obrigatorios

```
TESTE 01-01
dado:    PocketCli em /Users/irinery/Documents/PocketCli, PocketWiki existente e middlewareAuth existente
quando:  pocket ecosystem inspect --format json executa
entao:   a saida contem components com ids pocketcli, pocketwiki e middlewareauth, cada um com status present

TESTE 01-02
dado:    caminho de PocketWiki inexistente configurado no manifest
quando:  pocket ecosystem inspect --format json executa
entao:   a saida retorna pocketwiki.status=missing e exit_code=0, sem quebrar pocketcli basico

TESTE 01-03  [edge case]
dado:    manifest user override vazio em profile/ecosystem.json
quando:  o registry carrega
entao:   defaults do projeto sao usados e um warning estruturado ERR_MANIFEST_EMPTY e emitido em details

TESTE 01-04  [seguranca]
dado:    profile/ecosystem.json tenta declarar capability com endpoint file:///Users/irinery/Documents/middlewareAuth/.middleware-state/auth-profiles.json
quando:  pocket ecosystem inspect executa
entao:   capability e rejeitada com ERR_CAPABILITY_SENSITIVE_PATH e o arquivo sensivel nao e lido
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-01-1
  tipo: cli
  nome: pocket ecosystem inspect

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]
    - campo: refresh
      tipo: bool
      obrigatorio: false
      validacao: nenhuma
      valores_enum: []

  saida:
    sucesso:
      tipo: EcosystemSnapshot
      campos:
        - campo: schema_version
          tipo: int
        - campo: generated_at
          tipo: string
        - campo: components
          tipo: array<PocketComponent>
        - campo: capabilities
          tipo: array<PocketCapability>
        - campo: warnings
          tipo: array<PocketError>
    falha:
      valores_possiveis:
        - code: ERR_MANIFEST_INVALID
          quando: JSON de profile/ecosystem.json e invalido
          acao: retornar exit_code=2 com erro publico
        - code: ERR_CAPABILITY_SENSITIVE_PATH
          quando: capability aponta para path bloqueado
          acao: omitir capability e retornar warning se strict=false; falhar se strict=true
        - code: ERR_OUTPUT_FORMAT
          quando: format nao pertence ao enum
          acao: retornar exit_code=2 sem executar probes

  comportamento_em_falha:
    - condicao: componente opcional ausente
      acao: marcar status=missing e continuar
      observavel: exit_code=0 com warning
    - condicao: endpoint HTTP opcional sem resposta
      acao: marcar status=present, health=unreachable
      observavel: snapshot valido com detalhe de timeout
```

```yaml
interface:
  id: IF-01-2
  tipo: file
  nome: profile/ecosystem.json

  entrada:
    - campo: components
      tipo: array
      obrigatorio: false
      validacao: cada item deve seguir PocketComponentOverride
      valores_enum: []
    - campo: disabled_capabilities
      tipo: array
      obrigatorio: false
      validacao: cada item deve ser capability id valido
      valores_enum: []

  saida:
    sucesso:
      tipo: EcosystemManifestOverride
      campos:
        - campo: components
          tipo: array<PocketComponentOverride>
        - campo: disabled_capabilities
          tipo: array<string>
    falha:
      valores_possiveis:
        - code: ERR_MANIFEST_INVALID_JSON
          quando: arquivo nao parseia como JSON
          acao: ignorar override e reportar warning
        - code: ERR_MANIFEST_UNKNOWN_COMPONENT
          quando: override referencia componente desconhecido
          acao: ignorar componente desconhecido

  comportamento_em_falha:
    - condicao: arquivo ausente
      acao: usar defaults embutidos
      observavel: snapshot sem warning
```

---

### Estruturas de dados

```yaml
tipo: PocketComponent
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 64 caracteres
  - nome: title
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 120 caracteres
  - nome: root_path
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4096 caracteres
  - nome: status
    tipo: enum
    obrigatorio: true
    default: missing
    limite: valores [present, missing, disabled]
  - nome: health
    tipo: enum
    obrigatorio: true
    default: unknown
    limite: valores [healthy, degraded, unreachable, unknown]
  - nome: version
    tipo: string
    obrigatorio: false
    default: nenhum
    limite: 80 caracteres
```

```yaml
tipo: PocketCapability
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 120 caracteres
  - nome: component_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 64 caracteres
  - nome: mode
    tipo: enum
    obrigatorio: true
    default: viewer
    limite: valores [viewer, agent, server, external]
  - nome: kind
    tipo: enum
    obrigatorio: true
    default: cli
    limite: valores [cli, http, mcp, file, process]
  - nome: target
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4096 caracteres
  - nome: mutates
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
  - nome: requires_approval
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
```

```yaml
tipo: PocketError
campos:
  - nome: code
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: message
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 500 caracteres
  - nome: details
    tipo: array<PocketErrorDetail>
    obrigatorio: true
    default: []
    limite: maximo 50 itens
```

---

### Regras de comportamento

| Componente ausente | Capability obrigatoria | Resultado | Acao |
|--------------------|------------------------|-----------|------|
| sim | nao | degraded | registrar warning e continuar |
| sim | sim | failed | retornar erro do comando que exige a capability |
| nao | nao | healthy ou unreachable | executar probe com timeout |
| nao | sim | healthy ou degraded | retornar capability com status detalhado |

**Limites numericos explicitos:**
- timeout de probe HTTP local: 1500 ms
- timeout de probe de comando local: 1500 ms
- tamanho maximo de `profile/ecosystem.json`: 128 KiB
- maximo de capabilities no snapshot: 500
- maximo de warnings no snapshot: 50

---

### Dependencias desta fase

```yaml
- id: DEP-01-1
  tipo: componente_interno
  nome: cmd/pocket
  versao: qualquer
  papel: expor comando de inspecao
  fallback: imprimir erro se binario Go nao existir
  obrigatoria: true
- id: DEP-01-2
  tipo: arquivo
  nome: profile/ecosystem.json
  versao: schema_version >= 1
  papel: override opcional de paths e capacidades
  fallback: defaults embutidos
  obrigatoria: false
- id: DEP-01-3
  tipo: arquivo
  nome: /Users/irinery/Documents/pocketwiki
  versao: qualquer
  papel: detectar componente PocketWiki local
  fallback: status=missing
  obrigatoria: false
- id: DEP-01-4
  tipo: arquivo
  nome: /Users/irinery/Documents/middlewareAuth
  versao: qualquer
  papel: detectar componente middlewareAuth local
  fallback: status=missing
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket ecosystem inspect --format json`
- [ ] default manifest embutido ou `specs/ecosystem/default.json`
- [ ] validacao de paths sensiveis
- [ ] testes 01-01 a 01-04 passando

## 3.2 — Fase 02: Integracao middlewareAuth como Provider Broker

### O que e

Adapter oficial para o PocketCli chamar provedores LLM via middlewareAuth, usando o contrato generico `llm_*` ja existente. Isso move auth, refresh, selecao de modelo e SSE para o middleware, deixando PocketCli leve.

**Responsabilidades:**
- descobrir `MIDDLEWARE_BASE_URL` e `MIDDLEWARE_CLIENT_TOKEN` sem persistir segredo no PocketCli;
- consultar `llm_providers`, `llm_status`, `llm_refresh` e `llm_responses`;
- mapear `pocket ask --provider middleware` para request LLM generica;
- preservar fallback local/remoto existente;
- redigir token e headers em audit/log.

**Fora do escopo deste componente:**
- implementar OAuth no PocketCli;
- armazenar refresh token no PocketCli;
- alterar store criptografado do middlewareAuth;
- trocar contrato `llm_*`;
- implementar streaming TUI completo.

---

### Testes obrigatorios

```
TESTE 02-01
dado:    middlewareAuth saudavel em http://localhost:18787 e token valido no ambiente
quando:  pocket ask --provider middleware "responda ok"
entao:   PocketCli envia llm_responses e imprime o output_text retornado

TESTE 02-02
dado:    MIDDLEWARE_CLIENT_TOKEN ausente
quando:  pocket model status --provider openai executa
entao:   retorna ERR_MIDDLEWARE_TOKEN_MISSING sem tentar chamar o servidor

TESTE 02-03  [edge case]
dado:    middlewareAuth retorna 401
quando:  pocket ask --provider middleware executa
entao:   audit registra backend=middleware finish_reason=error sem gravar o token e CLI retorna erro controlado

TESTE 02-04  [seguranca]
dado:    resposta de erro do middleware contem Authorization: Bearer abcdefghijklmnopqrstuvwxyz
quando:  PocketCli renderiza o erro
entao:   stdout, stderr e audit contem Authorization: Bearer [REDACTED]
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-02-1
  tipo: cli
  nome: pocket model status

  entrada:
    - campo: provider
      tipo: string
      obrigatorio: false
      validacao: provider id lowercase; default openai
      valores_enum: []
    - campo: profile
      tipo: string
      obrigatorio: false
      validacao: profile id valido; default default
      valores_enum: []
    - campo: project
      tipo: string
      obrigatorio: false
      validacao: project id valido; default detectado pelo cwd
      valores_enum: []
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: LLMStatus
      campos:
        - campo: authenticated
          tipo: bool
        - campo: provider_id
          tipo: string
        - campo: project_id
          tipo: string
        - campo: profile_id
          tipo: string
        - campo: account_hint
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_MIDDLEWARE_TOKEN_MISSING
          quando: token interno nao esta no ambiente
          acao: falhar antes da rede
        - code: ERR_MIDDLEWARE_UNREACHABLE
          quando: base_url nao responde em 3000 ms
          acao: retornar erro controlado
        - code: ERR_LLM_PROVIDER_UNKNOWN
          quando: provider nao aparece em llm_providers
          acao: retornar erro controlado

  comportamento_em_falha:
    - condicao: middleware retorna erro JSON estruturado
      acao: mapear code/message/details preservando code
      observavel: erro publico sem segredo
    - condicao: middleware retorna body nao JSON
      acao: retornar ERR_MIDDLEWARE_BAD_RESPONSE com preview redigido de ate 500 caracteres
      observavel: CLI falha com exit_code=1
```

```yaml
interface:
  id: IF-02-2
  tipo: function
  nome: middlewareLLMComplete

  entrada:
    - campo: provider_id
      tipo: string
      obrigatorio: true
      validacao: provider suportado por llm_providers
      valores_enum: []
    - campo: project_id
      tipo: string
      obrigatorio: true
      validacao: regex ^[a-zA-Z0-9._-]{1,80}$
      valores_enum: []
    - campo: profile_id
      tipo: string
      obrigatorio: true
      validacao: regex ^[a-zA-Z0-9._-]{1,80}$
      valores_enum: []
    - campo: model
      tipo: string
      obrigatorio: false
      validacao: maximo 120 caracteres
      valores_enum: []
    - campo: input
      tipo: array
      obrigatorio: true
      validacao: array<LLMInputItem>, maximo 64 itens
      valores_enum: []
    - campo: reasoning_effort
      tipo: string
      obrigatorio: false
      validacao: maximo 40 caracteres
      valores_enum: []

  saida:
    sucesso:
      tipo: LLMCompletion
      campos:
        - campo: model
          tipo: string
        - campo: output_text
          tipo: string
        - campo: events
          tipo: array<LLMStreamEvent>
        - campo: usage
          tipo: LLMUsage
    falha:
      valores_possiveis:
        - code: ERR_LLM_AUTH_REQUIRED
          quando: provider/profile nao autenticado
          acao: orientar login via middlewareAuth sem abrir browser automaticamente
        - code: ERR_LLM_TIMEOUT
          quando: chamada excede timeout
          acao: cancelar request e registrar latencia
        - code: ERR_LLM_BAD_REQUEST
          quando: middleware rejeita input
          acao: retornar details com campos invalidos

  comportamento_em_falha:
    - condicao: stream SSE parcial
      acao: retornar eventos coletados e finish_reason=error
      observavel: audit contem partial=true
```

---

### Estruturas de dados

```yaml
tipo: LLMStatus
campos:
  - nome: authenticated
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
  - nome: provider_id
    tipo: string
    obrigatorio: true
    default: openai
    limite: 80 caracteres
  - nome: project_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: profile_id
    tipo: string
    obrigatorio: true
    default: default
    limite: 80 caracteres
  - nome: account_hint
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 160 caracteres
  - nome: expires_at
    tipo: int
    obrigatorio: false
    default: 0
    limite: unix epoch segundos
```

```yaml
tipo: LLMInputItem
campos:
  - nome: role
    tipo: enum
    obrigatorio: true
    default: user
    limite: valores [system, user, assistant]
  - nome: content
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 128 KiB
```

```yaml
tipo: LLMUsage
campos:
  - nome: input_tokens
    tipo: int
    obrigatorio: true
    default: 0
    limite: minimo 0
  - nome: output_tokens
    tipo: int
    obrigatorio: true
    default: 0
    limite: minimo 0
  - nome: reasoning_tokens
    tipo: int
    obrigatorio: false
    default: 0
    limite: minimo 0
```

---

### Regras de comportamento

| Middleware health | Token | Modo pedido | Resultado | Acao |
|-------------------|-------|-------------|-----------|------|
| healthy | presente | middleware | middleware | chamar `llm_responses` |
| unreachable | presente | middleware | erro | nao cair silenciosamente para outro backend |
| unreachable | presente | auto | fallback | usar roteamento atual local/remoto se configurado |
| healthy | ausente | qualquer middleware | erro | falhar antes da rede |

**Limites numericos explicitos:**
- timeout health/status: 3000 ms
- timeout completion: 120000 ms
- maximo de input enviado ao middleware por request: 512 KiB
- maximo de erro bruto preservado apos redacao: 500 caracteres
- maximo de retries no PocketCli: 0; retries pertencem ao middlewareAuth

---

### Dependencias desta fase

```yaml
- id: DEP-02-1
  tipo: servico_externo
  nome: middlewareAuth HTTP
  versao: contrato llm_* conforme docs/LLM_PROVIDER_CONTRACT.md
  papel: broker de auth/modelos
  fallback: erro controlado e roteamento legado quando modo auto permitir
  obrigatoria: false
- id: DEP-02-2
  tipo: variavel_de_ambiente
  nome: MIDDLEWARE_BASE_URL
  versao: qualquer
  papel: endpoint HTTP local
  fallback: http://localhost:18787
  obrigatoria: false
- id: DEP-02-3
  tipo: variavel_de_ambiente
  nome: MIDDLEWARE_CLIENT_TOKEN
  versao: qualquer
  papel: token interno bearer
  fallback: nenhum; falha fechada
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] client Go para `llm_status` e `llm_responses`
- [ ] `pocket model status`
- [ ] `pocket ask --provider middleware`
- [ ] redacao de token em erros/audit
- [ ] testes 02-01 a 02-04 passando

## 3.3 — Fase 03: Session Ledger, Historico e Busca Rebuildable

### O que e

Ledger append-only para registrar turns, comandos, tool calls, backend decisions, eventos de scheduler/fleet e contexto usado. Ele traz o insight do Hermes de historico pesquisavel, mas usando formato leve/rebuildable que funcione no iSH sem SQLite obrigatorio.

**Responsabilidades:**
- gravar sessoes em JSONL por mes;
- registrar `session_id`, `parent_session_id`, `source`, `cwd`, `host`, `project`, `tool_calls`, tokens e resumo;
- permitir busca simples por termo, projeto, host e source;
- permitir compactacao manual de sessoes longas com nova sessao filha;
- gerar indice invertido rebuildable em `~/.pocket/index/`.

**Fora do escopo deste componente:**
- banco SQLite obrigatorio;
- embedding/vetor obrigatorio;
- summarizacao LLM autonoma sem aprovacao;
- sincronizacao multiusuario;
- salvar segredo ou prompt bruto sensivel.

---

### Testes obrigatorios

```
TESTE 03-01
dado:    pocket ask executou com session_id fixo e resposta de backend fake
quando:  pocket history show <session_id> executa
entao:   mostra turn user/assistant, backend, latency_ms e memory_hit

TESTE 03-02
dado:    ledger mensal contem uma linha JSON truncada/corrompida no meio
quando:  pocket history search "tailscale" executa
entao:   ignora a linha invalida, retorna resultados validos e warning ERR_LEDGER_CORRUPT_LINE

TESTE 03-03  [edge case]
dado:    uma sessao compactada com parent_session_id apontando para sessao anterior
quando:  pocket history show --chain <child_session_id> executa
entao:   mostra a cadeia pai-filho em ordem cronologica

TESTE 03-04  [seguranca]
dado:    prompt contem OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz
quando:  ledger grava o evento
entao:   arquivo JSONL contem OPENAI_API_KEY=[REDACTED] e nao contem o valor original
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-03-1
  tipo: file
  nome: ~/.pocket/sessions/ledger-YYYY-MM.jsonl

  entrada:
    - campo: event
      tipo: PocketSessionEvent
      obrigatorio: true
      validacao: JSON serializavel; maximo 256 KiB apos redacao
      valores_enum: []

  saida:
    sucesso:
      tipo: LedgerAppendResult
      campos:
        - campo: path
          tipo: string
        - campo: offset
          tipo: int
        - campo: bytes_written
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_LEDGER_WRITE
          quando: append falha por permissao ou disco
          acao: retornar erro e manter comando principal funcionando quando possivel
        - code: ERR_LEDGER_EVENT_TOO_LARGE
          quando: evento excede 256 KiB apos redacao
          acao: truncar campos permitidos e repetir uma vez
        - code: ERR_LEDGER_REDACTION_FAILED
          quando: redator falha
          acao: falhar fechado e nao gravar evento

  comportamento_em_falha:
    - condicao: diretorio nao existe
      acao: criar com permissao 0700
      observavel: append bem-sucedido
    - condicao: disco cheio
      acao: retornar warning no comando chamador
      observavel: comando nao panica
```

```yaml
interface:
  id: IF-03-2
  tipo: cli
  nome: pocket history search

  entrada:
    - campo: query
      tipo: string
      obrigatorio: true
      validacao: 1 a 500 caracteres
      valores_enum: []
    - campo: project
      tipo: string
      obrigatorio: false
      validacao: maximo 120 caracteres
      valores_enum: []
    - campo: host
      tipo: string
      obrigatorio: false
      validacao: maximo 255 caracteres
      valores_enum: []
    - campo: limit
      tipo: int
      obrigatorio: false
      validacao: 1 a 100
      valores_enum: []
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: SessionSearchResult
      campos:
        - campo: query
          tipo: string
        - campo: hits
          tipo: array<SessionHit>
        - campo: warnings
          tipo: array<PocketError>
    falha:
      valores_possiveis:
        - code: ERR_HISTORY_QUERY_EMPTY
          quando: query vazia
          acao: retornar exit_code=2
        - code: ERR_HISTORY_INDEX_UNAVAILABLE
          quando: indice ausente e rebuild falha
          acao: buscar fallback linear ate 10 MiB mais recentes

  comportamento_em_falha:
    - condicao: indice ausente
      acao: tentar rebuild incremental
      observavel: warning se fallback linear for usado
```

---

### Estruturas de dados

```yaml
tipo: PocketSessionEvent
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: minimo 1
  - nome: event_id
    tipo: string
    obrigatorio: true
    default: uuid
    limite: 80 caracteres
  - nome: session_id
    tipo: string
    obrigatorio: true
    default: uuid
    limite: 80 caracteres
  - nome: parent_session_id
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 80 caracteres
  - nome: timestamp
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
  - nome: source
    tipo: enum
    obrigatorio: true
    default: cli
    limite: valores [cli, pocketwiki, scheduler, fleet, mcp, middleware]
  - nome: cwd
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 4096 caracteres
  - nome: host
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 255 caracteres
  - nome: project
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 120 caracteres
  - nome: kind
    tipo: enum
    obrigatorio: true
    default: turn
    limite: valores [turn, command, tool_call, scheduler_run, fleet_run, compact, error]
  - nome: summary
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 2000 caracteres
  - nome: body
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 32000 caracteres
  - nome: metadata
    tipo: object
    obrigatorio: true
    default: {}
    limite: maximo 64 chaves, valores string/int/bool/array simples
```

```yaml
tipo: SessionHit
campos:
  - nome: session_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: event_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: timestamp
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: ISO-8601 UTC
  - nome: score
    tipo: int
    obrigatorio: true
    default: 0
    limite: 0 a 100000
  - nome: preview
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 500 caracteres
```

---

### Regras de comportamento

| Evento | Campo body sensivel | Persistencia | Acao |
|--------|---------------------|--------------|------|
| turn | sim | redigido | gravar summary e body redigidos |
| tool_call | sim | parcial | gravar tool_name, args_hash e resultado redigido |
| command | nao | completo | gravar comando e exit code |
| fleet_run | sim | parcial | gravar por host sem segredo |

**Limites numericos explicitos:**
- linha JSONL maxima: 256 KiB
- shard mensal maximo antes de rotacao adicional: 10 MiB
- busca fallback linear maxima: 10 MiB
- indice rebuild timeout: 30000 ms
- maximo de hits default: 20
- maximo de hits permitido: 100

---

### Dependencias desta fase

```yaml
- id: DEP-03-1
  tipo: componente_interno
  nome: internal/audit
  versao: qualquer
  papel: base para evoluir eventos estruturados
  fallback: log atual texto continua existindo
  obrigatoria: true
- id: DEP-03-2
  tipo: arquivo
  nome: ~/.pocket/sessions
  versao: schema_version >= 1
  papel: ledger append-only
  fallback: criar diretorio 0700
  obrigatoria: true
- id: DEP-03-3
  tipo: componente_interno
  nome: SafetyPolicy redactor
  versao: fase 09 quando disponivel
  papel: redigir segredos antes de persistir
  fallback: redator minimo embutido de tokens conhecidos
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] writer JSONL redigido
- [ ] `pocket history search`
- [ ] `pocket history show`
- [ ] indice rebuildable simples
- [ ] testes 03-01 a 03-04 passando

## 3.4 — Fase 04: PocketWiki Context Provider e Context Compiler

### O que e

Provider de contexto para o PocketCli consultar PocketWiki e compilar trechos citaveis junto com memoria operacional. Ele transforma a wiki local em memoria de trabalho para `pocket ask`, sem exigir que o PocketCli entenda toda a UI do PocketWiki.

**Responsabilidades:**
- detectar PocketWiki HTTP local/macOS/server quando disponivel;
- consultar arquivos e contexto da wiki por API ou MCP futuro;
- compilar `ContextBundle` com fontes, citacoes e limites;
- respeitar `SensitiveFilePolicy` do PocketWiki;
- registrar no ledger quais fontes foram usadas.

**Fora do escopo deste componente:**
- reimplementar indexador do PocketWiki no PocketCli;
- escrever na wiki;
- promover conhecimento canonico;
- criar embeddings obrigatorios;
- resolver conflitos de wiki multiusuario.

---

### Testes obrigatorios

```
TESTE 04-01
dado:    PocketWiki servindo /api/wiki/files com duas paginas markdown
quando:  pocket context --wiki --query "tailscale"
entao:   retorna ContextBundle com source_id, path relativo, excerpt e citation_count > 0

TESTE 04-02
dado:    PocketWiki indisponivel
quando:  pocket context --wiki --query "tailscale"
entao:   retorna ERR_POCKETWIKI_UNREACHABLE se --wiki foi explicito; em modo auto apenas adiciona warning

TESTE 04-03  [edge case]
dado:    resposta /api/wiki/files contem 5000 paginas
quando:  context compiler executa com limit default
entao:   processa no maximo 100 paginas candidatas e indica truncated=true

TESTE 04-04  [seguranca]
dado:    wiki contem link para /Users/irinery/Documents/middlewareAuth/.middleware-state/auth-profiles.json
quando:  context compiler executa
entao:   fonte e bloqueada com ERR_CONTEXT_SENSITIVE_SOURCE e nenhum trecho do arquivo sensivel aparece na saida
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-04-1
  tipo: cli
  nome: pocket context --wiki

  entrada:
    - campo: query
      tipo: string
      obrigatorio: false
      validacao: maximo 500 caracteres; se ausente usa cwd/session
      valores_enum: []
    - campo: wiki
      tipo: bool
      obrigatorio: false
      validacao: nenhuma
      valores_enum: []
    - campo: limit
      tipo: int
      obrigatorio: false
      validacao: 1 a 100
      valores_enum: []
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: ContextBundle
      campos:
        - campo: sources
          tipo: array<ContextSource>
        - campo: compiled_text
          tipo: string
        - campo: citations
          tipo: array<ContextCitation>
        - campo: warnings
          tipo: array<PocketError>
    falha:
      valores_possiveis:
        - code: ERR_POCKETWIKI_UNREACHABLE
          quando: --wiki explicito e endpoint nao responde
          acao: retornar exit_code=1
        - code: ERR_CONTEXT_TOO_LARGE
          quando: bundle nao cabe no limite apos truncamento
          acao: retornar erro com detalhes de tamanho
        - code: ERR_CONTEXT_SENSITIVE_SOURCE
          quando: fonte bloqueada por policy
          acao: omitir fonte e registrar warning ou falhar se strict=true

  comportamento_em_falha:
    - condicao: PocketWiki responde JSON invalido
      acao: retornar ERR_POCKETWIKI_BAD_RESPONSE
      observavel: preview redigido de ate 500 caracteres
```

```yaml
interface:
  id: IF-04-2
  tipo: function
  nome: compilePocketWikiContext

  entrada:
    - campo: query
      tipo: string
      obrigatorio: true
      validacao: 1 a 500 caracteres
      valores_enum: []
    - campo: files
      tipo: array
      obrigatorio: true
      validacao: array<WikiFileCandidate>, maximo 5000 itens
      valores_enum: []
    - campo: max_chars
      tipo: int
      obrigatorio: false
      validacao: 1000 a 64000
      valores_enum: []

  saida:
    sucesso:
      tipo: ContextBundle
      campos:
        - campo: sources
          tipo: array<ContextSource>
        - campo: compiled_text
          tipo: string
        - campo: citations
          tipo: array<ContextCitation>
    falha:
      valores_possiveis:
        - code: ERR_CONTEXT_QUERY_EMPTY
          quando: query vazia
          acao: falhar sem processar
        - code: ERR_CONTEXT_INPUT_TOO_LARGE
          quando: files excede 5000 itens
          acao: truncar se strict=false; falhar se strict=true

  comportamento_em_falha:
    - condicao: arquivo individual grande demais
      acao: usar excerpt limitado
      observavel: source.truncated=true
```

---

### Estruturas de dados

```yaml
tipo: ContextSource
campos:
  - nome: source_id
    tipo: string
    obrigatorio: true
    default: hash path
    limite: 80 caracteres
  - nome: component
    tipo: enum
    obrigatorio: true
    default: pocketwiki
    limite: valores [pocketcli, pocketwiki, middlewareauth]
  - nome: path
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4096 caracteres
  - nome: title
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 200 caracteres
  - nome: excerpt
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 12000 caracteres
  - nome: score
    tipo: int
    obrigatorio: true
    default: 0
    limite: 0 a 100000
  - nome: truncated
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
```

```yaml
tipo: ContextCitation
campos:
  - nome: source_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: path
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4096 caracteres
  - nome: line_start
    tipo: int
    obrigatorio: false
    default: 0
    limite: minimo 0
  - nome: line_end
    tipo: int
    obrigatorio: false
    default: 0
    limite: minimo 0
  - nome: quote
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 500 caracteres
```

---

### Regras de comportamento

| PocketWiki disponivel | Flag --wiki | Resultado | Acao |
|-----------------------|-------------|-----------|------|
| sim | true | bundle wiki | compilar e registrar fontes |
| sim | false | bundle misto | usar se modo auto/contexto pedir |
| nao | true | erro | falhar explicitamente |
| nao | false | warning | continuar sem wiki |

**Limites numericos explicitos:**
- timeout `/api/config`: 1500 ms
- timeout `/api/wiki/files`: 5000 ms
- maximo paginas candidatas: 100
- maximo compiled_text default: 32000 caracteres
- maximo excerpt por pagina: 12000 caracteres
- maximo citations por bundle: 50

---

### Dependencias desta fase

```yaml
- id: DEP-04-1
  tipo: servico_externo
  nome: PocketWiki HTTP /api/wiki/files
  versao: atual
  papel: fonte de arquivos indexados
  fallback: contexto sem wiki
  obrigatoria: false
- id: DEP-04-2
  tipo: componente_interno
  nome: PocketWiki SensitiveFilePolicy
  versao: atual
  papel: bloquear fontes sensiveis
  fallback: policy minima duplicada no PocketCli ate existir contrato compartilhado
  obrigatoria: true
- id: DEP-04-3
  tipo: componente_interno
  nome: internal/contextcollector
  versao: qualquer
  papel: anexar contexto wiki ao TaskContext
  fallback: contexto atual sem wiki
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] deteccao de endpoint PocketWiki
- [ ] `pocket context --wiki`
- [ ] citacoes no `TaskContext`
- [ ] ledger registra fontes usadas
- [ ] testes 04-01 a 04-04 passando

## 3.5 — Fase 05: Toolsets Compostos e MCP Bridge Pocket

### O que e

Sistema de toolsets para expor capacidades do Pocket Stack de forma composta por modo e superficie. Ele absorve o insight do Hermes de toolsets por plataforma/cenario, mas usa um conjunto pequeno e operacional.

**Responsabilidades:**
- definir toolsets `pocket-viewer`, `pocket-agent`, `pocket-wiki`, `pocket-auth`, `pocket-fleet`, `pocket-safe`;
- validar tools com schema, timeout, mutabilidade e approval;
- expor MCP stdio `pocket mcp serve` para clientes externos;
- compor tools internas do PocketCli com tools HTTP/MCP do PocketWiki/middlewareAuth;
- impedir tools mutantes em superficies nao autorizadas.

**Fora do escopo deste componente:**
- reimplementar todas as tools do Hermes;
- criar browser automation;
- criar plataforma de mensagens completa;
- permitir plugins arbitrarios sem allowlist;
- executar tool sem SafetyPolicy.

---

### Testes obrigatorios

```
TESTE 05-01
dado:    specs/toolsets/pocket-agent.json contem pocketcli.ssh.exec e pocketwiki.context.search
quando:  pocket tools resolve pocket-agent --format json executa
entao:   retorna lista deduplicada de tools com mutates/requires_approval corretos

TESTE 05-02
dado:    toolset pocket-viewer tenta incluir pocketcli.fleet.run
quando:  validador de toolsets executa
entao:   retorna ERR_TOOLSET_VIEWER_MUTATING_TOOL

TESTE 05-03  [edge case]
dado:    toolset A inclui B e B inclui A
quando:  pocket tools resolve A executa
entao:   retorna ERR_TOOLSET_CYCLE com caminho A->B->A

TESTE 05-04  [seguranca]
dado:    MCP client chama pocket_exec com command "cat ~/.ssh/id_ed25519"
quando:  pocket mcp serve processa a chamada
entao:   retorna isError=true com ERR_APPROVAL_REQUIRED ou ERR_SAFETY_DENIED e nao executa o comando
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-05-1
  tipo: cli
  nome: pocket tools resolve

  entrada:
    - campo: toolset
      tipo: string
      obrigatorio: true
      validacao: id de toolset conhecido
      valores_enum: []
    - campo: mode
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [viewer, agent, server]
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: ToolsetResolution
      campos:
        - campo: toolset
          tipo: string
        - campo: tools
          tipo: array<PocketToolDefinition>
        - campo: warnings
          tipo: array<PocketError>
    falha:
      valores_possiveis:
        - code: ERR_TOOLSET_NOT_FOUND
          quando: toolset desconhecido
          acao: falhar com exit_code=2
        - code: ERR_TOOLSET_CYCLE
          quando: inclusoes formam ciclo
          acao: falhar sem retornar lista parcial
        - code: ERR_TOOLSET_VIEWER_MUTATING_TOOL
          quando: viewer inclui tool mutante proibida
          acao: falhar validação

  comportamento_em_falha:
    - condicao: tool opcional indisponivel
      acao: omitir tool e incluir warning
      observavel: resolution valido com degraded=true
```

```yaml
interface:
  id: IF-05-2
  tipo: ipc
  nome: pocket mcp serve

  entrada:
    - campo: jsonrpc
      tipo: string
      obrigatorio: true
      validacao: deve ser "2.0"
      valores_enum: []
    - campo: method
      tipo: string
      obrigatorio: true
      validacao: initialize | tools/list | tools/call | ping
      valores_enum: []
    - campo: params
      tipo: object
      obrigatorio: false
      validacao: depende do metodo
      valores_enum: []

  saida:
    sucesso:
      tipo: MCPResponse
      campos:
        - campo: jsonrpc
          tipo: string
        - campo: id
          tipo: string
        - campo: result
          tipo: object
    falha:
      valores_possiveis:
        - code: ERR_MCP_PARSE
          quando: linha nao e JSON-RPC valido
          acao: responder erro -32700
        - code: ERR_MCP_TOOL_UNKNOWN
          quando: tool chamada nao existe
          acao: responder tools/call com isError=true
        - code: ERR_MCP_TOOL_DENIED
          quando: SafetyPolicy bloqueia
          acao: responder isError=true sem executar tool

  comportamento_em_falha:
    - condicao: cliente envia payload > 2 MiB
      acao: responder erro e encerrar request
      observavel: stderr contem log redigido
```

---

### Estruturas de dados

```yaml
tipo: PocketToolDefinition
campos:
  - nome: name
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 120 caracteres
  - nome: component_id
    tipo: string
    obrigatorio: true
    default: pocketcli
    limite: 64 caracteres
  - nome: description
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 500 caracteres
  - nome: input_schema
    tipo: object
    obrigatorio: true
    default: {}
    limite: JSON schema subset, maximo 32 propriedades
  - nome: output_schema
    tipo: object
    obrigatorio: true
    default: {}
    limite: JSON schema subset, maximo 32 propriedades
  - nome: timeout_ms
    tipo: int
    obrigatorio: true
    default: 5000
    limite: 100 a 300000
  - nome: mutates
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
  - nome: requires_approval
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
```

```yaml
tipo: ToolsetManifest
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: minimo 1
  - nome: id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: includes
    tipo: array<string>
    obrigatorio: true
    default: []
    limite: maximo 20 itens
  - nome: tools
    tipo: array<string>
    obrigatorio: true
    default: []
    limite: maximo 100 itens
  - nome: modes
    tipo: array<enum>
    obrigatorio: true
    default: [agent]
    limite: valores [viewer, agent, server]
```

---

### Regras de comportamento

| Tool mutante | Mode viewer | requires_approval | Resultado | Acao |
|--------------|-------------|-------------------|-----------|------|
| nao | sim | false | permitido | executar |
| sim | sim | false | negado | falhar validacao |
| sim | nao | true | pendente | passar pela SafetyPolicy |
| sim | nao | false | permitido apenas se tool allowlisted | executar com audit |

**Limites numericos explicitos:**
- maximo de toolsets resolvidos por comando: 20
- maximo de tools por resolucao: 200
- payload MCP maximo por linha: 2 MiB
- timeout default tool read-only: 5000 ms
- timeout default tool mutante: 30000 ms

---

### Dependencias desta fase

```yaml
- id: DEP-05-1
  tipo: arquivo
  nome: specs/toolsets/*.json
  versao: schema_version >= 1
  papel: toolsets padrao
  fallback: toolsets embutidos minimos
  obrigatoria: true
- id: DEP-05-2
  tipo: componente_interno
  nome: internal/tools
  versao: atual
  papel: registry base de tools Go
  fallback: tools internas atuais
  obrigatoria: true
- id: DEP-05-3
  tipo: servico_externo
  nome: middlewareAuth MCP/HTTP
  versao: contrato llm_*
  papel: tools de LLM
  fallback: omitir tools middleware
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `specs/toolsets/pocket-safe.json`
- [ ] `specs/toolsets/pocket-agent.json`
- [ ] `pocket tools resolve`
- [ ] `pocket mcp serve` com `initialize`, `tools/list`, `tools/call`
- [ ] testes 05-01 a 05-04 passando

## 3.6 — Fase 06: Scheduler Agent-only com Entrega Auditavel

### O que e

Scheduler local para automacoes Pocket no modo Agent, inspirado no cron interno do Hermes. Ele permite tarefas recorrentes ou one-shot, com saida persistida e entrega opcional, sem introduzir daemon no Viewer.

**Responsabilidades:**
- criar, listar, pausar, retomar, remover e disparar jobs;
- armazenar jobs em `~/.pocket/scheduler/jobs.json`;
- armazenar saidas em `~/.pocket/scheduler/output/<job_id>/<timestamp>.md`;
- rodar apenas quando `pocket agent scheduler` ou servico Agent estiver ativo;
- bloquear comandos de ciclo de vida do proprio Pocket Stack;
- limitar toolsets disponiveis para jobs.

**Fora do escopo deste componente:**
- cron daemon no Viewer;
- gateway multi-plataforma completo;
- agendamento distribuido entre maquinas;
- execucao de job sem audit;
- auto-criacao de jobs por LLM sem confirmacao.

---

### Testes obrigatorios

```
TESTE 06-01
dado:    modo Agent e comando pocket schedule create --name backup --every 1h -- "pocket hosts"
quando:  pocket schedule list --format json executa
entao:   retorna job enabled=true, schedule.kind=interval e next_run_at definido

TESTE 06-02
dado:    modo Viewer
quando:  pocket agent scheduler executa
entao:   retorna ERR_SCHEDULER_VIEWER_FORBIDDEN e nao cria lock nem processo residente

TESTE 06-03  [edge case]
dado:    dois processos pocket agent scheduler iniciam simultaneamente
quando:  ambos tentam tick
entao:   apenas um adquire lock e o outro retorna ERR_SCHEDULER_LOCKED sem executar jobs

TESTE 06-04  [seguranca]
dado:    job tenta executar "pkill -f pocket" ou "pocket update"
quando:  pocket schedule create valida o comando
entao:   retorna ERR_SCHEDULER_LIFECYCLE_COMMAND_BLOCKED e nao persiste o job
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-06-1
  tipo: cli
  nome: pocket schedule create

  entrada:
    - campo: name
      tipo: string
      obrigatorio: true
      validacao: 1 a 80 caracteres
      valores_enum: []
    - campo: schedule
      tipo: string
      obrigatorio: true
      validacao: "10m", "every 1h", cron 5 campos ou ISO-8601
      valores_enum: []
    - campo: command
      tipo: string
      obrigatorio: true
      validacao: 1 a 4000 caracteres
      valores_enum: []
    - campo: deliver
      tipo: array
      obrigatorio: false
      validacao: valores permitidos [local, pocketwiki, webhook]
      valores_enum: []
    - campo: toolset
      tipo: string
      obrigatorio: false
      validacao: toolset permitido para scheduler
      valores_enum: []

  saida:
    sucesso:
      tipo: ScheduleCreateResult
      campos:
        - campo: job_id
          tipo: string
        - campo: next_run_at
          tipo: string
        - campo: schedule_display
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_SCHEDULER_INVALID_SCHEDULE
          quando: schedule nao parseia
          acao: retornar exit_code=2
        - code: ERR_SCHEDULER_LIFECYCLE_COMMAND_BLOCKED
          quando: comando tenta restart/stop/update/kill do Pocket Stack
          acao: rejeitar job
        - code: ERR_SCHEDULER_TOOLSET_DENIED
          quando: toolset inclui tool proibida em cron
          acao: rejeitar job

  comportamento_em_falha:
    - condicao: jobs.json corrompido
      acao: mover para .corrupt.<timestamp> e iniciar vazio somente com --repair
      observavel: sem perda silenciosa
```

```yaml
interface:
  id: IF-06-2
  tipo: cli
  nome: pocket agent scheduler

  entrada:
    - campo: once
      tipo: bool
      obrigatorio: false
      validacao: se true executa um tick e sai
      valores_enum: []
    - campo: interval_ms
      tipo: int
      obrigatorio: false
      validacao: 10000 a 3600000
      valores_enum: []

  saida:
    sucesso:
      tipo: SchedulerTickResult
      campos:
        - campo: due_count
          tipo: int
        - campo: started_count
          tipo: int
        - campo: skipped_count
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_SCHEDULER_VIEWER_FORBIDDEN
          quando: modo efetivo viewer
          acao: sair sem loop
        - code: ERR_SCHEDULER_LOCKED
          quando: outro scheduler possui lock ativo
          acao: sair sem executar
        - code: ERR_SCHEDULER_STATE_IO
          quando: nao consegue ler/gravar state
          acao: falhar com erro publico

  comportamento_em_falha:
    - condicao: job individual falha
      acao: registrar output e last_status=error; continuar demais jobs
      observavel: schedule list mostra last_error redigido
```

---

### Estruturas de dados

```yaml
tipo: PocketScheduleJob
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: uuid curto
    limite: 64 caracteres, sem / ou ..
  - nome: name
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: enabled
    tipo: bool
    obrigatorio: true
    default: true
    limite: nenhum
  - nome: schedule
    tipo: ScheduleSpec
    obrigatorio: true
    default: nenhum
    limite: nenhum
  - nome: command
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4000 caracteres
  - nome: toolset
    tipo: string
    obrigatorio: true
    default: pocket-safe
    limite: 80 caracteres
  - nome: deliver
    tipo: array<enum>
    obrigatorio: true
    default: [local]
    limite: valores [local, pocketwiki, webhook]
  - nome: next_run_at
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: ISO-8601 UTC
  - nome: last_status
    tipo: enum
    obrigatorio: false
    default: never
    limite: valores [never, ok, error, skipped]
```

```yaml
tipo: SchedulerRunOutput
campos:
  - nome: job_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 64 caracteres
  - nome: run_id
    tipo: string
    obrigatorio: true
    default: uuid
    limite: 80 caracteres
  - nome: started_at
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
  - nome: finished_at
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
  - nome: exit_code
    tipo: int
    obrigatorio: true
    default: 0
    limite: -1 a 255
  - nome: output_path
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4096 caracteres
```

---

### Regras de comportamento

| Job source | Toolset pedido | Toolset efetivo | Acao |
|------------|----------------|-----------------|------|
| humano CLI | vazio | pocket-safe | criar se comando permitido |
| humano CLI | pocket-agent | pocket-agent sem scheduler/messaging/clarify | criar se SafetyPolicy permitir |
| LLM | qualquer | nenhum | exigir confirmacao humana antes de persistir |
| arquivo editado manualmente | proibido | nenhum | marcar job invalid no list |

**Limites numericos explicitos:**
- tick interval default: 60000 ms
- timeout default por job: 300000 ms
- maximo jobs ativos: 200
- maximo jobs paralelos: 3
- lock stale timeout: 120000 ms
- output maximo por job: 1 MiB antes de truncar

---

### Dependencias desta fase

```yaml
- id: DEP-06-1
  tipo: componente_interno
  nome: toolsets pocket-safe
  versao: fase 05
  papel: limitar capacidades de jobs
  fallback: comando shell simples com SafetyPolicy
  obrigatoria: true
- id: DEP-06-2
  tipo: arquivo
  nome: ~/.pocket/scheduler/jobs.json
  versao: schema_version >= 1
  papel: armazenamento de jobs
  fallback: criar vazio 0600
  obrigatoria: true
- id: DEP-06-3
  tipo: componente_interno
  nome: Session Ledger
  versao: fase 03
  papel: registrar execucoes
  fallback: audit log texto
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `pocket schedule create/list/pause/resume/remove/run`
- [ ] `pocket agent scheduler --once`
- [ ] lock de tick
- [ ] bloqueio de comandos lifecycle
- [ ] testes 06-01 a 06-04 passando

## 3.7 — Fase 07: Fleet Runtime, Run Envelopes e Delegacao Restrita

### O que e

Camada formal para execucao remota e paralela via SSH/Tailscale. Ela evolui o modelo atual de `ssh`, `exec`, inventory e fleet para envelopes auditaveis, com concorrencia controlada, politica de aprovacao e opcionalmente workers de tarefa em hosts Agent.

**Responsabilidades:**
- executar comandos por host/grupo com limite de concorrencia;
- persistir `RunEnvelope` e `HostRunResult`;
- usar inventario atual Tailscale/saved hosts/seeds;
- exigir aprovacao para comandos destrutivos;
- permitir delegacao restrita para hosts Agent sem herdar memoria completa do pai.

**Fora do escopo deste componente:**
- Docker/Modal/Daytona obrigatorios;
- shell interativo multi-hop;
- delegacao recursiva sem limite;
- auto-aprovacao de comandos perigosos;
- sincronizacao bidirecional completa de filesystem.

---

### Testes obrigatorios

```
TESTE 07-01
dado:    inventario com hosts a,b,c e execSSH fake retorna ok
quando:  pocket fleet run --hosts a,b,c --concurrency 2 -- uptime executa
entao:   cria RunEnvelope com 3 HostRunResult e max_parallel_observed=2

TESTE 07-02
dado:    host b retorna timeout
quando:  pocket fleet run executa
entao:   resultado final exit_code=1, host b status=timeout e hosts a/c preservam status ok

TESTE 07-03  [edge case]
dado:    seletor de hosts nao encontra nenhum host
quando:  pocket fleet run --group missing -- uptime executa
entao:   retorna ERR_FLEET_NO_TARGETS sem criar run remoto

TESTE 07-04  [seguranca]
dado:    comando "rm -rf /" sem --approve
quando:  pocket fleet run --hosts a -- rm -rf / executa
entao:   retorna ERR_APPROVAL_REQUIRED e execSSH nao e chamado
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-07-1
  tipo: cli
  nome: pocket fleet run

  entrada:
    - campo: hosts
      tipo: array
      obrigatorio: false
      validacao: lista de host ids, maximo 200
      valores_enum: []
    - campo: group
      tipo: string
      obrigatorio: false
      validacao: grupo existente no inventario
      valores_enum: []
    - campo: concurrency
      tipo: int
      obrigatorio: false
      validacao: 1 a 20
      valores_enum: []
    - campo: timeout_ms
      tipo: int
      obrigatorio: false
      validacao: 1000 a 600000
      valores_enum: []
    - campo: command
      tipo: string
      obrigatorio: true
      validacao: 1 a 4000 caracteres
      valores_enum: []

  saida:
    sucesso:
      tipo: FleetRunResult
      campos:
        - campo: run_id
          tipo: string
        - campo: status
          tipo: enum
        - campo: host_results
          tipo: array<HostRunResult>
    falha:
      valores_possiveis:
        - code: ERR_FLEET_NO_TARGETS
          quando: seletor nao resolve hosts
          acao: falhar antes de executar
        - code: ERR_APPROVAL_REQUIRED
          quando: comando requer aprovacao e nao ha decisao
          acao: falhar antes de executar
        - code: ERR_FLEET_CONCURRENCY_INVALID
          quando: concurrency fora do range
          acao: retornar exit_code=2

  comportamento_em_falha:
    - condicao: host individual falha
      acao: registrar HostRunResult e continuar demais hosts
      observavel: summary final por status
```

```yaml
interface:
  id: IF-07-2
  tipo: cli
  nome: pocket delegate

  entrada:
    - campo: host
      tipo: string
      obrigatorio: true
      validacao: host conhecido e aprovado
      valores_enum: []
    - campo: task
      tipo: string
      obrigatorio: true
      validacao: 1 a 4000 caracteres
      valores_enum: []
    - campo: toolset
      tipo: string
      obrigatorio: false
      validacao: toolset permitido para delegate
      valores_enum: []

  saida:
    sucesso:
      tipo: DelegateResult
      campos:
        - campo: delegation_id
          tipo: string
        - campo: host
          tipo: string
        - campo: summary
          tipo: string
        - campo: status
          tipo: enum
    falha:
      valores_possiveis:
        - code: ERR_DELEGATE_HOST_NOT_AGENT
          quando: host nao tem PocketCli Agent detectado
          acao: orientar uso de fleet run simples
        - code: ERR_DELEGATE_TOOLSET_DENIED
          quando: toolset inclui memory/send_message/scheduler/delegate
          acao: rejeitar delegacao
        - code: ERR_DELEGATE_TIMEOUT
          quando: tarefa excede timeout
          acao: registrar output parcial redigido

  comportamento_em_falha:
    - condicao: host cai durante delegacao
      acao: marcar status=lost e preservar ultimo heartbeat
      observavel: ledger contem evento de falha
```

---

### Estruturas de dados

```yaml
tipo: RunEnvelope
campos:
  - nome: run_id
    tipo: string
    obrigatorio: true
    default: uuid
    limite: 80 caracteres
  - nome: created_at
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
  - nome: source_session_id
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 80 caracteres
  - nome: command
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4000 caracteres redigidos
  - nome: target_hosts
    tipo: array<string>
    obrigatorio: true
    default: []
    limite: maximo 200 hosts
  - nome: concurrency
    tipo: int
    obrigatorio: true
    default: 3
    limite: 1 a 20
  - nome: approval_id
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 80 caracteres
```

```yaml
tipo: HostRunResult
campos:
  - nome: host
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 255 caracteres
  - nome: status
    tipo: enum
    obrigatorio: true
    default: pending
    limite: valores [pending, running, ok, error, timeout, skipped]
  - nome: exit_code
    tipo: int
    obrigatorio: true
    default: -1
    limite: -1 a 255
  - nome: started_at
    tipo: string
    obrigatorio: false
    default: vazio
    limite: ISO-8601 UTC
  - nome: finished_at
    tipo: string
    obrigatorio: false
    default: vazio
    limite: ISO-8601 UTC
  - nome: stdout_preview
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 4000 caracteres
  - nome: stderr_preview
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 4000 caracteres
```

---

### Regras de comportamento

| Comando | Host aprovado | Safety decision | Resultado | Acao |
|---------|---------------|-----------------|-----------|------|
| read-only | sim | allow | executar | registrar run |
| mutante | sim | approval_required | pendente | pedir aprovacao |
| mutante | nao | deny | bloqueado | nao executar |
| qualquer | host nao resolvido | deny | falha | ERR_FLEET_NO_TARGETS |

**Limites numericos explicitos:**
- hosts por run: maximo 200
- concorrencia default: 3
- concorrencia maxima: 20
- timeout default por host: 60000 ms
- stdout/stderr persistido por host: 64 KiB cada
- preview stdout/stderr: 4000 caracteres cada
- profundidade maxima de delegacao: 1 no MVP

---

### Dependencias desta fase

```yaml
- id: DEP-07-1
  tipo: componente_interno
  nome: internal/ssh
  versao: atual
  papel: execucao SSH
  fallback: erro se ssh ausente
  obrigatoria: true
- id: DEP-07-2
  tipo: arquivo
  nome: ~/.local/share/pocketcli/inventory.json
  versao: atual
  papel: resolver hosts
  fallback: hosts salvos e seeds
  obrigatoria: false
- id: DEP-07-3
  tipo: componente_interno
  nome: SafetyPolicy
  versao: fase 09
  papel: decidir aprovacao/bloqueio
  fallback: bloqueio conservador de comandos destrutivos
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `pocket fleet run`
- [ ] run envelopes em `~/.pocket/runs/`
- [ ] limite de concorrencia
- [ ] bloqueio/aprovacao de comandos destrutivos
- [ ] testes 07-01 a 07-04 passando

## 3.8 — Fase 08: Skills, Procedural Memory e Adocao Manual

### O que e

Sistema simples de skills para o Pocket Stack, conectando `profile/skills`, PocketWiki e toolsets. A ideia vem do Hermes, mas o MVP evita auto-modificacao: skills podem ser sugeridas a partir de tarefas bem-sucedidas, mas so entram no runtime apos aprovacao humana.

**Responsabilidades:**
- definir formato `profile/skills/<skill_id>/SKILL.md`;
- listar, visualizar, validar, criar draft e adotar skills;
- suportar frontmatter com plataforma, modo, toolsets e arquivos auxiliares;
- permitir que PocketWiki indexe skills como conhecimento operacional;
- impedir skill de sobrescrever tools core ou policies.

**Fora do escopo deste componente:**
- skill auto-criada sem confirmacao;
- execucao de scripts de skill sem SafetyPolicy;
- marketplace externo;
- dependencia de agentskills.io;
- skill que altere `pocket update` fora do contrato de profile.

---

### Testes obrigatorios

```
TESTE 08-01
dado:    profile/skills/tailscale-debug/SKILL.md com frontmatter valido
quando:  pocket skills list --format json executa
entao:   retorna skill_id=tailscale-debug, status=valid e modes contendo agent

TESTE 08-02
dado:    SKILL.md sem name ou description
quando:  pocket skills validate executa
entao:   retorna ERR_SKILL_REQUIRED_FIELD com campo faltante

TESTE 08-03  [edge case]
dado:    skill declara platforms [macos] e PocketCli roda em linux/iSH
quando:  pocket skills list executa
entao:   skill aparece como incompatible, nao e carregada automaticamente

TESTE 08-04  [seguranca]
dado:    skill tenta declarar tool name "pocket_exec" sobrescrevendo core
quando:  validador executa
entao:   retorna ERR_SKILL_CORE_TOOL_SHADOW e a skill nao entra no toolset
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-08-1
  tipo: cli
  nome: pocket skills list

  entrada:
    - campo: all
      tipo: bool
      obrigatorio: false
      validacao: inclui incompatíveis e disabled
      valores_enum: []
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: SkillListResult
      campos:
        - campo: skills
          tipo: array<PocketSkill>
        - campo: warnings
          tipo: array<PocketError>
    falha:
      valores_possiveis:
        - code: ERR_SKILL_DIR_IO
          quando: profile/skills nao pode ser lido
          acao: retornar erro controlado
        - code: ERR_SKILL_FRONTMATTER_INVALID
          quando: YAML invalido
          acao: marcar skill invalid e continuar listagem

  comportamento_em_falha:
    - condicao: skill individual invalida
      acao: incluir na lista com status=invalid se --all; omitir sem --all
      observavel: warning estruturado
```

```yaml
interface:
  id: IF-08-2
  tipo: cli
  nome: pocket skills adopt

  entrada:
    - campo: draft_path
      tipo: string
      obrigatorio: true
      validacao: path dentro de ~/.pocket/skill-drafts ou profile/skills
      valores_enum: []
    - campo: skill_id
      tipo: string
      obrigatorio: false
      validacao: slug valido
      valores_enum: []

  saida:
    sucesso:
      tipo: SkillAdoptResult
      campos:
        - campo: skill_id
          tipo: string
        - campo: target_path
          tipo: string
        - campo: status
          tipo: enum
    falha:
      valores_possiveis:
        - code: ERR_SKILL_DRAFT_OUTSIDE_ALLOWED_ROOT
          quando: draft_path escapa roots permitidos
          acao: rejeitar
        - code: ERR_SKILL_ALREADY_EXISTS
          quando: skill_id ja existe
          acao: exigir --replace explicito
        - code: ERR_SKILL_VALIDATION_FAILED
          quando: draft nao passa validacao
          acao: nao copiar

  comportamento_em_falha:
    - condicao: target ja existe
      acao: nao sobrescrever sem --replace
      observavel: erro com target_path
```

---

### Estruturas de dados

```yaml
tipo: PocketSkill
campos:
  - nome: skill_id
    tipo: string
    obrigatorio: true
    default: nome do diretorio
    limite: 80 caracteres
  - nome: name
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 120 caracteres
  - nome: description
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 500 caracteres
  - nome: platforms
    tipo: array<string>
    obrigatorio: true
    default: []
    limite: valores conhecidos ou vazio
  - nome: modes
    tipo: array<enum>
    obrigatorio: true
    default: [agent]
    limite: valores [viewer, agent, server]
  - nome: toolsets
    tipo: array<string>
    obrigatorio: true
    default: []
    limite: maximo 20 itens
  - nome: status
    tipo: enum
    obrigatorio: true
    default: valid
    limite: valores [valid, invalid, incompatible, disabled]
```

```yaml
tipo: SkillDraft
campos:
  - nome: draft_id
    tipo: string
    obrigatorio: true
    default: uuid
    limite: 80 caracteres
  - nome: source_session_id
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 80 caracteres
  - nome: proposed_skill_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: content_path
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4096 caracteres
  - nome: created_at
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
```

---

### Regras de comportamento

| Origem da skill | Valida | Compatível | Resultado | Acao |
|-----------------|--------|------------|-----------|------|
| profile/skills | sim | sim | ativa | aparece em list default |
| profile/skills | sim | nao | incompatível | aparece so com --all |
| draft | sim | sim | pendente | precisa adopt |
| draft | nao | qualquer | rejeitada | nao copia |

**Limites numericos explicitos:**
- SKILL.md maximo: 128 KiB
- arquivos auxiliares por skill: 50
- tamanho total por skill: 5 MiB
- skills ativas carregadas por sessao: 20
- tempo maximo de validacao: 5000 ms

---

### Dependencias desta fase

```yaml
- id: DEP-08-1
  tipo: arquivo
  nome: profile/skills
  versao: schema_version >= 1
  papel: skills versionadas do projeto
  fallback: criar diretório vazio
  obrigatoria: true
- id: DEP-08-2
  tipo: componente_interno
  nome: PocketWiki indexer
  versao: atual
  papel: indexar SKILL.md como conhecimento operacional
  fallback: skills funcionam apenas no PocketCli
  obrigatoria: false
- id: DEP-08-3
  tipo: componente_interno
  nome: Toolsets
  versao: fase 05
  papel: declarar capacidades exigidas por skill
  fallback: carregar skill sem tools extras
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] formato `profile/skills/<id>/SKILL.md`
- [ ] `pocket skills list/view/validate/adopt`
- [ ] draft root `~/.pocket/skill-drafts`
- [ ] bloqueio de shadow de core tools
- [ ] testes 08-01 a 08-04 passando

## 3.9 — Fase 09: SafetyPolicy, Aprovacoes e Guardrails

### O que e

Politica comum de seguranca para contexto, tools, MCP, scheduler, fleet e skills. Ela unifica os bloqueios que hoje estao espalhados e importa os aprendizados do Hermes: denylist de arquivos, redacao agressiva, aprovacoes para comandos perigosos, limites de loop e protecao contra prompt injection em superficie de gateway/scheduler.

**Responsabilidades:**
- definir `SafetyPolicy` padrao em `specs/policies/safety-default.json`;
- bloquear leitura/escrita de secrets e stores internos;
- redigir segredos em stdout, stderr, audit, ledger e contexto;
- classificar comandos como allow, approval_required ou deny;
- impedir loops repetidos de tools sem progresso;
- prover `pocket policy check` e mecanismo de approval.

**Fora do escopo deste componente:**
- prometer isolamento contra usuario local malicioso com shell;
- substituir permissoes do sistema operacional;
- implementar sandbox forte;
- auto-aprovar comandos destrutivos;
- guardar segredo no PocketCli.

---

### Testes obrigatorios

```
TESTE 09-01
dado:    input "cat ~/.ssh/id_ed25519"
quando:  pocket policy check --kind command executa
entao:   retorna decision=deny code=ERR_SAFETY_PRIVATE_KEY_READ

TESTE 09-02
dado:    input "sudo systemctl restart nginx"
quando:  pocket policy check --kind command executa
entao:   retorna decision=approval_required e reason contem privileged_command

TESTE 09-03  [edge case]
dado:    mesma tool falha 5 vezes com args_hash identico na mesma sessao
quando:  guardrail before_call avalia a sexta tentativa
entao:   retorna decision=block code=ERR_TOOL_REPEATED_FAILURE

TESTE 09-04  [seguranca]
dado:    texto contem JWT, OpenAI key, GitHub token, Authorization Bearer e URL com password query param
quando:  redactor executa
entao:   nenhum valor bruto aparece na saida e cada substituicao usa [REDACTED] ou ***
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-09-1
  tipo: cli
  nome: pocket policy check

  entrada:
    - campo: kind
      tipo: enum
      obrigatorio: true
      validacao: valor deve estar em valores_enum
      valores_enum: [command, path_read, path_write, tool_call, scheduler_job]
    - campo: value
      tipo: string
      obrigatorio: true
      validacao: 1 a 32768 caracteres
      valores_enum: []
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: SafetyDecision
      campos:
        - campo: decision
          tipo: enum
        - campo: code
          tipo: string
        - campo: reason
          tipo: string
        - campo: approval_required
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_POLICY_INVALID_KIND
          quando: kind invalido
          acao: retornar exit_code=2
        - code: ERR_POLICY_LOAD_FAILED
          quando: policy padrao e override nao carregam
          acao: falhar fechado

  comportamento_em_falha:
    - condicao: override do profile invalido
      acao: ignorar override e usar safety-default se valido
      observavel: warning ERR_POLICY_OVERRIDE_INVALID
```

```yaml
interface:
  id: IF-09-2
  tipo: function
  nome: redactSensitiveText

  entrada:
    - campo: text
      tipo: string
      obrigatorio: true
      validacao: maximo 1 MiB
      valores_enum: []

  saida:
    sucesso:
      tipo: RedactionResult
      campos:
        - campo: text
          tipo: string
        - campo: replacements
          tipo: int
        - campo: patterns
          tipo: array<string>
    falha:
      valores_possiveis:
        - code: ERR_REDACTION_INPUT_TOO_LARGE
          quando: text excede 1 MiB
          acao: truncar para 1 MiB e marcar truncated=true
        - code: ERR_REDACTION_INTERNAL
          quando: regex/redator falha
          acao: falhar fechado para persistencia; retornar erro para CLI

  comportamento_em_falha:
    - condicao: texto binario/controle
      acao: substituir controle por U+FFFD ou ?
      observavel: saida UTF-8 valida
```

---

### Estruturas de dados

```yaml
tipo: SafetyDecision
campos:
  - nome: decision
    tipo: enum
    obrigatorio: true
    default: deny
    limite: valores [allow, approval_required, deny]
  - nome: code
    tipo: string
    obrigatorio: true
    default: ERR_SAFETY_DENIED
    limite: 80 caracteres
  - nome: reason
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 500 caracteres
  - nome: approval_required
    tipo: bool
    obrigatorio: true
    default: false
    limite: nenhum
  - nome: metadata
    tipo: object
    obrigatorio: true
    default: {}
    limite: maximo 32 chaves simples
```

```yaml
tipo: ApprovalRequest
campos:
  - nome: approval_id
    tipo: string
    obrigatorio: true
    default: uuid
    limite: 80 caracteres
  - nome: session_id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 80 caracteres
  - nome: action
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 4000 caracteres redigidos
  - nome: status
    tipo: enum
    obrigatorio: true
    default: pending
    limite: valores [pending, allow_once, allow_always, deny, expired]
  - nome: created_at
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
  - nome: expires_at
    tipo: string
    obrigatorio: true
    default: now+300s
    limite: ISO-8601 UTC
```

---

### Regras de comportamento

| Superficie | Acao sensivel | Sem aprovacao | Com aprovacao allow_once | Com aprovacao deny |
|------------|---------------|---------------|--------------------------|--------------------|
| CLI interativo | command mutante | prompt approval | executa uma vez | bloqueia |
| MCP | command mutante | retorna approval_required | executa se approval_id valido | bloqueia |
| scheduler | command mutante | rejeita criacao | permitido apenas se aprovado no create | bloqueia |
| Viewer | daemon/scheduler | deny | deny | deny |

**Limites numericos explicitos:**
- approval TTL: 300000 ms
- maximo approvals pendentes: 100
- redaction input maximo: 1 MiB
- repeated exact failure warn_after: 2
- repeated exact failure block_after: 5
- same tool failure block_after: 8
- no progress block_after: 5

---

### Dependencias desta fase

```yaml
- id: DEP-09-1
  tipo: arquivo
  nome: specs/policies/safety-default.json
  versao: schema_version >= 1
  papel: politica padrao
  fallback: policy embutida fail-closed
  obrigatoria: true
- id: DEP-09-2
  tipo: componente_interno
  nome: middlewareAuth security/redaction
  versao: atual
  papel: referencia de padroes de redacao
  fallback: implementacao propria no PocketCli
  obrigatoria: false
- id: DEP-09-3
  tipo: componente_interno
  nome: PocketWiki SensitiveFilePolicy
  versao: atual
  papel: referencia de paths bloqueados
  fallback: lista conservadora no PocketCli
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `specs/policies/safety-default.json`
- [ ] `pocket policy check`
- [ ] redactor compartilhado no PocketCli
- [ ] approval store em `~/.pocket/approvals.jsonl`
- [ ] guardrail de repeticao de tool
- [ ] testes 09-01 a 09-04 passando

## 3.10 — Fase 10: Doctor, Avaliacao e Gates de Release

### O que e

Camada de diagnostico e avaliacao para garantir que o Pocket Stack composto realmente funciona e nao so compila. Ela junta o estado dos tres projetos, valida contratos, mede retrieval/contexto e formaliza gates antes de PR/release.

**Responsabilidades:**
- implementar `pocket doctor --ecosystem`;
- validar health de PocketCli, PocketWiki e middlewareAuth;
- rodar probes de contexto, LLM, history, scheduler e fleet em modo dry-run;
- expor relatorio JSON para CI e PocketWiki;
- documentar testes locais e prerequisitos.

**Fora do escopo deste componente:**
- substituir suites de teste dos tres repos;
- rodar testes destrutivos em hosts reais por default;
- chamar LLM paga sem flag explicita;
- abrir PR automaticamente;
- exigir PocketWiki/middlewareAuth para instalar PocketCli basico.

---

### Testes obrigatorios

```
TESTE 10-01
dado:    PocketCli instalado, PocketWiki ausente e middlewareAuth ausente
quando:  pocket doctor --ecosystem --format json executa
entao:   retorna overall=degraded, pocketcli=healthy e componentes ausentes como optional_missing

TESTE 10-02
dado:    middlewareAuth responde /healthz mas token interno ausente
quando:  pocket doctor --ecosystem executa
entao:   health HTTP aparece healthy e auth probe aparece blocked com ERR_MIDDLEWARE_TOKEN_MISSING

TESTE 10-03  [edge case]
dado:    ledger existe mas indice esta ausente
quando:  pocket doctor --ecosystem executa
entao:   reporta history.index=rebuildable e sugere pocket history rebuild sem falhar geral

TESTE 10-04  [seguranca]
dado:    env contem MIDDLEWARE_CLIENT_TOKEN e OPENAI_API_KEY
quando:  doctor imprime text e json
entao:   nenhum valor bruto dos tokens aparece em stdout/stderr
```

Cobrir obrigatoriamente: caminho feliz · falha · edge case · seguranca.

---

### Contratos de interface

```yaml
interface:
  id: IF-10-1
  tipo: cli
  nome: pocket doctor --ecosystem

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]
    - campo: deep
      tipo: bool
      obrigatorio: false
      validacao: roda probes mais lentos sem acao destrutiva
      valores_enum: []
    - campo: allow_network
      tipo: bool
      obrigatorio: false
      validacao: permite chamadas externas; default false
      valores_enum: []

  saida:
    sucesso:
      tipo: DoctorReport
      campos:
        - campo: overall
          tipo: enum
        - campo: checks
          tipo: array<DoctorCheck>
        - campo: next_actions
          tipo: array<string>
    falha:
      valores_possiveis:
        - code: ERR_DOCTOR_INTERNAL
          quando: doctor panica/falha inesperada
          acao: recuperar e emitir report parcial
        - code: ERR_DOCTOR_FORMAT
          quando: format invalido
          acao: retornar exit_code=2

  comportamento_em_falha:
    - condicao: check individual falha
      acao: marcar check=failed e continuar
      observavel: overall degraded ou failed
```

```yaml
interface:
  id: IF-10-2
  tipo: cli
  nome: pocket eval retrieval

  entrada:
    - campo: corpus
      tipo: string
      obrigatorio: true
      validacao: path para JSONL de casos golden
      valores_enum: []
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: valor deve estar em valores_enum
      valores_enum: [text, json]

  saida:
    sucesso:
      tipo: RetrievalEvalReport
      campos:
        - campo: total_cases
          tipo: int
        - campo: mrr_at_10
          tipo: int
        - campo: recall_at_10
          tipo: int
        - campo: passed
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_EVAL_CORPUS_NOT_FOUND
          quando: corpus nao existe
          acao: retornar exit_code=2
        - code: ERR_EVAL_CASE_INVALID
          quando: linha JSONL invalida
          acao: falhar com numero da linha

  comportamento_em_falha:
    - condicao: PocketWiki indisponivel
      acao: marcar provider wiki unavailable e rodar apenas memoria local se permitido
      observavel: report parcial
```

---

### Estruturas de dados

```yaml
tipo: DoctorReport
campos:
  - nome: generated_at
    tipo: string
    obrigatorio: true
    default: now UTC
    limite: ISO-8601 UTC
  - nome: overall
    tipo: enum
    obrigatorio: true
    default: degraded
    limite: valores [healthy, degraded, failed]
  - nome: checks
    tipo: array<DoctorCheck>
    obrigatorio: true
    default: []
    limite: maximo 200 itens
  - nome: next_actions
    tipo: array<string>
    obrigatorio: true
    default: []
    limite: maximo 20 itens
```

```yaml
tipo: DoctorCheck
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: nenhum
    limite: 120 caracteres
  - nome: component_id
    tipo: string
    obrigatorio: true
    default: pocketcli
    limite: 64 caracteres
  - nome: status
    tipo: enum
    obrigatorio: true
    default: unknown
    limite: valores [pass, warn, fail, skipped]
  - nome: code
    tipo: string
    obrigatorio: false
    default: vazio
    limite: 80 caracteres
  - nome: message
    tipo: string
    obrigatorio: true
    default: vazio
    limite: 500 caracteres
  - nome: duration_ms
    tipo: int
    obrigatorio: true
    default: 0
    limite: minimo 0
```

---

### Regras de comportamento

| Check | Obrigatorio para PocketCli basico | Falha altera overall | Acao |
|-------|-----------------------------------|----------------------|------|
| pocketcli build/test docs | sim | failed | reportar fail |
| pocketwiki health | nao | degraded | reportar optional_missing/unreachable |
| middleware health | nao | degraded | reportar optional_missing/unreachable |
| safety policy | sim | failed | falhar fechado |
| history ledger | nao | degraded | sugerir rebuild/repair |

**Limites numericos explicitos:**
- timeout check rapido: 3000 ms
- timeout check deep: 30000 ms
- maximo checks: 200
- maximo next_actions: 20
- eval corpus maximo: 10 MiB
- threshold MRR@10 inicial: 60 de 100
- threshold recall@10 inicial: 70 de 100

---

### Dependencias desta fase

```yaml
- id: DEP-10-1
  tipo: componente_interno
  nome: Ecosystem manifest
  versao: fase 01
  papel: descobrir componentes
  fallback: detectar apenas PocketCli
  obrigatoria: true
- id: DEP-10-2
  tipo: componente_interno
  nome: scripts/run_local_ci.sh
  versao: atual
  papel: fluxo local Go/shell/smoke
  fallback: comandos manuais documentados em README
  obrigatoria: true
- id: DEP-10-3
  tipo: arquivo
  nome: tests/README.md
  versao: atual
  papel: documentar execucao de testes locais
  fallback: atualizar README do projeto
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `pocket doctor --ecosystem`
- [ ] `pocket eval retrieval`
- [ ] report JSON redigido
- [ ] docs de testes novos
- [ ] testes 10-01 a 10-04 passando

## 4. Cenarios de Falha Transversais

### Entrada invalida / malformada

Todo parser de JSON, JSONL, YAML frontmatter, schedule, toolset e policy deve falhar com erro publico fechado. Para arquivos de colecao, uma entrada invalida nao pode apagar ou reescrever o arquivo inteiro sem comando `--repair` explicito. Para entradas vindas de MCP/HTTP, limite de tamanho deve ser validado antes de parse completo.

### Dependencia indisponivel ou lenta

PocketWiki e middlewareAuth sao opcionais para o PocketCli basico. Quando explicitamente solicitados por flag, indisponibilidade vira erro. Quando usados em modo auto, indisponibilidade vira warning e o fluxo continua com capacidades reduzidas. Timeouts padrao: 1500 ms para probes locais, 3000 ms para status/auth, 5000 ms para wiki files, 120000 ms para LLM completion.

### Estado inconsistente entre fontes de dados

Se manifest diz que componente existe mas path nao existe, o snapshot marca `missing` e sugere atualizar `profile/ecosystem.json`. Se ledger tem sessoes que o indice nao reflete, `doctor` marca `history.index=rebuildable`. Se PocketWiki mudou base durante uma chamada, Context Compiler deve registrar `source_snapshot_id` quando disponivel ou marcar `source_version=unknown`.

### Concorrencia / race condition

Writes em JSON/JSONL usam lock por arquivo quando disponivel e escrita atomica. Scheduler usa lock de tick com stale timeout de 120000 ms. Fleet limita concorrencia por run. Approvals usam `approval_id` unico e transicao atomica de pending para decisao terminal. Se lock nao estiver disponivel no filesystem, componente degrada para execucao single-process e reporta warning.

### Exaustao de recurso

Linhas JSONL acima de 256 KiB sao truncadas apos redacao. Outputs de scheduler e fleet sao truncados por limite. ContextBundle respeita 32000 caracteres por default. Doctor e eval limitam corpus em 10 MiB. Em iSH/Viewer, nenhuma fase pode iniciar watcher, scheduler ou servidor residente.

### Seguranca — entrada controlada por ator externo

MCP, HTTP, scheduler, fleet e context compiler tratam entrada como nao confiavel. Paths sao normalizados e resolvidos antes de comparar com denylist. Symlink deve ser resolvido antes de permitir read/write quando o path existe. `.env*`, `.ssh`, `.middleware-state`, stores OAuth, tokens, chaves privadas, `.netrc`, `.npmrc`, `.pypirc`, `.git-credentials`, kubeconfigs, cloud credentials e auth stores sao bloqueados. Erros e outputs passam por redacao antes de persistir ou retornar.

### Timeout em operacao normalmente rapida

Timeout de probe nao deve travar TUI/CLI. Operacoes rapidas que excedem timeout retornam erro controlado com `duration_ms`. Scheduler/fleet registram timeout por unidade de trabalho, nao derrubam a execucao inteira sem registrar resultados parciais.

## 5. Decisoes e Restricoes

| ID | Decisao | Motivo | Reversivel |
|----|---------|--------|------------|
| DEC-01 | PocketCli e o runtime/operador do ecossistema | Ja e o ponto de entrada SSH/Tailscale e precisa continuar leve no iSH | sim |
| DEC-02 | PocketWiki e a memoria/contexto citavel | Ja indexa Markdown/Excalidraw, tem UI, grafo e RFC de knowledge store | sim |
| DEC-03 | middlewareAuth e o broker de auth/modelos | Ja centraliza OAuth, refresh, provider contract e MCP sem expor token ao cliente | sim |
| DEC-04 | Viewer nao roda daemon | Requisito central do PocketCli e do uso em iPad/iSH | nao |
| DEC-05 | Estado operacional novo fica em `~/.pocket` | Evita misturar runtime state com arquivos versionados do projeto | sim |
| DEC-06 | Customizacao versionada fica em `profile/` | Regra existente do projeto e essencial para `pocket update` resiliente | nao |
| DEC-07 | Historico MVP usa JSONL + indice rebuildable, nao SQLite obrigatorio | Mantem portabilidade em Alpine/iSH e evita dependencia CGO | sim |
| DEC-08 | Scheduler e Agent-only | Evita processo residente no Viewer e reduz risco em dispositivo leve | nao |
| DEC-09 | Skills sao adotadas manualmente no MVP | Evita auto-modificacao insegura e mantem controle humano | sim |
| DEC-10 | Hermes e referencia de design, nao dependencia | O stack local tem restricoes e objetivos diferentes | nao |

**Alternativas descartadas:**
- Copiar Hermes como submodulo: descartada porque adiciona runtime Python pesado e nao respeita Viewer/iSH.
- Colocar tudo dentro do PocketWiki: descartada porque auth/modelos e SSH/fleet pertencem melhor a middlewareAuth/PocketCli.
- Colocar tudo dentro do middlewareAuth: descartada porque middleware deve ficar focado em auth/provider e nao virar operador de infra.
- Usar SQLite obrigatorio no PocketCli: descartada no MVP por portabilidade e dependencia externa em ambientes minimos.
- Criar gateway multi-plataforma completo agora: descartada porque o ganho imediato vem de MCP/HTTP local, scheduler e wiki context.
- Permitir cron no Viewer: descartada por violar principio de sem servicos residentes.
- Permitir skills auto-escritas direto em `profile/skills`: descartada por risco de persistir prompt injection como procedimento confiavel.
