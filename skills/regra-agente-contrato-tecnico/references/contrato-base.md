# Contrato base (regra operacional)

## Fluxo de update

- Preservar a execução de atualização mesmo quando houver arquivos locais diferentes do padrão.

## Personalização

- Centralizar arquivos customizáveis em `profile/` (ou depender apenas dela).
- Fora de `profile/`, permitir apenas:
  1. arquivo padrão compartilhado sem personalização;
  2. arquivo que referencia `profile/` por constante, variável ou identificador central.
- Evitar personalização distribuída em múltiplos arquivos versionados.

## Testes locais

- Ao adicionar/alterar testes locais, documentar execução, pré-requisitos e variáveis de ambiente necessárias.

## Pré-PR para código Go

- Antes de abrir/atualizar PR: executar fluxo local documentado de `gofmt`, `go test`, build e smoke tests.
