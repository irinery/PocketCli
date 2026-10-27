# CI-02 - Tag criada sem release publicada

Nivel: 3. Estado: aberto.

API do GitHub: `v1.3.4` aponta para o merge `e23c46d`, mas nao ha Release para essa tag (404). A release mais recente publicada e `v1.3.0`, de 2026-05-12. Tag release passou no merge; o ultimo run do workflow Release e de maio.

`tag-release.yml` faz checkout com o token padrao e `git push` da tag (linha 73). `release.yml:3-7` depende de evento push de tag. Eventos push produzidos pelo `GITHUB_TOKEN` nao disparam outro workflow, conforme [documentacao oficial](https://docs.github.com/en/actions/concepts/security/github_token). O encadeamento atual explica a divergencia observada.

Correcao proposta: publicar no mesmo pipeline ou chamar um workflow reutilizavel explicitamente, usando o SHA aprovado. Permitir recuperar uma tag existente sem release e tornar a publicacao idempotente. Incluir os artefatos Go de INT-01 e checksums.

Validacao realizada: metadados de tags, releases e runs, leitura dos workflows e documentacao oficial. Validacao necessaria: tag aprovada gera release/artefatos; reexecucao nao duplica nem muda o SHA; falha de CI impede publicacao. Nenhuma release foi publicada durante esta review.
