# RFC - Pocket Stack Unified Pipeline inspirada no Hermes

**Projeto:** PocketCli + PocketWiki + middlewareAuth
**Data:** 2026-06-08
**Status:** COMPLETO
**Fases:** 10 arquivos a gerar
**Gerado por:** Codex GPT-5, sessao local PocketCli

## 1. Visao Geral

### 1.1 Problema

PocketCli, PocketWiki e middlewareAuth ja tem valor separados. O PocketCli e o cliente operacional SSH/Tailscale/TUI. O PocketWiki e a memoria documental/visual local com Markdown, Excalidraw, grafo, timeline, dashboard e contexto seletivo. O middlewareAuth e o broker de autenticacao/LLM com OAuth, tokens protegidos, HTTP interno e MCP `llm_*`.

O problema nao e falta de capacidade isolada. O problema e que a composicao ainda nao tem contrato de sistema. Sem esse contrato, a integracao tende a virar acoplamento acidental: CLI assumindo que wiki esta viva, wiki assumindo provider especifico, middleware recebendo chamadas sem provenance operacional, ou tudo tentando imitar o Hermes sem preservar as especialidades da stack Pocket.

O Hermes Agent foi analisado como referencia externa em `https://github.com/NousResearch/hermes-agent`, commit verificado `c3055d61857751ad82a2bf9e4f5de5d26a8f2a16`. O que interessa para a stack Pocket: toolsets compostos, state/session pesquisavel, MCP bridge, scheduler, skills, memory providers, guardrails, delegacao, ambientes SSH e event bridge. O diferencial Pocket deve ser outro: cada app permanece independente e, quando combinados, formam uma pipeline local-first melhor para infraestrutura pessoal/self-hosted, wiki visual, Tailscale/SSH, iPad/iSH e auth centralizado.

### 1.2 Solucao

Definir uma Pocket Stack Unified Pipeline opcional: apps independentes publicam capabilities e context packs, middlewareAuth oferece provider LLM seguro, PocketWiki oferece conhecimento/provenance, PocketCli executa e observa a operacao, e uma camada de composicao coordena tudo por contratos locais degradaveis.

### 1.3 Fora de escopo

- Fundir os tres projetos em um monorepo obrigatorio.
- Tornar PocketWiki dependencia obrigatoria do PocketCli.
- Tornar middlewareAuth dependencia obrigatoria do PocketWiki ou PocketCli.
- Expor middlewareAuth fora de localhost por padrao.
- Criar uma copia do Hermes Agent.
- Implementar mensageria Telegram/Discord nesta entrega.
- Permitir que LLM escreva direto na wiki sem aprovacao.
- Sincronizar segredos, OAuth tokens ou `.env`.
- Rodar scheduler always-on em dispositivos viewer como iPad/iSH.

## 2. Mapa de Fases

| Fase | Arquivo | Nome do componente | Depende de | Paralelizavel com |
|------|---------|--------------------|------------|-------------------|
| 01 | 01_app_capability_contract.md | App Capability Contract e Autonomia | - | - |
| 02 | 02_discovery_health_mesh.md | Discovery Local e Health Mesh | 01 | - |
| 03 | 03_identity_provider_broker.md | Identity e LLM Provider Broker | 01, 02 | 04 |
| 04 | 04_context_pack_protocol.md | Context Pack Protocol e Provenance | 01, 02 | 03 |
| 05 | 05_unified_session_ledger.md | Unified Session Ledger e Event Bridge | 01, 02, 03, 04 | - |
| 06 | 06_toolsets_mcp_bridge.md | Toolsets Compostos e MCP Bridge Pocket | 01, 03, 04, 05 | - |
| 07 | 07_pipeline_planner_scheduler.md | Pipeline Planner e Scheduler Auditavel | 05, 06 | 08 |
| 08 | 08_fleet_delegation_lane.md | Fleet/Delegation Execution Lane | 05, 06 | 07 |
| 09 | 09_skills_feedback_memory.md | Skills, Feedback e Procedural Memory | 04, 05, 06 | 07, 08 |
| 10 | 10_stack_doctor_release_gates.md | Stack Doctor, Eval e Release Gates | 01, 02, 03, 04, 05, 06, 07, 08, 09 | - |

### Convencoes globais

- Cada app deve ter modo standalone documentado e testado.
- Integracao entre apps e opt-in e descoberta por capabilities.
- Falha de um app nunca deve derrubar os outros; deve degradar com status observavel.
- Todos os contratos de rede local usam HTTP JSON ou MCP stdio/local com auth explicita.
- Nenhum app pode receber access token bruto de provider; middlewareAuth e dono do ciclo de auth.
- Context packs carregam provenance e limites; resposta sem fonte deve ser marcada como sem evidencia.
- Eventos cross-app usam `correlation_id` e `origin_app`.
- PII e segredos sao redigidos antes de sair do app que os detectou.
- Timeouts sao em milissegundos.

### Definition of Done global

- [ ] PocketCli passa seus testes standalone sem PocketWiki e sem middlewareAuth
- [ ] PocketWiki passa seus testes standalone sem PocketCli e sem middlewareAuth
- [ ] middlewareAuth passa seus testes standalone sem PocketCli e sem PocketWiki
- [ ] pipeline integrada funciona quando os tres apps estao saudaveis
- [ ] pipeline integrada degrada corretamente com qualquer um dos apps offline
- [ ] nenhum token OAuth ou segredo bruto aparece em ledger, context pack ou log
- [ ] contratos de health/capabilities/context/toolsets tem fixtures versionadas
- [ ] documentacao inclui como rodar testes locais e variaveis obrigatorias

## 3.1 - Fase 01: App Capability Contract e Autonomia

### O que e

Contrato comum para cada app publicar quem e, o que faz, quais endpoints/commands oferece e qual e seu modo standalone. Esta fase e a trava arquitetural principal: a stack so compoe capacidades que existem, mas nenhum app passa a depender da presenca dos outros.

**Responsabilidades:**
- Definir `PocketAppManifest` para PocketCli, PocketWiki e middlewareAuth.
- Publicar health, versao, capacidades e modo standalone.
- Declarar quais capacidades sao locais, opcionais, remotas ou sensiveis.
- Declarar degradation behavior por app.
- Criar fixtures de manifest para os tres apps.

**Fora do escopo deste componente:**
- Fazer discovery na rede.
- Executar pipeline.
- Chamar LLM.
- Ler conteudo da wiki.

---

### Testes obrigatorios

```
TESTE 01-01
dado:    PocketCli instalado sem PocketWiki e sem middlewareAuth
quando:  `pocket stack manifest --json` roda
entao:   retorna app_id="pocketcli", standalone_ready=true e dependencies_required=[]

TESTE 01-02
dado:    PocketWiki rodando sem MIDDLEWARE_CLIENT_TOKEN
quando:  `/api/stack/manifest` e consultado
entao:   retorna standalone_ready=true e capabilities.llm_remote.status="unavailable"

TESTE 01-03  [edge case]
dado:    middlewareAuth com provider nao autenticado
quando:  manifest e gerado
entao:   retorna service_ready=true e providers.openai.authenticated=false

TESTE 01-04  [seguranca]
dado:    ambiente contendo MIDDLEWARE_CLIENT_TOKEN
quando:  manifest e publicado
entao:   token nao aparece em nenhum campo e auth aparece apenas como status booleano
```

---

### Contratos de interface

```yaml
interface:
  id: IF-01-1
  tipo: cli
  nome: pocket stack manifest

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a json"
      valores_enum: ["json", "text"]

  saida:
    sucesso:
      tipo: PocketAppManifest
      campos:
        - campo: app_id
          tipo: enum
        - campo: standalone_ready
          tipo: bool
        - campo: capabilities
          tipo: array<PocketAppCapability>
    falha:
      valores_possiveis:
        - code: ERR_MANIFEST_RUNTIME
          quando: "runtime local nao pode detectar estado"
          acao: "retorna manifesto parcial com standalone_ready=false"
        - code: ERR_MANIFEST_SCHEMA
          quando: "manifesto gerado viola schema"
          acao: "retorna erro e nao publica cache"

  comportamento_em_falha:
    - condicao: "capacidade opcional falha"
      acao: "marcar status=unavailable"
      observavel: "manifesto ainda retorna exit 0"
```

```yaml
interface:
  id: IF-01-2
  tipo: http
  nome: GET /api/stack/manifest

  entrada:
    - campo: accept
      tipo: string
      obrigatorio: false
      validacao: "application/json ou vazio"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketAppManifest
      campos:
        - campo: app_id
          tipo: enum
        - campo: service_ready
          tipo: bool
        - campo: capabilities
          tipo: array<PocketAppCapability>
    falha:
      valores_possiveis:
        - code: ERR_HTTP_METHOD
          quando: "metodo diferente de GET"
          acao: "retornar 405"
        - code: ERR_MANIFEST_INTERNAL
          quando: "erro inesperado"
          acao: "retornar 500 sem segredo"

  comportamento_em_falha:
    - condicao: "provider opcional indisponivel"
      acao: "status unavailable"
      observavel: "HTTP 200 com capability degradada"
```

---

### Estruturas de dados

```yaml
tipo: PocketAppManifest
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: "valor fixo 1"
  - nome: app_id
    tipo: enum
    obrigatorio: true
    default: "pocketcli"
    limite: "pocketcli|pocketwiki|middlewareauth"
  - nome: app_version
    tipo: string
    obrigatorio: true
    default: "dev"
    limite: "80 chars"
  - nome: generated_at
    tipo: string
    obrigatorio: true
    default: "agora UTC"
    limite: "RFC3339 UTC"
  - nome: standalone_ready
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: service_ready
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: capabilities
    tipo: array<PocketAppCapability>
    obrigatorio: true
    default: "[]"
    limite: "max 80 capabilities"
  - nome: dependencies_required
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 20 itens"
```

```yaml
tipo: PocketAppCapability
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
    limite: "80 chars"
  - nome: kind
    tipo: enum
    obrigatorio: true
    default: "local"
    limite: "local|http|mcp|cli|sensitive|optional"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "available"
    limite: "available|unavailable|degraded|requires_auth"
  - nome: endpoint
    tipo: string
    obrigatorio: false
    default: ""
    limite: "300 chars"
  - nome: requires_auth
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| app sem deps opcionais | core ok | standalone_ready=true | publicar manifest |
| deps opcionais ausentes | core ok | service_ready=true | marcar capability unavailable |
| token presente | qualquer | nao expor valor | expor somente requires_auth/status |
| schema invalido | qualquer | erro | nao cachear manifest |

**Limites numericos explicitos:**
- timeout de manifest CLI: 2000 ms
- timeout de manifest HTTP: 2000 ms
- max capabilities: 80
- max endpoint string: 300 chars
- ttl cache: 60 s

---

### Dependencias desta fase

```yaml
- id: DEP-01-1
  tipo: componente_interno
  nome: PocketCli capabilities
  versao: "schema 1"
  papel: "manifesto do CLI"
  fallback: "manifesto minimo"
  obrigatoria: true
- id: DEP-01-2
  tipo: componente_interno
  nome: PocketWiki HTTP server
  versao: "rotas /api existentes"
  papel: "manifesto HTTP do app"
  fallback: "standalone sem composicao"
  obrigatoria: true
- id: DEP-01-3
  tipo: componente_interno
  nome: middlewareAuth health/MCP
  versao: "llm_* atual"
  papel: "manifesto de auth/provider"
  fallback: "provider unavailable"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] manifesto PocketCli
- [ ] manifesto PocketWiki
- [ ] manifesto middlewareAuth
- [ ] fixtures validas dos tres manifests
- [ ] todos os testes 01-01 a 01-04 passando

## 3.2 - Fase 02: Discovery Local e Health Mesh

### O que e

Camada de descoberta local para encontrar apps Pocket sem configuracao fragil. Ela combina configuracao explicita, localhost, mDNS/Tailscale do PocketWiki e health do middlewareAuth. Discovery nunca deve virar dependencia obrigatoria; ele so melhora a composicao quando os apps estao vivos.

**Responsabilidades:**
- Descobrir PocketWiki por config, localhost, `/api/routes`, LAN e Tailscale.
- Descobrir middlewareAuth por `MIDDLEWARE_BASE_URL` ou default `http://localhost:18787`.
- Descobrir PocketCli local por comando.
- Publicar `pocket stack status --json`.
- Cachear health snapshots com TTL curto.

**Fora do escopo deste componente:**
- Abrir portas no firewall.
- Fazer login Tailscale.
- Expor middlewareAuth em rede.
- Garantir mDNS atravessando VLAN/Tailscale.

---

### Testes obrigatorios

```
TESTE 02-01
dado:    PocketWiki em http://localhost e middlewareAuth em localhost:18787
quando:  `pocket stack status --json` roda
entao:   retorna apps pocketcli,pocketwiki,middlewareauth com status healthy

TESTE 02-02
dado:    PocketWiki offline
quando:  stack status roda
entao:   retorna pocketwiki status=offline e pocketcli status=healthy

TESTE 02-03  [edge case]
dado:    POCKETWIKI_BASE_URL apontando para URL invalida
quando:  discovery roda
entao:   retorna ERR_DISCOVERY_BAD_URL para aquela fonte e continua localhost

TESTE 02-04  [seguranca]
dado:    URL descoberta com host fora de allowlist local/tailscale
quando:  discovery avalia
entao:   rejeita target com ERR_DISCOVERY_TARGET_BLOCKED
```

---

### Contratos de interface

```yaml
interface:
  id: IF-02-1
  tipo: cli
  nome: pocket stack status

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "omitido equivale a text"
      valores_enum: ["text", "json"]
    - campo: refresh
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketStackStatus
      campos:
        - campo: apps
          tipo: array<PocketAppStatus>
        - campo: composition_ready
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_DISCOVERY_CONFIG
          quando: "config local malformada"
          acao: "ignorar config e tentar defaults"
        - code: ERR_DISCOVERY_TIMEOUT
          quando: "health mesh excede timeout"
          acao: "retornar parcial"

  comportamento_em_falha:
    - condicao: "um app offline"
      acao: "marcar offline"
      observavel: "exit 0 com composition_ready=false"
```

```yaml
interface:
  id: IF-02-2
  tipo: function
  nome: DiscoverPocketApps

  entrada:
    - campo: request
      tipo: PocketDiscoveryRequest
      obrigatorio: true
      validacao: "timeout_ms 100..10000"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketStackStatus
      campos:
        - campo: apps
          tipo: array<PocketAppStatus>
        - campo: checked_at
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_DISCOVERY_BAD_URL
          quando: "URL invalida"
          acao: "pular fonte"
        - code: ERR_DISCOVERY_TARGET_BLOCKED
          quando: "host nao permitido"
          acao: "pular target"

  comportamento_em_falha:
    - condicao: "fonte falha"
      acao: "continuar outras fontes"
      observavel: "warnings preenchido"
```

---

### Estruturas de dados

```yaml
tipo: PocketDiscoveryRequest
campos:
  - nome: timeout_ms
    tipo: int
    obrigatorio: true
    default: 2000
    limite: "100..10000"
  - nome: refresh
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: allow_tailscale
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
```

```yaml
tipo: PocketStackStatus
campos:
  - nome: checked_at
    tipo: string
    obrigatorio: true
    default: "agora UTC"
    limite: "RFC3339 UTC"
  - nome: composition_ready
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: apps
    tipo: array<PocketAppStatus>
    obrigatorio: true
    default: "[]"
    limite: "max 10 apps"
  - nome: warnings
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 20 warnings"
```

```yaml
tipo: PocketAppStatus
campos:
  - nome: app_id
    tipo: enum
    obrigatorio: true
    default: "pocketcli"
    limite: "pocketcli|pocketwiki|middlewareauth"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "offline"
    limite: "healthy|degraded|offline|blocked"
  - nome: base_url
    tipo: string
    obrigatorio: false
    default: ""
    limite: "300 chars"
  - nome: latency_ms
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..10000"
  - nome: manifest
    tipo: PocketAppManifest
    obrigatorio: false
    default: "nenhum"
    limite: "manifesto max 64 KB"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| app obrigatorio standalone local ok | outros offline | composition_ready=false | manter standalone |
| pocketwiki healthy | middleware offline | composicao parcial | context wiki sem LLM remoto |
| middleware healthy | pocketwiki offline | composicao parcial | LLM broker sem wiki context |
| URL host publico | nao allowlisted | blocked | nao consultar |
| timeout app | qualquer | degraded/offline | cachear status curto |

**Limites numericos explicitos:**
- timeout por app: 2000 ms
- timeout total discovery: 5000 ms
- ttl cache health: 30 s
- max URLs candidatas por app: 8
- max warnings: 20

---

### Dependencias desta fase

```yaml
- id: DEP-02-1
  tipo: componente_interno
  nome: Fase 01 App Capability Contract
  versao: "schema 1"
  papel: "validar apps encontrados"
  fallback: "status sem manifest"
  obrigatoria: true
- id: DEP-02-2
  tipo: servico_externo
  nome: PocketWiki /api/routes
  versao: "atual"
  papel: "descoberta LAN/Tailscale"
  fallback: "localhost/config explicita"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket stack status --json`
- [ ] discovery PocketWiki por config e localhost
- [ ] discovery middlewareAuth por env/default
- [ ] allowlist local/tailscale
- [ ] todos os testes 02-01 a 02-04 passando

## 3.3 - Fase 03: Identity e LLM Provider Broker

### O que e

Integracao formal do middlewareAuth como broker de provedores LLM para a stack. Ele continua standalone e dono de auth/tokens. PocketCli e PocketWiki passam a consumir `llm_*` quando disponivel, mas mantem seus backends locais/fallbacks.

**Responsabilidades:**
- Padronizar project_id/profile_id/provider_id/model na stack.
- Usar `llm_providers`, `llm_status`, `llm_refresh` e `llm_responses`.
- Impedir que tokens brutos saiam do middlewareAuth.
- Registrar provenance da chamada sem registrar segredo.
- Definir fallback quando provider nao autenticado.

**Fora do escopo deste componente:**
- Implementar OAuth no PocketCli ou PocketWiki.
- Remover LM Studio do PocketWiki.
- Remover backend local/remoto existente do PocketCli.
- Expor middlewareAuth fora de loopback por padrao.

---

### Testes obrigatorios

```
TESTE 03-01
dado:    middlewareAuth healthy e provider openai autenticado
quando:  PocketCli chama broker com input simples
entao:   recebe resposta e ledger registra provider_id=openai sem token

TESTE 03-02
dado:    middlewareAuth offline
quando:  PocketWiki solicita modelos remotos
entao:   UI/API retorna provider_remote.status=unavailable e LM Studio local continua elegivel

TESTE 03-03  [edge case]
dado:    provider autenticado mas model vazio
quando:  request passa pelo broker
entao:   usa default model do provider manifest

TESTE 03-04  [seguranca]
dado:    resposta de erro HTTP contendo Authorization header
quando:  erro e logado
entao:   header e redigido antes de aparecer em logs/eventos
```

---

### Contratos de interface

```yaml
interface:
  id: IF-03-1
  tipo: mcp
  nome: llm_responses

  entrada:
    - campo: request
      tipo: PocketLLMRequest
      obrigatorio: true
      validacao: "provider_id e project_id nao vazios"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketLLMResponse
      campos:
        - campo: provider_id
          tipo: string
        - campo: model
          tipo: string
        - campo: content
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_LLM_PROVIDER_OFFLINE
          quando: "middlewareAuth ou provider indisponivel"
          acao: "retornar fallback_required=true"
        - code: ERR_LLM_AUTH_REQUIRED
          quando: "profile sem credencial valida"
          acao: "retornar login_hint sem segredo"
        - code: ERR_LLM_RESPONSE_FAILED
          quando: "provider retorna erro"
          acao: "retornar erro redigido"

  comportamento_em_falha:
    - condicao: "auth ausente"
      acao: "nao tentar token direto no cliente"
      observavel: "ERR_LLM_AUTH_REQUIRED"
```

```yaml
interface:
  id: IF-03-2
  tipo: http
  nome: POST /v1/projects/{projectId}/codex/responses

  entrada:
    - campo: request
      tipo: PocketLLMRequest
      obrigatorio: true
      validacao: "Authorization Bearer obrigatorio no middleware"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketLLMResponse
      campos:
        - campo: content
          tipo: string
        - campo: usage
          tipo: PocketLLMUsage
    falha:
      valores_possiveis:
        - code: ERR_HTTP_UNAUTHORIZED
          quando: "client token ausente/invalido"
          acao: "retornar 401 sem detalhe de token"
        - code: ERR_PROVIDER_TIMEOUT
          quando: "provider excede timeout"
          acao: "retornar erro recuperavel"

  comportamento_em_falha:
    - condicao: "HTTP 401"
      acao: "cliente marca broker unavailable"
      observavel: "fallback para backend local quando existir"
```

---

### Estruturas de dados

```yaml
tipo: PocketLLMRequest
campos:
  - nome: provider_id
    tipo: string
    obrigatorio: true
    default: "openai"
    limite: "80 chars"
  - nome: project_id
    tipo: string
    obrigatorio: true
    default: "default"
    limite: "120 chars"
  - nome: profile_id
    tipo: string
    obrigatorio: true
    default: "default"
    limite: "120 chars"
  - nome: model
    tipo: string
    obrigatorio: false
    default: ""
    limite: "160 chars"
  - nome: intelligence
    tipo: string
    obrigatorio: false
    default: ""
    limite: "80 chars"
  - nome: reasoning_effort
    tipo: string
    obrigatorio: false
    default: ""
    limite: "80 chars"
  - nome: input
    tipo: array<PocketLLMMessage>
    obrigatorio: true
    default: "[]"
    limite: "max 32 mensagens"
```

```yaml
tipo: PocketLLMMessage
campos:
  - nome: role
    tipo: enum
    obrigatorio: true
    default: "user"
    limite: "system|user|assistant"
  - nome: content
    tipo: string
    obrigatorio: true
    default: ""
    limite: "64000 chars"
```

```yaml
tipo: PocketLLMResponse
campos:
  - nome: provider_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: model
    tipo: string
    obrigatorio: true
    default: ""
    limite: "160 chars"
  - nome: content
    tipo: string
    obrigatorio: true
    default: ""
    limite: "262144 chars"
  - nome: usage
    tipo: PocketLLMUsage
    obrigatorio: true
    default: "zeros"
    limite: "nenhum"
```

```yaml
tipo: PocketLLMUsage
campos:
  - nome: input_tokens
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..10000000"
  - nome: output_tokens
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..10000000"
  - nome: latency_ms
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..600000"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| middleware offline | app tem local LLM | fallback local | marcar provider_remote unavailable |
| middleware offline | app sem fallback | erro recuperavel | mostrar login/status hint |
| auth required | qualquer | nao chamar provider | retornar login_hint |
| model vazio | providers default existe | usar default | registrar model resolvido |
| token em erro | qualquer | redigido | nunca persistir token |

**Limites numericos explicitos:**
- timeout health broker: 2000 ms
- timeout llm_responses default: 60000 ms
- max input messages: 32
- max input total chars: 128000
- max erro redigido: 4000 chars

---

### Dependencias desta fase

```yaml
- id: DEP-03-1
  tipo: componente_interno
  nome: middlewareAuth llm_* MCP
  versao: "2025-11-25 MCP"
  papel: "broker LLM generico"
  fallback: "backend local de cada app"
  obrigatoria: true
- id: DEP-03-2
  tipo: variavel_de_ambiente
  nome: MIDDLEWARE_CLIENT_TOKEN
  versao: "32+ bytes recomendado"
  papel: "auth cliente para HTTP/MCP"
  fallback: "provider remoto unavailable"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] adapter PocketCli para broker opcional
- [ ] adapter PocketWiki para broker opcional preservando LM Studio
- [ ] redaction de erro/token
- [ ] fixtures de provider authenticated/unauthenticated/offline
- [ ] todos os testes 03-01 a 03-04 passando

## 3.4 - Fase 04: Context Pack Protocol e Provenance

### O que e

Protocolo comum para trocar contexto entre PocketCli e PocketWiki. O PocketWiki entrega contexto documental/visual com paths relativos e evidencia; o PocketCli entrega contexto operacional/projeto/host; o pipeline compila ambos sem misturar fonte com instrucao do usuario.

**Responsabilidades:**
- Definir `PocketContextPack`.
- Permitir PocketWiki gerar context pack por pergunta, pagina, tag ou path.
- Permitir PocketCli gerar context pack operacional.
- Preservar provenance: path relativo, tipo de fonte, score e excerpt.
- Impedir leitura de arquivos sensiveis.

**Fora do escopo deste componente:**
- Fazer embedding obrigatorio.
- Permitir escrita na wiki.
- Usar path absoluto como fonte exibida.
- Inventar fonte quando a wiki nao retorna evidencia.

---

### Testes obrigatorios

```
TESTE 04-01
dado:    PocketWiki com duas paginas relevantes para "tailscale"
quando:  `/api/context-pack?query=tailscale` e chamado
entao:   retorna pack com sources contendo paths relativos e excerpts

TESTE 04-02
dado:    PocketWiki sem pagina relevante
quando:  context pack e gerado
entao:   retorna sources=[] e notice="sem evidencia suficiente"

TESTE 04-03  [edge case]
dado:    Excalidraw com textos e relacoes
quando:  context pack e gerado
entao:   inclui source_type=excalidraw e excerpt textual limitado

TESTE 04-04  [seguranca]
dado:    query tentando acessar `../../.ssh/id_rsa`
quando:  context pack e chamado
entao:   retorna ERR_CONTEXT_SOURCE_BLOCKED e nao le arquivo
```

---

### Contratos de interface

```yaml
interface:
  id: IF-04-1
  tipo: http
  nome: GET /api/context-pack

  entrada:
    - campo: query
      tipo: string
      obrigatorio: true
      validacao: "1..1000 chars"
      valores_enum: []
    - campo: max_sources
      tipo: int
      obrigatorio: false
      validacao: "1..20"
      valores_enum: []
    - campo: max_chars
      tipo: int
      obrigatorio: false
      validacao: "1000..64000"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketContextPack
      campos:
        - campo: sources
          tipo: array<PocketContextSource>
        - campo: body
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_CONTEXT_QUERY_INVALID
          quando: "query vazia ou longa demais"
          acao: "retornar 400"
        - code: ERR_CONTEXT_SOURCE_BLOCKED
          quando: "path sensivel ou traversal"
          acao: "bloquear leitura"
        - code: ERR_CONTEXT_WIKI_UNLOADED
          quando: "wiki sem base carregada"
          acao: "retornar pack vazio com notice"

  comportamento_em_falha:
    - condicao: "sem evidencia"
      acao: "retornar sources vazias"
      observavel: "notice preenchido"
```

```yaml
interface:
  id: IF-04-2
  tipo: cli
  nome: pocket context-pack

  entrada:
    - campo: query
      tipo: string
      obrigatorio: true
      validacao: "1..1000 chars"
      valores_enum: []
    - campo: include_wiki
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []
    - campo: include_ops
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketContextPack
      campos:
        - campo: pack_id
          tipo: string
        - campo: sources
          tipo: array<PocketContextSource>
    falha:
      valores_possiveis:
        - code: ERR_CONTEXT_PACK_EMPTY
          quando: "include_wiki=false e include_ops=false"
          acao: "retornar erro"
        - code: ERR_CONTEXT_WIKI_OFFLINE
          quando: "include_wiki=true e wiki offline"
          acao: "retornar pack operacional parcial"

  comportamento_em_falha:
    - condicao: "wiki offline"
      acao: "usar contexto PocketCli"
      observavel: "pack.partial=true"
```

---

### Estruturas de dados

```yaml
tipo: PocketContextPack
campos:
  - nome: schema_version
    tipo: int
    obrigatorio: true
    default: 1
    limite: "valor fixo 1"
  - nome: pack_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: origin_app
    tipo: enum
    obrigatorio: true
    default: "pocketcli"
    limite: "pocketcli|pocketwiki|pipeline"
  - nome: query
    tipo: string
    obrigatorio: true
    default: ""
    limite: "1000 chars"
  - nome: body
    tipo: string
    obrigatorio: true
    default: ""
    limite: "64000 chars"
  - nome: sources
    tipo: array<PocketContextSource>
    obrigatorio: true
    default: "[]"
    limite: "max 20 sources"
  - nome: partial
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: notice
    tipo: string
    obrigatorio: false
    default: ""
    limite: "300 chars"
```

```yaml
tipo: PocketContextSource
campos:
  - nome: source_id
    tipo: string
    obrigatorio: true
    default: "fingerprint"
    limite: "128 chars"
  - nome: source_type
    tipo: enum
    obrigatorio: true
    default: "markdown"
    limite: "markdown|excalidraw|graph|timeline|cli_context|ledger|memory|host"
  - nome: path
    tipo: string
    obrigatorio: false
    default: ""
    limite: "path relativo 512 chars"
  - nome: score
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..100"
  - nome: excerpt
    tipo: string
    obrigatorio: true
    default: ""
    limite: "4000 chars"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| source path absoluto | pocketwiki source | erro | bloquear pack |
| source sem score | qualquer | score=0 | manter com baixa prioridade |
| query conversa comum | wiki automatic | pack vazio | nao forcar wiki |
| Excalidraw parse ok | texto extraido | source_type=excalidraw | incluir excerpt |
| max_chars excedido | qualquer | truncar por score | partial=true |

**Limites numericos explicitos:**
- max query chars: 1000
- max sources default: 8
- max sources absoluto: 20
- max body chars: 64000
- max excerpt por source: 4000
- timeout context pack HTTP: 5000 ms

---

### Dependencias desta fase

```yaml
- id: DEP-04-1
  tipo: componente_interno
  nome: PocketWiki LocalAIContextBuilder
  versao: "atual"
  papel: "selecao de paginas e contexto"
  fallback: "pack vazio com notice"
  obrigatoria: true
- id: DEP-04-2
  tipo: componente_interno
  nome: PocketCli contextcollector
  versao: "atual"
  papel: "contexto operacional"
  fallback: "pack com cwd/sistema"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `/api/context-pack` no PocketWiki
- [ ] `pocket context-pack`
- [ ] provenance por source
- [ ] bloqueio de path sensivel/traversal
- [ ] todos os testes 04-01 a 04-04 passando

## 3.5 - Fase 05: Unified Session Ledger e Event Bridge

### O que e

Ledger unificado para correlacionar eventos dos tres apps sem criar dependencia central obrigatoria. Cada app continua com seus logs locais; a pipeline pode agregar eventos por `correlation_id` quando a composicao esta ativa. Inspiracao do Hermes: session store pesquisavel e event bridge, mas sem exigir SQLite global.

**Responsabilidades:**
- Definir `PocketStackEvent`.
- Correlacionar ask, context pack, LLM call, action, SSH run e wiki source.
- Expor `pocket stack events search`.
- Permitir event bridge polling local.
- Redigir payloads antes de agregacao.

**Fora do escopo deste componente:**
- Armazenar transcript completo por padrao.
- Substituir logs internos de cada app.
- Criar broker de mensageria remoto.
- Persistir segredo ou token.

---

### Testes obrigatorios

```
TESTE 05-01
dado:    pipeline executa pergunta com wiki context e LLM broker
quando:  eventos sao pesquisados por correlation_id
entao:   aparecem eventos context.pack.created, llm.call.completed e cli.answer.completed

TESTE 05-02
dado:    PocketWiki offline no meio da pipeline
quando:  event bridge registra falha
entao:   evento app.unavailable e gravado e pipeline continua sem wiki

TESTE 05-03  [edge case]
dado:    dois apps geram eventos com mesmo timestamp
quando:  search ordena resultado
entao:   desempata por sequence e event_id

TESTE 05-04  [seguranca]
dado:    evento middleware contem email de conta
quando:  evento e agregado
entao:   email e redigido ou reduzido conforme politica configurada
```

---

### Contratos de interface

```yaml
interface:
  id: IF-05-1
  tipo: event
  nome: PocketStackEvent

  entrada:
    - campo: event
      tipo: PocketStackEvent
      obrigatorio: true
      validacao: "schema_version=1, origin_app valido, type valido"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketStackEventAppendResult
      campos:
        - campo: event_id
          tipo: string
        - campo: accepted
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_STACK_EVENT_SCHEMA
          quando: "evento invalido"
          acao: "rejeitar"
        - code: ERR_STACK_EVENT_REDACTION
          quando: "payload nao pode ser redigido com seguranca"
          acao: "persistir evento minimo sem payload"

  comportamento_em_falha:
    - condicao: "payload sensivel"
      acao: "redigir ou remover payload"
      observavel: "redaction_count > 0"
```

```yaml
interface:
  id: IF-05-2
  tipo: cli
  nome: pocket stack events search

  entrada:
    - campo: correlation_id
      tipo: string
      obrigatorio: false
      validacao: "max 80 chars"
      valores_enum: []
    - campo: origin_app
      tipo: enum
      obrigatorio: false
      validacao: "valor vazio ou app valido"
      valores_enum: ["pocketcli", "pocketwiki", "middlewareauth", "pipeline"]
    - campo: limit
      tipo: int
      obrigatorio: false
      validacao: "1..500"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketStackEventSearchResult
      campos:
        - campo: events
          tipo: array<PocketStackEvent>
        - campo: truncated
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_STACK_EVENT_BAD_FILTER
          quando: "filtro invalido"
          acao: "retornar erro"
        - code: ERR_STACK_EVENT_STORE_UNAVAILABLE
          quando: "store nao pode ser lido"
          acao: "retornar erro recuperavel"

  comportamento_em_falha:
    - condicao: "store parcial"
      acao: "retornar eventos validos"
      observavel: "partial=true"
```

---

### Estruturas de dados

```yaml
tipo: PocketStackEvent
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
  - nome: correlation_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "80 chars"
  - nome: origin_app
    tipo: enum
    obrigatorio: true
    default: "pipeline"
    limite: "pocketcli|pocketwiki|middlewareauth|pipeline"
  - nome: type
    tipo: enum
    obrigatorio: true
    default: "pipeline.started"
    limite: "pipeline.started|pipeline.completed|app.unavailable|context.pack.created|llm.call.started|llm.call.completed|cli.action.started|cli.action.completed|wiki.source.used|safety.denied"
  - nome: timestamp
    tipo: string
    obrigatorio: true
    default: "agora UTC"
    limite: "RFC3339 UTC"
  - nome: sequence
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..1000000"
  - nome: payload
    tipo: PocketStackEventPayload
    obrigatorio: true
    default: "campos vazios"
    limite: "32 KB redigido"
```

```yaml
tipo: PocketStackEventPayload
campos:
  - nome: summary
    tipo: string
    obrigatorio: false
    default: ""
    limite: "1000 chars"
  - nome: ref
    tipo: string
    obrigatorio: false
    default: ""
    limite: "300 chars"
  - nome: status
    tipo: enum
    obrigatorio: false
    default: "ok"
    limite: "ok|error|partial|denied|timeout"
  - nome: redaction_count
    tipo: int
    obrigatorio: true
    default: 0
    limite: "0..1000"
```

```yaml
tipo: PocketStackEventAppendResult
campos:
  - nome: event_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "36 chars"
  - nome: accepted
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
```

```yaml
tipo: PocketStackEventSearchResult
campos:
  - nome: events
    tipo: array<PocketStackEvent>
    obrigatorio: true
    default: "[]"
    limite: "max 500 events"
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

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| event sem correlation_id | pipeline ativo | gerar novo | propagar para proximos passos |
| event de app offline | qualquer | accepted | type=app.unavailable |
| payload > 32 KB | qualquer | truncar | partial=true |
| segredo detectado | qualquer | redigir | incrementar redaction_count |
| timestamp igual | sequence igual | desempatar event_id | ordem deterministica |

**Limites numericos explicitos:**
- max payload event: 32 KB
- max eventos search: 500
- retencao default stack events: 30 dias
- polling interval event bridge: 500 ms
- queue em memoria: 1000 eventos

---

### Dependencias desta fase

```yaml
- id: DEP-05-1
  tipo: componente_interno
  nome: PocketCli Event Ledger
  versao: "schema 1"
  papel: "store local inicial"
  fallback: "JSONL stack separado"
  obrigatoria: true
- id: DEP-05-2
  tipo: componente_interno
  nome: Fase 04 Context Pack Protocol
  versao: "schema 1"
  papel: "eventos de contexto/provenance"
  fallback: "eventos sem source detalhado"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] schema `PocketStackEvent`
- [ ] `pocket stack events search`
- [ ] correlation_id em pipeline integrada
- [ ] redaction central
- [ ] todos os testes 05-01 a 05-04 passando

## 3.6 - Fase 06: Toolsets Compostos e MCP Bridge Pocket

### O que e

Camada que agrupa capacidades da stack em toolsets claros, semelhante ao Hermes, mas com fronteiras Pocket. Um cliente MCP ou agente local pode pedir `pocket_ops`, `pocket_wiki`, `pocket_llm`, `pocket_full`, mas cada toolset respeita disponibilidade, auth e safety.

**Responsabilidades:**
- Definir toolsets compostos por capacidades.
- Expor MCP bridge local opcional.
- Bloquear tools perigosas em contextos nao interativos.
- Resolver toolsets dinamicamente com base em manifests.
- Manter compatibilidade com comandos HTTP/CLI existentes.

**Fora do escopo deste componente:**
- Permitir tool arbitraria do shell sem SafetyPolicy.
- Expor middlewareAuth token para cliente MCP.
- Criar canal Telegram/Discord.
- Fazer MCP obrigatorio para uso normal dos apps.

---

### Testes obrigatorios

```
TESTE 06-01
dado:    tres apps healthy
quando:  MCP tools/list e chamado
entao:   inclui tools de pocket_ops, pocket_wiki e pocket_llm

TESTE 06-02
dado:    PocketWiki offline
quando:  toolsets sao resolvidos
entao:   pocket_wiki aparece degraded ou omitido conforme include_unavailable

TESTE 06-03  [edge case]
dado:    cliente pede toolset desconhecido
quando:  resolver roda
entao:   retorna ERR_TOOLSET_UNKNOWN com lista de toolsets validos

TESTE 06-04  [seguranca]
dado:    tool `pocket_exec` com comando mutating sem approval_token
quando:  tools/call e chamado
entao:   retorna ERR_SAFETY_APPROVAL_REQUIRED e nao executa
```

---

### Contratos de interface

```yaml
interface:
  id: IF-06-1
  tipo: mcp
  nome: pocket_tools/list

  entrada:
    - campo: include_unavailable
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketToolsetList
      campos:
        - campo: toolsets
          tipo: array<PocketToolset>
        - campo: tools
          tipo: array<PocketTool>
    falha:
      valores_possiveis:
        - code: ERR_MCP_NOT_INITIALIZED
          quando: "initialize nao chamado"
          acao: "retornar erro MCP"
        - code: ERR_TOOLSET_DISCOVERY
          quando: "discovery falha"
          acao: "retornar toolsets parciais"

  comportamento_em_falha:
    - condicao: "app offline"
      acao: "omitir tools daquele app"
      observavel: "toolset.status=degraded"
```

```yaml
interface:
  id: IF-06-2
  tipo: mcp
  nome: pocket_tools/call

  entrada:
    - campo: request
      tipo: PocketToolCallRequest
      obrigatorio: true
      validacao: "tool_name existente"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketToolCallResult
      campos:
        - campo: content
          tipo: string
        - campo: is_error
          tipo: bool
    falha:
      valores_possiveis:
        - code: ERR_TOOL_UNKNOWN
          quando: "tool nao existe"
          acao: "retornar erro"
        - code: ERR_TOOL_APP_UNAVAILABLE
          quando: "app dono da tool offline"
          acao: "retornar erro recuperavel"
        - code: ERR_SAFETY_APPROVAL_REQUIRED
          quando: "tool exige aprovacao"
          acao: "nao executar"

  comportamento_em_falha:
    - condicao: "tool nao segura"
      acao: "chamar SafetyPolicy"
      observavel: "erro ou approval request"
```

---

### Estruturas de dados

```yaml
tipo: PocketToolset
campos:
  - nome: id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: description
    tipo: string
    obrigatorio: true
    default: ""
    limite: "200 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "available"
    limite: "available|degraded|unavailable"
  - nome: tools
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 40 tools"
```

```yaml
tipo: PocketTool
campos:
  - nome: name
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: app_id
    tipo: enum
    obrigatorio: true
    default: "pocketcli"
    limite: "pocketcli|pocketwiki|middlewareauth|pipeline"
  - nome: safety_level
    tipo: enum
    obrigatorio: true
    default: "safe"
    limite: "safe|confirm|blocked"
  - nome: requires_auth
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketToolCallRequest
campos:
  - nome: tool_name
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: arguments_json
    tipo: string
    obrigatorio: true
    default: "{}"
    limite: "65536 chars"
  - nome: approval_token
    tipo: string
    obrigatorio: false
    default: ""
    limite: "64 chars"
```

```yaml
tipo: PocketToolCallResult
campos:
  - nome: content
    tipo: string
    obrigatorio: true
    default: ""
    limite: "262144 chars"
  - nome: is_error
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
  - nome: correlation_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "80 chars"
```

```yaml
tipo: PocketToolsetList
campos:
  - nome: toolsets
    tipo: array<PocketToolset>
    obrigatorio: true
    default: "[]"
    limite: "max 20 toolsets"
  - nome: tools
    tipo: array<PocketTool>
    obrigatorio: true
    default: "[]"
    limite: "max 120 tools"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| toolset=pocket_ops | PocketCli healthy | available | incluir host/context/exec safe |
| toolset=pocket_wiki | PocketWiki offline | degraded | omitir tools wiki |
| toolset=pocket_llm | middleware auth required | degraded | incluir status/login, nao responses |
| tool safety confirm | approval_token ausente | erro | nao executar |
| cliente nao interativo | tool mutating | erro | exigir approval precriado |

**Limites numericos explicitos:**
- max tools por list: 120
- timeout tools/list: 3000 ms
- timeout tool safe: 10000 ms
- timeout tool llm: 60000 ms
- max arguments_json: 64 KB

---

### Dependencias desta fase

```yaml
- id: DEP-06-1
  tipo: componente_interno
  nome: Fase 02 Discovery Local
  versao: "schema 1"
  papel: "resolver apps disponiveis"
  fallback: "toolsets standalone"
  obrigatoria: true
- id: DEP-06-2
  tipo: componente_interno
  nome: Fase 03 Broker LLM
  versao: "schema 1"
  papel: "tools pocket_llm"
  fallback: "toolset degraded"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] toolsets `pocket_ops`, `pocket_wiki`, `pocket_llm`, `pocket_full`
- [ ] MCP bridge local opcional
- [ ] gating por app health/auth/safety
- [ ] todos os testes 06-01 a 06-04 passando

## 3.7 - Fase 07: Pipeline Planner e Scheduler Auditavel

### O que e

Planejador de pipelines e scheduler local para rotinas que combinam apps, como revisar wiki, gerar plano de manutencao, checar hosts e pedir resumo por LLM. Inspirado no cron do Hermes, mas com restricao: scheduler so roda em modo agent/desktop, nao viewer/iSH por padrao.

**Responsabilidades:**
- Definir `PocketPipelinePlan`.
- Criar `pocket pipeline run`.
- Criar scheduler opcional em `~/.local/share/pocketcli/pipelines/jobs.json`.
- Registrar output por job com path seguro.
- Bloquear jobs mutating sem approval policy.

**Fora do escopo deste componente:**
- Scheduler em iPad/iSH viewer.
- Jobs invisiveis sem audit log.
- Execucao paralela ilimitada.
- Integracao com calendario externo.

---

### Testes obrigatorios

```
TESTE 07-01
dado:    pipeline `wiki_review` com PocketWiki e middleware healthy
quando:  `pocket pipeline run wiki_review` roda
entao:   cria correlation_id, context pack, llm call e output markdown

TESTE 07-02
dado:    scheduler chamado em mode_effective=viewer
quando:  `pocket pipeline schedule add` roda
entao:   retorna ERR_SCHEDULER_VIEWER_UNSUPPORTED

TESTE 07-03  [edge case]
dado:    job_id contendo `../escape`
quando:  job e salvo
entao:   retorna ERR_SCHEDULER_JOB_ID_INVALID

TESTE 07-04  [seguranca]
dado:    pipeline inclui fleet exec mutating sem approval policy
quando:  scheduler valida
entao:   retorna ERR_SCHEDULER_APPROVAL_REQUIRED e nao agenda
```

---

### Contratos de interface

```yaml
interface:
  id: IF-07-1
  tipo: cli
  nome: pocket pipeline run

  entrada:
    - campo: pipeline_id
      tipo: string
      obrigatorio: true
      validacao: "1..80 chars"
      valores_enum: []
    - campo: input
      tipo: string
      obrigatorio: false
      validacao: "max 4000 chars"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketPipelineRunResult
      campos:
        - campo: run_id
          tipo: string
        - campo: status
          tipo: enum
        - campo: output_path
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_PIPELINE_UNKNOWN
          quando: "pipeline_id nao existe"
          acao: "retornar erro"
        - code: ERR_PIPELINE_APP_UNAVAILABLE
          quando: "step obrigatorio depende de app offline"
          acao: "falhar ou degradar conforme step"
        - code: ERR_PIPELINE_TIMEOUT
          quando: "tempo total excedido"
          acao: "salvar output parcial"

  comportamento_em_falha:
    - condicao: "step opcional falha"
      acao: "continuar pipeline"
      observavel: "step.status=skipped ou partial"
```

```yaml
interface:
  id: IF-07-2
  tipo: cli
  nome: pocket pipeline schedule add

  entrada:
    - campo: pipeline_id
      tipo: string
      obrigatorio: true
      validacao: "1..80 chars"
      valores_enum: []
    - campo: schedule
      tipo: string
      obrigatorio: true
      validacao: "once ISO, every Nm/Nh/Nd ou cron 5 campos"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketPipelineJob
      campos:
        - campo: job_id
          tipo: string
        - campo: next_run_at
          tipo: string
    falha:
      valores_possiveis:
        - code: ERR_SCHEDULER_VIEWER_UNSUPPORTED
          quando: "modo viewer"
          acao: "recusar"
        - code: ERR_SCHEDULER_BAD_SCHEDULE
          quando: "schedule invalido"
          acao: "recusar"
        - code: ERR_SCHEDULER_APPROVAL_REQUIRED
          quando: "job exige aprovacao persistente nao configurada"
          acao: "recusar"

  comportamento_em_falha:
    - condicao: "jobs.json invalido"
      acao: "nao sobrescrever"
      observavel: "ERR_SCHEDULER_STORE_CORRUPT"
```

---

### Estruturas de dados

```yaml
tipo: PocketPipelinePlan
campos:
  - nome: pipeline_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: title
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: steps
    tipo: array<PocketPipelineStep>
    obrigatorio: true
    default: "[]"
    limite: "max 20 steps"
  - nome: requires_agent_mode
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketPipelineStep
campos:
  - nome: step_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: app_id
    tipo: enum
    obrigatorio: true
    default: "pipeline"
    limite: "pocketcli|pocketwiki|middlewareauth|pipeline"
  - nome: tool_name
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: required
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
  - nome: timeout_ms
    tipo: int
    obrigatorio: true
    default: 30000
    limite: "100..600000"
```

```yaml
tipo: PocketPipelineRunResult
campos:
  - nome: run_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: correlation_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "80 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|partial|failed|timeout"
  - nome: output_path
    tipo: string
    obrigatorio: false
    default: ""
    limite: "1024 chars"
```

```yaml
tipo: PocketPipelineJob
campos:
  - nome: job_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: pipeline_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: schedule
    tipo: string
    obrigatorio: true
    default: ""
    limite: "120 chars"
  - nome: next_run_at
    tipo: string
    obrigatorio: true
    default: ""
    limite: "RFC3339 UTC"
  - nome: enabled
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| step required | app offline | pipeline failed | salvar output parcial |
| step optional | app offline | pipeline partial | continuar |
| scheduler em viewer | qualquer | erro | recusar add/run daemon |
| job_id unsafe | qualquer | erro | bloquear path |
| mutating step | sem approval | erro | nao agendar |

**Limites numericos explicitos:**
- max steps por pipeline: 20
- timeout total pipeline default: 300000 ms
- max jobs agendados: 100
- max output por run: 1 MB
- scheduler tick interval: 60000 ms

---

### Dependencias desta fase

```yaml
- id: DEP-07-1
  tipo: componente_interno
  nome: Fase 06 Toolsets MCP Bridge
  versao: "schema 1"
  papel: "executar steps por tool"
  fallback: "CLI direto para steps PocketCli"
  obrigatoria: true
- id: DEP-07-2
  tipo: componente_interno
  nome: PocketCli SafetyPolicy
  versao: "schema 1"
  papel: "validar steps mutating"
  fallback: "bloquear mutating"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `pocket pipeline run`
- [ ] `pocket pipeline schedule add/list/remove`
- [ ] output markdown por run
- [ ] scheduler desabilitado em viewer
- [ ] todos os testes 07-01 a 07-04 passando

## 3.8 - Fase 08: Fleet/Delegation Execution Lane

### O que e

Canal para execucao distribuida e delegacao restrita usando PocketCli como executor operacional. Diferente do Hermes, a delegacao aqui nao precisa spawnar agentes genericos por padrao; ela cria lanes com escopo, host selector, contexto de wiki opcional, provider opcional e SafetyPolicy obrigatoria.

**Responsabilidades:**
- Definir `PocketDelegationTask`.
- Permitir fan-out controlado para hosts via PocketCli fleet.
- Anexar context pack como leitura, nao como comando.
- Restringir ferramentas por lane.
- Agregar resultados por host e correlacionar eventos.

**Fora do escopo deste componente:**
- Delegacao recursiva ilimitada.
- Crianças/subtasks com acesso direto a memoria compartilhada.
- Autoaprovar comandos perigosos.
- Executar comandos fora do selector aprovado.

---

### Testes obrigatorios

```
TESTE 08-01
dado:    task read-only `uptime` para selector tag:linux
quando:  delegation lane roda com 3 hosts
entao:   retorna resultado por host e summary agregado

TESTE 08-02
dado:    task pede toolset pocket_full mas lane permite apenas pocket_ops_readonly
quando:  task e validada
entao:   retorna ERR_DELEGATION_TOOLSET_DENIED

TESTE 08-03  [edge case]
dado:    max_parallel=1 e 5 hosts
quando:  task roda
entao:   executa sequencialmente e preserva ordem de targets

TESTE 08-04  [seguranca]
dado:    task tenta escrever em `/etc/sudoers`
quando:  SafetyPolicy avalia
entao:   retorna ERR_SAFETY_PATH_BLOCKED e nenhum host executa
```

---

### Contratos de interface

```yaml
interface:
  id: IF-08-1
  tipo: cli
  nome: pocket delegate run

  entrada:
    - campo: task
      tipo: PocketDelegationTask
      obrigatorio: true
      validacao: "selector e goal nao vazios"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketDelegationResult
      campos:
        - campo: task_id
          tipo: string
        - campo: status
          tipo: enum
        - campo: host_results
          tipo: array<PocketDelegationHostResult>
    falha:
      valores_possiveis:
        - code: ERR_DELEGATION_TOOLSET_DENIED
          quando: "toolset fora da lane"
          acao: "recusar"
        - code: ERR_DELEGATION_SELECTOR_EMPTY
          quando: "selector sem hosts"
          acao: "recusar"
        - code: ERR_DELEGATION_DEPTH_EXCEEDED
          quando: "task tenta delegar novamente"
          acao: "recusar"

  comportamento_em_falha:
    - condicao: "um host falha"
      acao: "continuar conforme fail_fast"
      observavel: "host_results status failed"
```

---

### Estruturas de dados

```yaml
tipo: PocketDelegationTask
campos:
  - nome: task_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: goal
    tipo: string
    obrigatorio: true
    default: ""
    limite: "2000 chars"
  - nome: selector
    tipo: string
    obrigatorio: true
    default: ""
    limite: "200 chars"
  - nome: allowed_toolsets
    tipo: array<string>
    obrigatorio: true
    default: "pocket_ops_readonly"
    limite: "max 8 toolsets"
  - nome: context_pack_id
    tipo: string
    obrigatorio: false
    default: ""
    limite: "36 chars"
  - nome: max_parallel
    tipo: int
    obrigatorio: true
    default: 3
    limite: "1..16"
  - nome: fail_fast
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

```yaml
tipo: PocketDelegationResult
campos:
  - nome: task_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "36 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|partial|failed|denied"
  - nome: host_results
    tipo: array<PocketDelegationHostResult>
    obrigatorio: true
    default: "[]"
    limite: "max 200 hosts"
```

```yaml
tipo: PocketDelegationHostResult
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
    limite: "ok|failed|timeout|denied|skipped"
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
| allowed_toolsets vazio | qualquer | deny | usar default readonly somente se omitido |
| task tenta delegate | depth >= 1 | deny | bloquear recursao |
| context_pack_id invalido | required=false | warning | rodar sem wiki context |
| host falha | fail_fast=false | partial | continuar |
| host falha | fail_fast=true | failed | parar pendentes |

**Limites numericos explicitos:**
- max depth: 1
- max_parallel default: 3
- max_parallel absoluto: 16
- max hosts: 200
- timeout por task: 600000 ms
- output_preview por host: 4000 chars

---

### Dependencias desta fase

```yaml
- id: DEP-08-1
  tipo: componente_interno
  nome: PocketCli Fleet
  versao: "schema 1"
  papel: "execucao por hosts"
  fallback: "execucao single host"
  obrigatoria: true
- id: DEP-08-2
  tipo: componente_interno
  nome: Fase 04 Context Pack
  versao: "schema 1"
  papel: "contexto opcional da task"
  fallback: "task sem wiki"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket delegate run`
- [ ] lanes readonly e ops_confirm
- [ ] limite de profundidade
- [ ] agregacao por host
- [ ] todos os testes 08-01 a 08-04 passando

## 3.9 - Fase 09: Skills, Feedback e Procedural Memory

### O que e

Sistema de skills e feedback para transformar rotinas bem-sucedidas em procedimentos reutilizaveis. PocketWiki guarda conhecimento documental; PocketCli guarda padroes operacionais; middlewareAuth nao guarda memoria de usuario, apenas provider/auth. A pipeline pode sugerir uma skill, mas adocao deve ser manual.

**Responsabilidades:**
- Definir `PocketSkill` com frontmatter simples.
- Indexar skills do PocketCli e PocketWiki.
- Criar feedback de pipeline: success, failed, should_remember.
- Sugerir memoria/procedimento com provenance.
- Evitar que LLM escreva skill automaticamente sem aprovacao.

**Fora do escopo deste componente:**
- Marketplace de skills.
- Autoeditar wiki ou profile.
- Memoria externa obrigatoria.
- Treinar modelo.

---

### Testes obrigatorios

```
TESTE 09-01
dado:    skill com frontmatter app=pocketcli e platforms=[linux]
quando:  index roda em Linux
entao:   skill aparece como applicable=true

TESTE 09-02
dado:    pipeline falha por host offline
quando:  feedback e registrado
entao:   cria feedback status=failed sem salvar memoria automaticamente

TESTE 09-03  [edge case]
dado:    skill sem frontmatter
quando:  index roda
entao:   skill e tratada como manual/general e nao quebra index

TESTE 09-04  [seguranca]
dado:    skill tenta incluir `.env` como supporting file
quando:  index valida
entao:   retorna ERR_SKILL_SOURCE_BLOCKED
```

---

### Contratos de interface

```yaml
interface:
  id: IF-09-1
  tipo: cli
  nome: pocket skills list

  entrada:
    - campo: app
      tipo: enum
      obrigatorio: false
      validacao: "vazio ou app valido"
      valores_enum: ["pocketcli", "pocketwiki", "stack"]
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "text ou json"
      valores_enum: ["text", "json"]

  saida:
    sucesso:
      tipo: PocketSkillList
      campos:
        - campo: skills
          tipo: array<PocketSkill>
    falha:
      valores_possiveis:
        - code: ERR_SKILL_INDEX
          quando: "diretorio de skill ilegivel"
          acao: "retornar parcial"
        - code: ERR_SKILL_SOURCE_BLOCKED
          quando: "skill referencia fonte sensivel"
          acao: "ocultar skill"

  comportamento_em_falha:
    - condicao: "skill invalida"
      acao: "pular skill"
      observavel: "warnings preenchido"
```

```yaml
interface:
  id: IF-09-2
  tipo: cli
  nome: pocket feedback record

  entrada:
    - campo: feedback
      tipo: PocketFeedbackRecord
      obrigatorio: true
      validacao: "correlation_id e status obrigatorios"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketFeedbackRecord
      campos:
        - campo: feedback_id
          tipo: string
        - campo: status
          tipo: enum
    falha:
      valores_possiveis:
        - code: ERR_FEEDBACK_SCHEMA
          quando: "feedback invalido"
          acao: "recusar"
        - code: ERR_FEEDBACK_WRITE
          quando: "store indisponivel"
          acao: "retornar erro recuperavel"

  comportamento_em_falha:
    - condicao: "should_remember=true"
      acao: "criar sugestao, nao memoria final"
      observavel: "memory_candidate_id preenchido"
```

---

### Estruturas de dados

```yaml
tipo: PocketSkill
campos:
  - nome: skill_id
    tipo: string
    obrigatorio: true
    default: "path fingerprint"
    limite: "128 chars"
  - nome: name
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: app
    tipo: enum
    obrigatorio: true
    default: "stack"
    limite: "pocketcli|pocketwiki|stack"
  - nome: description
    tipo: string
    obrigatorio: true
    default: ""
    limite: "240 chars"
  - nome: platforms
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 10"
  - nome: applicable
    tipo: bool
    obrigatorio: true
    default: true
    limite: "nenhum"
```

```yaml
tipo: PocketSkillList
campos:
  - nome: skills
    tipo: array<PocketSkill>
    obrigatorio: true
    default: "[]"
    limite: "max 200 skills"
  - nome: warnings
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 50 warnings"
```

```yaml
tipo: PocketFeedbackRecord
campos:
  - nome: feedback_id
    tipo: string
    obrigatorio: true
    default: "UUID v4"
    limite: "36 chars"
  - nome: correlation_id
    tipo: string
    obrigatorio: true
    default: ""
    limite: "80 chars"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "success"
    limite: "success|failed|partial|ignored"
  - nome: summary
    tipo: string
    obrigatorio: true
    default: ""
    limite: "1000 chars"
  - nome: should_remember
    tipo: bool
    obrigatorio: true
    default: false
    limite: "nenhum"
```

---

### Regras de comportamento

| Condicao A | Condicao B | Resultado | Acao |
|------------|------------|-----------|------|
| skill explicitamente chamada | plataforma nao bate | carregar com warning | pedido explicito vence offer filter |
| skill apenas ofertada | plataforma nao bate | ocultar | reduzir ruido |
| feedback success | should_remember=true | memory candidate | exigir confirmacao |
| feedback failed | qualquer | no auto memory | registrar evento |
| supporting file sensivel | qualquer | block | ocultar skill |

**Limites numericos explicitos:**
- max skills indexadas: 200
- max skill body carregado em prompt: 16000 chars
- max feedback summary: 1000 chars
- retencao feedback: 90 dias
- timeout index skills: 3000 ms

---

### Dependencias desta fase

```yaml
- id: DEP-09-1
  tipo: componente_interno
  nome: PocketCli memory
  versao: "JSONL atual"
  papel: "memoria operacional manual"
  fallback: "feedback sem memoria"
  obrigatoria: true
- id: DEP-09-2
  tipo: componente_interno
  nome: PocketWiki SKILL
  versao: "SKILL.md atual"
  papel: "skill documental"
  fallback: "skills PocketCli somente"
  obrigatoria: false
```

---

### Entrega minima desta fase

- [ ] `pocket skills list`
- [ ] `pocket feedback record`
- [ ] sugestao manual de memoria/procedimento
- [ ] bloqueio de supporting files sensiveis
- [ ] todos os testes 09-01 a 09-04 passando

## 3.10 - Fase 10: Stack Doctor, Eval e Release Gates

### O que e

Validador da stack composta. Ele prova que autonomia e composicao continuam funcionando depois de mudancas. Cada repo roda seus testes; o stack doctor roda probes e fixtures cross-app sem exigir que todos os apps estejam sempre online.

**Responsabilidades:**
- Expor `pocket stack doctor --json`.
- Expor `pocket stack eval`.
- Validar manifests, discovery, context pack, broker, toolsets e pipeline.
- Gerar matriz standalone/composed/degraded.
- Documentar prerequisitos por repo.

**Fora do escopo deste componente:**
- Fazer commit, push ou PR.
- Rodar CI remoto.
- Instalar dependencias automaticamente.
- Testar provider pago real sem mock.

---

### Testes obrigatorios

```
TESTE 10-01
dado:    somente PocketCli disponivel
quando:  `pocket stack doctor --json` roda
entao:   retorna standalone.pocketcli=ok e composition.status=degraded

TESTE 10-02
dado:    tres apps disponiveis com fixtures mock
quando:  `pocket stack eval` roda
entao:   todos os cenarios integrated_baseline passam

TESTE 10-03  [edge case]
dado:    middlewareAuth healthy mas sem provider autenticado
quando:  doctor roda
entao:   status e degraded, nao error, e remediation aponta login llm

TESTE 10-04  [seguranca]
dado:    fixture cross-app contem token
quando:  eval carrega fixture
entao:   retorna ERR_STACK_EVAL_SECRET_FIXTURE e bloqueia teste
```

---

### Contratos de interface

```yaml
interface:
  id: IF-10-1
  tipo: cli
  nome: pocket stack doctor

  entrada:
    - campo: format
      tipo: enum
      obrigatorio: false
      validacao: "text ou json"
      valores_enum: ["text", "json"]
    - campo: strict
      tipo: bool
      obrigatorio: false
      validacao: "true ou false"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketStackDoctorReport
      campos:
        - campo: status
          tipo: enum
        - campo: apps
          tipo: array<PocketAppDoctor>
    falha:
      valores_possiveis:
        - code: ERR_STACK_DOCTOR_INTERNAL
          quando: "falha inesperada do doctor"
          acao: "retornar erro redigido"
        - code: ERR_STACK_DOCTOR_SCHEMA
          quando: "fixture/schema invalido em strict"
          acao: "retornar exit 1"

  comportamento_em_falha:
    - condicao: "app opcional offline"
      acao: "status degraded"
      observavel: "exit 0 sem strict"
```

```yaml
interface:
  id: IF-10-2
  tipo: cli
  nome: pocket stack eval

  entrada:
    - campo: suite
      tipo: string
      obrigatorio: false
      validacao: "max 80 chars"
      valores_enum: []
    - campo: fixtures
      tipo: string
      obrigatorio: false
      validacao: "path dentro de tests/fixtures/stack"
      valores_enum: []

  saida:
    sucesso:
      tipo: PocketStackEvalReport
      campos:
        - campo: passed
          tipo: int
        - campo: failed
          tipo: int
    falha:
      valores_possiveis:
        - code: ERR_STACK_EVAL_SECRET_FIXTURE
          quando: "fixture contem segredo"
          acao: "bloquear"
        - code: ERR_STACK_EVAL_APP_MISSING
          quando: "suite exige app ausente"
          acao: "skip ou fail conforme suite"

  comportamento_em_falha:
    - condicao: "suite degraded permite app ausente"
      acao: "skip controlado"
      observavel: "skipped incrementado"
```

---

### Estruturas de dados

```yaml
tipo: PocketStackDoctorReport
campos:
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|degraded|error"
  - nome: generated_at
    tipo: string
    obrigatorio: true
    default: "agora UTC"
    limite: "RFC3339 UTC"
  - nome: apps
    tipo: array<PocketAppDoctor>
    obrigatorio: true
    default: "[]"
    limite: "max 10 apps"
  - nome: composition
    tipo: PocketCompositionDoctor
    obrigatorio: true
    default: "degraded"
    limite: "nenhum"
```

```yaml
tipo: PocketAppDoctor
campos:
  - nome: app_id
    tipo: enum
    obrigatorio: true
    default: "pocketcli"
    limite: "pocketcli|pocketwiki|middlewareauth"
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "ok"
    limite: "ok|warning|error|offline"
  - nome: checks
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 50 checks"
```

```yaml
tipo: PocketCompositionDoctor
campos:
  - nome: status
    tipo: enum
    obrigatorio: true
    default: "degraded"
    limite: "ok|degraded|error"
  - nome: available_lanes
    tipo: array<string>
    obrigatorio: true
    default: "[]"
    limite: "max 20 lanes"
```

```yaml
tipo: PocketStackEvalReport
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
  - nome: skipped
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
| only PocketCli ok | strict=false | degraded | exit 0 |
| required suite app missing | suite integrated | fail | exit 1 |
| app optional missing | suite degraded | skip | registrar skipped |
| fixture secret | qualquer | fail | bloquear leitura |
| provider unauthenticated | broker healthy | degraded | nao error |

**Limites numericos explicitos:**
- timeout doctor total: 10000 ms
- timeout eval total: 120000 ms
- max fixtures: 1000
- max fixture size: 256 KB
- max app checks: 50

---

### Dependencias desta fase

```yaml
- id: DEP-10-1
  tipo: componente_interno
  nome: Fases 01-09
  versao: "schemas 1"
  papel: "validar stack completa"
  fallback: "doctor standalone"
  obrigatoria: true
- id: DEP-10-2
  tipo: arquivo
  nome: tests/fixtures/stack
  versao: "schema 1"
  papel: "fixtures cross-app"
  fallback: "eval indisponivel"
  obrigatoria: true
```

---

### Entrega minima desta fase

- [ ] `pocket stack doctor --json`
- [ ] `pocket stack eval`
- [ ] matriz standalone/composed/degraded
- [ ] docs de prerequisitos por repo
- [ ] todos os testes 10-01 a 10-04 passando

## 4. Cenarios de Falha Transversais

### Entrada invalida / malformada

Todo endpoint/CLI deve validar schema antes de executar step. URLs, paths, selectors, schedules, tool names e context queries sao entrada nao confiavel. O comportamento padrao e rejeitar o item invalido, nao adivinhar.

### Dependencia indisponivel ou lenta

Qualquer app pode estar offline. PocketCli sozinho deve continuar ok. PocketWiki offline remove contexto documental, mas nao bloqueia execucao operacional. middlewareAuth offline remove provider remoto, mas nao bloqueia LM Studio/local backend. Timeouts viram status degraded.

### Estado inconsistente entre fontes de dados

Manifests e health snapshots tem timestamp. O snapshot mais recente vence. Se um app diz healthy mas tool call falha, o event bridge registra a falha e discovery deve refreshar aquele app na proxima rodada.

### Concorrencia / race condition

Ledger e job store usam escrita atomica e lock. Scheduler nao pode executar o mesmo job duas vezes em paralelo. Fleet/delegation respeita max_parallel. Event sequence desempata eventos com mesmo timestamp.

### Exaustao de recurso

Context packs, event payloads, LLM inputs, outputs de pipeline e previews por host tem limites fixos. Se disco encher, pipeline retorna parcial e nao tenta reexecutar em loop.

### Seguranca - entrada controlada por ator externo

Query de wiki, selector de host, schedule, tool arguments, URLs descobertas e provider errors sao nao confiaveis. Paths sensiveis e traversal sao bloqueados. Tokens nunca saem do middlewareAuth. Contexto lembrado ou recuperado deve ser marcado como contexto, nao nova instrucao do usuario.

### Timeout em operacao normalmente rapida

Manifest, discovery, context pack, health, tool list e event search tem timeouts curtos. LLM e fleet tem timeouts maiores, mas sempre observaveis por correlation_id. Timeout de app opcional causa degradacao, nao crash global.

## 5. Decisoes e Restricoes

| ID | Decisao | Motivo | Reversivel |
|----|---------|--------|------------|
| DEC-01 | Cada app continua standalone | Esse e o diferencial pratico da stack Pocket | nao |
| DEC-02 | Composicao e opt-in por discovery/capabilities | Evita acoplamento fragil | nao |
| DEC-03 | middlewareAuth e unico dono de tokens/provider auth | Reduz superficie de segredo | nao |
| DEC-04 | PocketWiki fornece contexto/provenance, nao execucao operacional | Mantem fronteira clara | nao |
| DEC-05 | PocketCli e executor operacional/fleet | Ja e o app SSH/Tailscale/TUI | nao |
| DEC-06 | MCP bridge e opcional | Apps devem funcionar por CLI/HTTP existentes | sim |
| DEC-07 | Scheduler so roda por padrao em agent/desktop | Viewer/iSH nao deve ter daemon obrigatorio | sim |
| DEC-08 | Context pack usa path relativo da wiki | Evita vazar layout do filesystem local | nao |
| DEC-09 | Pipeline nao copia Hermes; adapta padroes | Especialidade Pocket vale mais que paridade superficial | nao |
| DEC-10 | Falha de app opcional gera degraded, nao error global | Respeita autonomia | nao |

**Alternativas descartadas:**
- Transformar PocketWiki em dependencia do PocketCli: descartada porque quebra uso SSH/iSH standalone.
- Transformar middlewareAuth em SDK embutido nos apps: descartada porque espalha auth e tokens.
- Usar um banco central obrigatorio para toda a stack: descartada porque dificulta deploy local-first e degradacao.
- Expor todos os endpoints em LAN por padrao: descartada porque aumenta superficie desnecessaria.
- Copiar toolsets completos do Hermes: descartada porque browser, messaging e desktop control nao sao o foco atual da stack Pocket.
