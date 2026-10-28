# decisionengine (L3)

Motor L3 em desenvolvimento contra `insightcontract` do bootstrap comum. O
modo degradado valida entradas e referências, calcula proveniência/chave de
invalidação, aplica a política de conteúdo e mantém cases/objetivos
indeterminados. O adapter OpenAI Responses é explícito, não tem modelo default
e só é usado se a integração injetar provider e credencial já resolvidos.
Provider externo exige `ExternalProcessingAllowed=true` em qualquer `ContentMode`;
`local_excerpts` só passa com provider que declare localidade. Nenhuma chamada
externa foi feita nesta implementação.

## Verificações locais

Pré-requisito: Go 1.22 ou mais recente. Os testes usam fixtures e um
round-tripper local; não precisam de chave, variável de ambiente ou rede.

```sh
gofmt -w internal/investigation/decisionengine/*.go
go test -buildvcs=false ./internal/investigation/decisionengine ./internal/investigation/insightcontract
go test -buildvcs=false -run TestSyntheticGoldenBenchmark -v ./internal/investigation/decisionengine
```

## Aceite e dependências

| Requisito | Situação nesta entrega |
|---|---|
| INV003-R01 | Atendido: alterações confinadas ao pacote L3 e seus testes/docs locais. |
| INV003-R02 | Parcial: usa tipos canônicos e valida enums, relações, referências cruzadas e validade; faltam invariantes periféricos do contrato. |
| INV003-R03 | Parcial: valida IDs, citações, status e hash de excerto; o adapter faz extração semântica quando executado e toda saída ainda requer avaliação real. |
| INV003-R04 | Parcial: valida escopo contra IDs do bundle, datas RFC3339, direção temporal e validade comparável; não filtra a análise inteira pela janela pedida. |
| INV003-R05 | Parcial: o adapter propõe relações e o validador exige contradição com IDs ordenados, escopo conhecido/igual e validade sobreposta, além de direção posterior em supersedes/revokes. Falta avaliação semântica rotulada. |
| INV003-R06 | Parcial: a rubrica solicita objetivos, impacto, complexidade, alternativas e recomendação rastreáveis; sem provider executado, os eixos ficam indeterminados e não há avaliação empírica. |
| INV003-R07 | Parcial: confiança e impacto permanecem separados; desconhecidos não viram score. Falta avaliação sustentada pelo provider. |
| INV003-R08 | Parcial: provider ausente, erro ou política restritiva resulta em modo degradado sem inventar julgamento. |
| INV003-R09 | Parcial: adapter OpenAI Responses usa endpoint/JSON mode documentados, chave e modelo explicitamente injetados, sem default/fallback. Falta integrar perfil/credencial e executar avaliação real autorizada. Jev não serve para a análise inteira porque retorna `choice`/`score`/`noul`, sem gerar texto/DecisionAnalysis. |
| INV003-R10 | Parcial: golden offline mede aceitação do validador por classe. Para cada classe de relação: total 2, positivos 1, precisão 1/1, cobertura 1/1, abstenção 1/2. Isso mede regras determinísticas, não qualidade do modelo; falta dimensão e avaliação real. |
| INV003-R11 | Parcial: chave de cache inclui entrada/configuração/versões e o adapter informa tokens/latência reportados; não há armazenamento/reuso de cache nem preço reportado. `MaxCost` bloqueia chamada quando preço não é verificável. |
| INV003-R12 | Parcial: `metadata_only` remove trechos e texto livre derivado da fonte; o prompt trata o conteúdo como dado não confiável. Falta avaliação de segurança com modelo configurado. |

Fontes públicas consultadas: [OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs) documenta JSON mode/`text.format`; o adapter pede JSON e valida o resultado no Go. [TypeSafe Quick start](https://docs.typesafe.ai/introduction/quickstart) e [Primitives](https://docs.typesafe.ai/primitives) documentam os tipos de resposta do Jev, que não incluem geração de texto.

## Handoff

Base: commit de bootstrap `cdadfef769d5f5b5a163a0cc583dded512dbffd1`, filho da
referência de comparação `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67`. Para
aceite total ainda faltam integração de profile/credencial, benchmark por
dimensão, execução real autorizada e revisão da qualidade semântica. Nenhum arquivo de
outra frente foi alterado.
