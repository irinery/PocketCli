# CI-01 - Toolchain Go no macOS 26

Criticidade: 3 (corrigido)

O job `macos-latest` passou a executar em macOS 26 ARM64, mas recebia Go 1.22.12 pelo `go-version-file`. Esse linker gerava alguns binarios de teste sem `LC_UUID`; o `dyld` abortava os processos antes de qualquer teste rodar. Os pacotes afetados foram `cmd/pocket`, `internal/catalogue` e `internal/connect`.

A matriz de CI agora declara o toolchain por plataforma: Linux continua em Go 1.22.12, preservando a verificacao da versao minima declarada no projeto, e macOS usa Go 1.26.x, compativel com o runtime do macOS 26. Nenhum teste foi modificado.

Validacao local: em macOS 26 com Go 1.26.1, `go test -count=1 ./cmd/pocket ./internal/catalogue ./internal/connect` e `go test ./...` passaram. A confirmacao final depende da nova execucao do GitHub Actions no runner macOS.
