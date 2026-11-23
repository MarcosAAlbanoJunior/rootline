---
id: ADR-0018
title: Agrupar as queries repetidas pelo pai efetivo, pulando um nível de wrapper repetido só com filhos de banco
status: Substituído por ADR-0020
date: 2026-11-23
summary: O pai efetivo de um span de query é o pai direto ou, quando o pai é um span INTERNAL repetido sob o mesmo avô e ele e seus homônimos só têm filhos de banco, o avô. Um nível só. Usado no agrupamento e no "+1"; o colapso vira evidência sem mudar a confiança.
modules: [detector, analysis, eval]
---

# 0018. Agrupar as queries repetidas pelo pai efetivo

## Contexto

O detector de N+1 agrupa os spans de query de um trace por `(pai direto, query normalizada)` e
procura o "+1" (a query anterior cujo número de linhas bate com o tamanho do grupo) sob o mesmo pai
direto. Isso funciona enquanto as queries repetidas são filhas diretas do mesmo span, como no
n1-basic, no n1-helper e no n1-rangefunc (filhas do span HTTP) e no errgroup, que propaga o contexto
da requisição.

Pools de trabalho instrumentados costumam abrir um span por job (por exemplo, `workpool.job`), filho
do contexto de quem submeteu. Um N+1 que submete um job por item gera N spans de query, cada um sob
um pai diferente. Cada grupo fica com tamanho 1 e o N+1 some. O mesmo vale para middlewares e
wrappers genéricos que embrulham cada item num span próprio.

Esse span de infraestrutura não é um span manual no lugar do bug, o que o ADR-0002 proíbe. Ele é
genérico, vale para qualquer job, e o Rootline não pode exigir que o serviço analisado deixe de
emiti-lo nem que o marque.

## Decisão

**O pai efetivo de um span de query `q`, com pai direto `p` no mesmo trace, é o avô `g` quando
todas as condições abaixo valem. Caso contrário, é `p`.**

1. `p` tem `Kind` `INTERNAL`.
2. `p` não é span de banco (`PhaseOf(p) == PhaseNone`).
3. `g`, o pai de `p`, está no trace.
4. Entre os filhos de `g`, pelo menos 2 têm o mesmo `Name` de `p`, contando o próprio `p`.
5. `p` e todos os seus homônimos sob `g` só têm filhos que são spans de banco (fase `query` ou
   `prepare`). Um wrapper com uma chamada HTTP externa, um sub-handler ou qualquer outro filho que
   não seja de banco não é colapsado. Várias queries diferentes no mesmo job são permitidas, e cada
   uma forma seu grupo sob `g`.

**Um nível só.** A regra não sobe em loop: pool dentro de pool não colapsa, e um teste unitário
documenta essa limitação.

**Fonte única.** Uma função do detector (`EffectiveParent`) implementa a regra e é usada pelo
agrupamento (`groupKey`) e pelo `plusOne`. O `ParentSpanID` da ocorrência e o
`sample_parent_span_id` da evidência passam a ser o do pai efetivo. O `db_query_spans_median` não
usa a regra: ele conta spans de query por trace, não grupos.

**Evidência sem mudança de confiança.** Quando houve colapso, a evidência de query repetida
registra o nome do span colapsado (`collapsed_parent_name`) e a quantidade de spans colapsados. A
confiança e o score da candidata não mudam: o colapso só torna o grupo visível, e a força do sinal
continua vindo da repetição, das linhas e do "+1".

## Alternativas consideradas

- **Sempre a operação de entrada como pai.** Agrupa por `(span raiz do serviço, query)`. É simples,
  mas muda a semântica de todos os cenários: o `plusOne` passa a aceitar qualquer query anterior do
  trace como "+1", e queries iguais em sub-etapas legítimas diferentes se fundem. Perdeu por raio de
  impacto.
- **Marcação explícita (lista de scopes ou atributo `rootline.*` no span do pool).** É precisa, mas
  exige que o serviço analisado se anote, o que vai contra o ADR-0002 (o hook vale igualmente para
  todas as queries, sem marcação onde está o problema). Perdeu.
- **Subir enquanto a regra valer (vários níveis).** Cobriria pool dentro de pool, mas aumenta o risco
  de fundir grupos que não deveriam se fundir, sem nenhum cenário que precise disso. Perdeu: um nível
  cobre o padrão observado, e a limitação fica documentada em teste.
- **Sem a condição sobre os filhos (condição 5).** Colapsaria spans repetidos que fazem outras coisas
  (chamada HTTP externa, sub-handler) e criaria grupos artificiais. Perdeu.

## Consequências

- O N+1 dentro de um pool com um span por job passa a ser detectado, com o "+1" achado sob o avô.
- Nos cenários em que as queries são filhas diretas de um span `SERVER`, nada muda (condição 1).
- Pool dentro de pool, ou wrapper com filho que não é de banco, continua sem detecção. É uma
  limitação conhecida, coberta por teste.
- O Resultado JSON ganha campos na evidência de query repetida. Quem consome o Resultado pode ver que
  o grupo veio de um colapso.
- Risco: um wrapper repetido que só faz queries mas representa unidades de trabalho legitimamente
  distintas (por exemplo, um job por cliente, cada um com sua própria query em lote) é colapsado. Os
  limiares do detector (repetições, linhas por query, "+1") continuam filtrando. Se isso gerar falso
  positivo, o caso vira cenário de distrator.

## Como validar

- Testes unitários da função do pai efetivo, só com spans sintéticos: o formato de pool (N wrappers
  `INTERNAL` homônimos, cada um com uma query) colapsa para o avô; wrapper único não colapsa; pai
  `SERVER` não colapsa; wrapper com filho que não é de banco não colapsa (nem os homônimos dele);
  pool dentro de pool não colapsa (limitação); avô ausente do trace mantém o pai direto; job com duas
  queries diferentes gera dois grupos sob o avô.
- Teste do detector: o "+1" sob o avô é reconhecido, e a evidência traz `collapsed_parent_name` e a
  quantidade sem mudar a confiança.
- Avaliação: o cenário de N+1 em pool de trabalho acerta Causa, Linha, Commit e Estrito no placar, e
  os cenários anteriores não regridem.
