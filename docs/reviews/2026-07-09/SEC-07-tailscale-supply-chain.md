# SEC-07 - autenticidade do instalador externo do Tailscale

Criticidade: 4. Estado: aberto.

O fluxo legado de daemon executava `curl | sh`. Ele foi corrigido para baixar em arquivo temporario e executar somente apos download completo, eliminando a execucao por pipe e permitindo auditoria/inspecao local do artefato.

Ainda falta uma cadeia de autenticidade verificavel para o script externo: o repositorio nao publica nem consome checksum assinado, tag imutavel ou pacote com chave do fornecedor por distribuicao. Executar o script baixado continua depender de TLS e da integridade do endpoint externo.

Para fechar: definir politica de releases assinadas, preferir pacote assinado por distribuicao/repo do fornecedor e fixar versao/checksum no instalador. Isso exige decidir a matriz Debian/Ubuntu/macOS e manter hashes/release metadata, por isso ficou fora desta branch.

Validacao realizada: `sh tests/test_install_tailscale.sh`, `sh tests/test_tailscale_setup_fallback.sh` e scanner `bash scripts/security/04_ssh_tailscale_hardening.sh`.
