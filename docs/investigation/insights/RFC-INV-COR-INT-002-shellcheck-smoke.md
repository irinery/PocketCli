# RFC-INV-COR-INT-002 — checagem POSIX de temporário no smoke

| Campo | Valor |
|---|---|
| Estado | implementada aguardando validação remota |
| Origem | PR #71, workflow Lint, job ShellCheck, run 40760199797 |
| Base/head | `origin/main` em `b068cfd`; integração `2fb710f` |
| Responsável | Sol, correção local delimitada de gate |

## Problema e reprodução

O teste `tests/smoke/test_pocket_investigate_build.sh` verifica sobras de arquivo `.build.` com `ls | grep`. O ShellCheck remoto sinaliza SC2010 na linha 65 e reprova o PR. O arquivo já estava em `main`; o teste funcional passa, mas o gate Lint inclui todos os scripts.

```sh
shellcheck --severity=warning --shell=bash --exclude=SC1091 tests/smoke/test_pocket_investigate_build.sh
```

## Correção e aceite

Trocar a leitura de `ls` por um loop POSIX sobre o glob `*.build.*`, com guarda `-e` para o caso sem correspondência. Isso preserva a checagem de sobra e aceita nomes com espaços. Sem alterar o fluxo de build ou instalação.

Executar o ShellCheck do arquivo, `sh tests/smoke/test_pocket_investigate_build.sh`, `POCKETCLI_GO_BINARY=/tmp/pocket-inv-rfc sh tests/run_smoke.sh` e `POCKETCLI_SHELL_SUITE_BUDGET=90 sh scripts/run_local_ci.sh` depois da mudança. Pré-requisitos: `sh`, ShellCheck, Go 1.22+, compilador C para o build do smoke; sem token ou variáveis de ambiente além das mostradas. Registrar o resultado e só então marcar esta RFC validada.

## Validação executada

- `shellcheck --severity=warning --shell=bash --exclude=SC1091 tests/smoke/test_pocket_investigate_build.sh`: passou, sem saída.
- `sh tests/smoke/test_pocket_investigate_build.sh`: passou.
- `POCKETCLI_SHELL_SUITE_BUDGET=90 sh scripts/run_local_ci.sh`: passou, shell 33/33, testes Go, build e smoke 7/7. Esse fluxo cobre também o smoke geral; não foi necessário rodá-lo em duplicata.
- `git diff --check`: conferir antes do commit. O Lint remoto do novo commit permanece gate obrigatório do PR.
