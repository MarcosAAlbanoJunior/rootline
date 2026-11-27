---
id: ADR-0034
title: Declarar a amostragem do backend na configuração e pular a latência com tail sampling
status: Aceito
date: 2026-11-26
summary: A amostragem é declarada (none, head com taxa, tail), não detectada. Com none e head tudo roda, e com head o prazo sem amostra considera a taxa. Com tail, a camada de latência é pulada com o motivo nas Verificações e só sai o sinal estrutural, com aviso. A taxa achada nos dados só confere o que foi declarado.
modules: [analysis, cmd/rootline]
---

# 0034. Declarar a amostragem do backend na configuração

## Contexto

Ao ler do backend (ADR-0033), o Rootline vê só os traces que sobraram da amostragem do time. Os dois
tipos comuns quebram coisas diferentes:

- **Head sampling** (por exemplo 10% aleatório na origem): não enviesa a latência. As duas versões
  perdem traces na mesma proporção, então só demora mais para juntar os 30 traces da amostra mínima
  (ADR-0032). A razão de carga continua comparável se a taxa for a mesma nas duas versões.
- **Tail sampling** (o Collector guarda os lentos e os com erro): enviesa a latência. O p50 da
  versão nova sai de traces escolhidos por serem lentos, e a regra de Regressão perde o sentido. O
  N+1 visto num trace continua verdadeiro, porque é a forma do trace e não a duração.

O trace não carrega, de forma confiável, qual política decidiu guardá-lo.

## Decisão

A configuração do Rootline declara a amostragem do serviço: `none`, `head` com a taxa, ou `tail`.

- **`none` e `head`:** a análise roda inteira. Com `head`, o prazo sem amostra (ADR-0035) e o
  resumo dizem que a taxa foi considerada.
- **`tail`:** a camada de latência é pulada. A `symptom_regression` sai como pulada, com o motivo,
  e não há Regressão. Sai só o sinal estrutural (ADR-0036), marcado como "sem comparação de
  latência possível com tail sampling".
- **Sem declaração:** vale `none`, e o Resultado avisa que a amostragem não foi declarada.
- Se os dados trouxerem a taxa (por exemplo no `tracestate`), ela só confere a declaração: uma
  divergência vira aviso e não muda a declarada.

## Alternativas consideradas

- **Detectar a amostragem pelos dados.** Não há sinal confiável para o tail sampling, e errar a
  detecção produziria Regressões falsas. Perdeu.
- **Recusar rodar com tail sampling.** Joga fora o sinal estrutural, que continua certo. Perdeu.
- **Corrigir o viés do tail sampling pela política.** Exigiria conhecer e modelar a política do
  Collector de cada time. Perdeu.

## Consequências

- Uma declaração errada (dizer `head` com tail sampling) produz Regressões enviesadas. O guia de
  instrumentação diz como conferir a política do Collector.
- Com `tail`, o watch fica restrito ao N+1, que é o único sinal estrutural de hoje.

## Como validar

- Testes do domínio: com `tail`, a `symptom_regression` sai pulada, e a Candidata estrutural sai com
  o aviso; sem declaração, sai o aviso de amostragem não declarada.
- Um cenário gravado com head sampling a 50% dá a mesma Regressão que sem amostragem, com amostra
  suficiente.
