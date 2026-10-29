# Prompt LUNA-1 — Implementação L1 (RFC-INV-001)

Você é o agente de implementação da pista L1 do PocketCli Investigation. Trabalhe com esforço alto, em branch/worktree isolada, e implemente somente o escopo de RFC-INV-001. Não faça chamadas reais a provedores/serviços externos durante o desenvolvimento offline, não use Claude e não faça coleta real. O serviço implementado deve delegar ao callback de interpretação conforme a política, usando fakes nos seus testes. Use apenas testes offline e fixtures sintéticas.

## Leitura obrigatória antes de editar

Leia integralmente, nesta ordem:

1. `README.md` do repositório e `AGENTS.md`.
2. `rfcs/RFC-INV-000_Contrato_Execucao.md` e `rfcs/RFC-INV-001_Dados_Evidencias_Cobertura.md` neste pacote.
3. O contrato Go canônico congelado `contracts/insight_v1.go`, que será copiado para `internal/investigation/insightcontract/v1.go`, e as fixtures comuns `fixtures/cases.json` e `fixtures/fixture_go.json`.
4. Os arquivos de código apontados pela RFC-INV-001: core de coleta, modelo interno do dataset, `storage.Store`, cursores/journals, evidências e vínculos.

Confirme o commit/base atual e compare com a revisão `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67` indicada em RFC-INV-000. Se houver mudanças do usuário ou divergência, preserve-as e registre o que afeta seu trabalho; nunca resete a árvore. Não dependa de arquivo arquivo de contexto externo para entender a tarefa: esta RFC, RFC-INV-000, contrato e fixtures contêm o contexto necessário. O contrato RFC-INV-000 é canônico: não redefina, duplique, renomeie ou “corrija” tipos por conta própria. Se estiver faltando campo indispensável, pare a implementação dependente e solicite-o ao root; prossiga nos itens independentes. Não espere a integração das outras pistas: compile contra contrato e fixtures comuns.

## Objetivo e fronteiras

Implemente a leitura L1 de snapshot imutável para `insightcontract.SourceBundle`, pela assinatura definida em RFC-INV-000. Preencha os campos canônicos `Snapshot`, `RequestedScope`, `ObservedResources`, `Coverage`, `Questions`, `Queues`, `Entities`, `Evidence`, `Links`, `Events`, `Stages`, `Estimates`, `Deliveries`, `LegacyFindings` e `Limitations`. O pacote público L1 é o contrato `insightcontract`, não o modelo interno; converta sem expor/redefinir tipos internos. Separe seleção solicitada, recursos observados, alcance histórico e `ReportWindow`. Use `HistoryCoveredIntervals` para cobertura comprovada; `HistoryObservedFrom/To` é somente o envelope, não prova continuidade. Preencha `Snapshot.SchemaVersion` e `QueryCatalogVersion`. Use enums e semântica de tempo/ausência de RFC-INV-000. Para vínculos explícitos da fonte use `Link.Status=explicit`; candidatos continuam `candidate`. Use `EvidenceView.ContentStatus`, `IntegrityStatus`, `Redacted` e `Truncated` como dimensões distintas. Use armazenamento e mecanismos legados existentes onde possível; não faça migração SQL ou redesenho do coletor sem prova de necessidade.

Edite apenas arquivos sob os limites L1 definidos em RFC-INV-000 §3 e RFC-INV-001: `internal/investigation/core/*`, `internal/investigation/collect/*`, `internal/investigation/gitmirror/*`, `internal/investigation/storage/*`, novo `internal/investigation/insights/*` e testes correspondentes. `insights` implementa `AnalysisController`, `InsightReader`, `Dependencies`, `NewService` e `Shutdown`. Não altere `analysisfacts/*`, `rules/*`, `decisionengine/*`, `presentation/*`, `report/*`, `web/*`, `editorial/*`, `query/*`, `internal/investigation/contract/v1.go` ou `cmd/*`. Sol só conecta callbacks reais no composition root depois dos handoffs. Se algo exigir arquivo fora da fronteira, não o altere: registre como correção em `RFC-INV-COR-L1-NNN` seguindo o template comum, incluindo reprodução, arquivo dono, risco e teste; entregue a RFC-COR junto do handoff.

## Regras funcionais obrigatórias

- Trate `SourceCoverage.Expected=nil` e `QueueProgress.Pending=nil` como desconhecidos, nunca como 0 ou 100%. Preserve diferenças entre não selecionado, pendente, parcial, interrompido, falho e completo conforme enums do contrato; preencha `QuestionCoverage.RequiredCollections` e `Missing` sem inventar completude.
- Deduplicate fila por identidade estável. Não some pendências repetidas por coleção nem alegue `observed + pending` como universo.
- Checkpoint não confirma cobertura. Só a conclusão completa avança watermark. Retomada deve ser idempotente, e snapshots antigos não mudam.
- Fixe cobertura, intervalos e resumo das filas em sidecar imutável vinculado ao snapshot/dataset hash no momento da publicação. `ReadInsightSource` nunca consulta journal ou cursor atual para rotular snapshot antigo. Snapshot legado sem metadata fixada resulta em `unknown`/`insufficient` e limitação, sem reconstrução retrospectiva.
- `ReadInsightSource` lê o snapshot e armazenamento local indicados. É somente leitura, sem rede e sem disparar coleta.
- Para evidência, carregue objeto pela referência existente, recalcule SHA-256 e compare com `EvidenceView.ContentHash`. Preencha `ContentStatus`, `IntegrityStatus`, `Excerpt`, `ExcerptHash` e `SanitizationVersion`; flags `Redacted` e `Truncated` permanecem separadas. Produza somente a vista semântica autorizada derivada do objeto verificado; respeite política de redação e truncamento. Não fabrique resumo ou fato para objeto ausente/corrompido.
- Preserve `Link.FromID`, `ToID`, `Relation`, `Status`, `TemporalStatus`, `ValidFrom`, `ValidTo` e `EvidenceIDs`; vínculos formais explícitos usam `Status=explicit`, candidatos seguem `candidate`. Valide endpoints/evidência. Não promova menções ou relações órfãs a causa/impacto.
- A saída deve ser determinística para snapshot/config iguais e não conter credenciais, tokens ou dados de outro escopo.
- Mantenha `investigate.v1`, fixtures e consumidores existentes compatíveis. Não calcule métricas/coortes/regras (L2), decisões/causas (L3) nem apresentação (L4).
- Implemente as operações de `AnalysisController`/`InsightReader` exatamente como no contrato e RFC-INV-000 §5.1. O start retorna rápido uma run persistida e agenda callbacks assíncronos; use estados `queued|running|completed|completed_partial|failed|cancelled` e fases `source|facts|reasoning|presentation|persisting|idle`. Mesmo `IdempotencyKey` + mesmo hash devolve a mesma run; chave igual com request diferente é conflito. Cancelar interrompe sem apagar artefatos já publicados. Listagens e leituras são limitadas ao snapshot/análise e usam cursores estáveis.
- Construa `Dependencies` com callbacks/funções injetados; não importe implementações reais de L2/L3/L4. Teste o pipeline inteiro com fakes. Persista cada artefato de forma imutável em namespaces privados por snapshot/análise/config hashes. Após restart, runs não terminais viram falha terminal e não disparam auto-replay pago. `Shutdown(ctx)` cancela jobs e aguarda limpeza.

## Passos pequenos

1. Liste os campos existentes do contrato e faça um mapa curto entre eles e os dados legados. Identifique lacunas sem alterar o contrato canônico; envie solicitação concreta ao root e prossiga nos itens independentes.
2. Implemente o leitor e a tradução de metadados/proveniência com validação do snapshot.
3. Mapeie seleção pedida, recursos observados, `History`, `ReportWindow` e `Snapshot.Window`; use os cursores existentes para o fluxo autorizado de coleta histórica retomável. `ReadInsightSource` permanece offline e não inicia fetch.
4. Na publicação, fixe cobertura/intervalos/filas em sidecar imutável. Faça a leitura depender somente do sidecar do snapshot; para legado sem sidecar, retorne unknown/insufficient.
5. Acrescente leitura local, validação do digest e vista semântica segura das evidências.
6. Acrescente preservação/validação de vínculos formais.
7. Adicione testes offline: histórico quatro anos + janela sete dias; diferença entre recursos solicitados e observados; intervalos com lacunas; parcial vs não selecionado; fila duplicada; retomada; sidecar congelado apesar de mudança em cursor/journal; snapshot legado sem sidecar; digest válido/inválido; conteúdo redigido/truncado/ausente; link explícito e relação órfã.
8. Implemente `internal/investigation/insights.Service` com fakes para todas as callbacks, execução assíncrona persistida, idempotência, cancelamento, shutdown, recuperação sem replay, `ListAnalyses` e `InsightReader` paginado/validado.
9. Teste retorno antes de callbacks bloqueantes, chave idempotente igual/divergente, erro em cada fase, cancelamento durante fase, shutdown com job ativo, restart sem replay e isolamento de leituras por snapshot/análise.
10. Rode os gates locais aplicáveis documentados em `docs/investigation/sol-implementation.md` e `AGENTS.md`: gofmt, testes Go, build e smokes; não acesse contas ou APIs externas.

Não amplia o escopo para “corrigir” problemas adjacentes. Toda correção adicional necessária deve ser registrada em uma `RFC-COR` e enviada ao dono correspondente; não a implemente neste branch se ultrapassar a fronteira L1.

## Entrega e handoff

Faça commits locais pequenos e claros; não faça push, merge ou alteração de branch compartilhada. No handoff informe:

- commit base e commit(s) produzidos;
- arquivos alterados e requisito(s) `INV001-Rxx` atendidos por cada grupo;
- testes/comandos executados e resultados;
- campos de contrato que faltaram e solicitações feitas ao root (não deixe dependência de confirmar caminho: os caminhos e nomes acima são canônicos);
- cada `RFC-INV-COR-L1-NNN` criada, com reprodução e próximo dono;
- limitações restantes, sem alegar cobertura além da evidência observada.

Considere concluído somente quando o aceite verificável de RFC-INV-001 estiver satisfeito e o handoff estiver completo.
