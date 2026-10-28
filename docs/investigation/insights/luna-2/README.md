# Testes da frente L2

Pré-requisito: Go 1.22 ou superior. Os testes usam somente fixtures sintéticas locais e não precisam de tokens, serviços externos ou variáveis de ambiente.

Na raiz do repositório, execute:

```sh
go test -buildvcs=false ./internal/investigation/insightcontract ./internal/investigation/analysisfacts
```

Para formatar o contrato e o pacote L2:

```sh
gofmt -w internal/investigation/insightcontract/v1.go internal/investigation/analysisfacts
```
