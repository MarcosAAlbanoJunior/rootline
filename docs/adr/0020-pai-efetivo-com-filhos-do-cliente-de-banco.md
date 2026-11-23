---
id: ADR-0020
title: Agrupar as queries repetidas pelo pai efetivo, contando como filho de banco todo span do cliente de banco
status: Aceito
date: 2026-11-23
summary: Substitui o ADR-0018. O pai efetivo é o pai direto ou, quando o pai é um span INTERNAL repetido sob o mesmo avô e ele e seus homônimos só têm filhos de banco, o avô. Filho de banco é span de query/prepare ou com db.system.name/db.system (pool.acquire, connect). Um nível só; colapso vira evidência sem mudar a confiança.
modules: [detector, analysis, eval]
---

# 0020. Agrupar as queries repetidas pelo pai efetivo, contando todo span do cliente de banco

> Substitui o [ADR-0018](0018-pai-efetivo-das-queries-repetidas.md). A única mudança de regra está na
> condição 5: o que conta como "span de banco". Este ADR reúne a decisão completa.

## Contexto

O detector de N+1 agrupa os spans de query de um trace por `(pai direto, query normalizada)` e
procura o "+1" (a query anterior cujo número de linhas bate com o tamanho do grupo) sob o mesmo pai
direto. Isso funciona enquanto as queries repetidas são filhas diretas do mesmo span, como no
n1-basic, no n1-helper e no n1-rangefunc (filhas do span HTTP) e no n1-errgroup, que propaga o
contexto da requisição.

Pools de trabalho instrumentados costumam abrir um span por job (por exemplo, `workpool.job`), filho
do contexto de quem submeteu. Um N+1 que submete um job por item gera N spans de query, cada um sob
um pai diferente. Cada grupo fica com tamanho 1 e o N+1 some. O mesmo vale para middlewares e
wrappers genéricos que embrulham cada item num span próprio.

Esse span de infraestrutura não é um span manual no lugar do bug, o que o ADR-0002 proíbe. Ele é
genérico, vale para qualquer job, e o Rootline não pode exigir que o serviço analisado deixe de
emiti-lo nem que o marque.

O ADR-0018 aceitava como filhos do wrapper só spans de fase `query` ou `prepare`. A primeira gravação
do cenário n1-workpool mostrou que isso não basta: nos 1600 spans `workpool.job` da versão com a
regressão, cada um tinha, além do `SELECT`, um filho `pool.acquire` (e, na primeira aquisição de uma
conexão, um neto `connect`). Esses spans vêm do otelpgx (escopo `github.com/exaring/otelpgx`), têm
`Kind` `CLIENT` e o atributo `db.system.name`, mas não têm `db.query.text`, então a fase deles é
`PhaseNone`. Com a regra do ADR-0018, nenhum job colapsava e o cenário errava as quatro métricas.

## Decisão

**O pai efetivo de um span de query `q`, com pai direto `p` no mesmo trace, é o avô `g` quando
todas as condições abaixo valem. Caso contrário, é `p`.**

1. `p` tem `Kind` `INTERNAL`.
2. `p` não é span de banco (`PhaseOf(p) == PhaseNone`).
3. `g`, o pai de `p`, está no trace.
4. Entre os filhos de `g`, pelo menos 2 têm o mesmo `Name` de `p`, contando o próprio `p`.
5. `p` e todos os seus homônimos sob `g` só têm filhos que são spans de banco. **Span de banco** é o
   que tem fase `query` ou `prepare` (`PhaseOf(s) != PhaseNone`) **ou** tem o atributo
   `db.system.name` **ou** o atributo `db.system` (nome antigo do mesmo atributo na semconv do
   OpenTelemetry). Isso inclui `pool.acquire` e `connect` do otelpgx. Um homônimo sem filhos conta
   como só com filhos de banco. Um wrapper com uma chamada HTTP externa, um sub-handler ou qualquer
   outro filho sem esses atributos não é colapsado. Várias queries diferentes no mesmo job são
   permitidas, e cada uma forma seu grupo sob `g`.

**Um nível só.** A regra não sobe em loop: pool dentro de pool não colapsa, e um teste unitário
documenta essa limitação.

**Fonte única.** Uma função do detector (`EffectiveParent`) implementa a regra e é usada pelo
agrupamento (`groupKey`) e pelo `plusOne`. O `ParentSpanID` da ocorrência e o
`sample_parent_span_id` da evidência são o do pai efetivo. O `db_query_spans_median` não usa a
regra: ele conta spans de query por trace, não grupos.

**Evidência sem mudança de confiança.** Quando houve colapso, a evidência de query repetida
registra o nome do span colapsado (`collapsed_parent_name`) e a quantidade de spans colapsados
(`collapsed_parent_count`). A confiança e o score da candidata não mudam: o colapso só torna o grupo
visível, e a força do sinal continua vindo da repetição, das linhas e do "+1".

## Alternativas consideradas

- **Sempre a operação de entrada como pai.** Agrupa por `(span raiz do serviço, query)`. É simples,
  mas muda a semântica de todos os cenários: o `plusOne` passa a aceitar qualquer query anterior do
  trace como "+1", e queries iguais em sub-etapas legítimas diferentes se fundem. Perdeu por raio de
  impacto.
- **Marcação explícita (lista de scopes ou atributo `rootline.*` no span do pool).** É precisa, mas
  exige que o serviço analisado se anote, o que vai contra o ADR-0002. Perdeu.
- **Subir enquanto a regra valer (vários níveis).** Cobriria pool dentro de pool, mas aumenta o risco
  de fundir grupos que não deveriam se fundir, sem nenhum cenário que precise disso. Perdeu.
- **Sem a condição sobre os filhos (condição 5).** Colapsaria spans repetidos que fazem outras coisas
  (chamada HTTP externa, sub-handler) e criaria grupos artificiais. Perdeu.
- **Filho de banco só com fase `query` ou `prepare` (ADR-0018).** Não colapsa nenhum job real com
  otelpgx, que emite `pool.acquire` em toda aquisição de conexão. Perdeu pela gravação do n1-workpool.
- **Lista de nomes de span do otelpgx (`pool.acquire`, `connect`).** Resolve o caso medido, mas amarra
  o domínio a uma biblioteca e quebra com qualquer outro driver ou versão que mude os nomes. Perdeu
  para o atributo da semconv, que vale para qualquer instrumentação de banco.
- **Desligar o trace de aquisição de conexão no demo-app.** Faria o cenário passar ajustando o app
  ao detector, e um serviço real com otelpgx continuaria sem detecção. Perdeu.

## Consequências

- O N+1 dentro de um pool com um span por job passa a ser detectado, com o "+1" achado sob o avô,
  também quando o driver emite spans de aquisição de conexão e de conexão.
- Nos cenários em que as queries são filhas diretas de um span `SERVER`, nada muda (condição 1).
- Pool dentro de pool, ou wrapper com filho que não é de banco, continua sem detecção. É uma
  limitação conhecida, coberta por teste.
- O Resultado JSON ganha campos na evidência de query repetida. Quem consome o Resultado pode ver que
  o grupo veio de um colapso.
- Risco: um wrapper repetido que só faz queries mas representa unidades de trabalho legitimamente
  distintas (por exemplo, um job por cliente, cada um com sua própria query em lote) é colapsado. Os
  limiares do detector (repetições, linhas por query, "+1") continuam filtrando. Se isso gerar falso
  positivo, o caso vira cenário de distrator.
- Risco: um span de outro sistema de dados com `db.system.name` (cache, por exemplo) conta como filho
  de banco e não impede o colapso. Ele não entra nos grupos (só spans de fase `query` entram), então o
  efeito se limita a tornar o colapso possível.

## Como validar

- Testes unitários da função do pai efetivo, só com spans sintéticos: o formato de pool (N wrappers
  `INTERNAL` homônimos, cada um com uma query e um `pool.acquire`) colapsa para o avô; job com query e
  `connect` colapsa; filho `CLIENT` só com `db.system` (nome antigo) conta como de banco; wrapper único
  não colapsa; pai `SERVER` não colapsa; wrapper com filho HTTP sem `db.system*` não colapsa (nem os
  homônimos dele); pool dentro de pool não colapsa (limitação); avô ausente do trace mantém o pai
  direto; job com duas queries diferentes gera dois grupos sob o avô.
- Teste do detector: o "+1" sob o avô é reconhecido, e a evidência traz `collapsed_parent_name` e a
  quantidade sem mudar a confiança.
- Avaliação: o cenário n1-workpool acerta Causa, Linha, Commit e Estrito no placar, e os cenários
  anteriores não regridem.
