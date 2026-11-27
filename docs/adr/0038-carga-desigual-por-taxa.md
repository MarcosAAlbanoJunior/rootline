---
id: ADR-0038
title: Julgar a Carga desigual pela razão de taxa de traces por minuto, e não pela razão de n
status: Aceito
date: 2026-11-26
summary: Substitui o 0032 e mantém o resto dele sem mudança (Regressão, Causa sem Regressão, teto, sumiço, symptoms, regressão generalizada, operação sugerida). A Carga desigual passa a usar a mediana da razão de taxa (traces por minuto da janela de cada versão), na mesma faixa [0,8; 1,25]. Com janelas de mesma duração, é igual à razão de n.
modules: [analysis, cmd/rootline]
---

# 0038. Julgar a Carga desigual pela razão de taxa

## Contexto

O ADR-0032 (que manteve a regra do ADR-0030) julga a Carga desigual pela mediana da razão de n entre
as operações com amostra mínima: fora de [0,8; 1,25], as duas gravações não tiveram carga
comparável, e o `symptoms` não sugere operação.

A razão de n só mede carga quando as duas janelas têm a mesma duração. É o caso das gravações do
placar e do piloto, em que cada versão recebe a mesma carga pelo mesmo tempo. No modo watch
(ADR-0035) não é: a janela da referência são até 24 h antes do deploy (ADR-0037), e a da versão nova
fecha quando a operação junta a amostra, o que pode levar minutos. Com a razão de n, quase toda
comparação do watch daria Carga desigual sem que a carga tivesse mudado.

## Decisão

**Muda: Carga desigual.** A taxa de uma operação numa versão é o número de traces dela dividido pela
duração da janela daquela versão, em traces por minuto. A janela vai do primeiro ao último trace da
versão na fonte (arquivo ou backend), ou é a janela da consulta no watch. A razão de taxa mediana é a
mediana da razão de taxa entre as operações com amostra mínima. Com pelo menos 3 dessas operações,
quando ela fica fora de [0,8; 1,25], as duas versões não tiveram carga comparável: o `symptoms` não
sugere operação nem imprime o `analyze` pronto, diz por quê, com a razão de taxa mediana, e mantém o
início e o sumiço na tabela. Com menos de 3 operações, a carga não é julgada.

Com head sampling (ADR-0034) na mesma taxa nas duas versões, a razão de taxa continua comparável.

O sumiço continua relatado pela razão de n, porque ele fala de requisições que não viraram span, e
não de carga.

**Mantido do ADR-0032, sem mudança:**

**Regressão.** Uma operação de entrada regrediu entre a referência e a versão ruim quando as duas
versões têm pelo menos 30 traces dela e:
- **latência**, em qualquer um dos três ramos:
  - p50 ≥ 1,5x, com pelo menos 5 ms a mais;
  - **p50 ≥ 2x, com pelo menos 2 ms a mais e o p50 da versão ruim em pelo menos 3 ms** (novo);
  - p95 ≥ 2x, com pelo menos 5 ms a mais;
- **erro:** a taxa de erro sobe pelo menos 1 ponto percentual e pelo menos dobra.

O ramo novo exige a mesma razão do p95 (2x) e deixa intacta a faixa de 1,5x a 2x, onde fica quase
todo o ruído. O piso de 2 ms saiu destas gravações, não de princípio: fica cerca de 3x acima do maior
ruído visto e abaixo do menor alvo (2,87 ms).

O mínimo de 3 ms no p50 da versão ruim deixa de fora a operação que continua extremamente rápida
depois de piorar. Sem ele, um health check que vai de 0,6 para 2,7 ms sob contenção (4,5x, +2,1 ms)
regrediria, e a sugestão do `symptoms`, que fica com a maior razão de p50, poderia apontá-lo no lugar
da operação que ficou lenta: abaixo de 1 ms, qualquer oscilação vira uma razão grande. O mínimo fica
abaixo do menor alvo (3,74 ms, app-03-guia). O corte vai no p50 da versão ruim, não no da referência:
o app-03-guia parte de 0,87 ms e é um N+1 real.

O tipo (latência, erro ou os dois) acompanha a Regressão. A regra é uma função só do domínio, usada
pela Verificação `symptom_regression` (ADR-0027), pelo `rootline symptoms` e como pré-condição do
detector de forma (ADR-0031). Os limiares são constantes, sem flag. O `analyze` continua analisando
só latência (ADR-0005), e a Regressão de erro só é relatada.

**Causa numa operação sem Regressão.**
- O status e a confiança não mudam. A confiança de uma Candidata mede a evidência dela, não a
  latência, e a Causa pode estar certa numa operação que só piorou um pouco.
- Sem Regressão de latência, a `symptom_regression` diz qual limiar faltou em cada ramo, com a
  razão e a diferença. Por exemplo: `p50 1.30x (+2.42 ms): under 1.5x and under 2x; p95 0.48x (-15.96 ms): under 2x`.
- Quando a `symptom_regression` rodou, não há Regressão de latência e há Causa, o Resultado ganha um
  aviso em `warnings`. O aviso diz que os detectores leem a forma dos traces, não a latência: a causa
  está no código da operação, mas pode não explicar a lentidão procurada, e o `rootline symptoms`
  mostra se outra operação regrediu.
- Sem referência ou sem amostra mínima, não há aviso: a `symptom_regression` já diz que não julgou.
- O `schema_version` não sobe, porque são só extensões.

**Teto.** A versão ruim de uma operação tem um teto quando pelo menos 20% dos traces dela ficam a
2% da duração máxima da operação naquela versão. Esses traces provavelmente foram cortados por um
timeout.

**Sumiço.** O Rootline não infere requisições que não viraram span. Ele relata a razão de n entre
as versões e a destaca abaixo de 50%, dizendo que pode ser requisição presa ou carga diferente.

**`rootline symptoms`.**
```
rootline symptoms --traces <file.jsonl> [--service <name>] [--repo <dir>]
                  [--baseline-version <sha>] [--bad-version <sha>] [--format text|json]
```

Inferência quando faltam flags:
- **Serviço:** com um serviço só no arquivo, ele é usado.
- **Versões:** com exatamente duas versões, a referência é a anterior no histórico do git quando
  `--repo` é dado, como na referência implícita do ADR-0005. Sem `--repo`, é a que aparece primeiro
  no tempo, e a saída avisa que a ordem veio do tempo.
- **Ambiguidade:** com mais de duas versões, ou sem `service.version`, o comando falha, listando as
  versões com o n de traces e a faixa de tempo de cada uma.

Saída, uma linha por operação de entrada: n de cada versão e a razão de n; p50 e p95 de cada versão;
taxa de erro de cada versão; tipo da Regressão; marca de teto; início da regressão. As operações com
Regressão vêm primeiro, pela maior razão de p50. Quando há operação sugerida, sai no fim um comando
`rootline analyze …` pronto, com ela e as versões.

**Regressão generalizada.** Quando pelo menos 75% das operações com amostra mínima regridem, a
saída diz que isso costuma indicar um recurso compartilhado saturado (pool, lock, CPU).

**Operação sugerida.**
- **Fora da regressão generalizada:** a sugerida é a de maior razão de p50.
- **Na regressão generalizada:** só há sugestão quando uma operação destoa, isto é, quando a maior
  razão de p50 é pelo menos 10x a segunda maior.
- **Sem operação que destoe:** não há sugestão nem comando pronto. A tabela traz, por operação, o
  **início da regressão** (o momento do 3º trace da versão ruim acima da p95 da referência, contado
  do primeiro trace da versão ruim) e o **sumiço** (a razão de n).
- **Com Carga desigual, nunca há sugestão.**
- A sugestão é sempre rotulada como sugestão, nunca como origem.

**Abaixo da amostra mínima.** Uma operação com amostra mínima na referência, mas não na versão
ruim, continua na lista, marcada com o sumiço. Ela não concorre à Regressão nem à sugestão.

## Alternativas consideradas

- **Manter a razão de n e igualar as janelas no watch.** Exigiria cortar a janela da referência na
  duração da janela nova, jogando fora traces e piorando a estabilidade dos percentis da
  referência. Perdeu.
- **Julgar a carga só no modo arquivo.** Deixaria o watch sem nenhuma proteção contra comparar um
  pico de tráfego com um vale. Perdeu.
- **Taxa por operação contra o total do serviço.** Mediria a mistura de rotas, não a carga. Perdeu.

## Consequências

- Nas gravações de carga fixa, a razão de taxa é igual à razão de n, a menos da diferença de
  duração das janelas, e o resultado do `symptoms` não muda.
- No watch, a Carga desigual passa a significar tráfego diferente entre antes e depois do deploy,
  que é o que ela quer dizer.
- Uma janela muito curta (poucos segundos) deixa a taxa instável. A amostra mínima de 30 traces
  limita esse efeito.
- Tudo o mais que o ADR-0032 decidiu continua valendo.

## Como validar

- Testes do domínio: janelas de mesma duração dão a mesma decisão que a razão de n; janelas de
  durações diferentes com a mesma taxa não dão Carga desigual; a razão de taxa mediana aparece na
  saída.
- Gravações guardadas: o `symptoms` nas 19 gravações das rodadas 2 e 3 dá as mesmas sugestões e
  as mesmas marcas de Carga desigual que com a razão de n.
- Placar: `rootline-eval score` sem `--update` igual ao `eval/placar.json`, e o gate
  (`scripts/check.sh`).
