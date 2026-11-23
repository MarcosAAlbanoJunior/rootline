---
id: ADR-0025
title: Gravar os planos de execução por versão num plans.jsonl, com o plano cru e o tamanho das tabelas
status: Aceito
date: 2026-11-23
summary: Complementa o ADR-0012. Após a carga de cada versão, a gravação roda ANALYZE, EXPLAIN (GENERIC_PLAN, FORMAT JSON) de cada query normalizada distinta e lê o reltuples das tabelas, gravando em plans.jsonl o plano cru ou o motivo de não ser explicável, com o server_version. O adapter ao vivo fica para outra slice.
modules: [explain, recording, eval]
---

# 0025. Gravar os planos de execução por versão num plans.jsonl

## Contexto

O ADR-0012 decide a porta `Explainer`, o adapter gravado (planos salvos na gravação, depois de
`ANALYZE`) e o adapter ao vivo com salvaguardas. Ele não fixa o formato da gravação, o momento
exato, o que fazer com queries não explicáveis nem como o tamanho das tabelas chega ao detector. O
detector de índice faltando (ADR-0023) precisa do plano e do `reltuples` de cada tabela do plano.

A gravação sobe as versões do demo-app em sequência sobre o mesmo Postgres. O `ANALYZE` amostra
linhas: numa tabela grande, `reltuples` e `n_distinct` variam um pouco entre gravações. Os traces
trazem o texto original da query (`db.query.text`), com parâmetros `$n`, e a análise agrupa pela
query normalizada.

## Decisão

**Quando.** Após a carga de cada versão, a gravação:
1. roda `ANALYZE` nas tabelas;
2. roda `EXPLAIN (GENERIC_PLAN, FORMAT JSON)` de cada query normalizada distinta que aparece nas
   spans de banco dos traces daquela versão (todas as rotas, não só a do sintoma, para não acoplar
   a gravação à análise), usando o texto original de um span representativo;
3. lê o `reltuples` (`pg_class`) de cada tabela que aparece nos planos.

**Onde.** Num `plans.jsonl` na gravação, com uma linha por (versão, query normalizada). Cada linha
traz:
- o plano JSON cru do Postgres e o mapa tabela → `reltuples`; ou
- o motivo de a query não ser explicável, como um `INSERT`, um comando de sessão ou o health check.

O `server_version` do Postgres também vai para a gravação. A chave é a query normalizada, e o plano
cru permite mudar o detector sem regravar.

**Validador.** A decisão "SELECT único ou motivo" é uma função só, usada pelos adapters gravado e ao
vivo. Sem parser de SQL (ADR-0006), ela é heurística: começa com `SELECT` ou `WITH` e não tem `;`
no meio. Um `WITH x AS (DELETE ...) SELECT` passa por ela. **No adapter ao vivo, a garantia é a
transação `READ ONLY` do ADR-0012; o validador é só a primeira barreira.**

**Na análise.** O adapter gravado lê o `plans.jsonl` da versão analisada. `rootline analyze` recebe
o arquivo por `--plans`, e `rootline-eval score` passa o de cada gravação.

**Determinismo.** Os planos crus não são comparados byte a byte entre gravações: as estimativas
variam com a amostra do `ANALYZE`. Os cenários ficam com folga grande em relação aos limiares dos
detectores, para que essa variação não mude o resultado.

**Cache.** Quando o cache de gravações do ADR-0008 existir, a chave também cobre:
- os arquivos de schema e seed dos patches do cenário;
- a configuração de estatísticas do Postgres (`default_statistics_target`).

A versão do Postgres e a seed base já estão na chave do ADR-0008. Os planos mudam a gravação, mas
não os traces.

**Escopo.** Esta decisão implementa só o adapter gravado. O adapter ao vivo do ADR-0012 fica para
uma slice própria. Ele é pré-requisito para rodar o detector de índice contra um sistema real.

## Alternativas consideradas

- **Gravar um resumo já interpretado** (tipo de nó, seletividade) em vez do plano cru. É menor, mas
  amarra a gravação a uma versão do detector, e mudar o detector exigiria regravar. Perdeu.
- **Explicar só as queries da rota do sintoma.** A gravação ficaria menor, mas passaria a depender
  de uma decisão da análise. Perdeu.
- **Gravar os planos uma vez, após a última versão.** Basta quando o schema é igual nas versões, mas
  o cenário sem commit precisa dos planos das duas janelas (ADR-0012). Perdeu para gravar por versão
  desde já.
- **Comparar planos byte a byte para detectar regravações divergentes.** Falharia por variação de
  amostragem sem que nada tenha mudado. Perdeu.

## Consequências

- A gravação fica mais lenta e maior, com um `EXPLAIN` por query distinta e por versão.
- Gravações anteriores a esta decisão não têm `plans.jsonl`. A análise roda e avisa (ADR-0023).
- Os Resultados dos cenários existentes mudam de forma esperada, com planos disponíveis e avisos
  novos. O critério de não regressão é o placar, não o Resultado byte a byte.
- Risco: o validador heurístico pode classificar mal SQL incomum. Na gravação, isso vira só um
  motivo de "não explicável".

## Como validar

- Teste do adapter gravado: lê o plano e o `reltuples` de uma linha do `plans.jsonl`, e devolve o
  motivo de uma query não explicável sem erro.
- Testes do validador: aceita `SELECT` e `WITH ... SELECT`; recusa `INSERT`, comandos múltiplos e
  comandos de sessão, com o motivo.
- Teste da gravação (e2e, com Docker): o `plans.jsonl` tem uma linha por (versão, query distinta) e
  o `server_version`.
