# UX-03 - T-13 sensivel a carga

Nivel: 3. Estado: investigacao. Complementa UX-02; nao invalida a aprovacao do CI do merge, mas limita a conclusao sobre estabilidade.

No clone limpo da main, a suite visual falhou em T-13 enquanto CI local e vet compilavam. Os demais casos passaram. Uma execucao isolada posterior passou todos os sete casos. Host: macOS 26.6.2 ARM64/Go 1.26.1.

`run_tests.sh:66-70` espera o titulo aparecer e ignora erro de readiness. T-13 usa esperas fixas de 250 ms por navegacao; o runtime faz muitas chamadas externas no desenho incremental. A evidencia atual nao distingue conclusivamente atraso de render de problema na sincronizacao do harness.

Proximo trabalho: medir latencia ate o frame completo e ate o highlight correto sob carga controlada; reduzir processos/redesenhos no runtime. So apos evidenciar a causa, considerar sincronizacao explicita do harness com estado pronto, mantendo fixtures, assercoes e budget. Aumentar sleeps ou afrouxar a fixture por si so nao fecha o item.

Validacao desta review usou os testes existentes sem modificacao. Nenhuma correcao funcional foi aplicada.
