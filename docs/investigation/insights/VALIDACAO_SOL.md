# Validação final Sol — Investigation Insights

**Data:** 28/10/2026
**Conclusão:** integrado com validações pendentes
**Branch:** `codex/investigation-insights-integration`
**Base comum:** `cdadfef769d5f5b5a163a0cc583dded512dbffd1`
**Contrato:** `investigate.insights.v1`, SHA-256 `c1abd2c95a2f0da87a24fe902285eef0ad067d83ceca4be15ba13d9fd4b9eeab`

## Escopo e entradas

Integração feita em worktree separada, sem alterar o checkout de origem e sem publicar, fazer push ou abrir PR. Os quatro commits foram preservados por merges `--no-ff`, em ordem L1 → L2 → L3 → L4. As branches L2–L4 usam `cdadfef`; L1 usa `8cbd526`. A diferença entre essas bases é uma reorganização documental: contrato e fixtures compartilhados têm árvores idênticas. O handoff L3, inicialmente apenas local/não commitado, foi registrado em `d2599b0` antes da integração.

| Frente | Branch / commit integrado | Resultado declarado no handoff |
|---|---|---|
| L1 Insights | `codex/luna-1-insights` / `3ca7902` | Serviço assíncrono, reader, persistência e testes; validado localmente. |
| L2 Analysis Facts | `codex/luna-2-analysisfacts` / `691654b` | Facts, métricas e cobertura; validado em pacotes focados; sem suíte/build/smoke completos na frente. |
| L3 Decision Engine | `codex/luna-3-decisionengine` / `d2599b0` | Motor determinístico, adaptador e handoff parcial; fixtures/golden e `go vet` passam; provider real e benchmark semântico pendentes. |
| L4 Executive | `codex/luna-4-executive` / `c2ca558` | Overview, navegação e export; parcial por dependências de contrato e teste de UI em browser pendentes. |

As instruções do pacote foram tratadas como requisitos de execução; comandos dentro dos documentos não foram considerados evidência até serem executados nesta branch. Nenhum provider, credencial, Claude ou dado empresarial foi usado. A suíte shell geral executa verificações genéricas de DNS/conectividade, sem acessar APIs de investigação ou providers.

## Correções Sol

1. **RFC-INV-COR-INT-001 — validada nesta branch.** L1 e L2 calculavam `ConfigHash` diferentemente, L1 confundia o hash da fonte com a proveniência bundle+facts de L3 e abortava antes do callback quando o provider era nulo. Um helper comum agora fixa esses hashes; o serviço permite callback com provider ausente e resultado degradado; o composition root resolve a profile somente na ação explícita. Sem mudança de wire.
2. **RFC-INV-COR-L4-003 — validada nesta branch.** O PDF detalhado reutilizava o headline “para leitura executiva”. Agora o cabeçalho identifica “Análise detalhada” e a contagem total; a headline persistida/executiva não muda.

As RFCs L4-001 e L4-002 continuam propostas pelo handoff: o contrato não representa metadados completos de objetivos/timeline/suficiência normativa nem fingerprint da política editorial. Mudanças de domínio/contrato não foram improvisadas no fechamento.

## Matriz de requisitos RFC-INV-000–005

| RFC / requisitos | Estado | Evidência ou pendência |
|---|---|---|
| INV-000 contrato e fluxo | Parcialmente atendido | Contrato congelado e integração source → facts → reasoning → presentation → persistência coberta por fixture sintética. Configuração local validada. Metadados/fingerprint adicionais continuam nas RFCs L4-001/002. |
| INV-001 L1 dados, evidências, cobertura | Parcialmente atendido | Handoff L1 e suíte completa local passaram; fluxo integrado não recolhe novamente. A semântica do provider não foi exercitada com endpoint real. |
| INV-002 facts, métricas, dependências | Parcialmente atendido | Handoff L2 e integração sintética validam métricas/facts. Não há dataset real rotulado para validar coortes e cobertura empíricas. |
| INV-003 decisões/julgamento | Parcial, gate empírico pendente | Motor determinístico e fixtures L3 integrados; provider/credencial não disponíveis nesta execução. Precisão, falsos alertas/conflitos e taxa de abstenção não medidos em benchmark real rotulado. |
| INV-004 apresentação, UI e export | Parcialmente atendido | L4 conectado no composition root; PDFs executivo e detalhado foram gerados e revisados. RFCs L4-001/002, ciclo UI em browser e bookmarks continuam pendentes conforme handoff. |
| INV-005-R01 handoffs encerrados | Atendido | Quatro handoffs identificados; entregas parciais e impedimentos documentados. |
| INV-005-R02 integração preservando commits | Atendido | Quatro merges `--no-ff` na branch Sol; bases conferidas; branches Luna não resetadas. |
| INV-005-R03 coordenador L1 e pipeline | Atendido para fixture | Reutiliza `insights.NewService`, reader/controller; injeta facts, decision engine, presentation e store. Teste completo offline termina e exporta. |
| INV-005-R04 wiring e ação explícita | Atendido | CLI `pocket investigate analyze --snapshot ID --config CAMINHO_LOCAL_JSON`; config local estrito; sem inferência ao abrir snapshot. Profile opcional central em `~/.config/pocketcli/profile/investigation-providers.json`; credencial vem do nome de variável de ambiente configurado, não do JSON. |
| INV-005-R05 persistência/IDs | Atendido pelo L1 + integração | Namespaces e IDs do serviço exercitados na pipeline offline; sem migração de artefatos de desenvolvimento. |
| INV-005-R06 PDFs versionados por modo | Parcialmente atendido | Artefatos executivo/detalhado vinculados a análise e renderer e gerados. Política não tem fingerprint canônico no contrato (RFC L4-002). |
| INV-005-R07 modo degradado e retry | Atendido na fixture degradada | Teste usa provider ausente, preserva fatos e limitações e termina sem conclusão semântica positiva; sem chamada externa. Retry de export mantém reader/persistência L4. |
| INV-005-R08–R10 RFCs de correção | Atendido | As duas correções locais têm RFC criada antes da mudança, reprodução, critérios e estado explícito. |
| INV-005-R11 gates técnicos | Atendido | Comandos e resultados efetivamente executados estão abaixo. |
| INV-005-R12 execução real de provider | Pendente | Não havia profile/credencial autorizada nem dataset de avaliação. Caminho sintético completo está validado sem provider; adaptação real não foi chamada. |
| INV-005-R13 avaliação do produto | Parcial | PDF sintético mostra escopo solicitado/observado, cobertura insuficiente, evidências, limitações e estados; revisão visual aprovada. Perguntas de aging, coortes, urgência/conflito e qualidade do julgamento exigem dados rotulados e contrato completo; não se inferiu aprovação a partir da fixture. |
| INV-005-R14 benchmark | Pendente | Não existe conjunto real rotulado/split nesta entrega; não foram calculadas métricas de qualidade semântica. |

## Ambiente e verificações

Ambiente local macOS arm64; Go 1.26.1; `git`, `sh`, compilador C/CGO disponíveis. Não foram requeridos tokens ou variáveis de ambiente. `gofmt -l` nas áreas alteradas retornou vazio.

| Comando | Resultado |
|---|---|
| `go test -buildvcs=false -count=1 ./cmd/pocket ./internal/investigation/insightcontract ./internal/investigation/analysisfacts ./internal/investigation/decisionengine ./internal/investigation/insights ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web ./tests/investigation/insights` | Passou. |
| `go test -buildvcs=false ./...` | Passou. |
| `CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite ./...` | Passou. |
| `CGO_ENABLED=1 go build -buildvcs=false -tags investigate_sqlite -o /tmp/pocket-inv-rfc ./cmd/pocket` | Passou. |
| `POCKETCLI_GO_BINARY=/tmp/pocket-inv-rfc sh tests/run_smoke.sh` | Passou, 7/7. |
| `POCKETCLI_SHELL_SUITE_BUDGET=90 sh scripts/run_local_ci.sh` | Passou: shell 33/33, testes Go, build e smoke 7/7. |
| `go test -buildvcs=false -count=1 ./internal/investigation/report` | Passou. |
| `CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite -run TestOfflineRuntimeToPDF -v ./tests/investigation/core` | Passou; PDF de 3 páginas, sem transporte/proxy. |
| `POCKETCLI_INSIGHTS_DEMO_DIR="$PWD/output/pdf" go test -buildvcs=false -count=1 -run TestOfflineRuntimeToExecutiveAndDetailPDF ./tests/investigation/insights` | Passou; gerou os dois PDFs desta entrega pela fixture sintética. |
| `pdfinfo output/pdf/pocketcli-insights-demo-executive.pdf` / `...detail.pdf` | 3 e 4 páginas, A4. |
| Renderização com `pdftoppm` e inspeção visual das páginas | Sem corte/overlap; estados parcial/insuficiente e proveniência visíveis; detalhe agora rotulado como análise detalhada. |
| `gofmt -l` nas áreas Go alteradas; `git diff --check` | Saída vazia; passou após documentação e RFCs finais. |

O smoke local não substitui uma validação com uma instalação configurada ou fontes reais. `go vet`/golden do L3 foram executados na branch L3 antes da integração; nenhum benchmark real foi executado. A inspeção com Poppler é somente ferramenta de revisão local; a geração em produção usa PDF nativo.

## Artefatos e próximos gates

- `output/pdf/pocketcli-insights-demo-executive.pdf` — amostra executiva sintética.
- `output/pdf/pocketcli-insights-demo-detail.pdf` — export detalhado sintético, com coleção completa da fixture.
- `output/pdf/README.md` — proveniência e comando de regeneração.
- `docs/investigation/insights/README.md` — CLI, profile, política externa, pré-requisitos e comandos locais.

Para chegar a **integrado e validado**, ainda é necessário ratificar as RFCs L4-001/002 (eventualmente com nova entrega L4), exercitar o ciclo da UI em browser, configurar um adapter autorizado e medir a semântica em dataset rotulado com split documentado. Até então, usar esta branch somente como integração local revisável e manter a conclusão **integrado com validações pendentes**.

Comparação posterior de fonte real: [VALIDACAO_SMF_GITHUB.md](VALIDACAO_SMF_GITHUB.md) registra uma consulta somente leitura ao repositório privado `irinery/socialMediaFactory` em 28/10/2026. A comparação não executa coleta do PocketCli e não altera os gates pendentes acima.
