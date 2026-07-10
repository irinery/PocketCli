# UX-02 - Eventos de entrada atrasados na TUI

Criticidade: 3 (corrigido)

O motor TUI consultava a entrada com `stty min 0 time 0`, executando `dd` seguido de `sleep` a cada ciclo sem tecla. Sob carga, os redraws podiam atrasar o consumo de `j`/`k`: a selecao visual pulava estados e a navegacao parecia perder teclas. A regressao T-13 reproduziu isso localmente e no GitHub Actions.

A leitura normal agora usa `min 0 time 1`, esperando ate 100 ms por entrada sem o ciclo anterior de `dd` mais `sleep`. Isso mantem o consumo ocioso baixo, limita a latencia de entrada e permite que `SIGWINCH` seja processado mesmo sem tecla. A sequencia de Escape usa o mesmo timeout para reconhecer setas sem travar uma tecla Escape isolada. Nenhuma fixture ou teste foi alterado.

Validacao: T-13 passou em tres execucoes consecutivas com o runtime corrigido; a suite `bash scripts/testing/visual_integrity/run_tests.sh` e o workflow Layout Gate devem passar no commit publicado.
