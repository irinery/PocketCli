# SEC-05 - instalacao e update nao reparavam permissoes nem preservavam bootstrap

Criticidade: 4. Estado: corrigido.

Instalacoes existentes em `~/.pocketcli` podiam continuar `0755`. Alem disso, `bootstrap.sh` atualizava um checkout Git sem o stash temporario usado por `pocket update`, contrariando o contrato de preservacao de mudancas locais.

Bootstrap e instalador passam a usar `0700` na arvore instalada, inclusive no fallback por archive. A atualizacao por bootstrap detecta mudancas, cria stash incluindo untracked, faz fetch/checkout/pull e restaura o stash; em conflito o stash fica preservado para revisao manual.

Testes: `sh tests/test_bootstrap_install.sh` valida clone, update, archive e instalacao. `sh tests/test_pocket_update.sh` valida o fluxo de stash do comando principal. Ambos entram em `sh scripts/run_local_ci.sh`.
