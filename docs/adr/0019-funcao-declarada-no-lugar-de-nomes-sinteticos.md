---
id: ADR-0019
title: Reportar a função declarada no lugar dos nomes sintéticos de range-over-func e closures
status: Aceito
date: 2026-11-23
summary: Substitui o ADR-0016. O detector remove de Location.Function os sufixos -rangeN e a cadeia de closures (.funcN e .N aninhados), porque esses nomes não existem no código. Closures de nível de pacote mantêm o nome original; gowrapN, deferwrapN e -fm ficam fora. File e Line continuam os do frame.
modules: [detector, goindex, eval]
---

# 0019. Reportar a função declarada no lugar dos nomes sintéticos

> Substitui o [ADR-0016](0016-funcao-declarada-no-lugar-do-corpo-range-over-func.md), que tratava só
> de `-rangeN` e deixava as closures em aberto. Este ADR reúne a regra completa.

## Contexto

O detector copia o nome do frame escolhido pela regra do loop (ADR-0002) para `Location.Function`.
O runtime dá nomes sintéticos a código que não é uma função declarada:

- corpo de `for range` sobre iterador (`iter.Seq`, `iter.Seq2`): sufixo `-rangeN`, por exemplo
  `example.com/m/pkg.(*Service).Lines-range1`;
- closure: sufixo `.funcN`, e closure dentro de closure ganha `.N` a mais, por exemplo
  `example.com/m/pkg.(*Service).Fetch.func1` e `...Fetch.func1.2`.

Em goroutines lançadas num loop (errgroup), a pilha termina na closure, cuja linha está dentro do
loop que a lança. A regra do loop acerta o arquivo e a linha, mas o nome reportado é `...Fetch.func1`.
Esse nome não aparece em nenhum arquivo, não dá para procurar no código, e o número muda quando alguém
adiciona outra closure antes dessa na mesma função. O gabarito da avaliação traz a função declarada,
e a comparação de Causa é exata: com o nome sintético, Causa e Estrito erram com arquivo e linha certos.

O `goindex` já resolve os dois casos: `FuncRange` remove `-rangeN` e descarta os tokens depois da
função declarada (`internal/adapter/goindex/index.go`, `rangeSuffix` e `symbolTokens`).

## Decisão

Ao montar `Location.Function`, o detector reporta a função declarada que contém o frame:

1. Remove todas as ocorrências de `-rangeN` (`N` = um ou mais dígitos), com a mesma expressão regular
   do `goindex`.
2. Depois, remove do fim do nome a cadeia de closures: um `.funcN` seguido de zero ou mais `.N`
   (`.func1`, `.func1.2`, `.func3.1.1`).
3. **Closures de nível de pacote mantêm o nome original.** Uma closure no inicializador de uma
   variável de pacote é nomeada `pkg.glob..func1` ou `pkg.init.func1`, conforme a versão do compilador.
   Remover o sufixo daria `pkg.glob.` (malformado) ou `pkg.init`, que pode não corresponder a nenhum
   `func init` declarado. Nesses casos não há nome declarado legível, e o nome do frame fica como está.
4. **Ficam fora** e não mudam:
   - `gowrapN` e `deferwrapN`, os wrappers que o compilador gera para `go f(x)` e `defer f(x)`;
   - `-fm`, o sufixo de method value (`(*T).M-fm`);
   - genéricos (`[...]`).

   Nenhum desses é o frame de um loop nos cenários atuais. Se um cenário mostrar o problema, a regra
   volta para um ADR novo.

`File` e `Line` continuam os do frame: apontam a linha dentro do corpo do `for range` ou da closure.
A regra vale para todo Resultado, não só para a avaliação.

## Alternativas consideradas

- **Manter o nome do frame.** Não muda nada, mas o Resultado aponta uma função que ninguém acha no
  código, e os cenários de range-over-func e de errgroup erram Causa e Estrito com arquivo e linha
  certos. Perdeu porque o Resultado deve citar o que um humano procura no código.
- **Normalizar só na pontuação.** Acerta o placar, mas deixa o Resultado com o nome sintético. Perdeu
  porque esconderia na métrica um defeito que o usuário veria.
- **Aceitar o nome sintético no gabarito.** Obriga quem escreve o cenário a prever o número que o
  compilador escolhe, e o gabarito quebra quando uma closure é adicionada antes. Perdeu.
- **Remover também `gowrapN`, `deferwrapN` e `-fm`.** Sem cenário que precise disso, seria regra sem
  teste de ponta a ponta. Perdeu por ora.

## Consequências

- Positivo: `Location.Function` é sempre uma função que existe no código, a mesma que o `goindex` usa
  para `FuncRange` e para a correlação com commits.
- Negativo: a regra fica em dois pacotes (`detector`, de domínio, e `goindex`, adapter). O domínio não
  pode importar o adapter, e um pacote compartilhado só para isso seria um `util`.
- Risco: corpos aninhados e closures diferentes da mesma função passam a ter o mesmo nome. É aceitável
  porque a linha os diferencia.
- Closures de nível de pacote continuam com o nome do runtime. O Resultado não fica pior do que antes.

## Como validar

Testes unitários do detector:

- `example.com/m/pkg.(*Service).Lines-range1` → `example.com/m/pkg.(*Service).Lines`;
- `example.com/m/pkg.(*Service).Fetch.func1` → `example.com/m/pkg.(*Service).Fetch`;
- `example.com/m/pkg.(*Service).Fetch.func1.2` → `example.com/m/pkg.(*Service).Fetch`;
- `example.com/m/pkg.Run-range1.func2` → `example.com/m/pkg.Run`;
- `example.com/m/pkg.glob..func1` e `example.com/m/pkg.init.func1` → nome original;
- `...(*T).M-fm`, `...Run.gowrap1`, `...Run.deferwrap1` e genéricos `[...]` → nome original;
- em todos, `File` e `Line` são os do frame.

Avaliação: os cenários de range-over-func e de errgroup acertam Causa com o gabarito na função
declarada.
