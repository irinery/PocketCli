# Prompt de implementação — Luna 2 (L2)

Você é responsável exclusivamente pela implementação de RFC-INV-002 no repositório PocketCli, após o bootstrap comum de RFC-INV-000. Leia integralmente `AGENTS.md`, RFC-INV-000, RFC-INV-002 e o contrato/fact fixtures congelados antes de alterar arquivos. Trabalhe na branch/worktree isolada atribuída; não faça push, merge ou chamada a provedor.

## Objetivo

Implemente uma análise determinística e offline em `internal/investigation/analysisfacts` com a assinatura definida no contrato comum:

```go
Analyze(bundle insightcontract.SourceBundle, cfg insightcontract.AnalysisConfig) (insightcontract.FactsResult, error)
```

Consuma e devolva exclusivamente os tipos canônicos de `internal/investigation/insightcontract`. Não redefina tipos em L2. L2 é dona de `analysisfacts/*`; alterações em `rules/*` só por RFC-COR explícita. Preserve investigate.v1 e `rules.Evaluate` legado sem mudanças. Não modifique `core/*`, `query/*`, `contract/v1.go`, SQL, `cmd/*`, web/report/editorial ou outra pista.

## Comportamento exigido

Implemente os requisitos INV002-R01–R10 da RFC. Em particular:

- Mantenha separadas as coortes `completed_at` em `cfg.RequestedScope.ReportWindow`, concluídas no snapshot com estado temporal conhecido e transições observadas para concluído na `ReportWindow`. `Snapshot.Window` descreve a coleta, não substitui a janela pedida. Se `ReportWindow` extrapolar a cobertura, calcule apenas as observações elegíveis, marque parcela unknown/partial e não declare total completo.
- Calcule aging na âncora explícita `SourceBundle.Snapshot.AnalysisReferenceAt`; se ausente/inválida, retorne unknown/not calculable, sem fallback a `Snapshot.Window.End`, distinguindo itens abertos contextuais (`Entity.IsContext`) e não contextuais. Vincule cada métrica à coorte por `Metric.CohortID`; use `Cohort.Window`, `ReferenceAt`, `Count`, `EligibleCount` nullable e `CoverageStatus`. Mostre n/eligible, janela ou âncora, definição e limitações. Não usar relógio local.
- Agrupe relações `blocked_by` apenas com `Link.Status=explicit` e `Link.TemporalStatus` compatível com o corte, em pares/sujeitos distintos, mantendo contagens brutas de links separadas. Reconcilie `SourceBundle.LegacyFindings` por `RuleID`/`SubjectID` contra os novos grupos, mantendo links, pares, sujeitos e findings em contagens separadas. Registre referências associadas em `FactCase.LegacyFindingIDs`. Compare registros sem recalcular a regra legada nem inferir impacto. Categorize backlog/triage como fila, e relação ativa fora disso como dependência observada. Relação não prova impacto, duração, causa ou urgência.
- Use intervalo semiaberto e validade temporal quando houver timestamps. Dados históricos ausentes/incompletos resultam em unknown/partial.
- Só produza compromisso ameaçado se houver compromisso e prazo/threshold explícitos, âncora e dependência aplicável. Sem eles, retorne indeterminado/sem critério. Ator que registrou evento não é decisor causal.
- Reporte zero ocorrências de retrabalho separado de maturidade incompleta, usando somente Events/Links e coverage do bundle; ausência de evento não prova ausência de retrabalho. Use janela de 30 dias terminando em `Snapshot.AnalysisReferenceAt`; sem união sem lacunas de `SourceCoverage.HistoryCoveredIntervals` aplicáveis à mesma fonte/recurso e coleções necessárias, cobrindo os 30 dias e status verificável, não chame zero de qualidade boa. Inclua o caso de dois intervalos contíguos versus um envelope com lacuna; `HistoryObservedFrom/To` descreve somente o envelope e não prova continuidade.
- Toda métrica numérica inclui unidade, definição, janela/âncora, sample size e eligible size nullable; limiar operacional ausente é desconhecido. Quantis ficam null abaixo de `AnalysisConfig.Policy.MinimumQuantileSample` (10 para compatibilidade atual), que é suficiência estatística, não meta de desempenho.
- Respeite coverage e expected nullable. Nunca derive percentual ou universo a partir de cobertura desconhecida, nem transforme itens pendentes por coleções em entidades diferentes.

Use os enums/literais normativos centralizados em RFC-INV-000 para estados, cobertura, temporalidade, classificação e severidade; use os campos exatos de `contracts/insight_v1.go` e suas estruturas `FactsResult`, `Cohort`, `Metric` e `FactCase`; IDs de cobertura vêm das `CoverageKey`, não invente IDs. O contrato já prevê deadline/target/comparator/owner em `Objective`, due date/owner/context/state temporal em `Entity` e validade em `Link`. Se ainda faltar campo necessário, não crie tipo local: envie solicitação concreta ao root. Continue tarefas independentes. Não chame provedores nem acesse dados reais.

## Execução

1. Registre `git status`, commit atual e divergências em relação ao commit informado no briefing; preserve trabalho preexistente. Inclua `Snapshot.SchemaVersion` e `QueryCatalogVersion` na proveniência/identidade de análise quando o contrato definir os hashes/IDs.
2. Inspecione o contrato e fixtures existentes. Implemente em passos pequenos, começando por helpers temporais puros e coortes, depois aging, agrupamento de dependências e maturidade/denominadores.
3. Adicione testes comportamentais com fixtures sintéticas cobrindo oito cenários de RFC-INV-002: coortes divergentes; item contextual antigo sem movimento; limites do `ReportWindow`, extrapolação e âncora; validade/histórico incompleto e lacuna entre intervalos cobertos; relação de backlog repetida com associação de `LegacyFindingIDs`; dependência sem compromisso/prazo/owner explícitos (incluindo objetivo e entidade); zero retrabalho imaturo; amostra n=9/n=10 com coverage parcial e expected desconhecido.
4. Documente como rodar esses testes conforme AGENTS.md. Rode apenas verificações locais offline necessárias da pista, incluindo testes Go e `gofmt`; não use tokens, rede ou provedores.
5. Confira `git diff --check`, arquivos alterados e que nenhuma fronteira de pista foi atravessada.

## Aceite e handoff

A entrega está pronta quando contrato e fixtures compilam sem tipos paralelos, os oito casos comportamentais passam, a saída serializada conserva null/unknown e denominadores, aging contextual é visível na análise nova e dependências não são rotuladas como bloqueios de impacto confirmado. Não altere arquivos externos à fronteira para “consertar” a integração. Se for necessária mudança estrutural do contrato, deixe um pedido de RFC de correção para root/integração.

Ao concluir, entregue ao integrador Sol: commit hash local, base/commit comparado, lista de arquivos alterados, requisitos cobertos, comandos e resultados, limitações e dependências de contrato pendentes. Não faça integração final nem push.
