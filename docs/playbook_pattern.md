# PocketCLI Playbook Pattern

Playbooks usados via `pocket ansible run <slug>` devem começar com uma play
`pocket_meta`. Esse bloco é o contrato lido pelo registry antes de qualquer
execução.

## Scaffold

Crie um playbook novo com:

```sh
pocket ansible init meu_playbook --category diagnostic
```

Sem `--category`, a categoria padrão é `utility`.

Use `--force` apenas para sobrescrever um scaffold existente:

```sh
pocket ansible init meu_playbook --force
```

## Categorias

| Categoria | Uso |
|---|---|
| `diagnostic` | coleta de informações, sem alterar estado |
| `setup` | instalação e configuração inicial |
| `deploy` | implantação de aplicação ou serviço |
| `maintenance` | limpeza, atualização, rotação e manutenção |
| `security` | hardening, auditoria e políticas |
| `network` | rede, firewall, DNS e conectividade |
| `utility` | suporte operacional geral |

## Safe Modes

| Categoria | Safe modes padrão |
|---|---|
| `diagnostic` | `check`, `diff` |
| `setup` | `check`, `diff`, `run` |
| `deploy` | `check`, `diff`, `run` |
| `maintenance` | `check`, `diff`, `run` |
| `security` | `check`, `diff` |
| `network` | `check`, `diff` |
| `utility` | `check`, `diff`, `run` |

Para `security` e `network`, inclua `run` manualmente só depois de revisão.

## Nomes

Use slugs em minúsculas:

```text
^[a-z0-9][a-z0-9_-]{0,79}$
```

Exemplos:

- `diagnostic_system_info`
- `setup_install_nginx`
- `deploy_static_site`

Convenção por elemento:

| Elemento | Convenção | Exemplo |
|---|---|---|
| Arquivo | `<categoria>_<acao>_<alvo>.yml` | `setup_install_nginx.yml` |
| Slug | snake_case, até 80 caracteres | `setup_install_nginx` |
| Variáveis | prefixo `pocket_<slug>_` quando expostas via `-e` | `pocket_setup_install_nginx_porta` |
| Handlers | verbo + substantivo, com `listen:` | `restart nginx` |
| Tags obrigatórias | toda task deve ter tag | `[always]`, `[install]` |

O bloco `vars:` do play principal deve deixar claro quais valores podem ser
sobrescritos pelo operador:

```yaml
vars:
  # Variáveis que podem ser sobrescritas via -e na linha de comando
  # Formato: pocket_<playbook_slug>_<variavel>
  pocket_meu_playbook_porta: 8080
```

## Idempotência

Use módulos Ansible nativos sempre que possível. Se precisar de `shell` ou
`command`, use `creates:`, `changed_when:` ou `when:`. Handlers devem ter
`listen:` para evitar duplicação de notificações.

## Exemplos

Exemplos por categoria ficam em `ansible/playbooks/examples/`. Eles são
referência de operador e não entram no registry porque o registry indexa apenas
o primeiro nível de `PLAYBOOKS_DIR`.

| Categoria | Arquivo |
|---|---|
| `diagnostic` | `diagnostic_system_info.yml` |
| `setup` | `setup_install_common_tools.yml` |
| `deploy` | `deploy_static_site_nginx.yml` |
| `maintenance` | `maintenance_rotate_logs.yml` |
| `security` | `security_audit_open_ports.yml` |
| `network` | `network_check_connectivity.yml` |
| `utility` | `utility_sync_dotfiles.yml` |
