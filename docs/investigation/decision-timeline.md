# Timeline de decisões e mudanças

Na análise executiva, abra **Timeline de decisões e mudanças** para ver os registros em ordem cronológica, com paginação. A visão usa o snapshot e a análise selecionados; abrir a timeline não inicia coleta nem envia conteúdo a um provedor externo. Ela também funciona com uma análise local sem LLM.

Cada mudança mostra o item, o executor registrado, antes/depois, evidências e uma leitura possível da intenção operacional. Mudanças de estado, projeto e estimativa, revisões, reaberturas e reversões podem sugerir replanejamento, reorganização ou retomada. Essas leituras aparecem como **hipótese de intenção**, com confiança baixa, explicação alternativa e limitações da cobertura. Evidência ausente ou sem integridade verificada mantém o motivo desconhecido. Datas ausentes ficam depois das datas conhecidas, com ordem desconhecida.

O executor não é promovido a decisor. O responsável atual pela issue também não substitui um autor ausente. Decisor e pessoa que registrou a decisão aparecem em campos próprios quando a análise os registra. Os atores usam os pseudônimos do snapshot; análises antigas sem esse mapa conservam apenas o ID registrado.

Uma justificativa só recebe o rótulo **motivo declarado na fonte** quando o método é `source` e a justificativa completa coincide com a citação e o trecho íntegro da fonte. Paráfrases, motivos produzidos pelo modelo e trechos incompletos continuam hipóteses. Não há inferência de motivação pessoal, culpa ou avaliação de desempenho individual.

O detalhe de cada caso e seu PDF detalhado usam a mesma interpretação. A timeline geral está disponível em `GET /api/v1/snapshots/{snapshot}/insights/{analysis}/timeline?limit=50&cursor=...`, com sessão local, verificação da investigação e análise previamente listada. O cursor pertence à versão da timeline, ao snapshot e à análise; o limite máximo é 200.

Os resultados reais continuam no storage privado. Não incluir históricos, evidências, relatórios ou dados da organização no Git ou em PRs. Os testes e exemplos desta funcionalidade usam apenas dados sintéticos; comandos e pré-requisitos estão em `tests/README.md`.
