# PocketCli Investigation — preview local

Para o passo a passo da interface ao vivo e da coleta em repositórios GitHub, veja o [guia de uso](usage.md).

O preview não usa Node, CDN, GitHub ou Linear; não chama Claude automaticamente. O servidor e o mock usam somente a biblioteca padrão do Go e sempre exibem a marca `DEMO`.

## Executar

Pré-requisitos: Go 1.22 ou superior. O PDF é gerado dentro da aplicação; não é preciso instalar browser nem Poppler.

```sh
go run -buildvcs=false ./cmd/investigate-preview
```

O processo escolhe uma porta livre, imprime o endereço loopback e um código de desbloqueio válido por cinco minutos. Os dados locais ficam no diretório de configuração do sistema em `pocketcli/investigation-preview`; `-data-dir` pode apontar para outro diretório privado.

Flags disponíveis: `-bind` (somente loopback), `-port` (zero escolhe porta livre), `-scenario` (`complete`, `empty`, `partial`, `unauthorized`, `rate_limited`, `offline`, `pdf_unavailable` ou `large_report`) e `-data-dir`.

O mock implementa o contrato `investigate.v1` e as dez consultas de catálogo. O cenário `large_report` contém a fixture adicional de 237 achados para paginação; `complete` usa os dois achados do contrato de UI/SQL. SQL editado fora do catálogo retorna `ERR_DEMO_QUERY_UNSUPPORTED`. A interface de preview não executa coleta real. O YAML conhecido está em `internal/investigation/web/mock/demo.yml`.

## Dependências para PDF

O renderer roda no próprio processo, só com a biblioteca padrão do Go: monta páginas A4 com as fontes Type 1 padrão (WinAnsi) e conta as páginas que gerou. Se o diretório de relatórios estiver inutilizável ou a geração falhar, o snapshot continua acessível e a interface apresenta o PDF como indisponível.

Poppler (`pdfinfo`, `pdftotext`) continua útil só para inspecionar manualmente um PDF gerado; nenhum fluxo do produto ou teste depende dele.

## Persistência do preview

Comentários são gravados em `annotations/annotations.jsonl` com modo privado, revisão por item e revisão global. Consultas ficam em `queries.jsonl`; rascunhos editoriais ficam em `editorial/editorial.jsonl`. PDFs ficam em `reports/` como arquivos versionados; uma nova geração não substitui as revisões anteriores.

Claude pode ser habilitado opcionalmente no preview: informe uma chave Anthropic utilizável pela API, liste os modelos por ação explícita e autorize cada payload depois do preview. A chave fica somente em memória durante o processo. O preview não valida credenciais GitHub/Linear nem faz coleta real. Parsing geral de YAML e execução de SQL livre dependem dos adaptadores reais e não estão ativos no preview. Não informe credenciais reais na fixture de demonstração.
