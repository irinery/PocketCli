# SEC-08 - Entrypoints remotos legados

Nivel: 4. Estado: aberto. Validado por leitura do commit e rastreamento de chamadas; nenhuma conexao remota foi executada.

`bin/pocketcli:34` executa `ssh "$HOST"` diretamente. `provision/pocketcli-provision-host.sh:12` tambem usa SSH direto e executa instaladores remotos. `radar/pocketcli-bootstrap.sh:24,32,35,41` usa SSH/SCP sem passar pelos wrappers seguros. O menu legado ainda referencia esses scripts.

Os hardenings de `pocket`, menu principal e fleet nao alcancam essas entradas: policy, aprovacoes e auditoria podem ser ignoradas. Provisionamento tambem recebe host sem validacao contra operandos que comecem por `-`.

Correcao proposta: apos mapear compatibilidade, desativar/delegar o menu legado e unificar provisionamento/radar no executor adequado. Preservar stdin de scripts e contrato de transferencia, rejeitar hosts option-like e garantir audit em sucesso/falha.

Validacao necessaria: invocacoes por cada entrada com mocks de transporte; ausencia de backend seguro deve impedir execucao; stdin, host keys, argumentos e audit devem ser verificados. Nenhum teste foi alterado nesta review.
