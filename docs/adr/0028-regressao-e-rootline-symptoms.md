---
id: ADR-0028
title: Definir Regressão por uma regra única e achar a operação que regrediu com rootline symptoms
status: Aceito
date: 2026-11-25
summary: Regressão = n ≥ 30 nas duas versões e (p50 ≥ 1,5x ou p95 ≥ 2x com +5 ms) ou erro +1 p.p. e ≥ 2x; mesma regra no analyze e no symptoms. Teto = ≥ 20% dos traces a 2% da duração máxima. O symptoms ordena por Regressão e sugere a maior razão de p50; na regressão generalizada (≥ 75%), só sugere se ela for ≥ 10x a segunda, senão lista início e sumiço como fatos. Validado nos 7 apps da rodada 2.
modules: [analysis, cmd/rootline]
---

# 0028. Definir Regressão por uma regra única e achar a operação que regrediu

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
- A sugestão é sempre rotulada como sugestão, nunca como origem.

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
- **Referência sempre pela ordem do tempo.** Mais simples, mas contradiz o ADR-0005 e erra num
  rollback. Perdeu.

## Consequências

- O `symptoms.py` do laboratório sai do fluxo.
- A mesma regra no `analyze` e no `symptoms` impede que um aponte uma operação que o outro diz que
  não regrediu.
- Na regressão generalizada sem operação que destoe, o usuário escolhe a operação olhando o início
  e o sumiço. No piloto, esses fatos apontavam a certa nos apps 04 (início) e 09 (sumiço).
- Os limiares saíram de 10 apps. Casos novos podem pedir ajuste, e isso vira ADR novo.

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
- A mesma conferência roda contra a implementação, nos traces do laboratório, antes de o lote ser
  declarado pronto.
