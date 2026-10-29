# RFC-INV-COR-L4-003 — headline do detalhe informa modo incorreto

| Campo | Preencher |
|---|---|
| Estado | validada na branch `codex/investigation-insights-integration` |
| Data e autor | 28/10/2026 — Sol (fechamento local) |
| RFC/requisito de origem | RFC-INV-004 R10; INV005-R06, R13 |
| Responsável pela implementação | Sol, correção de apresentação delimitada |
| Modelo/esforço | Codex desta sessão; a ferramenta não expõe seleção de Sol/high |
| Base e head observados | `cdadfef769d5f5b5a163a0cc583dded512dbffd1`; integração antes da correção `453034e` |
| Contrato/pacote | `investigate.insights.v1`, SHA-256 `c1abd2c95a2f0da87a24fe902285eef0ad067d83ceca4be15ba13d9fd4b9eeab` |
| Gravidade e dimensão | Baixa; rótulo editorial inconsistente na exportação completa; confiança alta (reproduzido no PDF sintético) |
| Dependências | `presentation.Build` e `report.layoutExecutive` da L4; nenhuma mudança de contrato |

## 1. Problema e evidência

`presentation.headline` sempre gera “N caso(s) para leitura executiva”. `layoutExecutive` reutiliza esse texto também quando `mode="analysis_detail"`. A renderização do PDF detalhado mostra “1 caso(s) para leitura executiva” mesmo que a seção seguinte seja “Casos para leitura” completa e as evidências incluam todos os casos. O estado funcional e a contagem do artefato continuam corretos; o título descreve o modo errado.

## 2. Esperado versus observado

| Aspecto | Esperado pelo contrato | Observado |
|---|---|---|
| Modo executivo | Título pode indicar a seleção resumida | Correto: leitura executiva |
| Modo detalhado | Título informa análise completa/detalhada | Herda headline de seleção executiva |
| Consequência | Usuário identifica sem ambiguidade a exportação completa | Cabeçalho contradiz o modo e a seção de detalhe |

## 3. Reprodução mínima

Pré-requisitos: Go e Poppler. Sem rede ou credenciais.

```sh
POCKETCLI_INSIGHTS_DEMO_DIR="$PWD/output/pdf" go test -buildvcs=false -count=1 -run TestOfflineRuntimeToExecutiveAndDetailPDF ./tests/investigation/insights
pdftotext output/pdf/pocketcli-insights-demo-detail.pdf - | head -30
```

Saída observada no PDF detalhado: `1 caso(s) para leitura executiva`.

## 4. Correção proposta

Ao compor o PDF com `mode="analysis_detail"`, substituir o headline de seleção por um rótulo simples derivado de `TotalCases`, como “Análise detalhada · N casos”. Preservar o headline do `ExecutiveReport` e o PDF executivo. Adicionar asserção de texto no teste de layout/artefato e regenerar/inspecionar os dois PDFs.

Arquivos: `internal/investigation/report/executive.go` e `internal/investigation/report/executive_test.go`. Sem alterar a ordenação, os campos do artefato ou o cálculo de casos.

## 5. Contrato e compatibilidade

- Nenhum campo ou enum wire muda.
- `ExecutiveReport.Headline` continua descrevendo a visão executiva persistida.
- Apenas o título impresso na exportação detalhada varia por modo; PDFs anteriores permanecem imutáveis.

## 6. Passos para reentrega

1. Reproduzir a diferença visual no PDF executivo e no detalhado.
2. Alterar somente o título do PDF `analysis_detail`.
3. Assegurar que o executivo continue exibindo headline de leitura executiva.
4. Rodar os testes do pacote `report` e a integração offline de insights; renderizar e revisar as páginas finais.

## 7. Critérios de aceite

- PDF executivo mantém “leitura executiva”.
- PDF detalhado diz explicitamente “análise detalhada” e mostra `TotalCases`.
- `SelectedCaseCount == TotalCaseCount` no export detalhado continua verdadeiro.
- PDFs gerados passam `pdfinfo`, começam com `%PDF-` e renderizam sem corte/overlap.

## 8. Validação e encerramento

Estado: **validada** em 28/10/2026 na branch integrada. A regressão é coberta por `TestExecutiveAndDetailPDFUseModeSpecificHeadline`; o texto executivo continua preservado e o detalhe imprime “Análise detalhada · 1 caso”. Os artefatos finais passaram `pdfinfo` (3 páginas executivo, 4 detalhe, A4), renderização por Poppler e inspeção visual das páginas; sem corte ou sobreposição.

```sh
go test -buildvcs=false -count=1 ./internal/investigation/report
POCKETCLI_INSIGHTS_DEMO_DIR="$PWD/output/pdf" go test -buildvcs=false -count=1 -run TestOfflineRuntimeToExecutiveAndDetailPDF ./tests/investigation/insights
pdfinfo output/pdf/pocketcli-insights-demo-executive.pdf
pdfinfo output/pdf/pocketcli-insights-demo-detail.pdf
```

Os testes passaram. PDFs são amostras de fixture sintética e não medem qualidade de provider nem usam dados reais.

## 9. Prompt de reentrega

> Você é Sol fechando a L4. Reproduza o título incorreto de `analysis_detail` com a fixture sintética. Ajuste somente o título de modo na exportação detalhada; não altere o `ExecutiveReport.Headline` persistido ou os PDFs executivos. Execute critérios e inspeção visual. Sem dados reais, provider ou chamadas externas.
