# Handoff — LUNA-2

**Estado:** concluído para a fronteira L2.
**RFC:** RFC-INV-002. **Base:** cdadfef769d5f5b5a163a0cc583dded512dbffd1 (bootstrap comum). **Base auditada:** b068cfdd40ea72f6c88df1c4ab69d30ab2905b67. **Head funcional:** 5948d458cd382411fb519d4caf3a706fd456d140. **Worktree/branch:** /Users/irinery/.codex/worktrees/luna-2-analysisfacts/PocketCli, codex/luna-2-analysisfacts.
**Modelo/esforço efetivos:** Codex desta sessão; a ferramenta não expõe seleção de Luna/high, então não afirmo que esse perfil foi usado. **Contrato/hash:** internal/investigation/insightcontract/v1.go, SHA-256 c1abd2c95a2f0da87a24fe902285eef0ad067d83ceca4be15ba13d9fd4b9eeab.

O checkout original estava no commit de referência, mas tinha alterações e arquivos não commitados de outro trabalho. Nenhum foi copiado ou alterado; o bootstrap e a implementação foram feitos em worktrees limpas.

## Requisitos

| Requisito | Estado | Arquivo/teste/evidência | Pendência/RFC-COR |
|---|---|---|---|
| INV002-R01 | Atendido | analysisfacts.Analyze; TestAnalyzeConfigIdentityIncludesSnapshotSchemaAndCatalogVersions | Nenhuma |
| INV002-R02 | Atendido | Coortes separadas de completed_at, estado atual e transições; TestAnalyzeFixtureKeepsCompletionCohortsAndContextualAgingSeparate, TestAnalyzeUsesSemiOpenWindowAndNoReferenceFallback, TestAnalyzeMarksWindowExtrapolationPartialAndEligibleUnknown | Nenhuma |
| INV002-R03 | Atendido | Aging contextual/não contextual pela âncora explícita; TestAnalyzeFixtureKeepsCompletionCohortsAndContextualAgingSeparate, TestAnalyzeUsesSemiOpenWindowAndNoReferenceFallback, TestAnalyzeQuantileSampleThresholdDoesNotInferExpectedUniverse | Quantis ficam indeterminados abaixo do mínimo configurado |
| INV002-R04 | Atendido | Intervalos semiabertos e validade as-of; diagnósticos para links candidatos, sem temporalidade, intervalos incompletos e expirados; TestAnalyzeGroupsOnlyExplicitTemporallyValidDependenciesAndReconcilesLegacy | Nenhuma |
| INV002-R05 | Atendido | Contagens distintas de links, pares, sujeitos e findings; associação de LegacyFindingIDs; fila separada de dependência observada; teste de agrupamento/reconciliação | Achados legados são comparados por RuleID/SubjectID; não se recalcula rules.Evaluate |
| INV002-R06 | Atendido | TestAnalyzeDoesNotInventCommitmentOrOwner; risco só com prazo explícito vencido, âncora e dependência aplicável | Sem data/meta e sem owner, permanece indeterminado |
| INV002-R07 | Atendido | Categorias separadas de reabertura, reversão e correção; TestAnalyzeReworkMaturityRequiresContinuousCoverage, TestAnalyzeKeepsIncompleteCoverageOutOfMatureReworkMetric | Zero observado não é tratado como qualidade boa |
| INV002-R08 | Atendido | Métricas incluem unidade, definição, amostra e elegibilidade nullable; teste n=9/n=10 e serialização null | Sem baseline/meta, nenhuma classificação operacional |
| INV002-R09 | Atendido | União de intervalos por fonte/recurso/coleção; cobertura desconhecida/parcial mantém elegibilidade null; testes de lacuna e janela extrapolada | IDs de cobertura não são inventados |
| INV002-R10 | Atendido | Nenhum limiar operacional é criado; deadline/target precisam estar registrados | Nenhuma |

## Mudanças e conexões

- internal/investigation/analysisfacts/analysis.go: implementa Analyze(bundle insightcontract.SourceBundle, cfg insightcontract.AnalysisConfig) (insightcontract.FactsResult, error), puro e offline.
- internal/investigation/analysisfacts/analysis_test.go: oito cenários comportamentais da RFC, além da identidade de análise.
- docs/investigation/insights/luna-2/README.md: pré-requisito e comandos para rodar testes/formatar.
- O bootstrap comum é o commit anterior da branch codex/investigation-insightcontract-bootstrap: contrato canônico, fixtures sintéticas e documentos L2 copiados para os caminhos acordados.
- Nenhum arquivo fora da fronteira L2 foi editado na worktree de implementação. rules/*, core/*, query/*, contract/v1.go, SQL e cmd/* permanecem intocados.

## Validação efetivamente executada

| Comando/cenário | Ambiente | Resultado real | Evidência |
|---|---|---|---|
| python3 tools/validate_package.py | Pacote anexo, Python padrão | passou: 50 estruturas Go, 24 cenários, 8 evidências sintéticas e 15 hashes conferidos | Saída do validador; sem erros |
| gofmt -w internal/investigation/analysisfacts | Go 1.26.1, Darwin arm64 | concluído | Formatação dos arquivos L2 |
| go test -count=1 -buildvcs=false ./internal/investigation/insightcontract ./internal/investigation/analysisfacts | Go 1.26.1, Darwin arm64 | passou; contrato sem testes próprios e L2 ok | Rodada local sem rede/provedor |
| go vet -buildvcs=false ./internal/investigation/insightcontract ./internal/investigation/analysisfacts | Go 1.26.1, Darwin arm64 | passou sem diagnósticos | Rodada local |
| git diff --check | Worktree L2 | passou sem saída | Nenhum erro de whitespace |

Não rodei a suíte completa, build ou smoke tests do produto; as verificações ficaram nos pacotes de contrato e L2. Nenhum provedor, token, rede ou dado real foi usado. O contrato foi compilado no Go disponível (1.26.1); Go 1.22 não estava instalado para uma verificação nessa versão exata.

## Saídas revisáveis

Os testes leem internal/investigation/insightcontract/testdata/fixture_go.json e adicionam casos sintéticos em memória. O resultado distingue estoque contextual, coortes, relações observadas e maturidade; os testes verificam IDs, contagens, âncora, estados desconhecidos e null. Nenhuma saída usa dados reais ou representa benchmark de modelo.

## RFCs de correção e dependências

Nenhuma RFC-COR foi necessária; nenhum campo do contrato bloqueou a implementação autônoma. As relações legado/novas são comparadas sem recalcular a regra antiga. O registro de origem de eventos fica em FactCase.ObservedFacts junto aos EvidenceIDs, pois FactCase não declara campo EventIDs; não alterei o contrato compartilhado.

## Fechamento

L2 está pronta no commit funcional 5948d458cd382411fb519d4caf3a706fd456d140; integração final e validação do produto permanecem para depois dos quatro handoffs, e Sol não foi iniciado.
