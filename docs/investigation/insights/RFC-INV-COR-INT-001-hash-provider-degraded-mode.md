# RFC-INV-COR-INT-001 — alinhar identidade de configuração e análise degradada

| Campo | Preencher |
|---|---|
| Estado | validada na branch `codex/investigation-insights-integration` |
| Data e autor | 28/10/2026 — Sol (fechamento local) |
| RFC/requisito de origem | RFC-INV-000 §§5.1, 7; RFC-INV-002-R01; INV005-R03, R07, R12 |
| Responsável pela implementação | Sol, correção transversal pequena e delimitada |
| Modelo/esforço | Codex desta sessão; a ferramenta não expõe seleção de Sol/high |
| Base e head observados | `cdadfef769d5f5b5a163a0cc583dded512dbffd1` bootstrap; integração `453034e` antes da correção; HEAD final registrado no commit da entrega |
| Contrato/pacote | `investigate.insights.v1`, SHA-256 `c1abd2c95a2f0da87a24fe902285eef0ad067d83ceca4be15ba13d9fd4b9eeab` |
| Gravidade e dimensão | Bloqueador funcional de ponta a ponta; confiança alta (reproduzido na fixture comum) |
| Dependências | Handoffs L1 `3ca7902`, L2 `691654b`, L3 `d2599b0`, L4 `c2ca558`; contrato comum sem alteração de campos |

## 1. Problema e evidência

O pipeline usa dois fingerprints diferentes para a mesma configuração. `analysisfacts.Analyze` chama `stableConfigHash`, que canonicaliza slices e acrescenta as versões do schema e do catálogo SQL do snapshot. `insights.Service.StartAnalysis` usa apenas `storage.Hash(json.Marshal(cfg))`. Ao integrar a fixture canônica, L2 devolve o hash correto do snapshot e L1 encerra a run com `ERR_FACTS_INVALID` antes da análise semântica ou apresentação.

Há também dois significados para `InputHash`: L1 fornece o hash da projeção de fonte, enquanto L3 registra em `DecisionAnalysis.Provenance.InputHash` o hash conjunto do bundle e dos facts. A validação de L1 compara esses valores diferentes e encerra com `ERR_REASONING_INVALID` após executar o L3 válido.

O serviço também falha com `ERR_PROVIDER_UNAVAILABLE` antes de chamar `Dependencies.Decisions` quando `Provider` está `nil` e a configuração pede provider externo. Isso impede a decisão callback de resolver o perfil/credencial pelo `AnalysisConfig` da ação explícita, e impede o modo degradado permitido por INV005-R07.

## 2. Esperado versus observado

| Aspecto | Esperado pelo contrato | Observado |
|---|---|---|
| ConfigHash | L1/L2 usam a mesma identidade, incluindo schema, catálogo e configuração canonicalizada | O serviço calcula hash de JSON bruto sem versões; L2 acrescenta ambas e ordena conjuntos |
| InputHash semântico | L1 verifica o bundle+facts consumido pelo L3 | L1 compara o hash semântico de L3 ao hash de fonte anterior aos facts |
| Provider ausente | A callback pode resolver o perfil explicitamente solicitado ou devolver análise degradada sem conclusão positiva | L1 aborta antes da callback quando o provider está ausente |
| Consequência | Caminho sintético source→facts→analysis→view→PDF termina; provider não configurado deixa fatos e limitações disponíveis | A run termina em `ERR_FACTS_INVALID` na fixture; provider resolvido sob demanda nem chega a ser tentado |

## 3. Reprodução mínima

Pré-requisitos: Go conforme `go.mod`; sem CGO, credenciais, rede ou variáveis de ambiente.

Na branch integrada antes da correção:

```sh
go test -buildvcs=false -count=1 -run TestOfflineRuntimeToExecutiveAndDetailPDF -v ./tests/investigation/insights
```

Resultados observados: inicialmente `analysis terminal state=failed error=ERR_FACTS_INVALID`; após alinhar o ConfigHash, `analysis terminal state=failed error=ERR_REASONING_INVALID`. Para o terceiro caso, executar a mesma fixture com `Provider.Kind="configured"`, `ExternalProcessingAllowed=true` e sem perfil/carregador; a run deve alcançar a callback e concluir como parcial/degradada, sem chamada externa.

## 4. Correção proposta

Adicionar helpers puros `insightcontract.ConfigHash(cfg, schemaVersion, queryCatalogVersion)` e `insightcontract.AnalysisInputHash(bundle, facts)`. Usá-los em L1/L2 para a configuração e em L1/L3 para o hash semântico. L1 lê as versões do snapshot local antes de criar a run. Separar os dois significados de hash sem mudar o JSON wire.

Remover o aborto prematuro por provider ausente no coordenador L1. A callback `Decisions` recebe provider `nil` quando indisponível e pode resolver a configuração por ação/profile. `decisionengine.Analyze` já retorna estado degradado com limitações quando não há provider; nenhum fallback afirma julgamento semântico.

Arquivos previstos: `internal/investigation/insightcontract/config_hash.go`, `analysisfacts/analysis.go`, `decisionengine/engine.go`, `insights/service.go` e testes focados; a conexão Sol de `cmd/pocket` injeta o resolver da profile central local. Sem alterar `v1.go`, schema JSON ou wire enums.

## 5. Contrato e compatibilidade

- Nenhum campo, enum ou assinatura do contrato wire muda.
- `ConfigHash` passa a identificar a configuração mais as versões do schema/catálogo e canonicaliza coleções tipo-set, conforme os handoffs L1/L2.
- Runs e análises novas recebem o fingerprint canônico. Artefatos anteriores em namespaces de desenvolvimento não são reescritos nem migrados.
- `Provider.Kind="none"` continua sem inferência. Com `configured` sem provider/profile/credencial, a execução preserva fatos e publica `partial`/`not_run` conforme os dados; nenhum pedido externo é disparado por abertura do snapshot.

## 6. Passos para reentrega

1. Reproduzir o erro na fixture antes da correção.
2. Implementar somente o helper de identidade compartilhada e a continuação degradada do L1.
3. Cobrir igualdade de hash L1/L2, canonicalização e versões divergentes.
4. Cobrir ausência de provider, callback executada e ausência de chamada externa.
5. Executar os testes dos pacotes L1/L2/L3 e `tests/investigation/insights`; registrar resultados reais.

## 7. Critérios de aceite

- O hash calculado por L2 é idêntico ao hash calculado antes do agendamento L1 para mesmo snapshot/config; mudança de schema/catálogo altera o resultado.
- O hash de proveniência L3 corresponde exatamente ao bundle+facts consumido; o hash de source continua separado.
- Reordenar resources, objectives, EntityIDs e EvidenceIDs não muda o hash; mudar schema, catálogo, objetivo, escopo ou política muda o hash.
- A fixture integrada conclui pipeline e produz PDFs executivo/detalhado válidos; detalhe declara todos os casos.
- Sem perfil/provider, callback executa com provider nulo e resultado fica parcial/indeterminado sem envio externo.
- Configuração inválida continua falhando fechada; contrato wire e providers legados não mudam.

## 8. Validação e encerramento

Estado: **validada** em 28/10/2026 na branch de integração. A fixture integrada reproduz o pipeline completo e cobre o caminho sem provider; o ConfigHash é compartilhado e inclui schema/catálogo, e a proveniência semântica usa bundle+facts. Verificações executadas após a correção:

```sh
go test -buildvcs=false -count=1 ./cmd/pocket ./internal/investigation/insightcontract ./internal/investigation/analysisfacts ./internal/investigation/decisionengine ./internal/investigation/insights ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web ./tests/investigation/insights
CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite ./...
```

Ambas passaram; também passaram build, smoke 7/7 e `scripts/run_local_ci.sh` (shell 33/33, Go, build e smoke 7/7). A fixture final `TestOfflineRuntimeToExecutiveAndDetailPDF` conclui source → facts → análise degradada → presentation → persistência → PDFs executivo/detalhado. Não houve profile/credencial configurada nem chamada de provider; a qualidade semântica real permanece pendente pelo gate INV005-R12/R14, sem invalidar a correção de compatibilidade e modo degradado aqui tratada.

## 9. Prompt de reentrega

> Você é Sol fechando o PocketCli Investigation. Leia esta RFC e os handoffs L1/L2. Reproduza `ERR_FACTS_INVALID` com a fixture comum. Corrija apenas o cálculo compartilhado de `ConfigHash` e o aborto de provider ausente que impede a callback L1 de produzir modo degradado. Preserve os campos e versões wire. Execute os critérios de aceite, registre comandos reais e não faça chamadas externas. Não use Claude.
