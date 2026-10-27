# Fila HTTP por principal e fornecedor

O Agent CLI/HTML resolve a conta antes da primeira coleta: GitHub usa `GET /user` e Linear usa a query fixa `viewer { id }`. O ID é vinculado localmente ao hash da credencial e ao host da fonte, sem persistir o token. O plano mostra o ID verificado e continua exigindo confirmação do hash. A leitura de identidade e a consulta GitHub `GET /versions` entram no orçamento de descoberta; se os repositórios selecionados consumirem o limite, o plano falha antes da rede.

Enquanto a identidade é desconhecida, a leitura usa um lock exclusivo por fornecedor/host. Requests de identidades conhecidas seguram um lock compartilhado desse fornecedor e um lock exclusivo do principal. Assim duas chaves da mesma conta compartilham fila e pacing entre processos, e contas diferentes podem executar em paralelo. `providerholds/` conserva a maior espera para impedir que uma chave ainda desconhecida consulte a API durante um rate limit já observado. `rateholds/` conserva a espera por principal; o primeiro vínculo migra uma espera antiga associada à credencial. Os arquivos ficam no storage privado da investigação; a chave de vinculação é um hash, não um token.

O pacing é gravado antes de cada tentativa para recuperação após crash e ajustado pelo horário real de início depois do recibo HTTP. O ajuste impede que um storage lento encurte o intervalo entre requests. O `RequestReceipt` continua registrando somente endpoint lógico, status, contagem e bytes, além das versões REST GitHub já documentadas.

`newInvestigationServicesWithSecrets` habilita a resolução para a CLI e o servidor HTML. Quem cria `Core` diretamente deve passar `CoreConfig{ResolvePrincipals: true}`; sem essa opção, o comportamento legado continua por hash da credencial. Este documento substitui a observação anterior de S03 em `sol-implementation.md` que dizia que duas chaves do mesmo ator Linear não compartilhavam fila. A fila com duas chaves da mesma conta e o fluxo Linear foram validados com servidores falsos; ainda faltam smoke real Linear e observação de duas instâncias do processo em contas reais.

`TestPrincipalQueueAcrossProcesses` usa dois processos Go, duas chaves fictícias ligadas ao mesmo ator e um servidor TLS local. Os processos entram juntos na request de negócio; o servidor observa no máximo uma request em voo. O teste passou cinco vezes consecutivas. Isso valida o lock entre processos sem depender de contas reais.

Se a credencial for trocada entre a consulta de identidade e a request seguinte, o coletor retorna `ERR_PLAN_CHANGED` antes dessa request. `TestCredentialRotationDuringLookupStopsBeforeBusinessRequest` cobre essa troca com uma SecretStore de sessão.

Smoke real autorizado em 26/10/2026: a CLI preparada com a conta GitHub padrão e somente `irinery/socialMediaFactory`, janela de 30 dias, Linear desligado e orçamento de duas chamadas de descoberta concluiu `--prepare --json`. Os dois recibos foram `GET /user` e `GET /repos/{id}/{id}`, ambos HTTP 200, com versão REST solicitada e selecionada `2026-03-10`. O plano trouxe exatamente o repositório selecionado e aviso da conta verificada (ID `73475414`). O storage temporário guardou um vínculo com esse ID e nenhum campo de token/segredo. Uma segunda preparação em outro processo com o mesmo storage e orçamento de uma chamada concluiu sem nova consulta `/user`; o único recibo novo foi `GET /repos/{id}/{id}` com HTTP 304. O token não apareceu em stdout nem stderr. Este smoke valida a identificação persistida, seu orçamento e o reuso do cache, mas não valida a coleta, duas credenciais reais da mesma conta nem Linear real.

Smoke real adicional em 27/10/2026, após introduzir a consulta de versões: com a mesma conta e repositório, orçamento 2 falhou antes de qualquer request; orçamento 3 concluiu com `GET /user`, `GET /versions` e `GET /repos/{id}/{id}`. O servidor listou e selecionou `2026-03-10`. Nova preparação em outro processo, orçamento 1 e mesmo storage, concluiu com uma única chamada ao repositório (HTTP 304); identidade e versão vieram dos vínculos/cache privados. O token não apareceu em stdout nem stderr. A lista de versões é reconsultada após 24 horas; comportamento em GitHub Enterprise Server ainda não foi observado ao vivo.

A versão REST configurada também entra na identidade privada e no hash do plano. Se uma atualização do PocketCli mudar a versão, `StartRun` rejeita um plano antigo com `ERR_PLAN_CHANGED` e exige nova preparação; snapshots já publicados continuam disponíveis. `TestPlanPinsGitHubAPIVersionInPrivateIdentity` cobre a divergência de versão sem chamar a rede real.

Teste local (Go 1.22+, compilador C para a suite SQLite; sem token, rede externa, Chromium ou variáveis de ambiente):

```sh
go test -buildvcs=false -run 'TestTwoCredentialsOfSameActorShareRequestQueue|TestTwoLinearKeysOfSameViewerShareQueue|TestSameActorSharesRateHoldAcrossCredentials|TestUnknownCredentialDoesNotProbeDuringProviderHold|TestDifferentActorsCanRequestConcurrently|TestPacingUsesActualRequestStartAfterSlowStorage|TestPrincipalLookupKeepsGitHubAPIPrefix|TestCredentialRotationDuringLookupStopsBeforeBusinessRequest' -count=1 ./internal/investigation/collect
go test -buildvcs=false -run TestPrincipalQueueAcrossProcesses -count=1 ./internal/investigation/collect
go test -buildvcs=false -run TestSharedLockBlocksWriterAndAllowsReaders -count=1 ./internal/investigation/storage
CGO_ENABLED=1 go test -buildvcs=false -tags investigate_sqlite -run 'TestPrincipalResolutionConsumesDiscoveryBudgetOnlyOnFirstUse|TestUnsupportedGitHubVersionStopsPlanBeforeRepositoryDiscovery' -count=1 ./internal/investigation/core
go test -buildvcs=false -run TestPlanPinsGitHubAPIVersionInPrivateIdentity -count=1 ./internal/investigation/core
go test -buildvcs=false -run 'TestGitHubVersionProbe|TestCorruptGitHubVersionCacheStopsBeforeNetwork' -count=1 ./internal/investigation/collect
```

As leituras de identidade seguem as APIs oficiais: [GitHub `/user`](https://docs.github.com/en/rest/users/users#get-the-authenticated-user) e [Linear `viewer`](https://linear.app/developers/graphql).
