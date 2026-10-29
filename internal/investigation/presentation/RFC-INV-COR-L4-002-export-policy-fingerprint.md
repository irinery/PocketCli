# RFC-INV-COR-L4-002 — Fingerprint da política em exportações executivas

| Campo | Valor |
|---|---|
| Estado | ratificada e implementada na integração Sol em 28/10/2026 |
| RFC/requisito de origem | RFC-INV-004 §5 passo 6, INV004-R11 |
| Responsável pela decisão | responsável pelo contrato comum |
| Consumidor | L4 (`report.ExecutiveFinalizer`) |
| Contrato congelado | `investigate.insights.v1` |

## Problema

A RFC-INV-004 exige identidade/idempotência de exportação baseada na análise imutável, modo, política de conteúdo e renderer. `ExecutiveExportRequest` recebe snapshot, análise, modo e uma chave de request. `ExecutiveReport` não retorna a política (`Policy.ID`, `Policy.Version`, `ContentMode` ou hash equivalente) e o `InsightReader` não oferece consulta da configuração da análise. Logo, o exporter não consegue registrar nem incluir no fingerprint a versão exata da política que determinou trechos, redação e seleção.

L4 calcula hoje `AnalysisHash` sobre o relatório executivo e os detalhes efetivamente lidos e deriva idempotência desse hash, do modo e da versão do renderer. Requisições semanticamente idênticas retornam o mesmo artefato mesmo quando trazem uma chave de request diferente. Isso protege contra duplicação do mesmo conteúdo, mas não prova qual identidade/versionamento de política gerou o conteúdo, nem distingue políticas diferentes que produzam os mesmos campos observáveis.

## Reprodução

No contrato em `internal/investigation/insightcontract/v1.go`, `AnalysisConfig.Policy` contém `ID`, `Version`, `ExcerptMaxRunes` e `ContentMode`; esses campos não aparecem em `ExecutiveReport`, `CaseDetail`, `ExecutiveExportRequest` ou `ExecutiveArtifact`. O exporter recebe somente `InsightReader` e consulta `GetExecutiveReport`/páginas de casos. Portanto, não há valor canônico disponível para acrescentar ao hash sem inferir ou criar configuração paralela.

## Correção proposta

Adicionar ao retorno executivo um identificador imutável de política de conteúdo, preferencialmente `PolicyHash` calculado sobre os campos efetivos da política, ou os campos canônicos `Policy.ID`, `Policy.Version`, `ExcerptMaxRunes` e `ContentMode`. O exporter deve persistir esse valor no artefato/manifest e incluí-lo na identidade idempotente junto de análise, modo e renderer. A atualização deve incluir schema, fixture Go e vetores de hash. Não transportar credencial ou configuração de provider no relatório.

## Critérios de aceite

- Mesmo conteúdo, política, modo e renderer reutilizam o mesmo artefato, independentemente da chave de request.
- Alteração da política gera outra identidade do artefato mesmo quando, por coincidência, a apresentação visível permanece igual.
- Manifest e artefato registram a versão/hash da política usada para produzir o PDF.
- O hash é reprodutível por fixture sintética e não depende de relógio, caminho local ou chave de request.

## Limite da L4

L4 não altera o contrato compartilhado congelado nem inventa um hash a partir de campos parciais. Até a decisão e atualização do contrato comum, o artefato registra hashes do conteúdo de relatório/casos e renderer, sem afirmar identidade explícita da política.

## Decisão de integração Sol (28/10/2026)

O pedido explícito de fechar os gates ratifica `PolicyHash` como SHA-256 da serialização JSON de **todos** os campos efetivos de `AnalysisConfig.Policy`, sem configuração do provider ou credencial. O hash é fixado no `ExecutiveReport`, entra na identidade de exportação junto com análise, modo e renderer, e é persistido no `ExecutiveArtifact`/manifest. Uma política diferente recebe artefato diferente mesmo com PDF visualmente igual. Artefatos antigos sem o campo continuam legíveis, mas não recebem uma identidade de política retroativa.

O schema versionado e a fixture compartilhada trazem o campo e um vetor de hash reprodutível; `TestPolicyHashMatchesSharedFixture` e `TestExecutiveExportUsesSelectionAndCompleteDetailWithIdempotency` verificam o contrato e a idempotência.
