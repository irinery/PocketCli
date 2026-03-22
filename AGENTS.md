# AGENTS.md

## Regras para futuras atualizações

- Ao alterar qualquer fluxo relacionado a `pocket update`, preserve a atualização mesmo quando existirem arquivos locais diferentes do padrão.
- Arquivos customizáveis do usuário devem ficar centralizados em uma pasta `profile/` (ou depender apenas dela).
- Fora de `profile/`, os arquivos do projeto devem seguir apenas um destes formatos:
  1. arquivo padrão compartilhado por todos os usuários, baixado sem personalização;
  2. arquivo que referencia `profile/` por constantes, variáveis ou um identificador único centralizado.
- Evite espalhar personalização direta em múltiplos arquivos versionados; prefira um único ponto de referência em `profile/`.
