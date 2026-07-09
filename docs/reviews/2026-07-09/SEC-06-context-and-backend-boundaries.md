# SEC-06 - vazamento e consumo ilimitado no contexto e backend por comando

Criticidade: 4. Estado: corrigido.

O backend configurado por comando exportava o prompt completo em `POCKETCLI_PROMPT` e acumulava sua saida sem limite. O coletor de contexto lia arquivos e `git diff` integralmente antes de truncar. Anexos seguiam symlinks sem testar o destino e podiam incluir conteudo com segredo.

O prompt agora segue apenas por stdin e a saida do backend e limitada a 64 KiB. Status Git, contexto Git, Tailscale e comandos capturados receberam limites de memoria; coleta de arquivos para no maximo 64 arquivos de 128 KiB cada. Anexos resolvem symlink antes da policy e sao bloqueados quando o destino ou conteudo tem indicador de segredo.

Testes: `go test ./cmd/pocket ./internal/contextcollector ./internal/contextcompiler ./internal/tools ./internal/tailscale` cobre prompt via stdin, truncamento, limite de arquivo e bloqueio de symlink/conteudo. O fluxo completo roda em `sh scripts/run_local_ci.sh`.
