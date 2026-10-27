# SEC-09 - Estado temporario e autenticacao Tailscale

Nivel: 4. Estado: aberto. Evidencia por leitura de `scripts/tailscale_daemon.sh`; nenhuma exploracao ou alteracao do daemon real foi realizada.

O script usa `/tmp/tailscaled.pid` e `/tmp/tailscaled.log` fixos (linhas 16-17), trunca o log (148), grava PID (157) e confia no PID lido ao enviar `kill` (193-195). Auth usa `/tmp/ts_auth_$$.log` (465-466) e grava a URL de login sem criar arquivo privado nem rejeitar symlink. Nao ha `umask` restritiva nesse script.

Outro usuario local pode precriar caminhos ou links. Impacto depende das permissoes do processo que roda o script: gravacao indevida em arquivo, exposicao da URL de auth ou sinalizacao de processo incorreto. O caminho sem URL tambem faz `wait` sem deadline (502), podendo ultrapassar o timeout descrito na interface.

Correcao proposta: diretorio privado no runtime/cache do usuario, arquivos 0600 criados de forma segura, validar ownership/identidade do PID e centralizar cleanup/deadline para todos os caminhos de autenticacao. Tratar backend nativo como gerenciado pelo sistema.

Validacao necessaria: mocks de daemon/auth para link preexistente, PID invalido, timeout com e sem URL, cancelamento e permissoes. Assercoes dos testes existentes devem ser preservadas.
