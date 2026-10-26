# PocketCli Investigation — preview local

O preview não usa Node, CDN, GitHub ou Linear; não chama Claude automaticamente. O servidor e o mock usam somente a biblioteca padrão do Go e sempre exibem a marca `DEMO`.

## Executar

Pré-requisitos: Go 1.22 ou superior. Para PDF, instale Chromium/Google Chrome e Poppler (`pdfinfo`) pelo gerenciador de pacotes da máquina. O PocketCli não baixa nem instala browser automaticamente.

```sh
go run -buildvcs=false ./cmd/investigate-preview
```

O processo escolhe uma porta livre, imprime o endereço loopback e um código de desbloqueio válido por cinco minutos. Os dados locais ficam no diretório de configuração do sistema em `pocketcli/investigation-preview`; `-data-dir` pode apontar para outro diretório privado.

Flags disponíveis: `-bind` (somente loopback), `-port` (zero escolhe porta livre), `-scenario` (`complete`, `empty`, `partial`, `unauthorized`, `rate_limited`, `offline`, `pdf_unavailable` ou `large_report`) e `-data-dir`.

O mock implementa o contrato `investigate.v1` e as dez consultas de catálogo. O cenário `large_report` contém a fixture adicional de 237 achados para paginação; `complete` usa os dois achados do contrato de UI/SQL. SQL editado fora do catálogo retorna `ERR_DEMO_QUERY_UNSUPPORTED`. A interface de preview não executa coleta real. O YAML conhecido está em `internal/investigation/web/mock/demo.yml`.

## Dependências para PDF

O renderer usa Chromium headless instalado localmente, com sandbox habilitado, perfil temporário privado e sem dependências externas no HTML. `pdfinfo` valida a existência de páginas antes de publicar o arquivo. Se qualquer binário estiver ausente ou o renderer falhar, o snapshot continua acessível e a interface apresenta o PDF como indisponível. Não existe fallback que omita a sandbox nem instalação em runtime.

Poppler também pode ser usado na validação manual de PDFs com `pdfinfo` e `pdftotext`. O preview não precisa de Poppler para navegar no HTML, executar queries catalogadas ou registrar comentários.

## Persistência do preview

Comentários são gravados em `annotations/annotations.jsonl` com modo privado, revisão por item e revisão global. Consultas ficam em `queries.jsonl`; rascunhos editoriais ficam em `editorial/editorial.jsonl`. PDFs ficam em `reports/` como arquivos versionados; uma nova geração não substitui as revisões anteriores.

Claude pode ser habilitado opcionalmente no preview: informe uma chave Anthropic utilizável pela API, liste os modelos por ação explícita e autorize cada payload depois do preview. A chave fica somente em memória durante o processo. O preview não valida credenciais GitHub/Linear nem faz coleta real. Parsing geral de YAML e execução de SQL livre dependem dos adaptadores reais e não estão ativos no preview. Não informe credenciais reais na fixture de demonstração.
