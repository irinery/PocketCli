# SEC-07 - Complemento de supply chain

Nivel: 4. Estado: aberto. Complementa [SEC-07 anterior](../2026-07-09/SEC-07-tailscale-supply-chain.md).

A autenticidade dos instaladores externos continua pendente. Alem do script Tailscale, `scripts/setup/requirements.yml:83-97` baixa lazygit de latest sem checksum e fixa `Linux_x86_64`, inclusive em hosts ARM64; as linhas 115-126 baixam e executam Starship sem assinatura/checksum. `provision/pocketcli-provision-host.sh` baixa e executa Docker/Tailscale remotos.

Download completo em arquivo temporario elimina execucao parcial por pipe, mas nao verifica autenticidade do conteudo.

Correcao proposta: pacotes assinados do fornecedor/distribuicao quando disponiveis, versoes e manifestos verificaveis para downloads, selecao de OS/arquitetura e instalacao centralizada via Ansible. O bootstrap minimo deve apenas destravar esse fluxo.

Validacao realizada: inspecao dos caminhos de download no SHA revisado. Validacao necessaria na correcao: checksum divergente impede instalacao; targets ARM64/x86_64 corretos; idempotencia e comportamento offline. Nao foram instaladas dependencias no host.
