# TUI autocomplete de envio

O autocomplete do menu fica no fluxo de conexão (`connect -> _pick_host`) porque é o ponto atual da TUI onde o usuário digita texto livre antes de enviar uma ação.

## Escopo

- Sugere host enquanto o usuário digita.
- Usa duas fontes, nesta ordem:
  - histórico local de inputs enviados anteriormente;
  - hosts disponíveis/salvos no momento.
- `Tab` aceita a sugestão atual.
- `Enter` envia o valor digitado.
- `Backspace` edita.
- `Esc` cancela.

## Estado local

O histórico fica em:

```text
~/.pocketcli/state/tui-input-history
```

O arquivo guarda somente valores sanitizados de host/input operacional (`A-Za-z0-9._-`), com limite padrão de 50 entradas. O item mais recente fica no topo. A permissão esperada do arquivo é `0600`, e o diretório `state` é ajustado para `0700` quando possível.

## Decisão de implementação

A implementação ficou no menu POSIX sh para preservar compatibilidade com Alpine/iSH e evitar uma migração maior do menu para o runtime Go. A dificuldade avaliada foi nível 3: envolve estado local, render interativo e testes, mas não exige redesenhar a arquitetura da TUI.

## Validação local

```sh
sh tests/test_tui_autocomplete.sh
```

Pré-requisitos: shell POSIX com `awk`, `sed`, `mktemp`, `stat`, `chmod`, `mkdir`, `tr` e `dirname`.
