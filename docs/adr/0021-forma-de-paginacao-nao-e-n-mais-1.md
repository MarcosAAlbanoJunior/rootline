---
id: ADR-0021
title: Descartar como N+1 a ocorrência com forma de paginação
status: Aceito
date: 2026-11-23
summary: Uma ocorrência cuja query tem LIMIT, cujas linhas são páginas cheias iguais (k ≥ 2) seguidas de uma página menor e que não tem "+1" é paginação. Ela sai das candidatas antes da agregação, e o Resultado ganha uma observação com o prefixo estável pagination_shape:.
modules: [detector, analysis]
---

# 0021. Descartar como N+1 a ocorrência com forma de paginação

## Contexto

O detector de N+1 aceita como ocorrência um grupo de pelo menos `MinRepetitions` (5) queries iguais
sob o mesmo pai efetivo (ADR-0020), com mediana de linhas até `MaxMedianRows` (10). O "+1" (uma query
anterior cujo número de linhas bate com o tamanho do grupo) não é obrigatório: sem ele, a candidata
sai com confiança `medium`.

Uma paginação em lotes pequenos passa nesses limiares. Ler os 30 itens de um cliente com `LIMIT 4`
gera 8 queries iguais, com 4, 4, 4, 4, 4, 4, 4 e 2 linhas, dentro de um `for`. O detector a reporta
como N+1, com localização de nível 1 e `in_loop: true`. A slice 1 aceitou esse falso positivo e o
deixou para quando o distrator de paginação e a métrica de falso positivo de detector (ADR-0009)
entrassem na avaliação.

Os spans de query não trazem os parâmetros (o otelpgx só os grava com opção explícita), então não dá
para ver que cada página continua a anterior. Sobram dois sinais: o texto da query e a sequência de
linhas por query. `LIMIT` sozinho não serve: um N+1 que busca um item por vez com `LIMIT 1` é comum e
precisa continuar detectado.

## Decisão

**Uma ocorrência tem forma de paginação quando as três condições valem:**

1. A query normalizada contém a palavra `LIMIT` (sem diferenciar maiúsculas), como palavra inteira.
2. Com as queries do grupo ordenadas pelo início, todas as linhas menos a última são iguais a um
   mesmo `k ≥ 2` (as páginas cheias), e a última tem menos que `k` linhas, inclusive 0 (a página que
   encerra a leitura). Toda query do grupo precisa ter o número de linhas; sem ele, não é paginação.
3. A ocorrência não tem "+1".

**A ocorrência com forma de paginação sai antes da agregação.** Ela não conta para os traces
afetados, não vira candidata e não entra no Resultado como candidata. A decisão é por ocorrência
(por trace): se só alguns traces de uma query têm a forma, só esses saem.

**O descarte fica visível.** Quando alguma ocorrência é descartada, o Resultado ganha uma observação
por query, com o prefixo estável `pagination_shape:`, a query normalizada e quantos traces tiveram a
forma, por exemplo `pagination_shape: SELECT ... LIMIT 4 (12 traces)`. Quem consome o Resultado casa
pelo prefixo, não pelo resto do texto.

## Alternativas consideradas

- **Rebaixar em vez de descartar.** A candidata continuaria no Resultado com confiança `low` e score
  menor. É mais transparente, mas continua sendo um falso positivo de detector pela métrica do
  ADR-0009, a menos que a métrica passe a ignorar `low`, o que seria uma segunda regra. Perdeu; a
  observação dá a transparência sem criar candidata.
- **Só pelas linhas, sem olhar o SQL.** Cobriria cursores sem `LIMIT`, mas um N+1 cujo último item
  tem menos linhas por acaso (por exemplo, 3, 3, 3, 3, 1) viraria paginação e sumiria. Perdeu pelo
  risco de falso negativo.
- **Só pelo `LIMIT`.** Descartaria o N+1 com `LIMIT 1` por item, que é exatamente o padrão que o
  detector existe para achar. Perdeu.
- **Pelos parâmetros da query (o cursor de cada página vem da anterior).** É o sinal mais forte, mas
  exige gravar os parâmetros, o que o serviço analisado não faz por padrão e que pode expor dados.
  Perdeu.

## Consequências

- Paginação em lotes pequenos para de virar candidata, tanto keyset quanto `OFFSET`, porque as duas
  usam `LIMIT`.
- Um N+1 com linhas constantes termina com a mesma contagem das anteriores e não casa com a condição
  2. Um N+1 com "+1" nunca é paginação (condição 3), mesmo com `LIMIT` por item e um último item menor.
- Risco: um N+1 sem "+1" que usa `LIMIT` por item e cujo último item, por acaso, tem menos linhas que
  os outros é descartado. A observação `pagination_shape:` deixa o caso visível.
- Risco: uma paginação precedida por uma query cujo número de linhas bate com o número de páginas
  ganha um "+1" por coincidência e continua sendo candidata. Esse é o lado conservador do erro.
- Paginação sem `LIMIT` (`FETCH FIRST n ROWS ONLY`, cursores do servidor) não é reconhecida e
  continua podendo virar candidata.
- A palavra `LIMIT` dentro de um literal de texto da query conta. Não há parser de SQL no detector, e
  o caso é raro.

## Como validar

- Testes unitários do detector, só com spans sintéticos, para cada condição: a paginação com última
  página menor sai, e a com última página vazia também; um grupo com `LIMIT` e linhas constantes
  continua; um grupo com última página menor sem `LIMIT` continua; uma paginação com "+1" continua;
  um grupo sem o número de linhas em alguma query continua; `limit` em minúsculas conta.
- Teste da análise: o Resultado traz a observação `pagination_shape:` com a contagem de traces.
- Avaliação: no cenário com distrator de paginação, o falso positivo de detector é 0, e os demais
  cenários não regridem.
