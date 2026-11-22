---
id: ADR-0001
title: Analisar em duas fases, com detectores determinísticos antes do LLM
status: Aceito
date: 2026-11-22
summary: Detectores em código geram candidatas com evidências e correlação com o diff; o LLM só escolhe e explica, com citação validada em código; sem candidata, cai no agente livre com confiança baixa.
modules: [dominio, agente, eval]
---

> Revisão em 2026-11-22 (ADR-0003, mudança para Go): a decisão continua válida, porque não
> depende de linguagem. Só mudaram os exemplos específicos de Java: a variação "N+1 via lazy
> loading" e a regra ArchUnit.

# 0001. Analisar em duas fases, com detectores determinísticos antes do LLM

## Contexto

O brief pede duas coisas que puxam em direções opostas: "o que dá para resolver de forma
determinística é feito em código, o LLM entra para correlacionar e explicar" e um agente com
ferramentas (`get_trace`, `find_slow_spans`, `get_code`, `diff_between`, `explain_query`).
Um agente livre como modo principal infla o contexto, é caro de testar e torna a avaliação
difícil de interpretar. Detectores sozinhos não lidam com o que não conhecem.

## Decisão

A análise roda em duas fases.

**Fase 1, determinística (domínio puro).**
- Cada detector examina o trace do sintoma e emite candidatas, cada uma com suas evidências
  (spans, query normalizada, contagens, arquivo:linha).
- Na v1 há um detector para cada regressão do demo: N+1, índice faltando, contenção de lock e
  chamada lenta em loop.
- A correlação candidata ↔ diff também é calculada aqui: o commit entre a referência e a
  regressão tocou no arquivo ou método da candidata? O resultado é anexado como evidência.
- Cada candidata recebe um score determinístico, para que o modo "só detectores" produza uma
  resposta única.

**Fase 2, LLM.**
- O LLM recebe só as candidatas e o contexto mínimo delas: span suspeito, linhas relevantes e
  trecho do diff.
- Ele escolhe a causa, correlaciona e explica, citando os IDs das evidências.
- As ferramentas somente leitura do brief ficam disponíveis com orçamento de chamadas.

**Validação de citação em código.** Se a resposta do LLM citar um ID de evidência inexistente,
ou não citar nada, o resultado vira "inconclusivo". A regra é determinística e tem teste.

**Fallback.** Se nenhum detector disparar, roda o agente livre, com orçamento de chamadas e
`confiança: baixa` marcada no resultado.

## Alternativas consideradas

- **Agente livre como modo principal.** Mais geral, mas manda contexto demais ao LLM, é lento e
  caro de testar, e o número da avaliação não separa o mérito dos detectores do mérito do LLM.
  Ficou como fallback.
- **Só detectores, sem LLM.** Barato e 100% testável, mas não explica, não correlaciona causas
  concorrentes e não sugere correção. Ficou como linha de base na avaliação.

## Consequências

- Os detectores e a validação de citação são domínio puro, testados com testes unitários em
  milissegundos, sem LLM.
- O contexto mandado ao LLM fica pequeno por construção.
- **A avaliação mede três modos, cada um forçado em todos os cenários:** só detectores,
  pipeline (detectores + LLM) e agente livre. Para não ser tautológica, ela precisa incluir:
  - cenários com várias candidatas: duas regressões simultâneas, ou uma real mais distratores
    (span lento não relacionado, commit irrelevante);
  - variações do mesmo tipo, como N+1 escondido atrás de um helper em outra camada (lista
    completa no ADR-0002);
  - um conjunto reservado, escrito depois de congelar os detectores e nunca usado para ajustá-los.
- Regressões fora dos quatro tipos só são cobertas pelo fallback, com confiança baixa.
- Risco: quem escreve o conjunto reservado não pode ajustar os detectores depois de vê-lo, senão
  ele deixa de ser reservado. O processo precisa ser registrado na spec da avaliação.

## Como validar

- Teste unitário: uma resposta com ID de evidência inexistente, ou sem citação, resulta em "inconclusivo".
- Relatório da avaliação com acerto por modo e por grupo de cenários (básicos, várias
  candidatas, variações, reservado).
- Regra de import verificada no CI: o pacote de detectores não importa OTel, banco, LLM nem adapters.
