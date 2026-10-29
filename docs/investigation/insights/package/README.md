# Pacote de implementação `investigate.insights.v1`

Bootstrap comum preparado para a série RFC-INV-000/001 em 2026-10-28.

- Base inspecionada e escolhida: `b068cfdd40ea72f6c88df1c4ab69d30ab2905b67`.
- Branch deste bootstrap: `codex/rfc-inv-base`.
- Contrato congelado copiado de `contracts/insight_v1.go` para `internal/investigation/insightcontract/v1.go`.
- Fixtures comuns copiadas para `internal/investigation/insightcontract/testdata/`.
- RFCs e prompt LUNA-1 preservados nesta pasta.
- A cópia do contrato adiciona tipos/interfaces e não ativa funcionalidade do produto.

Validação do bootstrap:

- `gofmt -w internal/investigation/insightcontract/v1.go` — executado.
- `env GOCACHE=/tmp/pocketcli-go-build-cache go test -buildvcs=false ./internal/investigation/insightcontract` — passou (`[no test files]`).
- `python3 /Users/irinery/Downloads/PocketCli_Luna_1_Alto_2026-10-28/tools/validate_package.py` — passou: 50 estruturas Go, 24 casos sintéticos, 8 evidências e 15 arquivos do manifesto conferidos.

Este bootstrap não implementa L1–L4 e não altera o checkout de origem.
