---
id: ADR-0036
title: Publicar o N+1 antes da amostra mínima só quando ele é novo na versão e tocado por um commit do deploy
status: Aceito
date: 2026-11-26
summary: O watch publica um Sinal antecipado quando o N+1 aparece em pelo menos 3 traces completos da versão nova, não existe na referência e o local foi tocado por um commit do intervalo. Ele sai como não confirmado; com a amostra, sai uma segunda mensagem que confirma a Regressão ou mantém o N+1 como Causa sem Regressão. Não é retirado.
modules: [analysis, cmd/rootline]
---

# 0036. Publicar o N+1 antes da amostra mínima

## Contexto

A Regressão precisa de 30 traces em cada versão (ADR-0032). Numa operação rara, isso pode levar
horas depois do deploy. O N+1 é diferente: ele aparece num trace só, porque é a mesma query repetida
dentro de um loop, com a linha vinda do hook. Hoje ele é o único sinal estrutural. O trabalho a mais
por requisição (ADR-0031) compara a presença da dependência nas duas versões e precisa de amostra.

Avisar cedo tem dois riscos:
- **Culpar o deploy por um N+1 antigo.** Um N+1 que já existia na versão anterior aparece no
  primeiro trace da versão nova sem que o deploy tenha culpa.
- **Avisar algo que não pesa.** Um N+1 com 2 itens por requisição pode não piorar a latência. O
  ADR-0032 já trata esse caso como Causa sem Regressão, com aviso.

## Decisão

O watch publica um **Sinal antecipado** de N+1 numa operação da versão nova, antes da amostra
mínima, quando as três condições valem:
1. o N+1 aparece em pelo menos 3 traces completos da versão nova;
2. ele não existe na referência: nenhuma Candidata de N+1 da referência tem a mesma função e a
   mesma query normalizada;
3. o local foi tocado por um commit do intervalo do deploy (correlação do ADR-0006).

A mensagem diz que é um Sinal antecipado, ainda sem amostra para confirmar lentidão, com a função,
a linha, o commit e quantos traces o sustentam.

Quando a operação junta a amostra, sai uma segunda mensagem, ligada à primeira:
- **com Regressão:** o Resultado completo, como confirmação;
- **sem Regressão de latência:** o N+1 continua como Causa sem Regressão, com o aviso do ADR-0032.

O Sinal antecipado não é retirado: o N+1 é real mesmo sem lentidão. Fica ligado por padrão, e a
configuração permite desligar.

## Alternativas consideradas

- **Publicar no primeiro trace.** Um trace pode ser um caso extremo, e o custo de esperar mais dois
  é pequeno. Perdeu.
- **Sem a condição de não existir na referência.** Culparia o deploy por N+1 antigos, que é
  alucinação de commit. Perdeu.
- **Sem a condição do commit tocado.** Um N+1 novo sem commit no intervalo tem outra origem (dado,
  configuração). Ele espera a amostra e o `analyze` completo. Perdeu.
- **Retirar o sinal quando não há Regressão.** Contradiz o ADR-0032, que mantém a Causa sem
  Regressão. Perdeu.

## Consequências

- Uma operação rara com N+1 novo ganha aviso em minutos, e não em horas.
- Toda Causa antecipada depende do hook, porque sem a linha não há comparação de local com a
  referência.
- São duas mensagens por operação nesse caso, e o publicador precisa ligar a segunda à primeira.

## Como validar

- Testes do domínio: o N+1 novo e tocado dispara com 3 traces e não com 2; o N+1 que existe na
  referência não dispara; o N+1 sem commit no intervalo não dispara; a segunda mensagem sai nos dois
  desfechos.
- Placar: um cenário com N+1 preexistente fora do commit do deploy não produz Sinal antecipado.
