---
id: ADR-0030
title: Definir Regressão por uma regra única e só sugerir operação no rootline symptoms com carga comparável
status: Aceito
date: 2026-11-25
summary: Substitui o 0028 e mantém a regra dele (Regressão, teto, sumiço, sugestão, regressão generalizada). Acrescenta a Carga desigual: com 3 ou mais operações e razão de n mediana fora de [0,8; 1,25], o symptoms não sugere operação. Validado em 18 gravações das rodadas 2 e 3.
modules: [analysis, cmd/rootline]
---

# 0030. Definir Regressão por uma regra única e só sugerir operação com carga comparável

## Contexto

O `analyze` exige a operação de entrada e as versões (ADR-0005). No piloto em apps reais, achar a
operação que piorou só foi possível com um script externo (`symptoms.py`), e três casos ficaram
difíceis:

- **Todas as operações regridem juntas** quando um pool ou um lock satura (app-04, app-09). A
  maior razão de p50 não diz nada, porque todas batem no mesmo timeout.
- **Ruído em operações muito rápidas:** GETs abaixo de 1 ms aparecem 1,8x mais lentos (app-08).
- **O trace mostra menos do que aconteceu:**
  - traces cortados num timeout de 10 ou 15 s, às vezes sem status de erro no servidor (app-04,
    app-06);
  - requisições presas que nunca viram span: 27 spans para cerca de 1.274 requisições (app-09).

As regressões reais do piloto foram de 3x a 6000x na p50. O ruído ficou abaixo de 1,8x ou abaixo
de 1 ms.

Este ADR substitui o ADR-0028, que definiu a regra na rodada 2. Na rodada 3 (teste com usuário
cego, #60), os 10 apps gravaram em paralelo, com a máquina a load average 36:
- No app-01, a versão ruim gravou cerca de metade dos traces de todas as operações (razão de n de
  0,47 a 0,57, mediana 0,51). O p95 da referência no `POST /v1/devices/{id}/readings` já estava
  em 2,5 s.
- O `symptoms` sugeriu esse `POST` (p50 6,93x, marcado com sumiço) no lugar da operação certa
  (5,74x).
- Gravado com a máquina parada, o mesmo app teve razão de n 1,00 em todas as operações e sugestão
  certa.

Quando a carga das duas versões difere, a razão de p50 de cada operação mistura a regressão com a
diferença de carga, e a ordem entre as operações deixa de dizer qual piorou.

## Decisão

**Regressão.** Uma operação de entrada regrediu entre a referência e a versão ruim quando as duas
versões têm pelo menos 30 traces dela e:
- **latência:** p50 ≥ 1,5x ou p95 ≥ 2x, com diferença absoluta de pelo menos 5 ms; ou
- **erro:** a taxa de erro sobe pelo menos 1 ponto percentual e pelo menos dobra.

O tipo (latência, erro ou os dois) acompanha a Regressão. A regra é uma função só do domínio, usada
pela Verificação `symptom_regression` (ADR-0027) e pelo `rootline symptoms`. Os limiares são
constantes, sem flag por enquanto. O `analyze` continua analisando só latência (ADR-0005), e a
Regressão de erro só é relatada.

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

Saída, uma linha por operação de entrada:
- n de cada versão e a razão de n;
- p50 e p95 de cada versão;
- taxa de erro de cada versão;
- tipo da Regressão;
- marca de teto;
- início da regressão.

As operações com Regressão vêm primeiro, pela maior razão de p50. Quando há operação sugerida,
sai no fim um comando `rootline analyze …` pronto, com ela e as versões.

**Regressão generalizada.** Quando pelo menos 75% das operações com amostra mínima regridem, a
saída diz que isso costuma indicar um recurso compartilhado saturado (pool, lock, CPU).

**Operação sugerida.**
- **Fora da regressão generalizada:** a sugerida é a de maior razão de p50.
- **Na regressão generalizada:** só há sugestão quando uma operação destoa, isto é, quando a maior
  razão de p50 é pelo menos 10x a segunda maior.
- **Sem operação que destoe:** não há sugestão nem comando pronto. A tabela ganha dois fatos por
  operação, sem o Rootline escolher:
  - o **início da regressão**: o momento do 3º trace da versão ruim acima da p95 da referência,
    contado do primeiro trace da versão ruim;
  - o **sumiço**: a razão de n.
- **Com Carga desigual, nunca há sugestão** (veja abaixo).
- A sugestão é sempre rotulada como sugestão, nunca como origem.

**Carga desigual.** A razão de n mediana é a mediana da razão de n entre as operações com amostra
mínima. Com pelo menos 3 dessas operações, quando ela fica fora de [0,8; 1,25], as duas gravações
não tiveram carga comparável:
- o `symptoms` não sugere operação nem imprime o `analyze` pronto;
- ele diz por quê, com a razão de n mediana;
- a tabela continua com o início e o sumiço como fatos.

Com menos de 3 operações, a carga não é julgada: com duas, uma operação regredida que perdeu traces
para requisições presas decidiria a mediana sozinha. A Carga desigual não distingue requisição presa
de carga diferente. As duas tornam a comparação
entre operações inútil para sugerir.

**Abaixo da amostra mínima.** Uma operação com amostra mínima na referência, mas não na versão
ruim, continua na lista, marcada com o sumiço. Ela não concorre à Regressão nem à sugestão.

## Alternativas consideradas

- **Só razão de p50, sem piso absoluto nem amostra mínima.** Mais simples, mas reproduz o ruído do
  app-08. Perdeu.
- **Limiares como flags desde já.** Mais flexível, mas sem dado para escolher outros valores, e
  cada flag vira contrato. Ficam constantes até um caso real pedir. Perdeu por enquanto.
- **Inferir a origem na regressão generalizada (pool, lock) e chamá-la de causa.** Seria um
  detector sem evidência suficiente e ameaça o zero falso positivo do piloto. Perdeu.
- **Sugerir pela operação que começou primeiro.** Em janelas de 5 s, todas as operações do app-04
  começaram na janela 0. Pelo 3º trace lento, acerta os apps 04 e 06, mas sugere o heartbeat no
  lugar do claim no app-09. Uma sugestão errada custa mais que nenhuma. Perdeu, e o início ficou
  como fato na tabela.
- **Sugerir pelo maior sumiço.** Destaca o claim no app-09, mas empata com outra operação de 8
  traces e não diz nada no app-04, onde todas caem para 12%. Perdeu, e o sumiço ficou como fato.
- **Não sugerir uma operação marcada com sumiço.** Corrige o app-01 em paralelo, mas por acaso:
  a sugerida estava com razão de n 0,47, e a certa com 0,51, logo acima da marca de 50%. Ruído um
  pouco menor teria sugerido errado do mesmo jeito. Perdeu.
- **Pular as operações com sumiço e sugerir a próxima.** No app-01, acertaria por acaso, pelo
  mesmo motivo. Perdeu.
- **Referência sempre pela ordem do tempo.** Mais simples, mas contradiz o ADR-0005 e erra num
  rollback. Perdeu.

## Consequências

- O `symptoms.py` do laboratório sai do fluxo.
- A mesma regra no `analyze` e no `symptoms` impede que um aponte uma operação que o outro diz que
  não regrediu.
- Na regressão generalizada sem operação que destoe, o usuário escolhe a operação olhando o início
  e o sumiço. No piloto, esses fatos apontavam a certa nos apps 04 (início) e 09 (sumiço).
- Os limiares saíram de 10 apps. Casos novos podem pedir ajuste, e isso vira ADR novo.
- Com Carga desigual, o usuário fica sem sugestão mesmo quando a regressão é real. Isso inclui um
  pool saturado fora da regressão generalizada. Uma sugestão errada custa mais que nenhuma.
- A faixa [0,8; 1,25] é larga para uma carga de taxa fixa e estreita para uma carga por
  concorrência fixa com regressão grande. Os geradores do piloto eram de taxa fixa.

## Como validar

- Testes unitários:
  - da regra de Regressão;
  - do teto e do sumiço;
  - da inferência de versões;
  - da regressão generalizada, com e sem operação que destoe.
- Validação nos traces da rodada 2 do piloto (2026-11-25, protótipo da regra; os traces da rodada 1
  não foram guardados):
  - a operação do gabarito foi marcada como Regressão nos 7 apps;
  - nenhum GET abaixo de 1 ms foi marcado (app-08: até 1,95x, com menos de 5 ms de diferença);
  - as outras operações marcadas são vazamentos reais citados nos gabaritos (contenção no app-06,
    CPU no app-10);
  - a sugestão acertou nos 5 apps em que houve sugestão (05, 06, 07, 08, 10) e ficou ausente nos 2
    em que não havia operação que destoasse (04, 09). Nenhuma sugestão errada.
- Validação nas 18 gravações guardadas das rodadas 2 e 3 (7 da rodada 2, 11 da rodada 3):
  - razão de n mediana de 0,99 a 1,13 em todas as gravações com sugestão certa ou sem sugestão por
    falta de Regressão;
  - 0,51 no app-01 em paralelo, que passa a ficar sem sugestão;
  - de 0,12 a 0,26 nas regressões generalizadas dos apps 04 e 09, que já ficavam sem sugestão.
    O app-09 da rodada 2 tem mediana 0,99, porque lá as requisições presas nem viraram span.
- A mesma conferência roda contra a implementação, nessas gravações: o app-01 em paralelo fica
  sem sugestão, e as outras 17 mantêm a sugestão de antes.
