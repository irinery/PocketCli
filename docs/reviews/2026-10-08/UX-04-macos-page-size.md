# UX-04 - Memoria incorreta no macOS ARM64

Nivel: 2. Estado: aberto.

`scripts/pocketcli_menu.sh:363-364` e `scripts/pocket-status.sh:69-70` multiplicam contadores de `vm_stat` por 4096. Neste Mac, a primeira linha real de `vm_stat` informa `page size of 16384 bytes`. Os valores exibidos ficam em um quarto do valor correto.

Correcao proposta: ler tamanho de pagina e contadores de uma unica coleta `vm_stat`, compartilhar o parser e manter nomenclatura precisa (paginas ativas/livres nao equivalem a total usado/total RAM). Evitar duplicar comandos em cada coleta.

Validacao realizada: comparacao da saida real do host com as constantes nos dois consumidores. Validacao necessaria: entradas de 4096 e 16384 bytes, retorno incompleto/erro e equivalencia entre menu/status. Os fixtures visuais em modo de teste usam memoria fixa e nao exercitam esse calculo.
