#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
POCKET_GO_BINARY="${POCKETCLI_GO_BINARY:-/tmp/pocket-go}"

if ! command -v go >/dev/null 2>&1; then
    printf '[PocketCli] Go não encontrado no PATH.\n' >&2
    printf '[PocketCli] Instale o toolchain Go (com gofmt incluído) antes de rodar a validação local.\n' >&2
    exit 1
fi

if ! command -v gofmt >/dev/null 2>&1; then
    printf '[PocketCli] gofmt não encontrado no PATH.\n' >&2
    printf '[PocketCli] Reinstale ou ajuste o toolchain Go antes de rodar a validação local.\n' >&2
    exit 1
fi

export GOCACHE="${GOCACHE:-/tmp/pocketcli-go-build-cache}"

printf '[PocketCli] Usando GOCACHE=%s\n' "${GOCACHE}"
printf '[PocketCli] Formatando arquivos Go...\n'
if find "${REPO_ROOT}/cmd" "${REPO_ROOT}/internal" -type f -name '*.go' | grep -q '.'; then
    find "${REPO_ROOT}/cmd" "${REPO_ROOT}/internal" -type f -name '*.go' -print0 | xargs -0 gofmt -w
fi

printf '[PocketCli] Rodando shell regression suite...\n'
(cd "${REPO_ROOT}" && sh tests/run_all.sh)

printf '[PocketCli] Rodando go test ./...\n'
(cd "${REPO_ROOT}" && go test ./...)

printf '[PocketCli] Gerando binário Go...\n'
(cd "${REPO_ROOT}" && go build -buildvcs=false -o "${POCKET_GO_BINARY}" ./cmd/pocket)

printf '[PocketCli] Rodando smoke tests do binário Go...\n'
(cd "${REPO_ROOT}" && POCKETCLI_GO_BINARY="${POCKET_GO_BINARY}" sh tests/run_smoke.sh)

printf '[PocketCli] Validação local concluída com sucesso.\n'
