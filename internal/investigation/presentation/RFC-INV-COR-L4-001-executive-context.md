# RFC-INV-COR-L4-001 — Contexto de objetivos e timeline no contrato executivo

| Campo | Valor |
|---|---|
| Estado | ratificada e implementada na integração Sol em 28/10/2026 |
| Data e autor | 28/10/2026 · L4 |
| RFC/requisito de origem | RFC-INV-004 R02, R06, R12 |
| Responsável pela implementação | responsável pelo contrato comum; consumidores em L1/L3/L4 |
| Base e head observados | `cdadfef769d5f5b5a163a0cc583dded512dbffd1` → branch `codex/luna-4-executive` |
| Contrato/pacote | `investigate.insights.v1`, `internal/investigation/insightcontract/v1.go` |
| Gravidade e dimensão | limita contexto gerencial e impede timeline completa verificável |
| Dependências | fixture sintética `internal/investigation/insightcontract/testdata/fixture_go.json` |

## 1. Problema e evidência

`ExecutiveReport` contém `Dimensions[].ObjectiveID`, mas não carrega títulos, critérios, metas, unidade, prazo ou baseline de `AnalysisConfig.Objectives`. `InsightReader.GetExecutiveReport` não recebe `AnalysisConfig`, `DecisionAnalysis` nem `SourceBundle`; portanto, a UI e o PDF só conseguem rotular dimensões pelo ID do objetivo. Isso não permite explicar o objetivo ao leitor como pede INV004-R02.

`CaseDetail` contém fatos, afirmações, decisões, relações, entidades e evidências, mas não contém `Event`, `Stage` ou `DeliveryObservation`. A fonte de eventos possui `Type`, `EventAt`, `ObservedAt`, valores anterior/novo, ator e evidências; esses campos não estão acessíveis no detalhe de caso. Assim INV004-R12 não consegue distinguir de forma estruturada decisão, resultado de entrega e observação posterior, nem preservar o timestamp e a proveniência de cada tipo de evento.

Além disso, RFC-INV-000 não define regra para consolidar `QuestionCoverage.Status` e `DimensionStatus.Status` em `ExecutiveReport.SufficiencyStatus`. A camada de apresentação precisa devolver esse campo, mas qualquer agregação altera o significado; a escolha de L4 foi conservadora e está isolada em `presentation.sufficiency`, pendente de ratificação do contrato comum.

Reprodução sintética: comparar `config.objectives` com `report.dimensions` e `source.events`/`source.deliveries` com `CaseDetail` em `fixture_go.json`. Os primeiros contêm metadados ausentes do relatório; os eventos e entregas não têm campo correspondente no detalhe. A regra de suficiência também não aparece na RFC-INV-000 nem na fixture como algoritmo de consolidação.

## 2. Esperado versus observado

| Aspecto | Esperado | Observado |
|---|---|---|
| Objetivos | Exibir nome, dimensão e critérios/limites quando informados, mantendo `null` desconhecido | `ExecutiveReport` só referencia `ObjectiveID` por dimensão e caso |
| Timeline | Decisões, resultados e observações com data, escopo, origem e evidência | `CaseDetail` fornece datas de claims/decisões/evidências; não fornece eventos/stages/entregas completos |
| Suficiência geral | Consolidar estado com regra normativa sem misturar publicação/análise | Há enums dos estados individuais, sem regra de agregação para `ExecutiveReport.SufficiencyStatus` |
| Consequência | Leitor entende o critério e pode seguir a trilha temporal | Interface mostra IDs genéricos e pode omitir transições semânticas que não foram convertidas em claim/decisão |

## 3. Reprodução mínima

Pré-requisito: Go conforme `go.mod`; sem variáveis de ambiente.

```sh
python3 - <<'PY'
import json
f = json.load(open('internal/investigation/insightcontract/testdata/fixture_go.json'))
print('objetivos:', [x['id'] for x in f['config']['objectives']])
print('dimensões:', [x['objective_id'] for x in f['report']['dimensions']])
print('eventos:', [x['id'] for x in f['source']['events']])
print('entregas:', [x['id'] for x in f['source']['deliveries']])
print('campos de CaseDetail:', list(f['report'].keys()))
PY
```

O último objeto de fixture é um `ExecutiveReport`, não um `CaseDetail`; serve para demonstrar o contexto de objetivo ausente. A limitação temporal é demonstrada pela ausência de coleção de timeline em `CaseDetail` no tipo Go. Dados e IDs são sintéticos.

## 4. Correção proposta

Recomendação: ampliar aditivamente o contrato compartilhado com `ExecutiveReport.Objectives []Objective` ou um view estável com ID, título, dimensão, comparator, target, unit, deadline, owner e baseline; `CaseDetail.Timeline []TimelineItem` com tipo (`decision`, `result`, `observation`), data opcional, escopo, fonte, referência original e IDs de evidência; e uma regra normativa para consolidar estados individuais em suficiência global sem criar score de saúde. Preservar eventos de data desconhecida sem fabricar ordenação. Atualizar schema/fixtures e seus hashes no bootstrap comum antes da integração.

L4 renderizará os novos campos sem cálculos ou inferências. Se a ampliação for recusada, a UI permanecerá limitada a IDs de objetivo e às decisões/afirmações que já chegam no caso; a falta será descrita como limitação, nunca preenchida por heurística local.

## 5. Contrato e compatibilidade

A mudança seria aditiva a `investigate.insights.v1`; nenhuma alteração foi feita nesta frente porque o contrato está congelado e L4 não é seu dono. `null` continua significando desconhecido e listas vazias não provam completude. `ObjectiveID` e IDs originais de eventos permanecem referências estáveis. O responsável pelo contrato precisa decidir versão/additividade, nomes finais e fixtures antes de L1/L3/L4 alterarem consumidores.

## 6. Passos para reentrega

1. Ler RFC-INV-000, RFC-INV-004 e esta RFC.
2. Aprovar ou rejeitar os campos aditivos e atualizar contrato, schema e fixtures no bootstrap comum.
3. Atualizar o mapper `presentation`, rotas de detalhe, UI/PDF e seus testes em L4.
4. Validar caso com objetivo completo, objetivo sem meta, decisão superada, conflito, evento sem data e entrega observada com evidências.
5. Entregar novo handoff com contrato/head, resultados e limitações.

## 7. Critérios de aceite

- Objetivo é compreensível sem depender de ID opaco; meta/baseline ausentes aparecem como indeterminadas.
- Decisões, resultados e observações são tipos distintos e retêm origem, escopo, data opcional e evidências.
- A suficiência global segue uma regra aprovada e reproduzível a partir dos estados por pergunta/dimensão; publicação/export e resultado de negócio não alteram essa avaliação.
- Decisão superada e conflito simultâneo mantêm os registros e relações originais.
- Data desconhecida não recebe posição cronológica inventada.
- Nenhum cálculo de suficiência, impacto ou causalidade é movido para a apresentação.

## 8. Validação e encerramento

Status proposto. Nenhuma mudança de contrato nem correção de produto foi aplicada. Requer decisão do responsável pelo contrato comum.

### Decisão de integração Sol (28/10/2026)

O pedido explícito de fechar os gates ratifica a extensão **aditiva** de `investigate.insights.v1`: `ExecutiveReport.Objectives` copia os objetivos efetivos da configuração imutável; `CaseDetail.Timeline` expõe itens tipados derivados de decisões, eventos, etapas e observações de entrega vinculados às entidades do caso. Os itens sem timestamp válido ficam no fim, sem ordem cronológica afirmada. IDs e evidências originais são preservados. Campos ausentes em análises antigas continuam vazios/desconhecidos.

O schema versionado em `internal/investigation/insightcontract/insight_v1.schema.json` e a fixture compartilhada foram atualizados. A timeline conserva também observação, ator, valores anterior/novo e fim da etapa quando a fonte os fornece. A UI e o PDF expõem esses campos sem reinterpretar os eventos. `TestCaseTimelineSeparatesDecisionResultObservationAndUnknownDate`, os testes da apresentação e a integração offline cobrem a extensão.

A agregação de suficiência existente em `presentation.sufficiency` fica ratificada: análise semântica `not_run`/`failed` é insuficiente; ausência de pergunta/dimensão aplicável é `not_applicable`; lacuna de cobertura, dimensão indeterminada ou análise parcial produz `partial` se houver alguma dimensão avaliada, senão `insufficient`; sem essas lacunas, `sufficient`. Publicação, exportação e nível de risco não alteram a suficiência. A regra é de evidência, não um score de saúde.

## 9. Prompt de reentrega

> Você é o responsável pela decisão do contrato comum. Leia RFC-INV-000, RFC-INV-004 e RFC-INV-COR-L4-001. Decida a adição de metadados de objetivo e timeline tipada mantendo ausência como desconhecido e sem inferir cronologia. Se aprovada, atualize contrato/schema/fixtures no bootstrap compartilhado e avise L1/L3/L4 dos campos congelados. Não implemente apresentação ou wiring de Sol neste retrabalho.
