# INT-01 - Binario Go ausente no instalador

Nivel: 3. Estado: aberto.

`scripts/fleet.sh:7-16` exige `bin/pocket-go` ou `POCKETCLI_GO_BINARY`. `bootstrap.sh`, `install.sh`, instaladores de requisitos e playbook nao constroem nem baixam esse artefato. O Release publica somente bootstrap, install e checksums. Go esta documentado como requisito de desenvolvimento.

Consequencia: instalacao fresca pelo caminho feliz nao entrega o backend Go necessario para fleet. A CI nao demonstra esse fluxo, porque compila o binario antes do smoke e passa um override.

Reproducao segura: `POCKETCLI_DIR=<clone> POCKETCLI_GO_BINARY=<arquivo-inexistente> sh scripts/fleet.sh list` retornou 64 e exigiu backend Go. Nenhum transporte foi chamado.

Correcao proposta: publicar artefatos por OS/arquitetura, verificar integridade e instalar atomicamente `bin/pocket-go` via Ansible, com atualizacao coerente com a versao shell. Go nao deve virar pre-requisito do viewer para compilar em runtime.

Validacao necessaria: instalacao em HOME temporario sem Go usando artefatos mockados; fleet/help/capabilities acessiveis; checksum invalido bloqueia; update preserva profile. Nao alterar assercoes existentes para aceitar ausencia do binario.
