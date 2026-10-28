# Handoff — LUNA-4

**Estado:** parcial encerrado; a visão executiva e exportação estão implementadas nesta frente, com bloqueios de contrato registrados.  
**RFC:** RFC-INV-004. **Base:** `cdadfef769d5f5b5a163a0cc583dded512dbffd1` (bootstrap comum; diff comparado também com `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67`).  
**Head:** `codex/luna-4-executive` (ver commit local). **Worktree:** `/Users/irinery/.codex/worktrees/luna-4-executive/PocketCli`.  
**Contrato:** `investigate.insights.v1`, copiado no bootstrap; L4 não alterou contrato, schemas nem composição root.

## Requisitos

| Requisito | Estado | Evidência | Pendência |
|---|---|---|---|
| R01 compatibilidade | Atendido | `presentation`, `report` e `web` usam tipos e assinaturas congelados; legacy preservado | Wiring fica com Sol |
| R02 objetivos/dimensões | Parcial | Overview e PDF exibem dimensões e assessments em separado | Títulos, metas, critérios e baseline dos objetivos não existem em `ExecutiveReport`; RFC-INV-COR-L4-001 |
| R03 eixos independentes | Atendido | Impacto, complexidade, urgência e confiança são apresentados separadamente com racional, evidência e missing | — |
| R04 suficiência/desconhecido | Parcial | UI/PDF separam publicação, análise e suficiência; estados ausentes não viram zero e avisos persistem | A agregação geral de suficiência não é normativa no contrato; regra conservadora local está isolada e precisa ratificação na RFC-INV-COR-L4-001 |
| R05 unidades | Atendido | Overview/PDF mostram unidade, amostra, elegíveis, numerador/denominador, definição, coorte, método e limitações | — |
| R06 casos/cronologia | Parcial | Detalhe preserva claims, decisões, estados e relações; cronologia ordena datas disponíveis e mantém data ausente indeterminada | Eventos, stages e observações de entrega não chegam a `CaseDetail`; RFC-INV-COR-L4-001 |
| R07 evidência/links | Atendido | Detalhe apresenta origem, locator, timestamps, hashes, estado, redação/truncamento; URLs HTTPS validadas; texto usa `textContent`/writer PDF escapado | — |
| R08 overview leve | Atendido | Overview lê apenas `GetExecutiveReport`; lista completa usa páginas de `ListInsightCases`; limites e ordem são validados | — |
| R09 navegação progressiva | Atendido | Seleção de análises, dimensões, casos paginados, detalhe e export sob demanda | — |
| R10 PDF nativo | Atendido | Writer PDF interno; inclui IDs, data, versão do renderer, statuses e avisos; não trunca para meta de páginas | Bookmarks não implementados; não suportados pelo writer atual |
| R11 exportações | Atendido | Export executivo preserva seleção/total; detalhado pagina coleção completa e falha fechado em inconsistências; manifest/PDF e download por ID | Wiring do exporter no composition root fica com Sol |
| R12 timeline tipada | Bloqueado pelo contrato | Timeline disponível ordena afirmações/decisões e mantém relações | Decisão/resultado/observação, origem e escopo dos eventos não são representados em `CaseDetail`; RFC-INV-COR-L4-001 |
| R13 segurança/escopo | Atendido | Sessão/CSRF existentes, posse do snapshot, autorização analysisID, validação de identidade e download por artifact ID | — |
| R14 testes/escopo | Parcial | Fixtures sintéticas, testes focados e verificações locais listadas abaixo | Sem browser real; cenários de polling dos seis estados terminais e run sem `AnalysisID` não têm teste comportamental de DOM |
| R15 ciclo de análise | Atendido | Lista versões, escolhe config JSON local, pede confirmação, inicia por clique, polling limitado e cancelamento explícito; abrir snapshot só lista análises | Controller real depende do wiring de Sol/L1 |

## Mudanças e conexões

- `presentation.Build` valida referências e preserva a ordem das decisões, aplicando `Policy.MaximumExecutiveCases` à seleção sem alterar `TotalCases`.
- `report.ExecutiveFinalizer` pagina detalhe completo, valida snapshot/análise/total/cursor/IDs, renderiza PDF nativo, persiste manifest e serve artefato por ID.
- `web.Handler` implementa rotas canônicas de análises, runs, casos e exportações. A UI embutida carrega overview leve, pagina análises/casos e obtém detalhe sob demanda.
- `RFC-INV-COR-L4-001-executive-context.md` contém a dependência de contrato reproduzível; `README.md` documenta como executar testes e seus pré-requisitos.
- Sol deve injetar `InsightReader`, `AnalysisController` e `ExecutiveExporter` em `PreviewOptions` no composition root/coordinator. L4 não alterou `cmd/*` nem inicialização compartilhada.

## Validação efetivamente executada

| Comando/cenário | Resultado |
|---|---|
| `go test -count=1 ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web` | passou após as mudanças desta revisão |
| `node --check /tmp/pocketcli-l4-real-assets.js` | passou; script extraído do asset real, IDs substituídos por literais sintéticos |
| `git diff --check` | passou |
| Rede, credenciais ou provider | não usados |

Pré-requisito: Go conforme `go.mod`; Node.js somente para a checagem opcional de sintaxe do asset; sem variáveis de ambiente.

## RFCs e dependências

`RFC-INV-COR-L4-001-executive-context.md`: estado proposto, requer decisão do responsável pelo contrato comum sobre metadados de objetivos, timeline tipada e consolidação normativa da suficiência. Não alterar o contrato nesta branch L4. Depois da decisão, atualizar apresentação/PDF/UI e testes em nova entrega L4.

## Fechamento

A UI, o mapper e as exportações estão implementados e validados localmente; L4 permanece parcial até a decisão do contrato comum e o wiring reservado a Sol.
