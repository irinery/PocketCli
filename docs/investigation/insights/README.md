# Análise de insights integrada

A análise usa o snapshot já publicado e roda source → facts determinísticos → julgamento semântico → apresentação. Abrir a UI não inicia análise nem chama provider. A ação explícita fica disponível na interface; pela CLI:

```sh
pocket investigate analyze --snapshot SNAPSHOT_ID --config /caminho/local/analysis.json
```

O arquivo é um `AnalysisConfig` JSON do contrato `investigate.insights.v1`. O leitor rejeita propriedades desconhecidas, inclusive `api_key`, e não envia os campos até validar o JSON. Defina escopo, objetivos, política e orçamento; o config deve apontar para recursos presentes no snapshot. `provider.kind` pode ser `none` para fatos determinísticos e saída sem julgamento semântico.

## Provider via MiddlewareAuth

Para habilitar a opção externa, configure os perfis locais em:

```text
~/.config/pocketcli/profile/investigation-providers.json
```

O arquivo é metadado local, sem credencial. Exemplo de estrutura:

```json
{
  "version": "1",
  "profiles": [
    {
      "id": "openai-work",
      "adapter": "middlewareauth-llm",
      "model": "MODEL_EXPLICITAMENTE_ESCOLHIDO",
      "credential_env": "MIDDLEWARE_CLIENT_TOKEN",
      "base_url": "http://127.0.0.1:18787",
      "project_id": "pocketcli",
      "provider_id": "openai",
      "profile_id": "default"
    }
  ]
}
```

Defina o token interno na variável indicada em `credential_env` na sessão local. Nunca coloque o token no perfil, no `AnalysisConfig`, no repositório ou em argumentos de linha de comando. O JSON por análise deve usar `provider.kind="configured"`, o mesmo `endpoint_profile` e `model` do perfil, e `adapter_version="middlewareauth-llm-v1"`. `policy.external_processing_allowed` também precisa ser `true`. O adaptador fala com o broker local em `127.0.0.1`, envia `store=false`, não segue redirects e valida o JSON retornado; o MiddlewareAuth guarda a credencial do provedor. `project_id`, `provider_id` e `profile_id` devem existir e estar autorizados no catálogo do broker. O endpoint local é opcional e usa a porta 18787 por padrão.

O limite de custo é fail-closed enquanto o broker não fornece preço verificável por chamada: `budget.max_cost` deve ficar nulo para permitir tentativa. `max_requests`, tokens e duração precisam ser positivos. A abertura de snapshot não chama o serviço externo; somente a ação explícita de análise o faz. Sem perfil/token interno, o modo configurado permanece parcial/indeterminado e conserva os fatos, sem alegação positiva. Nenhuma chamada real é necessária para a suíte offline.

## Testes

Pré-requisitos: Go compatível com `go.mod`; testes locais usam fixtures sintéticas e não precisam de CGO, chave, rede ou variáveis de ambiente.

```sh
go test -buildvcs=false ./cmd/pocket ./internal/investigation/insightcontract ./internal/investigation/analysisfacts ./internal/investigation/decisionengine ./internal/investigation/insights ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web ./tests/investigation/insights
```

O teste de integração offline gera PDFs executivos e detalhados em diretório temporário. Ele não mede qualidade semântica de um modelo real.

Para conferir snapshots antigos após mudanças nos tipos do dataset, execute `go test -buildvcs=false -run TestPublishedDatasetHashPreservesOldWireShapeAndDetectsTamper ./internal/investigation/core`. O leitor valida o hash dos bytes publicados, sem preencher campos adicionados mais tarde e sem desativar a verificação de integridade.

## Ciclo local da UI

Pré-requisitos: Go e compilador C para SQLite/CGO; snapshot publicado no armazenamento local. Compile e sirva a investigação do snapshot:

```sh
CGO_ENABLED=1 go build -buildvcs=false -tags investigate_sqlite -o /tmp/pocket-insights ./cmd/pocket
/tmp/pocket-insights investigate serve INVESTIGATION_ID
```

Abra a URL `127.0.0.1` impressa, desbloqueie com o código local, selecione um `AnalysisConfig` JSON que aponta para o snapshot e clique em **Iniciar análise**. Confira versão, cobertura, métricas, casos e as duas exportações. Na aba SQL, execute `SELECT id, window_start, window_end FROM v_snapshot` e confira o ID/janela. A análise sem provider usa `provider.kind="none"` e `external_processing_allowed=false`; o ciclo semântico real exige profile/token interno e autenticação prévia no MiddlewareAuth. Não copie o token para o JSON nem para o log do browser.
