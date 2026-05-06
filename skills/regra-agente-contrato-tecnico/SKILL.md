---
name: regra-agente-contrato-tecnico
description: Converte e aplica regras de um contrato técnico de agente para mudanças de código e revisões de PR. Use quando for necessário transformar políticas em critérios verificáveis, criar checklist de compliance para alterações, revisar fluxos sensíveis (como update/sincronização), ou garantir centralização de personalização em profile/ sem espalhar regras em múltiplos arquivos.
---

# Regra Agente Contrato Tecnico

## Objetivo

Aplicar um contrato técnico como padrão operacional de desenvolvimento, revisão e entrega.

## Fluxo de execução

1. Ler o contrato técnico (arquivo principal e anexos necessários).
2. Extrair regras normativas em formato imperativo e testável.
3. Classificar regras por área (fluxo, estrutura de arquivos, testes, release/PR).
4. Converter regras em checklist objetivo para implementação e code review.
5. Validar mudanças com evidências (arquivos alterados + comandos executados).

## Extração de regras

Ao processar o contrato técnico, registrar cada regra com:

- **ID curto** (ex.: `UPD-001`, `PROF-002`)
- **Regra** (imperativa e sem ambiguidade)
- **Escopo** (arquivos, comandos, fluxos afetados)
- **Critério de aceitação** (como provar conformidade)
- **Risco se violada** (baixo/médio/alto)

Se o contrato trouxer frases vagas, reescrever em formato verificável sem alterar intenção.

## Checklist padrão de conformidade

Use este checklist como base e ajuste ao contrato carregado:

- Preservar atualização em fluxos de update mesmo com arquivos locais divergentes.
- Centralizar personalização do usuário em `profile/`.
- Fora de `profile/`, manter apenas arquivos padrão globais ou arquivos que referenciem `profile/` por identificador central.
- Evitar personalização espalhada em vários arquivos versionados.
- Documentar execução de testes locais (pré-requisitos e variáveis de ambiente) quando novos testes forem criados/alterados.
- Em mudanças Go, executar e registrar `gofmt`, `go test`, build e smoke tests antes de PR.

## Evidências mínimas para PR

Sempre produzir evidências objetivas:

- Lista de arquivos alterados por regra impactada.
- Comandos executados para validação.
- Resultado final por regra: `conforme`, `parcial`, ou `não conforme`.

## Recursos

- Regras de base do contrato: `references/contrato-base.md`.
- Se existir anexo externo com regras adicionais, consolidar neste mesmo arquivo antes de implementar mudanças.
