# PocketCli — Plano de Implementação por Fases

Este plano prioriza evolução incremental, mínima pegada de recursos (iSH/Alpine/~1GB RAM) e redução de retrabalho.

## Princípios obrigatórios (não negociáveis)

Aplicar em todas as fases:

- Binário único (Go)
- Sem serviços em background no Viewer
- SSH como transporte universal
- Tailscale via CLI (não SDK)
- UI só renderiza (sem lógica pesada)
- Tudo opcional/lazy

## Critérios de arquitetura (transversais)

1. **Configuração centralizada em `profile/`**
   - Toda personalização de usuário deve residir em `profile/`.
   - Arquivos fora de `profile/` devem ser padrão compartilhado ou apenas referenciar `profile/` por constante/variável/ID central.
2. **Atualização resiliente (`pocket update`)**
   - O fluxo de atualização deve continuar funcionando mesmo com diferenças locais relevantes.
   - Detecção de drift local com preservação explícita de customizações.
3. **Execução leve por padrão**
   - Feature flags simples e carregamento sob demanda.
   - Evitar inicialização de componentes não utilizados.

## Fase 0 — Fundação de produto (curto prazo)

**Objetivo:** estabilizar base atual e preparar transição para o binário Go sem quebrar fluxos existentes.

### Entregas

- Definir contrato de comandos (`pocket`, `pocket-menu`, `pocket-radar`, `pocket-update`) e mapear comportamento atual.
- Especificar estrutura de configuração única:
  - `profile/` como fonte canônica de customização.
  - Chave central para referências externas (ex.: `POCKET_PROFILE_DIR`).
- Formalizar política de update com arquivos divergentes:
  - merge seguro;
  - backup automático;
  - relatório final de mudanças aplicadas/preservadas.
- Criar suíte mínima de testes de regressão para instalação/update/menu em ambientes limitados.

### Critério de pronto

- Documentação de contratos publicada.
- Testes atuais cobrindo regressões críticas de bootstrap e update.
- Nenhuma nova personalização fora de `profile/`.

## Fase 1 — Núcleo Go mínimo (MVP funcional)

**Objetivo:** introduzir binário único Go mantendo shell scripts como fallback temporário.

### Entregas

- Criar `cmd/pocketcli` em Go com subcomandos principais:
  - `install` (orquestração)
  - `menu` (renderização simples + delegação)
  - `radar` (consulta Tailscale CLI)
  - `update` (pipeline de atualização resiliente)
- Adaptador de compatibilidade:
  - wrappers atuais chamam o binário quando disponível.
- Camada de execução enxuta:
  - invoca `ssh`, `tailscale`, `tmux` por processo externo.
  - sem daemon local no modo Viewer.
- Telemetria local opcional e desligada por padrão (logs texto rotacionados).

### Critério de pronto

- Fluxos críticos funcionam via binário Go em Linux/Alpine/iSH.
- Viewer continua sem serviço residente.
- `pocket update` preserva divergências locais de forma previsível.

## Fase 2 — UX operacional leve (menu/radar)

**Objetivo:** melhorar usabilidade sem aumentar custo de execução.

### Entregas

- Menu TUI com renderização desacoplada da lógica de negócio.
- Camada de “providers” lazy:
  - status SSH,
  - status Tailscale,
  - sessão tmux.
- Cache efêmero em arquivo temporário para acelerar listagens de hosts.
- Modo degradado explícito para dependências ausentes (ex.: sem `fzf`, sem `jq`).

### Critério de pronto

- Menu responde rapidamente em ambiente ~1GB RAM.
- Sem regressão em fallback quando ferramentas não existem.

## Fase 3 — Fleet básico e operações remotas

**Objetivo:** ampliar alcance operacional mantendo modelo SSH-first.

### Entregas

- Comandos de conexão por host/grupo:
  - `pocket connect <host>`
  - `pocket fleet <grupo>`
- Inventário leve baseado em arquivo (sem banco e sem serviço).
- Execução paralela controlada por limite de concorrência configurável.
- Estratégia de retry simples para falhas de rede transitórias.

### Critério de pronto

- Operações em múltiplos hosts com consumo controlado.
- Logs por host e sumário final legível.

## Fase 4 — Hardening e distribuição

**Objetivo:** tornar releases confiáveis e fáceis de adotar.

### Entregas

- Pipeline de release com binários estáticos para alvos principais.
- Checksum assinado e instruções de verificação.
- Matriz de testes de compatibilidade (Debian/Ubuntu, Alpine, macOS, WSL, iSH).
- Política de versionamento semântico e changelog automático.

### Critério de pronto

- Instalação/atualização reproduzível por versão.
- Rollback documentado e testado.

## Sequência recomendada (execução)

1. Fase 0 (estabilização + contratos)
2. Fase 1 (núcleo Go + compatibilidade)
3. Fase 2 (UX leve)
4. Fase 3 (fleet)
5. Fase 4 (hardening)

## Métricas de sucesso

- **Tempo de bootstrap** em ambiente limitado dentro de meta definida pela equipe.
- **Taxa de sucesso do update** com arquivos locais divergentes > 95% em testes.
- **Consumo de memória no Viewer** estável e sem processos residentes extras.
- **Tempo de resposta do menu** consistente mesmo em iSH.

## Riscos e mitigação

- **Risco:** regressão ao migrar scripts para Go.
  - **Mitigação:** wrappers de compatibilidade + testes de regressão por comando.
- **Risco:** quebra de customizações existentes.
  - **Mitigação:** centralização em `profile/` + dry-run de update + backups.
- **Risco:** aumento de complexidade no menu.
  - **Mitigação:** UI estritamente de renderização, lógica em serviços simples e lazy.
