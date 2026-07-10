# SEC-01 - capacidade de aprovacao reutilizavel

Criticidade: 5. Estado: corrigido.

O token de aprovacao podia ser reutilizado ate expirar e, se `crypto/rand` falhasse, recebia um fallback previsivel baseado em tempo. Isso permitia replay de uma aprovacao e enfraquecia a unica barreira para comandos confirmados.

`internal/safety` agora falha fechado quando nao ha entropia criptografica e usa comparacao em tempo constante. `ConsumeApproval` reivindica o arquivo de token com `rename` atomico, valida o token e remove a reivindicacao; a segunda tentativa falha. `pocket exec`, `fleet exec` e o helper de safety usam consumo, nao apenas validacao.

Testes: `go test ./internal/safety ./cmd/pocket` cobre consumo unico e indisponibilidade de entropia; a validacao completa roda em `sh scripts/run_local_ci.sh`.
