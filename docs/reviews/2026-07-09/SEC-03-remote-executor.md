# SEC-03 - fronteira do executor remoto

Criticidade: 4. Estado: corrigido.

O executor acumulava stdout/stderr sem limite antes de truncar, aceitava hostname configurado com formato amplo demais e permitia prompt no stdin durante `exec`. Um `remote-hosts.json` gravavel por grupo/outros tambem podia alterar o destino ou transporte.

O executor agora limita captura em memoria a 64 KiB por stream, preserva marcador de truncamento, executa SSH em batch sem stdin e valida hostname, IP Tailscale, usuario, porta e transporte. Arquivo de hosts precisa ser regular e nao gravavel por grupo/outros; diretorio gravavel por grupo/outros tambem falha fechado. Falha de escrita da auditoria agora aparece como `audit_failed`; logs existentes sao reparados para `0600`.

Testes: `go test ./internal/remoteaccess ./cmd/pocket` cobre captura limitada, host option-like, store inseguro, transporte e falha de auditoria. A suite completa roda em `sh scripts/run_local_ci.sh`.
