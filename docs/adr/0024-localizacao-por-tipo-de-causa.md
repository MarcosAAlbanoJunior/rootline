---
id: ADR-0024
title: Localizar cada candidata pela estratégia do tipo de causa
status: Aceito
date: 2026-11-23
summary: Complementa o ADR-0017. Cada detector escolhe a estratégia de localização de nível 1. O N+1 usa o primeiro frame em loop, com loop_not_found quando falta. O índice faltando usa o frame mais interno da aplicação, onde a query é emitida, sem regra do loop nem loop_not_found. O nível 2 vale para todos.
modules: [detector, analysis]
---

# 0024. Localizar cada candidata pela estratégia do tipo de causa

## Contexto

O ADR-0002 define a localização de nível 1 pela `rootline.app_stack`. O índice escolhe o primeiro
frame cujo ponto de chamada está dentro de um `for`/`range`. O ADR-0017 completa a regra: sem frame
em loop, a localização fica no frame mais interno resolvível, com `in_loop: false` e o aviso
`loop_not_found`.

Essa regra foi feita para o N+1, em que o defeito está no loop que repete a query. No índice
faltando (ADR-0023), a query é emitida uma vez, e o defeito está no plano dela. O conserto fica no
schema ou na própria query, que mora no frame mais interno, em geral o método do repository que
chama o driver. Aplicar a regra do loop a essa candidata a deixaria no mesmo frame, mas com um
`loop_not_found` em toda candidata, um aviso sem sentido para o tipo.

## Decisão

**Cada detector escolhe a estratégia de localização de nível 1 da sua candidata.** No código, é uma
estratégia por tipo de detector, não uma flag booleana no localizador.

- **N+1:** a regra dos ADRs 0002 e 0017, sem mudança. Primeiro frame em loop. Sem ele, o frame mais
  interno resolvível, com `in_loop: false` e `loop_not_found`.
- **Índice faltando:** o frame mais interno resolvível da aplicação, que é a linha onde a query é
  emitida. Não há regra do loop, `in_loop` não é preenchido (nada foi verificado, como no nível 2),
  e não há `loop_not_found`.

**O nível 2 vale para todos os tipos.** Sem localização de nível 1 (sem pilha, caminho que não
resolve), a análise cai para rota → handler, como no ADR-0014.

**O gabarito segue a mesma convenção.** A linha marcada com `// rootline:answer` ou
`// rootline:distractor` é a que a estratégia do tipo aponta. No índice faltando, é a linha da
chamada que emite a query. Nos cenários, essa chamada fica numa linha só, porque o runtime do Go pode
reportar outra linha numa chamada de várias linhas.

## Alternativas consideradas

- **Uma regra só, a do loop, para todos os tipos.** Daria a mesma linha no índice faltando, mas com
  `loop_not_found` em toda candidata, e o aviso perderia o valor de sinal no N+1. Perdeu.
- **Uma flag booleana no localizador** ("exigir loop"). Resolve dois tipos, mas os próximos (lock,
  chamada lenta em loop) podem pedir outras regras, e a flag viraria um conjunto de ifs. Perdeu para
  a estratégia por tipo.
- **Localizar o índice faltando no service, que chama o repository.** Fica mais perto da regra de
  negócio, mas longe do conserto: a query e o filtro moram no repository. Perdeu.

## Consequências

- O localizador passa a receber a estratégia do detector. A regra do N+1 não muda, e o placar dos
  cenários de N+1 tem de continuar idêntico.
- Quem consome o Resultado precisa saber que `in_loop` só existe nas candidatas cuja estratégia
  verifica o loop.
- Um tipo novo de causa precisa declarar a sua estratégia. Não há padrão implícito.

## Como validar

- Teste unitário: a mesma pilha, sem frame em loop, dá `loop_not_found` na estratégia do N+1 e não
  dá aviso na do índice faltando. As duas apontam o frame mais interno.
- Teste unitário: com um frame em loop acima do mais interno, o N+1 aponta o frame em loop e o
  índice faltando aponta o mais interno.
- Avaliação: no cenário de índice faltando, a métrica Linha acerta com nível 1 na linha da chamada
  do repository.
