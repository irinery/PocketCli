# Validação da frente de apresentação executiva

Pré-requisito: Go instalado conforme a versão declarada no `go.mod`. Os testes usam fixtures sintéticas do pacote `insightcontract`, não acessam rede, credenciais nem provedor externo. Não há variáveis de ambiente necessárias.

Na raiz do repositório, execute:

```sh
go test ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web
```
