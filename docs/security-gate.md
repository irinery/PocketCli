# PocketCLI Security Gate

O workflow `.github/workflows/security-gate.yml` executa os scanners em `scripts/security/`, gera `security-report.txt` e falha quando qualquer scanner encontra severidade `HIGH` ou `CRITICAL`, ou quando algum scanner retorna erro de execução.

Para validar localmente:

```sh
sh tests/test_security_scanners.sh
```

Pré-requisitos: `bash`, `awk`, `sed`, `find`, `stat`, `chmod`, `mktemp`, `git` e shell POSIX. Variáveis úteis: `REPO_ROOT` para apontar os scanners para outro diretório, `SECURITY_RESULTS_DIR` para o diretório de saídas individuais e `SECURITY_REPORT_FILE` para o relatório consolidado.

Para branch protection da `main`, marque como required status check:

```text
Security Gate / security-gate
```
