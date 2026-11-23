---
id: ADR-0023
title: Detectar índice faltando só pelo plano de execução, sem ANALYZE e sem tempo
status: Aceito
date: 2026-11-23
summary: Uma query da versão ruim é candidata a missing_index quando o plano genérico tem Seq Scan ou Parallel Seq Scan com filtro numa tabela com reltuples ≥ 10 mil e seletividade estimada < 1%. O Explainer devolve o plano e o reltuples das tabelas; sem planos, o detector não roda e o Resultado ganha o aviso missing_index_skipped:.
modules: [detector, analysis, explain]
---

# 0023. Detectar índice faltando só pelo plano de execução

## Contexto

O índice faltando é uma das quatro regressões da v1 (ADR-0001), e até a slice 3c só existia o
detector de N+1. O ADR-0004 proíbe tempo absoluto como critério de causa: o sinal precisa ser
estrutural. O ADR-0012 define a porta `Explainer`. O adapter ao vivo roda `EXPLAIN` **sem**
`ANALYZE`, com `GENERIC_PLAN` quando a query tem parâmetros. Um sinal que dependa do `EXPLAIN
ANALYZE`, como "Rows Removed by Filter" ou o tempo real, funcionaria na avaliação e quebraria no uso
real.

O plano JSON do Postgres traz, por nó, o tipo (`Node Type`), a tabela (`Relation Name`), o filtro
(`Filter`, como texto) e as linhas estimadas (`Plan Rows`). Ele não traz o tamanho da tabela: o
`reltuples` fica no catálogo (`pg_class`). Num `Parallel Seq Scan`, o `Plan Rows` do nó é por
worker. A estimativa total fica no `Gather` acima dele.

Com `GENERIC_PLAN`, o planner estima a seletividade de `col = $1` como 1/n_distinct da coluna, e a
de uma desigualdade (`col > $1`) com o padrão de 1/3, sem olhar a amostra.

Em tabelas pequenas, o planner escolhe Seq Scan mesmo quando o índice existe, porque ler a tabela é
mais barato. O schema base do demo-app não tinha índice nas chaves estrangeiras
(`orders.customer_id`, `order_items.order_id`), e as tabelas tinham 200 e 600 linhas.

## Decisão

**A porta `Explainer` devolve, por query, o plano JSON cru e o `reltuples` de cada tabela que
aparece nele.** A forma de gravar os planos está no ADR-0025.

**Uma query da versão ruim vira candidata `missing_index` quando o plano tem um nó com as três
condições:**

1. `Seq Scan`, ou `Parallel Seq Scan` sob `Gather`, com `Filter`.
2. Tabela grande: `reltuples` ≥ 10.000.
3. Filtro seletivo: as linhas estimadas do nó divididas pelo `reltuples` ficam abaixo de 1%. No
   `Parallel Seq Scan`, a estimativa usada é a total, a do `Gather`, não a por worker.

Os limiares (10.000 linhas e 1%) são valores iniciais, a calibrar como os do N+1. Nenhum tempo entra
no critério.

**Agregação como no N+1.** As ocorrências se agrupam por query normalizada e localização, e o
score é a fração de traces da versão ruim em que a query aparece, mais o bônus de commit da
correlação. Valem os mínimos de traces afetados do N+1.

**Alvo e evidência.**
- O alvo da causa é a tabela do nó (`Relation Name`). Ele entra na métrica Causa (ADR-0026).
- A evidência traz:
  - o tipo do nó;
  - a tabela;
  - o `reltuples`;
  - as linhas estimadas;
  - a seletividade;
  - o texto cru do `Filter`;
  - as colunas extraídas dele em melhor esforço, só da forma `coluna = $n`.
- A coluna não entra na Causa. Extraí-la de forma confiável exige parser de SQL, que está em aberto
  no ADR-0006.

**Sem planos, o detector não roda e diz isso.** Sem planos na análise (sem `--plans`, ou uma
gravação anterior a eles), o Resultado ganha o aviso
`missing_index_skipped: no query plans`. É um aviso, não uma observação, porque descreve o processo
de análise, não o sistema analisado. Uma query específica sem plano, por ter sido gravada com um
motivo de "não explicável", também gera aviso (ADR-0012). O prefixo `missing_index_skipped:` é o
contrato.

**Schema base saudável.** O schema base do demo-app ganha índices nas chaves estrangeiras
(`orders.customer_id`, `order_items.order_id`). Os cenários de índice faltando crescem só as tabelas
que usam, num arquivo de seed próprio, aplicado pelo demo-app junto com os demais `.sql` da pasta,
em ordem.

## Alternativas consideradas

- **Usar o `EXPLAIN ANALYZE` na gravação** ("Rows Removed by Filter", tempo real). É um sinal mais
  forte, mas não existe no adapter ao vivo, que não pode executar a query. O detector se comportaria
  diferente na avaliação e no uso real. Perdeu.
- **Usar a fração do tempo do trace gasta na query.** É tempo relativo, mais robusto que o absoluto,
  mas oscila entre gravações e quebra o determinismo do placar. Perdeu.
- **Comparar com o plano da referência e só marcar quando o plano mudou.** Não serve quando a query
  nem existe na referência, que é o caso do índice faltando introduzido por commit. É o sinal do
  cenário sem commit, numa decisão própria. Perdeu aqui.
- **Ignorar tabelas pequenas sem corrigir o schema base.** Esconderia o Seq Scan nas chaves
  estrangeiras em vez de corrigi-lo, e o app de demonstração continuaria doente sem que nenhum
  cenário dissesse isso. Perdeu.
- **Ler `reltuples` de fora da porta.** Espalharia acesso a catálogo pelo código. Perdeu para a
  porta devolver o plano e o tamanho juntos.

## Consequências

- O Rootline passa a ter um segundo detector, com evidência que não vem do trace.
- O Seq Scan pouco seletivo numa tabela grande não vira candidata, por exemplo um total com
  `amount > $1`, que com o plano genérico tem seletividade de cerca de 1/3. Um índice ali nem
  ajudaria.
- Uma coluna de baixa cardinalidade (`status` com 3 valores) também não vira candidata. É o lado
  certo do erro.
- Risco: uma query repetida (N+1) que também faz Seq Scan seletivo numa tabela grande gera duas
  candidatas válidas. Os scores dos dois detectores não foram calibrados entre si, e a ordem entre
  causas concorrentes não está decidida.
- Risco: o plano genérico pode divergir do plano real com um valor específico (ADR-0012). A
  evidência marca o plano como genérico.
- Risco: os limiares são iniciais. Os cenários ficam com folga grande em relação a eles para não
  ficarem sob medida.
- Mudar o schema base regrava os cenários existentes. O placar deles precisa continuar idêntico.

## Como validar

- Testes unitários do detector, só com planos sintéticos:
  - tabela grande com Index Scan não vira candidata;
  - Seq Scan seletivo numa tabela grande vira;
  - Seq Scan pouco seletivo não vira;
  - Seq Scan seletivo numa tabela abaixo do limiar não vira;
  - `Parallel Seq Scan` seletivo sob `Gather` vira, usando a estimativa total.
- Teste da análise: sem planos, o aviso `missing_index_skipped: no query plans` aparece e não há
  candidata de índice.
- Avaliação:
  - zero candidatas de índice nos cenários de N+1, com o placar deles idêntico depois da mudança no
    schema base;
  - no cenário de índice faltando, a causa acerta tipo, função e alvo;
  - o distrator de Seq Scan pouco seletivo não vira candidata.
