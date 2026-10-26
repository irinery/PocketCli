# PocketCli Investigation — núcleo

Estado em 26/10/2026: implementação parcial do RFC 01. O contrato `investigate.v1` e a projeção C.8 estão congelados. O comando já prepara planos, coleta metadados selecionados, preserva versões observadas, publica snapshots e oferece SQL local protegido. As lacunas abaixo aparecem como `coverage=unsupported|partial`, sem conclusão negativa inventada.

Changelog do contrato compartilhado: nenhuma alteração em relação ao apêndice C do RFC 01.

## Build e testes locais

Pré-requisitos: Go 1.22 ou posterior, `gofmt`, compilador C para CGO, `git`, `sh`; para o PDF automático, Chromium/Google Chrome e `pdfinfo` (Poppler). Não são baixados durante a investigação. No macOS, se o navegador não estiver no `PATH`, configure `POCKETCLI_CHROMIUM` com o caminho absoluto do executável.

```sh
gofmt -w cmd/pocket/investigate_command.go cmd/pocket/investigate_services.go internal/investigation/core internal/investigation/collect internal/investigation/gitmirror internal/investigation/storage internal/investigation/query internal/investigation/rules internal/investigation/secrets
go test -buildvcs=false ./...
CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite ./...
CGO_ENABLED=1 go build -buildvcs=false -tags investigate_sqlite -o /tmp/pocket-go ./cmd/pocket
POCKETCLI_GO_BINARY=/tmp/pocket-go sh tests/run_smoke.sh
POCKETCLI_SHELL_SUITE_BUDGET=90 sh scripts/run_local_ci.sh
POCKETCLI_CHROMIUM=/caminho/para/chromium CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite -run TestRealPDFWithFixture -v ./internal/investigation/report
POCKETCLI_CHROMIUM=/caminho/para/chromium CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite -run TestOfflineRuntimeToPDF -v ./tests/investigation/core
```

Os testes comuns de `internal/investigation/` usam APIs falsas e diretórios temporários. Não precisam de tokens, rede, Chromium nem `pdfinfo`; o teste de PDF real é opt-in via `POCKETCLI_CHROMIUM` e requer `pdfinfo`. A fixture SQL é `tests/investigation/core/demo.sql`. O build padrão sem tag mantém o Viewer e responde explicitamente que o motor completo não está disponível.

`-buildvcs=false` evita que o toolchain tente ler objetos Git ainda não hidratados pelo iCloud. O orçamento da suite shell é configurável por `POCKETCLI_SHELL_SUITE_BUDGET`; 90s é adequado para validação local neste Mac. O padrão do CI do projeto permanece inalterado.

## Uso atual

Use `POCKETCLI_INVESTIGATE_GITHUB_TOKEN` e `POCKETCLI_INVESTIGATE_LINEAR_KEY` somente quando a fonte correspondente estiver habilitada. No terminal interativo, a chave ausente é lida sem eco. Chaves nunca entram em flags ou YAML. O plano pode ser inspecionado antes de iniciar:

```sh
pocket investigate --scope ./auditoria.yaml --prepare --json
pocket investigate start PLAN_ID --confirm HASH
pocket investigate refresh INVESTIGATION_ID --prepare --json
pocket investigate serve INVESTIGATION_ID --snapshot SNAPSHOT_ID
pocket investigate report INVESTIGATION_ID --snapshot SNAPSHOT_ID
```

Para incluir um snapshot operacional, prepare primeiro um plano com `modules.runtime_import: true` e anote `investigation_id`. Importe o JSON local e prepare outro plano para a mesma investigação; o novo hash fixa exatamente a versão importada:

```sh
pocket investigate import-runtime INVESTIGATION_ID ./runtime.json
pocket investigate --scope ./auditoria.yaml --investigation INVESTIGATION_ID --prepare --json
```

Formato aceito (arquivo até 2 MiB, JSON UTF-8, sem campos extras ou chaves duplicadas):

```json
{
  "schema_version": "pocketcli.runtime.v1",
  "observed_at": "2026-01-08T00:00:00Z",
  "environment": "production",
  "components": [
    {
      "name": "api",
      "repository": "acme/api",
      "path": "services/api/go.mod",
      "enabled": true,
      "revision_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  ]
}
```

`repository` e `path` são opcionais em conjunto; quando coincidem exatamente com um componente descoberto no Git, a observação é vinculada a ele. `digest` opcional aceita `sha256:` seguido de 64 caracteres hexadecimais. `enabled: false` registra desativação na data observada. O import não coleta hosts nem reconstrói estados anteriores.

Sem arquivo de escopo, use `pocket investigate --period 90d`; o padrão é descobrir recursos autorizados e exigir a confirmação do hash. Em modo sem TTY, omitir período ou confirmação retorna erro explícito. Para desabilitar uma fonte, use `--no-github` ou `--no-linear`. `--repo` e `--team` são repetíveis e limitam apenas a fonte correspondente; a outra fonte habilitada continua descoberta e exige credencial. O plano exibe avisos para módulos solicitados que ainda não têm coletor. A identidade das credenciais entra no hash do plano; mudar de conta exige preparar e confirmar outro plano, sem gravar o token. O armazenamento fica em `~/.local/share/pocketcli/investigations` com diretórios 0700 e arquivos 0600. O servidor escuta somente em loopback e exibe o código local de desbloqueio no stderr.

## Cobertura implementada e limites

O coletor faz descoberta limitada de repositórios GitHub e teams Linear; lê PRs, issues, comentários de issues/PRs, comentários de revisão, revisões formais e timeline GitHub, além de issues Linear inclusive arquivadas, com paginação. No Linear, introspecta `Issue` antes de consultar `history` e `comments` por issue; coleta transições de estado, estimativas e comentários. Até 100 issues por coleção são revalidadas em cada rodada, com cursor circular; campos ausentes no schema recebem `unsupported`. O inventário de projetos do team pagina inclusive arquivados, e associações explícitas de issue→projeto geram vínculos diretos. Revisões e timeline GitHub seguem o mesmo teto de 100 itens por coleção e rodada; o restante aparece como cobertura parcial. Os eventos observados de `ready_for_review` e `reopened` alimentam as regras de espera por revisão e reabertura. Um PR criado pronto sem evento `ready_for_review` não recebe horário ready inferido. A observação bruta fica em versões privadas por hash; títulos publicados recebem redação dos tokens da sessão. Páginas e cursores são confirmados em diário privado antes da próxima request; após interrupção, `resume` retoma do cursor confirmado. `SourceCursor` privado registra query/credencial, intervalos cobertos, watermark e checkpoint para PRs/issues/comentários GitHub e issues Linear. O watermark só avança em varredura confirmada; expansão de janela sem cobertura volta ao inventário completo. O refresh dos dois tipos de comentário GitHub usa `since` por atualização com sobreposição de 48 horas; issues Linear usam `updatedAt` com a mesma sobreposição depois da primeira cobertura completa. Versões editadas ficam preservadas separadamente. Respostas ETag/304 reaproveitam representação em cache de memória por credencial e URL durante o processo; sem cache, o cliente faz nova requisição normal. Referências explícitas de issue ou comentário para PR coletado geram vínculos diretos; texto só parecido não gera vínculo. Refresh reaproveita versões idênticas apenas com a mesma credencial, recursos e módulos; uma janela histórica não recebe o estado atual de uma issue sem observação naquela janela. A paginação de PRs/issues GitHub pode parar na janela de reconciliação de 48 horas quando a coleta anterior foi completa e a janela não foi ampliada. Como essas listas não fornecem snapshot estável, qualquer varredura com mais de uma página fica `partial`; PRs/issues em delta também ficam `partial`, evitando declarar cobertura completa se itens mudarem entre páginas. Snapshots publicados não são alterados. O SQL consulta apenas a projeção SQLite C.8 por `snapshot_id`, com conexão somente leitura, authorizer, funções permitidas, limite de tempo/linhas/bytes e parâmetros bind.

Quando `code_history` está ativo, o Agent consulta a HEAD padrão e mantém um espelho Git privado de commits sem trees/blobs (`--filter=tree:0`). O fetch usa URL sem credencial, configuração temporária isolada, hooks e helpers desabilitados, sem submódulos/LFS/GC automático. Versões antigas observadas continuam referenciadas localmente. `merge-base --is-ancestor` distingue avanço de divergência quando o grafo está completo; clone raso ou ancestral ausente fica `unknown`. A medição de bytes usa o crescimento/peak do espelho privado como aproximação conservadora da transferência, com cancelamento ao cruzar o limite. A observação de ref fica em journal por plano para que `resume` não perca uma divergência já detectada. Só a ref padrão é observada por enquanto. A árvore dessa versão é consultada via REST e identifica caminhos de `go.mod`, `package.json`, Dockerfile, Compose e OpenAPI. Blobs desses manifestos até 2 MiB são verificados pelo OID Git, guardados em cache privado e analisados sem execução. Compose gera componentes de service; árvores truncadas, blobs inacessíveis e parsers inválidos ficam parciais. Os arquivos alterados de PRs selecionados são paginados e ligados diretamente ao PR, em até 100 PRs por rodada; renames são eventos observados, sem inferir continuidade causal. O mapeamento arquivo→componente usa a árvore do próprio head do PR, obtida pelo commit fixado e guardada em cache por credencial. Até 20 heads distintos da ref atual são processados por rodada com cursor circular. O vínculo só é direto quando existe um único manifesto de diretório mais próximo naquela árvore; head inacessível, árvore truncada, manifesto ambíguo e fila pendente ficam parciais. Vínculos de arquivos de heads antigos ficam preservados como evidência após force-push, mas não são remapeados para os manifestos do head novo. `code_history` continua parcial porque commits fora dos heads selecionados e a história completa dos arquivos não são inventariados. Git HTTPS, árvore e blobs requerem permissão `Contents read` no token selecionado.

CI opcional coleta workflow runs do GitHub Actions e check runs nos heads dos PRs selecionados, com paginação e journal. Registra status/conclusão como eventos sem tratar sucesso de CI como implantação. Check runs exigem permissão `Checks: read` em tokens granulares; a cobertura é parcial quando algum head ou permissão não está disponível. A API de check runs por ref pode limitar resultados às 1000 check suites mais recentes, e o escopo não representa todo o histórico de CI. Deployments opcionais coletam registros e status; sucesso produz artefato com proveniência `declared` no nível do repositório. O estado efetivo do servidor e a associação com um componente de negócio continuam desconhecidos. Histórico de status anterior à retenção de 90 dias da API recebe cobertura parcial.

Ainda faltam mapa temporal completo de arquivos e recuperação integral após crash. A chave de idempotência é persistida antes do run; se o processo cair entre as duas gravações, repetir `StartRun` com a mesma chave recria o run com o ID reservado, sem duplicar execução. A recuperação reinicia runs interrompidos durante coleta/build como `paused`, reaproveita os diários GitHub, Linear e de observação Git e marca como `report_failed` a interrupção entre publicação do snapshot e fim do PDF, preservando o snapshot para nova exportação. Evidência JSON é normalizada antes do hash; objeto local corrompido interrompe a coleta com erro de auditoria, sem sobrescrita silenciosa. A coleta das áreas ausentes permanece `unsupported`, `pending` ou `partial`. O mecanismo de regras cobre os oito IDs do RFC somente quando recebe os eventos e vínculos necessários. O espelho Git foi testado contra repositório local fictício; não foi feita validação contra contas reais GitHub/Linear.

O `Finalizer` e o servidor da apresentação são injetados na composição CLI. A composição guarda uma chave por snapshot, anotações, redações aceitas e histórico de consultas para evitar gerar o mesmo PDF duas vezes. Se Chromium ou `pdfinfo` estiverem indisponíveis, o snapshot continua consultável e o run fica em `report_failed`; esse estado não satisfaz o DoD de PDF. Os testes opt-in geraram PDFs reais das fixtures offline com Chromium headless e Poppler, inclusive 237 achados paginados sem corte. Outro teste executa importação local → confirmação do plano → run → snapshot → SQL → PDF, sem chamadas externas. Uma execução com fontes reais e a matriz completa S09 ainda precisam de validação autorizada.
