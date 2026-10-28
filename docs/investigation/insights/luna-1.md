# Handoff L1 — RFC-INV-001

**Estado:** concluído para a fronteira L1, com limites desconhecidos preservados.  
**Base comum:** `8cbd526ea327b4b3bc5ca2defe0a12a2bc40b901`. **Base auditada:** `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67`.  
**Branch/worktree:** `codex/luna-1-insights` / `/Users/irinery/.codex/worktrees/luna-1-insights/PocketCli`.  
**Commits L1:** `51c0e72`, `df24259`, `2c15daa`, `0263329`; ver `git log` para hashes completos.  
**Contrato:** `investigate.insights.v1`, copiado e formatado no bootstrap comum.  
**Modelo efetivo:** Codex GPT-6 nesta sessão. O prompt do pacote pede Luna/high; essa configuração não estava disponível neste agente.

## Entrega

- `core.ReadInsightSource` valida snapshot, dataset e digest; projeta o contrato congelado sem editar `investigate.v1`.
- A seleção pedida, recursos observados, janela do snapshot, alcance histórico, evidências e vínculos ficam separados. Dados fora dos recursos solicitados são filtrados.
- A publicação fixa cobertura, intervalos confirmados e progresso de cursores em `insightcoverage`, imutável e ligado ao `snapshotID`/`datasetHash`. Leituras não consultam cursores atuais. Snapshots antigos sem sidecar ficam `unknown`/`insufficient`.
- Evidências são lidas localmente, têm SHA-256 recalculado e só expõem campos semânticos autorizados com redação/truncamento e hashes separados. Conteúdo ausente ou divergente não é enviado adiante como trecho válido.
- Links são preservados com endpoints, validade e evidências; referências textuais e mapeamentos derivados permanecem `candidate`, enquanto relações de fonte enumeradas como explícitas só são promovidas após validar endpoints e evidência.
- `insights.Service` implementa controller e reader persistidos com callbacks injetáveis: start assíncrono, idempotência, erros por fase, cancelamento, shutdown, paginação, validação de referências e recuperação terminal sem replay.

## Rastreabilidade

| Requisito | Evidência nesta branch |
|---|---|
| INV001-R01 | `SnapshotRef`, `RequestedScope` e `ObservedResources` separados; teste cobre quatro anos, janela semanal e recurso fora do escopo. |
| INV001-R02 | Estados canônicos, `Expected=nil`, envelope observado e intervalos fixados; lacuna nunca vira cobertura contínua. |
| INV001-R03 | IDs de fila estáveis, referências deduplicadas no manifesto e `Pending=nil` quando o legado não conhece quantidade/universo. |
| INV001-R04 | Checkpoint exposto só como ID com hash; sem cursor bruto. `RetryAfter`/`ErrorCode` permanecem nulos quando o legado não os fornece. |
| INV001-R05 | Reutiliza `sourceCursor` e suas regras de checkpoint/watermark; sidecar não altera objeto ou snapshot legado. |
| INV001-R06 | Sidecar imutável na montagem/publicação; teste altera o cursor depois e prova que o manifesto não muda; legado sem sidecar continua desconhecido. |
| INV001-R07–R08 | Testes cobrem digest válido/divergente, objeto ausente, `metadata_only`, redação e truncamento sem rede/provedor. |
| INV001-R09 | Testes cobrem vínculo explícito, menção/declarative fix como candidato e endpoint órfão inválido. |
| INV001-R10 | Todos os arrays canônicos são inicializados e listas ordenadas; hash de bundle repetido é determinístico. Fixture comum decodifica contra o contrato. |
| INV001-R11 | Mudança aditiva em namespaces privados; sem alteração em `contract/v1.go`, snapshots, relatório ou UI legados. |
| INV001-R12 | Testes de start antes de callback bloqueante, chave igual/divergente, falhas, cancelamento, listagens/páginas e isolamento por snapshot/análise. |
| INV001-R13 | `Dependencies`, `NewService`, callbacks fake e `Shutdown`; teste cobre encerramento de job ativo e reinício sem auto-replay. |

## Validação executada

Pré-requisitos: Go 1.22+ e `sh`; `GOCACHE` gravável. Nos comandos abaixo usei `/tmp/pocketcli-go-build-cache`.

| Comando | Resultado |
|---|---|
| `python3 /Users/irinery/Downloads/PocketCli_Luna_1_Alto_2026-10-28/tools/validate_package.py` | Passou: 50 estruturas Go, 24 casos sintéticos, 8 digests de evidência e 15 arquivos do manifesto. |
| `env GOCACHE=/tmp/pocketcli-go-build-cache go test -buildvcs=false ./internal/investigation/core ./internal/investigation/insights ./internal/investigation/insightcontract` | Passou. |
| `env GOCACHE=/tmp/pocketcli-go-build-cache go test -buildvcs=false ./...` | Passou em todos os pacotes. |
| `env GOCACHE=/tmp/pocketcli-go-build-cache go build -buildvcs=false -o /tmp/pocketcli-l1 ./cmd/pocket` | Passou. |
| `env POCKETCLI_GO_BINARY=/tmp/pocketcli-l1 sh tests/run_smoke.sh` | Passou: 7/7. |
| `sh scripts/run_local_ci.sh` | A suíte shell passou 33/33, mas o agregador encerrou com status de falha porque levou 96 s e tem teto de 45 s; por isso os gates Go seguintes foram rodados manualmente acima. O teste de ambiente `test_ish.sh` fez apenas suas verificações de conectividade/DNS/Tailscale; nenhum provedor GitHub/Linear foi chamado e nenhuma coleta real foi feita. |

## Limites registrados

- O legado não fornece contagem confiável de pendências por fila, `RetryAfter` nem `ErrorCode` estruturados. Esses campos ficam nulos/desconhecidos; não há percentual nem universo inferido.
- A cobertura GitHub pode comprovar que a listagem percorreu um intervalo, mas não reconstruir estados históricos. `history_scope` continua parcial mesmo quando os cursores cobrem o período.
- O catálogo de perguntas é conservador: sem meta/critério explícito, `objective_status` é insuficiente; custo real ausente permanece insuficiente.
- Callbacks reais de fatos, julgamento e apresentação não foram conectados, conforme a fronteira de L1. Nenhum adaptador, provedor, coleta ou teste com dados reais foi executado.
- Não criei RFC-COR: os limites encontrados puderam ser representados com campos/statuses canônicos e limitações sem editar arquivos de outras frentes.

Sol não foi iniciado. Esta branch encerra somente L1 e aguarda as demais frentes/handoffs previstos pelo pacote.
