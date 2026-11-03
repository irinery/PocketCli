# Plano de correções do relatório determinístico

Origem: snapshot `snap-exemplo` (Acme, 2026-11-03), comparado
com a API do GitHub e do Linear em 2026-11-02. As contagens do Linear bateram 100%
(1637 issues, 1115 concluídas, 62 canceladas, 65 duplicadas, 395 abertas). A parte
GitHub, a janela temporal e a legibilidade das métricas não.

Cada item lista: sintoma observado → causa no código → correção → teste.

## Fase 1 — Escopo e atribuição (críticos)

**Status (2026-11-02): implementada**, exceto a consulta `/orgs/{org}` do item 1.1.3,
adiada. Ela exigiria chamadas fora do orçamento de descoberta do plano; a detecção via
anexos do Linear cobre o caso da Acme. A inclusão opcional dos commits próprios de
um fork (1.2) também ficou para depois. O que mudou:
- `core/links.go`: anexos de PR em repos não coletados viram `references_pr` com
  status `out_of_scope` e geram coberturas `referenced_repository` (por repo) e
  `pull_references` (resolvidos/total). O pacote de insights ignora esses vínculos.
- `contract/references.go`: `UncollectedRepositories`, `PullReferenceGap` (limite de
  20%), `ForkReason`, `Forks`.
- `core/core.go`/`core/collect.go`: `fork`/`parent` na descoberta; um fork não roda a
  coleta de histórico Git (`code_history not_selected`).
- `report/render.go`: o resumo e a seção 2 mostram "Cobertura Git insuficiente", os
  repos citados e não coletados e os forks ignorados.

### 1.1 Repositórios privados invisíveis aparecem como cobertura válida
- **Sintoma:** só 4 repos públicos legados analisados; anexos do Linear apontam para
  ≥10 repos privados (`repo-a`, `repo-b`, `repo-c`…) que não entraram.
  `empty-repo` aparece como "coleta completa".
- **Causa:** `core/core.go:262` descobre repos via `/user/repos?affiliation=…`. Se o
  token não é membro da org, a lista só traz o que é público e nada sinaliza a lacuna.
- **Correção:**
  1. Na fase de plano, extrair os repos citados em URLs de PR dos anexos Linear
     (`pullURL` em `core/links.go`) e compará-los com `plan.Resources`.
  2. Para cada repo citado e não coletado, registrar cobertura
     `github <repo> repository forbidden` com contagem de referências.
  3. Consultar `/orgs/{org}`: `total_private_repos == null` → aviso "token sem
     associação à org; repositórios privados não visíveis".
  4. Cobertura global vira **insuficiente** (não "parcial") quando mais de 20% das
     referências a PR apontam para repos não coletados; o resumo diz isso na 1ª linha.
- **Teste:** fixture com anexo para `org/privado/pull/1` fora do escopo → cobertura
  `forbidden` + aviso no resumo.

### 1.2 Código de fork atribuído à org
- **Sintoma:** 578 de 589 componentes e 579 vínculos `modifies_file` vêm do fork
  `vendor-sdk` (62 commits, todos da Meta).
- **Causa:** a descoberta (`core/core.go:267`, `discoverRepository`) não lê `fork` nem
  `parent`; `manifests.go`, `git_commit_files.go` e `pr_head_components.go` marcam tudo
  como `ImplementationStatus: "identified"`.
- **Correção:** adicionar `Fork` e `Parent` a `contract.Resource`. Por padrão, um fork
  fica fora da coleta Git, com a linha "fork de X — ignorado" no relatório. Com opt-in,
  só entram commits posteriores à criação do fork e de autores diferentes do upstream;
  o resto fica com status `upstream`.
- **Teste:** fixture de fork sem commits próprios → 0 componentes e o recurso listado
  como fork.

### 1.3 Anexos Linear→PR descartados em silêncio
- **Sintoma:** 261 anexos coletados e nenhum vínculo issue→PR.
- **Causa:** `core/links.go:57` faz `continue` quando o PR não está no dataset.
- **Correção:** emitir `references_pr` com `Status: "out_of_scope"` e o alvo como chave
  externa (`owner/repo#n`). Adicionar a métrica "Issues com PR fora do escopo". Isso
  também alimenta o 1.1.
- **Teste:** anexo para PR não coletado → vínculo `out_of_scope` e métrica = 1.

## Fase 2 — Métricas confiáveis (altos)

**Status (2026-11-02): implementada** (itens 2.1 a 2.4). Pendências registradas no fim
desta fase. O que mudou:
- 2.1 `core/core.go`: cada team usa o próprio `createdAt` e o `retiredAt` vira
  `LastChangeAt`; a data da organização só aparece como aviso do plano.
  `contract.ObservedResources` tira da janela os recursos sem nenhum registro;
  `Snapshot.ActivityStart/ActivityEnd` (`core/publication.go`) dão a janela de atividade,
  que o PDF mostra ao lado da janela de recursos. O texto de "Tempo de vida dos recursos"
  e o resumo do plano na CLI listam cada team (aposentado com a data de aposentadoria).
- 2.2 `core/run.go` define o status: com fila de reconciliação pendente
  (`contract.PendingQueues`), o snapshot sai `provisional`; `--publish-partial MOTIVO`
  (`RunRequest.PublishPartial`, preservado no `Run`) publica e grava
  `Snapshot.PublishOverride` (motivo, horário, filas), exibido na proveniência do PDF.
  As métricas de etapa e as conclusões por ator do Linear ganham `Metric.Base`
  ("base: X de Y issues do team") e o rótulo **baixa cobertura** abaixo de 50%.
- 2.3 A coleta lê `displayName` (assignee, actor do histórico, autor de comentário) e o
  `login` do GitHub (autor de PR, revisor, ator da timeline) e guarda em
  `Snapshot.Actors`; `rules/actors.go` atribui "Pessoa A, B…" pela ordem da primeira
  atividade. Nomes reais só com `profile/investigation-report.json`
  (`{"version":"1","actor_names":"real"}`), lido em `cmd/pocket` e passado ao
  renderizador; o cache de PDF considera a preferência.
- 2.4 A query de issues lê `canceledAt` e `cycle { id number startsAt endsAt completedAt }`;
  `rules/evolution.go` gera séries por mês × team (criadas/concluídas/canceladas), por
  ciclo (escopo final = issues observadas no ciclo, concluídas) e por projeto (pelo nome).
  A seção "Evolução" do PDF fica antes dos registros, e os vínculos mostram o nome do
  projeto.

Pendências da Fase 2:
- A seção "Evolução" não tem número: as seções 3 e 10–14 seguem a numeração do HTML vivo,
  que não foi alterado (a árvore principal tem mudanças locais em `web/`). Levar a série
  temporal ao HTML fica para depois.
- Os ciclos vêm das issues observadas; um ciclo sem issues não aparece, e o escopo é contado
  em issues (o `scopeHistory` do Linear, em pontos, não é lido).
- A fila de histórico do Linear continua limitada a 100 issues por rodada e reescolhe as
  issues pelo cursor, mesmo as que já têm etapas retidas de rodadas anteriores; por isso o
  ENG pode seguir `provisional` após vários `refresh`. Pular as issues já cobertas é a
  próxima correção.

### 2.1 Janela e tempo de vida
- **Sintoma:** janela de 2023-03 a 2026-11 para 3,5 meses de atividade; todos os
  times com a data da organização; o time QA, aposentado em 08-20, aparece ativo.
- **Causa:** `core/core.go:360` troca a data do team pela da org quando não há escopo de
  team. A query de teams não lê `retiredAt`. `contract.EffectiveWindow` usa recursos
  sem atividade (repos vazios/legados) como limite inferior.
- **Correção:** manter `team.createdAt` por linha e usar a data da org só como nota.
  Ler `retiredAt` como `LastChangeAt`. Em `EffectiveWindow`, ignorar recursos sem
  nenhum registro observado e mostrar a "janela de atividade" (1º e último evento)
  ao lado da janela de recursos.
- **Teste:** org mais antiga que o team, team aposentado, repo vazio de 2023 → janela
  de atividade começa no 1º evento.

### 2.2 Métricas de etapa sobre amostra enviesada
- **Sintoma:** o histórico do ENG tinha 871 de 971 issues pendentes. As permanências
  por etapa do ENG saem de cerca de 100 issues, enquanto CAD/PRD saem de quase todas.
- **Causa:** `core/linear_children.go:120` limita a 100 issues por team por rodada
  (ordem por UUID), e o relatório foi publicado com a fila pendente.
- **Correção:** (a) não permitir status `published` com fila de reconciliação pendente
  sem override explícito do operador, registrado na proveniência; (b) cada métrica de
  etapa/ator mostra "base: X de Y issues do team" e recebe o rótulo **baixa
  cobertura** abaixo de 50%.
- **Teste:** team com 300 issues e 100 com histórico → métrica marcada como baixa
  cobertura; publicação bloqueada sem override.

### 2.3 Atores ilegíveis
- **Sintoma:** 14 atores como UUID, com "tipo desconhecido".
- **Causa:** `rules/metrics_detail.go:141` usa o ID cru como rótulo.
- **Correção:** resolver `user.id → displayName` (Linear `users`, GitHub `login`) e
  guardar o mapeamento no dataset. A exibição de nomes é uma preferência em
  `profile/` (padrão: pseudônimos estáveis "Pessoa A, B…"), seguindo o AGENTS.md.
- **Teste:** com e sem nomes no perfil; o rótulo nunca vira score de produtividade.

### 2.4 Falta de série temporal
- **Sintoma:** nenhuma visão por mês, ciclo ou projeto; projetos como `proj-<hash>`.
- **Correção:** métricas `created/completed/canceled` por mês e por team; coleta de
  `cycles` (número, início, fim, escopo final); nome dos projetos nos vínculos. Nova
  seção "Evolução" no PDF, antes dos registros.
- **Teste:** dataset de 2 meses × 2 teams → tabela com 4 células corretas.

## Fase 3 — Redução de ruído (médios)

**Status (2026-11-03): implementada** em `rules/noise.go` (`rules.Version` 1.18.0):
- 3.1: `R-BLOCK-001` (`medium`, "Item em andamento bloqueado") só quando o item
  bloqueado está `started`/`in_progress`/`open`; entre itens planejados vira
  `R-BLOCK-002` (`info`, "Dependência planejada observada"). Agora há um achado por
  item bloqueado, e não um por relação.
- 3.2: métrica "Pontos entregues no team X" (soma por team, sem somar entre teams) e
  achado `R-BATCH-001` (`info`) para ≥20 issues de um projeto criadas em 24h.
- 3.3: achado `R-MOVE-001` (`info`), um por team, para issues criadas antes do team, e
  métrica de números de identificador ausentes (só no período `all`).
- 3.4: métrica de duplicadas por team e retrabalho `stage_rejection` para entradas em
  etapas de reprovação (Reprovado, Rejeitado, Devolvido…).

| Item | Correção | Arquivo |
|---|---|---|
| 3.1 R-BLOCK infla severidade | `medium` só quando o bloqueado está `started`; backlog↔backlog vira `info` "dependência planejada". Agrupar por cadeia (ITEM-24 aparecia 7×). | `rules/rules.go:187`, `issueActiveObserved` |
| 3.2 Entrega sem peso | Somar pontos de estimativa ao lado da contagem. Achado `info` para lote (≥20 issues do mesmo projeto em 24h). | `rules/rules.go:488` |
| 3.3 Movimentação entre teams | Achado quando `issue.createdAt < team.createdAt` ou há lacunas de numeração (ENG: 189). | `rules/` + query de teams |
| 3.4 Retrabalho estreito | Incluir taxa de duplicadas e transições para etapas de reprovação como tipos de retrabalho. | `rules/rules.go` |

## Fase 4 — Forma

- Durações em horas/dias (hoje em segundos) no `report/render.go`.
- Evidências (67% das páginas, só hashes) em anexo separado (JSON/HTML vivo); o PDF
  fica só com a contagem e o hash do conjunto.
- Resumo com 5 números e a cobertura **por métrica**, em vez de "parcial" genérico.

## Ordem sugerida e validação

1. Fase 1 inteira (1.3 → 1.1 → 1.2): 1.3 é pré-requisito do 1.1.
2. 2.1 e 2.2 (correção de números), depois 2.4 e 2.3.
3. Fases 3 e 4.

Para validar, gerar um novo snapshot da Acme e conferir contra os agregados do
Linear (contagens por team/mês/status) e contra o aviso de repos privados. Para o
GitHub completo, é preciso um token de membro da org; isso não é código.

Para cada PR, rodar o fluxo local de `gofmt`, `go test ./internal/investigation/...`,
build e smoke tests, conforme o AGENTS.md.
