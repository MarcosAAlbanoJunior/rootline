---
id: ADR-0002
title: Localizar a origem de queries pela pilha da aplicação capturada no span de banco
status: Proposto
date: 2026-11-22
summary: O wrapper de banco anexa ao span os frames da aplicação (code.*); o índice (AST) acha o frame cujo ponto de chamada está dentro de um loop; sem a captura, cai para a rota HTTP → handler, como localização aproximada. Pendente de spike em Go.
modules: [dominio, indice, demo-app, eval]
---

# 0002. Localizar a origem de queries pela pilha da aplicação capturada no span de banco

> Histórico: a primeira versão deste ADR, para Java (agente OTel, Spring Data, Hibernate e
> extensão de stack trace), nunca foi aceita e foi reescrita depois do ADR-0003 (Go).

## Contexto

O Resultado precisa apontar a função e a linha responsáveis pela regressão. Em Go não há agente
automático: a instrumentação padrão é por biblioteca. Fatos verificados na pesquisa
`docs/research/2026-11-22-instrumentacao-go.md`:

- **otelhttp** cria o span do servidor HTTP com `http.route`.
- **exaring/otelpgx** (pgx nativo) cria os spans das queries com o SQL. Não tem opção de
  localização no código nem de atributos dinâmicos por span; para isso é preciso um
  `pgx.QueryTracer` próprio que embrulhe o dele.
- **XSAM/otelsql** (`database/sql`) tem `AttributesGetter`, chamado a cada span criado, onde dá
  para adicionar atributos.
- **OBI (eBPF)** é estável e rastreia `database/sql` e pgx sem mudar código, mas não produz
  localização no código e exige Linux 5.8+, BTF e privilégios.
- `code.function.name`, `code.file.path` e `code.line.number` são estáveis na semconv.

**Ponto crítico:** no N+1, a função que executa a query (ex.: `repo.FindItems`) não é onde está o
bug. O bug é o loop que a chama, muitas vezes em outra camada. Um único frame, o chamador
imediato, aponta o lugar errado.

## Decisão

**O que o Rootline exige do serviço analisado:** OTel Go SDK, otelhttp, o wrapper de banco
padrão e um **hook de captura do chamador**. É um pacote pequeno publicado pelo Rootline, com
duas formas: um `pgx.QueryTracer` que embrulha o do otelpgx e uma função pronta para o
`AttributesGetter` do otelsql. O hook vale igualmente para todas as queries. Continua proibido
criar spans manuais onde sabemos que está o bug.

**Banco no demo-app:** pgx nativo, com o hook compondo com o tracer do otelpgx. Antes de
escrever um embrulho do zero, o spike verifica se o pgx já oferece composição de tracers.
Plano B: `database/sql` via `pgx/stdlib` + otelsql com `AttributesGetter`.

**O hook** usa `runtime.Callers` e anexa ao span de banco os primeiros frames que pertencem ao
módulo da aplicação. O módulo vem de `debug.ReadBuildInfo` ou de configuração; frames da stdlib,
do pgx e do OTel ficam fora. O mais interno vai em `code.function.name`, `code.file.path` e
`code.line.number`. A pilha curta da aplicação vai em um atributo próprio, com **profundidade
máxima fixa (8 frames)** e **tamanho de atributo limitado**.

**OBI (eBPF)** fica registrado como fonte complementar futura de spans, não descartado. Ele não
substitui o hook porque não fornece localização.

**Localização em dois níveis**, com a confiança marcada no Resultado:

1. **Pilha da aplicação no span de banco.** As linhas vêm dos frames e correspondem ao SHA em
   execução. Para N+1, o índice (AST) percorre os frames do mais interno para fora e escolhe o
   primeiro cujo ponto de chamada está dentro de um `for`/`range`. Isso também cobre o
   goroutine/errgroup: a pilha da goroutine termina na closure, e o índice verifica se o literal
   da função está dentro de um loop. Confiança alta.
   Com range-over-func (`for x := range seq`), o corpo do loop vira uma função sintética chamada
   pelo iterador a cada iteração. O índice precisa reconhecer que esse frame pertence ao corpo
   do `for range` da função que o contém, e que esse corpo roda a cada iteração.
2. **Fallback, sem o hook:** `http.route` do span do servidor → índice localiza o registro da
   rota (`mux.HandleFunc("GET /orders", ...)`) → função handler, marcada como "localização
   aproximada" (função, sem linha).

**Localizar não é classificar.** A regra do frame dentro de um loop só diz onde está o código
responsável. Quem decide que algo é N+1 é o detector, pela evidência do trace: a mesma query
normalizada repetida sob o mesmo span pai, com parâmetros variando. Uma query repetida dentro de
um loop não é N+1 por si só, e o distrator de paginação existe para provar isso.

**Variações de N+1 da avaliação** (substituem o lazy loading do Java):
- N+1 escondido atrás de um helper em outra camada (o loop no service, a query no repository);
- N+1 dentro de goroutines lançadas num loop (errgroup);
- N+1 dentro de um iterador com range-over-func;
- **distrator:** queries repetidas legítimas (paginação em lotes fixos), para medir falso positivo.

## Spike que decide este ADR (timebox: 1 dia)

Demo-app Go mínimo (net/http + pgx), com N+1 explícito, N+1 atrás de helper, N+1 em errgroup e
N+1 em iterador range-over-func, cada um com e sem o hook. Verificar:

- spans e atributos realmente emitidos pelo otelhttp e pelo otelpgx em cada cenário;
- se o pgx já oferece composição de tracers; senão, se um embrulho de `pgx.QueryTracer` consegue
  enriquecer o span criado pelo otelpgx, e em que ordem;
- se a regra "primeiro frame com o ponto de chamada dentro de um loop" acerta nos quatro cenários,
  inclusive como aparecem os frames do corpo de um range-over-func;
- **o custo do hook, medido em separado:** capturar a pilha (`runtime.Callers`) e resolver os
  frames (nome de função e linha), por query, além da latência p50/p95 do demo-app com e sem o hook;
- o nome a usar para a pilha (`code.stacktrace`, se o status na semconv permitir, ou um atributo `rootline.*`).

**Entregável:** documento com os spans e atributos reais de cada cenário, anexado a este ADR como evidência.

## Alternativas consideradas

- **Só as bibliotecas padrão, sem hook.** Zero esforço extra no alvo, mas toda localização cai
  no nível 2 (só o handler).
- **OBI (eBPF), sem mudar código.** Estável e zero-code, mas sem localização no código, e com
  exigências de kernel e privilégio. Pode entrar depois como fonte de spans, não de localização.
- **Capturar só o chamador imediato.** Mais barato, mas aponta a função que faz a query, não o
  loop que a multiplica.
- **Spans manuais nas funções da aplicação.** Proibido: instrumentaria exatamente onde sabemos
  que está o bug.

## Consequências

- O Rootline publica e mantém o pacote do hook, com testes e documentação de instalação.
- O índice precisa saber, para uma linha, se ela está dentro de um loop e em qual função ou
  literal de função. A pergunta 7 (qual índice) precisa considerar isso.
- **A avaliação reporta acerto com e sem o hook, mais o overhead medido.**
- Risco: custo do hook em serviços com muitas queries. Mitigações a medir: a profundidade máxima
  (já fixada em 8), um cache de resolução por PC (no N+1 os mesmos PCs se repetem) e amostragem.
- O critério de classificação de N+1 pertence ao detector e é especificado na spec dele, não aqui.

## Como validar

- Documento do spike anexado.
- Testes unitários da escolha de frame e da regra do loop, com pilhas e ASTs em fixtures.
- Avaliação: acerto de localização por nível e por variação, com e sem o hook, mais a latência p50/p95.

## Questões em aberto

- O pgx compõe tracers nativamente? Senão, o embrulho enriquece o span do otelpgx de forma confiável? (spike)
- Qual é o overhead do hook por query, separado em captura e resolução? (spike)
- Como os frames do corpo de range-over-func aparecem na pilha? (spike)
- Limite exato do tamanho do atributo da pilha. (spike)
- Nome do atributo da pilha (spike, com a semconv).
