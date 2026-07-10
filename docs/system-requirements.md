# Requisitos do sistema

Este documento separa requisito de uso do PocketCli de requisito de desenvolvimento/teste.

## Regra de instalacao

O caminho feliz de instalacao e:

1. `bootstrap.sh` baixa ou atualiza o PocketCli em `~/.pocketcli`.
2. `install.sh` detecta OS e modo (`viewer` ou `agent`).
3. `install.sh` chama `scripts/install_requirements.sh`.
4. `install_requirements.sh` roda `scripts/setup/requirements.yml` via `ansible-playbook`.
5. Se Ansible ainda nao existir, o fallback minimo `scripts/install_deps.sh` instala o necessario para destravar o playbook.
6. `install.sh` configura profile/tmux/starship, roda setup do Tailscale e inicia viewer ou agent.

Regra pratica: requisitos de sistema novos devem entrar no playbook `scripts/setup/requirements.yml`. O shell fica restrito ao bootstrap, download do repo e fallback para conseguir rodar Ansible quando ele ainda nao existe.

## Runtime viewer

Viewer e o cliente leve, pensado para iPad/iSH ou terminal remoto simples. Ele nao deve exigir Ansible em runtime.

Requisitos instalados/esperados:

| Ferramenta | Uso |
|---|---|
| `sh` | execucao dos scripts POSIX |
| `ssh` | conexao remota |
| `git` | update quando disponivel |
| `curl` | downloads e checks remotos |
| `jq` | parsing JSON dos fluxos shell |
| `tmux` | sessao persistente quando disponivel |
| `zsh` | shell/profile padrao do projeto |
| `tailscale` | rede privada; recomendado, mas com fallback para hosts salvos |
| `qrencode` | opcional para auth QR do Tailscale |

## Runtime agent

Agent e a maquina completa, onde faz sentido rodar automacao local e Skill Layer.

Inclui os requisitos do viewer e adiciona:

| Ferramenta | Uso |
|---|---|
| `python3` | validacao JSON e helpers da Skill Layer |
| `ansible` / `ansible-playbook` | instalacao por playbook e execucao da Skill Layer |
| `ripgrep` | busca rapida |
| `htop` | painel operacional |
| `lazygit` | TUI de Git |
| `starship` | prompt do perfil agent |

## Desenvolvimento e validacao local

Os requisitos abaixo nao sao obrigatorios para usar o PocketCli como viewer. Eles sao para desenvolver, testar e validar release localmente.

| Ferramenta | Uso |
|---|---|
| `go` / `gofmt` | build e testes dos comandos Go |
| `bash` | scanners e alguns testes de seguranca |
| `python3` | validacao JSON em testes e Skill Layer |
| `jq` | testes e parsing de fixtures |
| `tmux` | testes de sessao/TUI |
| `git` | testes de bootstrap/update e versionamento |

Fluxo local documentado:

```sh
sh scripts/run_local_ci.sh
```
