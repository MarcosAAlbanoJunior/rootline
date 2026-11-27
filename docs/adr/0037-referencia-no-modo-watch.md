---
id: ADR-0037
title: Usar no watch, como referência, a versão mais recente com amostra que seja ancestral da nova no git
status: Aceito
date: 2026-11-26
summary: Complementa o ADR-0005 para o modo watch. A referência de cada operação é a versão mais recente do perfil, com amostra mínima nela, que seja ancestral da nova pelo git. A janela dela são até 24 h antes do deploy. Rollback não é analisado e canary fica para depois.
modules: [analysis, cmd/rootline]
---

# 0037. Referência no modo watch

## Contexto

O ADR-0005 define a referência para o `analyze`: explícita por flag, ou implícita, a versão
imediatamente anterior com amostra mínima, recuando uma versão se faltar amostra. No watch
(ADR-0035) não há quem passe flag, e o histórico de versões vem dos traces e do perfil (ADR-0040),
não de um arquivo com duas versões.

Casos que a regra precisa cobrir:
- uma operação sem amostra na versão anterior, porque ela foi substituída rápido;
- um rollback, em que a versão "nova" é mais antiga no git;
- a janela da referência: com o tráfego real, a versão anterior pode ter rodado semanas.

## Decisão

- **Referência por operação:** a versão mais recente do perfil que seja ancestral da versão nova no
  git (`IsAncestor`) e tenha a amostra mínima daquela operação. Ela pode ser diferente para cada
  operação.
- **Janela da referência:** os traces dela nas 24 h anteriores ao deploy da versão nova, ou desde o
  deploy dela, se foi mais curto. Com os traces brutos já expirados no backend, vale o perfil
  guardado (ADR-0040).
- **Rollback:** se a versão nova é ancestral da última versão analisada, ela não é analisada. O
  resumo registra o rollback.
- **Sem referência com amostra:** a operação não é comparada. Ela sai no resumo do prazo.
- **Canary** (as duas versões ativas ao mesmo tempo) segue o ADR-0005, mas fica para uma fatia
  posterior do watch.
- Toda escolha aparece no Resultado, como no ADR-0005: qual referência, por que e com quantos traces.

## Alternativas consideradas

- **Sempre a versão imediatamente anterior.** Deixaria sem comparação toda operação que não juntou
  amostra numa versão de vida curta. Perdeu.
- **A versão anterior no tempo, sem o git.** Erra no rollback e em versões de branches diferentes.
  Perdeu.
- **Toda a vida da referência como janela.** Mistura semanas de tráfego e mudanças de dados, e
  deixa a janela muito maior que a da versão nova. Perdeu.

## Consequências

- O intervalo de commits do diff é `referência..nova`, e pode cobrir vários deploys. A correlação
  já olha todos os commits do intervalo.
- O horário das janelas não é o mesmo: a da referência são as horas antes do deploy, e a da nova,
  as horas depois. A comparação de carga passa a ser por taxa (ADR-0038).

## Como validar

- Testes do domínio com um histórico falso: a operação sem amostra na anterior usa a de antes; o
  rollback não é analisado; a janela é cortada em 24 h; a escolha aparece no Resultado.
