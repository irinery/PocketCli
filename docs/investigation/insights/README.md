# Análise de insights integrada

A análise usa o snapshot já publicado e roda source → facts determinísticos → julgamento semântico → apresentação. Abrir a UI não inicia análise nem chama provider. A ação explícita fica disponível na interface; pela CLI:

```sh
pocket investigate analyze --snapshot SNAPSHOT_ID --config /caminho/local/analysis.json
```

O arquivo é um `AnalysisConfig` JSON do contrato `investigate.insights.v1`. O leitor rejeita propriedades desconhecidas, inclusive `api_key`, e não envia os campos até validar o JSON. Defina escopo, objetivos, política e orçamento; o config deve apontar para recursos presentes no snapshot. `provider.kind` pode ser `none` para fatos determinísticos e saída sem julgamento semântico.

## Provider OpenAI Responses opcional

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
      "adapter": "openai-responses",
      "model": "MODEL_EXPLICITAMENTE_ESCOLHIDO",
      "credential_env": "POCKETCLI_INSIGHT_OPENAI_KEY"
    }
  ]
}
```

Defina o valor da variável de ambiente indicada em `credential_env` na sessão local. Nunca coloque o token no perfil, no `AnalysisConfig`, no repositório ou em argumentos de linha de comando. O JSON por análise deve usar `provider.kind="configured"`, o mesmo `endpoint_profile` e `model` do perfil, e `adapter_version="openai-responses-v1"`. `policy.external_processing_allowed` também precisa ser `true`. O adaptador envia `store=false`, não segue redirects e usa somente o endpoint fixo da OpenAI; perfis não configuram URLs arbitrárias.

O limite de custo é fail-closed enquanto a API não fornece preço verificável por chamada: `budget.max_cost` deve ficar nulo para permitir tentativa. `max_requests`, tokens e duração precisam ser positivos. A abertura de snapshot não chama o serviço externo; somente a ação explícita de análise o faz. Sem perfil/credencial, o modo configurado permanece parcial/indeterminado e conserva os fatos, sem alegação positiva. Nenhuma chamada real é necessária para a suíte offline.

## Testes

Pré-requisitos: Go compatível com `go.mod`; testes locais usam fixtures sintéticas e não precisam de CGO, chave, rede ou variáveis de ambiente.

```sh
go test -buildvcs=false ./cmd/pocket ./internal/investigation/insightcontract ./internal/investigation/analysisfacts ./internal/investigation/decisionengine ./internal/investigation/insights ./internal/investigation/presentation ./internal/investigation/report ./internal/investigation/web ./tests/investigation/insights
```

O teste de integração offline gera PDFs executivos e detalhados em diretório temporário. Ele não mede qualidade semântica de um modelo real.
