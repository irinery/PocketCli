# pocket ansible

`pocket ansible` integra o Ansible ao PocketCli como camada de execução segura e
gestão de inventário. Os artefatos principais são:

- `lib/ansible_adapter.sh`, instalado como `~/.pocketcli/lib/ansible_adapter.sh`
- `lib/ansible_inventory.sh`, instalado como `~/.pocketcli/lib/ansible_inventory.sh`
- `lib/ansible_registry.sh`, instalado como `~/.pocketcli/lib/ansible_registry.sh`
- `lib/ansible_wiki_hook.sh`, instalado como `~/.pocketcli/lib/ansible_wiki_hook.sh`
- `lib/ansible_init.sh`, instalado como `~/.pocketcli/lib/ansible_init.sh`

## Estrutura esperada

```text
~/.pocketcli/ansible/
  inventory/
    hosts.yml
    tailscale_generated.yml
    merged.yml
  playbooks/
    exemplo.yml
~/.pocketcli/logs/
  ansible.log
~/.pocketcli/wiki/ansible/
  _index.md
  <run_id>.md
```

Todos os caminhos derivam de `POCKET_DIR`, que por padrão é `~/.pocketcli`.
Para testes locais, é seguro sobrescrever `POCKET_DIR`, `ANSIBLE_DIR`,
`INVENTORY_DIR`, `PLAYBOOKS_DIR` e `ANSIBLE_LOG`.

## Inventário

```sh
pocket ansible inventory
pocket ansible inventory --source static
pocket ansible inventory --source tailscale
pocket ansible inventory --source merged
pocket ansible inventory refresh
```

`inventory refresh` lê `tailscale status --json` e grava
`inventory/tailscale_generated.yml`. Se o Tailscale estiver offline, o arquivo
gerado anteriormente é preservado.

Origens suportadas:

| Origem | Arquivo |
|---|---|
| `static` | `inventory/hosts.yml` |
| `tailscale` | `inventory/tailscale_generated.yml` |
| `merged` | `inventory/merged.yml`, com estático prevalecendo em conflito |

Schema mínimo do `hosts.yml`:

```yaml
all:
  vars:
    ansible_user: root
  hosts:
    web01:
      ansible_host: 100.64.0.10
      ansible_port: 22
      pocket_tags:
        - web
  children:
    prod:
      hosts:
        web01: {}
```

Hostnames precisam casar com:

```text
^[a-zA-Z0-9][a-zA-Z0-9_\-\.]{0,62}$
```

Hosts inválidos são rejeitados individualmente; os demais continuam disponíveis.
No merge, conflitos são deduplicados por hostname e o inventário estático
prevalece.

## Execução

```sh
pocket ansible list
pocket ansible init exemplo --category utility
pocket ansible run exemplo
pocket ansible run exemplo --diff
pocket ansible run exemplo --run
pocket ansible run exemplo --run --diff
pocket ansible run exemplo --source merged
pocket ansible run exemplo --source merged --limit web01
```

`pocket ansible list` indexa os `.yml` em `PLAYBOOKS_DIR`, sem recursão, ignora
symlinks e mostra playbooks válidos separados dos inválidos.

`pocket ansible run <slug>` passa primeiro pelo registry. O registry valida
`pocket_meta`, YAML básico, categoria, `safe_modes` e `inventory_source`. Depois
de aprovado, delega ao adapter.

O modo padrão é `check`: o adapter chama `ansible-playbook` com `--check` quando
`--run` não é passado. Com `--diff` sem `--run`, ele usa `--check --diff`. Com
`--run --diff`, ele executa de verdade e passa `--diff`.

Quando `--inventory` não é passado, o adapter resolve o inventário via
`ansible_inventory.sh`. O limite padrão é 50 hosts por run; acima disso a
execução é bloqueada, a menos que `--limit` seja informado.

O nome do playbook precisa casar com:

```text
^[a-zA-Z0-9_\-]{1,80}\.yml$
```

Qualquer valor fora desse formato é recusado antes da chamada ao Ansible.

## Registry

Todo playbook registrado precisa ter `pocket_meta` como primeira play:

```yaml
- name: pocket_meta
  hosts: localhost
  gather_facts: false
  vars:
    pocket_meta:
      name: exemplo
      description: Exemplo operacional
      author: irinery
      version: "1.0.0"
      category: utility
      safe_modes:
        - check
        - run
      inventory_source: static
      created_at: "2026-05-11"
      updated_at: "2026-05-11"
  tasks: []
```

Categorias aceitas: `diagnostic`, `setup`, `deploy`, `maintenance`, `security`,
`network` e `utility`. O limite do registry é 200 playbooks por listagem.

## Scaffold

```sh
pocket ansible init meu_playbook --category diagnostic
pocket ansible init deploy_site --category deploy
pocket ansible init meu_playbook --force
```

O nome precisa casar com:

```text
^[a-z0-9][a-z0-9_\-]{0,79}$
```

Sem `--category`, o scaffold usa `utility` e mostra um aviso. Safe modes padrão:

| Categoria | Safe modes |
|---|---|
| `diagnostic` | `check`, `diff` |
| `setup` | `check`, `diff`, `run` |
| `deploy` | `check`, `diff`, `run` |
| `maintenance` | `check`, `diff`, `run` |
| `security` | `check`, `diff` |
| `network` | `check`, `diff` |
| `utility` | `check`, `diff`, `run` |

Convenções completas ficam em [docs/playbook_pattern.md](../playbook_pattern.md).

## Logs

Cada execução que chega ao `ansible-playbook` grava uma linha JSON em
`~/.pocketcli/logs/ansible.log` com:

```json
{"run_id":"...","timestamp":"...","playbook":"exemplo.yml","mode":"check","inventory_source":"static","result":"success","exit_code":0,"duration_seconds":1,"ansible_version":"ansible [core ...]","hosts_targeted":1,"stderr_excerpt":"","stdout_excerpt":"..."}
```

O log é rotacionado para `ansible.log.1` quando passa de 10 MB antes de uma nova
gravação. Saídas maiores que 512 KB são truncadas com a nota `[TRUNCADO]`.

## PocketWiki

Após toda execução que chega ao adapter, o hook cria uma entrada Markdown em:

```text
~/.pocketcli/wiki/ansible/<run_id>.md
```

O índice fica em:

```text
~/.pocketcli/wiki/ansible/_index.md
```

O índice mantém as 500 execuções mais recentes. Arquivos `.md` antigos são
preservados; apenas saem do índice.

Para consultar o histórico:

```sh
pocket ansible log
pocket ansible log --last 5
```

O hook lê o JSONL já gravado em `ANSIBLE_LOG`; ele não reexecuta Ansible.
`stderr_excerpt` é sanitizado antes de entrar no Markdown.

## Exit codes

| Código | Significado |
|---:|---|
| 0 | execução Ansible concluída com sucesso |
| 1 | erro interno do adapter ou estrutura incompleta |
| 2 | `ansible-playbook` retornou falha |
| 3 | playbook ou inventário não encontrado |
| 4 | input recusado por validação |
| 124 | timeout de execução |
| 127 | `ansible-playbook` ou `ansible` ausente no `PATH` |

## Teste local

Para validar só o contrato da Fase 01:

```sh
sh tests/test_ansible_adapter.sh
```

Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `wc`, `dd` e `chmod`.
O teste usa mocks locais de `ansible` e `ansible-playbook`, então não exige
Ansible real instalado.

Para validar só o contrato da Fase 02:

```sh
sh tests/test_ansible_inventory.sh
```

Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `wc`, `jq` ou
`python3`, e `chmod`. O teste usa mocks locais de `tailscale`, `ansible` e
`ansible-playbook`.

Para validar só o contrato da Fase 03:

```sh
sh tests/test_ansible_registry.sh
```

Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `wc`, `ln` e
`chmod`. O teste usa mocks locais de `ansible` e `ansible-playbook`.

Para validar só o contrato da Fase 04:

```sh
sh tests/test_ansible_wiki_hook.sh
```

Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `wc`, `tr` e
`chmod`. O teste usa mocks locais de `ansible` e `ansible-playbook`.

Para validar só o contrato da Fase 05:

```sh
sh tests/test_ansible_init.sh
```

Pré-requisitos: shell POSIX com `mktemp`, `awk`, `sed`, `grep`, `find`, `seq` e
`chmod`.
