# SEC-04 - bypass da policy nos entrypoints shell

Criticidade: 4. Estado: corrigido.

Os entrypoints shell tinham fallback para `ssh`/`scp` puros quando wrappers nao existiam. O `fleet` legado executava comandos paralelos por SSH direto. A configuracao de policy tambem podia injetar opcoes porque flags eram expandidas sem validacao suficiente. O pacote Go `internal/ssh` continuava disponivel com execucao direta, embora sem uso.

`pocket connect`, `run`, `copy` e o menu falham fechado sem wrapper seguro. `scripts/fleet.sh` agora delega somente ao binario Go configurado, que contem plano, approvals, audit e policy; sem ele retorna erro 64. A policy shell restringe cada valor a um dominio conhecido, protege o arquivo e seu diretorio, e `copy` rejeita host/operando option-like. O pacote Go inseguro foi removido.

Testes: `sh tests/test_secure_shell_entrypoints.sh`, `sh tests/unit/test_ssh_policy.sh` e `sh tests/unit/test_ssh_copy_wrapper.sh`; validacao integral em `sh scripts/run_local_ci.sh`.
