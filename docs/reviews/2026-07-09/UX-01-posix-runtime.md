# UX-01 - runtime declarado POSIX com sintaxe nao POSIX

Criticidade: 3. Estado: corrigido.

O inventario usava `IFS=$'\t'`, sintaxe Bash que nao e definida por POSIX `sh`; isso afeta Alpine/iSH. O teste de compatibilidade tambem usava `local`, mascarando a garantia declarada.

Leitura tabulada usa agora `TAB=$(printf '\t')` e o teste usa somente escopo compativel com POSIX. Ajustes complementares removem avisos do ShellCheck no detector de capabilities.

Testes: `shellcheck -S warning pocket bootstrap.sh install.sh $(rg --files -g '*.sh' scripts lib tests)` nao retorna warnings; `sh scripts/run_local_ci.sh` executa a regressao shell em conjunto.
