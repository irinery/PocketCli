# PERF-01 - Metas de performance sem medicao por operacao

Nivel: 2. Estado: proposto, lacuna de verificacao.

[A review anterior](../../review-2026-07-03.md) define metas como help 150 ms, primeiro render 700 ms, CPU idle abaixo de 2%, ledger 10k em 500 ms e fleet plan 200 hosts em 500 ms. `tests/lib/ci.sh` mede segundos inteiros e budgets de cenarios inteiros; nao estabelece p95 por operacao, CPU/RSS idle ou crescimento de memoria. Nao foram encontrados benchmarks Go no codigo revisado.

Complemento proposto: medir help/capabilities/primeiro frame, p95 de highlight, CPU/RSS idle, ledger com 10k eventos e fleet plan com 200 hosts. Usar inventario e transportes mockados, relogio monotonico e perfis separados macOS/Linux/Alpine/iSH; registrar baseline antes de impor novos limites.

Validacao realizada: leitura dos budgets, metas e busca por benchmarks. Nao se afirma que todas as metas foram violadas; hoje nao ha evidencia automatizada suficiente para garantir que sejam cumpridas. Nenhum budget existente foi aumentado.
