---
id: ADR-0022
title: Declarar distratores no cenário e contar o falso positivo de detector pelas candidatas que casam com eles
status: Aceito
date: 2026-11-23
summary: O scenario.json declara distratores (função, arquivo e marcador rootline:distractor). O gerador rastreia a linha até o commit ruim com git blame. O falso positivo de detector conta os cenários em que alguma candidata casa com um distrator (arquivo:linha no nível 1, função no nível 2), e o portão do placar reprova quando ele sobe.
modules: [eval, fixture, evaluation, ci]
---

# 0022. Declarar distratores no cenário e contar o falso positivo de detector

## Contexto

O ADR-0009 define a métrica "Falso positivo de detector": o detector classifica errado um distrator,
por exemplo paginação como N+1. O ADR-0004 prevê distratores nos cenários. Até a slice 3b, nenhum
cenário tinha distrator, e a pontuação (`ScoreOne`) só olhava a causa do Resultado. As outras
candidatas não entravam em métrica nenhuma.

O primeiro distrator é uma paginação em lotes pequenos (ADR-0021). Ele precisa ficar na mesma rota
da causa, porque o `score` analisa só a rota do sintoma. Também precisa existir antes da regressão,
porque é código pré-existente que o detector confunde, não parte do bug. Isso traz três problemas:

- O gabarito marca a causa com o comentário `// rootline:answer`, removido antes do commit. O
  distrator precisa de uma marca equivalente, que também não pode vazar para os commits: os modos
  com LLM leem o código pelo `get_code`.
- O distrator nasce no commit `referencia` (patch de preparação), e a regressão pode editar a mesma
  função. A linha do distrator muda até o commit `regressao`, onde o Resultado localiza as
  candidatas.
- Causa e distrator podem estar na mesma função. Casar a candidata com o distrator só pela função
  marcaria a própria causa como falso positivo.

## Decisão

**Declaração.** O `scenario.json` ganha `distractors`, uma lista de
`{function, file, marker}`, com o marcador `" // rootline:distractor"` no fim da linha de código.
A linha marcada segue a mesma convenção do gabarito: é a linha que a localização de nível 1 aponta
para aquela query (a chamada dentro do loop). Um cenário sem `distractors` não muda.

**Linha no commit ruim.** O gerador remove o marcador antes do commit em que ele aparece e anota a
linha. Em cada commit seguinte, até o commit ruim do sintoma, acha a linha nova com
`git blame --porcelain`: a linha cuja origem é o commit e a linha anotados. O `answer_key.json`
ganha `distractors: [{function, file, line}]`, com a linha no commit ruim. Se um commit posterior
editar a própria linha do distrator, a geração falha: o distrator deixou de ser código
pré-existente. Os patches seguintes são escritos sobre o código já sem marcador.

**Sem vazamento.** O `assertNoLeak` passa a procurar o prefixo `rootline:` em todo commit, o que
cobre os dois marcadores e os que vierem depois.

**Casamento por nível.** Uma candidata do Resultado (a causa e as demais) casa com um distrator
quando:

- tem localização de nível 1 com o arquivo e a linha do distrator; ou
- tem localização de nível 2 (sem linha) com a função do distrator.

Uma candidata que casa com o gabarito, pela mesma regra de nível, nunca conta como falso positivo.
Uma candidata sem localização não é atribuída a nenhum distrator.

**Métrica.** Por cenário com distrator, `false_positive` é `true` quando alguma candidata casa com
algum distrator. No agregado, `detector_false_positive` é `{hits, total}`: `hits` conta os cenários
com falso positivo e `total` conta os cenários com distrator. O campo por cenário é omitido nos
cenários sem distrator, para que o placar deles não mude. O relatório imprime a métrica como as
outras, com denominador (ADR-0009), por exemplo `Falso positivo de detector: 0/1`.

**Portão.** Para esta métrica, menos é melhor. O portão do placar reprova quando um cenário passa de
`false_positive: false` para `true`, ou, num placar sem cenários, quando `hits` sobe com o mesmo
`total`. As outras métricas continuam reprovando quando caem.

## Alternativas consideradas

- **Cenário só com o distrator e gabarito "nenhuma causa".** Isola melhor o distrator, mas obriga a
  definir "causa nenhuma" no gabarito e no score, o que encosta em Alucinação e Abstenção indevida
  (ADR-0009), métricas que o modo só-detectores ainda não tem. Perdeu; fica para quando essas
  métricas entrarem.
- **Casar só por arquivo:linha.** É igual à métrica Linha, mas um distrator localizado no nível 2
  escaparia da métrica. Perdeu.
- **Casar por função em qualquer nível.** Com causa e distrator na mesma função, a causa contaria
  como falso positivo. Perdeu para o casamento por nível.
- **Casar pela query normalizada.** Independe da localização, mas duplica o SQL no `scenario.json` e
  quebra quando a normalização muda. Perdeu.
- **Repetir o marcador no patch de regressão.** Dispensa o blame, mas o autor do cenário precisa
  lembrar de reposicionar o marcador, e o patch passa a tocar uma linha que não mudou. Perdeu.
- **Distrator num arquivo que a regressão não toca.** A linha não mudaria, mas a regra limitaria os
  cenários futuros. Perdeu.
- **Métrica invertida (`distractor_ignored`, mais é melhor).** Manteria o portão como está, mas o
  nome foge do ADR-0009 e o relatório precisaria inverter o número. Perdeu.

## Consequências

- O placar passa a medir falso positivo de detector, e a pontuação passa a olhar todas as candidatas,
  não só a causa.
- Um cenário pode ter causa e distrator na mesma função. No nível 1, a linha os distingue.
- Risco: com causa e distrator na mesma função e as duas candidatas no nível 2, as duas casam com o
  gabarito, e o falso positivo não é contado. O caso exige que o nível 1 falhe para as duas.
- Risco: uma candidata sem localização nenhuma que seja o distrator não é contada. Se acontecer, o PR
  que regravar o placar registra o caso.
- O gerador passa a depender de `git blame`, que já vem com o `git` que ele usa.
- O placar ganha um campo agregado novo. A primeira gravação com distrator exige `--update`, com
  justificativa no PR.

## Como validar

- Testes do gerador: o marcador do distrator é removido no commit em que aparece; a linha no commit
  ruim acompanha linhas inseridas antes dela pela regressão; a geração falha se a regressão editar a
  linha do distrator; nenhum commit contém `rootline:`.
- Testes da pontuação: candidata de nível 1 na linha do distrator conta; de nível 1 em outra linha da
  mesma função não conta; de nível 2 com a função do distrator conta; a que casa com o gabarito não
  conta; sem localização não conta; cenário sem distrator fica fora do denominador.
- Testes do portão: `false_positive` que passa de `false` para `true` reprova; de `true` para `false`
  não reprova.
- Avaliação: o cenário com distrator de paginação mostra o falso positivo antes da regra do ADR-0021
  e 0 depois dela.
