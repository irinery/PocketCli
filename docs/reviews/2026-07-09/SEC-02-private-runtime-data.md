# SEC-02 - estado operacional exposto por permissoes permissivas

Criticidade: 4. Estado: corrigido.

Ledger, planos fleet, memoria, capabilities e estado recente podiam ser criados como `0755`/`0644`. Isso expunha comandos, historico, topologia e, no caso do ultimo comando, um token de aprovacao informado por flag.

`internal/pocketpath` centraliza diretorios privados em `0700` e escrita atomica em arquivos `0600`, corrigindo tambem modos herdados. Ledger, memoria, planos e capabilities adotaram esses modos. O runtime shell aplica `umask 077`, protege config/data/cache/state/inventario e deixa de persistir comandos com `--approval-token`.

Testes: `go test ./internal/pocketpath ./internal/ledger ./internal/memory ./internal/safety` valida criacao e reparo de permissao; `sh scripts/run_local_ci.sh` cobre os fluxos integrados.
