---
id: ADR-0016
title: Reportar a função declarada no lugar do corpo sintético de range-over-func
status: Substituído por ADR-0019
date: 2026-11-22
summary: O detector remove o sufixo -rangeN do nome do frame ao montar Location.Function, com a mesma regra do goindex, porque o nome sintético não existe no código. File e Line continuam os do frame.
modules: [detector, goindex, eval]
---

# 0016. Reportar a função declarada no lugar do corpo sintético de range-over-func

## Contexto

Num `for range` sobre um iterador (`iter.Seq`, `iter.Seq2`), o compilador transforma o corpo do loop
numa função sintética. O runtime a nomeia com o sufixo `-rangeN`, por exemplo
`example.com/m/pkg.(*Service).Lines-range1`. O ADR-0002 já conta com isso: o frame do corpo tem a
linha dentro do `for range`, então a regra do loop acha o lugar certo sem caso especial.

O problema está no nome. O detector copia o nome do frame para `Location.Function`, então o Resultado
mostra `...Lines-range1`, um nome que não aparece em nenhum arquivo e que não dá para procurar no
código. Na avaliação, o gabarito traz a função declarada (`...Lines`), e a métrica Causa erraria um
cenário em que arquivo e linha estão certos.

O `goindex` já resolve esse nome: `FuncRange` remove `-rangeN` (e `.funcN`) para achar a `FuncDecl`
que envolve o corpo (`internal/adapter/goindex/index.go`, `rangeSuffix`).

## Decisão

Ao montar `Location.Function`, o detector remove todas as ocorrências do sufixo `-rangeN` (`N` = um ou
mais dígitos), com a mesma expressão regular do `goindex`.

- `File` e `Line` continuam os do frame: apontam a linha dentro do corpo do `for range`.
- Nomes sem `-rangeN` não mudam, inclusive closures (`.funcN`) e genéricos (`[...]`).
- A regra vale para todo Resultado, não só para a avaliação.

## Alternativas consideradas

- **Manter o nome do frame.** Não muda nada, mas o Resultado aponta uma função que ninguém acha no
  código, e o cenário de range-over-func erra Causa e Estrito com arquivo e linha certos. Perdeu porque
  o Resultado deve citar o que um humano procura no código.
- **Normalizar só na pontuação** (o avaliador remove o sufixo antes de comparar com o gabarito). Acerta
  o placar, mas deixa o Resultado com o nome sintético. Perdeu porque esconderia na métrica um defeito
  que o usuário veria.
- **Aceitar `-rangeN` no gabarito.** Mesmo problema da anterior, e obriga quem escreve o cenário a prever
  o número que o compilador escolhe.

## Consequências

- Positivo: `Location.Function` é sempre uma função que existe no código, a mesma que o `goindex` usa para
  `FuncRange` e para a correlação com commits.
- Negativo: a expressão regular fica em dois pacotes (`detector`, de domínio, e `goindex`, adapter). O
  domínio não pode importar o adapter, e um pacote compartilhado só para isso seria um `util`.
- Risco: corpos aninhados (`-range1` dentro de `-range2`) passam a ter o mesmo nome da função declarada.
  É aceitável porque a linha os diferencia.
- Closures (`.funcN`) continuam com o nome do runtime. Isso não foi decidido aqui e volta se um cenário
  com closure (errgroup) mostrar o mesmo problema.

## Como validar

- Teste unitário do detector: o frame `example.com/m/pkg.(*Service).Lines-range1` gera
  `Function = example.com/m/pkg.(*Service).Lines`, com `File` e `Line` do frame. Nomes com `.funcN` e
  genéricos não mudam.
- Avaliação: o cenário de range-over-func acerta Causa com o gabarito na função declarada.
