# RFC-INV-000 — Contrato comum e execução com quatro Lunas

| Campo | Valor |
|---|---|
| Versão do pacote | 1.0 — 28/10/2026, America/Sao_Paulo |
| Estado | Especificado para implementação; código do produto ainda não alterado nesta entrega |
| Base inspecionada | `irinery/PocketCli`, commit `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67` |
| Execução | L1–L4: `gpt-6-luna`, esforço `high`, em paralelo; Sol somente após os quatro handoffs |
| Dono desta RFC | Coordenação do pacote; nenhum Luna altera unilateralmente o contrato |
| Contrato adicional | `investigate.insights.v1`, preservando `investigate.v1` |

## 1. Objetivo e diagnóstico que originam o pacote

O objetivo do usuário é compreender decisões, direção, tempo, custo, complexidade e resultados em uma base de aproximadamente quatro anos, envolvendo Git e Linear. O produto deve condensar essa base em casos prioritários e explicar o que está dentro do esperado, em risco, fora do esperado ou ainda indeterminado.

O PDF `investigation-report-2.pdf`, snapshot `snap-79c16135581ffd329ca27bd4`, tem 366 páginas e uma janela de apenas sete dias, de 21/10/2026 04:12:28Z a 28/10/2026 04:12:28Z. Dos 3.297 registros, 1.982 evidências exibem somente o mesmo placeholder de conteúdo protegido; os primeiros achados aparecem na página 259. Dos 124 achados, 96 são relações de dependência com severidade medium, referentes a 62 sujeitos; 83 desses alertas têm sujeito com intervalo aberto de Backlog. As fontes GitHub não produziram PRs no recorte. A análise não pode transformar esses fatos em uma conclusão sobre desempenho da equipe.

O problema de aceitação é a ausência de julgamento contextual e reconciliação, além de insuficiências de dados e apresentação. Alterar apenas o texto ou o layout não satisfaz este pacote. A matriz `ACHADOS_E_RASTREABILIDADE.md` preserva cada origem e a RFC responsável.

## 2. Requisitos transversais

- **INV000-R01:** cada conclusão gerencial deve ter escopo, referência temporal, evidência compreensível e limites. Uma referência/hash sozinho não basta para uma conclusão semântica.
- **INV000-R02:** desconhecido, zero observado, não selecionado, imaturo e não aplicável são estados diferentes. Nenhum deles será convertido automaticamente em desempenho bom.
- **INV000-R03:** impacto, complexidade, urgência e confiança permanecem separados. Não haverá nota global que compense um risco grave com várias métricas boas, nem multiplicação por confiança que esconda um risco ainda pouco conhecido.
- **INV000-R04:** compromissos, metas, orçamento, baseline e responsáveis ausentes permanecem ausentes. Não estimar custo de pessoas por permanência em status, nem produtividade por commits, pontos ou quantidade de tickets.
- **INV000-R05:** decisões superadas devem sair da lista principal de conflitos sem desaparecer da trilha histórica. A afirmação mais recente não vence automaticamente: escopo, validade e autoridade documentada precisam ser examinados.
- **INV000-R06:** dados de uma fonte não comprovam ausência de fatos em outra. Cobertura parcial não pode gerar inferência negativa sobre trabalho ou qualidade.
- **INV000-R07:** todas as propostas adicionais de correção geram arquivo RFC conforme o template. Mensagem de chat, comentário de PR ou nota de handoff não substitui a RFC.
- **INV000-R08:** Claude não é dependência deste pacote, não será chamado como fallback e não será usado automaticamente em avaliação. O editor legado continua separado. Um uso excepcional exige decisão explícita do usuário após alternativa, motivo e custo serem apresentados.
- **INV000-R09:** configurações personalizáveis devem seguir `profile/` e a resolução central já usada no projeto, conforme `AGENTS.md`. Não espalhar endpoint, modelo, critérios ou orçamento em arquivos versionados.
- **INV000-R10:** reanálises geram artefatos novos ligados ao snapshot. Nenhum snapshot publicado, evidência original ou relatório legado é reescrito para refletir novas regras.

## 3. Quatro frentes realmente independentes

| Frente | Entrega | Arquivos/pacotes de implementação sob sua responsabilidade |
|---|---|---|
| L1 / RFC-INV-001 | Fonte normalizada, escopo, cobertura, evidências e vínculos | `internal/investigation/core/`, `collect/`, `gitmirror/`, `storage/`, novo `insights/`; testes desses pacotes |
| L2 / RFC-INV-002 | Fatos, coortes e agrupamento determinístico | Novo `analysisfacts/` e seus testes; `rules/` somente mediante RFC-COR explícita |
| L3 / RFC-INV-003 | Decisões e avaliação semântica | Novo `internal/investigation/decisionengine/`, seus adaptadores, benchmark e testes |
| L4 / RFC-INV-004 | Visão executiva, detalhe, PDF e navegação | Novo `presentation/`, `internal/investigation/report/` e `web/`; testes desses pacotes |
| Sol / RFC-INV-005 | Conexões finais, integração e revisão | `cmd/pocket/investigate_services.go`, `cmd/pocket/investigate_command.go`, validação integrada e RFCs de correção |

`contract/v1.go`, `query/`, `go.mod`, `go.sum`, scripts de CI, regras globais e documentação compartilhada não têm edição concorrente. Uma necessidade nesses arquivos deve virar RFC-COR-INT com patch proposto. Sol aplica a mudança no fechamento, se ainda necessária e validada. Cada Luna pode documentar sua entrega em `docs/investigation/insights/luna-N.md` e criar testes sob `tests/investigation/insights/lunaN/`; não editar o mesmo README central.

O contrato novo é materializado antes dos quatro worktrees, a partir de `contracts/insight_v1.go`. O responsável pelo bootstrap copia os arquivos fornecidos e registra a base; essa etapa não exige um modelo Sol. Os Lunas não aguardam a implementação uns dos outros: usam a fixture e fakes contra o contrato. A dependência SourceBundle → FactsResult → DecisionAnalysis → ExecutiveReport existe na execução do produto, não na agenda de implementação.

## 4. Bootstrap e divergência de base

1. Ler `AGENTS.md` e registrar HEAD, branch e estado do checkout atual. O commit inspecionado é referência, não uma ordem para resetar o repositório. Preservar alterações locais.
2. Criar um worktree de base a partir do HEAD escolhido e registrar diferenças relevantes em relação à base inspecionada. Se uma correção já existe, provar a equivalência e marcá-la como atendida; não reintroduzir código antigo.
3. Copiar `contracts/insight_v1.go` para `internal/investigation/insightcontract/v1.go`. Copiar `fixtures/fixture_go.json` e `fixtures/cases.json` para `internal/investigation/insightcontract/testdata/`. Copiar as RFCs para uma pasta de documentação própria desta série.
4. Rodar `gofmt` no contrato e `go test -buildvcs=false ./internal/investigation/insightcontract`. Registrar o hash do commit de base comum. O pacote inclui apenas tipos e interfaces; nenhuma funcionalidade é ativada por copiá-lo.
5. Criar quatro branches/worktrees a partir desse mesmo commit. Entregar a cada Luna seu prompt, RFC, contrato, fixtures e esta RFC. Dar início aos quatro em esforço alto; deixar Sol desligado.

Exemplo de preparação, adaptando caminhos antes de executar:

```sh
git worktree add -b rfc/inv-base ../PocketCli-inv-base HEAD
```

Após copiar, formatar e commitar somente os anexos de contrato/documentação no worktree de base, usar o hash desse commit nas quatro chamadas `git worktree add -b rfc/inv-luna-N ... HASH_DA_BASE`. Não criar um agente em cima do branch de outro.

Um conflito real de contrato gera `RFC-INV-COR-INT-NNN`. Continuar a parte autônoma da pista com fixture/fake, explicitar a dependência no handoff e não mudar tipos por conta própria. Sol só entra quando as quatro pistas tiverem terminado seu trabalho ou declarado uma entrega parcial encerrada com a dependência concreta documentada.

## 5. API adicional congelada

O arquivo Go anexo é a definição normativa dos campos e assinaturas. Os exemplos JSON ilustram o contrato; não podem alterá-lo. Os nomes abaixo são referências de implementação planejada, não funções já existentes no commit auditado:

```go
// L1: método adicional, sem mudar contract.Core legado.
(*Core).ReadInsightSource(ctx context.Context, snapshotID string,
    cfg insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error)

// L2: função pura, sem rede nem consulta ao relógio real.
analysisfacts.Analyze(bundle insightcontract.SourceBundle,
    cfg insightcontract.AnalysisConfig) (insightcontract.FactsResult, error)

// L3: interpretação com evidência, orçamento e provedor explicitamente escolhido.
decisionengine.Analyze(ctx context.Context, bundle insightcontract.SourceBundle,
    facts insightcontract.FactsResult, cfg insightcontract.AnalysisConfig,
    provider insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error)

// L4: apresentação determinística, sem recalcular fatos ou pedir inferência ao modelo.
presentation.Build(bundle insightcontract.SourceBundle,
    facts insightcontract.FactsResult, decisions insightcontract.DecisionAnalysis,
    cfg insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error)
```

Essas assinaturas esquemáticas não são um arquivo Go executável; o anexo contém as interfaces compiláveis e os tipos. Os pacotes L1–L4 implementam as funções. `ReasoningProvider.Evaluate` é uma interface interna; não declara o protocolo público de Jev ou de qualquer fornecedor.

`InsightReader` recebe sempre `snapshotID` e `analysisID` em suas leituras. `ListInsightCases` recebe cursor opaco e limite (padrão 50, máximo 200), mantém ordenação estável por ID dentro da análise imutável e rejeita cursor de outra análise. `ExecutiveReport.Cases` e `CaseIDs` são somente a seleção executiva, na mesma ordem; `TotalCases` é o total na análise. `CaseIDs` não contém toda a base nem prova completude. A visão inicial não busca todas as evidências ou todos os casos. `Policy.MaximumExecutiveCases` é uma meta de síntese, não autorização para ocultar casos críticos: casos críticos relevantes permanecem visíveis ou recebem indicação explícita de quantidade e acesso imediato; nunca desaparecem silenciosamente por ordenação ou limite.

L4 implementa `ExecutiveExporter` com `ExportExecutive` e `GetExecutiveArtifact`. `Mode` aceita `executive` ou `analysis_detail`. No primeiro, `SelectedCaseCount` descreve a seleção resumida e o total continua visível; no segundo, é obrigatório percorrer todas as páginas e `SelectedCaseCount == TotalCaseCount`. O inventário canônico legado continua disponível pelo fluxo anterior, sem reinterpretar `FindingCount`, `RecordCount` ou `ReportArtifact`.

O novo artefato carrega `SnapshotID`, `AnalysisID`, hash da análise e PDF, revisão, versão do renderizador e `DownloadPath` relativo à rota local autenticada. O backend resolve o arquivo por ID; não recebe caminhos de sistema arbitrários do cliente. A chave de idempotência inclui análise imutável, modo, política de conteúdo e renderizador. Não reutilizar PDF de outra análise com o mesmo snapshot.

### 5.1. Iniciar, encontrar e selecionar uma análise

`AnalysisController` é implementado por L1 no novo pacote `insights`, junto de `InsightReader`. L4 injeta essas interfaces opcionais na UI e trabalha com fakes enquanto L1 implementa. Sol apenas conecta as implementações no composition root. As operações são:

- `StartAnalysis(ctx, AnalysisRequest)`: valida snapshot/configuração/autorizações já vigentes, agenda execução e retorna `AnalysisRun` com ID; nunca espera o modelo no ciclo da requisição HTTP.
- `GetAnalysisRun(ctx, runID)`: informa estado/fase e `AnalysisID` quando houver análise publicada.
- `CancelAnalysis(ctx, runID)`: cancela o trabalho e preserva artefatos já publicados sem recomeçar cobrança.
- `ListAnalyses(ctx, snapshotID, cursor, limit)`: lista versões disponíveis no snapshot, incluindo `AnalysisSummary.RequestedScope` para a seleção ser compreensível. A UI recebe daí o `analysisID`; nunca inventa o ID nem escolhe a versão de outro escopo silenciosamente.

Rotas novas de L4, mantendo proteções atuais: `POST /insights/runs`, `GET /insights/runs/{runID}`, `POST /insights/runs/{runID}/cancel`, `GET /snapshots/{snapshotID}/insights`, `GET /snapshots/{snapshotID}/insights/{analysisID}`, sufixos `/cases` e `/cases/{caseID}`, `POST /insights/exports`, `GET /insights/exports/{artifactID}` e `/download`. São rotas propostas desta RFC, não endpoints que já existam. Envelopes de erro e validação de sessão/escopo seguem o projeto. `DownloadPath` usa apenas a última rota relativa.

Ao abrir snapshot: listar análises existentes, mostrar escolha/escopo e abrir a selecionada; se não houver, mostrar a ação explícita de analisar e seus requisitos. Polling do run é limitado e termina no estado terminal; fechar uma aba não cancela implicitamente o job. Encerramento do serviço cancela jobs ativos e espera por limpeza dentro do limite do projeto. Reinício não reexecuta modelos automaticamente: runs interrompidos ficam em estado terminal de falha com código específico e artefatos íntegros continuam acessíveis.

O serviço L1 é coordenação/persistência e recebe callbacks, não importa implementações de L2–L4 para compilar. Definir em `insights` um `Dependencies` com `Source insightcontract.SourceReader`, `Facts` com a assinatura de `analysisfacts.Analyze`, `Decisions` com a assinatura de `decisionengine.Analyze`, `Presentation` com a assinatura de `presentation.Build`, `Provider insightcontract.ReasoningProvider` e `Store *storage.Store`. `NewService(Dependencies) (*Service,error)` e `(*Service).Shutdown(context.Context) error` fecham a construção/ciclo de vida. L1 testa com funções falsas; Sol injeta as funções reais. Os detalhes de cálculo e julgamento continuam nos donos L2/L3.

### 5.2. Perguntas canônicas

`QuestionCoverage.QuestionID` usa: `history_scope`, `decision_lineage`, `delivery_trace`, `flow_timing`, `quality_outcome`, `cost_actual`, `objective_status`. O config define recursos, período e objetivos; as exigências de cada pergunta ficam em catálogo versionado dentro da L1. `history_scope` exige alcance comprovado para os recursos selecionados; `decision_lineage` exige conteúdo e contexto temporal; `delivery_trace` exige vínculos formais e prova do nível de entrega declarado; `flow_timing` exige histórico suficiente na coorte; `quality_outcome` exige sinais de qualidade definidos e cobertura/maturação pertinentes; `cost_actual` exige moeda e medidas efetivas de custo/alocação; `objective_status` exige meta ou critério explícito e medidas comparáveis. Baseline é necessário quando o julgamento depende de comparação histórica, não para toda métrica com meta absoluta. Uma pergunta pode ser suficiente para uma coorte/caso delimitado e insuficiente para o projeto inteiro; a razão precisa explicitar esse limite.

## 6. Vocabulário normativo

Os valores wire são em inglês conforme a tabela; a interface exibe rótulos em português. Strings desconhecidas recebidas de provedor são erro de validação da resposta, não autorização para inventar estados. Uma fonte desconhecida deve ser mapeada para `unknown`/`unclassified` nos campos que admitem isso e carregar limitação.

| Campo | Valores permitidos |
|---|---|
| `SourceCoverage.Status` | `complete`, `partial`, `not_selected`, `unsupported`, `unknown` |
| `QuestionCoverage.Status`, `ExecutiveReport.SufficiencyStatus` | `sufficient`, `partial`, `insufficient`, `not_applicable` |
| `QueueProgress.Status` | `pending`, `running`, `paused`, `complete`, `failed`, `unknown` |
| `EvidenceView.ContentStatus` | `available`, `metadata_only`, `unavailable`, `redacted_all`, `integrity_error` |
| `EvidenceView.IntegrityStatus` | `verified`, `unverified`, `mismatch`, `missing` |
| `Entity.Kind` | `issue`, `pull_request`, `project`, `component` |
| `Entity.State` | `open`, `todo`, `in_progress`, `started`, `reopened`, `unstarted`, `backlog`, `triage`, `done`, `completed`, `closed`, `cancelled`, `duplicate`, `unknown` |
| `Entity.Outcome` | `active`, `completed`, `cancelled`, `duplicate`, `not_planned`, `unclassified`, `unknown` |
| `Entity.StateTemporalStatus` | `known_at_reference`, `current_only`, `ambiguous`, `unknown` |
| `Link.Status` | `explicit`, `candidate`, `invalid`, `unknown` |
| `Link.TemporalStatus` | `observed_at_reference`, `known_interval`, `current_only`, `ambiguous`, `unknown` |
| `Metric.State` | `observed`, `not_calculable`, `immature`, `unknown` |
| `FactCase.Kind` | `dependency`, `flow`, `scope`, `coverage`, `estimate`, `outcome`, `rework` |
| `FactCase.Classification` | `planned_dependency`, `active_impediment`, `commitment_at_risk`, `shared_blocker`, `cycle`, `unclassified` |
| `ContextScope.Status` | `known`, `partial`, `unknown` |
| `Claim.Kind` | `direction`, `decision`, `assumption`, `constraint`, `outcome` |
| `Claim.Status` | `supported`, `inferred`, `unverified`, `disputed` |
| `Claim.Method`, `DecisionRecord.Method` | `source`, `rule`, `model`, `human` |
| `DecisionRecord.Status` | `proposed`, `accepted`, `implemented`, `superseded`, `revoked`, `conflicting`, `unknown` |
| `ClaimRelation.Kind`, `DecisionRelation.Kind` | `supports`, `supersedes`, `revokes`, `contradicts`, `different_scope`, `unresolved` |
| Estado das duas relações | `established`, `candidate`, `indeterminate` |
| `ValidityStatus` de claim/decisão/relação | `known_interval`, `open_ended`, `unknown`, `ambiguous` |
| `Assessment.State` | `assessed`, `indeterminate`, `not_applicable` |
| `Assessment.Level` em impacto/complexidade/urgência | `critical`, `high`, `medium`, `low`, `unknown` |
| `Assessment.Level` em confiança | `high`, `medium`, `low`, `unknown` |
| `DecisionCase.State` | `actionable`, `monitor`, `needs_evidence`, `resolved` |
| `DimensionStatus.Status` | `on_track`, `at_risk`, `off_track`, `indeterminate`, `not_applicable` |
| `DecisionAnalysis.Status`, `ExecutiveReport.AnalysisStatus` | `complete`, `partial`, `not_run`, `failed` |
| `ExecutiveReport.PublicationStatus` | `ready`, `exporting`, `exported`, `export_failed` |
| `ReasoningRequest.Task` | `extract`, `reconcile`, `assess` |
| `ProviderConfig.Kind` | `none`, `configured` |
| `Alternative.Basis`, `DecisionCase.RecommendationBasis` | `source`, `inference`, `hypothesis` |
| `Objective.Dimension` | `direction`, `delivery`, `flow`, `quality`, `cost`, `capacity` |
| `Objective.Comparator` | `lte`, `gte`, `eq`, `none` |
| `Policy.ContentMode` | `metadata_only`, `local_excerpts`, `export_excerpts` |
| `AnalysisRun.State` | `queued`, `running`, `completed`, `completed_partial`, `failed`, `cancelled` |
| `AnalysisRun.Phase` | `source`, `facts`, `reasoning`, `presentation`, `persisting`, `idle` |
| `EstimateChange.Timing` | `before_start`, `after_start`, `unknown` |
| `DeliveryObservation.Status` | `observed`, `unknown` |

`Scope` define a seleção global; `ContextScope` define aplicabilidade semântica de uma afirmação/decisão por recursos, entidades, componentes, ambiente e condições. Ausência de ambiente ou condição documentada não significa que os escopos sejam iguais. Em `supersedes`/`revokes`, `From...ID` é a decisão/afirmação posterior que substitui/revoga `To...ID`. Uma contradição é representada uma vez, com os dois IDs ordenados para identificação estável.

`Link.Relation` preserva tipos documentados da fonte, incluindo `blocked_by`, `belongs_to_project`, `implements` e `contains_manifest`. Em `blocked_by`, `FromID` é o bloqueado e `ToID` o bloqueador. Uma referência sem correspondência formal permanece `candidate`. Para calcular efeitos, não inverter essa direção nem contar a relação inversa como uma segunda dependência.

## 7. Semântica de ausência, tempo, cobertura e evidência

**Ausência:** ponteiro `nil` serializa `null` e significa desconhecido/sem evidência. Coleções no JSON devem ser arrays, inclusive vazias; inicializar slices para não serializar `null` onde o contrato prevê lista. `0` só representa uma observação ou cálculo válido. Arrays presentes vazios significam nenhum elemento no conjunto observado, nunca prova de completude da fonte. Campos opcionais não passam a conhecidos por default de Go.

**Tempo:** timestamps são RFC3339 em UTC; a interface usa America/Sao_Paulo quando configurado. As coortes de eventos usam `[cfg.RequestedScope.ReportWindow.Start,cfg.RequestedScope.ReportWindow.End)`; `Snapshot.Window` descreve a coleta original e não substitui a janela solicitada. Se a janela pedida extrapolar a cobertura disponível, calcular somente observações elegíveis presentes, declarar a parcela desconhecida e não rotular o total como completo. O estoque aberto é avaliado no instante de referência, com regras de inclusão explícitas para evento exatamente no corte. `ObservedAt` é a coleta, não a data da decisão. `ValidTo=null` só representa fim aberto quando `ValidityStatus=open_ended` e houver início/afirmação que sustente isso. Nos demais casos é desconhecido. Estado atual coletado depois do corte não é automaticamente estado histórico no corte.

**Cobertura:** `HistoryObservedFrom/To` é apenas o envelope das ocorrências observadas. `HistoryCoveredIntervals` só contém intervalos que o coletor comprova ter percorrido com o escopo/semântica necessários; uma união com lacunas não comprova continuidade. Nem uma lista paginada percorrida por inteiro garante reconstrução de estados passados se a API só fornece representação atual. Cobertura por pergunta deve explicitar essa limitação.

**Filas:** `QueueProgress.ID` identifica o mesmo trabalho lógico por fonte, recurso, escopo e família de coleta. `Pending` é a quantidade conhecida de alvos únicos pendentes dessa fila; `QueueIDs` apenas referencia a fila em várias coleções. Não somar cópias dessas referências nem interpretar `observed+pending` como universo da fonte. Checkpoints são opacos, sem credenciais.

**Evidências:** hash original é verificado antes de derivar `Excerpt`; o hash do trecho e versão da sanitização são distintos. `Redacted`/`Truncated` são flags independentes. Um trecho pode estar disponível e parcialmente redigido; se tudo foi removido, usar `redacted_all`. Mismatch não entra no motor semântico. Citação deve apontar para ID fornecido e sua `Quote` deve ser trecho exato do excerto autorizado. Fatos de cobertura e métricas podem ser sustentados por seus `FactCaseIDs`/`MetricIDs` e manifesto fixado, sem fabricar um `EvidenceID` textual para metadados. Isso verifica referência, não prova que uma inferência esteja correta.

**Coortes:** `Cohort.Count` conta seus membros observados; `EligibleCount` só é informado quando conhecido. `Metric.SampleSize`, `EligibleSize`, `Numerator`, `Denominator` e `CohortID` não são intercambiáveis. A RFC-INV-002 determina suas definições. Não somar medianas, pontos de escalas diferentes ou tempos de estado para calcular custo humano.

## 8. Reprodutibilidade, persistência e orçamento

SourceBundle, FactsResult e DecisionAnalysis pertencem ao mesmo snapshot. `InputHash` inclui o hash do dataset, evidências realmente usadas e política de extração; `ConfigHash` inclui seleção, objetivos e políticas sem segredo. Serializar structs em ordem fixa com `encoding/json`, ordenar coleções que representam conjuntos por ID e preservar a ordem de sequências temporais. Não usar maps não normalizados ou representação numérica diferente entre produtores para calcular a mesma chave.

A chave de cache inclui versões de contrato, regras, adapter, modelo, prompt, rubrica, sanitização, configuração, evidências e objetivos. Troca de decisão/metas invalida análise mesmo que o PDF anterior já exista. `CreatedAt` e duração não devem contaminar a identidade do conteúdo de entrada.

Persistir a análise em namespace privado separado e imutável usando os mecanismos de storage do projeto. O coordenador final gera a identidade da análise, valida referências e só então publica o conjunto. Resultados parciais continuam disponíveis com status parcial, sem substituir análises completas de outro escopo.

`Budget` limita requisições, tokens, duração e custo quando preço verificável existir. Sem preço conhecido, `Usage.Cost=null`, nunca zero. Um `MaxCost` obrigatório com preço desconhecido impede execução paga e informa o motivo. Contabilizar chamadas mal sucedidas quando houver uso reportado. Sem suporte do provedor à contagem exata, registrar incerteza e adotar estimativa conservadora para o orçamento, sem publicá-la como custo real.

`ProviderConfig.EndpointProfile` referencia configuração; não contém token. `ExternalProcessingAllowed=false` bloqueia envio externo pelo novo motor. Um modo local não ganha permissão externa por falha. A seleção de Jev depende de validação do adapter e benchmark; não presumir API de chat, compatibilidade OpenAI ou capacidade de gerar texto a partir de endpoints de classificação.

## 9. Fixtures e aceite comum

`fixtures/fixture_go.json` é inteiramente sintética e fornece config, source, facts e análise esperada para fakes. `fixtures/cases.json` define os cenários comportamentais e suas expectativas; não contém dados privados da empresa. `fixtures/pdf_baseline.json` preserva somente os agregados e origens da auditoria, sem fingir ser o dataset original.

Os quatro Lunas devem demonstrar testes de comportamento de suas fronteiras. A integração final deve provar o fluxo real configurado além de fakes; execução com provedor não configurado mostra indisponibilidade sem esconder fatos determinísticos. Um benchmark somente com respostas fixas não comprova qualidade do modelo. Fixtures sintéticas são o primeiro gate; avaliação de casos reais rotulados deve ocorrer no ambiente do usuário antes de usar o julgamento para decisões gerenciais.

Handoff obrigatório: base/head, arquivos, requisitos atendidos/pendentes, comandos e resultados reais, amostras de saída, RFCs de correção/dependência, e orientação para integrar a fronteira. Usar `templates/HANDOFF.md`. Não declarar um teste executado quando apenas foi especificado.

## 10. Critério de encerramento

O pacote só está implementado quando: há fontes legíveis e suficiência por pergunta; fatos e coortes consistentes; reconciliação de decisões com evidência e abstenção; avaliação de impacto/complexidade/alternativas; visão executiva navegável; integração e regressões verificadas; e todas as correções propostas estão em RFCs reaplicáveis. Dimensão indeterminada pode ser um resultado correto, mas a ausência da capacidade de julgamento não pode ser rebatizada como entrega concluída.
