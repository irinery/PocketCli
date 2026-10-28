# Handoff — LUNA-4

**Estado:** parcial encerrado; a visão executiva e exportação estão implementadas nesta frente, com bloqueios de contrato registrados.  
**RFC:** RFC-INV-004. **Base:** `cdadfef769d5f5b5a163a0cc583dded512dbffd1` (bootstrap comum; diff comparado também com `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67`).  
**Head de implementação:** `cc0f2177eb8ef4c8b71eac578463e32219b9acec` em `codex/luna-4-executive`. **Worktree:** `/Users/irinery/.codex/worktrees/luna-4-executive/PocketCli`.
**Modelo/esforço:** GPT-6; o esforço efetivo não é exposto nesta execução.
**Contrato:** `investigate.insights.v1`; SHA-256 de `internal/investigation/insightcontract/v1.go`: `c1abd2c95a2f0da87a24fe902285eef0ad067d83ceca4be15ba13d9fd4b9eeab`. L4 não alterou contrato, schema ou composition root.

Comparado a `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67`, o bootstrap compartilhado `cdadfef` acrescentou o contrato e as fixtures `insightcontract`; a branch L4 parte desse bootstrap. A diferença de UI legacy relevante para esta frente é a remoção de pré-carga de findings/queries e da varredura integral de `/report` no overview.

## Requisitos

| Requisito | Estado | Evidência | Pendência |
|---|---|---|---|
| R01 compatibilidade | Atendido | `presentation`, `report` e `web` usam tipos e assinaturas congelados; legacy preservado | Wiring fica com Sol |
| R02 objetivos/dimensões | Parcial | Overview e PDF exibem dimensões e assessments em separado | Títulos, metas, critérios e baseline dos objetivos não existem em `ExecutiveReport`; RFC-INV-COR-L4-001 |
| R03 eixos independentes | Atendido | Impacto, complexidade, urgência e confiança apresentam `State` e `Level` separadamente, com racional, evidência e missing; mapper valida enums e referências | — |
| R04 suficiência/desconhecido | Parcial | UI/PDF separam publicação, análise e suficiência; estados ausentes não viram zero e avisos persistem | A agregação geral de suficiência não é normativa no contrato; regra conservadora local está isolada e precisa ratificação na RFC-INV-COR-L4-001 |
| R05 unidades | Atendido | Overview/PDF mostram unidade, amostra, elegíveis, numerador/denominador, definição, coorte, método e limitações | — |
| R06 casos/cronologia | Parcial | Detalhe preserva IDs de fatos/claims/decisões/relações/objetivos, estados e ordem de claims/decisões com timestamps RFC3339; data ausente/ambígua fica sem ordem | Eventos, stages e observações de entrega não chegam a `CaseDetail`; RFC-INV-COR-L4-001 |
| R07 evidência/links | Atendido | Detalhe apresenta origem, locator, timestamps, hashes, estado, redação/truncamento; URLs HTTPS validadas; texto usa `textContent`/writer PDF escapado | — |
| R08 overview leve | Atendido | Overview lê apenas `GetExecutiveReport`; seleção respeita o teto sem ocultar assessments críticos; lista completa usa páginas de `ListInsightCases`; limites, ciclos, duplicatas, total e ordem são validados | — |
| R09 navegação progressiva | Atendido | Seleção de análises, dimensões, casos paginados, detalhe e export sob demanda; estados de carregamento/vazio/parcial/erro distintos; cursores, IDs, total e ordem ficam vinculados a snapshot/análise | — |
| R10 PDF nativo | Atendido | Writer PDF interno; inclui IDs, data, versão do renderer, statuses e avisos; não trunca para meta de páginas | Bookmarks não implementados; não suportados pelo writer atual |
| R11 exportações | Parcial | Export executivo preserva seleção/total; detalhado pagina coleção completa e falha fechado em inconsistências; artefatos repetidos por conteúdo/modo/renderer são idempotentes | Contrato não expõe identidade/versionamento da política de conteúdo para persistir no artefato/fingerprint; RFC-INV-COR-L4-002. Wiring fica com Sol |
| R12 timeline tipada | Bloqueado pelo contrato | Timeline disponível ordena afirmações/decisões e mantém relações | Decisão/resultado/observação, origem e escopo dos eventos não são representados em `CaseDetail`; RFC-INV-COR-L4-001 |
| R13 segurança/escopo | Atendido | Sessão/CSRF existentes, posse do snapshot, autorização analysisID, validação dos metadados/identidade do artefato e download por artifact ID | — |
| R14 testes/escopo | Parcial | Fixtures sintéticas; testes de paginação/autorização de análises, run sem `AnalysisID`, enums, referências/unidades, distinção State/Level, preservação de caso crítico além do teto, C15 seleção 5/total 12, campos extras/credenciais rejeitados, falhas fail-closed e layout/status/aviso PDF; verificações locais abaixo | Testes de UI verificam a estrutura/limites do JS, mas não executam o ciclo DOM em browser/headless |
| R15 ciclo de análise | Atendido | Lista versões, valida/whitelista config JSON local canônico (rejeita campos extras e credenciais), pede confirmação, inicia por clique, polling limitado e cancelamento explícito; abrir snapshot só lista análises | Controller real depende do wiring de Sol/L1 |

## Mudanças e conexões

- `presentation.Build` valida referências/unidades/enums e preserva a ordem das decisões; `MaximumExecutiveCases` é meta e assessment crítico explicitamente avaliado permanece selecionado além do teto. `TotalCases` continua integral.
- `report.ExecutiveFinalizer` pagina detalhe completo, valida snapshot/análise/total/cursor/IDs, renderiza PDF nativo, persiste manifest e serve artefato por ID.
- `web.Handler` implementa rotas canônicas de análises, runs, casos e exportações. A UI embutida carrega overview leve, pagina análises/casos, rejeita ciclos/incompletude e obtém detalhe sob demanda. O JSON local é whitelisted ao contrato antes do envio.
- `RFC-INV-COR-L4-001-executive-context.md` contém a dependência de contrato reproduzível; `README.md` documenta como executar testes e seus pré-requisitos.
- `RFC-INV-COR-L4-002-export-policy-fingerprint.md` registra a falta de fingerprint explícito da política de conteúdo no contrato.
- Sol deve injetar `InsightReader`, `AnalysisController` e `ExecutiveExporter` em `PreviewOptions` no composition root/coordinator. L4 não alterou `cmd/*` nem inicialização compartilhada.

## Validação efetivamente executada

| Comando/cenário | Resultado |
|---|---|
| `go test -count=1 ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web` | passou após as mudanças desta auditoria |
| `node --check /tmp/pocketcli-l4-real-assets.js` | passou; script extraído do asset real, IDs substituídos por literais sintéticos |
| `git diff --check` | passou |
| Rede, credenciais ou provider | não usados |

Pré-requisito: Go conforme `go.mod`; Node.js somente para a checagem opcional de sintaxe do asset; sem variáveis de ambiente.

## RFCs e dependências

`RFC-INV-COR-L4-001-executive-context.md`: estado proposto, requer decisão do responsável pelo contrato comum sobre metadados de objetivos, timeline tipada e consolidação normativa da suficiência. `RFC-INV-COR-L4-002-export-policy-fingerprint.md`: estado proposto, requer uma identidade canônica da política de conteúdo para o artefato. Não alterar o contrato nesta branch L4. Depois das decisões, atualizar apresentação/PDF/UI/export e testes em nova entrega L4.

## Fechamento

A UI, o mapper e as exportações estão implementados e validados localmente; L4 permanece parcial até as decisões de contrato, testes completos do ciclo UI e wiring reservado a Sol.
