# Pocket Stack no Insomnia

Esta pasta contém dois artefatos importáveis, gerados a partir do código local em 2026-06-19:

- `pocket-stack.insomnia.json`: coleção pronta, com pastas, requests, exemplos e ambiente.
- `pocket-stack.openapi.yaml`: OpenAPI 3.1 para usar como Design Document e documentação navegável.

O Insomnia 13.0.1 instalado nesta máquina aceita Insomnia JSON v4/v5 e OpenAPI 3.0/3.1. No app, use **Import > File**. Importe o JSON para executar a coleção ou o OpenAPI para navegar/lintar o contrato; importar ambos cria dois projetos independentes.

No ambiente da coleção, preencha pelo menos `middleware_client_token`, `lmstudio_api_key` e `model_id`. Os arquivos não contêm segredo real. Os defaults de rede são:

```text
PocketWiki     http://127.0.0.1:8787
PocketKernel   http://127.0.0.1:8080
MiddlewareAuth http://127.0.0.1:18787
LM Studio      http://127.0.0.1:1234
```

Ordem prática de teste:

1. Rode `MiddlewareAuth — Health`.
2. Rode `LM Studio — registrar API key` e depois `LM Studio — status`.
3. Rode `PocketKernel — Query governada`.
4. Rode `PocketWiki — Configuração pública` e `Proxy → PocketKernel`.
5. Para OpenAI, inicie `device code`, abra a URL retornada, copie `loginSessionId` para o ambiente e consulte o status da sessão.

O `PocketCli` não aparece como pasta HTTP porque não expõe servidor. Seu `skill_endpoint.sh` usa stdin/arquivo local. O MCP Evidence do PocketWiki também usa stdio e, portanto, não é testável como request HTTP no Insomnia.

A documentação completa e os diagramas estão em `/Users/irinery/Documents/pocketwiki/SKILL/wiki-reference/pocket-stack-rotas-insomnia.md`.

Referências do formato: [import/export do Insomnia](https://developer.konghq.com/insomnia/import-export/) e [OpenAPI como Design Document](https://developer.konghq.com/how-to/import-an-api-spec-as-a-document/).
