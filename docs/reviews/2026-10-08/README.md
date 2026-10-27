# Validacao apos merge do PR 60

Data: 2026-10-08. Revisao do commit `e23c46dfb1175e190b24ae14b0af8813edaf4002`, HEAD remoto de `main` nesta verificacao.

O [PR 60](https://github.com/irinery/PocketCli/pull/60) ja havia sido mergeado por `irinery` em 2026-07-10. Nenhum novo merge foi necessario. A tag `v1.3.4` aponta para o mesmo commit. CI (Linux, macOS e Alpine), Layout Gate, Lint, Security Gate e Tag release passaram nesse SHA no GitHub.

O checkout original permaneceu em `codex/security-usability-integration-review`, SHA `f1f2141`, porque comandos de status e leitura demoraram a responder. Nenhuma alteracao local foi descartada ou branch trocada. A validacao foi feita em clone limpo de `main` em `/tmp/pocketcli-main-validation.3y6eGs/repo`.

## Validacao executada agora

Host: macOS 26.6.2 ARM64, Go 1.26.1. Testes e fixtures existentes foram preservados.

- `POCKETCLI_CI_PROFILE=macos POCKETCLI_GO_BINARY=/tmp/pocketcli-main-validation.3y6eGs/pocket-go sh scripts/run_local_ci.sh`: passou; 33 regressoes shell, todos os pacotes Go, build e 5 smoke tests.
- `go vet ./...`: passou.
- ShellCheck nivel warning sobre `pocket`, bootstrap, instalador, scripts, lib e testes: passou.
- Suite visual junto de compilacao/vet: T-13 falhou; demais casos passaram.
- Suite visual isolada: todos os sete casos passaram. Isso nao estabelece causa unica ou estabilidade sob carga.
- `fleet.sh list` sem binario Go: exit 64, comportamento fail-closed esperado, mas evidencia lacuna no empacotamento do instalador.
- Clone validado continuou sem diff de codigo ao fim das verificacoes.

Logs locais: `/tmp/pocketcli-main-validation.3y6eGs/local-ci.log`, `visual.log` e `visual-isolated.log`. Sao artefatos temporarios; as conclusoes relevantes estao neste documento.

## Proximos trabalhos

| ID | Nivel 0-5 | Estado | Escopo |
|---|---:|---|---|
| [SEC-08](SEC-08-legacy-remote-entrypoints.md) | 4 | aberto | Unificar os entrypoints remotos legados |
| [SEC-09](SEC-09-tailscale-runtime-files.md) | 4 | aberto | Proteger logs/PID e limitar auth Tailscale |
| [SEC-07 complemento](SEC-07-external-installers.md) | 4 | aberto | Autenticidade dos instaladores externos |
| [INT-01](INT-01-install-go-runtime.md) | 3 | aberto | Entregar o binario Go no caminho feliz |
| [CI-02](CI-02-release-publication.md) | 3 | aberto | Publicar release depois da tag automatica |
| [UX-03](UX-03-visual-load.md) | 3 | investigacao | Navegacao visual sob carga |
| [UX-04](UX-04-macos-page-size.md) | 2 | aberto | Corrigir memoria no macOS ARM64 |
| [PERF-01](PERF-01-product-budgets.md) | 2 | proposto | Transformar metas em budgets de produto |

Ordem recomendada: SEC-08/SEC-09; INT-01 junto de CI-02; cadeia de autenticidade; UX-03/UX-04 e budgets. O escopo desta etapa foi validar e propor complementos, portanto nenhum desses novos achados foi corrigido aqui. Niveis 4 precisam de PR dedicado e validacao dos contratos de transporte/instalacao.

CI verde demonstra cobertura aprovada, nao ausencia de falhas de seguranca. Esta revisao dirigida nao constitui uma auditoria exaustiva de todos os caminhos do projeto.
